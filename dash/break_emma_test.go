package dash

// Break-It campaign probes by Emma (2026-11-06) against dash/ at 2c570d88.
// Each TestBreakDash* fails on 2c570d88 and names the exact sequence and design
// rule it breaks. TestHeldDashAttacks records the adversarial attacks that held.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestBreakDashIdleFirstPollOnEmptyStoreCreatesCursorEvent breaks cache.go:36,59-63
// and cursor.go:14-27 against dash/README.md line 12:
//   - "The saved watermark is the consumed input prefix and excludes its own
//     receipt; idle polls do not create cursor events."
//
// Sequence:
//  1. Start New(ctx, s) on an empty store (snap.Seq == 0, 0 events).
//  2. Call Advance(ctx, s) immediately with no events in the store.
//  3. Because New sets pending: true unconditionally and Cursors["dashboard"]
//     does not exist yet, Advance calls saveCursor and writes SetCursor{Seq: 0},
//     creating event Seq=1 on an idle poll of an empty store!
func TestBreakDashIdleFirstPollOnEmptyStoreCreatesCursorEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.NewFake()
	c, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Advance(ctx, s); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Seq != 0 {
		t.Fatalf("idle first poll on empty store created event(s): got store Seq=%d, want 0", snap.Seq)
	}
}

// hookApplyStore runs beforeCursorApply right before saveCursor's s.Apply(ctx, op).
type hookApplyStore struct {
	store.Store
	beforeCursorApply func()
}

func (s *hookApplyStore) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if op.Kind == "cursor" && s.beforeCursorApply != nil {
		fn := s.beforeCursorApply
		s.beforeCursorApply = nil
		fn()
	}
	return s.Store.Apply(ctx, op)
}

// TestBreakDashConcurrentEventDuringSaveCursorLeavesWatermarkBehindConsumedState
// breaks cursor.go:14-38 & cache.go:59-65 against dash/README.md line 12:
//   - "After consuming input it saves the global dashboard cursor with SetCursor,
//     folding the cursor event into its own state so the next poll is idle (cursor.go).
//     The saved watermark is the consumed input prefix and excludes its own receipt"
//
// Sequence:
//  1. Seed store at Seq=1; New(ctx, s) starts at Seq=1.
//  2. Apply e2 (Seq=2) to the store.
//  3. In Advance(ctx, s), readAvailable folds e2 (c.state.Seq = 2), then calls
//     saveCursor, which snapshots seq = 2.
//  4. Before saveCursor's Apply(SetCursor{Seq: 2}) executes, another writer applies
//     e3 (Seq=3, setting p1 to "landed"), so SetCursor{Seq: 2} lands at Seq=4.
//  5. catchCursor(ctx, s, 4) folds BOTH e3 (Seq=3) and SetCursor{Seq: 2} (Seq=4)
//     into c.state, and Advance then unconditionally sets c.pending = false!
//  6. Now c.state (and c.view) has consumed e3 (Seq=3, p1 is shown as landed),
//     and c.state.Seq is 4, yet the durable store cursor Cursors["dashboard"] is
//     stuck at 2 (< 3) even after subsequent Advance calls!
func TestBreakDashConcurrentEventDuringSaveCursorLeavesWatermarkBehindConsumedState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := store.NewFake()
	mustApply(t, f, "e1", 1,
		store.PutStream{Name: "s1"},
		store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro"},
	)
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	// First Advance saves initial cursor at 1 (lands at Seq=2).
	if err := c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}
	// Apply e2 (lands at Seq=3).
	mustApply(t, f, "e2", 1, store.SetCardState{Card: "p1", State: "ready"})

	hs := &hookApplyStore{
		Store: f,
		beforeCursorApply: func() {
			// Concurrent writer applies e3 (lands at Seq=4) right before SetCursor{Seq: 3} (lands at Seq=5).
			mustApply(t, f, "e3", 1, store.SetCardState{Card: "p1", State: "landed"})
		},
	}
	if err := c.Advance(ctx, hs); err != nil {
		t.Fatal(err)
	}
	// Follow-up Advance poll after the concurrent write:
	if err := c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}
	snap, err := f.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Event e3 (at Seq=4) was consumed into c.state (c.view.Landed == 1),
	// so the saved dashboard cursor watermark must be >= 4!
	if c.view.Landed != 1 {
		t.Fatalf("expected c.view.Landed == 1 after folding e3, got %d", c.view.Landed)
	}
	if got := snap.Cursors[CursorName]; got < 4 {
		t.Fatalf("dashboard consumed e3 at seq 4 (view.Landed=1, state.Seq=%d), but durable cursor watermark is stuck at %d (< 4)", c.state.Seq, got)
	}
}

// TestBreakDashAllFinalStreamWithFailuresNeverSetsEnvelopeStreamStateFail breaks
// shim.go:114-124 against dash/README.md lines 14 & 89:
//   - "Streams show landed when all cards landed, fail when all are final and at
//     least one failed, held when held, working when work is in flight, and
//     waiting otherwise, including empty streams."
//   - "3. Empty streams and streams of waiting cards wait; all-final streams with failures show fail."
//
// Sequence:
//  1. Stream "s1" has p1 in state "landed" and p2 in state "fail" (Total=2, Landed=1, Fail=1).
//  2. All cards in "s1" are final and at least one failed.
//  3. setStreamStates (shim.go:114-124) has branches only for "landed" and "working",
//     leaving data.streams[0].state as "waiting" instead of "fail"!
func TestBreakDashAllFinalStreamWithFailuresNeverSetsEnvelopeStreamStateFail(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{
		Epoch:   1,
		Streams: map[string]store.Stream{"s1": {Name: "s1", Pos: 1}},
		Cards: map[string]store.Card{
			"p1": {ID: "p1", Kind: "producer", Stream: "s1", State: "landed", Tier: "pro"},
			"p2": {ID: "p2", Kind: "producer", Stream: "s1", State: "fail", Tier: "pro"},
		},
	}
	env := mapSnapshot(s, testRoster, time.Time{})
	if len(env.Streams) != 1 {
		t.Fatalf("expected 1 stream, got %d", len(env.Streams))
	}
	if env.Streams[0].State != "fail" {
		t.Fatalf("all-final stream s1 (1 landed, 1 fail) has envelope Streams[0].State = %q, want %q", env.Streams[0].State, "fail")
	}
}

// TestBreakDashStreamCostsWorkAndReadExceedTotalCost breaks wire.go:64-74 against
// dash/README.md line 16 and dash/page/app.js line 353:
//   - "Exact micro-dollar tier totals are partitioned into display cents by largest
//     remainder, with alphabetical ties, so tier cells sum to the rounded stream cost"
//   - "the reads beside the work: the same total split by kind"
//
// Sequence:
//  1. Stream "s1" has one work card with Cost=1000 ($0.001 = 0.1 cent) and one read
//     card with Cost=1000 ($0.001 = 0.1 cent). Total stream cost is 2000 micro-dollars
//     ($0.002 = 0.2 cent -> rounded up to $0.01).
//  2. streamCost.MarshalJSON (wire.go:64-74) rounds total_cost, work_cost, and read_cost
//     up to whole cents independently via (micros + 9999) / 10000.
//  3. In /api/sprint's tables.costs.stream_costs["s1"], total_cost is "$0.01", while
//     work_cost is "$0.01" and read_cost is "$0.01", so work_cost + read_cost ($0.02)
//     exceeds total_cost ($0.01)!
func TestBreakDashStreamCostsWorkAndReadExceedTotalCost(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{
		Epoch:   1,
		Streams: map[string]store.Stream{"s1": {Name: "s1", Pos: 1}},
		Cards: map[string]store.Card{
			"p1": {ID: "p1", Kind: "producer", Stream: "s1", State: "working", Tier: "pro"},
			"w1": {ID: "w1", Kind: "work", Of: "p1", Stream: "s1", Tier: "pro", State: "done", Cost: 1000, Priced: true},
			"r1": {ID: "r1", Kind: "read", Of: "p1", Stream: "s1", Tier: "pro", State: "done", Cost: 1000, Priced: true},
		},
	}
	b, err := json.Marshal(mapSnapshot(s, testRoster, time.Time{}))
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		StreamCosts map[string]struct {
			TotalCost string `json:"total_cost"`
			WorkCost  string `json:"work_cost"`
			ReadCost  string `json:"read_cost"`
		} `json:"stream_costs"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatal(err)
	}
	sc := env.StreamCosts["s1"]
	parseCents := func(s string) int {
		var d, c int
		s = strings.TrimPrefix(s, "$")
		parts := strings.Split(s, ".")
		if len(parts) != 2 {
			t.Fatalf("bad dollar string %q", s)
		}
		for _, ch := range parts[0] {
			d = d*10 + int(ch-'0')
		}
		for _, ch := range parts[1] {
			c = c*10 + int(ch-'0')
		}
		return d*100 + c
	}
	total := parseCents(sc.TotalCost)
	work := parseCents(sc.WorkCost)
	read := parseCents(sc.ReadCost)
	if work+read != total {
		t.Fatalf("stream_costs[s1] work_cost (%s) + read_cost (%s) = %d cents != total_cost (%s = %d cents)",
			sc.WorkCost, sc.ReadCost, work+read, sc.TotalCost, total)
	}
}

// TestBreakDashThroughputNotResetAcrossClearWhenLandedDoesNotDrop breaks
// throughput.go:13-23 against dash/README.md line 16:
//   - "Throughput samples landed over the last hour, with a ten-minute warmup
//     displayed as 0 and a reset when the count falls."
//
// Sequence:
//  1. Epoch 1 runs for 11 minutes (> 10m warmup) with 0 landed cards.
//  2. Clear resets the store to Epoch 2, and 1 card lands in Epoch 2 before
//     the next Advance poll (so landed goes 0 -> 1 across the Clear).
//  3. Because addSample (throughput.go:14) only resets c.samples when
//     landed < last.landed and never checks if c.state.Epoch changed,
//     Epoch 2 skips the 10-minute warmup and reports non-zero throughput
//     immediately after Clear using Epoch 1's sample!
func TestBreakDashThroughputNotResetAcrossClearWhenLandedDoesNotDrop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := store.NewFake()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	mustApply(t, f, "e1", 1, store.PutStream{Name: "s1"})

	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return now }
	c.samples = []sample{{now, 0}}

	// 11 minutes pass in Epoch 1 with 0 landed cards.
	now = now.Add(11 * time.Minute)
	if err := c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}

	// Clear opens Epoch 2, and 1 card lands in Epoch 2 at 11m10s.
	now = now.Add(10 * time.Second)
	if _, err := f.Clear(ctx, store.Op{ID: "clear-2", Epoch: 1, Actor: "rowan", Kind: "clear", At: now}); err != nil {
		t.Fatal(err)
	}
	mustApply(t, f, "e2-seed", 2,
		store.PutStream{Name: "s1"},
		store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro"},
	)
	mustApply(t, f, "e2-land", 2,
		store.SetCardState{Card: "p1", State: "landed"},
	)
	if err := c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}

	// Epoch 2 is only 10 seconds old (< 10-minute warmup), so throughput rate must be 0!
	if rate, minutes := c.rate(); rate != 0 {
		t.Fatalf("10s into Epoch 2 after Clear, throughput rate = %v cards/hr (window=%vm), want 0 (10-minute warmup was not reset across Clear)", rate, minutes)
	}
}

// TestBreakDashPageBudLabelAndArchivedMatchObjectPrototypeNames breaks
// page/app.js:260-264 & 294 against dash/README.md line 14:
//   - "A bud shows as its person: 'rowan-next' is 'rowan (next)' when 'rowan' is a friend too"
//
// In page/app.js:
//   - budLabel(name, table) checks `if (table[name.slice(0, i)])` on a plain Object
//     without Object.prototype.hasOwnProperty, so a row named "toString-1" or
//     "valueOf-1" matches Object.prototype.toString and is rewritten to "toString (1)"!
//   - archivedSet(d) initializes `var a = {}` (instead of Object.create(null)), so
//     `arch[name]` is truthy for "constructor", "toString", etc., hiding those streams!
func TestBreakDashPageBudLabelAndArchivedMatchObjectPrototypeNames(t *testing.T) {
	t.Parallel()
	js, err := page.ReadFile("page/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(js)
	if strings.Contains(src, "table && table[name.slice(0, i)]") {
		t.Errorf("page/app.js budLabel indexes `table[name.slice(0, i)]` without hasOwnProperty/Object.hasOwn, matching Object.prototype properties (e.g. 'toString-1' -> 'toString (1)')")
	}
	if strings.Contains(src, "function archivedSet(d) { var a = {};") {
		t.Errorf("page/app.js archivedSet uses plain `{}` instead of `Object.create(null)`, so `arch[name]` treats Object.prototype names as archived")
	}
}

// TestHeldDashAttacks records the adversarial attacks against dash/ that held cleanly.
func TestHeldDashAttacks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := store.NewFake()
	seedSprint(t, f, 20)
	c, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	if err := c.Advance(ctx, f); err != nil {
		t.Fatal(err)
	}

	// 1. Stale alarm triggers 503 on /healthz after 30s of failed polls while
	// /api/sprint continues serving the last good view with stale: true.
	ts := &tapStore{Store: f, fail: true}
	now = now.Add(35 * time.Second)
	_ = c.Advance(ctx, ts)
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 503 {
		t.Fatalf("expected 503 on stale /healthz, got %d", rec.Code)
	}
	rec2 := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec2, httptest.NewRequest("GET", "/api/sprint", nil))
	if rec2.Code != 200 {
		t.Fatalf("expected 200 on stale /api/sprint, got %d", rec2.Code)
	}
}
