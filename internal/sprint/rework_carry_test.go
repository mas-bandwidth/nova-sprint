package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rework --carry (docs/SPEC-SPRINT.md, rework) answers a bound judgment and a brief-defect
// judgment whose named cause is coordination in one step: each card keeps its brief, records
// its latest pushed head as the brief's CARRY: line (the form --widen writes), resets its
// bound and opens the next attempt staged from that head. A card with no pushed head refuses
// the whole step, by name, and nothing is written.

const (
	carryHead1 = "1111111111111111111111111111111111111111"
	carryHead2 = "2222222222222222222222222222222222222222"
)

// pushedFirstAttempt deals, takes and finishes a card's first attempt at a full pushed head,
// leaving the primary in review.
func pushedFirstAttempt(w *world, id, head string) {
	w.t.Helper()
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{id}}}))
	wc := w.s.Fleet.Card(WorkCardID(id, 1))
	require.NotNil(w.t, wc, "%s's first attempt", id)
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Head: head, Report: "done"}))
	require.Equal(w.t, Review, w.s.StateOf(id), "%s finishes in review", id)
}

// nextPacket is the packet of the card's next attempt (attempt want), read the way a take
// reads it: the attempt before's work cards are its base.
func nextPacket(t *testing.T, w *world, id string, want int) Packet {
	t.Helper()
	pr := w.s.Work.Card(id)
	require.Equal(t, want, pr.Int("attempt"), "%s's next attempt", id)
	wc := w.s.Fleet.Card(WorkCardID(id, want))
	require.NotNil(t, wc, "%s's attempt %d is dealt", id, want)
	var earlier []*Card
	for k := 1; k < want; k++ {
		if c := w.s.Fleet.Card(WorkCardID(id, k)); c != nil {
			earlier = append(earlier, c)
		}
	}
	return PacketOf("sprint", 1, wc, pr, earlier, nil)
}

// withoutCarry is the brief with every CARRY: line taken out: the brief a carry keeps.
func withoutCarry(brief string) string {
	var kept []string
	for _, l := range strings.Split(brief, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "CARRY:") {
			continue
		}
		kept = append(kept, l)
	}
	return strings.Join(kept, "\n")
}

func TestABoundGroupIsReworkedFromEachCardsLastHead(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	ids := []string{"s1-1", "s1-2"}
	heads := map[string]string{"s1-1": carryHead1, "s1-2": carryHead2}
	before := map[string]string{}
	for _, id := range ids {
		pushedFirstAttempt(w, id, heads[id])
		pr := w.s.Work.Card(id)
		pr.Fields["attempt"] = itoa(AttemptsDefault) // at the cap's attempts on one brief
		_, at := AtBriefBound(pr, "", w.s.AttemptsCap(pr.Row))
		require.True(t, at, "%s is at its bound", id)
		before[id] = pr.F("brief")
	}

	w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: ids}, Carry: true, Fix: "your needs have landed"}))

	for _, id := range ids {
		pr := w.s.Work.Card(id)
		// the bound is reset: the brief's count starts at this attempt
		assert.Equal(t, itoa(AttemptsDefault), pr.F(FieldBriefAttempt), "%s: bound reset", id)
		_, at := AtBriefBound(pr, "", w.s.AttemptsCap(pr.Row))
		assert.False(t, at, "%s: not at its bound after the carry", id)
		// the brief is unchanged apart from the carry we recorded
		carry := "CARRY: " + id + " attempt 1 head=" + heads[id]
		assert.Contains(t, pr.F("brief"), carry, "%s: its own head carried", id)
		assert.Equal(t, before[id], withoutCarry(pr.F("brief")), "%s: the brief is otherwise kept", id)
		// the next attempt stages from its own head
		p := nextPacket(t, w, id, AttemptsDefault+1)
		assert.Equal(t, heads[id], p.BaseHead, "%s: its next attempt starts from its own head", id)
		assert.Equal(t, "your needs have landed", p.Fix, "%s: the note is the fix", id)
	}
}

func TestAReworkCarryRefusesACardWithNoPushedHead(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	// s1-1 pushed a head; s1-2 finished at no commit (the head a finish with none writes is
	// the card id, never a full sha)
	pushedFirstAttempt(w, "s1-1", carryHead1)
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-2"}}}))
	wc := w.s.Fleet.Card("s1-2.w1")
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Report: "done"}))
	require.Equal(t, Review, w.s.StateOf("s1-2"))
	require.Less(t, len(w.s.Work.Card("s1-2").F("head")), 40, "s1-2 pushed no full head")

	attemptBefore := w.s.Work.Card("s1-1").F("attempt")
	p := Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1", "s1-2"}}, Carry: true, Fix: "x"})
	require.Empty(t, p.Units, "nothing is written when a card has no pushed head: %+v", p)
	require.Len(t, p.Refused, 1, "refused: %+v", p.Refused)
	assert.Equal(t, "s1-2", p.Refused[0].Key, "refused by name")
	assert.Contains(t, p.Refused[0].Why, "s1-2", "the refusal names the card")
	// the other card was not moved either
	assert.Equal(t, attemptBefore, w.s.Work.Card("s1-1").F("attempt"))
	assert.Nil(t, w.s.Fleet.Card(WorkCardID("s1-1", AttemptsDefault+1)))
}

func TestAReworkCarryAnswersACoordinationBriefDefect(t *testing.T) {
	t.Parallel()
	// a card that ended on a coordination brief defect (dealt before its needs landed) with a
	// landed need and one not landed: refused, naming the need not landed; once it lands, the
	// same verb reworks it with its brief unchanged
	w := setup(t, 1)
	pushedFirstAttempt(w, "s1-1", carryHead1)
	pr := w.s.Work.Card("s1-1")
	pr.Fields["needs"] = "need-landed,need-open"
	pr.Fields[FieldBriefDefect] = BriefDefectCoordination
	w.s.Work.Put(&Card{ID: "need-landed", Row: "s1", Col: Landed, Fields: map[string]string{"attempt": "0"}})
	w.s.Work.Put(&Card{ID: "need-open", Row: "s1", Col: Ready, Fields: map[string]string{"attempt": "0"}})
	before := pr.F("brief")

	p := Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Carry: true, Fix: "your needs have landed"})
	require.Empty(t, p.Units, "a need not landed refuses the step: %+v", p)
	require.Len(t, p.Refused, 1, "refused: %+v", p.Refused)
	assert.Contains(t, p.Refused[0].Why, "need-open", "the refusal names the need not landed")

	w.place(w.s.Work, "need-open", "s1", Landed)
	w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Carry: true, Fix: "your needs have landed"}))
	pr = w.s.Work.Card("s1-1")
	assert.Empty(t, pr.F(FieldBriefDefect), "the brief-defect mark is cleared")
	carry := "CARRY: s1-1 attempt 1 head=" + carryHead1
	assert.Equal(t, before, withoutCarry(pr.F("brief")), "the brief is otherwise unchanged")
	assert.Contains(t, pr.F("brief"), carry, "its last pushed head is carried")
}
