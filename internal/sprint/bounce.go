package sprint

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// BounceBack (v1.2.6; the model is tla/DealFill.tla, rules B1 to B4). The owner, 2026-10-10:
// "We can't have reviews just disappearing like this." "Cards need to bounce back. We can't
// just have cards disappearing." "If they fail to do the thing they are supposed to do, you
// MUST be notified via push."
//
// That day 43 reads were asked of a friend's reader row whose lanes all failed (exit 126)
// and none was begun; 47 cards were dealt to two friends whose daemons could not take them;
// a friend down held 30 ready cards. Each moved only on a coordinator verb.
//
// One machinery for work and reads (reads are consumer cards), the tick's part PartBounce,
// run in the work table's update before the deal, and the one path that returns either:
//
//   - B1: a work card dealt to a row (a machine's or a friend's) and not taken, or a read
//     card asked of one and not begun, within the take bound (set --take-bound, TakeBound)
//     in running time (the sprint's stopped time left out), while the row has a lane its
//     begun cards leave free (rowHasRoom) and has taken, begun or finished nothing within
//     the bound (rowQuiet), returns to the pool: the work card withdrawn, the read card
//     retired spending no one (RetiredByUnstarted). A card waiting behind a member at work
//     is that member's queue, never a member that does not take.
//   - B2: every return is a note addressed to the coordinator, pushed (inbox --push wakes
//     the seat for a note with a To), naming the card, the row, how long and what was done.
//   - B3: a card returned from a friend's row is taken from her (FieldTakenFrom, then
//     FieldFriendsLeft), so no deal places it on her again; one returned from a machine's
//     row goes to another machine with room when there is one (FieldBouncedFrom, the deal);
//     the deal of the same tick gives it to the next capable live member.
//   - B4: the read-card ask asks every read a primary in review still needs, every tick,
//     so a returned read is asked again of the next reader until the card has its reads.
//
// A card is begun when its row's take stamped it, when progress or a report was written on
// it, or when its friend's beat names it running by any name a beat carries (its id, its
// primary, or its job, as friendStarted reads them).

// PartBounce is the bounce-back part of the work table's update, before the deal.
const PartBounce = "bounce"

// NBounced is the note of a card the bounce-back returned to the pool, addressed to the
// coordinator so that it is pushed.
const NBounced = "bounced"

// FieldBouncedFrom is the row a card was last returned from by the bounce-back, and
// FieldBounced when.
const (
	FieldBouncedFrom = "bounced_from"
	FieldBounced     = "bounced"
)

// TakenBackBounced is the FieldTakenBack reason of a friend's card the bounce-back returned.
const TakenBackBounced = "bounced: not taken within the take bound"

// beatsOf is the friend's running list as the tick has it: her seat's (the tick's seats and
// the snapshot's) and her last beat's, by her name or her row.
func beatsOf(s *Snapshot, r TickReq, row string) []string {
	f, ok := FriendOfRow(row)
	if !ok {
		return nil
	}
	var out []string
	for _, key := range []string{f, row} {
		if b, ok := r.Beats[key]; ok && b.Friend != nil {
			out = append(out, b.Friend.Running...)
		}
	}
	for _, seats := range [][]FriendSeat{r.Friends, s.Friends} {
		for _, seat := range seats {
			if seat.Name == f {
				out = append(out, seat.Running...)
			}
		}
	}
	return out
}

// readBegun says the card was begun: a take stamp, a progress stamp or a report on it, or
// its friend's beat naming it running by any name a beat carries (friendStarted: its id,
// its job at its generation, or its primary).
func readBegun(s *Snapshot, c *Card, running []string) bool {
	if c.F("taken") != "" || c.F(FieldReported) != "" {
		return true
	}
	return friendStarted(s, FriendSeat{Running: running}, c)
}

// rowHasRoom says the row has a lane its begun cards leave free, in half slots (a read is
// half a slot, as the ask counts it: readUnitsOf): a machine's width, a friend's seat's.
func rowHasRoom(s *Snapshot, r TickReq, row string, running []string) bool {
	width := 0
	if f, ok := FriendOfRow(row); ok {
		for _, seats := range [][]FriendSeat{r.Friends, s.Friends} {
			for _, seat := range seats {
				if seat.Name == f && seat.Width > 0 && width == 0 {
					width = seat.Width
				}
			}
		}
	} else {
		width = s.Width(row)
	}
	filled := 0
	for _, c := range s.Fleet.Cell(row, Working) {
		switch {
		case !readBegun(s, c, running):
		case isRead(c):
			filled++
		default:
			filled += 2
		}
	}
	return filled < 2*max(width, 1)
}

// rowLastTake is the row's newest take, progress or finish, zero when none.
func rowLastTake(s *Snapshot, row string) time.Time {
	var at time.Time
	for _, col := range []string{Working, DoneOK, DoneFailed} {
		for _, c := range s.Fleet.Cell(row, col) {
			for _, k := range []string{"taken", "first_taken", FieldProgress, "finished"} {
				if t := stampAt(c, k); t.After(at) {
					at = t
				}
			}
		}
	}
	return at
}

// rowQuiet says the row has taken, begun or finished nothing within the take bound, in
// running time: a member that finished a card a moment ago and has not yet taken the next
// is at work.
func rowQuiet(s *Snapshot, row string, elapsed func(string) (time.Duration, bool)) bool {
	at := rowLastTake(s, row)
	if at.IsZero() {
		return true
	}
	d, ok := elapsed(stamp(at))
	return ok && d >= s.TakeBound()
}

// notTakenPast says the card was dealt to its row (a work card in ready) or asked of it (a
// read card in ready, or dealt straight to working on a friend's row and never begun) and
// not taken or begun within the take bound, while the row had a lane free and was quiet
// (B1, tla/DealFill.tla Late), with how long, in running time (elapsed).
func notTakenPast(s *Snapshot, r TickReq, c *Card, running []string, elapsed func(string) (time.Duration, bool)) (time.Duration, bool) {
	var at string
	switch {
	case isRead(c):
		if c.F(FieldReadCard) == "" || readBegun(s, c, running) {
			return 0, false
		}
		at = c.F("asked")
	case c.Col != Ready || friendStarted(s, FriendSeat{Running: running}, c):
		return 0, false // taken, or her beat names it running: started
	default:
		// the work card's own deal to this row, the stamp WorkDeadline names (one clock:
		// the take bound is a second limit on it, never a second stamp)
		_, _, _, own := WorkDeadline(s, c)
		at = c.F(own)
	}
	d, ok := elapsed(at)
	if !ok || d < s.TakeBound() || !rowHasRoom(s, r, c.Row, running) || !rowQuiet(s, c.Row, elapsed) {
		return d, false
	}
	return d, true
}

// TickBounce is the bounce-back (PartBounce; tla/DealFill.tla Tick, B1 to B3): every work
// card dealt and not taken and every read card asked and not begun within the take bound
// (notTakenPast) returned to the pool, each with a note pushed to the coordinator; a
// friend's card taken from her.
func TickBounce(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	if s == nil || s.Fleet == nil || s.Work == nil {
		return p, 0
	}
	bound := s.TakeBound()
	elapsed := func(at string) (time.Duration, bool) { return r.running(s.Now, at) }
	note := func(stream, primary, what string) Note {
		n := happened(NBounced, stream, s.Now, primary)
		n.Who, n.To, n.What = r.who(), s.Coordinator, what
		return n
	}
	reads := map[string]*Unit{}
	for _, c := range s.Fleet.Column(Ready, Working) {
		running := beatsOf(s, r, c.Row)
		d, late := notTakenPast(s, r, c, running, elapsed)
		if !late {
			continue
		}
		set := map[string]string{FieldBouncedFrom: c.Row, FieldBounced: stamp(s.Now)}
		if isRead(c) {
			what := fmt.Sprintf("read %s of %s asked of %s %s ago and not begun within the take bound %s while it had a lane free: retired, asked again of the next reader", c.ID, c.F("primary"), c.Row, d.Round(time.Second), bound)
			set["retired"], set["retired_by"] = stamp(s.Now), RetiredByUnstarted
			u, ok := reads[c.F("primary")]
			if !ok {
				u = &Unit{Key: c.F("primary"), Stream: c.F("stream")}
				reads[c.F("primary")] = u
			}
			u.Changes = append(u.Changes, change(Fleet, removeEntry(c, set)))
			u.Notes = append(u.Notes, note(c.F("stream"), c.F("primary"), what))
			u.Moved = strings.TrimPrefix(u.Moved+"; "+c.ID+" retired off "+c.Row+" (bounce)", "; ")
			continue
		}
		what := fmt.Sprintf("%s (of %s) dealt to %s %s ago and not taken within the take bound %s while it had a lane free: returned to the pool and dealt to the next capable member", c.ID, c.F("primary"), c.Row, d.Round(time.Second), bound)
		if IsFriendRow(c.Row) {
			// B3: taken from her, as a take-back is (FriendTake): no deal places it on her again
			set[FieldTakenFrom], set[FieldTakenBack] = c.Row, TakenBackBounced
		}
		u := withdrawUnit(s, c, set, nil, NBounced, r.who(), what)
		u.Notes = []Note{note(c.F("stream"), c.F("primary"), what)}
		p.Units = append(p.Units, u)
	}
	for _, k := range slices.Sorted(maps.Keys(reads)) {
		p.Units = append(p.Units, *reads[k])
	}
	return p, 0
}
