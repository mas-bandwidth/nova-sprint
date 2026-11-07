package dash

import (
	"context"
	"fmt"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestLoadRefresh5000 measures the refresh path at 5,000 and 20,000 producers: an idle tick, a one-event
// tick and the HTTP JSON. The 5,000-producer worst case must stay under 100 ms.
func TestFunctionalLoadRefresh5000(t *testing.T) {
	t.Parallel()
	requireDashLoad(t)
	for _, n := range []int{5000, 20000} {
		ctx := context.Background()
		f := store.NewFake()
		seedSprint(t, f, n)
		s := &tapStore{Store: f}
		c, err := New(ctx, s)
		if err != nil {
			t.Fatal(err)
		}
		worst := time.Duration(0)
		var idle, one, js time.Duration
		for i := 0; i < 5; i++ {
			t0 := time.Now()
			if err = c.Advance(ctx, s); err != nil { // no events
				t.Fatal(err)
			}
			idle = time.Since(t0)
			mustApply(t, f, fmt.Sprintf("r%d", i), 1, store.SetCardReason{Card: "p00001", Reason: fmt.Sprint(i)})
			t1 := time.Now()
			if err = c.Advance(ctx, s); err != nil {
				t.Fatal(err)
			}
			one = time.Since(t1)
			t2 := time.Now()
			rec := httptest.NewRecorder()
			Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/api/sprint", nil))
			js = time.Since(t2)
			worst = max(worst, idle, one, js)
		}
		t.Logf("producers=%d idle-advance=%s one-event-advance=%s json=%s worst=%s snapshots=%d", n, idle, one, js, worst, s.snapshots)
		if n == 5000 && worst > 100*time.Millisecond {
			t.Errorf("5,000-card refresh %s: less than 10x headroom on the 1 s bar", worst)
		}
	}
}

// TestLoadMeasure5000 logs the idle tick, one-event tick and JSON (plain and gzip) times at 5,000 producers on
// the in-memory store.
func TestFunctionalLoadMeasure5000(t *testing.T) {
	t.Parallel()
	requireDashLoad(t)
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 5000)
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Advance(ctx, f); err != nil { // the cold cursor
		t.Fatal(err)
	}
	h := Handler(c)
	var idle, one, js, jsz []time.Duration
	var size, sizez int
	for i := 0; i < 30; i++ {
		t0 := time.Now()
		if err = c.Advance(ctx, f); err != nil {
			t.Fatal(err)
		}
		idle = append(idle, time.Since(t0))
		mustApply(t, f, fmt.Sprintf("m%d", i), 1, store.SetCardReason{Card: "p00001", Reason: fmt.Sprint(i)})
		t1 := time.Now()
		if err = c.Advance(ctx, f); err != nil {
			t.Fatal(err)
		}
		one = append(one, time.Since(t1))
		t2 := time.Now()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/sprint", nil))
		js = append(js, time.Since(t2))
		size = rec.Body.Len()
		t3 := time.Now()
		req := httptest.NewRequest("GET", "/api/sprint", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		jsz = append(jsz, time.Since(t3))
		sizez = rec.Body.Len()
	}
	q := func(d []time.Duration) string {
		s := append([]time.Duration(nil), d...)
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
		return fmt.Sprintf("median=%s p90=%s max=%s", s[len(s)/2], s[len(s)*9/10], s[len(s)-1])
	}
	t.Logf("5000 producers, 30 iterations: idle tick %s; one-event tick %s; JSON %s (%d B); JSON gzip %s (%d B)", q(idle), q(one), q(js), size, q(jsz), sizez)
}

// TestLoadMeasure5000WithRowsAndConsumers logs the startup, tick and JSON times at 5,000 producers in 50
// streams with a work and a read consumer for each (10,000 consumers) and 16 rows, on the in-memory store.
func TestFunctionalLoadMeasure5000WithRowsAndConsumers(t *testing.T) {
	t.Parallel()
	requireDashLoad(t)
	ctx := context.Background()
	f := store.NewFake()
	var w []store.Write
	for i := 0; i < 12; i++ {
		w = append(w, store.PutRow{Name: fmt.Sprintf("m%02d", i)})
	}
	for i := 0; i < 4; i++ {
		w = append(w, store.PutRow{Name: fmt.Sprintf("k%d", i)})
	}
	for i := 0; i < 50; i++ {
		w = append(w, store.PutStream{Name: fmt.Sprintf("s%02d", i)})
	}
	for i := 0; i < 5000; i++ {
		w = append(w, store.PutCard{ID: fmt.Sprintf("p%05d", i), Kind: "producer", Tier: "pro", Stream: fmt.Sprintf("s%02d", i%50)})
	}
	mustApply(t, f, "seed", 1, w...)
	w = nil
	for i := 0; i < 5000; i++ {
		p, st := fmt.Sprintf("p%05d", i), fmt.Sprintf("s%02d", i%50)
		w = append(w, store.PutCard{ID: "w" + p, Kind: "work", Of: p, Stream: st, Tier: "pro", Holder: fmt.Sprintf("m%02d", i%12)},
			store.PutCard{ID: "r" + p, Kind: "read", Of: p, Stream: st, Tier: "pro", Holder: fmt.Sprintf("k%d", i%4)})
	}
	mustApply(t, f, "consumers", 1, w...)
	t0 := time.Now()
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	startup := time.Since(t0)
	if err = c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}
	h := Handler(c)
	var idle, one, js, jsz []time.Duration
	var size, sizez int
	for i := 0; i < 30; i++ {
		t0 := time.Now()
		if err = c.Advance(ctx, f); err != nil {
			t.Fatal(err)
		}
		idle = append(idle, time.Since(t0))
		mustApply(t, f, fmt.Sprintf("m%d", i), 1, store.SetCardReason{Card: "p00001", Reason: fmt.Sprint(i)})
		t1 := time.Now()
		if err = c.Advance(ctx, f); err != nil {
			t.Fatal(err)
		}
		one = append(one, time.Since(t1))
		t2 := time.Now()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/sprint", nil))
		js = append(js, time.Since(t2))
		size = rec.Body.Len()
		t3 := time.Now()
		req := httptest.NewRequest("GET", "/api/sprint", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		jsz = append(jsz, time.Since(t3))
		sizez = rec.Body.Len()
	}
	q := func(d []time.Duration) string {
		s := append([]time.Duration(nil), d...)
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
		return fmt.Sprintf("median=%s p90=%s max=%s", s[len(s)/2], s[len(s)*9/10], s[len(s)-1])
	}
	t.Logf("5000 producers + 10000 consumers, 16 rows, 30 iterations: startup %s; idle tick %s; one-event tick %s; JSON %s (%d B); JSON gzip %s (%d B)", startup, q(idle), q(one), q(js), size, q(jsz), sizez)
}

// TestFunctionalPG5000IncrementalAdvance runs against a throwaway PostgreSQL with 5,000 producers: one
// cold Snapshot, then incremental advances that must each stay under 100 ms.
func TestFunctionalPG5000IncrementalAdvance(t *testing.T) {
	t.Parallel()
	requireDashLoad(t)
	dsn := startPG(t)
	ctx := context.Background()
	var pg *store.PG
	var err error
	for i := 0; i < 100; i++ {
		if pg, err = store.Open(ctx, dsn, ""); err == nil {
			if err = pg.Migrate(ctx); err == nil {
				break
			}
			pg.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	seedSprint(t, pg, 5000)
	s := &tapStore{Store: pg}
	c, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	worst := time.Duration(0)
	for i := 0; i < 20; i++ {
		mustApply(t, pg, fmt.Sprintf("m%d", i), 1, store.SetCardState{Card: fmt.Sprintf("p%05d", i+1), State: "ready"})
		t0 := time.Now()
		if err = c.Advance(ctx, s); err != nil {
			t.Fatal(err)
		}
		worst = max(worst, time.Since(t0))
	}
	fresh, err := pg.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.state, fresh) {
		t.Fatal("fold != PG Snapshot")
	}
	t.Logf("pg 5000: worst advance=%s snapshots=%d", worst, s.snapshots)
	if s.snapshots != 1 || worst > 100*time.Millisecond {
		t.Errorf("snapshots=%d worst=%s", s.snapshots, worst)
	}
}
