package sprint

import "fmt"

// friend give of a read card (roadmap reads-never-wait-behind-a-full-row; the coordinator,
// 2026-10-24: four reads dealt to a friend whose row was at its width sat in her ready column
// for an hour, and friend give refused their ids, "no card <primary>.r7.<friend>"). A read card
// named, ready and not begun (FriendCardInFlight), moves to the friend named as a work card is handed: the card is
// retired (RetiredByCoordinator, spending no one: its reader may be dealt it again) and the
// friend's read card of the attempt is cut on her row, working, in one unit. The friend must be
// a reader up, at or above the read's tier, not its own author (the worker of the attempt: a
// friend never reads her own work), not spent at the attempt (readRefusal), with a free slot on
// her row (a read is never given behind a row at its width). tla/FriendReadSlot.tla, Give.

// friendGiveRead hands the read card c to the friend r names, or refuses it, one line.
// given counts the reads this give already handed to each friend, against her free slots;
// tops the read tops the cards it cuts need; declared says her row is declared in the plan.
func friendGiveRead(p *Plan, s *Snapshot, r FriendGiveReq, c *Card, seat FriendSeat, haveSeat bool, idx map[string][]*Card, given map[string]int, tops readGenTops, declared *bool) {
	row := FriendRow(r.Friend)
	pr := s.Work.Placed(c.F("primary"))
	attempt := c.Int("attempt")
	switch {
	case !c.Placed():
		p.refuse(c.ID, fmt.Sprintf("%s is retired (%s): no read to give", c.ID, orDash(c.F("retired_by"))))
		return
	case c.Col != Ready && c.Col != Working:
		p.refuse(c.ID, fmt.Sprintf("%s is %s: no open read to give", c.ID, placeOf(c)))
		return
	case c.F("reader") == r.Friend || c.Row == row:
		p.refuse(c.ID, fmt.Sprintf("%s is friend %s's already", c.ID, r.Friend))
		return
	case FriendCardInFlight(c) != "" || readerRunsIt(s, c):
		why := FriendCardInFlight(c)
		if why == "" {
			why = "its reader's beat names it running"
		}
		p.refuse(c.ID, fmt.Sprintf("%s has begun with %s (%s): it finishes there (friend take --working takes it back first)", c.ID, orDash(c.F("reader")), why))
		return
	case pr == nil || pr.Col != Review && !DecisionParkedReview(pr) || readAttempt(pr) != attempt:
		p.refuse(c.ID, fmt.Sprintf("%s reads %s at attempt %d, which is not in review at it: the take-back retires it", c.ID, c.F("primary"), attempt))
		return
	}
	worker := attemptUnit(s, pr.ID, attempt)
	switch {
	case worker == r.Friend:
		p.refuse(c.ID, snapshotFriendPronouns(s, r.Friend).Sprintf("friend %s worked %s at attempt %d: a friend never reads {their} own work", r.Friend, pr.ID, attempt))
		return
	case !haveSeat:
		p.refuse(c.ID, fmt.Sprintf("friend %s has no seat on the friends table (run: nova-sprint friend sync)", r.Friend))
		return
	case !friendHasRole(seat, RoleReader):
		p.refuse(c.ID, snapshotFriendPronouns(s, r.Friend).Sprintf("friend %s's roles do not name %s: no read is given to {their} row", r.Friend, RoleReader))
		return
	case !friendCanRead(s, seat):
		p.refuse(c.ID, fmt.Sprintf("friend %s is %s: a read is given only to a reader up", r.Friend, orDash(seat.Status)))
		return
	}
	u := readUnit{name: r.Friend, row: row, friend: true, seat: seat}
	if why := readRefusal(s, u, pr, attempt, worker, idx[pr.ID]); why != "" {
		p.refuse(c.ID, fmt.Sprintf("friend %s may not read %s at attempt %d: %s", r.Friend, pr.ID, attempt, why))
		return
	}
	_, width := friendRoom(seat)
	work, reads := rowLoad(s, row)
	if friendFreeLanes(width, work, reads+given[r.Friend]) <= 0 {
		p.refuse(c.ID, fmt.Sprintf("friend %s's row is at its width %d (%d live): a read is given only to a free slot", r.Friend, width, work+reads+given[r.Friend]))
		return
	}
	id := readCardIDFor(idx[pr.ID], pr.ID, attempt, r.Friend, readGenTop(s, pr.ID, attempt))
	if id == "" || s.Fleet.Card(id) != nil {
		p.refuse(c.ID, fmt.Sprintf("friend %s's read card of %s at attempt %d cannot be cut (%s)", r.Friend, pr.ID, attempt, orDash(id)))
		return
	}
	tops.cut(s, pr.ID, attempt, r.Friend, id)
	given[r.Friend]++
	fields := map[string]string{
		"kind": "read", "primary": pr.ID, "stream": pr.Row, "reader": r.Friend,
		"attempt": itoa(attempt), "head": c.F("head"), "asked": stamp(s.Now), "gen": "1",
		FieldReadCard: "1", FieldTier: s.readTierOf(pr),
	}
	for _, k := range []string{"branch", "start"} {
		if v := c.F(k); v != "" {
			fields[k] = v
		}
	}
	priorityOnRead(fields, pr)
	if !s.Fleet.HasRow(row) && !*declared {
		p.Rows = append(p.Rows, RowAdd{Fleet, row})
		*declared = true
	}
	reason := r.Reason
	if reason == "" {
		reason = "given by the coordinator"
	}
	p.Units = append(p.Units, Unit{Key: pr.ID, Stream: pr.Row, Changes: []Change{
		change(Fleet, removeEntry(c, map[string]string{"retired": stamp(s.Now), "retired_by": RetiredByCoordinator, "reason": reason})),
		change(Fleet, createEntry(id, row, Working, pr.Score, fields)),
	}, Moved: fmt.Sprintf("%s %s -> %s %s:working (%s)", c.ID, placeOf(c), id, row, reason)})
	p.Said = append(p.Said, c.ID+": handed to "+r.Friend)
}

// readerRunsIt says the friend whose row holds the read card c names it running in her beat.
func readerRunsIt(s *Snapshot, c *Card) bool {
	name, ok := FriendOfRow(c.Row)
	if !ok {
		return false
	}
	f, have := friendSeatOf(s, name)
	return have && friendStarted(s, f, c)
}
