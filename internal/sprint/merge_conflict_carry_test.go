package sprint_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// A landing refused twice ONLY for a merge conflict with the advanced base is the base's
// bound, not the brief's (tla/Land.tla, THE BOUND, ConflictBoundCarries): the head is right
// and the base moved under it, so the judgment says so, offers rework --carry first, and the
// card is never held as a brief defect against rework --carry. The gate, checks and paths
// refusals twice keep today's bound: the head itself is wrong, the brief is wrong, and
// rework --carry is refused for the card the brief-defect rule marked.
// Incident of 2026-10-15: three v13-blockers cards were marked brief-defect after two
// conflict refusals and each was re-cut by hand.
func TestAMergeConflictIsNotABriefDefect(t *testing.T) {
	t.Parallel()
	conflict := func(n int) store.Step {
		return store.MergeStep(sprint.MergeReq{Stream: "s1", Batch: 1, Conflict: "s1-1", ConflictKind: "file", ConflictPaths: []string{"internal/x.go"},
			Note: "the head h" + string(rune('0'+n)) + " of s1-1 does not merge: CONFLICT (content): Merge conflict in internal/x.go"})
	}
	gate := func(n int) store.Step {
		return store.MergeStep(sprint.MergeReq{Stream: "s1", Batch: 1, Conflict: "s1-1",
			Note: "the head h" + string(rune('0'+n)) + " of s1-1 fails the tree gate: go vet ./...: exit status 1"})
	}
	// twice drives s1-1 to two refusals the same way and returns the rig with the card in
	// review under the bound's judgment, the rule's tick run
	twice := func(t *testing.T, refuse func(int) store.Step) (*conflictRig, string) {
		t.Helper()
		r := newConflictRig(t)
		r.toMergingAt("s1-1", refusedHeads[0])
		r.must(refuse(1))
		require.Equal(t, sprint.Ready, r.snap().Work.Card("s1-1").Col, "the first refusal reworks it")
		r.toMergingAt("s1-1", refusedHeads[1])
		r.must(refuse(2))
		require.Equal(t, sprint.Review, r.snap().Work.Card("s1-1").Col, "the second refusal is the bound")
		open := r.open(sprint.NBriefWrong)
		require.Len(t, open, 1, "the bound's judgment")
		what := open[0].Note.What
		r.tick() // the brief-defect rule answers the judgment: it marks the card or leaves it
		return r, what
	}
	carry := func(r *conflictRig) sprint.Plan {
		return sprint.Rework(r.snap(), sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}, Carry: true, Fix: "the base moved: merge it and rerun the gates"})
	}

	t.Run("a conflict twice is the base's", func(t *testing.T) {
		t.Parallel()
		r, what := twice(t, conflict)
		assert.NotContains(t, what, "the brief is wrong", "the base moved; the brief is not the cause")
		assert.Contains(t, what, "s1-1: the landing refused its head twice only for a merge conflict with the advanced base (attempts 1 and 2): the brief is not the cause")
		carryAt, briefAt, dropAt := strings.Index(what, "nova-sprint rework s1-1 --carry --fix <text>"), strings.Index(what, "nova-sprint brief s1-1 --brief-file <path>"), strings.Index(what, "nova-sprint drop s1-1 --reason '<why>'")
		require.GreaterOrEqual(t, carryAt, 0, "the remedy offers rework --carry: %s", what)
		assert.Less(t, carryAt, briefAt, "rework --carry first, then brief")
		assert.Less(t, briefAt, dropAt, "then drop")
		for _, o := range r.open(sprint.NBriefWrong) {
			assert.NotContains(t, o.Note.What, "the brief is wrong", "the judgment never says the brief is wrong")
		}
		// rework --carry is taken for it, whether or not a rule stamped the card
		p := carry(r)
		assert.Empty(t, p.Refused, "rework --carry is taken: %+v", p.Refused)
		assert.Len(t, p.Units, 1, "and moves the card")
	})

	t.Run("a gate twice is the brief's", func(t *testing.T) {
		t.Parallel()
		r, what := twice(t, gate)
		assert.Contains(t, what, "has failed the same way twice (attempts 1 and 2", "today's bound")
		assert.Contains(t, what, "the brief is wrong, not the worker", "today's wording")
		assert.NotContains(t, what, "only for a merge conflict")
		assert.Less(t, strings.Index(what, "nova-sprint brief s1-1 --brief-file <path>"), strings.Index(what, "nova-sprint drop s1-1"), "brief, drop")
		assert.NotContains(t, what, "nova-sprint rework s1-1 --carry", "no carry offered for a gate")
		p := carry(r)
		require.Len(t, p.Refused, 1, "rework --carry is refused: %+v", p)
		assert.Contains(t, p.Refused[0].Why, "brief defect")
	})
}
