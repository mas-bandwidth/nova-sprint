package sprint

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/hook"
)

// tla/FriendUnhookGrace.tla: an unhooked friend inside the grace keeps her working cards
// and reads (NoReturnInGrace); past it they are returned (NoStrandedPastGrace), and a
// friend flapping without answering is past it by her last answer (AnswerBounds).
func TestFriendUnhookInGraceByBothClocks(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	s := &Snapshot{Now: now}
	grace := s.FriendIdleAfter()
	if grace != FriendIdleDefault {
		t.Fatalf("grace is %s, want the friend-idle default %s", grace, FriendIdleDefault)
	}
	for _, tc := range []struct {
		name string
		seat FriendSeat
		want bool
	}{
		{"hooked is no grace", FriendSeat{Unhooked: false, UnhookedAt: now, AnsweredAt: now}, false},
		{"just unhooked, answered recently", FriendSeat{Unhooked: true, UnhookedAt: now.Add(-time.Minute), AnsweredAt: now.Add(-6 * time.Minute)}, true},
		{"busy friend answered half an hour before the unhook", FriendSeat{Unhooked: true, UnhookedAt: now.Add(-time.Minute), AnsweredAt: now.Add(-36 * time.Minute)}, true},
		{"just inside the unhook clock", FriendSeat{Unhooked: true, UnhookedAt: now.Add(-grace + time.Second), AnsweredAt: now.Add(-grace)}, true},
		{"at the grace", FriendSeat{Unhooked: true, UnhookedAt: now.Add(-grace), AnsweredAt: now.Add(-grace)}, false},
		{"past the grace", FriendSeat{Unhooked: true, UnhookedAt: now.Add(-grace - time.Minute), AnsweredAt: now.Add(-grace - 6*time.Minute)}, false},
		{"flapping, last answer past grace plus a proof's life", FriendSeat{Unhooked: true, UnhookedAt: now.Add(-time.Minute), AnsweredAt: now.Add(-grace - hook.ProofTTL)}, false},
		{"no unhook time", FriendSeat{Unhooked: true, AnsweredAt: now.Add(-time.Minute)}, false},
		{"never answered", FriendSeat{Unhooked: true, UnhookedAt: now.Add(-time.Minute)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FriendUnhookInGrace(s, tc.seat); got != tc.want {
				t.Fatalf("in grace = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFriendUnhookGraceFollowsTheFriendIdleSetting(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	work := NewTable(Work)
	work.SetProp(PropFriendIdle, "5m")
	s := &Snapshot{Now: now, Work: work}
	seat := FriendSeat{Unhooked: true, UnhookedAt: now.Add(-6 * time.Minute), AnsweredAt: now.Add(-7 * time.Minute)}
	if FriendUnhookInGrace(s, seat) {
		t.Fatal("a friend unhooked 6m ago is past a 5m friend-idle grace")
	}
	seat.UnhookedAt = now.Add(-4 * time.Minute)
	if !FriendUnhookInGrace(s, seat) {
		t.Fatal("a friend unhooked 4m ago is inside a 5m friend-idle grace")
	}
}

func unhookGraceWorld(now time.Time, seat FriendSeat) *Snapshot {
	fleet := NewTable(Fleet)
	fleet.SetRows([]string{FriendRow("amy")})
	fleet.Put(&Card{ID: "read.g2", Row: FriendRow("amy"), Col: Working, Fields: map[string]string{"kind": "read", "gen": "2"}})
	seat.Name, seat.Status = "amy", Down
	return &Snapshot{Now: now, Fleet: fleet, Friends: workerSeatFixtures([]FriendSeat{seat})}
}

func TestAnUnhookedFriendInsideTheGraceKeepsHerReads(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	s := unhookGraceWorld(now, FriendSeat{Unhooked: true, UnhookedAt: now.Add(-time.Minute), AnsweredAt: now.Add(-6 * time.Minute)})
	for _, stopped := range []bool{false, true} {
		if p := LiveReturns(s, nil, stopped); len(p.Units) != 0 {
			t.Fatalf("stopped=%v: %d returns inside the grace, want none: %v", stopped, len(p.Units), p.Units[0].Moved)
		}
	}
}

func TestAnUnhookedFriendInsideTheGraceKeepsHerWork(t *testing.T) {
	w := friendWorld(t, friendBrief("friend"))
	seats := workerSeatFixtures([]FriendSeat{{Name: "amy", Width: 1, Status: Up, Class: "flash"}})
	dealStarted(w, seats...)
	c := w.s.Fleet.Cell(FriendRow("amy"), Working)[0]
	gen := c.Int("gen")
	w.s.Friends = workerSeatFixtures([]FriendSeat{{Name: "amy", Status: Down, Unhooked: true, UnhookedAt: w.s.Now.Add(-time.Minute), AnsweredAt: w.s.Now.Add(-6 * time.Minute)}})
	if p := LiveReturns(w.s, nil, false); len(p.Units) != 0 {
		t.Fatalf("%d returns inside the grace, want none", len(p.Units))
	}
	// she hooks in again inside the grace: up, and the card is still hers at its generation
	w.s.Friends = workerSeatFixtures([]FriendSeat{{Name: "amy", Width: 1, Status: Up, Class: "flash"}})
	if p := LiveReturns(w.s, nil, false); len(p.Units) != 0 {
		t.Fatalf("%d returns after she came back, want none", len(p.Units))
	}
	if c = w.s.Fleet.Card(c.ID); c.Col != Working || c.Row != FriendRow("amy") || c.Int("gen") != gen {
		t.Fatalf("her card is %s:%s gen=%d, want %s:working gen=%d", c.Row, c.Col, c.Int("gen"), FriendRow("amy"), gen)
	}
}

func TestAnUnhookedFriendPastTheGraceHasHerWorkAndReadsReturned(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, tc := range []struct {
		name string
		seat FriendSeat
	}{
		{"unhooked past the grace", FriendSeat{Unhooked: true, UnhookedAt: now.Add(-FriendIdleDefault), AnsweredAt: now.Add(-FriendIdleDefault - 5*time.Minute)}},
		{"flapping past the answer bound", FriendSeat{Unhooked: true, UnhookedAt: now.Add(-time.Minute), AnsweredAt: now.Add(-UnhookAnswerBound(FriendIdleDefault))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := LiveReturns(unhookGraceWorld(now, tc.seat), nil, false)
			if len(p.Units) != 1 {
				t.Fatalf("got %d returns, want the read returned", len(p.Units))
			}
		})
	}
	w := friendWorld(t, friendBrief("friend"))
	dealStarted(w, workerSeatFixtures([]FriendSeat{{Name: "amy", Width: 1, Status: Up, Class: "flash"}})...)
	c := w.s.Fleet.Cell(FriendRow("amy"), Working)[0]
	w.s.Friends = workerSeatFixtures([]FriendSeat{{Name: "amy", Status: Down, Unhooked: true, UnhookedAt: w.s.Now.Add(-FriendIdleDefault - time.Second), AnsweredAt: w.s.Now.Add(-FriendIdleDefault - 6*time.Minute)}})
	p := LiveReturns(w.s, nil, false)
	if len(p.Units) == 0 {
		t.Fatal("a friend past the grace must have her working card returned")
	}
	w.must(p)
	if w.s.Fleet.Card(c.ID).Placed() {
		t.Fatal("her working card past the grace must retire to the work deal")
	}
}
