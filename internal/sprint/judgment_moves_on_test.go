package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A judgment whose card has moved past the state it was raised for closes itself
// at the next tick, answered by the card's move: a brief defect whose card is
// re-briefed, and any judgment whose card's attempt has advanced past the one the
// judgment was raised at (docs/SPEC-SPRINT.md section 8, "A judgment retires with
// its card": the 2026-10-12 j127, a brief defect still open after its card was
// re-briefed to attempt 5). A judgment whose card is still at the state it was
// raised for stays open, and so does one the check raises for the card's own state.
func TestJudgmentClosesWhenItsCardMovesOn(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend amy"), friendBrief("friend amy"))
	dealStarted(w, FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"})
	row := FriendRow("amy")
	// an ok push gives the card a head, then the same card's attempt ends on a
	// brief defect: the judgment is raised at attempt 1
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: row, Gens: gensOf(w.s, "s1-1.w1"), Head: "abc"}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-2.w1"}}, As: row, Gens: gensOf(w.s, "s1-2.w1"), Failed: true,
		Report: "friend amy HOLD: the base lacks a PATHS file: internal/seatcheck/seatcheck.go is on sprint/y only"}))

	require.Len(t, w.openOn("s1-2"), 1)
	defect := w.openOn("s1-2")[0]
	require.Equal(t, NBriefDefect, defect.Note.Type)
	require.Equal(t, 1, defect.Note.Attempt, "the judgment records the attempt it was raised at")
	// ack is refused while the card is still the one the judgment was raised for:
	// the brief defect asks for a re-cut, a decision
	p := w.do(Ack(w.s, AckReq{Notes: []string{defect.Note.ID}, Reason: "seen"}))
	require.Len(t, p.Refused, 1, "ack of a brief defect: %+v", p)
	require.Contains(t, p.Refused[0].Why, "ack does not answer")

	// the coordinator re-briefs the card in place: the same card, attempt 1, its
	// next attempt; the brief defect is no longer what the card is
	next := "c: a corrected card\nREPO: mas-bandwidth/nova-tools\nWHO: friend amy\n\nThe corrected task."
	w.must(Brief(w.s, BriefReq{ID: "s1-2", Brief: next, Who: "coordinator", Reason: "the brief was wrong"}))
	require.Equal(t, Ready, w.state("s1-2"), "the re-briefed card waits for its next attempt")
	require.Equal(t, defect.Note.ID, w.openOn("s1-2")[0].Note.ID, "the judgment is still open before the tick")

	// the next tick closes it, answered by the card's move
	w.tick(time.Minute)
	w.part(TickCheck, TickReq{})
	require.Empty(t, w.openOn("s1-2"), "a judgment outlived by its card stayed open: %+v", w.s.Open)
	var d Note
	for _, n := range w.notes {
		if n.Kind == Decided && n.Answers == defect.Note.ID {
			d = n
		}
	}
	require.NotEmpty(t, d.ID, "no decided note answers %s: %+v", defect.Note.ID, w.notes)
	require.Equal(t, MachineActor, d.Who, "the card's move answered it: %+v", d)
	require.Contains(t, d.What, "answered by the card's move", "the retirement's reason: %+v", d)
	require.Contains(t, d.What, "s1-2", "the retirement names the card: %+v", d)

	// the inbox never shows it
	for _, g := range Inbox(InboxReq{Now: w.s.Now, Open: w.s.Open}) {
		assert.NotContains(t, g.Notes, defect.Note.ID, "the inbox shows a judgment its card moved past: %+v", g)
	}

	// a second tick finds nothing more to close
	p2, _ := TickCheck(w.s, TickReq{})
	require.Empty(t, p2.Closes, "closed twice: %+v", p2.Closes)
}

// A judgment naming no attempt, and one whose card is still at the attempt it was
// raised for, stay open: the card's move, not the tick alone, is what closes it.
func TestAJudgmentStaysWhenItsCardHasNotMovedOn(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	card := w.s.Work.Card("s1-1")
	card.Fields["attempt"] = "2"
	card.Rev++
	same := judgment(NWorkFailed, "s1", w.s.Now, 0, "s1-1")
	same.Attempt = 2
	none := judgment(NReadBroken, "s1", w.s.Now, 0, "s1-1")
	w.note(same, none)
	w.tick(time.Minute)
	w.part(TickCheck, TickReq{})
	open := map[string]bool{}
	for _, o := range w.s.Open {
		if o.Subject() == "s1-1" {
			open[o.Note.Type] = true
		}
	}
	require.True(t, open[NWorkFailed], "a judgment whose card is still at its attempt closed: %+v", w.s.Open)
	require.True(t, open[NReadBroken], "a judgment naming no attempt closed: %+v", w.s.Open)
}

// An outlived judgment holds no card that needs its decision, so the coordinator
// may ack it with a reason before the tick that would retire it; the ack is
// recorded as the card's move.
func TestAMovedOnJudgmentCanBeAcked(t *testing.T) {
	t.Parallel()
	w, defect := reBriefedDefect(t)
	p := w.do(Ack(w.s, AckReq{Notes: []string{defect.Note.ID}, Reason: "the re-brief answered it"}))
	require.Empty(t, p.Refused, "ack of a judgment its card moved past: %+v", p)
	require.Len(t, p.Units, 1, "the ack of %s: %+v", defect.Note.ID, p)
	require.Empty(t, w.openOn("s1-2"), "the acked judgment stayed open: %+v", w.s.Open)
	var d Note
	for _, n := range w.notes {
		if n.Kind == Decided && n.Answers == defect.Note.ID {
			d = n
		}
	}
	require.NotEmpty(t, d.ID)
	require.Contains(t, d.What, "answered by the card's move", "the ack's answer: %+v", d)
	require.Contains(t, d.What, "s1-2", "the answer names the card: %+v", d)
}

// reBriefedDefect is a world whose card s1-2 ended on a brief defect and was
// re-briefed in place, the judgment still open: the state j127 was in.
func reBriefedDefect(t *testing.T) (*world, Open) {
	t.Helper()
	w := friendWorld(t, friendBrief("friend amy"), friendBrief("friend amy"))
	dealStarted(w, FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"})
	row := FriendRow("amy")
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: row, Gens: gensOf(w.s, "s1-1.w1"), Head: "abc"}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-2.w1"}}, As: row, Gens: gensOf(w.s, "s1-2.w1"), Failed: true,
		Report: "friend amy HOLD: the base lacks a PATHS file: internal/seatcheck/seatcheck.go is on sprint/y only"}))
	require.Len(t, w.openOn("s1-2"), 1)
	defect := w.openOn("s1-2")[0]
	next := "c: a corrected card\nREPO: mas-bandwidth/nova-tools\nWHO: friend amy\n\nThe corrected task."
	w.must(Brief(w.s, BriefReq{ID: "s1-2", Brief: next, Who: "coordinator", Reason: "the brief was wrong"}))
	require.Equal(t, Ready, w.state("s1-2"))
	require.Len(t, w.openOn("s1-2"), 1, "the judgment is still open after the re-brief")
	return w, defect
}
