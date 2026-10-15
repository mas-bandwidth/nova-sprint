package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A card dealt to a friend and not taken within the dealt bound returns to the pool and is
// dealt again, never back to the friend it was returned from (docs/SPEC-SPRINT.md section 1,
// a friend's card; tla/FriendDealtReturn.tla Return). The return moves cards and never a
// friend's state: both friends stay up and dealable (the dealt-no-more mark, which kept
// them from the deal and so sent every card to the machines, is retired, v1.3.6).
func TestAFriendWhoNeverTakesReturnsHerCardsToThePool(t *testing.T) {
	t.Parallel()
	briefs := make([]string, 8)
	for i := range briefs {
		briefs[i] = fleetBrief("flash")
	}
	w := friendWorld(t, briefs...)
	w.s.Work.SetProp(PropDealtMax, "90s")
	amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
	bob := FriendSeat{Name: "bob", Width: 2, Status: Up, Class: "flash,pro"}

	// friends first: the two friends who never take hold all eight cards, the machines none
	dealWith(w, amy, bob)
	first := map[string]string{}
	for i := 1; i <= 8; i++ {
		id := "s1-" + itoa(i) + ".w1"
		wc := w.s.Fleet.Card(id)
		require.NotNil(t, wc, "s1-%d is dealt", i)
		require.True(t, IsFriendRow(wc.Row), "s1-%d sits untaken on a friend's row", i)
		first[id] = wc.Row
	}

	// past the dealt bound the tick returns them to the pool, and the next deal places each
	// again, never on the row it was returned from
	w.tick(2 * time.Minute)
	dealWith(w, amy, bob) // the return
	for _, f := range []FriendSeat{amy, bob} {
		require.True(t, friendDealable(w.s, f), "%s is up, so dealable: the return wrote no state of hers", f.Name)
	}
	dealWith(w, amy, bob) // the deal
	for i := 1; i <= 8; i++ {
		id := "s1-" + itoa(i) + ".w1"
		wc := w.s.Fleet.Card(id)
		require.NotNil(t, wc, "s1-%d is still dealt", i)
		require.NotEqual(t, first[id], wc.Row, "s1-%d is not dealt back to the friend it was returned from", i)
	}
	w.clean("returned and dealt again")
}

// A friend who takes a working card (the tick's friendUnstartedWorking confirms it) has her
// other ready card left alone in the same tick that it is past the bound: she has taken one,
// so the return leaves her alone (tla/FriendDealtReturn.tla Return requires none of her
// cards started).
func TestAFriendWhoTakesAWorkingCardIsNotMarkedInTheSameTick(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, fleetBrief("flash"), fleetBrief("flash"))
	w.s.Work.SetProp(PropDealtMax, "90s")
	amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
	dealWith(w, amy)
	require.NotNil(t, w.s.Fleet.Card("s1-1.w1"), "s1-1 is dealt to amy")
	require.NotNil(t, w.s.Fleet.Card("s1-2.w1"), "s1-2 is dealt to amy")

	// a finish's next moved s1-1 to working and she is working it (progress, no start)
	wc := w.s.Fleet.Card("s1-1.w1")
	wc.Col = Working
	wc.Fields["taken"] = stamp(w.s.Now)
	wc.Fields[FieldProgress] = stamp(w.s.Now)
	w.s.Fleet.Put(wc)

	// past the dealt bound, the tick confirms her start and leaves her alone
	w.tick(2 * time.Minute)
	dealWith(w, amy)
	require.Equal(t, "1", w.s.Fleet.Card("s1-1.w1").F(FieldStarted), "she took s1-1")
	require.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-2.w1").Row, "she took a card, so the same tick does not return her other")
	w.clean("no return from a friend who took a working card")
}

// friendSeat is a bare amy seat, up with flash and pro, no running job.
func friendSeat() FriendSeat {
	return FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
}

// A friend who has taken a card has none returned in the same tick that another of her
// cards is past the bound: the return leaves her alone, and the untaken card stays
// (tla/FriendDealtReturn.tla Return requires none of her cards started; friendHasTaken).
// One row per start path, each through the real API (Deal, beat/Progress, FriendStart,
// FriendTake, TickDeal), never a hand-set field.
func TestAFriendWhoHasTakenACardHasNoneReturned(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "ready to working by the tick (her beat names it running)",
			run: func(t *testing.T) {
				w := friendWorld(t, fleetBrief("flash"), fleetBrief("flash"))
				w.s.Work.SetProp(PropDealtMax, "90s")
				dealWith(w, friendSeat())
				// her beat names s1-1 running: the tick's start moves it ready -> working
				dealWith(w, FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro", Running: []string{"s1-1.w1"}})
				require.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "s1-1 is working")
				require.Equal(t, "1", w.s.Fleet.Card("s1-1.w1").F(FieldStarted), "she started s1-1")

				w.tick(2 * time.Minute)
				dealWith(w, friendSeat())
				require.Equal(t, Ready, w.s.Fleet.Card("s1-2.w1").Col, "the untaken s1-2 is not returned")
				require.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-2.w1").Row, "s1-2 stays on her row")
			},
		},
		{
			name: "the sync receipt (FriendStart)",
			run: func(t *testing.T) {
				w := friendWorld(t, fleetBrief("flash"), fleetBrief("flash"))
				w.s.Work.SetProp(PropDealtMax, "90s")
				dealWith(w, friendSeat())
				w.must(FriendStart(w.s, FriendStartReq{Friend: "amy", IDs: []string{"s1-1.w1"}, Gens: map[string]int{"s1-1.w1": 1}}))
				require.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "friend sync's start moves s1-1 to working")
				require.Equal(t, "1", w.s.Fleet.Card("s1-1.w1").F(FieldStarted), "she started s1-1")

				w.tick(2 * time.Minute)
				dealWith(w, friendSeat())
				require.Equal(t, Ready, w.s.Fleet.Card("s1-2.w1").Col, "the untaken s1-2 is not returned")
			},
		},
		{
			name: "a working card confirmed started by the tick (friendUnstartedWorking)",
			run: func(t *testing.T) {
				w := friendWorld(t, fleetBrief("flash"), fleetBrief("flash"), fleetBrief("flash"))
				w.s.Work.SetProp(PropDealtMax, "90s")
				dealWith(w, friendSeat())
				// she takes s1-1 and finishes it: her finish's next moves s1-2 to working
				// with no start of hers
				dealWith(w, FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro", Running: []string{"s1-1.w1"}})
				w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: FriendRow("amy"), Gens: gensOf(w.s, "s1-1.w1"), Head: "abc"}))
				require.Equal(t, Working, w.s.Fleet.Card("s1-2.w1").Col, "her next, s1-2, is working")
				require.Empty(t, w.s.Fleet.Card("s1-2.w1").F(FieldStarted), "no start of hers yet")
				// her daemon stamps progress on s1-2: the tick's own read confirms her start
				w.must(Progress(w.s, ProgressReq{Sel: Sel{IDs: []string{"s1-2.w1"}}, As: FriendRow("amy"), Gens: map[string]int{"s1-2.w1": 1}}))

				w.tick(2 * time.Minute)
				dealWith(w, friendSeat())
				require.Equal(t, "1", w.s.Fleet.Card("s1-2.w1").F(FieldStarted), "the tick confirms her start")
				require.Equal(t, Ready, w.s.Fleet.Card("s1-3.w1").Col, "the untaken s1-3 is not returned")
			},
		},
		{
			name: "a ready card carrying progress beside an untaken past-bound card",
			run: func(t *testing.T) {
				w := friendWorld(t, fleetBrief("flash"), fleetBrief("flash"))
				w.s.Work.SetProp(PropDealtMax, "90s")
				dealWith(w, friendSeat())
				// she starts s1-1 and it carries progress
				dealWith(w, FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro", Running: []string{"s1-1.w1"}})
				w.must(Progress(w.s, ProgressReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: FriendRow("amy"), Gens: map[string]int{"s1-1.w1": 1}}))
				// the hold takes her cards back, and the tick redeals them: s1-1 comes back
				// ready carrying its progress stamp, beside the untaken s1-2
				w.must(FriendTake(w.s, FriendTakeReq{Friend: "amy", All: true, Hold: true, Started: map[string]string{"s1-1.w1": "her beat names it running"}}))
				dealWith(w, friendSeat())
				require.Equal(t, Ready, w.s.Fleet.Card("s1-1.w1").Col, "s1-1 is redealt ready")
				require.NotEmpty(t, w.s.Fleet.Card("s1-1.w1").F(FieldProgress), "s1-1 keeps its progress stamp")

				w.tick(2 * time.Minute)
				dealWith(w, friendSeat())
				require.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "s1-1's progress promotes it to working this tick")
				require.True(t, startedNow(w.s.Fleet.Card("s1-1.w1")), "her start is stamped at its generation")
				require.Equal(t, Ready, w.s.Fleet.Card("s1-2.w1").Col, "the untaken s1-2 is not returned")
			},
		},
		{
			name: "her beat names a job running (f.Running > 0)",
			run: func(t *testing.T) {
				w := friendWorld(t, fleetBrief("flash"), fleetBrief("flash"))
				w.s.Work.SetProp(PropDealtMax, "90s")
				// her beat names a job that is none of these cards
				amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro", Running: []string{"some-other-job"}}
				dealWith(w, amy)

				w.tick(2 * time.Minute)
				dealWith(w, amy)
				require.Equal(t, Ready, w.s.Fleet.Card("s1-2.w1").Col, "the untaken s1-2 is not returned")
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, tt.run)
	}
}

// A friend whose untaken cards were returned to the pool stays up and is dealt again: the
// return moves cards and never her state (the dealt-no-more mark, a fourth state that kept an
// up friend from the deal, is retired, v1.3.6; the owner, 2026-10-14: "there is only up,
// down, held from the sprint POV").
func TestAFriendWhoseCardsWereReturnedIsDealtAgain(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, fleetBrief("flash"))
	w.s.Work.SetProp(PropDealtMax, "90s")
	dealWith(w, friendSeat())
	require.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-1.w1").Row, "s1-1 is dealt to amy")

	// she does not take it: past the bound the tick returns it to the pool
	w.tick(2 * time.Minute)
	dealWith(w, friendSeat())
	require.Equal(t, Withdrawn, w.s.Fleet.Card("s1-1.w1").Col, "the untaken card is returned to the pool")
	require.True(t, friendDealable(w.s, friendSeat()), "she is up, so she is dealt: the return wrote no state of hers")

	// a fresh card is dealt to her
	w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-2", Brief: fleetBrief("flash")}}}))
	dealWith(w, friendSeat())
	require.NotNil(t, w.s.Fleet.Card("s1-2.w1"), "s1-2 is dealt")
	require.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-2.w1").Row, "an up friend whose cards were returned is dealt again")
}
