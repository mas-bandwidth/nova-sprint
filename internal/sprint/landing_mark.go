package sprint

import "strings"

// FieldLandingHead is the merge card's mark that a lander checked the card's reads and
// committed to landing it at that head: written in one store step with that check, just
// before git push (MarkLanding, the lander's markLanding), so from then on, through the push,
// the report and a crash between them, the tick never sends the card back to review
// (ShortReadsBack) and no lander holds it for its reads (land.go readsWhy, MarkLanding): a
// landing once committed is completed, its reads judged at the check that marked it. A
// rework moves the card to another head, which the mark does not name; a return and a new
// accept at the same head keep it (that head may be on the base already); a clear empties it.
// tla/Land.tla markneed, PushedNeverSentBack and NoLandWithoutReads.
const FieldLandingHead = "landing_head"

// LandingMarked says the merging card id is marked landing at its current head.
func LandingMarked(s *Snapshot, id string) bool {
	if s == nil || s.Work == nil || s.Merge == nil {
		return false
	}
	pr, m := s.Work.Placed(id), s.Merge.Placed(id)
	return pr != nil && m != nil && pr.Col == Merging && m.F(FieldLandingHead) != "" && m.F(FieldLandingHead) == pr.F("head")
}

// MarkLanding is the lander's check and mark before the push, one step: every pinned card
// still merging and queued in stream at the head pinned, and with the reads it needs there
// (ReadsShortWhy) unless it is marked already (a landing a lander committed to before), or
// the whole step is refused naming the first card and why, nothing written; then each card
// is marked landing at its head (FieldLandingHead). tla/Land.tla Check.
func MarkLanding(s *Snapshot, stream string, pins []PushedPin) Plan {
	var p Plan
	p.on(s)
	var changes []Change
	var ids []string
	for _, pin := range pins {
		pr, m := s.Work.Placed(pin.ID), s.Merge.Placed(pin.ID)
		switch {
		case pr == nil || m == nil || pr.Col != Merging || m.Col != Queued || pr.Row != stream || m.Row != stream:
			p.refuse(pin.ID, "not merging and queued in stream "+stream+" (it is "+placeWord(orEmpty(pr, pin.ID))+"); nothing pushed")
			return p
		case pin.Head == "" || pr.F("head") != pin.Head:
			p.refuse(pin.ID, "at head "+orDash(pr.F("head"))+" now, not "+orDash(pin.Head)+" as it was built; nothing pushed")
			return p
		case m.F(FieldLandingHead) == pin.Head:
			continue // committed to already: its reads were judged at that mark
		}
		if why := ReadsShortWhy(s, pr); why != "" && !PushedUnreportedMatches(s, pin.ID) {
			p.refuse(pin.ID, why+" now (the reads setting or its tier raised since land read it); nothing pushed, and the tick sends it back to review for the read it lacks")
			return p
		}
		changes = append(changes, change(Merge, setEntry(m, map[string]string{FieldLandingHead: pin.Head})))
		ids = append(ids, pin.ID)
	}
	if len(changes) == 0 {
		return p
	}
	p.Units = []Unit{{Key: ids[0], Stream: stream, Changes: changes, Moved: "landing " + strings.Join(ids, ", ")}}
	return p
}
