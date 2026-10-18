package sprint

import (
	"fmt"
	"strings"
)

// The fleet's and the friends' ok% is the readers' (docs/SPEC-SPRINT.md section 1;
// the owner, 2026-10-04: "trust but VERIFY"; "I want to trust the ok%"; "Are they
// actually doing the work that is shown in the friend table? Really?"). A worker's
// finish, ok or failed, moves its work card to the member's hidden finished cell,
// where it counts in no ok%; a reader's verdict on that attempt moves it on, to ok when
// as many different readers as the primary needs (ReadsNeeded) read it ok, to failed
// when a reader read it broken. Work no reader reads (a failed finish) stays finished.

// readVerdict is the readers' verdict on the work card wc, an attempt of primary pr, by the
// accept's own rules, from the reads that stand on the attempt whichever table they are on
// (standingReads, the function okReaders and hasBrokenAtHead count from), so the verdict and
// the accept cannot disagree: DoneOK when the primary was accepted at this attempt and its
// reads still stand as the lander holds them (ReadsShort), DoneFailed when a broken read
// stands at the attempt's head, DoneOK when as many different readers as the primary needs
// (ReadsNeededIn, the sprint's count while one is set, as the accept counts it) read it ok,
// with the coordinator's heavy read and a script read counted as the accept counts them
// (ReadsStanding, scriptVerified); "" while neither. With no read needed (set --reads 0) the
// worker's own LAND is the verdict, as it is the accept's evidence (acceptable); a failed
// finish no reader reads stays finished. An earlier attempt than the primary's current one is
// judged on its own reads at its work head.
func readVerdict(s *Snapshot, pr, wc *Card) string {
	if pr == nil || wc == nil {
		return ""
	}
	attempt := max(wc.Int("attempt"), 1)
	oks, broken := standingReads(s, pr, attempt)
	need := ReadsNeededIn(s, pr)
	if attempt != max(pr.Int("attempt"), 1) {
		switch {
		case len(broken) > 0:
			return DoneFailed
		case need == 0:
			return landWord(wc)
		case len(oks) >= need:
			return DoneOK
		}
		return ""
	}
	_, _, short := ReadsShort(s, pr)
	accepted := (pr.Col == Merging || pr.Col == Landed) && pr.F("accepted") != ""
	switch {
	case accepted && !short:
		// the accept read these reads (Accept), a heavy read overruling a broken one
		// included, and the lander still holds them standing
		return DoneOK
	case len(broken) > 0:
		return DoneFailed
	case need == 0:
		return landWord(wc)
	case !short:
		return DoneOK
	}
	return ""
}

// landWord is the worker's own word on wc as a verdict where no read is needed: DoneOK for a
// LAND at a head, "" for a failed finish, which no reader reads and which stays finished.
func landWord(wc *Card) string {
	if wc.F("ok") == "yes" && wc.F("head") != "" {
		return DoneOK
	}
	return ""
}

// verdictChange is the move of a work card waiting in finished to the cell of the
// readers' verdict on it (readVerdict), false when there is none yet.
func verdictChange(s *Snapshot, pr, wc *Card) (Change, string, bool) {
	if wc == nil || !wc.Placed() || wc.Col != Finished {
		return Change{}, "", false
	}
	col := readVerdict(s, pr, wc)
	if col == "" {
		return Change{}, "", false
	}
	return change(Fleet, moveEntry(wc, wc.Row, col, nil)), col, true
}

// TickVerdicts is the fleet update's part that counts the readers' verdicts: each work
// card in a member's finished cell whose attempt the readers have read (readVerdict)
// moves to its ok or failed cell, up to TickMaxMoves a tick; the rest are due. A rework
// that retires the read cards moves its work card in the same step (Rework).
func TickVerdicts(s *Snapshot, _ TickReq) (Plan, int) {
	var p Plan
	due := 0
	for _, wc := range s.Fleet.Column(Finished) {
		pr := s.Work.Placed(wc.F("primary"))
		if pr == nil {
			continue
		}
		ch, col, ok := verdictChange(s, pr, wc)
		if !ok {
			continue
		}
		if len(p.Units) >= TickMaxMoves {
			due++
			continue
		}
		p.Units = append(p.Units, Unit{Key: wc.ID, Stream: wc.F("stream"), Changes: []Change{ch},
			Moved: fmt.Sprintf("%s finished -> %s (the readers' verdict)", wc.ID, col)})
	}
	return p, due
}

// TickFriendRedeal is the fleet update's part that redeals a friend's card past its
// deadline unfinished (the deadline WorkDeadline holds every work card to): never a
// failure and never a late judgment. Its work card goes to her redealt cell, where the
// friends table counts it, at its next generation, so her late report finishes nothing;
// its primary goes back to ready, and the next deal (FriendDeal) deals its next attempt
// to a friend up with room. Up to TickMaxMoves a tick; the rest are due.
func TickFriendRedeal(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	due := 0
	for _, wc := range s.Fleet.Column(Ready, Working) {
		if !friendLate(s, r, wc) {
			continue
		}
		if why := laneLiveWhy(s, r, wc); why != "" {
			u, ok := noticeLivePastDeadline(s, r, wc, why)
			if !ok {
				continue
			}
			if len(p.Units) >= TickMaxMoves {
				due++
				continue
			}
			p.Units = append(p.Units, u)
			continue
		}
		if len(p.Units) >= TickMaxMoves {
			due++
			continue
		}
		field, limit, word, _ := WorkDeadline(s, wc)
		set := nextGen(wc, "", s.Now)
		set[Redealt] = stamp(s.Now)
		what := fmt.Sprintf("%s %s at %s, %s past %s: redealt", wc.ID, field, wc.F(field), word, limit)
		u := Unit{Key: wc.ID, Stream: wc.F("stream"), Changes: []Change{change(Fleet, moveEntry(wc, wc.Row, Redealt, set))},
			Moved: fmt.Sprintf("%s %s -> redealt gen=%d, past its deadline", wc.ID, wc.Col, wc.Int("gen")+1)}
		if pr := s.Work.Placed(wc.F("primary")); pr != nil && pr.Col == Working && pr.F("work") == wc.ID {
			u.Changes = append(u.Changes, change(Work, moveEntry(pr, pr.Row, Ready, nil, "work")))
			u.Moved += "; " + pr.ID + " working -> ready"
			n := happened(NFriendRedealt, pr.Row, s.Now, pr.ID)
			n.Who, n.What, n.Card = r.who(), what, wc.ID
			u.Notes = append(u.Notes, n)
		}
		p.Units = append(p.Units, u)
	}
	return p, due
}

// friendLate says wc is a friend's card past its deadline (WorkDeadline) in running time.
func friendLate(s *Snapshot, r TickReq, wc *Card) bool {
	if !IsFriendRow(wc.Row) {
		return false
	}
	field, limit, _, _ := WorkDeadline(s, wc)
	d, ok := r.running(s.Now, wc.F(field))
	return ok && d > limit
}

// FinishedAs is the worker's own word a fleet log line records, DoneOK or DoneFailed,
// when the line is a worker's finish: a move into finished, its word in the ok field the
// finish sets, or, in a log written before the readers' verdicts counted, a move into ok
// or failed from anywhere but finished. A reader's verdict (finished to ok or failed) and
// every other line are "" (docs/SPEC-SPRINT.md section 1).
func FinishedAs(l Line) string {
	if l.Note != nil || l.Table != Fleet || l.Removed {
		return ""
	}
	_, from, _ := strings.Cut(l.From, ":")
	_, to, _ := strings.Cut(l.To, ":")
	switch {
	case to == Finished && l.Set["ok"] == "no":
		return DoneFailed
	case to == Finished:
		return DoneOK
	case (to == DoneOK || to == DoneFailed) && from != Finished:
		return to
	}
	return ""
}

// noticeLivePastDeadline is the one coordinator notice raised the first time a friend's live
// working card passes unfinishedLimit from first_taken in its current generation: the card is
// never moved, and the notice is raised once per generation so the coordinator decides.
// tla/LaneCheck.tla NoticePastDeadline.
func noticeLivePastDeadline(s *Snapshot, r TickReq, wc *Card, why string) (Unit, bool) {
	if wc.Col != Working {
		return Unit{}, false
	}
	field, limit, _, _ := WorkDeadline(s, wc)
	firstTaken := wc.F(field)
	ran, ok := r.running(s.Now, firstTaken)
	if !ok || ran <= limit {
		return Unit{}, false
	}
	gen := wc.F("gen")
	if gen == "" {
		gen = "1"
	}
	if wc.F(FieldDeadlineNoticed) == gen {
		return Unit{}, false
	}
	primary := wc.F("primary")
	if primary == "" {
		primary = wc.ID
	}
	stream := wc.F("stream")
	if pr := s.Work.Placed(primary); pr != nil && pr.Row != "" {
		stream = pr.Row
	}
	what := fmt.Sprintf("%s on %s has run %s since first_taken %s (past %s, %s): live lane kept; the coordinator decides",
		wc.ID, wc.Row, ran, firstTaken, limit, why)
	n := happened(NFriendLivePastDeadline, stream, s.Now, primary)
	n.Who, n.To, n.What, n.Card = r.who(), s.Coordinator, what, wc.ID
	if n.To == "" {
		n.To = "coordinator"
	}
	return Unit{
		Key:     wc.ID,
		Stream:  wc.F("stream"),
		Changes: []Change{change(Fleet, setEntry(wc, map[string]string{FieldDeadlineNoticed: gen}))},
		Notes:   []Note{n},
		Moved:   fmt.Sprintf("%s %s:working gen=%s noticed past %s (%s since first_taken %s, %s)", wc.ID, wc.Row, gen, limit, ran, firstTaken, why),
	}, true
}
