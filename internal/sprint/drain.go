package sprint

import (
	"fmt"
	"strings"
)

// A member or reader that drains (the owner, 2026-10-11 ~03:50Z: "Why are cards stuck in
// ready."; and ~04:10Z: "held machines don't take work rowan."). A member's loop whose binary
// was replaced under it drains: it takes no new card and stops when the last child it runs is
// reported, which can be hours. Its beat says so (fleet beat --draining, a reader's queue
// --draining; Beat.Draining), so the server holds the state, never the member: while that
// beat is fresh its no-room word is a drain's (NoRoomNow, DrainWord), and every path that
// gives a member a card passes it by as it passes a member under its disk floor (notQuiet:
// the deal, the level, a down's redeal, the rebalance, a rework's and a redo's deal; the
// read-card deal). What was dealt to it and not taken is returned on the next tick
// (drainReturns, the level's), and the reads asked of a reader that drains, or of a reader
// whose machine is held or drains, and not begun are taken back (readerOff, sweepReads), each
// return a note addressed to the coordinator. The model is tla/NoRoom.tla
// (NeverDealtToDraining, NeverAskedOfHeldOrDraining, DealtReturns).

// Draining says the member's or reader's fresh beat says it drains (its no-room word is a
// drain's, DrainWord). // tla/DirtyTick.tla DrainStart
func (s *Snapshot) Draining(name string) bool { return IsDrainWord(s.NoRoom[name]) }

// ReaderOwner is the fleet machine a reader row belongs to: reader-<m>, or reader-<m>-<n>
// with n digits, for a fleet member m of the snapshot. A friend's reader, and a reader
// named for no fleet member, belongs to none.
func (s *Snapshot) ReaderOwner(reader string) (string, bool) {
	m, ok := ReaderMachine(reader)
	if !ok || s.Fleet == nil {
		return "", false
	}
	for _, f := range s.Friends {
		if f.Name == m {
			return "", false
		}
	}
	member := func(name string) bool {
		return name != "" && !IsFriendRow(name) && s.MemberCtl(name) != nil
	}
	if member(m) {
		return m, true
	}
	if i := strings.LastIndexByte(m, '-'); i > 0 && i < len(m)-1 && strings.Trim(m[i+1:], "0123456789") == "" && member(m[:i]) {
		return m[:i], true
	}
	return "", false
}

// readerOff is why a reader up is asked nothing and its reads asked and not begun go back:
// its beat says it drains, or the fleet machine it belongs to (ReaderOwner) is held or
// drains; "" when none is so. A reader whose beat says only that it has no room (its disk)
// is passed by the ask (readerRooms) and hands back what it is asked itself.
// tla/NoRoom.tla Off: known[m] and (held or draining), so a reader of a machine the
// sprint does not know belongs to no machine and is asked like any reader.
// tla/DirtyTick.tla NotAskedOfHeldOrDraining
func (s *Snapshot) readerOff(reader string) string {
	if s.Draining(reader) {
		return reader + " drains (" + s.NoRoom[reader] + ")"
	}
	m, ok := s.ReaderOwner(reader)
	if !ok {
		return ""
	}
	if ctl := s.MemberCtl(m); ctl != nil && ctl.F("held") != "" {
		return "its machine " + m + " is held"
	}
	if s.Draining(m) {
		return "its machine " + m + " drains (" + s.NoRoom[m] + ")"
	}
	return ""
}

// drainNote is the note, addressed to the coordinator, of one card returned (NDrainReturned).
func drainNote(s *Snapshot, n Note, who, what string) Note {
	n.Who, n.What = who, what
	n.To = s.Coordinator
	n.Hint = "nothing to answer: the deal or the ask gives it to another; nova-sprint doctor names a drain past " + DoctorDrainBound.String()
	return n
}

// drainReturns is the level's return of what a draining member was dealt and has not taken:
// every card ready on the row of an up member whose fresh beat says it drains. A work card is
// withdrawn as a ready card on a resting route is (withdrawCard: no take ended, so no redeal
// is spent), its primary ready for the deal, which gives it to a member that does not drain
// (notQuiet); a read card is taken back as one off a member going down is, for the read-card
// deal. Its working cards stay: they are its children's, and finish there. One note per card,
// to the coordinator.
func drainReturns(s *Snapshot, p *Plan, who string) {
	if s.Fleet == nil || len(s.NoRoom) == 0 {
		return
	}
	for _, m := range s.UpMembers() {
		if !s.Draining(m) {
			continue
		}
		cards := append([]*Card{}, s.Fleet.Cell(m, Ready)...)
		SortCards(cards)
		for _, c := range cards {
			what := fmt.Sprintf("%s returned from %s: dealt and not taken, and %s takes no new card (%s); the deal gives it to a member that does", c.ID, m, m, s.NoRoom[m])
			if isRead(c) {
				n := drainNote(s, happened(NDrainReturned, c.F("stream"), s.Now, c.F("primary")), who, what)
				p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.F("stream"),
					Changes: []Change{change(Fleet, removeEntry(c, map[string]string{"retired": stamp(s.Now), "retired_by": RetiredByAway}))},
					Notes:   []Note{n}, Moved: what})
				continue
			}
			u := withdrawCard(s, c, false, NDrainReturned, who, what)
			for i := range u.Notes {
				u.Notes[i] = drainNote(s, u.Notes[i], who, what)
			}
			if len(u.Notes) == 0 {
				u.Notes = []Note{drainNote(s, happened(NDrainReturned, c.F("stream"), s.Now, c.F("primary")), who, what)}
			}
			p.Units = append(p.Units, u)
		}
	}
}
