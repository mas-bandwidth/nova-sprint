package dash

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestTierCellsSumToStreamCost checks that a stream's tier cells partition its rounded total: three
// tiers of 0.1 cent each must not each show 1 cent beside a 1-cent total.
func TestTierCellsSumToStreamCost(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"p": {ID: "p", Kind: "producer", Stream: "s", State: "working", Tier: "pro"},
		"a": {ID: "a", Kind: "work", Of: "p", Stream: "s", Tier: "flash", Cost: 1000, Priced: true},
		"b": {ID: "b", Kind: "work", Of: "p", Stream: "s", Tier: "pro", Cost: 1000, Priced: true},
		"c": {ID: "c", Kind: "work", Of: "p", Stream: "s", Tier: "heavy", Cost: 1000, Priced: true},
	}}
	w := mapSnapshot(s, testRoster, time.Time{}).Tables["work"].(map[string]*workRow)["s"]
	total, tiers := w.Cost.Int64(), int64(0)
	for _, d := range w.ByTier {
		tiers += d.Int64()
	}
	if tiers != total {
		t.Errorf("tier cells sum to %d cents, stream cost %d cents; the tier cells must partition the total", tiers, total)
	}
}

// TestStreamCostsFieldsAreCurrencyStrings checks the stream_costs fields against the reference envelope:
// total_cost, work_cost and read_cost are currency strings, unpriced_runs a number, and work and read
// partition the rounded total.
func TestStreamCostsFieldsAreCurrencyStrings(t *testing.T) {
	t.Parallel()
	ref := readJSON(t, "testdata/reference-envelope.json")["data"].(map[string]any)["stream_costs"].(map[string]any)
	var refCosts map[string]any
	for _, v := range ref {
		if m := v.(map[string]any); m["total_cost"] != nil && m["work_cost"] != nil && m["read_cost"] != nil {
			refCosts = m
			break
		}
	}
	if refCosts == nil {
		t.Fatal("reference envelope has no stream cost entry")
	}
	s := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"p": {ID: "p", Kind: "producer", Stream: "s", State: "working", Tier: "pro"},
		"w": {ID: "w", Kind: "work", Of: "p", Stream: "s", Tier: "pro", State: "done", Cost: 1234567, Priced: true},
		"r": {ID: "r", Kind: "read", Of: "p", Stream: "s", Tier: "pro", State: "done", Cost: 1000, Priced: true},
		"u": {ID: "u", Kind: "work", Of: "p", Stream: "s", Tier: "pro", State: "waiting"},
	}}
	cache := &Cache{view: mapSnapshot(s, testRoster, time.Time{}), at: t1000, now: at1000}
	b, err := json.Marshal(cache.pageSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err = json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	sc := got["data"].(map[string]any)["stream_costs"].(map[string]any)["s"].(map[string]any)
	if sc["total_cost"] != "$1.24" || sc["work_cost"] != "$1.24" || sc["read_cost"] != "$0.00" || sc["unpriced_runs"] != float64(1) {
		t.Fatalf("stream cost values and count: %v", sc)
	}
	for _, k := range []string{"total_cost", "work_cost", "read_cost"} {
		if fmt.Sprintf("%T", sc[k]) != fmt.Sprintf("%T", refCosts[k]) {
			t.Errorf("stream_costs.%s: reference %T (%v), served %T (%v)", k, refCosts[k], refCosts[k], sc[k], sc[k])
		}
	}
}

// TestWorkAndReadCostsSumToStreamTotal checks that a stream's work_cost and read_cost, in cents, add up
// to its total_cost: a 0.1 cent work run and a 0.1 cent read run must not each round up to 1 cent beside a
// 1-cent total.
func TestWorkAndReadCostsSumToStreamTotal(t *testing.T) {
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

// TestExactWorkCostOnTheWire checks that a work cost beyond float64 precision stays exact in cents and in
// the grouped currency cell.
func TestExactWorkCostOnTheWire(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{"a": {Kind: "work", Stream: "s", Tier: "pro", Priced: true, Cost: 9_000_000_000_000_010_000}}}
	d := mapSnapshot(s, testRoster, time.Time{})
	w := d.Tables["work"].(map[string]*workRow)["s"]
	want := big.NewInt(900_000_000_000_001)
	if w.Cost.Cmp(want) != 0 {
		t.Fatalf("cents %v", w.Cost)
	}
	if got := moneyCell(w.Cost); got != "$9,000,000,000,000.01" {
		t.Fatal(got)
	}
}

// TestUnpricedRunKeepsPricedTotal checks that a priced sum stays visible when a stream also has unpriced
// work, in both the view and the page source.
func TestUnpricedRunKeepsPricedTotal(t *testing.T) {
	t.Parallel()
	s := store.Snapshot{Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{"a": {ID: "a", Kind: "work", Stream: "s", Tier: "pro", Cost: 1200000, Priced: true}, "b": {ID: "b", Kind: "work", Stream: "s", Tier: "pro", Priced: false}}}
	d := mapSnapshot(s, testRoster, time.Time{})
	w := d.Tables["work"].(map[string]*workRow)["s"]
	if w.Cost == nil || w.Cost.Int64() != 120 {
		t.Fatalf("priced sum lost: %+v", w)
	}
	js, _ := page.ReadFile("page/app.js")
	if strings.Contains(string(js), "unpriced ? \"\" : money(recorded)") {
		t.Fatal("unpriced run hides priced total")
	}
}
