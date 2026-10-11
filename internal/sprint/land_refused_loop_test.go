package sprint_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// The loop of 2026-10-11, epoch 16 (land-ns56-backup-live-beats and ~30 more across v126-land,
// v126-fix, v128 and v129): the seat accepted a card whose reads passed; the lander refused its
// head on a conflict in FIXES.md and docs/fixes.sexp; the merge step returned it to review at
// its brief's bound and raised, beside the bound's judgment, a fresh "ready to accept" on the
// same head; the seat accepted again, and the lander refused the same head again, for ever. And
// rework was refused, the card marked a brief defect, though a conflict in the files every card
// writes says nothing about the brief.

// refusedHeads are two attempts' heads, as the lander pins them.
var refusedHeads = [...]string{strings.Repeat("a1", 20), strings.Repeat("b2", 20), strings.Repeat("c3", 20)}

// ledgerWhy is the lander's refusal of a head that conflicts only in the fixes ledger, in its
// own words as measured.
func ledgerWhy(head string) string {
	return "the head " + head + " of s1-1 does not merge: git merge: exit status 1: Auto-merging FIXES.md | CONFLICT (content): Merge conflict in FIXES.md | " +
		"Auto-merging docs/fixes.sexp | CONFLICT (content): Merge conflict in docs/fixes.sexp | Automatic merge failed; fix conflicts and then commit the result."
}

// openOn is the open judgments of the type on the primary.
func (r *conflictRig) openOn(typ, id string) []sprint.Open {
	r.t.Helper()
	var out []sprint.Open
	for _, o := range r.open(typ) {
		if o.Subject() == id {
			out = append(out, o)
		}
	}
	return out
}

// A head the landing refused is never offered to accept again: at the brief's bound the card
// goes back to review with the bound's judgment alone (brief and drop), and no later step
// raises "ready to accept" on that head, which an accept would queue for the lander to refuse
// again. Red before the fix: the merge step raised ready to accept beside the bound.
func TestAConflictReturnNeverOffersTheRefusedHeadToAccept(t *testing.T) {
	t.Parallel()
	r := newConflictRig(t)
	why := func(head string) string {
		return "the head " + head + " of s1-1 does not merge: CONFLICT (content): Merge conflict in internal/x.go"
	}
	r.toMergingAt("s1-1", refusedHeads[0])
	r.must(store.MergeStep(sprint.MergeReq{Stream: "s1", Batch: 1, Conflict: "s1-1", Note: why(refusedHeads[0]), ConflictKind: "file", ConflictPaths: []string{"internal/x.go"}}))
	require.Equal(t, sprint.Ready, r.snap().Work.Card("s1-1").Col, "the first refusal reworks it")
	r.toMergingAt("s1-1", refusedHeads[1])
	r.must(store.MergeStep(sprint.MergeReq{Stream: "s1", Batch: 1, Conflict: "s1-1", Note: why(refusedHeads[1]), ConflictKind: "file", ConflictPaths: []string{"internal/x.go"}}))

	pr := r.snap().Work.Card("s1-1")
	require.Equal(t, sprint.Review, pr.Col, "at its bound: back in review")
	assert.Equal(t, refusedHeads[1], pr.F(sprint.FieldLandRefusedHead), "the refused head is kept")
	assert.True(t, sprint.LandRefusedAtHead(pr))
	require.Len(t, r.openOn(sprint.NBriefWrong, "s1-1"), 1, "the bound's judgment")
	assert.Empty(t, r.openOn(sprint.NReadyToAccept, "s1-1"), "the refused head is not offered to accept")

	// the machine's later steps (the tick, its rules and its acks) offer it no accept either
	for range 3 {
		r.tick()
	}
	assert.Empty(t, r.openOn(sprint.NReadyToAccept, "s1-1"), "no step offers the refused head to accept")
	r.clean("refused head in review")
}

// A conflict only in the shared ledgers (here FIXES.md and docs/fixes.sexp, as the lander of
// v1.2.x reported them: kind file) is the tip moving under the card, never the brief: the card is
// reworked at the tip every time, never stopped at the brief's bound, never marked a brief
// defect, and its finding seeds no same-refusal bound for a later file conflict. Red before the
// fix: the second refusal raised "the brief is wrong, not the worker" and the rule marked it.
func TestALedgerOnlyConflictIsNeverABriefDefect(t *testing.T) {
	t.Parallel()
	r := newConflictRig(t)
	ledgers := []string{"FIXES.md", "docs/fixes.sexp"}
	for i, head := range refusedHeads[:2] {
		r.toMergingAt("s1-1", head)
		r.must(store.MergeStep(sprint.MergeReq{Stream: "s1", Batch: 1, Conflict: "s1-1", Note: ledgerWhy(head), ConflictKind: "file", ConflictPaths: ledgers}))
		pr := r.snap().Work.Card("s1-1")
		require.Equal(t, sprint.Ready, pr.Col, "refusal %d: reworked at the tip", i+1)
		assert.Contains(t, pr.F("finding"), "a conflict in the shared ledgers only (FIXES.md, docs/fixes.sexp)")
		assert.Empty(t, r.openOn(sprint.NBriefWrong, "s1-1"), "refusal %d: no brief bound", i+1)
	}
	// a file conflict after them is a first refusal of its way: reworked, not bound
	r.toMergingAt("s1-1", refusedHeads[2])
	why := "the head " + refusedHeads[2] + " of s1-1 does not merge: CONFLICT (content): Merge conflict in internal/x.go"
	r.must(store.MergeStep(sprint.MergeReq{Stream: "s1", Batch: 1, Conflict: "s1-1", Note: why, ConflictKind: "file", ConflictPaths: []string{"internal/x.go"}}))
	pr := r.snap().Work.Card("s1-1")
	assert.Equal(t, sprint.Ready, pr.Col, "a file conflict after ledger conflicts is no repeat")
	assert.Empty(t, r.openOn(sprint.NBriefWrong, "s1-1"))

	// the rules never mark it a brief defect
	r.tick()
	pr = r.snap().Work.Card("s1-1")
	assert.Empty(t, pr.F(sprint.FieldBriefDefect), "never a brief defect")
	assert.Empty(t, r.openOn(sprint.NBriefWrong, "s1-1"))
	r.clean("ledger conflicts")
}

// LedgerConflict is the shared ledgers only: the roadmap's data and pages, the class ledgers,
// the AGENTS maps, the keyed TLA+ tables and the tables lock; any other path, or none, is not.
func TestLedgerConflictIsTheSharedLedgersOnly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		paths []string
		want  bool
	}{
		{[]string{"FIXES.md", "docs/fixes.sexp"}, true},
		{[]string{"ROADMAP.md", "docs/roadmap.sexp", "FIXES.md", "docs/fixes.sexp"}, true},
		{[]string{"internal/ci/testdata/deleted-tests.txt", "AGENTS.md", "cmd/AGENTS.md"}, true},
		{[]string{"tla/CASES.tsv", "tla/RUNS.tsv", "internal/sprint/TABLES.lock"}, true},
		{[]string{"FIXES.md", "internal/x.go"}, false},
		{[]string{"docs/FIXES.md"}, false},
		{nil, false},
	} {
		assert.Equal(t, tc.want, sprint.LedgerConflict(tc.paths), "%v", tc.paths)
	}
}
