package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/member"
)

const (
	carryHeadA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	carryHeadB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// carryCard puts a primary in review at its attempt cap on the brief it was added with, its
// latest attempt's work card a full-sha pushed head, and the bound judgment open on it: the
// state a "a card has reached its bound" judgment names.
func carryCard(w *world, id, head string, note string) {
	w.t.Helper()
	pr := w.s.Work.Card(id)
	pr.Col = Review
	pr.Fields["attempt"] = "4"
	pr.Fields[FieldFindings] = "attempt 1: one way\nattempt 2: another\nattempt 3: a third\nattempt 4: a fourth"
	// a card at its bound has already been reworked, so it carries the previous fix;
	// the carry's note must replace it, not be dropped with it
	pr.Fields["fix"] = "the previous attempt's fix"
	w.s.Fleet.Put(&Card{ID: WorkCardID(id, 4), Row: "m1", Col: DoneOK, Score: pr.Score, Rev: 1, Fields: map[string]string{
		"kind": "work", "primary": id, "stream": pr.Row, "attempt": "4", "gen": "1", "member": "m1", "head": head}})
	if note == NBriefWrong {
		w.note(judgment(NBriefWrong, pr.Row, w.s.Now, 0, id))
	} else if note == NBriefDefect {
		pr.Fields[FieldBriefDefect] = BriefDefectCoordination
		w.note(judgment(NBriefDefect, pr.Row, w.s.Now, 0, id))
	}
}

// TestABoundGroupIsReworkedFromEachCardsLastHead: a group of two bound cards, each with its
// own pushed head, is reworked by one verb: each card's next attempt is staged from its own
// head through the CARRY: line the verb writes, its bound reset, its brief unchanged apart
// from the carry.
func TestABoundGroupIsReworkedFromEachCardsLastHead(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	carryCard(w, "s1-1", carryHeadA, NBriefWrong)
	carryCard(w, "s1-2", carryHeadB, NBriefWrong)

	before := map[string]string{"s1-1": w.s.Work.Card("s1-1").F("brief"), "s1-2": w.s.Work.Card("s1-2").F("brief")}
	p := Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1", "s1-2"}}, Carry: true, Fix: "your needs have landed; finish it"})
	require.Empty(t, p.Refused, "the group is reworked: %+v", p)
	w.must(p)

	heads := map[string]string{"s1-1": carryHeadA, "s1-2": carryHeadB}
	for id, head := range heads {
		pr := w.s.Work.Card(id)
		assert.Equal(t, Ready, pr.Col, "%s: its next attempt is opened (review -> ready)", id)
		assert.Equal(t, "4", pr.F(FieldBriefAttempt), "%s: its bound is reset from the attempt it ran", id)
		assert.Equal(t, "your needs have landed; finish it", pr.F("fix"), "%s: the note is the attempt's fix", id)
		carry := "CARRY: " + id + " attempt 4 head=" + head
		assert.Contains(t, pr.F("brief"), carry, "%s: its own head as its carry", id)
		c, ok := member.CarryOf(pr.F("brief"))
		require.True(t, ok, "%s: the carry line is the member's shape", id)
		assert.Equal(t, head, c.Head, "%s: its own head", id)
		assert.Equal(t, 4, c.Attempt, "%s: the attempt it pushed", id)
		staged := member.Carried(member.Packet{Kind: "work", Brief: pr.F("brief")})
		assert.Equal(t, head, staged.BaseHead, "%s: the member stages its next attempt from that head", id)
		// the brief is the one it had, plus the carry line
		lines := strings.Split(pr.F("brief"), "\n")
		assert.Contains(t, lines, carry)
		for _, l := range strings.Split(before[id], "\n") {
			assert.Contains(t, lines, l, "%s: every line of its brief is kept", id)
		}
		assert.Empty(t, w.openOn(id), "%s: the bound judgment is answered", id)
	}
}

// TestACarryRefusesACardWithNoPushedHead: a card none of whose attempts pushed a head cannot
// be carried; it refuses the whole step, by name, and nothing is written for any card of it.
func TestACarryRefusesACardWithNoPushedHead(t *testing.T) {
	t.Parallel()
	w := setup(t, 2)
	carryCard(w, "s1-1", carryHeadA, NBriefWrong)
	carryCard(w, "s1-2", "", NBriefWrong) // no work card pushed a head

	before := w.s.Work.Card("s1-1").F("brief")
	p := Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1", "s1-2"}}, Carry: true, Fix: "the fix"})
	require.Len(t, p.Refused, 1, "the card with no pushed head refuses the step: %+v", p)
	assert.Equal(t, "s1-2", p.Refused[0].Key, "refused by name")
	assert.Contains(t, p.Refused[0].Why, "no pushed head", "%+v", p.Refused[0])
	assert.Empty(t, p.Units, "nothing is written, not even for the card that could be carried")
	assert.Equal(t, before, w.s.Work.Card("s1-1").F("brief"), "the other card's brief is untouched")
	assert.Equal(t, Review, w.s.Work.Card("s1-1").Col, "the other card is untouched")
}

// TestACarryAnswersACoordinationBriefDefectOnceItsNeedsHaveLanded: a brief-defect judgment
// whose cause is coordination is reworked by the verb once the needs it names have landed,
// its brief unchanged apart from the carry; while a need is unlanded the step is refused,
// naming it.
func TestACarryAnswersACoordinationBriefDefectOnceItsNeedsHaveLanded(t *testing.T) {
	t.Parallel()
	// the worker's words are the coordination cause: dealt before its DEPENDS-ON / ORDER
	for _, report := range []string{
		"friend amy HOLD: I was dealt before its DEPENDS-ON prerequisites landed",
		"friend amy HOLD: started before its ORDER prerequisites landed",
		"friend amy HOLD: a brief defect: its needs have not landed",
	} {
		assert.Equal(t, BriefDefectCoordination, BriefDefectOf(report), report)
	}

	// the needs have landed: the verb reworks it, its brief unchanged apart from the carry
	w := setup(t, 2)
	carryCard(w, "s1-2", carryHeadA, NBriefDefect)
	w.s.Work.Card("s1-2").Fields["needs"] = "s1-1"
	w.s.Work.Card("s1-1").Col = Landed
	before := w.s.Work.Card("s1-2").F("brief")
	p := Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-2"}}, Carry: true, Fix: "your needs have landed"})
	require.Empty(t, p.Refused, "%+v", p)
	w.must(p)
	pr := w.s.Work.Card("s1-2")
	assert.Equal(t, Ready, pr.Col, "reworked once its needs have landed")
	assert.Contains(t, pr.F("brief"), "CARRY: s1-2 attempt 4 head="+carryHeadA)
	assert.Empty(t, pr.F(FieldBriefDefect), "the brief-defect mark is cleared")
	assert.Empty(t, w.openOn("s1-2"), "the brief-defect judgment is answered")
	for _, l := range strings.Split(before, "\n") {
		assert.Contains(t, strings.Split(pr.F("brief"), "\n"), l, "its brief is kept apart from the carry")
	}

	// a need still unlanded: refused, naming it, nothing written
	w2 := setup(t, 2)
	carryCard(w2, "s1-2", carryHeadA, NBriefDefect)
	w2.s.Work.Card("s1-2").Fields["needs"] = "s1-1"
	w2.s.Work.Card("s1-1").Col = Waiting
	before2 := w2.s.Work.Card("s1-2").F("brief")
	p2 := Rework(w2.s, ReworkReq{Sel: Sel{IDs: []string{"s1-2"}}, Carry: true, Fix: "the fix"})
	require.Len(t, p2.Refused, 1, "%+v", p2)
	assert.Contains(t, p2.Refused[0].Why, "s1-1", "the unlanded need is named")
	assert.Empty(t, p2.Units, "nothing is written")
	assert.Equal(t, before2, w2.s.Work.Card("s1-2").F("brief"), "the brief is untouched")
	assert.Equal(t, Review, w2.s.Work.Card("s1-2").Col, "the card is untouched")
}

// TestTheBoundJudgmentPrintsTheCarryLineFirst: the bound judgment's printed actions name
// rework --carry ahead of brief and drop, and its remedy says the same.
func TestTheBoundJudgmentPrintsTheCarryLineFirst(t *testing.T) {
	t.Parallel()
	d := Decisions[NBriefWrong]
	require.GreaterOrEqual(t, len(d), 3, "%v", d)
	assert.Equal(t, DecisionReworkCarry, d[0], "the carry line comes first: %v", d)
	assert.Contains(t, d, "brief")
	assert.Contains(t, d, "drop")
	remedy := BriefBound{ID: "s1-1"}.Remedy()
	assert.Contains(t, remedy, "rework s1-1 --carry --fix")
	assert.Less(t, strings.Index(remedy, "rework s1-1 --carry"), strings.Index(remedy, "brief s1-1"), "ahead of brief")
}
