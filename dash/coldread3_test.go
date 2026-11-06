package dash

// Third cold read of the v2 dashboard shim (2026-11-06), head be777a82.
// Adapted from Rowan’s cold reader; original is preserved in testdata.
// The other tests are checks that passed or measurements (t.Log only).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// cr3WorkGrid is the no-blocked Work grid's minimum width and its row's content
// width, in px, at viewport width vw, from page/index.html's own rules.
func cr3WorkGrid(vw float64) (grid, content float64) {
	const rem = 16.0
	clamp := func(lo, v, hi float64) float64 { return max(lo, min(v, hi)) }
	e := clamp(18.5*rem, 0.45*vw, 42*rem)               // .panel { --E: clamp(18.5rem, 45vw, 42rem) }
	gap := clamp(1.5*rem, 0.10*vw-10.5625*rem, 2.5*rem) // .streams.no-blocked .row --gap
	pad, s := 2*rem, 9.75*rem                           // .streams .row --pad, --S
	name := min(27.75*rem, e-pad-gap-s)                 // --name-w, --name-cap 27.75rem
	status := e - pad - gap - name                      // --status-w
	// name status repeat(6, minmax(5.625rem,1fr)) 4rem minmax(5.625rem,1fr) minmax(5.625rem,1fr) minmax(7.5rem,1.25fr)
	grid = name + status + 6*5.625*rem + 4*rem + 5.625*rem + 5.625*rem + 7.5*rem + 11*gap
	content = vw - 2*2*rem - 2 - 2*pad // body padding 2rem each side, panel border 1px each side, row padding
	return grid, content
}

// At 2000 px with no blocked card the full eleven-column Work grid needs 1894 px and its
// row has 1870 px: it runs 24 px into the row's right padding, so the cost column ends
// 8 px from the panel edge where v1's ends 32 px from it (v2-2000.png vs v1-live-2000.png,
// Work and Total rows). fitTables does not trip because the overflow stays inside the
// padding. README: "the full eleven-column Work table fits at both 2000 and 2800 px".
func TestColdread3DefectWorkGridOverflowsAt2000(t *testing.T) {
	css, err := page.ReadFile("page/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{
		".panel { --E: clamp(18.5rem, 45vw, 42rem); }",
		".streams.no-blocked .row { --gap: clamp(1.5rem, calc(10vw - 10.5625rem), 2.5rem); grid-template-columns: var(--name-w) var(--status-w) repeat(6, minmax(5.625rem, 1fr)) 4rem minmax(5.625rem, 1fr) minmax(5.625rem, 1fr) minmax(7.5rem, 1.25fr); }",
		".streams .row { --name-cap: 27.75rem; }",
		"padding: 2rem 2rem 4rem;",
	} {
		if !strings.Contains(string(css), rule) {
			t.Fatalf("index.html changed; recompute: %s", rule)
		}
	}
	for _, vw := range []float64{2000, 2800} {
		grid, content := cr3WorkGrid(vw)
		t.Logf("vw=%v grid=%.1f content=%.1f overflow=%.1f", vw, grid, content, grid-content)
		if grid > content {
			t.Errorf("at %v px the Work grid's minimum %.0f px exceeds its row's %.0f px by %.0f px (into the 32 px right padding)", vw, grid, content, grid-content)
		}
	}
}

// A read with no holder (waiting, unassigned) makes a Readers row named "" with no cells:
// countRead creates readers[c.Holder] before its switch (shim.go:151). The ?all=1 Readers
// table then shows an unnamed row; v1's readers are named rows only.
func TestColdread3DefectReadersEmptyHolderRow(t *testing.T) {
	s := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"p": {ID: "p", Kind: "producer", Stream: "s", State: "review", Tier: "pro"},
		"r": {ID: "r", Kind: "read", Of: "p", Stream: "s", State: "waiting", Tier: "pro"},
	}}
	readers := mapSnapshot(s, time.Time{}).Tables["readers"].(map[string]numberCells)
	if _, ok := readers[""]; ok {
		t.Errorf("readers table has an unnamed row: %v", readers)
	}
}

// v1 sends ok% "0.0%" (grey) for a row whose ok and failed are both 0: live v1 capture
// friends.alex, fleet.captain and fleet.studio; v1-live-1440.png / v1-live-2000.png show
// "0.0%". v2 sends "-". Round-two ruling 4: "v1's cost, ok%, ETA and throughput fallbacks
// remain". (dash/README.md states the opposite for zero-denominator rows.)
func TestColdread3DefectOkPctZeroDenominator(t *testing.T) {
	v1 := readJSON(t, "testdata/live-v1-envelope.json")["data"].(map[string]any)["tables"].(map[string]any)
	alex := v1["friends"].(map[string]any)["alex"].(map[string]any)
	if alex["ok"] != "0" || alex["failed"] != "0" || alex["okpct"] != "0.0%" {
		t.Fatalf("capture changed: %v", alex)
	}
	s := store.Snapshot{Epoch: 1, Rows: map[string]store.Row{"alex": {Name: "alex", Kind: "kin", Width: 2, State: "down"}}}
	b, err := json.Marshal(mapSnapshot(s, time.Time{}).Tables["friends"])
	if err != nil {
		t.Fatal(err)
	}
	var rows map[string]map[string]string
	if err = json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	if got := rows["alex"]["okpct"]; got != alex["okpct"] {
		t.Errorf("zero-denominator ok%%: v1 %q, v2 %q", alex["okpct"], got)
	}
}

// The fold equals Rebuild(all) and Snapshot when one Events page carries a policy
// activation, a clear and writes after it; the copy's cursors resume exactly where the
// previous call ended (after + returned), never re-reading or skipping.
type cr3Tap struct {
	store.Store
	mu    sync.Mutex
	calls [][2]int64 // after, returned
}

func (s *cr3Tap) Events(ctx context.Context, after int64, max int) ([]store.Event, error) {
	e, err := s.Store.Events(ctx, after, max)
	s.mu.Lock()
	s.calls = append(s.calls, [2]int64{after, int64(len(e))})
	s.mu.Unlock()
	return e, err
}

func TestColdread3FoldPolicyClearOnePage(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 30)
	mustApply(t, f, "pol1", 1, store.PutPolicy{Kind: "data", Name: "a", Body: "{}"})
	mustApply(t, f, "act1", 1, store.SetActivePolicy{Version: 1})
	s := &cr3Tap{Store: f}
	c, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, f, "pol2", 1, store.PutPolicy{Kind: "data", Name: "b", Body: "{}"})
	mustApply(t, f, "act2", 1, store.SetActivePolicy{Version: 2})
	mustApply(t, f, "land", 1, store.SetCardState{Card: "p00003", State: "landed"})
	if _, err = f.Clear(ctx, store.Op{ID: "clr", Epoch: 1, Actor: "x", Kind: "clear", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	mustApply(t, f, "after", 2, store.PutStream{Name: "n"}, store.PutCard{ID: "q", Kind: "producer", Tier: "pro", Stream: "n"})
	if err = c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	fresh, err := f.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	all, err := f.Events(ctx, 0, store.MaxEvents)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.state, fresh) || !reflect.DeepEqual(store.Rebuild(all), fresh) {
		t.Fatalf("fold diverged: policy %d/%d epoch %d/%d", c.state.Policy, fresh.Policy, c.state.Epoch, fresh.Epoch)
	}
	for i := 1; i < len(s.calls); i++ {
		if s.calls[i][0] != s.calls[i-1][0]+s.calls[i-1][1] {
			t.Fatalf("cursor did not resume exactly: %v", s.calls)
		}
	}
	if d := c.pageSnapshot().Data.(envelope); d.Epoch != 2 || d.All != 1 {
		t.Fatalf("view after clear epoch=%d all=%d", d.Epoch, d.All)
	}
}

// Every store call of a tick is under the 500 ms deadline: a hung Apply (the cursor write)
// and a hung Replay end the tick near 500 ms, and the next tick recovers.
type cr3Hang struct {
	store.Store
	hangApply, hangReplay bool
}

func (s *cr3Hang) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if s.hangApply {
		<-ctx.Done()
		return store.Receipt{}, fmt.Errorf("%w: %w", store.ErrUnknown, ctx.Err())
	}
	return s.Store.Apply(ctx, op)
}
func (s *cr3Hang) Replay(ctx context.Context, id string) (store.Receipt, bool, error) {
	if s.hangReplay {
		<-ctx.Done()
		return store.Receipt{}, false, ctx.Err()
	}
	return s.Store.Replay(ctx, id)
}

func TestColdread3DeadlineCoversCursorWrite(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 10)
	s := &cr3Hang{Store: f, hangApply: true, hangReplay: true}
	c, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = c.Advance(ctx, s)
	if el := time.Since(start); err == nil || el > 900*time.Millisecond {
		t.Fatalf("hung cursor write: err=%v elapsed=%s", err, el)
	}
	s.hangApply, s.hangReplay = false, false
	if err = c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	if d := c.pageSnapshot().Data.(envelope); !d.OK {
		t.Fatal("not recovered")
	}
}

// Concurrent ticks and page reads under -race.
func TestColdread3ConcurrentTicksAndReads(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 200)
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(c)
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				for _, p := range []string{"/api/sprint", "/healthz"} {
					rec := httptest.NewRecorder()
					h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
				}
			}
		}()
	}
	for i := 0; i < 50; i++ {
		mustApply(t, f, fmt.Sprintf("c%d", i), 1, store.SetCardReason{Card: "p00001", Reason: fmt.Sprint(i)})
		wg.Add(1)
		go func() { defer wg.Done(); _ = c.Advance(ctx, f) }()
		_ = c.Advance(ctx, f)
	}
	wg.Wait()
	if err = c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}
	fresh, _ := f.Snapshot(ctx)
	if !reflect.DeepEqual(c.state, fresh) {
		t.Fatal("fold != Snapshot after concurrent ticks")
	}
}

// Measurement, not a gate: two dashboard processes on one store (outside the one-dashboard
// design) answer each other's cursor receipts with cursor writes, one event per tick each,
// forever.
func TestColdread3TwoDashboardsCursorEvents(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 10)
	a, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	s0, _ := f.Snapshot(ctx)
	for i := 0; i < 20; i++ {
		_ = a.Advance(ctx, f)
		_ = b.Advance(ctx, f)
	}
	s1, _ := f.Snapshot(ctx)
	t.Logf("two idle dashboards, 20 ticks each, no outside writes: %d cursor events", s1.Seq-s0.Seq)
}

// Measurement: 5,000 producers (+500 work and 500 read consumers) on the in-memory store.
func TestColdread3Measure5000(t *testing.T) {
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

// cr3PG is a throwaway PostgreSQL that the test can stop and start on one port.
type cr3PG struct {
	bin, dir string
	port     int
	p        *exec.Cmd
}

func (g *cr3PG) start(t *testing.T) {
	t.Helper()
	g.p = exec.Command(filepath.Join(filepath.Dir(g.bin), "postgres"), "-D", g.dir, "-p", fmt.Sprint(g.port), "-c", "listen_addresses=127.0.0.1", "-c", "unix_socket_directories=", "-c", "fsync=off")
	if err := g.p.Start(); err != nil {
		t.Fatal(err)
	}
}
func (g *cr3PG) stop() {
	if g.p != nil {
		_ = g.p.Process.Signal(syscall.SIGQUIT)
		_ = g.p.Wait()
		g.p = nil
	}
}
func (g *cr3PG) dsn() string {
	return fmt.Sprintf("postgres://nova@127.0.0.1:%d/postgres?sslmode=disable", g.port)
}

func cr3StartPG(t *testing.T) *cr3PG {
	t.Helper()
	bin, err := exec.LookPath("initdb")
	if err != nil {
		t.Skip("initdb required")
	}
	g := &cr3PG{bin: bin, dir: t.TempDir()}
	if out, err := exec.Command(bin, "-D", g.dir, "-U", "nova", "--auth=trust", "--no-locale").CombinedOutput(); err != nil {
		t.Fatalf("initdb %v: %s", err, out)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g.port = l.Addr().(*net.TCPAddr).Port
	l.Close()
	g.start(t)
	t.Cleanup(g.stop)
	return g
}

// cr3Apply is the test's own write, retried through Replay: the pool may hand the test a
// connection the server already terminated (the test's writer, not the dashboard).
func cr3Apply(t *testing.T, s store.Store, id string, w ...store.Write) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		r, err := s.Apply(ctx, store.Op{ID: id, Epoch: 1, Actor: "test", Kind: "test", At: time.Now(), Writes: w})
		if err == nil && r.Accepted {
			return
		}
		if r, found, e := s.Replay(ctx, id); e == nil && found && r.Accepted {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s: write failed", id)
}

// Functional: killed backends and a full PostgreSQL restart are recovered by later
// ticks without a second Snapshot, and the copy equals a fresh Snapshot.
func TestColdread3FunctionalPGRestartRecovers(t *testing.T) {
	if os.Getenv("NOVA_SKIP_PG") != "" {
		t.Skip()
	}
	g := cr3StartPG(t)
	ctx := context.Background()
	var pg *store.PG
	var err error
	for i := 0; i < 100; i++ {
		if pg, err = store.Open(ctx, g.dsn(), ""); err == nil {
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
	seedSprint(t, pg, 200)
	s := &tapStore{Store: pg}
	c, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	// 1: every other backend terminated
	admin, err := pgx.Connect(ctx, g.dsn())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND backend_type = 'client backend'"); err != nil {
		t.Fatal(err)
	}
	admin.Close(ctx)
	cr3Apply(t, pg, "k1", store.SetCardState{Card: "p00001", State: "ready"})
	recovered, tries := false, 0
	for ; tries < 5 && !recovered; tries++ {
		recovered = c.Advance(ctx, s) == nil
	}
	t.Logf("ticks to recover after terminated backends: %d", tries)
	if !recovered {
		t.Fatal("not recovered after terminated backends")
	}
	// 2: PostgreSQL stopped, ticks fail, then started on the same port
	g.stop()
	for i := 0; i < 3; i++ {
		if err = c.Advance(ctx, s); err == nil {
			t.Fatal("advance succeeded with the store down")
		}
	}
	if d := c.pageSnapshot().Data.(envelope); d.OK {
		t.Fatal("ok while the store is down")
	}
	g.start(t)
	deadline, restarted := time.Now().Add(20*time.Second), time.Now()
	for {
		if err = c.Advance(ctx, s); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not recovered after restart: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("recovered %s after the restart", time.Since(restarted))
	cr3Apply(t, pg, "k2", store.SetCardState{Card: "p00002", State: "ready"})
	if err = c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	fresh, err := pg.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.state, fresh) || s.snapshots != 1 || !c.pageSnapshot().Data.(envelope).OK {
		t.Fatalf("after restart: equal=%v snapshots=%d", reflect.DeepEqual(c.state, fresh), s.snapshots)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
