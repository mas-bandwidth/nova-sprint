package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A sentinel added with a need on a card of its own stream is placed
// directly after that card (its producer), not at the end of the line:
// on a stream a1..a4 a sentinel needing a2 sits between a2 and a3 and
// waits on a1 and a2 only (docs/SPEC-SPRINT.md, sentinel cards).
func TestASentinelIsPlacedAfterItsProducer(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"a1", "a2", "a3", "a4"}}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s-gate"}, Sentinel: true, Needs: []string{"a2"}}))
	gate := w.s.Work.Card("s-gate")
	require.NotNil(t, gate, "the sentinel was not admitted")
	a2, a3 := w.s.Work.Card("a2"), w.s.Work.Card("a3")
	require.Greater(t, gate.Score, a2.Score, "the sentinel sorts after its producer a2: gate=%v a2=%v a3=%v", gate.Score, a2.Score, a3.Score)
	require.Less(t, gate.Score, a3.Score, "the sentinel sorts before a3, directly after a2: gate=%v a2=%v a3=%v", gate.Score, a2.Score, a3.Score)
	require.ElementsMatch(t, []string{"a1", "a2"}, WaitsFor(w.s, gate, nil), "the sentinel waits on a1 and a2 only")
	// With no need on its own stream the sentinel still goes to the end.
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s-end"}, Sentinel: true}))
	end := w.s.Work.Card("s-end")
	a4 := w.s.Work.Card("a4")
	require.Greater(t, end.Score, a4.Score, "a sentinel with no producer sorts last: end=%v a4=%v", end.Score, a4.Score)
}

// sentinels says what each sentinel waits behind and what it blocks: the
// unlanded cards ahead of it in its line, the cards behind it, and the
// cards of other streams that need it.
func TestSentinelsSaysWhatEachWaitsBehindAndBlocks(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"a1", "a2", "a3", "a4"}}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s-gate"}, Sentinel: true, Needs: []string{"a2"}}))
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"b1"}, Needs: []string{"s-gate"}}))
	gate := w.s.Work.Card("s-gate")
	require.Equal(t, "a1,a2", strings.Join(SentinelWaitsBehind(w.s, gate), ","), "what the sentinel waits behind")
	require.Equal(t, "a3,a4,b1", strings.Join(SentinelBlocks(w.s, gate), ","), "what the sentinel blocks")
	// An add that would close a cycle through the line is refused with
	// the line edge marked: late (before the sentinel) needs a3 (behind
	// it), and a3 waits on the sentinel by its place in line.
	p := Add(w.s, AddReq{Stream: "s1", IDs: []string{"late"}, Needs: []string{"a3"}, After: "a1"})
	require.Empty(t, p.Units, "a cycle through the line: %+v", p)
	require.Len(t, p.Refused, 1, "a cycle through the line: %+v", p)
	require.Contains(t, p.Refused[0].Why, "the needs would make a cycle: ", "a cycle through the line: %s", p.Refused[0].Why)
	require.Contains(t, p.Refused[0].Why, "-line->", "the line edge is marked: %s", p.Refused[0].Why)
	require.Contains(t, p.Refused[0].Why, "-needs->", "the named edge is marked: %s", p.Refused[0].Why)
}
