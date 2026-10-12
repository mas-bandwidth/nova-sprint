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

// dealtAttempt is the attempt of the work card of the primary on a row ready or working, "".
func dealtAttempt(w *world, prID string) string {
	for _, row := range w.s.Fleet.Rows() {
		for _, col := range []State{Ready, Working} {
			for _, c := range w.s.Fleet.Cell(row, col) {
				if c.F("primary") == prID {
					return c.F("attempt")
				}
			}
		}
	}
	return ""
}

// A worker that stops because its card's needs have not landed is never a brief defect and
// never the worker's failure (docs/SPEC-SPRINT.md, needs and the deal): the card was dealt
// before its needs landed, a coordination failure. Decided on the table's facts alone (a named
// need not landed at the finish), never on the report's words. The finish sends the work card
// back to the pool (no fail mark, no ok% hit, no redeal spent; the next deal cuts the next
// attempt) and the primary goes working -> waiting at once, so the end-of-tick check finds no
// rule-11 break; no brief-defect, failed-work or invariant judgment is written. Capped:
// MaxNeedsReturns returns, then the failure is judged as any other.
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
	notTheWorkers := func(t *testing.T, w *world, row, wcID, prID, need string) {
		t.Helper()
		assert.Empty(t, w.notesOf(NBriefDefect), "no brief-defect judgment")
		assert.Empty(t, w.notesOf(NWorkFailed), "no failed-work judgment")
		assert.Empty(t, w.notesOf(NInvariant), "no invariant judgment")
		wc := w.s.Fleet.Card(wcID)
		require.NotNil(t, wc)
		assert.False(t, wc.Placed(), "the work card goes back to the pool, never done")
		assert.Empty(t, wc.F(FieldTakeEnded), "no redeal is spent")
		assert.Empty(t, wc.F(FieldBriefDefect))
		assert.Equal(t, 0, w.s.Fleet.Count(row, DoneFailed), "the worker's row is not marked failed")
		assert.Equal(t, 0, w.s.Fleet.Count(row, DoneDefect))
		assert.Equal(t, -1, okPct(w.s, row), "no ok%% hit")
		pr := w.s.Primary(prID)
		assert.Equal(t, 0, pr.Int("failed"), "no failure counts on the primary")
		assert.Empty(t, pr.F(FieldBriefDefect))
		assert.Empty(t, pr.F("result"))
		assert.Equal(t, 1, pr.Int(FieldNeedsReturns), "one return counted toward the cap")
		assert.Equal(t, Waiting, pr.Col, "working -> waiting at once: the lifecycle's move for needs not landed")
		ns := w.notesOf(NNeedsUnlanded)
		require.Len(t, ns, 1, "the coordination failure is noted once")
		assert.Equal(t, Happened, ns[0].Kind, "nothing for the seat to answer")
		assert.Contains(t, ns[0].What, "coordination failure")
		assert.Contains(t, ns[0].What, need)
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
		notTheWorkers(t, w, row, "s1-1.w1", "s1-1", "n1")
		w.clean("the end-of-tick check after a stop on unlanded needs: no rule-11 break")
		lawfulDeal(w, amy)
		assert.Equal(t, Waiting, w.s.StateOf("s1-1"), "the primary waits for its needs")
		assert.Empty(t, dealtAttempt(w, "s1-1"), "dealt to no one")
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
		notTheWorkers(t, w, wc.Row, "s1-1.w1", "s1-1", "n1")
		w.clean("the end-of-tick check after a member's stop on unlanded needs")
		lawfulDeal(w)
		assert.Empty(t, dealtAttempt(w, "s1-1"), "dealt to no member")
	})

	t.Run("when the need lands the next deal cuts the next attempt", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, friendBrief("friend amy"), fleetBrief("flash"))
		amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
		w.s.Work.Card("s1-2").Col = Review
		dealStarted(w, amy)
		row := FriendRow("amy")
		require.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "the fixture: she took it")
		setNeeds(w.s, "s1-1", "s1-2")
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: row, Gens: gensOf(w.s, "s1-1.w1"), Failed: true,
			Report: "friend amy " + strings.ReplaceAll(hold, "n1", "s1-2")}))
		notTheWorkers(t, w, row, "s1-1.w1", "s1-1", "s1-2")
		w.s.Work.Card("s1-2").Col = Landed
		p, _ := TickResolve(w.s, TickReq{Friends: []FriendSeat{amy}})
		w.must(p)
		lawfulDeal(w, amy)
		assert.Equal(t, "2", dealtAttempt(w, "s1-1"), "an attempt number is spent, never a redeal or a failure")
		assert.Equal(t, 0, w.s.Primary("s1-1").Int("failed"))
	})

	t.Run("a hold naming a need that has landed is judged as any other", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, friendBrief("friend amy"), fleetBrief("flash"))
		amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
		dealStarted(w, amy)
		row := FriendRow("amy")
		// the need has landed by the finish; the report says it had not (a stale base): the
		// table's facts decide, so the brief-defect rule judges the HOLD as before
		setNeeds(w.s, "s1-1", "s1-2")
		w.s.Work.Card("s1-2").Col = Landed
		report := "friend amy " + strings.ReplaceAll(hold, "n1", "s1-2")
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: row, Gens: gensOf(w.s, "s1-1.w1"), Failed: true, Report: report}))
		assert.Empty(t, w.notesOf(NNeedsUnlanded), "no needs-stop: every need has landed")
		assert.Len(t, w.notesOf(NBriefDefect), 1, "the HOLD is judged: a brief defect, the seat's")
		assert.Equal(t, Review, w.s.StateOf("s1-1"), "never returned to the deal")
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

	t.Run("past the cap a stop on unlanded needs is judged", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, friendBrief("friend amy"))
		amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
		dealStarted(w, amy)
		row := FriendRow("amy")
		// the primary was already returned MaxNeedsReturns times for its needs
		w.s.Work.Card("s1-1").Fields[FieldNeedsReturns] = itoa(MaxNeedsReturns)
		setNeeds(w.s, "s1-1", "n1")
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: row, Gens: gensOf(w.s, "s1-1.w1"), Failed: true, Report: "friend amy " + hold}))
		assert.Empty(t, w.notesOf(NNeedsUnlanded), "no free return past the cap")
		assert.NotEmpty(t, append(w.notesOf(NBriefDefect), w.notesOf(NWorkFailed)...), "a judgment for the seat")
		assert.Equal(t, Review, w.s.StateOf("s1-1"))
	})
}

// A needs-stop whose worker reported no usage is still one take of its route, counted neither
// ok nor failed (Stella's read of PR 96): its consumer record is written priced or not, as an
// ordinary finish's is, so the route stats (StatsSince) never lose it.
func TestANoUsageNeedsStopIsStillOneTake(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
	wc := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, wc)
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1"), Who: wc.Row}))
	route := wc.F(FieldRoute)
	if route == "" {
		route = "-"
	}
	setNeeds(w.s, "s1-1", "n1")
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: wc.Row, Gens: gensOf(w.s, "s1-1.w1"), Failed: true,
		Report: "HOLD: DEPENDS-ON n1 has not landed"})) // no Usage
	require.Len(t, w.notesOf(NNeedsUnlanded), 1, "the fixture: a needs-stop")
	var got *RouteTakes
	for i, r := range Stats(w.s).Routes {
		if r.Route == route {
			got = &Stats(w.s).Routes[i]
		}
	}
	require.NotNil(t, got, "the take is attributed to its route %q", route)
	assert.Equal(t, 1, got.Takes, "one take")
	assert.Equal(t, 0, got.OK)
	assert.Equal(t, 0, got.Failed, "a coordination failure is never the route's failure")
}

// A taken card given a need not landed whose worker finishes ok is the coordination's
// failure, never a pass: the finish sends it working -> waiting, not to review, whatever the
// worker reports, so no primary reaches review (or merging, or landed) with a named need not
// landed (docs/SPEC-SPRINT.md section 9, rule 11; the gap named open in section 3).
func TestATakenCardFinishingOkWithAnUnlandedNeedWaits(t *testing.T) {
	t.Parallel()
	w := setup(t, 2) // s1-1 and s1-2, both ready
	w.must(Deal(w.s, DealReq{Sel: Sel{Only: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, wc, "the fixture: s1-1 is dealt")
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1"), Who: wc.Row}))
	require.Equal(t, Working, wc.Col, "the fixture: taken")
	// the taken card is given a need not landed (s1-2 is ready, not landed)
	setNeeds(w.s, "s1-1", "s1-2")
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: wc.Row, Gens: gensOf(w.s, "s1-1.w1")}))

	require.Equal(t, Waiting, w.s.StateOf("s1-1"), "an ok finish with an unlanded need waits, never review")
	require.Empty(t, w.notesOf(NWorkOK), "no ok note for work that would reach review with an unlanded need")
	require.Len(t, w.notesOf(NNeedsUnlanded), 1, "the coordination failure is noted")
	w.clean("the end-of-tick check after an ok finish with an unlanded need: no rule-11 break")

	// once the need lands, the next deal cuts the next attempt, as a needs-stop does
	w.s.Work.Card("s1-2").Col = Landed
	p, _ := TickResolve(w.s, TickReq{})
	w.must(p)
	lawfulDeal(w)
	require.Equal(t, "2", dealtAttempt(w, "s1-1"), "an attempt number is spent, never a redeal or a failure")
}

// (5) When the needs land, the card is dealt on the next tick as any card: a card withdrawn
// for its needs waits (working -> waiting at once), and once they land the tick's resolve
// readies it and the deal deals it.
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
	require.Equal(t, Waiting, w.s.StateOf("s1-1"), "working -> waiting in the withdrawing tick")
	w.clean("the end-of-tick check after the withdrawing tick: no rule-11 break")

	// the need lands: the next tick readies and deals it
	w.s.Work.Card("s1-2").Col = Landed
	p, _ := TickResolve(w.s, TickReq{Friends: []FriendSeat{amy}})
	w.must(p)
	lawfulDeal(w, amy)
	require.NotEmpty(t, dealtAttempt(w, "s1-1"), "s1-1 is dealt once its need landed: %s", w.s.StateOf("s1-1"))
}
