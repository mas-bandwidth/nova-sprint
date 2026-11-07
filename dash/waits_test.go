package dash

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestBoardSaysWhyWaiting: the envelope carries one line per stream whose
// producers wait (as check says it, steps.Waits), a long chain on its
// stream's line, and the summary over every stream with its command; a
// sprint with nothing waiting carries neither field.
func TestBoardSaysWhyWaiting(t *testing.T) {
	p := func(id, stream, state string, needs []string, paths ...string) store.Card {
		return store.Card{ID: id, Kind: "producer", Stream: stream, State: state, Tier: "pro", Needs: needs, Paths: paths}
	}
	s := store.Snapshot{Streams: map[string]store.Stream{"a": {Name: "a"}, "b": {Name: "b"}}, Cards: map[string]store.Card{
		"a1": p("a1", "a", "working", nil, "x.go"),
		"a2": p("a2", "a", "waiting", []string{"a1"}),
		"a3": p("a3", "a", "waiting", []string{"a2"}),
		"a4": p("a4", "a", "waiting", []string{"a3"}),
		"a5": p("a5", "a", "waiting", nil, "x.go"),
		"b1": p("b1", "b", "ready", nil),
	}}
	env := mapSnapshot(s, testRoster, time.Time{})
	want := []waitLine{{Stream: "a", Why: "3 wait on a1 (working); 1 held behind x.go of a1; longest NEEDS chain 4: a1 -> a2 -> a3 -> a4"}}
	if len(env.Waits) != 1 || env.Waits[0] != want[0] {
		t.Fatalf("waits %+v, want %+v", env.Waits, want)
	}
	if !strings.HasPrefix(env.WaitsSummary, "1 chain (longest 4, top: a1 blocks 3), 1 overlap (top: a1 holds 1 on x.go); each stream's waits and chains: nova-sprint check") {
		t.Fatal(env.WaitsSummary)
	}
	b, err := json.Marshal(env)
	if err != nil || !strings.Contains(string(b), `"waits":[{"stream":"a","why":"3 wait on a1`) {
		t.Fatalf("%v %s", err, b)
	}
	s.Cards = map[string]store.Card{"b1": p("b1", "b", "ready", nil)}
	b, err = json.Marshal(mapSnapshot(s, testRoster, time.Time{}))
	if err != nil || strings.Contains(string(b), `"waits`) {
		t.Fatalf("%v %s", err, b)
	}
}

// TestReadCostIsInTheTierSpend: a read's priced cost (read --cost) counts
// in its stream's cost, its tier's spend (the pie) and the read subtotal.
func TestReadCostIsInTheTierSpend(t *testing.T) {
	s := store.Snapshot{Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"p.w1": {ID: "p.w1", Kind: "work", Of: "p", Stream: "s", State: "done", Tier: "pro", Cost: 420_000, Priced: true},
		"p.r1": {ID: "p.r1", Kind: "read", Of: "p", Stream: "s", State: "done", Tier: "heavy", Cost: 40_000, Priced: true},
	}}
	env := mapSnapshot(s, testRoster, time.Time{})
	w := env.Tables["work"].(map[string]*workRow)["s"]
	if w.Cost.Int64() != 46 || w.ByTier["pro"].Int64() != 42 || w.ByTier["heavy"].Int64() != 4 {
		t.Fatalf("cost %v by tier %v", w.Cost, w.ByTier)
	}
	if env.StreamCosts["s"].amounts["read_cost"].Int64() != 40_000 {
		t.Fatalf("read cost %v", env.StreamCosts["s"].amounts)
	}
}
