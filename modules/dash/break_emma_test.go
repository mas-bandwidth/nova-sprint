package dash

// Break-It campaign probes by Emma (2026-11-06) against dash/ at 2c570d88.
// Each TestBreakDash* fails on 2c570d88 and names the exact sequence and design
// rule it breaks. TestHeldDashAttacks records the adversarial attacks that held.

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// TestAllFinalStreamWithFailuresIsWaiting: a stream has no fail status. Stream
// "s1" has p1 landed and p2 failed (Total=2, Landed=1, Fail=1); nothing is in
// flight and not every card landed, so the stream shows waiting.
func TestAllFinalStreamWithFailuresIsWaiting(t *testing.T) {
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
	if env.Streams[0].State != "waiting" {
		t.Fatalf("all-final stream s1 (1 landed, 1 fail) has envelope Streams[0].State = %q, want %q", env.Streams[0].State, "waiting")
	}
}

// TestBreakDashStreamCostsWorkAndReadExceedTotalCost breaks wire.go:64-74 against
// dash/README.md line 16 and modules/dash/page/app.js line 353:
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
	f := store.NewFake()
	seedSprint(t, f, 20)
	c := boardOf(t, f)

	// 1. Stale alarm triggers 503 on /healthz once run has published nothing
	// for 35s, while /api/sprint continues serving the last good view.
	now := c.at.Add(35 * time.Second)
	c.now = func() time.Time { return now }
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
