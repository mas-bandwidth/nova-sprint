package sprint

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"
)

// A worker's lanes are thin clients of the server (v1.2.6; the owner, 2026-10-10: "always
// client/server, not distributed programming"). The server is the one truth for what each
// lane of a worker's row holds: a working card carries the lane that holds it (FieldLane),
// and the epoch is the store's own (cards are keyed by it, and every worker verb is fenced
// on it). A lane keeps nothing that decides what it holds: it asks.
//
// A lane's take (take --as <member> --lane <n>, LaneTake) names its member and its lane and
// nothing else, the server picking the epoch: it answers the card the lane holds (the
// server's record, after a crash, a restart or a clear the lane asks the same way), else a
// working card on the row no lane holds (a friend's next, a read dealt into working), else
// takes the next ready card, as a take by count does, held to the row's width. A friend's
// card a lane takes is her start (friendStartSet): a lane's take is the run beginning, so the
// tick never puts it back ready as unstarted. Progress and finish name the lane (--lane):
// a card another lane of the row holds is refused, and the lane drops the job and asks
// again. A lane that has not been heard from within LaneGoneAfter (its take, its progress)
// is gone, and the tick bounces its card back ready (laneGoneUnits). The model is
// nova-tools tla/FriendLane.tla (WorkingOnlyOnTheServersSay, OneRequestApart, NoStaleStep,
// WorkingResolves, AllDone; reversed witnesses MCFriendLaneBroken*.cfg).

// FieldLane is the lane of its row that holds a working card ("1", "2", ...); FieldLaneAt is
// when that lane was last heard from on it (its take, its progress).
const (
	FieldLane   = "lane"
	FieldLaneAt = "lane_at"
)

// LaneGoneAfter is how long a lane holding a card may go unheard before the tick says it
// gone and bounces its card back ready: its daemon stamps progress every three minutes while
// the card runs (nova-tools friend.ProgressEvery), so this is three stamps missed and more.
const LaneGoneAfter = 10 * time.Minute

// laneOf is the lane a request names as a field value; "" for none.
func laneOf(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// readerRow says row is a reader's row on the readers table (reader-<name>), whose lanes are
// its read slots: a lane's take there begins a read (asked -> reading).
func readerRow(s *Snapshot, row string) bool {
	_, ok := ReaderMachine(row)
	return ok && s.Readers != nil && s.Readers.HasRow(row)
}

// laneCells is where a lane of row holds its card: a worker's working cell on the fleet
// table, a reader's reading cell on the readers table.
func laneCells(s *Snapshot, row string) []*Card {
	if readerRow(s, row) {
		return s.Readers.Cell(row, Reading)
	}
	return s.Fleet.Cell(row, Working)
}

// LaneHolds is the card lane n of row holds on the server: the working (a reader's: reading)
// card on the row whose FieldLane is the lane, nil when none does (the oldest when, against
// every rule, two do).
func LaneHolds(s *Snapshot, row string, n int) *Card {
	lane := laneOf(n)
	if lane == "" {
		return nil
	}
	var held []*Card
	for _, c := range laneCells(s, row) {
		if c.F(FieldLane) == lane {
			held = append(held, c)
		}
	}
	if len(held) == 0 {
		return nil
	}
	SortCards(held)
	return held[0]
}

// laneSet is the fields a lane holding c is given now: the lane, heard from now, and on a
// friend's row her start (a lane's take is her run beginning; friendStartSet), on a machine's
// the taken stamps.
func laneSet(s *Snapshot, c *Card, row string, n int) (set map[string]string, unset []string) {
	if friend, ok := FriendOfRow(row); ok {
		if isRead(c) {
			set, unset = friendTaken(s, c, friend)
		} else {
			set, unset = friendStartSet(s, c, friend)
		}
	} else {
		set, unset = takenStamps(c, s.Now), []string{"untaken_since"}
	}
	set[FieldLane] = laneOf(n)
	set[FieldLaneAt] = stamp(s.Now)
	return set, unset
}

// LaneTake is a lane's take: nothing moves when the lane holds a card already (the verb says
// which); else a working card on its row that no lane holds becomes the lane's; else the
// next ready card is taken to the lane, as a take by count of one (takeOne: the row's
// width, its seat, its turns). A take naming cards, several members or no lane is refused.
func LaneTake(s *Snapshot, r TakeReq) Plan {
	var p Plan
	switch {
	case r.Lane <= 0:
		p.refuse("take", "a lane's take names its lane: take --as <member> --lane <n>, n from 1")
		return p
	case named(r.Sel):
		p.refuse("take", "a lane's take names its member and its lane and nothing else: take --as <member> --lane <n>; the server says which card")
		return p
	case len(Split(r.As)) != 1:
		p.refuse("take", "a lane's take names one member: take --as <member> --lane <n>")
		return p
	}
	if LaneHolds(s, r.As, r.Lane) != nil {
		return p // the lane holds its card: nothing moves, the verb answers it
	}
	if readerRow(s, r.As) {
		return readerLaneTake(s, r)
	}
	working := append([]*Card(nil), s.Fleet.Cell(r.As, Working)...)
	SortCards(working)
	for _, c := range working {
		if c.F(FieldLane) != "" {
			continue
		}
		// a card working on the row that no lane holds (her finish's next, a read dealt into
		// working, a card from before lanes asked the server) is the asking lane's
		if _, why := takeSeat(s, r.As); why != "" {
			p.refuse("take", why)
			return p
		}
		set, unset := laneSet(s, c, r.As, r.Lane)
		p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.F("stream"), Changes: []Change{change(Fleet, setEntry(c, set, unset...))},
			Moved: fmt.Sprintf("%s %s:working -> lane %d (working, no lane held it) gen=%s", c.ID, r.As, r.Lane, cmp.Or(c.F("gen"), "1"))})
		return p
	}
	q := r
	q.Sel = Sel{Limit: 1}
	one := takeOne(s, q)
	for i := range one.Units {
		u := &one.Units[i]
		c := s.Fleet.Card(u.Key)
		if c == nil {
			continue
		}
		set, unset := laneSet(s, c, r.As, r.Lane)
		for j := range u.Changes {
			e := &u.Changes[j].Entry
			if e.ID != c.ID {
				continue
			}
			merged := maps.Clone(e.Set)
			if merged == nil {
				merged = map[string]string{}
			}
			maps.Copy(merged, set)
			e.Set = nonEmpty(merged)
			// each field unset once: the store refuses a name twice (the take's own and the lane's)
			all := append(slices.Clone(e.Unset), unset...)
			slices.Sort(all)
			e.Unset = unsetPresent(c, slices.Compact(all))
		}
		u.Moved += " lane=" + laneOf(r.Lane)
	}
	return one
}

// readerLaneTake is a reader's lane given its read: a read reading on its row that no lane
// holds, else its oldest asked read begun now (asked -> reading, as read --begin moves it),
// the lane on it either way. A reader's lanes are its read slots, which its daemon counts.
func readerLaneTake(s *Snapshot, r TakeReq) Plan {
	var p Plan
	lane := map[string]string{FieldLane: laneOf(r.Lane), FieldLaneAt: stamp(s.Now)}
	reading := append([]*Card(nil), s.Readers.Cell(r.As, Reading)...)
	SortCards(reading)
	for _, c := range reading {
		if c.F(FieldLane) == "" {
			p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.F("stream"), Changes: []Change{change(Readers, setEntry(c, lane))},
				Moved: fmt.Sprintf("%s %s:reading -> lane %d (reading, no lane held it) gen=%d", c.ID, r.As, r.Lane, max(c.Int("gen"), 1))})
			return p
		}
	}
	asked := append([]*Card(nil), s.Readers.Cell(r.As, Asked)...)
	SortCards(asked)
	if len(asked) == 0 {
		return p
	}
	c := asked[0]
	set := maps.Clone(lane)
	set["begun"] = stamp(s.Now)
	p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.F("stream"), Changes: []Change{change(Readers, moveEntry(c, c.Row, Reading, set, FieldReturned))},
		Moved: fmt.Sprintf("%s asked -> reading lane=%d gen=%d", c.ID, r.Lane, max(c.Int("gen"), 1))})
	return p
}

// laneRefusal is why lane n of as may not report on c: another lane of the row holds it. A
// card no lane holds (a worker that names no lane, a card from before lanes) is the row's.
func laneRefusal(c *Card, as string, n int) string {
	if n <= 0 || c.F(FieldLane) == "" || c.F(FieldLane) == laneOf(n) {
		return ""
	}
	return fmt.Sprintf("held by lane %s of %s, not lane %d: the lane drops it and asks the server what it holds", c.F(FieldLane), as, n)
}

// laneGoneUnits is the tick's bounce-back: every working card a lane holds whose lane has not
// been heard from (FieldLaneAt, its progress stamp) within LaneGoneAfter goes back ready on its
// row, untaken, its lane and its start cleared, one line each: the lane is gone (its daemon
// stopped, its host down, its process lost), and a lane that asks again is told it holds
// nothing.
func laneGoneUnits(s *Snapshot) []Unit {
	var units []Unit
	working := append([]*Card(nil), s.Fleet.Column(Working)...)
	SortCards(working)
	for _, c := range working {
		lane := c.F(FieldLane)
		if lane == "" {
			continue
		}
		heard := latestStamp(c.F(FieldLaneAt), c.F(FieldProgress))
		if heard.IsZero() || s.Now.Sub(heard) < LaneGoneAfter {
			continue
		}
		unset := []string{FieldLane, FieldLaneAt, FieldProgress, "taken", FieldFriendDeadline, FieldStarted}
		units = append(units, Unit{Key: c.ID, Stream: c.F("stream"), Changes: []Change{change(Fleet, moveEntry(c, c.Row, Ready, map[string]string{"untaken_since": stamp(s.Now)}, unset...))},
			Moved: fmt.Sprintf("%s %s:working -> ready (lane %s gone: not heard from since %s, past %s)", c.ID, c.Row, lane, stamp(heard), LaneGoneAfter)})
	}
	return units
}

// latestStamp is the latest of RFC3339 stamps; zero when none reads.
func latestStamp(stamps ...string) time.Time {
	var at time.Time
	for _, v := range stamps {
		if t, err := time.Parse(time.RFC3339, v); err == nil && t.After(at) {
			at = t
		}
	}
	return at
}
