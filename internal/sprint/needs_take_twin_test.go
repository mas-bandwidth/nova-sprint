package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// One rule held by three doors (docs/SPEC-SPRINT.md, needs and the deal; take): a primary
// whose named needs have not all landed (WaitsFor, steps_work.go) is neither dealt nor
// taken, by any worker. The friends' deal never deals it (TickDeal's offer,
// friendDealPass); a ready work card already dealt on a row goes back to the pool at once
// in the tick's deal (needsWithdrawals); and a take refuses it (takeOne). A repeated take
// of a card already working on the row at the generation named is a no-op.

// setNeeds puts the named needs on a primary and leaves the cards off the table
// (not landed), the way a brief edit records them.
func setNeeds(s *Snapshot, id string, needs ...string) {
	s.Work.Card(id).Fields["needs"] = strings.Join(needs, ",")
}

// (1) The friends' deal never deals a card whose needs have not landed: a ready primary
// released by the seat whose two needs are in review is on no row after a tick.
func TestTheFriendsDealNeverDealsACardWithUnlandedNeeds(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m2"}))
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{
		{ID: "s1-1", Brief: fleetBrief("flash")},
		{ID: "n1", Brief: fleetBrief("flash")},
		{ID: "n2", Brief: fleetBrief("flash")},
	}}))
	// the two needs are in review (not landed)
	for _, n := range []string{"n1", "n2"} {
		w.s.Work.Card(n).Col = Review
	}
	// the seat releases the primary: it is ready, its two needs in review
	pr := w.s.Work.Card("s1-1")
	pr.Col = Ready
	setNeeds(w.s, "s1-1", "n1", "n2")
	require.Len(t, WaitsFor(w.s, pr, nil), 2, "the fixture: two needs have not landed")

	amy := workerSeatFixture(FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"})
	dealWith(w, amy)
	for _, row := range w.s.Fleet.Rows() {
		for _, col := range []State{Ready, Working} {
			for _, c := range w.s.Fleet.Cell(row, col) {
				require.NotEqual(t, "s1-1", c.F("primary"), "s1-1 is on %s, a card whose needs have not landed", row)
			}
		}
	}
	// rule 11 (check.go): a primary anywhere but waiting has every need landed, so the
	// undealt primary goes back to waiting for its needs (needsWaits)
	require.Equal(t, Waiting, w.s.StateOf("s1-1"), "the undealt primary waits for its needs")
}

// (2) A card already on a friend's ready cell whose needs have not landed goes back to the
// pool at once in the tick's deal; the friend's takes take nothing, and no rule-11 judgment
// is raised.
func TestADealtThenUnheldFriendDoesNotTakeACardWithUnlandedNeeds(t *testing.T) {
	t.Parallel()
	// The withdrawal moves the dealt primary working -> waiting (PR #96's move), held
	// by the world to the store's lawful-move check: the card's own named needs in the
	// pre-state have not landed (lifecycle.go namedNeedsOpen).
	w := friendWorld(t, fleetBrief("flash"))
	amy := workerSeatFixture(FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"})
	w.s.Friends = workerSeatFixtures([]FriendSeat{amy})
	dealWith(w, amy)
	wc := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, wc, "the fixture: s1-1 is dealt")
	require.True(t, IsFriendRow(wc.Row), "the fixture: it sits on a friend's row")

	// its two needs have not landed
	setNeeds(w.s, "s1-1", "n1", "n2")
	// the next tick withdraws the ready card off her row, every move a row of the lifecycle
	// as the store holds it (a dealt primary is working: working -> waiting at once)
	lawfulDeal(w, amy)
	require.False(t, w.s.Fleet.Card("s1-1.w1").Placed(), "the dealt card leaves the fleet table")
	require.Equal(t, Waiting, w.s.StateOf("s1-1"), "the primary waits for its needs again")
	require.Equal(t, 0, w.s.Fleet.Count(FriendRow("amy"), Ready), "the friend's ready cell is empty")

	// a take by count and a take by id by the friend take nothing and say why
	count := Take(w.s, TakeReq{As: FriendRow("amy"), Sel: Sel{Limit: 5}, Who: "amy"})
	require.Empty(t, count.Units, "a take by count takes nothing")
	byID := Take(w.s, TakeReq{As: FriendRow("amy"), Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1"), Who: "amy"})
	require.Empty(t, byID.Units, "a take by id takes nothing")
	require.NotEmpty(t, byID.Refused, "a take by id says why")
	require.Empty(t, w.openOn("s1-1"), "no rule-11 judgment is raised")
	w.clean("withdrawn and not taken")
}

// (3) A take by id on a member's row refuses a card whose needs have not landed; a take by
// count passes over it.
func TestAMemberTakeRefusesACardWithUnlandedNeeds(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
	wc := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, wc, "the fixture: s1-1 is dealt")
	require.Equal(t, Ready, wc.Col)
	setNeeds(w.s, "s1-1", "n1", "n2")

	byID := Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1"), Who: wc.Row})
	require.Empty(t, byID.Units, "a take by id takes nothing")
	require.Len(t, byID.Refused, 1, "a take by id is refused")
	require.Contains(t, byID.Refused[0].Why, "needs n1,n2, not landed: not taken")

	count := Take(w.s, TakeReq{As: wc.Row, Sel: Sel{Limit: 5}, Who: wc.Row})
	require.Empty(t, count.Units, "a take by count passes over it")
}

// (4) A repeated take of a card already working on the row at the generation named is a
// no-op: it writes nothing and records nothing.
func TestARepeatedTakeIsANoOp(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
	wc := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, wc)
	gen := wc.Int("gen")
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1"), Who: wc.Row}))
	require.Equal(t, Working, wc.Col, "the first take moves it to working")

	again := Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: map[string]int{"s1-1.w1": gen}, Who: wc.Row})
	require.Empty(t, again.Units, "a repeated take writes nothing")
	require.Empty(t, again.Refused, "a repeated take is no refusal")
	require.Empty(t, again.Notes, "a repeated take records nothing")
}
