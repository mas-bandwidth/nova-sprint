package sprint

import "fmt"

// A friend's cards returned to ready (FriendReturn): the live pass's return (live.go) of a
// card working on her row whose run is gone. The QUEUE.json reconcile that also called it
// (friend reconcile, and its pass after every tick) is retired: a friend lane's state is the
// server's records alone, her lanes are clients that ask, and a local file never decides
// (the friend lane is a client with no local state; nova-tools tla/FriendLaneClient.tla).

// RetiredByReconcile is the retired_by of a friend's work card friend reconcile returned.
const RetiredByReconcile = "friend reconcile"

// FriendReturnCard is one card reconcile returns: the work card, the generation it read
// and why.
type FriendReturnCard struct {
	ID  string
	Gen int
	Why string
}

// FriendReturnReq returns a friend's abandoned cards to ready.
type FriendReturnReq struct {
	Stopped bool // Return carries the STOP receipt for its captured owner lease.
	Friend  string
	Who     string // who acts: the coordinator
	Cards   []FriendReturnCard
	// Push addresses each card's note to the coordinator, so inbox --push carries it: the
	// run loop's pass, which nobody watches as it acts (friend_reconcile_tick.go).
	Push bool
}

// FriendReturn returns each card named from the friend's row (docs/SPEC-SPRINT.md section
// 1, friend reconcile): a work card working on her row at the generation read, its
// primary working on it, is retired off the table (its record kept, retired_by friend
// reconcile, its return_reason the why), as rework retires a bound card, and its primary
// moves working -> ready, as a member going down leaves one, with no failed-work judgment
// and no redeal spent (the take was never the card's); the tick's friendDealPass places it
// again at its next attempt. A friend's next attempt is a new work card, never this one
// redealt, so the card leaves the table rather than wait withdrawn. One move line and one
// happened note each, saying why. A card not working on her row, at another generation,
// whose primary is not working on it, or named twice is refused.
func FriendReturn(s *Snapshot, r FriendReturnReq) Plan {
	var p Plan
	row := FriendRow(r.Friend)
	seen := map[string]bool{}
	for _, fc := range r.Cards {
		c := s.Fleet.Card(fc.ID)
		var pr *Card
		if c != nil {
			pr = s.Work.Placed(c.F("primary"))
		}
		twice := seen[fc.ID]
		seen[fc.ID] = true
		switch {
		case twice:
			p.refuse(fc.ID, "named twice")
			continue
		case c == nil:
			p.refuse(fc.ID, "no such work card")
			continue
		case !c.Placed():
			p.refuse(fc.ID, "not working: it is off the table, retired by "+orDash(c.F("retired_by")))
			continue
		case c.Col != Working:
			p.refuse(fc.ID, "not working: it is "+placeWord(c))
			continue
		case c.Row != row:
			p.refuse(fc.ID, "dealt to "+c.Row+", not "+row)
			continue
		case c.Int("gen") != fc.Gen:
			p.refuse(fc.ID, fmt.Sprintf("at generation %d, not %d: it moved since it was read", c.Int("gen"), fc.Gen))
			continue
		case pr == nil || pr.Col != Working || pr.F("work") != c.ID:
			p.refuse(fc.ID, "its primary "+c.F("primary")+" is not working on it")
			continue
		}
		retired := map[string]string{"retired": stamp(s.Now), "retired_by": RetiredByReconcile, "return_reason": fc.Why}
		if r.Stopped {
			retired["stopped_from_gen"], retired["gen"] = itoa(fc.Gen), itoa(fc.Gen+1)
		}
		what := "returned by friend reconcile: " + fc.Why
		n := happened(NFriendReturned, pr.Row, s.Now, pr.ID)
		n.Who, n.What = r.Who, what
		if r.Push {
			n.To, n.Hint = s.Coordinator, "run: nova-sprint card "+pr.ID
		}
		p.Units = append(p.Units, Unit{Key: c.ID, Stream: pr.Row, Changes: []Change{
			change(Fleet, removeEntry(c, retired)),
			change(Work, moveEntry(pr, pr.Row, Ready, nil, "work")),
		}, Notes: []Note{n}, Moved: fmt.Sprintf("%s working -> retired, %s; %s working -> ready", c.ID, what, pr.ID)})
	}
	return Lawful(p)
}
