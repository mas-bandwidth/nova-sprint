package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The mechanical proof on the world harness and the pure proof functions
// (docs/SPEC-SPRINT.md section 6, the mechanical proof): a mechanical card whose
// proof passes needs no read and goes to merge; one whose proof refuses is reworked
// with the proof's finding, naming the place it found; a non-mechanical card is
// read as today.

// proofBrief is a mechanical card's brief: the kind it declares and the operands
// its proof reads.
func proofBrief(kind, proof string) string {
	b := "tier: pro\nKIND: " + kind + "\n"
	if proof != "" {
		b += "PROOF: " + proof + "\n"
	}
	return b + "\nThe task.\n"
}

// proofWorld is a world with two members, three readers and one finished primary
// in review on the brief.
func proofWorld(t *testing.T, brief string) *world {
	t.Helper()
	w := newWorld(t, "reader-a", "reader-b", "reader-c")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m2"}))
	w.must(Add(w.s, AddReq{Brief: brief, Stream: "s1", Count: 1}))
	w.clean("setup")
	finished(w, "s1-1", false)
	require.Equal(t, Review, w.s.Work.Card("s1-1").Col)
	return w
}

// withProof installs a fake proof runner for the test and restores the default.
func withProof(t *testing.T, p ProofRunner) {
	t.Helper()
	old := DefaultProof
	DefaultProof = p
	t.Cleanup(func() { DefaultProof = old })
}

// TestAMechanicalCardWithAPassingProofNeedsNoRead pins the passing path: a
// deletion card whose symbol is gone passes its proof, ReadsNeeded drops to zero,
// no reader is asked, and the card goes to merge.
func TestAMechanicalCardWithAPassingProofNeedsNoRead(t *testing.T) {
	w := proofWorld(t, proofBrief(cardhdr.KindDeletion, "A"))
	require.Equal(t, 2, ReadsNeeded(w.s.Work.Card("s1-1")), "before the proof a pro card needs two reads")
	withProof(t, func(s *Snapshot, c *Card) ProofRun { return ProofRun{Passed: true} })

	p, ok := TickProof(w.s, TickReq{Who: "machine"})
	require.True(t, ok, "a passing proof moves the attempt")
	w.must(p)

	pr := w.s.Work.Card("s1-1")
	assert.Equal(t, ProofGreen, pr.F(FieldProof))
	assert.Equal(t, "1", pr.F(FieldProofAttempt))
	assert.Equal(t, 0, ReadsNeeded(pr), "a passing proof needs no read")
	assert.Empty(t, readsAt(w.s, pr, 1), "no reader is asked of a proved mechanical card")
	assert.Equal(t, 0, ReadsWanted(w.s, pr), "a proved card wants no read")

	// the card goes to merge with no read
	acc, _ := TickAccept(w.s, TickReq{Who: "machine"})
	w.must(acc)
	assert.Equal(t, Merging, w.state("s1-1"), "a proved mechanical card goes to merge")
	assert.Equal(t, Queued, w.s.Merge.Placed("s1-1").Col)
	assert.Empty(t, w.s.Readers.Of("s1-1"), "it merges unread")

	// and the ask holds a mechanical attempt whose proof is undecided
	w2 := proofWorld(t, proofBrief(cardhdr.KindDeletion, "A"))
	withProof(t, func(s *Snapshot, c *Card) ProofRun { return ProofRun{Passed: true} })
	held := proofHeldPrimaries(w2.s, TickReq{Who: "machine"})
	require.Len(t, held, 1, "the proof holds a mechanical attempt whose proof has not run")
	assert.Equal(t, "s1-1", held[0].ID)
	w2.must(TickProofPlan(t, w2))
	assert.Empty(t, proofHeldPrimaries(w2.s, TickReq{Who: "machine"}), "a proved attempt is not held")
}

// TickProofPlan is TickProof's plan, the test failing when it moved nothing.
func TickProofPlan(t *testing.T, w *world) Plan {
	t.Helper()
	p, ok := TickProof(w.s, TickReq{Who: "machine"})
	require.True(t, ok, "the proof moves the attempt")
	return p
}

// TestAFailingProofReworksTheAttemptWithItsFinding pins the refusing path: a
// deletion card with a reference left is reworked at once with the proof's finding
// naming the reference as its fix, and the ask asks no reader of the attempt.
func TestAFailingProofReworksTheAttemptWithItsFinding(t *testing.T) {
	w := proofWorld(t, proofBrief(cardhdr.KindDeletion, "A"))
	withProof(t, func(s *Snapshot, c *Card) ProofRun {
		return ProofRun{Failed: []ProofFinding{
			{File: "internal/sprint/a_test.go", Line: 4, What: "the deleted symbol A is still named here"},
		}}
	})

	p, ok := TickProof(w.s, TickReq{Who: "machine"})
	require.True(t, ok, "a refusing proof moves the attempt")
	w.must(p)

	pr := w.s.Work.Card("s1-1")
	require.NotEqual(t, Review, pr.Col, "a refusing proof reworks the attempt out of review")
	assert.Equal(t, 2, pr.Int("attempt"), "the refusing proof sends the next attempt")
	assert.Contains(t, pr.F("fix"), "internal/sprint/a_test.go:4", "the failing line is the fix")
	assert.Contains(t, pr.F("fix"), "machine proof")
	assert.Contains(t, pr.F("finding"), "machine proof")
	assert.Equal(t, "1", pr.F(FieldProofReworks), "the proof rework is counted")
	assert.Equal(t, "1", pr.F("broken_reads"), "a proof rework counts toward the bound as a broken read")
	assert.Equal(t, ProofRed, pr.F(FieldProof))
	assert.Empty(t, readsAt(w.s, pr, 1), "no reader is asked of the attempt the proof refused")
	if wc := w.s.Fleet.Card(WorkCardID("s1-1", 2)); assert.NotNil(t, wc) {
		assert.Contains(t, wc.F("finding"), "machine proof")
	}
}

// TestANonMechanicalCardIsReadAsToday pins the kind rule: a card of a kind no proof
// reads is not proved, and the read requirement is unchanged.
func TestANonMechanicalCardIsReadAsToday(t *testing.T) {
	w := proofWorld(t, "tier: pro\n\nThe task.\n")
	withProof(t, func(s *Snapshot, c *Card) ProofRun {
		return ProofRun{Failed: []ProofFinding{{What: "this proof must not run"}}}
	})
	p, ok := TickProof(w.s, TickReq{Who: "machine"})
	require.False(t, ok, "a non-mechanical card is no proof's")
	require.True(t, p.Empty())
	pr := w.s.Work.Card("s1-1")
	assert.Empty(t, pr.F(FieldProof))
	assert.Equal(t, 2, ReadsNeeded(pr), "a non-mechanical card is read as today")
	assert.Empty(t, proofHeldPrimaries(w.s, TickReq{Who: "machine"}), "a non-mechanical card is asked as today")
}

// TestThePureProofChecks pins WorkProof's three kinds over a view: a deletion
// passes when the symbols are gone and refuses naming every reference left, a
// rename passes when the old name is gone and the new exists, and an assertion
// rewrite changes only assertion lines.
func TestThePureProofChecks(t *testing.T) {
	t.Parallel()
	file := func(src string) ProofView { return ProofView{Files: map[string]string{"a.go": src}} }

	t.Run("a deletion with the symbol gone passes", func(t *testing.T) {
		fs := WorkProof(cardhdr.Proof{Kind: cardhdr.KindDeletion, Old: []string{"A"}},
			file("package a\n\nfunc B() int { return 1 }\n"))
		assert.Empty(t, fs)
	})
	t.Run("a deletion with a reference left refuses naming it", func(t *testing.T) {
		fs := WorkProof(cardhdr.Proof{Kind: cardhdr.KindDeletion, Old: []string{"A"}},
			file("package a\n\nfunc B() int { return A() }\n"))
		require.Len(t, fs, 1)
		assert.Equal(t, "a.go", fs[0].File)
		assert.Equal(t, 3, fs[0].Line)
		assert.Contains(t, fs[0].What, "still named here")
	})
	t.Run("a rename with the old name gone and the new there passes", func(t *testing.T) {
		fs := WorkProof(cardhdr.Proof{Kind: cardhdr.KindRename, Old: []string{"A"}, New: "B"},
			file("package a\n\nfunc B() int { return 1 }\n"))
		assert.Empty(t, fs)
	})
	t.Run("a rename with the old name left refuses", func(t *testing.T) {
		fs := WorkProof(cardhdr.Proof{Kind: cardhdr.KindRename, Old: []string{"A"}, New: "B"},
			file("package a\n\nvar B = 1\n\nfunc A() int { return B }\n"))
		require.Len(t, fs, 1)
		assert.Contains(t, fs[0].What, "old name A")
	})
	t.Run("a rename with the new name missing refuses", func(t *testing.T) {
		fs := WorkProof(cardhdr.Proof{Kind: cardhdr.KindRename, Old: []string{"A"}, New: "B"},
			file("package a\n\nvar C = 1\n"))
		require.Len(t, fs, 1)
		assert.Contains(t, fs[0].What, "new name B")
	})
	t.Run("an assertion rewrite of assertion lines passes", func(t *testing.T) {
		diff := "diff --git a/a_test.go b/a_test.go\n--- a/a_test.go\n+++ b/a_test.go\n@@ -1,2 +1,2 @@\n-\tassert.Equal(t, 1, A())\n+\tassert.Equal(t, 2, A())\n-\trequire.NoError(t, err)\n+\trequire.NoError(t, err2)\n"
		assert.Empty(t, WorkProof(cardhdr.Proof{Kind: cardhdr.KindAssertion}, ProofView{Diff: diff}))
	})
	t.Run("an assertion rewrite of another line refuses", func(t *testing.T) {
		diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-func A() int { return 1 }\n+func A() int { return 2 }\n"
		fs := WorkProof(cardhdr.Proof{Kind: cardhdr.KindAssertion}, ProofView{Diff: diff})
		require.Len(t, fs, 1)
		assert.Equal(t, "a.go", fs[0].File)
		assert.Contains(t, fs[0].What, "no assertion")
	})
}

// TestNewTreeProofGatesOnTheKind pins the tree proof's own reading: a mechanical
// card whose tree cannot be read is a proof that refuses with the reason, a card of
// a non-mechanical kind is no proof's, and a malformed PROOF line refuses naming it.
func TestNewTreeProofGatesOnTheKind(t *testing.T) {
	t.Parallel()
	proof := NewTreeProof(ProofGit{})
	mech := &Card{ID: "c1", Fields: map[string]string{"brief": proofBrief(cardhdr.KindDeletion, "A")}}
	run := proof(nil, mech)
	require.NotEmpty(t, run.Failed, "a mechanical card with no repository the proof can read refuses")
	assert.False(t, run.Passed)

	plain := &Card{ID: "c2", Fields: map[string]string{"brief": "tier: pro\n\nThe task.\n"}}
	assert.Equal(t, ProofRun{}, proof(nil, plain), "a non-mechanical card is no proof's")

	bad := &Card{ID: "c3", Fields: map[string]string{"brief": "KIND: rename\nPROOF: onlyone\n"}}
	badRun := proof(nil, bad)
	require.NotEmpty(t, badRun.Failed)
	assert.Contains(t, badRun.Failed[0].What, "old")
}

// TestReadProofAndReadKind pins the cardhdr vocabulary the proof reads.
func TestReadProofAndReadKind(t *testing.T) {
	t.Parallel()
	kind, why := cardhdr.ReadProof("KIND: deletion\nPROOF: A,B\n")
	require.Empty(t, why)
	assert.Equal(t, cardhdr.KindDeletion, kind.Kind)
	assert.Equal(t, []string{"A", "B"}, kind.Old)

	rename, why := cardhdr.ReadProof("KIND: rename\nPROOF: Old New\n")
	require.Empty(t, why)
	assert.Equal(t, []string{"Old"}, rename.Old)
	assert.Equal(t, "New", rename.New)

	_, why = cardhdr.ReadProof("KIND: deletion\n")
	assert.Contains(t, why, "no PROOF")

	_, why = cardhdr.ReadProof("KIND: rename\nPROOF: onlyone\n")
	assert.Contains(t, why, "old")

	assertion, why := cardhdr.ReadProof("KIND: assertion-rewrite\n")
	require.Empty(t, why, "an assertion rewrite carries no operands and needs no PROOF line")
	assert.Equal(t, cardhdr.KindAssertion, assertion.Kind)
}
