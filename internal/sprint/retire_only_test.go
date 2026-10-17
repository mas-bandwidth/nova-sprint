package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tick --retire-only (v1.5.4): a need-blocked judgment whose need blocks no
// card any more (the 2026-10-17 j24647: "an earlier card", a bogus need taken
// off every card) is closed with the machine's answer; one whose need still
// blocks a card stays open and is not rewritten; nothing new is raised and no
// card moves. tla/JudgmentRetire.tla RetireOnly.
func TestRetireOnlyClosesANeedBlockedJudgmentWhoseNeedIsGone(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"blocker"}}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"dep"}, Needs: []string{"blocker"}}))
	w.s.Work.Card("blocker").Col = Review
	w.s.Work.cells = nil
	w.note(judgment(NBound, "s1", w.s.Now, 0, "blocker"))
	raised, _ := TickNeedBlocked(w.s, TickReq{})
	require.NotEmpty(t, raised.Notes, "the blocker's judgment is raised")
	w.note(raised.Notes...)
	gone := judgment(NNeedBlocked, "s1", w.s.Now, 0, "an earlier card")
	gone.What = "an earlier card (absent) blocks 1 cards: dep (s1)"
	w.note(gone)
	var goneID, stillID string
	for _, o := range w.s.Open {
		if o.Note.Type == NNeedBlocked && o.Subject() == "an earlier card" {
			goneID = o.Note.ID
		}
		if o.Note.Type == NNeedBlocked && o.Subject() == "blocker" {
			stillID = o.Note.ID
		}
	}
	require.NotEmpty(t, goneID)
	require.NotEmpty(t, stillID)
	cols := map[string]string{}
	for _, c := range w.s.Work.Cards() {
		cols[c.ID] = c.Col
	}

	p := RetireOnly(w.s, MachineActor)
	require.Len(t, p.Closes, 1, "closes: %+v", p.Closes)
	assert.Equal(t, goneID, p.Closes[0].Note.ID)
	require.Len(t, p.Notes, 1, "notes: %+v", p.Notes)
	assert.Equal(t, Decided, p.Notes[0].Kind)
	assert.Equal(t, goneID, p.Notes[0].Answers)
	assert.Equal(t, "no dependents remain", p.Notes[0].What)
	assert.Equal(t, MachineActor, p.Notes[0].Who)
	assert.Empty(t, p.Units, "a retire moves no card")
	assert.Empty(t, p.Updates, "a retire rewrites no judgment")
	w.must(p)
	for _, c := range w.s.Work.Cards() {
		assert.Equal(t, cols[c.ID], c.Col, "%s moved", c.ID)
	}
	open := false
	for _, o := range w.s.Open {
		open = open || o.Note.ID == stillID
		assert.NotEqual(t, goneID, o.Note.ID, "the gone need's judgment is still open")
	}
	assert.True(t, open, "the standing blocker's judgment was closed")
}

// RetireOnly closes an orphan judgment (its card is on no table of the epoch),
// as the tick's retire does (RetireJudgments, OrphanJudgments).
func TestRetireOnlyRetiresAnOrphanJudgment(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"here"}}))
	w.note(judgment(NBound, "s1", w.s.Now, 0, "no-such-card"))
	p := RetireOnly(w.s, MachineActor)
	require.Len(t, p.Closes, 1, "the orphan judgment is not retired: %+v", p.Closes)
	assert.Equal(t, "no-such-card", p.Closes[0].Subject())
	for _, n := range p.Notes {
		assert.Equal(t, Decided, n.Kind, "a retire raises nothing: %+v", n)
	}
	assert.Empty(t, p.Units, "a retire moves no card")
}
