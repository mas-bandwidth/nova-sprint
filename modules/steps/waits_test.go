package steps

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitCards is a sprint of two streams: in s1 a chain a -> b -> c -> d with
// a working, e and f also needing a, g needing a missing card, h and i
// waiting with their needs landed (the next tick resolves them; h names a's
// file, which holds nothing), a sentinel over d, and a landed and a failed
// card; in s2 one ready card and nothing waiting.
func waitCards() map[string]store.Card {
	p := func(id, stream, state string, needs []string, paths ...string) store.Card {
		return store.Card{ID: id, Kind: "producer", Stream: stream, State: state, Needs: needs, Paths: paths}
	}
	cs := map[string]store.Card{
		"a":    p("a", "s1", "working", nil, "x.go", "corpus/**"),
		"b":    p("b", "s1", "waiting", []string{"z", "a"}),
		"c":    p("c", "s1", "waiting", []string{"b"}),
		"d":    p("d", "s1", "waiting", []string{"c"}),
		"e":    p("e", "s1", "waiting", []string{"a"}),
		"f":    p("f", "s1", "waiting", []string{"a"}),
		"g":    p("g", "s1", "waiting", []string{"gone"}),
		"h":    p("h", "s1", "waiting", []string{"z"}, "x.go", "corpus/**"),
		"i":    p("i", "s1", "waiting", nil, "corpus/**"),
		"z":    p("z", "s1", "landed", nil),
		"q":    p("q", "s1", "fail", nil),
		"k":    p("k", "s2", "ready", nil, "x.go"),
		"st":   {ID: "st", Kind: "sentinel", Stream: "s1", State: "waiting", Needs: []string{"d"}},
		"a.w1": {ID: "a.w1", Kind: "work", Of: "a", Stream: "s1", State: "working"},
	}
	return cs
}

// TestWaitsGroupsByChainFront: each waiting producer is counted once, on
// the front of the chain it waits on (followed through waiting needs); one
// whose needs landed waits on nothing, whatever files it names; the longest
// chain runs first to land first, without sentinels or landed cards.
func TestWaitsGroupsByChainFront(t *testing.T) {
	t.Parallel()
	ws := Waits(waitCards())
	require.Len(t, ws, 2)
	s1, s2 := ws[0], ws[1]
	assert.Equal(t, "s1", s1.Stream)
	assert.Equal(t, []Blocker{{Card: "a", State: "working", Count: 5}, {Card: "gone", State: "missing", Count: 1}}, s1.Blockers,
		"b, c, d, e and f all wait on a (c and d through b); g on a card that does not exist")
	assert.Equal(t, []string{"a", "b", "c", "d"}, s1.Chain)
	assert.Equal(t, "5 wait on a (working), 1 on gone (missing)", s1.Line())
	assert.Equal(t, "s2", s2.Stream)
	assert.Empty(t, s2.Line(), "nothing in s2 waits")
	assert.Equal(t, []string{"k"}, s2.Chain)
	assert.Equal(t, []string{"a", "b", "c", "d"}, LongestChain(waitCards(), "s1"))
	assert.Empty(t, LongestChain(waitCards(), "none"))
}

// TestWaitsLineNamesThreeAndCounts: a line names the three largest and
// counts the rest; a long chain is abbreviated.
func TestWaitsLineNamesThreeAndCounts(t *testing.T) {
	t.Parallel()
	cs := map[string]store.Card{}
	for i := 0; i < 5; i++ {
		front := fmt.Sprintf("f%d", i)
		cs[front] = store.Card{ID: front, Kind: "producer", Stream: "s", State: "ready"}
		for j := 0; j <= i; j++ {
			id := fmt.Sprintf("w%d-%d", i, j)
			cs[id] = store.Card{ID: id, Kind: "producer", Stream: "s", State: "waiting", Needs: []string{front}}
		}
	}
	assert.Equal(t, "5 wait on f4 (ready), 4 on f3 (ready), 3 on f2 (ready) and 2 more cards", Waits(cs)[0].Line())
	assert.Equal(t, "a -> b -> c", ChainText([]string{"a", "b", "c"}))
	assert.Equal(t, "a -> b -> c -> d -> ... -> i", ChainText(strings.Split("a b c d e f g h i", " ")))
}

// TestWaitsSurvivesACycle: a cycle written past add's refusal ends, and
// counts its cards once.
func TestWaitsSurvivesACycle(t *testing.T) {
	t.Parallel()
	cs := map[string]store.Card{
		"a": {ID: "a", Kind: "producer", Stream: "s", State: "waiting", Needs: []string{"b"}},
		"b": {ID: "b", Kind: "producer", Stream: "s", State: "waiting", Needs: []string{"a"}},
	}
	ws := Waits(cs)
	require.Len(t, ws, 1)
	require.Len(t, ws[0].Blockers, 1)
	assert.Equal(t, 2, ws[0].Blockers[0].Count)
	assert.Len(t, ws[0].Chain, 2)
}

// TestIdleWhyAndFix: the idle summary over every stream, and the command
// that moves the top blocker for each of its states.
func TestIdleWhyAndFix(t *testing.T) {
	t.Parallel()
	what, fix := IdleWhy(Waits(waitCards()))
	assert.Equal(t, "2 chains (longest 4, top: a blocks 5)", what)
	assert.Equal(t, "each stream's waits and chains: nova-sprint check; the top: nova-sprint show a (it is working; its landing frees the chain); "+unchain, fix)
	what, fix = IdleWhy(nil)
	assert.Empty(t, what+fix)
	for state, want := range map[string]string{
		"waiting": "nova-sprint show x (it is waiting",
		"ready":   "nova-sprint show x (it is ready",
		"missing": "nova-sprint relink <card> --need x --to <card>",
		"fail":    "nova-sprint relink <card> --need x --to <card>",
		"review":  "nova-sprint show x (it is review",
	} {
		assert.Contains(t, Blocker{Card: "x", State: state}.Fix(), want, state)
	}
	for _, why := range []string{"held: wrong brief", card.MissingRoute + " adb"} {
		assert.Equal(t, `nova-sprint unhold x --reason "<why>" (it waits: `+why+")", Blocker{Card: "x", State: "waiting", Reason: why}.Fix(), "a held blocker is unheld")
	}
}

// TestIdleJudgementSaysWhyNothingDeals: with a row idle and nothing
// dealable, the idle judgement names the chains and the command that helps, not a bare idle.
func TestIdleJudgementSaysWhyNothingDeals(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("r1", 8, "pro")
	s.stream("s1")
	s.producer("p1", "s1", "pro", "a.go")
	s.must("add", store.PutCard{ID: "p2", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p2", Needs: []string{"p1"}})
	s.must("add", store.PutCard{ID: "p3", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p3", Needs: []string{"p2"}})
	s.must("add", store.PutCard{ID: "p4", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p4", Needs: []string{"p1"}})
	s.run(4)
	require.Equal(t, "working", s.state("p1"))
	require.Equal(t, []string{"idle.fleet.1"}, s.openJudgements("idle"))
	assert.Equal(t, "working 0 of 8 open width while 0 ready and 4 waiting: 0 in held streams, 0 with no serving row", s.snap().Judgements["idle.fleet.1"].Question,
		"the first tick resolves p1: something is dealable")
	s.must("expire", store.ExpireJudgement{ID: "idle.fleet.1", Reason: "test"})
	s.run(2)
	require.Equal(t, []string{"idle.fleet.2"}, s.openJudgements("idle"))
	assert.Equal(t, "working 1 of 8 open width while 0 ready and 3 waiting: nothing dealable: 1 chain (longest 3, top: p1 blocks 3); ready: 0 in held streams, 0 with no serving row; each stream's waits and chains: nova-sprint check; the top: nova-sprint show p1 (it is working; its landing frees the chain); "+unchain,
		s.snap().Judgements["idle.fleet.2"].Question)
}

// BenchmarkWaits5000 is Waits over 5,000 producers in 50 streams, each
// stream one chain of 20 and 40 waiting on its front.
func BenchmarkWaits5000(b *testing.B) {
	cs := map[string]store.Card{}
	for s := 0; s < 50; s++ {
		stream := fmt.Sprintf("s%d", s)
		for i := 0; i < 100; i++ {
			id := fmt.Sprintf("%s-%d", stream, i)
			c := store.Card{ID: id, Kind: "producer", Stream: stream, State: "waiting"}
			switch {
			case i == 0:
				c.State = "working"
			case i < 20:
				c.Needs = []string{fmt.Sprintf("%s-%d", stream, i-1)}
			case i < 60:
				c.Needs = []string{stream + "-0"}
			}
			cs[id] = c
		}
	}
	b.ResetTimer()
	for b.Loop() {
		Waits(cs)
	}
}

// TestChainThrough: the longest chain through the named cards runs both up
// their needs and down their dependants, skips landed cards and sentinels,
// and ignores a longer chain elsewhere in the stream.
func TestChainThrough(t *testing.T) {
	t.Parallel()
	p := func(id, state string, needs ...string) store.Card {
		return store.Card{ID: id, Kind: "producer", Stream: "s", State: state, Needs: needs}
	}
	cards := map[string]store.Card{
		"l": p("l", "landed"), "a": p("a", "working", "l"), "b": p("b", "waiting", "a"), "c": p("c", "waiting", "b"),
		"z": {ID: "z", Kind: "sentinel", Stream: "s", State: "waiting", Needs: []string{"c"}}, "d": p("d", "waiting", "z"),
		"x1": p("x1", "ready"), "x2": p("x2", "waiting", "x1"), "x3": p("x3", "waiting", "x2"), "x4": p("x4", "waiting", "x3"), "x5": p("x5", "waiting", "x4"),
		"e": p("e", "ready"),
	}
	for _, tc := range []struct {
		ids  []string
		want string
	}{
		{[]string{"b"}, "a -> b -> c -> d"},
		{[]string{"e"}, "e"},
		{[]string{"e", "a"}, "a -> b -> c -> d"},
		{[]string{"x3"}, "x1 -> x2 -> x3 -> x4 -> x5"},
	} {
		if got := strings.Join(ChainThrough(cards, tc.ids), " -> "); got != tc.want {
			t.Errorf("ChainThrough(%v) = %q, want %q", tc.ids, got, tc.want)
		}
	}
}
