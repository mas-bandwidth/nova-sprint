package sprint

import (
	"github.com/mas-bandwidth/nova-tools/hook"
	"testing"
	"time"
)

func TestFriendHookStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		hook hook.Status
		held bool
		want string
	}{
		{"down", hook.Status{State: hook.StateUnhooked}, false, Down},
		{"down trumps held", hook.Status{State: hook.StateUnhooked}, true, Down},
		{"held", hook.Status{State: hook.StateProven}, true, Held},
		{"up", hook.Status{State: hook.StateProven}, false, Up},
		{"stale server trumps held", hook.Status{State: hook.StateProven, Down: "hook server stale"}, true, Down},
		{"missing", hook.Status{}, false, Down},
		{"unproven", hook.Status{State: hook.StateLive}, false, Down},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FriendHookStatus(tc.hook, tc.held); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestExplicitUnhookReturnsWorkWithoutABeatAndKeepsStopReceipt(t *testing.T) {
	w := friendWorld(t, friendBrief("friend"))
	seats := []FriendSeat{{Name: "amy", Width: 1, Status: Up, Class: "flash"}}
	dealStarted(w, seats...)
	c := w.s.Fleet.Cell(FriendRow("amy"), Working)[0]
	gen := c.Int("gen")
	w.s.Friends = []FriendSeat{{Name: "amy", Status: Down, Held: true, Unhooked: true}}
	p := LiveReturns(w.s, nil, true)
	if len(p.Units) == 0 {
		t.Fatal("explicit unhook must return work without a report")
	}
	w.must(p)
	c = w.s.Fleet.Card(c.ID)
	if c.Placed() {
		t.Fatal("unhooked work must retire to the work deal")
	}
	if c.Int("stopped_from_gen") != gen || c.Int("gen") <= gen {
		t.Fatal("STOP receipt missing on retired work")
	}
}

func TestHookDownPreservesSprintReaderHold(t *testing.T) {
	s := &Snapshot{Friends: []FriendSeat{{Name: "amy", Status: Down, Held: true}}}
	if !s.heldFriendReader(ReaderPrefix + "amy") {
		t.Fatal("down display must not release sprint hold")
	}
}

func TestExplicitUnhookReadCarriesPairedStopReceipt(t *testing.T) {
	fleet := NewTable(Fleet)
	fleet.SetRows([]string{FriendRow("amy")})
	c := &Card{ID: "read.g2", Row: FriendRow("amy"), Col: Working, Fields: map[string]string{"kind": "read", "gen": "2"}}
	fleet.Put(c)
	s := &Snapshot{Now: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC), Fleet: fleet, Friends: []FriendSeat{{Name: "amy", Status: Down, Unhooked: true}}}
	p := LiveReturns(s, nil, true)
	if len(p.Units) != 1 {
		t.Fatalf("got %d returns, want one read return", len(p.Units))
	}
	set := p.Units[0].Changes[0].Entry.Set
	if set["stopped_from_gen"] != "2" || set["gen"] != "3" {
		t.Fatalf("missing paired STOP receipt: %v", set)
	}
}
