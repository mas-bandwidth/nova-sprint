package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClosureEjectsTransitiveDependentsAndLeavesTheRest(t *testing.T) {
	t.Parallel()
	order := []string{"a", "b", "c"}
	needs := map[string][]string{"b": {"a"}, "c": nil}
	got := Closure(order, needs, map[string]bool{"a": true})
	assert.Equal(t, map[string]bool{"a": true, "b": true}, got)
	got = Closure(order, needs, map[string]bool{"b": true})
	assert.Equal(t, map[string]bool{"b": true}, got)
	got = Closure(order, needs, map[string]bool{"c": true})
	assert.Equal(t, map[string]bool{"c": true}, got)
	chain := map[string][]string{"b": {"a"}, "c": {"b"}}
	got = Closure(order, chain, map[string]bool{"a": true})
	assert.Equal(t, map[string]bool{"a": true, "b": true, "c": true}, got)
}

func TestEjectReturnsTheCardAndItsDependentAndWaivesTheNeed(t *testing.T) {
	t.Parallel()
	w := setup(t, 3)
	accepted(w, "s1-1", "s1-2", "s1-3")
	w.s.Work.Card("s1-2").Fields["needs"] = "s1-1"
	w.must(Eject(w.s, EjectReq{
		Stream: "s1", Who: "tester", Landed: []string{"s1-3"},
		Cards: []EjectCard{
			{ID: "s1-1", Reason: "refusing to merge unrelated histories", Class: "unrelated"},
			{ID: "s1-2", Reason: "refusing to merge unrelated histories", Class: "unrelated", Waited: "s1-1"},
		},
	}))
	require.Equal(t, Review, w.state("s1-1"))
	require.Equal(t, Returned, w.s.Merge.Placed("s1-1").Col)
	require.Equal(t, Review, w.state("s1-2"))
	require.Equal(t, "s1-1", w.s.Work.Card("s1-2").F("waived"))
	require.Contains(t, w.s.Work.Card("s1-2").F("return_reason"), "waited on s1-1")
	require.Equal(t, Merging, w.state("s1-3"))
	require.Equal(t, Queued, w.s.Merge.Placed("s1-3").Col)
	var ejected int
	for _, n := range w.notes {
		if n.Type == NEjected {
			ejected++
			assert.Equal(t, "coordinator", n.To)
			assert.Contains(t, n.What, "landed: s1-3")
		}
	}
	require.Equal(t, 2, ejected)
	require.Contains(t, w.s.StreamCtl("s1").F(FieldEjectLog), "s1-1 unrelated 1")
	require.Contains(t, w.s.StreamCtl("s1").F(FieldEjectLog), "s1-2 waited:s1-1 1")
	w.clean("ejected")
}

func TestAThirdEjectForTheSameReasonSaysTheBriefIsWrong(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	card := EjectCard{ID: "s1-1", Reason: "refusing to merge unrelated histories", Class: "unrelated"}
	accepted(w, "s1-1")
	for n := 1; n <= 2; n++ {
		w.must(Eject(w.s, EjectReq{Stream: "s1", Who: "tester", Cards: []EjectCard{card}}))
		require.Equal(t, Review, w.state("s1-1"), "eject %d", n)
		require.Contains(t, w.s.StreamCtl("s1").F(FieldEjectLog), "s1-1 unrelated "+itoa(n))
		w.must(Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		require.Equal(t, Merging, w.state("s1-1"), "back for eject %d", n)
	}
	p := w.must(Eject(w.s, EjectReq{Stream: "s1", Who: "tester", Cards: []EjectCard{card}}))
	require.Equal(t, Merging, w.state("s1-1"), "a third eject is not a return")
	require.Equal(t, Queued, w.s.Merge.Placed("s1-1").Col)
	require.Equal(t, StreamStopped, w.s.StreamCtl("s1").F("state"))
	require.Equal(t, "brief", w.s.StreamCtl("s1").F("cause"))
	require.Contains(t, p.Said, "stream s1 stopped: the brief is wrong")
	var saw bool
	for _, n := range w.notes {
		if n.Type == NBriefWrong && stringsContains(n.What, "the brief is wrong") {
			saw = true
			assert.Equal(t, "coordinator", n.To)
		}
	}
	require.True(t, saw)
	w.clean("brief")
}

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || containsAt(s, sub))
}

func containsAt(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestMergeIdleRaisesOnceWhenTheClockAdvancesFifteenMinutes(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	accepted(w, "s1-1")
	why := "the merge of s1-1 failed in git, not on its changes"
	w.must(MergeIdle(w.s, IdleReq{Stream: "s1", Who: "tester", Why: why, Held: true}))
	require.Empty(t, notesOfType(w, NMergeStuck))
	require.Equal(t, stamp(w.s.Now), w.s.StreamCtl("s1").F(FieldMergeIdleSince))
	w.tick(MergeStuckAfter)
	w.must(MergeIdle(w.s, IdleReq{Stream: "s1", Who: "tester", Why: why, Held: true}))
	got := notesOfType(w, NMergeStuck)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].What, "merge stuck")
	assert.Contains(t, got[0].What, why)
	assert.Equal(t, "coordinator", got[0].To)
	assert.True(t, got[0].StreamLevel)
	w.tick(time.Hour)
	p := MergeIdle(w.s, IdleReq{Stream: "s1", Who: "tester", Why: why, Held: true})
	require.Empty(t, p.Units, "a second judgment: %+v", p)
	w.must(MergeIdle(w.s, IdleReq{Stream: "s1", Who: "tester", Landed: true, Held: true}))
	require.Empty(t, w.s.StreamCtl("s1").F(FieldMergeIdleSince))
	w.clean("idle")
}

func notesOfType(w *world, typ string) []Note {
	var out []Note
	for _, n := range w.notes {
		if n.Type == typ {
			out = append(out, n)
		}
	}
	return out
}
