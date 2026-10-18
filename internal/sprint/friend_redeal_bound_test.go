package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A friend's card past its deadline is a redeal, never a late judgment and never a failure
// (docs/SPEC-SPRINT.md section 1; TickFriendRedeal, verdicts.go): its work card goes to her
// redealt cell and its primary waits ready, and the friend the next deal gives its next
// attempt runs her own whole deadline from her own deal, not from the first friend's deal or
// the redeal. This replaces the old friend-take bound coverage (the take-back measured from
// the next friend's deal, WordNeverTaken, FieldTakenFrom, RuleFriendTake): a friend's late
// card is a redeal, not a take-back, so rule friend-take has nothing to answer against her.

// TestAFriendsRedealtCardGetsTheNextFriendsOwnBound is the redeal rule's bound: the deadline
// the redeal measures is the friend's own, and the next friend dealt its next attempt gets
// her own whole deadline, never the first friend's spent one.
func TestAFriendsRedealtCardGetsTheNextFriendsOwnBound(t *testing.T) {
	t.Parallel()

	t.Run("past her deadline the card is redealt, never taken back", func(t *testing.T) {
		t.Parallel()
		w, _ := friendDealt(t)
		wc := w.s.Fleet.Card("s1-1.w1")
		assert.Equal(t, Ready, wc.Col, "dealt to amy, ready past her bound")
		assert.Empty(t, openOf(w, NWorkLate, "s1-1"), "no late judgment on a friend's card")
		w.part(TickFriendRedeal, TickReq{})
		wc = w.s.Fleet.Card("s1-1.w1")
		assert.Equal(t, Redealt, wc.Col, "redealt on her row, not taken back")
		assert.Equal(t, Ready, w.s.StateOf("s1-1"), "its primary waits ready for the next deal")
		assert.Empty(t, openOf(w, NWorkLate, "s1-1"), "never a late judgment")
		assert.Empty(t, logged(w, RuleFriendTake), "never a friend-take: the redeal is no take-back")
		w.clean("redealt past its deadline")
	})

	t.Run("the next friend's deadline runs from her own deal, not the first friend's", func(t *testing.T) {
		t.Parallel()
		w, bob := friendDealt(t)
		w.part(TickFriendRedeal, TickReq{})
		require.Equal(t, Redealt, w.s.Fleet.Card("s1-1.w1").Col, "redealt on amy's row")

		// the next deal gives bob the next attempt; her own deal starts her own deadline, so
		// amy's eight hours spent on the card are not bob's
		dealStarted(w, bob)
		wc := w.s.Fleet.Card("s1-1.w2")
		require.NotNil(t, wc, "the next deal deals the next attempt")
		assert.Equal(t, FriendRow("bob"), wc.Row, "dealt to the next friend")
		assert.Equal(t, Working, wc.Col, "bob started it: working on her row")
		w.part(TickFriendRedeal, TickReq{})
		assert.Equal(t, Working, w.s.Fleet.Card("s1-1.w2").Col, "inside her own deadline: still hers, not redealt")

		// past her own deadline it is redealt too
		w.tick(DeadlineUnfinished * 4)
		w.part(TickFriendRedeal, TickReq{})
		assert.Equal(t, Redealt, w.s.Fleet.Card("s1-1.w2").Col, "past her own deadline: redealt on her row")
		w.clean("redealt after her own deadline")
	})
}

// TickFriendRedeal never moves a live lane at the deadline, at any age (beat or lane renewal);
// the first time a live card passes unfinishedLimit it raises ONE notice to the coordinator
// naming the card and how long it has run since first_taken, and never repeats it on later
// ticks of the same generation.
func TestFriendRedealNoticesALiveLaneOncePastTheDeadline(t *testing.T) {
	t.Parallel()

	t.Run("live by fresh beat past unfinishedLimit", func(t *testing.T) {
		t.Parallel()
		w, _ := friendDealt(t, "s1-1.w1")
		amy := workerSeatFixture(FriendSeat{
			Name:    "amy",
			Width:   1,
			Status:  Up,
			Class:   "flash,pro",
			Running: []string{"s1-1.w1"},
			Beat:    Beat{At: w.s.Now.Add(-time.Minute), Friend: &FriendReport{Running: []string{"s1-1.w1"}}},
		})
		p, due := TickFriendRedeal(w.s, TickReq{Friends: []FriendSeat{amy}})
		require.Zero(t, due)
		w.must(p)

		wc := w.s.Fleet.Card("s1-1.w1")
		assert.Equal(t, Working, wc.Col, "live lane is never redealt at the deadline")
		assert.Equal(t, 1, wc.Int("gen"), "stays on the same generation")
		assert.Equal(t, Working, w.s.StateOf("s1-1"), "primary stays working")

		var notices []Note
		for _, n := range p.Notes {
			if n.Kind == Happened {
				notices = append(notices, n)
			}
		}
		for _, u := range p.Units {
			for _, n := range u.Notes {
				if n.Kind == Happened {
					notices = append(notices, n)
				}
			}
		}
		require.Len(t, notices, 1, "exactly one coordinator notice on the first tick past unfinishedLimit")
		assert.Contains(t, notices[0].What, "s1-1.w1")
		assert.Contains(t, notices[0].What, "8h0m0s")
		assert.Contains(t, notices[0].What, "first_taken")

		// Next tick on the same generation: still live, never moved, and never noticed a second time.
		w.tick(10 * time.Minute)
		amy.Beat.At = w.s.Now.Add(-time.Minute)
		p2, due2 := TickFriendRedeal(w.s, TickReq{Friends: []FriendSeat{amy}})
		require.Zero(t, due2)
		assert.Empty(t, p2.Units, "never repeated on a later tick of the same generation")
		assert.Empty(t, p2.Notes, "never repeated on a later tick of the same generation")
	})

	t.Run("live by lane renewal past unfinishedLimit", func(t *testing.T) {
		t.Parallel()
		w, _ := friendDealt(t, "s1-1.w1")
		w.must(Progress(w.s, ProgressReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: FriendRow("amy"), Gens: gensOf(w.s, "s1-1.w1")}))
		p, due := TickFriendRedeal(w.s, TickReq{})
		require.Zero(t, due)
		w.must(p)

		wc := w.s.Fleet.Card("s1-1.w1")
		assert.Equal(t, Working, wc.Col, "lane renewed inside LaneGoneAfter is live and never redealt")
		assert.Equal(t, 1, wc.Int("gen"))
		var notices []Note
		for _, u := range p.Units {
			notices = append(notices, u.Notes...)
		}
		notices = append(notices, p.Notes...)
		require.Len(t, notices, 1, "one notice on the first tick past unfinishedLimit")
		assert.Contains(t, notices[0].What, "s1-1.w1")

		w.tick(time.Minute)
		p2, _ := TickFriendRedeal(w.s, TickReq{})
		assert.Empty(t, p2.Units)
		assert.Empty(t, p2.Notes)
	})
}

// A working friend card that is not live at the deadline keeps today's redeal unchanged.
func TestFriendRedealMovesANotLiveLaneAsToday(t *testing.T) {
	t.Parallel()
	w, _ := friendDealt(t, "s1-1.w1")
	w.part(TickFriendRedeal, TickReq{})
	wc := w.s.Fleet.Card("s1-1.w1")
	assert.Equal(t, Redealt, wc.Col, "not live past its deadline: redealt as today")
	assert.Equal(t, 2, wc.Int("gen"))
	assert.Equal(t, Ready, w.s.StateOf("s1-1"), "primary returns to ready")
	w.clean("not-live lane redealt past its deadline")
}
