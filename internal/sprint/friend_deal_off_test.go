package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// set --friend-deal off (v1.2.27 item 8; the owner, 2026-10-14: "something simple globally
// that just disables friends taking tasks in v1.2.x series"): the deal assigns no card to any
// friend's row and counts no friend as room. On, the default, deals as today. Every test is on
// the in-memory world with its test clock (w.tick): no store, no sleep, no socket.

// friendDealOffWorld is two machines up and five unheld friends up whose tiers cover the
// cards: ten WHO: friend cards and six plain cards, ready.
func friendDealOffWorld(t *testing.T) (*world, []FriendSeat) {
	t.Helper()
	plain := "c: plain work\nREPO: mas-bandwidth/nova-tools\n\nThe task."
	var briefs []string
	for range 10 {
		briefs = append(briefs, friendBrief("friend"))
	}
	for range 6 {
		briefs = append(briefs, plain)
	}
	w := friendWorld(t, briefs...)
	var seats []FriendSeat
	for _, n := range []string{"amy", "bob", "cat", "dan", "eve"} {
		seats = append(seats, FriendSeat{Name: n, Width: 2, Status: Up, Class: "flash,pro"})
	}
	return w, seats
}

// friendRowsDealt is the work cards on every friend's row; machineRowsDealt the work cards on the machines'.
func friendRowsDealt(w *world) (n int) {
	for _, c := range w.s.Fleet.Column(Ready, Working) {
		if IsFriendRow(c.Row) && c.F("kind") == "work" {
			n++
		}
	}
	return n
}

func machineRowsDealt(w *world) (n int) {
	for _, c := range w.s.Fleet.Column(Ready, Working) {
		if !IsFriendRow(c.Row) && c.F("kind") == "work" {
			n++
		}
	}
	return n
}

func TestFiveUnheldFriendsAreDealtToday(t *testing.T) {
	t.Parallel()
	w, seats := friendDealOffWorld(t)
	require.False(t, w.s.FriendDealOff(), "on by default")
	dealWith(w, seats...)
	assert.Positive(t, friendRowsDealt(w), "today's deal hands the friends their cards")
}

func TestFriendDealOffDealsNoFriendOverAHundredTicks(t *testing.T) {
	t.Parallel()
	w, seats := friendDealOffWorld(t)
	p := w.must(Set(w.s, SetReq{FriendDeal: SwitchOff, Who: "coordinator"}))
	assert.Contains(t, p.Units[0].Moved, "friend-deal off")
	require.True(t, w.s.FriendDealOff())
	for range 100 {
		dealWith(w, seats...)
		require.Zero(t, friendRowsDealt(w), "no card is dealt to a friend while the friend deal is off")
		w.tick(time.Minute)
	}
	assert.Positive(t, machineRowsDealt(w), "the machines are still dealt the cards they serve")
	assert.False(t, w.s.FriendsOff(), "the friends' switch is untouched")

	w.must(Set(w.s, SetReq{FriendDeal: SwitchOn, Who: "coordinator"}))
	dealWith(w, seats...)
	assert.Positive(t, friendRowsDealt(w), "on: today's deal again")
}

func TestTheFriendDealSwitchRoundTripsThroughTheStore(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	assert.Equal(t, SwitchOn, settingValue(t, w, "friend-deal"))
	w.must(Set(w.s, SetReq{FriendDeal: SwitchOff, Who: "coordinator"}))
	v, ok := w.s.Work.Prop(PropFriendDeal)
	require.True(t, ok)
	assert.Equal(t, SwitchOff, v)
	assert.Equal(t, SwitchOff, settingValue(t, w, "friend-deal"))
	w.must(Set(w.s, SetReq{FriendDeal: SwitchOn, Who: "coordinator"}))
	assert.False(t, w.s.FriendDealOff())
	assert.Equal(t, SwitchOn, settingValue(t, w, "friend-deal"))

	p := Set(w.s, SetReq{FriendDeal: "maybe", Who: "coordinator"})
	require.Len(t, p.Refused, 1)
	assert.True(t, strings.Contains(p.Refused[0].Why, "--friend-deal wants on or off"), p.Refused[0].Why)
}

func settingValue(t *testing.T, w *world, flag string) string {
	t.Helper()
	for _, g := range SettingsOf(w.s) {
		if g.Flag == flag {
			return g.Value
		}
	}
	t.Fatalf("no setting %s", flag)
	return ""
}
