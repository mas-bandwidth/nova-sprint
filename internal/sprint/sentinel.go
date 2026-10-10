package sprint

import (
	"fmt"
)

// The cut sentinel releases itself (docs/roadmap.sexp, auto-sentinels-released-by-tick;
// the owner, 2026-10-10: "what else is manual that should be automatic from the machine").
// A sentinel that names its needs marks a cut: the cards it names are the cut's, and when
// every one of them has landed the gate is readable from the state, so the tick releases
// the sentinel itself -- the resolve that finds it reached releases it at its next pass --
// and pushes the seat that the release is ready to cut. The publish itself stays a gated
// step with a cold read: the push is a notice, not a judgment, and the cut is still the
// seat's. A plain gate (a stop by position alone, no needs named) is a wave the
// coordinator passes on their own look, and its release stays theirs (docs/SPEC-SPRINT.md
// section 16); a held sentinel keeps its hold, and the machine releases through no
// stream's hold. The release lives in the resolve part, the duty the reference model
// already names (refmodel decide.go, DutyResolve), so the tick's shape stays the shape
// the model holds: no part of its own, and nothing the model's walks would have to know
// -- no walk admits a sentinel that names needs.

// NCutReady is the seat push of a resolve's self-release: a happened note addressed to
// the seat (Note.To, which the push loop delivers, as the accept's "ready to merge"
// notice is), that the release is ready to cut.
const NCutReady = "the release is ready to cut"

// cutReleaseReason is what the machine records on a sentinel it released itself
// (release_reason, where the coordinator's release records its --reason).
const cutReleaseReason = "every card it needs has landed"

// isCut says the sentinel names its needs: the stored needs are only what a card names
// itself (docs/SPEC-SPRINT.md section 16), and a sentinel that names them marks a cut the
// machine can read.
func isCut(c *Card) bool { return len(Split(c.F("needs"))) > 0 }

// SentinelReleases is the waiting cut sentinels' self-release, as the pieces of the
// tick's resolve part (steps_tick.go, TickResolve) that land them: every cut sentinel
// marked reached -- the step that landed its last need, or the resolve before this one,
// marked it, so the reached judgment is open on it and this step's landing closes it --
// and waiting on nothing now (WaitsFor empty; a held sentinel waits on release, not on
// its line, and keeps its hold, docs/SPEC-SPRINT.md section 16), is released as the
// coordinator's release lands it (steps_sentinel.go, Release): waiting -> landed, the
// judgments open on it closed, released_by the machine's, never the coordinator's, and
// what waited behind it moved to ready in the same step (resolveAfter), each sentinel's
// Moved naming how many of its wave it let through. It plans the release only: a need
// still in flight (working, in review, merging) is not landed, and the machine waives
// nothing -- the coordinator's release, which waives, stays the way past work under way.
// A sentinel of a held stream is left to the coordinator, whose hand is on the stream
// (hold.go). The release units go first and the waves after them, so the caller's bound
// never cuts a landing from the wave it lets through.
func SentinelReleases(s *Snapshot, who string) (units, after []Unit, notes []Note, landing map[string]bool) {
	var ids []string
	for _, c := range s.Work.Column(Waiting) {
		if !IsSentinel(c) || !isCut(c) || c.F("reached") == "" {
			continue
		}
		if w := WaitOf(s, c); w.Operand != WaitOnLine || len(w.On) > 0 {
			continue // a held sentinel waits on release; a wait unmet stands
		}
		if StreamHeld(s, c.Row) {
			continue // the coordinator's hand is on the stream: the release stays theirs
		}
		ids = append(ids, c.ID)
		if landing == nil {
			landing = map[string]bool{}
		}
		landing[c.ID] = true
	}
	if landing == nil {
		return nil, nil, nil, nil
	}
	after = resolveAfter(s, landing, who)
	moving := map[string]bool{}
	for _, u := range after {
		moving[u.Key] = true
	}
	for _, id := range ids {
		c := s.Work.Card(id)
		ready := 0
		for _, b := range Behind(s, c) {
			if moving[b.ID] {
				ready++
			}
		}
		for _, u := range after {
			if pc := s.Work.Card(u.Key); !IsSentinel(pc) && pc.Row != c.Row && contains(Split(pc.F("needs")), c.ID) {
				ready++ // named it from another stream
			}
		}
		set := map[string]string{"landed": stamp(s.Now), "released": stamp(s.Now), "released_by": who, "release_reason": cutReleaseReason}
		n := happened(NSentinelLanded, c.Row, s.Now, c.ID)
		n.Who = who
		n.What = fmt.Sprintf("sentinel %s landed, released by %s: %d cards are now ready; %s", c.ID, who, ready, cutReleaseReason)
		units = append(units, Unit{Key: c.ID, Stream: c.Row,
			Changes: []Change{change(Work, moveEntry(c, c.Row, Landed, set, FieldHeld))},
			Notes:   []Note{n}, Closes: closesFor(s.Open, nil, c.ID),
			Moved: fmt.Sprintf("sentinel %s waiting -> landed (released by %s: %s); %d cards are now ready", c.ID, who, cutReleaseReason, ready)})
	}
	if n, ok := cutNotice(s, ids, who); ok {
		notes = append(notes, n)
	}
	return units, after, notes, landing
}

// cutNotice is the one seat push of a resolve's self-releases (SentinelReleases), as
// acceptedNotice (steps_tick.go) is the accept's: a happened note addressed to the seat,
// naming every sentinel the resolve released, that the release is ready to cut. It is a
// notice, not a judgment: the publish itself stays a gated step with a cold read, and the
// note commands nothing. ok is false when the resolve released none.
func cutNotice(s *Snapshot, ids []string, who string) (Note, bool) {
	if len(ids) == 0 {
		return Note{}, false
	}
	stream := ""
	for _, id := range ids {
		c := s.Work.Card(id)
		switch {
		case c == nil:
		case stream == "":
			stream = c.Row
		case stream != c.Row:
			stream = ""
		}
	}
	n := happened(NCutReady, stream, s.Now, ids...)
	n.Who, n.To = who, s.Coordinator
	n.What = fmt.Sprintf("%s released itself: every card it needs has landed; the release is ready to cut -- the publish itself stays a gated step with a cold read", Preview(ids, ", "))
	return n, true
}
