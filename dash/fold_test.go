package dash

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestFoldMatchesSnapshotAndRebuild checks that one Snapshot is taken, the read position advances by at
// most one event per call, and the folded state equals both a fresh Snapshot and Rebuild of every event.
func TestFoldMatchesSnapshotAndRebuild(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 200)
	s := &tapStore{Store: f}
	c, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	moves := [][]store.Write{
		{store.SetRowState{Row: "amd-1", State: "up"}},
		{store.SetRowState{Row: "friendG", State: "up"}, store.SetRowHeld{Row: "friendG", Held: true, Reason: "x"}},
		{store.SetCardState{Card: "p00000", State: "ready"}, store.SetCardState{Card: "wp00000", State: "ready"}},
		{store.SetCardState{Card: "wp00000", State: "working"}},
		{store.SetCardCost{Card: "wp00000", Cost: 1234567, Priced: true}, store.SetCardState{Card: "wp00000", State: "done"}},
		{store.SetCardState{Card: "rp00000", State: "ready"}},
		{store.SetCardState{Card: "rp00000", State: "working"}},
		{store.SetCardResult{Card: "rp00000", Result: "ok"}, store.SetCardAuthor{Card: "rp00000", Author: "amd-1"}, store.SetCardState{Card: "rp00000", State: "done"}},
		{store.SetCardState{Card: "p00000", State: "landed"}},
		{store.SetCardState{Card: "p00010", State: "blocked"}},
		{store.SetCardState{Card: "p00020", State: "fail"}},
		{store.SetStreamHeld{Stream: "s03", Held: true}},
		{store.PutJudgement{ID: "j1", Kind: "red", About: "s03", Question: "?"}},
		{store.SetCursor{Name: "someone", Seq: 3}},
		{store.SetCardState{Card: "p00000", State: "landed"}}, // refused: already landed
	}
	for i, m := range moves {
		apply(t, f, fmt.Sprintf("m%d", i), 1, m...)
		if err = c.Advance(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	fresh, err := f.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.state, fresh) {
		t.Fatalf("fold != Snapshot")
	}
	all, err := f.Events(ctx, 0, store.MaxEvents)
	if err != nil {
		t.Fatal(err)
	}
	if rb := store.Rebuild(all); !reflect.DeepEqual(rb, fresh) {
		t.Fatalf("Rebuild(all) != Snapshot")
	}
	if viewJSON(t, c.state) != viewJSON(t, fresh) {
		t.Fatal("view of fold != view of Snapshot")
	}
	if s.snapshots != 1 {
		t.Fatalf("Snapshot calls %d", s.snapshots)
	}
	// Each advance first checks that its current sequence still exists, then
	// reads after it. Cursor catch-up may add a read at the same sequence.
	for i := 1; i < len(s.cursors); i++ {
		if s.cursors[i] < s.cursors[i-1] || s.cursors[i] > s.cursors[i-1]+1 {
			t.Fatalf("cursor did not resume: %v", s.cursors)
		}
	}
	t.Logf("snapshots=%d events-cursors=%v", s.snapshots, s.cursors)
}

// TestClearEmptiesViewAndUnknownOpFolds checks that a clear empties the view and moves the epoch, and that
// an op kind the fold has never seen still folds.
func TestClearEmptiesViewAndUnknownOpFolds(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 20)
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.Apply(ctx, store.Op{ID: "odd", Epoch: 1, Actor: "x", Kind: "frobnicate", At: time.Now(), Writes: []store.Write{store.SetCardState{Card: "p00001", State: "landed"}}})
	if err != nil || !r.Accepted {
		t.Fatalf("odd op: %v %+v", err, r)
	}
	if err = c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}
	if d := c.pageSnapshot().Data; d.Landed != 1 || d.All != 20 {
		t.Fatalf("after odd kind landed=%d all=%d", d.Landed, d.All)
	}
	if _, err = f.Clear(ctx, store.Op{ID: "clear", Epoch: 1, Actor: "x", Kind: "clear", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err = c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}
	d := c.pageSnapshot().Data
	if d.Epoch != 2 || d.Landed != 0 || d.All != 0 || len(d.Tables["work"].(map[string]*workRow)) != 0 {
		t.Fatalf("after clear epoch=%d landed=%d all=%d", d.Epoch, d.Landed, d.All)
	}
}

// TestLostConnectionKeepsViewThenRecovers checks that a lost connection keeps the copy and its clock,
// observes the health grace, and recovers on the next successful advance.
func TestLostConnectionKeepsViewThenRecovers(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 20)
	s := &tapStore{Store: f}
	c, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	before := c.pageSnapshot().Data
	s.fail = true
	mustApply(t, f, "move", 1, store.SetCardState{Card: "p00001", State: "landed"})
	if err = c.Advance(ctx, s); err == nil {
		t.Fatal("no error")
	}
	d := c.pageSnapshot().Data
	if d.OK || d.At != before.At || d.Landed != 0 {
		t.Fatalf("on error ok=%v at moved=%v landed=%d", d.OK, d.At != before.At, d.Landed)
	}
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 {
		t.Fatalf("healthz during grace %d", rec.Code)
	}
	c.at = c.at.Add(-33 * time.Second)
	rec = httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 503 {
		t.Fatalf("healthz after grace %d", rec.Code)
	}
	s.fail = false
	if err = c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	if d = c.pageSnapshot().Data; !d.OK || d.Landed != 1 || s.snapshots != 1 {
		t.Fatalf("after recovery ok=%v landed=%d snapshots=%d", d.OK, d.Landed, s.snapshots)
	}
}

// TestRefreshCountsAndShape is a small deterministic counterpart to the load measurements: one Snapshot,
// an incremental fold equal to it, and the page envelope's shape.
func TestRefreshCountsAndShape(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 20)
	s := &tapStore{Store: f}
	c, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	mustApply(t, f, "shape-land", 1, store.SetCardState{Card: "p00001", State: "landed"})
	if err := c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	fresh, err := f.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.snapshots != 1 || !reflect.DeepEqual(c.state, fresh) {
		t.Fatal("incremental fold differs from snapshot")
	}
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/api/sprint", nil))
	var got struct {
		OK   bool `json:"ok"`
		Data struct {
			All    int `json:"all"`
			Landed int `json:"landed"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || !got.OK || got.Data.All != 20 || got.Data.Landed != 1 {
		t.Fatalf("unexpected page shape: %s", rec.Body.String())
	}
}

// The fold equals Rebuild(all) and Snapshot when one Events page carries a policy
// activation, a clear and writes after it; the copy's cursors resume exactly where the
// previous call ended (after + returned), never re-reading or skipping.
type eventSpy struct {
	store.Store
	mu    sync.Mutex
	calls [][2]int64 // after, returned
}

func (s *eventSpy) Events(ctx context.Context, after int64, max int) ([]store.Event, error) {
	e, err := s.Store.Events(ctx, after, max)
	s.mu.Lock()
	s.calls = append(s.calls, [2]int64{after, int64(len(e))})
	s.mu.Unlock()
	return e, err
}

// TestFoldAcrossPolicyActivationAndClear checks that the fold equals Rebuild of every event and a fresh
// Snapshot when one Events page carries a policy activation, a clear and writes after it, and that the cursors
// resume exactly where the previous call ended, never re-reading or skipping.
func TestFoldAcrossPolicyActivationAndClear(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 30)
	mustApply(t, f, "pol1", 1, store.PutPolicy{Kind: "data", Name: "a", Body: "{}"})
	mustApply(t, f, "act1", 1, store.SetActivePolicy{Version: 1})
	s := &eventSpy{Store: f}
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
	if d := c.pageSnapshot().Data; d.Epoch != 2 || d.All != 1 {
		t.Fatalf("view after clear epoch=%d all=%d", d.Epoch, d.All)
	}
}

// Inject deadline errors in the cursor Apply and Replay. Both must receive a
// deadline-bearing context, and a later successful tick must recover.
type deadlineProbeStore struct {
	store.Store
	hangApply, hangReplay         bool
	applyDeadline, replayDeadline bool
}

func (s *deadlineProbeStore) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if s.hangApply {
		_, s.applyDeadline = ctx.Deadline()
		return store.Receipt{}, fmt.Errorf("%w: %w", store.ErrUnknown, context.DeadlineExceeded)
	}
	return s.Store.Apply(ctx, op)
}

func (s *deadlineProbeStore) Replay(ctx context.Context, id string) (store.Receipt, bool, error) {
	if s.hangReplay {
		_, s.replayDeadline = ctx.Deadline()
		return store.Receipt{}, false, context.DeadlineExceeded
	}
	return s.Store.Replay(ctx, id)
}

// TestCursorWriteRunsUnderDeadline checks that the cursor Apply and Replay both receive a context with a
// deadline, and that a later successful tick recovers.
func TestCursorWriteRunsUnderDeadline(t *testing.T) {
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 10)
	s := &deadlineProbeStore{Store: f, hangApply: true, hangReplay: true}
	c, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	err = c.Advance(ctx, s)
	if err == nil || !s.applyDeadline || !s.replayDeadline {
		t.Fatalf("cursor error=%v apply deadline=%v replay deadline=%v", err, s.applyDeadline, s.replayDeadline)
	}
	s.hangApply, s.hangReplay = false, false
	if err = c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	if d := c.pageSnapshot().Data; !d.OK {
		t.Fatal("not recovered")
	}
}

// TestConcurrentTicksAndReads runs concurrent advances and page reads (meant for -race) and checks the fold
// still equals a fresh Snapshot.
func TestConcurrentTicksAndReads(t *testing.T) {
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

// TestTwoDashboardsOnOneStoreCursorEvents logs how many cursor events two idle dashboards on one store
// write; a store is meant to have one dashboard. It asserts nothing.
func TestTwoDashboardsOnOneStoreCursorEvents(t *testing.T) {
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
