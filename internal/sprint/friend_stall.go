package sprint

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"
)

// The friend stall ladder (docs/SPEC-SPRINT.md section friend-stall-ladder-r.w1).
// The model is tla/StallLadder.tla (with invariants NoCardHeldPastBound, NoStartedRedealt,
// ReleasedOnlyByActivity, which is ReleasedOnlyByEvidence and NeverDownWhileWorking).
//
// A friend holding dealt cards is stalled when no evidence of her work is newer than
// friend_stall_after (default 20m). Evidence is everything the store already has
// (friendEvidence): session activity (FriendReport.Active), a beat whose running list is
// non-empty, a finish or a report collected for her (a card of her row stamped finished or
// reported), a read she recorded, and a progress stamp (FieldProgress). Awake is not
// working: a beat that names nothing running is no evidence.
// While stalled, the ladder climbs one rung per friend_stall_step (default 5m):
//   (1) Wake turn 1: bus message to her pushed into daemon as a turn.
//   (2) Wake turn 2: second wake bus message.
//   (3) Coordinator note: pushed judgment ("friend <f> stalled <d>: two wakes unanswered").
//   (4) Unstarted cards taken back: FriendTake with All: true (started cards stay and finish).
//   (5) Friend marked down with reason "stalled", released to up by the tick itself at her
//       first evidence of work after it.
//
// Every rung emits a happened note (Kind: Happened), which says what the rung did: the
// part's units are only the cards and rows it changes. Any evidence of work resets her to
// rung 0.

// NFriendStall is the happened notification type for stall ladder climbing and clearing.
const NFriendStall = "friend stall"

// PropFriendStallRung is the fleet property recording the friend's current stall ladder rung
// (0..5). A property name is letters, digits, _ . and - (the table store refuses any other),
// so the friend's name follows a dot.
func PropFriendStallRung(friend string) string { return "friend_stall_rung." + friend }

// PropFriendStallDown is the fleet property recording when the friend was marked down for stall.
func PropFriendStallDown(friend string) string { return "friend_stall_down." + friend }

// TickFriendStall is the friend stall part of the tick (PartFriendStall): it runs in the
// fleet update pass, checks each friend holding cards against the stall bounds, climbs
// the ladder when stalled, takes back unstarted cards at rung 4, marks her down at rung 5,
// and releases her to up at her first evidence of work after going down.
func TickFriendStall(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	if s.Fleet == nil || s.Work == nil {
		return p, 0
	}

	propsWritten := map[string]bool{}
	write := func(name, value string) {
		if propsWritten[name] {
			return
		}
		was, had := s.Fleet.Prop(name)
		if (!had && value == "") || (had && was == value) {
			return
		}
		propsWritten[name] = true
		p.Props = append(p.Props, PropWrite{Table: Fleet, Name: name, Value: value, Was: was, WasAbsent: !had})
	}

	friendsSet := map[string]bool{}
	for _, f := range s.Friends {
		friendsSet[f.Name] = true
	}
	for _, f := range r.Friends {
		friendsSet[f.Name] = true
	}
	for _, row := range s.Fleet.Rows() {
		if f, ok := FriendOfRow(row); ok {
			friendsSet[f] = true
		}
	}
	friends := slices.Sorted(maps.Keys(friendsSet))

	stallAfter := s.FriendStallAfter()
	stallStep := s.FriendStallStep()
	var cardEvidence map[string]evidence
	if len(friends) > 0 {
		cardEvidence = friendCardEvidence(s)
	}

	for _, f := range friends {
		row := FriendRow(f)
		mine := append(append([]*Card(nil), s.Fleet.Cell(row, Ready)...), s.Fleet.Cell(row, Working)...)

		// 1. Evidence of work: session activity, a running beat, a finish, a read, progress
		last := friendEvidence(s, r, f, row, cardEvidence[f])

		// Check if she is marked stall down
		downStamp, _ := s.Fleet.Prop(PropFriendStallDown(f))
		isStallDown := downStamp != ""
		var downTime time.Time
		if isStallDown {
			downTime, _ = time.Parse(time.RFC3339, downStamp)
		}

		curRungStr, _ := s.Fleet.Prop(PropFriendStallRung(f))
		curRung, _ := strconv.Atoi(curRungStr)

		// ReleasedOnlyByActivity (tla/StallLadder.tla, ReleasedOnlyByEvidence): release from
		// stall down occurs when evidence of work newer than the down is observed, and only then.
		if isStallDown && !last.at.IsZero() && (downTime.IsZero() || !last.at.Before(downTime)) {
			write(PropFriendStallDown(f), "")
			write(PropFriendStallRung(f), "")
			curRung = 0
			gen := max(s.SeatGeneration, FirstSeatGeneration)
			if p.Health == nil {
				p.Health = &FriendHealthWrite{
					Friend: f,
					Health: FriendHealth{
						State:      Up,
						Seen:       s.Now,
						Generation: gen,
					},
				}
			}
			if ctl := s.MemberCtl(row); ctl != nil {
				p.Units = append(p.Units, Unit{
					Key: ctl.ID,
					Changes: []Change{
						change(Fleet, setEntry(ctl, map[string]string{
							"status": Up,
							"since":  stamp(s.Now),
						})),
					},
					Moved: fmt.Sprintf("friend %s up: released by %s", f, last.by),
				})
			}
			hn := happened(NFriendStall, "", s.Now)
			hn.Who, hn.To = r.who(), s.Coordinator
			hn.What = fmt.Sprintf("friend %s released to up: %s at %s", f, last.by, last.at.UTC().Format(time.RFC3339))
			p.Notes = append(p.Notes, hn)
			isStallDown = false
		}

		// A friend holding no cards cannot be stalled
		if len(mine) == 0 {
			if curRung > 0 {
				write(PropFriendStallRung(f), "")
			}
			continue
		}

		// 2. Card dealt / taken timestamp: the ladder's start, never a release
		var cardDealt time.Time
		for _, c := range mine {
			for _, k := range []string{"dealt", "first_dealt", "taken", "first_taken"} {
				if dStr := c.F(k); dStr != "" {
					if t, err := time.Parse(time.RFC3339, dStr); err == nil && t.After(cardDealt) {
						cardDealt = t
					}
				}
			}
		}

		lastSignOfLife := last.at
		if cardDealt.After(lastSignOfLife) {
			lastSignOfLife = cardDealt
		}
		if lastSignOfLife.IsZero() {
			lastSignOfLife = s.Now
		}

		idleDuration := s.Now.Sub(lastSignOfLife)
		if r.Stopped != nil && !lastSignOfLife.IsZero() {
			idleDuration -= r.Stopped(lastSignOfLife, s.Now)
		}
		if idleDuration < 0 {
			idleDuration = 0
		}

		var targetRung int
		switch {
		case idleDuration < stallAfter:
			targetRung = 0
		case idleDuration < stallAfter+stallStep:
			targetRung = 1
		case idleDuration < stallAfter+2*stallStep:
			targetRung = 2
		case idleDuration < stallAfter+3*stallStep:
			targetRung = 3
		case idleDuration < stallAfter+4*stallStep:
			targetRung = 4
		default:
			targetRung = 5
		}

		switch {
		case targetRung == 0:
			if curRung > 0 {
				write(PropFriendStallRung(f), "")
				hn := happened(NFriendStall, "", s.Now)
				hn.Who, hn.To = r.who(), s.Coordinator
				hn.What = fmt.Sprintf("friend %s stall reset to rung 0", f)
				p.Notes = append(p.Notes, hn)
			}
		case targetRung > curRung:
			for nextRung := curRung + 1; nextRung <= targetRung; nextRung++ {
				switch nextRung {
				case 1:
					if r.WakeFriend != nil {
						_ = r.WakeFriend(f, 1, idleDuration) // ignored: the wake message is best effort; the rung climbs regardless
					}
					hn := happened(NFriendStall, "", s.Now)
					hn.Who, hn.To = r.who(), s.Coordinator
					hn.What = fmt.Sprintf("friend %s stalled %s: wake turn 1", f, idleDuration.Round(time.Second))
					p.Notes = append(p.Notes, hn)
				case 2:
					if r.WakeFriend != nil {
						_ = r.WakeFriend(f, 2, idleDuration) // ignored: the wake message is best effort; the rung climbs regardless
					}
					hn := happened(NFriendStall, "", s.Now)
					hn.Who, hn.To = r.who(), s.Coordinator
					hn.What = fmt.Sprintf("friend %s stalled %s: wake turn 2", f, idleDuration.Round(time.Second))
					p.Notes = append(p.Notes, hn)
				case 3:
					jn := judgment(NStalled, "", s.Now, 0, f)
					jn.Who, jn.To = r.who(), s.Coordinator
					jn.What = fmt.Sprintf("friend %s stalled %s: two wakes unanswered", f, idleDuration.Round(time.Second))
					jn.Decisions = []string{
						"friend take " + f + " --all-unstarted",
						"friend down " + f + " --reason 'stalled'",
					}
					p.Notes = append(p.Notes, jn)
					hn := happened(NFriendStall, "", s.Now)
					hn.Who, hn.To = r.who(), s.Coordinator
					hn.What = fmt.Sprintf("friend %s stalled %s: coordinator note", f, idleDuration.Round(time.Second))
					p.Notes = append(p.Notes, hn)
				case 4:
					started := map[string]string{}
					if r.Beats != nil {
						if b, ok := r.Beats[f]; ok && b.Friend != nil {
							for _, run := range b.Friend.Running {
								started[run] = "her beat names it running"
							}
						} else if b, ok := r.Beats[row]; ok && b.Friend != nil {
							for _, run := range b.Friend.Running {
								started[run] = "her beat names it running"
							}
						}
					}
					for _, c := range mine {
						if c.F(FieldProgress) != "" {
							started[c.ID] = "progress was stamped on it"
						}
					}
					takePlan := FriendTake(s, FriendTakeReq{
						Friend:  f,
						All:     true,
						Reason:  "stalled",
						Who:     r.who(),
						Started: started,
					})
					p.Units = append(p.Units, takePlan.Units...)
					p.Refused = append(p.Refused, takePlan.Refused...)
					hn := happened(NFriendStall, "", s.Now)
					hn.Who, hn.To = r.who(), s.Coordinator
					hn.What = fmt.Sprintf("friend %s stalled %s: unstarted cards taken back", f, idleDuration.Round(time.Second))
					p.Notes = append(p.Notes, hn)
				case 5:
					gen := max(s.SeatGeneration, FirstSeatGeneration)
					if p.Health == nil {
						p.Health = &FriendHealthWrite{
							Friend: f,
							Health: FriendHealth{
								State:      Down,
								Reason:     "stalled",
								Seen:       s.Now,
								Generation: gen,
							},
						}
					}
					write(PropFriendStallDown(f), stamp(s.Now))
					if ctl := s.MemberCtl(row); ctl != nil {
						p.Units = append(p.Units, Unit{
							Key: ctl.ID,
							Changes: []Change{
								change(Fleet, setEntry(ctl, map[string]string{
									"status": Down,
									"since":  stamp(s.Now),
								})),
							},
							Moved: fmt.Sprintf("friend %s down: stalled", f),
						})
					}
					hn := happened(NFriendStall, "", s.Now)
					hn.Who, hn.To = r.who(), s.Coordinator
					hn.What = fmt.Sprintf("friend %s stalled %s: marked down (reason stalled)", f, idleDuration.Round(time.Second))
					p.Notes = append(p.Notes, hn)
				}
			}
			write(PropFriendStallRung(f), strconv.Itoa(targetRung))
		}
	}

	return p, 0
}

// evidence is the newest evidence of a friend's work and what it was ("" and zero: none).
type evidence struct {
	at time.Time
	by string
}

// newer keeps the later of e and (at, by).
func (e *evidence) newer(at time.Time, by string) {
	if !at.IsZero() && at.After(e.at) {
		e.at, e.by = at, by
	}
}

// friendCardEvidence is each friend's newest evidence of work on the fleet's cards, placed
// or kept, in one pass: a finish or a report collected for her (a card of her row stamped
// finished or reported), a progress stamp on a card of her row, and a read she recorded (a
// read card naming her its reader, stamped read; FriendReadClose retires it off her row).
func friendCardEvidence(s *Snapshot) map[string]evidence {
	out := map[string]evidence{}
	for _, c := range s.Fleet.Cards() {
		if f, ok := FriendOfRow(c.Row); ok {
			e := out[f]
			e.newer(stampAt(c, "finished"), "a finish")
			e.newer(stampAt(c, FieldReported), "a finish")
			e.newer(stampAt(c, FieldProgress), "a progress stamp")
			out[f] = e
		}
		if f := c.F("reader"); f != "" && c.F("kind") == "read" {
			e := out[f]
			e.newer(stampAt(c, "read"), "a read")
			out[f] = e
		}
	}
	return out
}

// friendEvidence is the friend's newest evidence of work: the evidence on the cards
// (friendCardEvidence), her session activity (her beat's Active, her control card's
// active, her row's Active text), and her beat when its running list is non-empty, at
// the beat's time. A beat that names nothing running is no evidence: awake is not working.
func friendEvidence(s *Snapshot, r TickReq, f, row string, cards evidence) evidence {
	e := cards
	if r.Beats != nil {
		b, ok := r.Beats[f]
		if !ok || b.Friend == nil {
			b, ok = r.Beats[row]
		}
		if ok && b.Friend != nil {
			e.newer(b.Friend.Active, "session activity")
			if len(b.Friend.Running) > 0 {
				e.newer(b.At, "a running beat")
			}
		}
	}
	if ctl := s.MemberCtl(row); ctl != nil {
		e.newer(stampAt(ctl, "active"), "session activity")
	}
	if s.Fleet.Texts != nil {
		if t, err := time.Parse(time.RFC3339, s.Fleet.Texts[row][Active]); err == nil {
			e.newer(t, "session activity")
		}
	}
	return e
}
