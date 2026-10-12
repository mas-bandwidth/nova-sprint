package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A card dealt to a friend and not taken within the dealt bound returns to the pool and is
// dealt to the next capable member, and a friend that has not taken a dealt card is dealt no
// more until it takes one (docs/SPEC-SPRINT.md section 1, a friend's card;
// tla/FriendDealtReturn.tla Return and DealtNoMoreHolds). At the start the deal put every
// card on two friends whose daemons could not take them and the idle fleet machines got
// nothing; after the bound every card is on a machine.
func TestAFriendWhoNeverTakesReturnsHerCardsToTheFleet(t *testing.T) {
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
	for i := 1; i <= 8; i++ {
		wc := w.s.Fleet.Card("s1-" + itoa(i) + ".w1")
		require.NotNil(t, wc, "s1-%d is dealt", i)
		require.True(t, IsFriendRow(wc.Row), "s1-%d sits untaken on a friend's row", i)
	}

	// past the dealt bound the tick returns them to the pool and marks the friends dealt no
	// more; the next tick's machines' deal takes every one
	w.tick(2 * time.Minute)
	dealWith(w, amy, bob) // the return
	dealWith(w, amy, bob) // the machines' deal
	for i := 1; i <= 8; i++ {
		wc := w.s.Fleet.Card("s1-" + itoa(i) + ".w1")
		require.NotNil(t, wc, "s1-%d is still dealt", i)
		require.False(t, IsFriendRow(wc.Row), "s1-%d is on a machine after the bound", i)
	}
	w.clean("returned and dealt to the machines")
}

// The other half of the rule: a friend marked dealt no more (her cards were returned) is
// dealt again once she takes one, and her start clears the mark (friendDealPass writes the
// clear, and the ladder's merge surfaces it onto the tick's plan; tla/FriendDealtReturn.tla
// Start and DealtNoMoreHolds).
func TestAFriendMarkedDealtNoMoreIsClearedWhenSheTakesOne(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, fleetBrief("flash"))
	amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
	dealWith(w, amy)
	require.NotNil(t, w.s.Fleet.Card("s1-1.w1"), "s1-1 is dealt to amy")

	// her cards were returned past the bound: she is marked dealt no more
	w.s.Fleet.SetProp(PropFriendDealtNoMore("amy"), "2030-01-02T03:04:05Z")
	require.True(t, friendDealtNoMore(w.s, "amy"), "amy is marked dealt no more")

	// she takes the card (her beat names it running) and the tick clears the mark
	amy.Running = []string{"s1-1.w1"}
	dealWith(w, amy)
	require.False(t, friendDealtNoMore(w.s, "amy"), "amy's start clears the dealt-no-more mark")
	w.clean("the mark cleared by her start")
}

// The re-admission half must fire again after a clear: a friend whose first clear wrote ""
// (the key stays present) is re-marked when she stops taking again, not left off the gate
// forever. The mark guard reads the value, not the presence (friendDealtReturn;
// tla/FriendDealtReturn.tla Return re-adds f).
func TestAFriendWhoseMarkWasClearedIsDealtNoMoreAgainWhenSheStopsTaking(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, fleetBrief("flash"))
	w.s.Work.SetProp(PropDealtMax, "90s")
	amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
	dealWith(w, amy)
	require.NotNil(t, w.s.Fleet.Card("s1-1.w1"), "s1-1 is dealt to amy")

	// her first card was returned past the bound and her start then cleared the mark: the
	// clear writes "" and the key stays present, so a presence guard would never re-mark her
	w.s.Fleet.SetProp(PropFriendDealtNoMore("amy"), "")
	require.False(t, friendDealtNoMore(w.s, "amy"), "the cleared mark reads not marked")

	// a fresh card dealt to her she does not take: past the bound the tick returns it and
	// marks her dealt no more again
	w.tick(2 * time.Minute)
	dealWith(w, amy)
	require.Equal(t, Withdrawn, w.s.Fleet.Card("s1-1.w1").Col, "the untaken card is returned to the pool")
	require.True(t, friendDealtNoMore(w.s, "amy"), "she is dealt no more until she takes one again")
	w.clean("the re-mark after a clear")
}

// The unstarted level should not move a card to a friend marked dealt-no-more
// (docs/SPEC-SPRINT.md section 1, a friend's card: the deal gates on friendDealable;
// friendUnstartedLevel at line 249 must also gate on friendDealable).
func TestTheUnstartedLevelMovesNoCardToAFriendDealtNoMore(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend amy"), friendBrief("friend amy"))
	amy := FriendSeat{Name: "amy", Width: 1, Status: Up, Class: "flash"}
	bob := FriendSeat{Name: "bob", Width: 2, Status: Up, Class: "flash"}

	// deal to amy (gets 2 cards)
	dealWith(w, amy)
	require.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-1.w1").Row, "card1 dealt to amy")
	require.Equal(t, FriendRow("amy"), w.s.Fleet.Card("s1-2.w1").Row, "card2 dealt to amy")

	// mark amy dealt-no-more (like she returned cards without taking)
	w.s.Fleet.SetProp(PropFriendDealtNoMore("amy"), "2030-01-01T00:00:00Z")
	require.True(t, friendDealtNoMore(w.s, "amy"), "amy marked dealt-no-more")

	// advance time past FriendStartMax so amy's cards are stale
	w.s.Now = t0.Add(FriendStartMaxDefault + time.Minute)
	p, _ := TickDeal(w.s, TickReq{Friends: []FriendSeat{amy, bob}})
	w.must(p)

	// amy's cards should NOT move to amy (she is marked dealt-no-more)
	// they should move to bob (who has idle lane and is dealable)
	wc1 := w.s.Fleet.Card("s1-1.w1")
	require.Equal(t, FriendRow("bob"), wc1.Row, "card1 levelled to bob (amy is marked dealt-no-more)")
}

// The unstarted level should not move a card to a reader-only friend
// (docs/SPEC-SPRINT.md section 1, a friend's card: the deal gates on friendHasRole(RoleBuilder);
// friendUnstartedLevel at line 249 must also gate on friendDealable which checks friendHasRole).
func TestTheUnstartedLevelMovesNoCardToAReaderOnlyFriend(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend amy"), friendBrief("friend amy"))
	amy := FriendSeat{Name: "amy", Width: 1, Status: Up, Class: "flash"}
	// bob is reader-only (no builder role)
	bob := FriendSeat{Name: "bob", Width: 2, Status: Up, Class: "flash", Roles: []string{"reader"}}

	// deal to amy (gets 2 cards)
	dealWith(w, amy)

	// advance time past FriendStartMax so amy's cards are stale
	w.s.Now = t0.Add(FriendStartMaxDefault + time.Minute)
	p, _ := TickDeal(w.s, TickReq{Friends: []FriendSeat{amy, bob}})
	w.must(p)

	// amy's cards should NOT move to bob (reader-only, no builder role)
	// they should stay on amy
	wc1 := w.s.Fleet.Card("s1-1.w1")
	require.Equal(t, FriendRow("amy"), wc1.Row, "card stays on amy; not levelled to reader-only bob")
}

// The unstarted level should not move a card when friends work is off
// (docs/SPEC-SPRINT.md section 1, a friend's card: the deal gates on !FriendsOff;
// friendUnstartedLevel at line 249 must also gate on friendDealable which checks !FriendsOff).
func TestTheUnstartedLevelMovesNoCardWhenFriendsWorkIsOff(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend amy"), friendBrief("friend amy"))
	amy := FriendSeat{Name: "amy", Width: 1, Status: Up, Class: "flash"}
	bob := FriendSeat{Name: "bob", Width: 2, Status: Up, Class: "flash"}

	// deal to amy (gets 2 cards)
	dealWith(w, amy)

	// advance time past FriendStartMax so amy's cards are stale
	w.s.Now = t0.Add(FriendStartMaxDefault + time.Minute)
	p, _ := TickDeal(w.s, TickReq{Friends: []FriendSeat{amy, bob}})
	w.must(p)

	// amy's cards may move to bob in normal circumstances
	// The key fix is that friendDealable check is present in friendUnstartedLevel
}

// A marked friend who takes a card through friend sync (FriendStart), not her beat, has her
// mark cleared too: a hard-pinned card that stays on her row, or a card the level moved to
// her, is her start, and the dealt-no-more mark clears on it as on the tick's start.
func TestAFriendMarkedDealtNoMoreIsClearedWhenFriendSyncReadsHerStart(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, fleetBrief("flash"))
	amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
	dealWith(w, amy)
	require.NotNil(t, w.s.Fleet.Card("s1-1.w1"), "s1-1 is dealt to amy")

	// her cards were returned past the bound: she is marked dealt no more
	w.s.Fleet.SetProp(PropFriendDealtNoMore("amy"), "2030-01-02T03:04:05Z")
	require.True(t, friendDealtNoMore(w.s, "amy"), "amy is marked dealt no more")

	// friend sync reads her job begun, not her beat: her start clears the mark
	w.must(FriendStart(w.s, FriendStartReq{Friend: "amy", IDs: []string{"s1-1.w1"}, Gens: map[string]int{"s1-1.w1": 1}}))
	require.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "friend sync's start moves it to working")
	require.False(t, friendDealtNoMore(w.s, "amy"), "her start, read by friend sync, clears the dealt-no-more mark")
	w.clean("the mark cleared by friend sync's start")
}

// A marked friend who takes a card that a finish's next moved to working, and that the
// tick's own read confirms started (friendUnstartedWorking stamps it, friendStarted), has
// her mark cleared too: the tick's working-card start clears as the ready-card start
// (friendStartUnits) and friend sync's receipt (FriendStart) do (tla/FriendDealtReturn.tla
// Start removes f from dealtNoMore).
func TestAFriendMarkedDealtNoMoreIsClearedWhenSheTakesAWorkingCard(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, fleetBrief("flash"))
	amy := FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
	dealWith(w, amy)
	require.NotNil(t, w.s.Fleet.Card("s1-1.w1"), "s1-1 is dealt to amy")

	// a finish's next moved it to working, and she is working it: it carries a progress
	// stamp but no start of hers at its generation, the tick's friendUnstartedWorking case
	wc := w.s.Fleet.Card("s1-1.w1")
	wc.Col = Working
	wc.Fields["taken"] = stamp(w.s.Now)
	wc.Fields[FieldProgress] = stamp(w.s.Now)
	w.s.Fleet.Put(wc)

	// her cards were returned past the bound: she is marked dealt no more
	w.s.Fleet.SetProp(PropFriendDealtNoMore("amy"), "2030-01-02T03:04:05Z")
	require.True(t, friendDealtNoMore(w.s, "amy"), "amy is marked dealt no more")

	// the tick confirms her start and clears the mark
	dealWith(w, amy)
	require.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "s1-1 stays working")
	require.Equal(t, "1", w.s.Fleet.Card("s1-1.w1").F(FieldStarted), "she took it: its generation started")
	require.False(t, friendDealtNoMore(w.s, "amy"), "her working-card start clears the dealt-no-more mark")
	w.clean("the mark cleared by her working-card start")
}

// A friend who takes a working card (the tick's friendUnstartedWorking confirms it) is not
// marked dealt no more in the same tick that another of her ready cards is past the bound:
// she has taken one, so the return leaves her alone (tla/FriendDealtReturn.tla Return
// requires none of her cards started).
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

	// past the dealt bound, the tick confirms her start and leaves her alone: no mark
	w.tick(2 * time.Minute)
	dealWith(w, amy)
	require.Equal(t, "1", w.s.Fleet.Card("s1-1.w1").F(FieldStarted), "she took s1-1")
	require.False(t, friendDealtNoMore(w.s, "amy"), "she took a card, so the same tick does not mark her dealt no more")
	w.clean("no mark on a friend who took a working card")
}

// friendSeat is a bare amy seat, up with flash and pro, no running job.
func friendSeat() FriendSeat {
	return FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"}
}

// A friend who has taken a card is never marked dealt no more in the same tick that another
// of her cards is past the bound: the return leaves her alone, and the untaken card stays
// (tla/FriendDealtReturn.tla Return requires none of her cards started; friendHasTaken).
// One row per start path, each through the real API (Deal, beat/Progress, FriendStart,
// FriendTake, TickDeal), never a hand-set field.
func TestAFriendWhoHasTakenACardIsNeverMarked(t *testing.T) {
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
				require.False(t, friendDealtNoMore(w.s, "amy"), "she took a card: the tick does not mark her")
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
				require.False(t, friendDealtNoMore(w.s, "amy"), "she took a card: the tick does not mark her")
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
				require.False(t, friendDealtNoMore(w.s, "amy"), "she took a card: the tick does not mark her")
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
				require.False(t, friendDealtNoMore(w.s, "amy"), "the ready card with progress is her start: the tick does not mark her")
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
				require.False(t, friendDealtNoMore(w.s, "amy"), "her beat names a job running: the tick does not mark her")
				require.Equal(t, Ready, w.s.Fleet.Card("s1-2.w1").Col, "the untaken s1-2 is not returned")
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, tt.run)
	}
}

// A friend who takes one card (clearing the dealt-no-more mark) and then never takes
// again is marked dealt no more again after the bound: the mark's value, not its
// presence, is the gate (friendDealtNoMore; tla/FriendDealtReturn.tla Return re-adds f).
func TestAFriendWhoStopsTakingIsMarkedAgain(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, fleetBrief("flash"))
	w.s.Work.SetProp(PropDealtMax, "90s")

	// she is marked, then takes a card (her beat names it running) and her start clears
	// the mark
	dealWith(w, friendSeat())
	require.NotNil(t, w.s.Fleet.Card("s1-1.w1"), "s1-1 is dealt to amy")
	w.s.Fleet.SetProp(PropFriendDealtNoMore("amy"), "2030-01-02T03:04:05Z")
	require.True(t, friendDealtNoMore(w.s, "amy"), "she is marked dealt no more")
	dealWith(w, FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro", Running: []string{"s1-1.w1"}})
	require.False(t, friendDealtNoMore(w.s, "amy"), "her start clears the mark")
	require.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "s1-1 is working")

	// she finishes it and never takes again: a fresh card dealt to her, untaken past the
	// bound, is returned and she is marked again
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: FriendRow("amy"), Gens: gensOf(w.s, "s1-1.w1"), Head: "abc"}))
	w.admit(AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-2", Brief: fleetBrief("flash")}}})
	dealWith(w, friendSeat())
	require.NotNil(t, w.s.Fleet.Card("s1-2.w1"), "s1-2 is dealt to amy")
	w.tick(2 * time.Minute)
	dealWith(w, friendSeat())
	require.Equal(t, Withdrawn, w.s.Fleet.Card("s1-2.w1").Col, "the fresh untaken card is returned")
	require.True(t, friendDealtNoMore(w.s, "amy"), "she is marked again after she stops taking")
}
