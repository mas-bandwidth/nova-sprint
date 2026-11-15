package verbs

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withEmpty is the sprint with a second stream, rocketnet, that no card
// names.
func withEmpty(s *store.Snapshot) {
	s.Streams["rocketnet"] = store.Stream{Name: "rocketnet", Repo: "o/r", Base: "main", Pos: 1, Mode: "auto"}
}

// TestStreamRemove: the coordinator removes a stream no card names: a dry
// run applies nothing; the removal is one op, after which the stream is in
// no read (show, check, the whole snapshot) and the log, folded over the
// store it started from, replays it; stream add then puts it back as a new
// stream, and the log replays that too.
func TestStreamRemove(t *testing.T) {
	t.Parallel()
	s := sprint()
	withEmpty(&s)
	w := worldFrom(t, s)
	start := w.snap(t)
	want(t, w.do("carol", "stream", "remove", "rocketnet", "--reason", "empty", "--dry-run"), 0,
		"STREAM rocketnet removed (dry run)", "next: nova-sprint stream remove rocketnet --reason")
	if _, ok := w.snap(t).Streams["rocketnet"]; !ok {
		t.Fatal("a dry run removed the stream")
	}
	want(t, w.do("carol", "stream", "remove", "rocketnet", "--reason", "empty"), 0, "STREAM rocketnet removed at seq", "next: "+waitNext)
	got := w.snap(t)
	if _, ok := got.Streams["rocketnet"]; ok {
		t.Fatalf("rocketnet is still in a read: %+v", got.Streams["rocketnet"])
	}
	assert.Contains(t, got.Streams, "s", "the other stream stays")
	assert.Equal(t, replayed(t, w, start), got, "the log replays the removal")
	want(t, w.do("carol", "show", "rocketnet"), 1, "rocketnet")
	assert.NotContains(t, w.do("carol", "show").Output, "rocketnet", "the whole snapshot has no removed stream")
	assert.NotContains(t, w.do("", "check").Output, "rocketnet", "check names no removed stream")
	want(t, w.do("carol", "stream", "remove", "rocketnet", "--reason", "again"), 1, "no stream rocketnet in the store")
	want(t, w.do("carol", "stream", "add", "rocketnet", "--repo", "o/r"), 0, "STREAM rocketnet repo=o/r base=main mode=auto forge=none applied at seq")
	back := w.snap(t)
	assert.Equal(t, store.Stream{Name: "rocketnet", Repo: "o/r", Base: "main", Pos: 1, Mode: "auto"}, back.Streams["rocketnet"], "added again as a new stream")
	assert.Equal(t, replayed(t, w, start), back, "the log replays the removal and the add")
}

// TestStreamRemoveRefusals: stream remove refuses with no reason, with no
// name, an unknown stream, a stream any card names in any state (each
// state a card can be in, consumers' done included, names the count and
// the fix), and a worker asking.
func TestStreamRemoveRefusals(t *testing.T) {
	t.Parallel()
	remove := []string{"stream", "remove", "rocketnet", "--reason", "empty"}
	cases := []tcase{
		{name: "no reason", as: "carol", snap: withEmpty, args: []string{"stream", "remove", "rocketnet"}, exit: 1, want: []string{"a stream remove needs its reason", "--reason"}},
		{name: "no name", as: "carol", args: []string{"stream", "remove", "--reason", "r"}, exit: 2, want: []string{"stream remove takes"}},
		{name: "no such stream", as: "carol", args: []string{"stream", "remove", "zz", "--reason", "r"}, exit: 1, want: []string{"no stream zz in the store", "nova-sprint show"}},
		{name: "the stream with cards", as: "carol", args: []string{"stream", "remove", "s", "--reason", "r"}, exit: 1,
			want: []string{"stream s has 4 cards (p1, p1.w1, p2, p2.r1)", "no card of the epoch names it", "nova-sprint hold s --reason"}},
		{name: "a worker", as: "alice", snap: withEmpty, args: remove, exit: 1, want: []string{"stream remove is the coordinator's"}},
	}
	for _, st := range store.CardStates {
		cases = append(cases, tcase{name: "a producer " + st, as: "carol", args: remove, exit: 1,
			snap: func(s *store.Snapshot) {
				withEmpty(s)
				s.Cards["r1"] = store.Card{ID: "r1", Kind: "producer", Stream: "rocketnet", State: st, Tier: "pro", Priority: "normal"}
			},
			want: []string{"stream rocketnet has 1 cards (r1)", "in any state (landed, done and fail included)"}})
	}
	cases = append(cases, tcase{name: "a sentinel landed and a consumer done", as: "carol", args: remove, exit: 1,
		snap: func(s *store.Snapshot) {
			withEmpty(s)
			s.Cards["g1"] = store.Card{ID: "g1", Kind: "sentinel", Stream: "rocketnet", State: "landed", Tier: "pro", Priority: "normal"}
			s.Cards["r1"] = store.Card{ID: "r1", Kind: "producer", Stream: "rocketnet", State: "landed", Tier: "pro", Priority: "normal"}
			s.Cards["r1.w1"] = store.Card{ID: "r1.w1", Kind: "work", Of: "r1", Stream: "rocketnet", State: "done", Attempt: 1, Holder: "alice", Tier: "pro", Priority: "normal"}
		},
		want: []string{"stream rocketnet has 3 cards (g1, r1, r1.w1)"}})
	runCases(t, cases)
}

// TestStreamRemoveCardAddedBetween: a card added to the stream between the
// verb's read and its commit refuses the removal (the store's check, which
// the plan's fresh read names), and the stream stays.
func TestStreamRemoveCardAddedBetween(t *testing.T) {
	t.Parallel()
	s := sprint()
	withEmpty(&s)
	w := worldFrom(t, s)
	snap := w.snap(t)
	r, err := w.f.Apply(context.Background(), store.Op{ID: "between", Epoch: 1, Actor: "carol", Kind: "add", At: t0,
		Writes: []store.Write{store.PutCard{ID: "r1", Kind: "producer", Stream: "rocketnet", Tier: "pro"}}})
	require.NoError(t, err)
	require.True(t, r.Accepted, r.Reason)
	w.e.Actor = "carol"
	_, err = runStreamRemove(w.e, context.Background(), &input{verb: "stream remove", args: []string{"rocketnet"}, vals: map[string][]string{"reason": {"empty"}}, actor: "carol"}, snap)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream rocketnet has 1 cards (r1)")
	assert.Contains(t, w.snap(t).Streams, "rocketnet")
}

// replayed is every event of w's store folded over start, the store it
// began from (store.Fold, the one fold every replay runs).
func replayed(t *testing.T, w *world, start store.Snapshot) store.Snapshot {
	t.Helper()
	var out []store.Event
	for after := start.Seq; ; {
		es, err := w.f.Events(context.Background(), after, store.MaxEvents)
		require.NoError(t, err)
		if len(es) == 0 {
			return store.Fold(start, out)
		}
		out = append(out, es...)
		after = es[len(es)-1].Seq
	}
}
