package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lawfulDeal is the tick's deal part with these friends, held to the lifecycle as the store
// holds every plan (Applied, Lawful) before it is applied: a unit the lifecycle refuses fails.
func lawfulDeal(w *world, seats ...FriendSeat) Plan {
	w.t.Helper()
	p, _ := TickDeal(w.s, TickReq{Friends: seats})
	q := p
	q.on(w.s)
	require.Empty(w.t, Lawful(q).Refused, "every move of the deal is a row of the lifecycle")
	return w.must(p)
}

// A worker that stops because its card's needs have not landed is never a brief defect and
// never the worker's failure (docs/SPEC-SPRINT.md, needs and the deal): the card was dealt
// before its needs landed, a coordination failure. The finish withdraws the work card
// untouched (no fail mark, no ok% hit, no redeal or attempt spent) and the primary waits for
// its needs again, or goes back to ready when they have landed since; no brief-defect,
// failed-work or invariant judgment is written.
func TestAStopOnUnlandedNeedsIsNotTheWorkers(t *testing.T) {
	t.Parallel()
	// the HOLD a worker writes when the card's DEPENDS-ON need is not on its base: its words
	// would read as a brief defect (the base lacks) were the needs not the cause
	const hold = "HOLD: DEPENDS-ON n1 has not landed; the base lacks lib/msg1.dart, which n1 adds"
	okPct := func(s *Snapshot, row string) int {
		ok, failed := s.Fleet.Count(row, DoneOK), s.Fleet.Count(row, DoneFailed)
		if ok+failed == 0 {
			return -1
		}
		return 100 * ok / (ok + failed)
	}
	notTheWorkers := func(t *testing.T, w *world, row, wcID, prID string) {
		t.Helper()
		assert.Empty(t, w.notesOf(NBriefDefect), "no brief-defect judgment")
		assert.Empty(t, w.notesOf(NWorkFailed), "no failed-work judgment")
		assert.Empty(t, w.notesOf(NInvariant), "no invariant judgment")
		wc := w.s.Fleet.Card(wcID)
		require.NotNil(t, wc)
		assert.False(t, wc.Placed(), "the work card goes back to the pool untouched, never done")
		assert.Equal(t, "1", wc.F("attempt"), "its attempt is unchanged")
		assert.Empty(t, wc.F(FieldTakeEnded), "no redeal is spent")
		assert.Empty(t, wc.F(FieldBriefDefect))
		assert.Equal(t, 0, w.s.Fleet.Count(row, DoneFailed), "the worker's row is not marked failed")
		assert.Equal(t, 0, w.s.Fleet.Count(row, DoneDefect))
		assert.Equal(t, -1, okPct(w.s, row), "no ok%% hit")
		pr := w.s.Primary(prID)
		assert.Equal(t, 0, pr.Int("failed"), "no failure counts on the primary")
		assert.Empty(t, pr.F(FieldBriefDefect))
		assert.Empty(t, pr.F("result"))
		ns := w.notesOf(NNeedsUnlanded)
		require.Len(t, ns, 1, "the coordination failure is noted once")
		assert.Equal(t, Happened, ns[0].Kind, "nothing for the seat to answer")
		assert.Contains(t, ns[0].What, "coordination failure")
		assert.Contains(t, ns[0].What, "n1")
	}

	t.Run("a friend, the needs still not landed", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, friendBrief("friend amy"))
		amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
		dealStarted(w, amy)
		row := FriendRow("amy")
		require.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "the fixture: she took it")
		// the card was dealt and taken before its needs were checked (the live case of
		// 2026-10-11): its named needs have not landed
		setNeeds(w.s, "s1-1", "n1", "n2")
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: row, Gens: gensOf(w.s, "s1-1.w1"), Failed: true, Report: "friend amy " + hold}))
		notTheWorkers(t, w, row, "s1-1.w1", "s1-1")
		assert.Equal(t, Ready, w.s.StateOf("s1-1"), "working -> ready, the lifecycle's move")
		// the next tick deals it nowhere and moves it to waiting for its needs
		lawfulDeal(w, amy)
		assert.Equal(t, Waiting, w.s.StateOf("s1-1"), "the primary waits for its needs again")
		assert.Nil(t, w.s.Fleet.Placed("s1-1.w1"), "dealt to no one")
		w.clean("a stop on unlanded needs")
	})

	t.Run("a member, the needs still not landed", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
		wc := w.s.Fleet.Card("s1-1.w1")
		require.NotNil(t, wc)
		w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1"), Who: wc.Row}))
		setNeeds(w.s, "s1-1", "n1")
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: wc.Row, Gens: gensOf(w.s, "s1-1.w1"), Failed: true, Report: hold}))
		notTheWorkers(t, w, wc.Row, "s1-1.w1", "s1-1")
		assert.Equal(t, Ready, w.s.StateOf("s1-1"))
		lawfulDeal(w)
		assert.Equal(t, Waiting, w.s.StateOf("s1-1"))
		assert.Nil(t, w.s.Fleet.Placed("s1-1.w1"), "dealt to no member")
		w.clean("a member's stop on unlanded needs")
	})

	t.Run("the needs landed after the worker stopped", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, friendBrief("friend amy"), fleetBrief("flash"))
		amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
		dealStarted(w, amy)
		row := FriendRow("amy")
		require.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "the fixture: she took it")
		// the need is s1-2 under the name n1 in the report: it has landed by the finish, and
		// the worker's report names it as not landed when she stopped
		setNeeds(w.s, "s1-1", "s1-2")
		w.s.Work.Card("s1-2").Col = Landed
		report := strings.ReplaceAll("friend amy "+hold, "n1", "s1-2")
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: row, Gens: gensOf(w.s, "s1-1.w1"), Failed: true, Report: report}))
		assert.Empty(t, w.notesOf(NBriefDefect), "no brief-defect judgment")
		assert.Empty(t, w.notesOf(NWorkFailed), "no failed-work judgment")
		assert.False(t, w.s.Fleet.Card("s1-1.w1").Placed(), "back to the pool, never done")
		assert.Equal(t, 0, w.s.Fleet.Count(row, DoneFailed), "the worker's row is not marked failed")
		assert.Equal(t, 0, w.s.Fleet.Count(row, DoneDefect))
		assert.Equal(t, Ready, w.s.StateOf("s1-1"), "its needs landed: back to ready for the deal")
		require.Len(t, w.notesOf(NNeedsUnlanded), 1)
		assert.Contains(t, w.notesOf(NNeedsUnlanded)[0].What, "s1-2")
	})

	t.Run("a failure that names no unlanded need is still the worker's", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, friendBrief("friend amy"))
		dealStarted(w, FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"})
		row := FriendRow("amy")
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: row, Gens: gensOf(w.s, "s1-1.w1"), Failed: true, Report: "friend amy FAIL: the test TestX fails"}))
		assert.Empty(t, w.notesOf(NNeedsUnlanded))
		assert.Len(t, w.notesOf(NWorkFailed), 1)
	})
}

// (5) When the needs land, the card is dealt on the next tick as any card: a card withdrawn
// for its needs waits, and once they land the tick's resolve readies it and the deal deals it.
func TestACardWithdrawnForItsNeedsIsDealtWhenTheyLand(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, fleetBrief("flash"), fleetBrief("flash"))
	amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
	w.s.Friends = []FriendSeat{amy}
	// s1-2 is in review; s1-1 is dealt first, then names s1-2 as its need
	w.s.Work.Card("s1-2").Col = Review
	dealWith(w, amy)
	require.NotNil(t, w.s.Fleet.Placed("s1-1.w1"), "the fixture: s1-1 is dealt")
	setNeeds(w.s, "s1-1", "s1-2")
	lawfulDeal(w, amy)
	require.Nil(t, w.s.Fleet.Placed("s1-1.w1"), "withdrawn for its need")
	lawfulDeal(w, amy)
	require.Equal(t, Waiting, w.s.StateOf("s1-1"), "waiting for its need")

	// the need lands: the next tick readies and deals it
	w.s.Work.Card("s1-2").Col = Landed
	p, _ := TickResolve(w.s, TickReq{Friends: []FriendSeat{amy}})
	w.must(p)
	lawfulDeal(w, amy)
	dealt := false
	for _, row := range w.s.Fleet.Rows() {
		for _, col := range []State{Ready, Working} {
			for _, c := range w.s.Fleet.Cell(row, col) {
				if c.F("primary") == "s1-1" {
					dealt = true
				}
			}
		}
	}
	require.True(t, dealt, "s1-1 is dealt once its need landed: %s", w.s.StateOf("s1-1"))
}
