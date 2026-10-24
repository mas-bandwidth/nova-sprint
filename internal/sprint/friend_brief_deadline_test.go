package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A friend's card briefed longer than DeadlineUnfinished runs its brief's own deadline plus
// BriefDeadlineMargin before the redeal takes it (unfinishedLimit). 2026-10-24: cards briefed
// 240 minutes were redealt at exactly 2h0m0s with their work in flight, and most attempts of
// rn-event-driven-timing, rn-no-alloc-after-create and rn-win-lifecycle-1000x came back empty.
// The clock is the world's, moved by hand.
func TestAFriendsCardBriefed240MinutesIsNotRedealtAtTwoHours(t *testing.T) {
	t.Parallel()
	for _, line := range []string{"DEADLINE: 240m", "Deadline: finish within 240 minutes; report what you have", "DEADLINE: 14400"} {
		t.Run(line, func(t *testing.T) {
			t.Parallel()
			w := friendWorld(t, "c: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend amy\n"+line+"\n\nThe task.")
			amy := workerSeatFixture(FriendSeat{Name: "amy", Width: 1, Status: Up, Class: "flash,pro"})
			dealStarted(w, amy)
			require.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "she started it")
			assert.Equal(t, 240*time.Minute+BriefDeadlineMargin, unfinishedLimit(w.s, w.s.Fleet.Card("s1-1.w1")))

			t0 := w.s.Now
			w.s.Now = t0.Add(2*time.Hour + time.Minute)
			p, _ := TickFriendRedeal(w.s, TickReq{})
			assert.Empty(t, p.Units, "at 2h01m it is inside its brief's 240 minutes: not redealt")

			w.s.Now = t0.Add(4*time.Hour + 16*time.Minute)
			w.part(TickFriendRedeal, TickReq{})
			assert.Equal(t, Redealt, w.s.Fleet.Card("s1-1.w1").Col, "at 4h16m it is past 240 minutes and the margin: redealt")
		})
	}
	t.Run("a brief naming a shorter deadline keeps the fleet's two hours", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, "c: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend amy\nDEADLINE: 60m\n\nThe task.")
		dealStarted(w, workerSeatFixture(FriendSeat{Name: "amy", Width: 1, Status: Up, Class: "flash,pro"}))
		assert.Equal(t, DeadlineUnfinished, unfinishedLimit(w.s, w.s.Fleet.Card("s1-1.w1")))
	})
}
