package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A mechanical card that passes its proof goes to merge with no read: the machine
// proof (docs/SPEC-SPRINT.md section 6, the mechanical proof) runs after the work
// lint (TickLint) and the machine gate (TickGate) and, when it passes, makes
// ReadsNeeded zero for that attempt no reader is asked of it, and the card is
// acceptable at once; a failing proof reworks the attempt with the proof's
// finding, naming what blocks it (go/parser over the tree).
func TestAMechanicalCardWithAPassingProofNeedsNoRead(t *testing.T) {
	t.Parallel()

	t.Run("a deletion whose symbol is gone goes to merge with no read", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		// the card is a deletion: a mechanical kind that carries a proof
		deleteBrief := "KIND: delete\n"
		w.s.Work.Card("s1-1").Fields["brief"] = deleteBrief
		finished(w, "s1-1", false)
		// the gate must have passed green before the proof runs (proofable)
		pr := w.s.Work.Card("s1-1")
		pr.Fields[FieldGate] = GateGreen
		pr.Fields[FieldGateAttempt] = "1"

		// a fake proof: the deleted symbol is gone, nothing references it
		passing := func(s *Snapshot, c *Card) ProofRun {
			return ProofRun{
				Kind:  cardhdr.KindDelete,
				Host:  "machine",
				Lines: []string{"deleted: Old; no references to Old remain"},
			}
		}
		p, ok := TickProof(w.s, TickReq{Who: "machine", Proof: passing})
		require.True(t, ok, "a passing proof marks the attempt")
		w.must(p)

		assert.Equal(t, 0, ReadsNeeded(w.s.Work.Card("s1-1")),
			"a passing proof makes ReadsNeeded zero for the attempt")
		assert.Empty(t, w.s.Readers.Of("s1-1"), "no readers are asked of a proven card")
		require.True(t, acceptable(w.s, w.s.Work.Card("s1-1")),
			"a proven card is acceptable with no ok reads")
		assert.Equal(t, ProofPass, w.s.Work.Card("s1-1").F(FieldProof),
			"the proof state is pass")

		// accept: review -> merging, no reads ever asked
		w.must(Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		assert.Equal(t, Merging, w.state("s1-1"), "review -> merging on the proof")
		require.NotNil(t, w.s.Merge.Placed("s1-1"), "the accepted primary is in its stream's merge queue")
		assert.Equal(t, Queued, w.s.Merge.Placed("s1-1").Col, "queued to merge")
		assert.Empty(t, w.s.Readers.Of("s1-1"), "no read cards were placed")
		w.clean("accepted with no read")
	})

	t.Run("a deletion with a reference left is reworked naming it", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		deleteBrief := "KIND: delete\n"
		w.s.Work.Card("s1-1").Fields["brief"] = deleteBrief
		finished(w, "s1-1", false)
		pr := w.s.Work.Card("s1-1")
		pr.Fields[FieldGate] = GateGreen
		pr.Fields[FieldGateAttempt] = "1"

		// a fake proof that fails: the symbol is gone but a reference remains
		failing := func(s *Snapshot, c *Card) ProofRun {
			return ProofRun{
				Kind: cardhdr.KindDelete,
				Failed: []ProofFinding{
					{File: "internal/sprint/a.go", Line: 5, What: "reference to deleted symbol Old remains"},
				},
			}
		}
		p, ok := TickProof(w.s, TickReq{Who: "machine", Proof: failing})
		require.True(t, ok, "a failing proof reworks the attempt")
		w.must(p)

		require.Equal(t, 2, pr.Int("attempt"), "the failing proof sent the next attempt")
		assert.Equal(t, Working, pr.Col, "review -> working on the rework")
		if wc := w.s.Fleet.Card(WorkCardID("s1-1", 2)); assert.NotNil(t, wc) {
			assert.Contains(t, wc.F("finding"), "reference to deleted symbol Old remains",
				"the rework's work card carries the proof's finding")
			assert.Contains(t, wc.F("finding"), "mechanical proof",
				"the finding is named as a mechanical proof finding")
		}
		assert.Equal(t, "1", pr.F(FieldProofReworks), "the proof rework is counted")
		assert.Equal(t, "1", pr.F("broken_reads"), "a proof rework counts toward the bound as a broken read")
		assert.Equal(t, "1", pr.F(FieldProofAttempt), "the proof state is for the attempt it was decided at")
		assert.Empty(t, w.s.Readers.Of("s1-1"), "no reader is asked of the attempt the proof refused")
		w.clean("reworked by the proof")
	})

	t.Run("a non-mechanical card is read as today", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		// proBrief has no KIND: line, so it is not mechanical
		finished(w, "s1-1", false)
		pr := w.s.Work.Card("s1-1")
		pr.Fields[FieldGate] = GateGreen
		pr.Fields[FieldGateAttempt] = "1"

		// a proof runner is configured, but the card is not mechanical
		never := func(s *Snapshot, c *Card) ProofRun {
			t.Fatalf("the proof must not run on a non-mechanical card: %s", c.ID)
			return ProofRun{}
		}
		p, ok := TickProof(w.s, TickReq{Who: "machine", Proof: never})
		assert.False(t, ok, "no mechanical card to prove")
		assert.True(t, p.Empty(), "no mechanical card to prove: %+v", p)

		assert.Equal(t, ReadsNeeded(w.s.Work.Card("s1-1")), 2,
			"a non-mechanical pro card is read as today: two reads")
		assert.Equal(t, "", pr.F(FieldProof), "no proof state was written")
	})

	t.Run("a proof no runner is configured leaves the mechanical card to the gate", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		deleteBrief := "KIND: delete\n"
		w.s.Work.Card("s1-1").Fields["brief"] = deleteBrief
		finished(w, "s1-1", false)
		pr := w.s.Work.Card("s1-1")
		pr.Fields[FieldGate] = GateGreen
		pr.Fields[FieldGateAttempt] = "1"

		// no proof runner: the card is read as today (flash: one read)
		p, ok := TickProof(w.s, TickReq{Who: "machine"})
		assert.False(t, ok, "no proof runner: nothing to do")
		assert.True(t, p.Empty(), "no proof runner: %+v", p)
		assert.Equal(t, "", pr.F(FieldProof), "no proof state was written")
	})
}

// TestAMechanicalCardWithAPassingProofIsHeldFromTheAsk pins the hold: a proof
// that waits (no host answered) keeps no reader from the attempt until the proof
// runs, as the gate's wait does.
func TestAMechanicalCardWithAPassingProofIsHeldFromTheAsk(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	deleteBrief := "KIND: delete\n"
	w.s.Work.Card("s1-1").Fields["brief"] = deleteBrief
	finished(w, "s1-1", false)
	pr := w.s.Work.Card("s1-1")
	pr.Fields[FieldGate] = GateGreen
	pr.Fields[FieldGateAttempt] = "1"

	// a proof that cannot run: the attempt waits, no reader is asked
	waiting := func(s *Snapshot, c *Card) ProofRun {
		return ProofRun{Waiting: "no proof host is configured"}
	}
	p, ok := TickProof(w.s, TickReq{Who: "machine", Proof: waiting})
	require.True(t, ok, "a waiting proof is a move")
	w.must(p)

	assert.Equal(t, ProofWaiting, pr.F(FieldProof), "the proof state is waiting")

	// the ask holds the attempt: no reader is asked while the proof waits
	held := gateHeldPrimaries(w.s, TickReq{Who: "machine", Proof: waiting})
	require.Contains(t, held, w.s.Work.Card("s1-1"),
		"a proof that waits holds the attempt from the ask")
	assert.Empty(t, w.s.Readers.Of("s1-1"), "no reader is asked while the proof waits")
	w.clean("held by the waiting proof")
}
