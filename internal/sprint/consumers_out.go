package sprint

import "strings"

// ConsumersOut is the tick's postcondition (docs/SPEC-SPRINT.md, "The tick
// postconditions"): after every tick, every producer card (working, review)
// has its consumer out. A missing consumer is a stall, and it raises one
// judgment naming the card and what is missing, at that tick.
//
//   - For a working card: a live attempt on a member at its current generation.
//   - For a review card: a read out for it (by card, never "any read"), or an
//     open judgment that names it and is refused wait (a parked card is already
//     the coordinator's).
//
// The check reads the card's own consumer record too (CheckConsumers,
// SPEC-SPRINT "the consumer record"): a card whose newest consumer has ended
// and no newer one is out is named with its record, so the stall says who ran
// what, when and how it ended, not only that the table is missing a card.
//
// It returns one LaneQuiet per card whose consumer is missing. The tick pushes
// each once into its result's quiet judgments.
func ConsumersOut(s *Snapshot) []LaneQuiet {
	if s == nil || s.Work == nil {
		return nil
	}
	// The record the producer/consumer check reads, by card: a card whose
	// newest consumer has ended and no newer one is out.
	fromRecord := map[string]string{}
	for _, v := range CheckConsumers(s) {
		if f := strings.Fields(v.Detail); len(f) > 0 {
			fromRecord[f[0]] = v.Detail
		}
	}
	var out []LaneQuiet
	for _, c := range s.Work.Cards() {
		switch c.Col {
		case Working:
			if q, missing := workingConsumerOut(s, c); missing {
				out = append(out, withRecordWhy(c, q, fromRecord))
			}
		case Review:
			if q, missing := reviewConsumerOut(s, c); missing {
				out = append(out, withRecordWhy(c, q, fromRecord))
			}
		}
	}
	return out
}

// withRecordWhy names the card's consumer record when the record shows the
// newest consumer ended (CheckConsumers): the push carries the history, not
// only the missing table's line.
func withRecordWhy(c *Card, q LaneQuiet, fromRecord map[string]string) LaneQuiet {
	if why, ok := fromRecord[c.ID]; ok {
		q.Why = why
	}
	return q
}

// workingConsumerOut is the one quiet of a working primary whose attempt is
// not live on an up member or friend, and whether it is missing.
func workingConsumerOut(s *Snapshot, c *Card) (LaneQuiet, bool) {
	wc := s.Fleet.Placed(c.F("work"))
	switch {
	case c.F("work") == "" || wc == nil:
		return LaneQuiet{Type: "consumers-out", Subject: c.ID, Why: "no live attempt"}, true
	case wc.Col != Ready && wc.Col != Working:
		return LaneQuiet{Type: "consumers-out", Subject: c.ID, Why: "its attempt is " + wc.Col}, true
	}
	if name, ok := FriendOfRow(wc.Row); ok {
		// held by a friend: the friend must be up (her row, never a machine's)
		if !friendUp(s, name) {
			return LaneQuiet{Type: "consumers-out", Subject: c.ID, Why: "friend " + name + " is down"}, true
		}
		return LaneQuiet{}, false
	}
	if s.MemberCtl(wc.Row).F("status") != Up {
		return LaneQuiet{Type: "consumers-out", Subject: c.ID, Why: "member " + wc.Row + " is down"}, true
	}
	return LaneQuiet{}, false
}

// friendUp says the friend is up among the tick's friend seats: a card on her
// row is held by her only while she is up.
func friendUp(s *Snapshot, name string) bool {
	for _, f := range s.Friends {
		if f.Name == name {
			return f.Status == Up
		}
	}
	return false
}

// reviewConsumerOut is the one quiet of a review primary with no read out for
// it and no open judgment that names it, and whether it is missing.
func reviewConsumerOut(s *Snapshot, c *Card) (LaneQuiet, bool) {
	if readOutFor(s, c.ID) || heldByJudgment(s, c.ID) {
		return LaneQuiet{}, false
	}
	return LaneQuiet{Type: "consumers-out", Subject: c.ID, Why: "no read asked for it"}, true
}

// readOutFor says a read card in asked or reading names the primary: a read
// out for this card, never "any read".
func readOutFor(s *Snapshot, id string) bool {
	if s.Readers == nil {
		return false
	}
	for _, rc := range s.Readers.Cards() {
		if rc.F("primary") == id && (rc.Col == Asked || rc.Col == Reading) {
			return true
		}
	}
	return false
}

// heldByJudgment says an open judgment names the card and is refused wait: the
// card is parked, already the coordinator's, never a silent stall.
func heldByJudgment(s *Snapshot, id string) bool {
	for _, o := range s.Open {
		if o.Note.Kind == Judgment && !o.Note.StreamLevel && o.Subject() == id && WaitRefusal(o.Note) != "" {
			return true
		}
	}
	return false
}
