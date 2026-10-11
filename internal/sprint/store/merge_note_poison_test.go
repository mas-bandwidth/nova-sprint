package store

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// The 2026-10-11 freeze, replayed: the lander reported a conflict with a 40 KiB note; the
// rework wrote it uncut into the card's why; the store refused the tick's drain step on that
// one field, and the step being all-or-none, no finish, read, accept or landing in the sprint
// moved for twenty minutes. Now the note is refused at the merge verb, loudly and whole:
// nothing is written, the stream goes on, and the next tick still drains another member's
// finish. This test fails on the tree before the fix (the note was taken and the drain froze).
func TestAnOverBoundMergeNoteIsRefusedAndTheTickStillDrains(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(4)
	h.through("s1-1", "s1-2", "s1-3") // queued to merge
	require.Equal(t, sprint.Merging, h.state("s1-1"))

	// the poison: a conflict on the merging card with a note over the card text bound
	note := "the head of s1-1 fails the lander's checks: " + strings.Repeat("tla/RUNS.tsv:3 has no current run record for the case MCX.cfg (E4); ", 1200)
	require.Greater(t, len(note), MaxCardTextBytes)
	res := h.run(MergeStep(sprint.MergeReq{Stream: "s1", Conflict: "s1-1", Note: note, Who: "lander"}))
	require.Len(t, res.Refused, 1, "the note is refused, whole")
	require.Contains(t, res.Refused[0].Why, "--note is")
	require.Contains(t, res.Refused[0].Why, "over the bound of")
	require.Contains(t, res.Refused[0].Why, "nothing was changed")
	require.Empty(t, res.Moved, "a refused note writes nothing")
	require.Equal(t, sprint.Merging, h.state("s1-1"), "the card stays where it was")
	require.NotEqual(t, sprint.StreamStopped, h.snap().StreamCtl("s1").F("state"), "the stream is not stopped by a refused note")

	// another member finishes another card: the next tick drains it (the sprint is not frozen)
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-4"}}}))
	h.takeAndFinish(false, "s1-4")
	h.machine()
	require.Equal(t, sprint.Review, h.state("s1-4"), "the finish drained: the tick moved it to review")
	h.clean("after a refused merge note")
}

// A merge note at the bound is taken: the bound is a bound, not a guess.
func TestAMergeNoteAtTheBoundIsTaken(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	h.through("s1-1")
	note := strings.Repeat("x", MaxCardTextBytes)
	res := h.run(MergeStep(sprint.MergeReq{Stream: "s1", Conflict: "s1-1", Note: note, Who: "lander"}))
	require.Empty(t, res.Refused, "a note at the bound is taken: %v", res.Refused)
}
