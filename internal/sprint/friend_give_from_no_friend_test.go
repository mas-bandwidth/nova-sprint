package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// friend give on a card bounced to ready with its work card finished (the "Done" of tla/FriendGive.tla) (a brief-replaced bounce,
// steps_work.go AttemptPredatesBrief): the friend deal deals it over the Done work card at its
// next attempt, so the verb does too, from no friend (tla/FriendGive.tla; the coordinator,
// 2026-10-15: "<id>.w2 was never taken back from friend <f> (taken from no friend)").

// bounce is a brief-replaced bounce of the card's current work card: the work card to Done with
// head_superseded, its primary ready, nothing writing taken_from.
func bounce(w *world, id string) {
	w.t.Helper()
	pr := w.s.Primary(id)
	wc := w.s.Fleet.Placed(WorkCardID(id, pr.Int("attempt")))
	require.NotNil(w.t, wc)
	w.must(Plan{Units: []Unit{{Key: id, Stream: pr.Row, Changes: []Change{
		change(Fleet, moveEntry(wc, wc.Row, Finished, map[string]string{"head_superseded": "true"})),
		change(Work, moveEntry(pr, pr.Row, Ready, nil, "work")),
	}}}})
	require.Equal(w.t, Ready, w.s.StateOf(id))
	require.Equal(w.t, Finished, w.s.Fleet.Card(wc.ID).Col)
}

// bouncedWorld is s1-1 bounced at attempt 2: .w2 Done on amy's row, the primary ready, no
// machine up, amy up with room.
func bouncedWorld(t *testing.T, briefs ...string) *world {
	t.Helper()
	w := friendWorld(t, briefs...)
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m2"}))
	amy := FriendSeat{Name: "amy", Width: 1, Status: Up, Class: "flash"}
	dealStarted(w, amy)
	bounce(w, "s1-1")
	w.s.Friends = []FriendSeat{amy}
	w.must(FriendGive(w.s, FriendGiveReq{Friend: "amy", IDs: []string{"s1-1"}, Who: "rowan"}))
	require.NotNil(t, w.s.Fleet.Card("s1-1.w2"))
	bounce(w, "s1-1")
	require.Equal(t, "2", w.s.Primary("s1-1").F("attempt"))
	return w
}

func TestFriendGiveHandsAReadyCardFromNoFriend(t *testing.T) {
	t.Parallel()
	w := bouncedWorld(t, friendBrief("friend"))
	require.Empty(t, w.s.Fleet.Card("s1-1.w2").F(FieldTakenFrom), "a bounce writes no taken_from")
	p := FriendGive(w.s, FriendGiveReq{Friend: "amy", IDs: []string{"s1-1"}, Who: "rowan"})
	require.Empty(t, p.Refused, "a Done work card is not dealt: the deal deals over it")
	assert.Contains(t, strings.Join(p.Said, "\n"), "s1-1: handed to amy")
	w.must(p)
	wc := w.s.Fleet.Card("s1-1.w3")
	require.NotNil(t, wc)
	assert.Equal(t, FriendRow("amy"), wc.Row)
	assert.Equal(t, Ready, wc.Col)
	assert.Equal(t, Working, w.s.StateOf("s1-1"))
	assert.Equal(t, "3", w.s.Primary("s1-1").F("attempt"))
	assert.Equal(t, Finished, w.s.Fleet.Card("s1-1.w2").Col, "the bounced attempt is left as it was")
	assert.Empty(t, Check(w.s, nil))
}

func TestFriendGiveRefusesACardWhoseWorkCardIsLive(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend"))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "down", Member: "m2"}))
	amy := FriendSeat{Name: "amy", Width: 1, Status: Up, Class: "flash"}
	dealStarted(w, amy, FriendSeat{Name: "bob", Width: 1, Status: Up, Class: "flash"})
	wc := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, wc)
	// force the primary ready over a live work card: the state the verb must still refuse
	pr := w.s.Primary("s1-1")
	w.s.Friends = []FriendSeat{amy, {Name: "bob", Width: 1, Status: Up, Class: "flash"}}
	for _, col := range []string{Ready, Working} {
		wc.Col = col
		w.s.Fleet.Put(wc)
		pr.Col = Ready
		w.s.Work.Put(pr)
		p := FriendGive(w.s, FriendGiveReq{Friend: "bob", IDs: []string{"s1-1"}})
		require.Len(t, p.Refused, 1, col)
		assert.Contains(t, p.Refused[0].Why, wc.Row+":"+col, "names its row")
		assert.Contains(t, p.Refused[0].Why, "friend take")
		assert.Empty(t, p.Units)
	}
}

func TestFriendGiveFromNoFriendHoldsTheDealsEligibility(t *testing.T) {
	t.Parallel()
	// hard-pinned to another friend: refused naming the pin and unpin
	w := bouncedWorld(t, friendBrief("only friend amy"))
	w.s.Friends = []FriendSeat{{Name: "amy", Width: 1, Status: Up, Class: "flash"}, {Name: "bob", Width: 1, Status: Up, Class: "flash"}}
	p := FriendGive(w.s, FriendGiveReq{Friend: "bob", IDs: []string{"s1-1"}})
	require.Len(t, p.Refused, 1)
	assert.Contains(t, p.Refused[0].Why, "pinned to friend amy")
	assert.Contains(t, p.Refused[0].Why, "nova-sprint unpin s1-1")
	assert.Empty(t, p.Units)
	// to the friend it names it is a hand
	p = FriendGive(w.s, FriendGiveReq{Friend: "amy", IDs: []string{"s1-1"}})
	require.Empty(t, p.Refused)

	// her tiers, her restriction, the card's needs
	w = bouncedWorld(t, friendBrief("friend"), friendBrief("friend"))
	w.s.Friends = []FriendSeat{{Name: "amy", Width: 1, Status: Up, Tiers: []string{"pro"}}}
	p = FriendGive(w.s, FriendGiveReq{Friend: "amy", IDs: []string{"s1-1"}})
	require.Len(t, p.Refused, 1)
	assert.Contains(t, p.Refused[0].Why, "does not serve tier")
	w.s.Friends = []FriendSeat{{Name: "amy", Width: 1, Status: Up, Class: "flash", Streams: []string{"other-*"}}}
	p = FriendGive(w.s, FriendGiveReq{Friend: "amy", IDs: []string{"s1-1"}})
	require.Len(t, p.Refused, 1)
	assert.Contains(t, p.Refused[0].Why, "restriction")
	w.s.Friends = []FriendSeat{{Name: "amy", Width: 1, Status: Up, Class: "flash"}}
	w.s.Work.Card("s1-1").Fields["needs"] = "s1-2"
	p = FriendGive(w.s, FriendGiveReq{Friend: "amy", IDs: []string{"s1-1"}})
	require.Len(t, p.Refused, 1)
	assert.Contains(t, p.Refused[0].Why, "not landed")

	// a friend not up is no refusal for a coordinator's give: the card waits on her row
	w = bouncedWorld(t, friendBrief("friend"))
	w.s.Friends = []FriendSeat{{Name: "amy", Width: 1, Status: Down, Class: "flash"}}
	p = FriendGive(w.s, FriendGiveReq{Friend: "amy", IDs: []string{"s1-1"}})
	require.Empty(t, p.Refused)
}
