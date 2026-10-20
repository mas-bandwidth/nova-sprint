package sprint_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// A landing refused ONLY for a merge conflict with the advanced base is never the brief's
// bound (tla/Land.tla, THE BOUND, ConflictNeverBounds; tla/WaitFor.tla,
// ConflictSpendsNoAttempt): the head is right and the base moved under it, so the refusing
// step reworks the card at the tip, keeps the conflicting paths on it, spends no attempt and
// never marks it a brief defect. The gate, checks and paths refusals twice keep the bound:
// the head itself is wrong, the brief is wrong, and rework --carry is refused for the card
// the brief-defect rule marked.
// Incidents: 2026-10-15 three v13-blockers cards were marked brief-defect after two conflict
// refusals; 2026-10-19 a landing was refused twice only for a merge conflict and the card was
// held against a brief that did not have the defect.
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
	// twice drives s1-1 to two non-conflict refusals the same way and returns the rig with
	// the card in review under the bound's judgment, the rule's tick run
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

	t.Run("a conflict twice is never the bound", func(t *testing.T) {
		t.Parallel()
		r := newConflictRig(t)
		for i, head := range refusedHeads[:2] {
			r.toMergingAt("s1-1", head)
			r.must(conflict(i + 1))
			pr := r.snap().Work.Card("s1-1")
			require.Equal(t, sprint.Ready, pr.Col, "refusal %d: reworked at the tip, never the bound", i+1)
			assert.Equal(t, "internal/x.go", pr.F(sprint.FieldConflictPaths), "refusal %d: the conflicting path stays on the card", i+1)
			assert.Equal(t, strconv.Itoa(i+1), pr.F(sprint.FieldConflictRefusals), "refusal %d: the conflict is on the card", i+1)
			assert.Empty(t, r.open(sprint.NBriefWrong), "refusal %d: no bound judgment", i+1)
		}
		r.tick() // the brief-defect rule answers any judgment: there is none to mark
		pr := r.snap().Work.Card("s1-1")
		assert.Empty(t, pr.F(sprint.FieldBriefDefect), "never a brief defect")
		assert.NotEqual(t, sprint.Review, pr.Col, "never at the bound")
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
