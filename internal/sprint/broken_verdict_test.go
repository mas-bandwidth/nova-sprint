package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestABrokenVerdictAtTheBriefBoundRecordsAndReturnsToWork pins the card's own
// case: a card at its brief's bound (Cap attempts on one brief) with the
// coordinator's broken verdict at its head and a fleet reader's ok at the same
// head. The broken verdict outweighs the ok, so the card is not accepted and
// goes back to work with the finding (docs/SPEC-SPRINT.md section 8, the
// read-broken rule; tla/SprintRules.tla, Part "reads": a broken verdict at the
// same head outweighs any number of oks, ReworkAtBound).
func TestABrokenVerdictAtTheBriefBoundRecordsAndReturnsToWork(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Work.SetProp(PropAttempts, "2") // the cap: two attempts on one brief
	finished(w, "s1-1", false)
	const finding = "internal/x.go:12 drops the error; return it"
	pr := w.s.Work.Card("s1-1")
	pr.Fields["brief"] = "c: the work (s1) tier: flash\n" // one reader's ok is its read rule
	pr.Fields["attempt"] = "2"                            // at the cap: its brief's bound
	pr.Fields["finding"] = finding
	pr.Fields[FieldFindingAttempt] = "1"
	head := pr.F("head")

	// a fleet reader's ok read at the same head (a friend's read on her row)
	amy := FriendRow("amy")
	if !w.s.Fleet.HasRow(amy) {
		w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), amy))
	}
	w.s.Fleet.Put(&Card{ID: ReadCardID("s1-1", 2, "amy"), Rev: 1, Fields: map[string]string{
		"kind": "read", "primary": "s1-1", "stream": "s1", "reader": "amy",
		"attempt": "2", "head": head, "verdict": "ok"}})

	// the coordinator's broken verdict at the head, a read of the same attempt
	rc := &Card{ID: ReadCardID("s1-1", 2, "reader-a"), Row: "reader-a", Col: Reading, Rev: 1, Fields: map[string]string{
		"kind": "read", "primary": "s1-1", "stream": "s1", "reader": "reader-a",
		"attempt": "2", "head": head, "begun": stamp(w.s.Now)}}
	w.s.Readers.Put(rc)
	w.must(Read(w.s, ReadReq{Usage: "input=1000 output=100", As: "reader-a", Verdict: "broken", Finding: finding, Sel: Sel{IDs: []string{rc.ID}}}))

	pr = w.s.Work.Card("s1-1")
	require.Equal(t, Broken, w.s.Readers.Card(rc.ID).Col, "the broken verdict stands")
	require.Equal(t, NReadBroken, openTypes(w, "s1-1"), "the broken verdict records as a judgment")
	require.False(t, acceptable(w.s, pr), "the broken verdict outweighs the fleet ok")
	require.False(t, tickTakes(w, "s1-1"), "the tick does not accept it")
	p := Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.Empty(t, p.Units, "accept takes no card with a broken read at its head: %+v", p)
	require.Len(t, p.Refused, 1, "accept refuses it: %+v", p)
	require.Contains(t, p.Refused[0].Why, "broken", "accept names the broken read: %+v", p.Refused[0])

	// the card goes back to work with the finding
	w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	require.NotEqual(t, Review, w.state("s1-1"), "back to work")
	require.Equal(t, finding, w.s.Work.Card("s1-1").F("fix"), "the finding is the fix")
	require.Empty(t, w.openOn("s1-1"), "the broken judgment is answered")
	w.clean("broken at the bound")
}
