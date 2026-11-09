package verbs

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// chained is the sprint with p1 working on x.go and four producers waiting:
// p3 -> p4 -> p5 on p1 (a chain of four with p1), and p6, which names p1's
// file x.go and waits on nothing.
func chained(s *store.Snapshot) {
	p1 := s.Cards["p1"]
	p1.Paths = []string{"x.go"}
	s.Cards["p1"] = p1
	wait := func(id string, needs []string, paths ...string) {
		s.Cards[id] = store.Card{ID: id, Kind: "producer", Stream: "s", State: "waiting", Tier: "pro", Priority: "normal", Needs: needs, Paths: paths,
			Brief: "tier: pro\nPATHS: x.go\nTEST: go test ./...\nDONE-WHEN: go test ./...\n\nDo it.\n", DoneWhen: []string{"go", "test", "./..."}}
	}
	wait("p3", []string{"p1"})
	wait("p4", []string{"p3"})
	wait("p5", []string{"p4"})
	wait("p6", nil, "x.go")
}

// landTwo lands two producers of s an hour apart, an hour before now.
func landTwo(t *testing.T, w *world) {
	t.Helper()
	for i, id := range []string{"l1", "l2"} {
		s := w.snap(t)
		applyTest(t, w.f, store.Op{ID: "land-" + id, Epoch: s.Epoch, Actor: "carol", Kind: "test", At: t0.Add(time.Duration(i-2) * time.Hour),
			Writes: []store.Write{store.PutCard{ID: id, Kind: "producer", Stream: "s", Tier: "pro", Brief: "do " + id}, store.SetCardState{Card: id, State: "landed"}}})
	}
}

// TestCheckSaysWhyTheFleetIsIdle: with nothing ready and the fleet under
// its width, the width line warns "nothing dealable" with the chains, each
// stream says why its producers wait, and a chain of four
// or more warns with its time at the stream's land rate.
func TestCheckSaysWhyTheFleetIsIdle(t *testing.T) {
	t.Parallel()
	runCases(t, []tcase{
		{name: "no land rate yet", args: []string{"check"}, exit: 0, snap: chained,
			prep: then(healthy, func(_ *testing.T, w *world) { w.origin.tree = []string{"x.go"} }),
			want: []string{
				"CHECK WARN width: working 2 (2 dealt) of 4 open on 3 rows; 0 ready; nothing dealable: 1 chain (longest 4, top: p1 blocks 3): each stream's waits and chains: nova-sprint check; the top: nova-sprint show p1 (it is working; its landing frees the chain); a NEEDS that is not an output the card consumes: nova-sprint replace <card> --by <its brief without it> --reason \"<why>\"\n",
				"CHECK waits s: 3 wait on p1 (working)\n",
				"CHECK WARN stream s: its longest NEEDS chain is 4 cards: p1 -> p3 -> p4 -> p5; no land rate yet (fewer than two landings in the store's recent history): flatten it before the deal",
			}},
		{name: "at the stream's land rate", args: []string{"check"}, exit: 0, snap: chained,
			prep: then(healthy, landTwo, func(_ *testing.T, w *world) { w.origin.tree = []string{"x.go"} }),
			want: []string{"its longest NEEDS chain is 4 cards: p1 -> p3 -> p4 -> p5; at the stream's land rate (1.0 an hour over its last 2 landings) that is about 4.0 hours"}},
	})
}

// TestCheckQuietWithoutWaits: a sprint with nothing waiting and no long
// chain prints no waits or chain line.
func TestCheckQuietWithoutWaits(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	healthy(t, w)
	r := w.do("", "check")
	want(t, r, 0, "CHECK width: working 2 (2 dealt) of 4 open on 3 rows; 0 ready\n")
	if strings.Contains(r.Output, "waits ") || strings.Contains(r.Output, "NEEDS chain") {
		t.Fatalf("a waits or chain line with nothing waiting:\n%s", r.Output)
	}
}

// TestAddRefusesALongChain: add (and --dry-run) prints the longest NEEDS
// chain through the cards it adds; one longer than the policy's chain_limit
// (3 by default) is refused, naming the chain and how to flatten it, and
// nothing is added. A card that needs nothing is added beside a long chain.
func TestAddRefusesALongChain(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	healthy(t, w)
	want(t, w.do("carol", "add", "--stream", "s", "--brief-file", brief(t, "a1", "p1")), 0, "CHAIN stream s: the longest NEEDS chain through these cards is 2 cards (chain_limit 3): p1 -> a1\n")
	r := w.do("carol", "add", "--stream", "s", "--brief-file", brief(t, "a2", "a1"), "--brief-file", brief(t, "a3", "a2"), "--dry-run")
	want(t, r, 1, "stream s: this add makes a NEEDS chain of 4 cards, over the policy's chain_limit of 3: p1 -> a1 -> a2 -> a3; no land rate yet", "flatten it before the deal")
	landTwo(t, w)
	want(t, w.do("carol", "add", "--stream", "s", "--brief-file", brief(t, "a2", "a1"), "--brief-file", brief(t, "a3", "a2")), 1, "about 4.0 hours")
	if _, ok := w.snap(t).Cards["a2"]; ok {
		t.Fatal("a refused add put a2")
	}
	want(t, w.do("carol", "add", "--stream", "s", "--brief-file", brief(t, "a2", "a1")), 0, "ADDED 1 cards")
	want(t, w.do("carol", "add", "--stream", "s", "--brief-file", brief(t, "a3", "a2")), 1, "chain_limit of 3")
	want(t, w.do("carol", "add", "--stream", "s", "--brief-file", brief(t, "b1", "")), 0, "through these cards is 1 cards", "ADDED 1 cards")
}

// TestChainTime: the land rate over the landings' span, none below two
// landings or over no time, and an unreadable history said with its cause.
func TestChainTime(t *testing.T) {
	t.Parallel()
	at := []time.Time{t0, t0.Add(30 * time.Minute), t0.Add(time.Hour)}
	if got := chainTime(at, 6, nil); got != "at the stream's land rate (2.0 an hour over its last 3 landings) that is about 3.0 hours" {
		t.Fatal(got)
	}
	for _, ts := range [][]time.Time{nil, {t0}, {t0, t0}} {
		if got := chainTime(ts, 6, nil); !strings.HasPrefix(got, "no land rate yet") {
			t.Fatal(got)
		}
	}
	if got := chainTime(at, 6, errors.New("store gone")); got != "the land rate is unreadable (store gone)" {
		t.Fatal(got)
	}
}

// TestLandingsCountsEachLandingOnce: a producer counts when an event moves
// it into landed, in the snapshot's epoch, accepted; never a consumer, a
// repeat or a refused op.
func TestLandingsCountsEachLandingOnce(t *testing.T) {
	t.Parallel()
	landed := store.Card{ID: "p", Kind: "producer", Stream: "s", State: "landed"}
	before := store.Card{ID: "p", Kind: "producer", Stream: "s", State: "merging"}
	ev := func(accepted bool, epoch int, b *store.Card, a store.Card) store.Event {
		e := store.Event{Accepted: accepted, Epoch: epoch, At: t0, After: &store.State{Cards: map[string]store.Card{a.ID: a}}}
		if b != nil {
			e.Before = &store.State{Cards: map[string]store.Card{b.ID: *b}}
		}
		return e
	}
	out := map[string][]time.Time{}
	for _, e := range []store.Event{
		ev(true, 1, &before, landed),
		ev(true, 1, &landed, landed),
		ev(false, 1, &before, landed),
		ev(true, 2, &before, landed),
		ev(true, 1, nil, store.Card{ID: "w", Kind: "work", Stream: "s", State: "landed"}),
	} {
		landings(e, 1, out)
	}
	if len(out["s"]) != 1 {
		t.Fatalf("%v", out)
	}
}

// TestReadTakesCost: read takes --cost as finish does (the same parsing and
// the same write), so a read's spend reaches its card: on a bench (API
// billing) the --cost is charged evidence and the card is priced from its
// tokens alone, so with none it is unpriced (priceReport).
func TestReadTakesCost(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	want(t, w.do("bob", "read", "p2.r1@1", "yes", "--cost", "0.04"), 0, "READ p2.r1")
	if c := w.snap(t).Cards["p2.r1"]; c.Cost != 0 || c.Priced || c.Usage != "billing=api charged=0.04 unpriced=no-tokens" {
		t.Fatalf("cost %d priced %t usage %q, want 0 false and the charged evidence", c.Cost, c.Priced, c.Usage)
	}
	want(t, w.do("bob", "read", "p2.r1@1", "yes", "--cost", "0.0000001"), 2, "--cost is dollars, at most six decimals")
}
