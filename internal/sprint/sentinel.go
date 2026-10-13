package sprint

import (
	"fmt"
)

// Automatic releases are notification-only. The recorded trunk's freshly
// fetched ancestry authorizes live producers; dropped producers are complete.
// Explicit Release remains a separate early override.
const NCutReady = "the release is ready to cut"
const cutReleaseReason = "every card it needs has landed"

func isCut(c *Card) bool { return len(Split(c.F("needs"))) > 0 }

// SentinelReleases implements SentinelRelease.tla AutoRelease. No reached
// judgment, coordinator decision, sentinel hold or stream hold gates release.
func SentinelReleases(s *Snapshot, who string, observations ...SentinelObservation) (units, after []Unit, notes []Note, landing map[string]bool) {
	var ids []string
	reasons := map[string]string{}
	merges := map[string][]DevInclusion{}
	for _, c := range s.Work.Column(Waiting) {
		if !IsSentinel(c) {
			continue
		}
		reason, proof, ok := SentinelDevReason(s, c, observations)
		if !ok {
			continue
		}
		ids = append(ids, c.ID)
		reasons[c.ID], merges[c.ID] = reason, proof
		if landing == nil {
			landing = map[string]bool{}
		}
		landing[c.ID] = true
	}
	if landing == nil {
		return nil, nil, nil, nil
	}
	// Consumers merge the trunk on the next observation before becoming ready.
	after = nil
	for _, id := range ids {
		c := s.Work.Card(id)
		set := map[string]string{"landed": stamp(s.Now), "released": stamp(s.Now), "released_by": who, "release_reason": reasons[id], "release_kind": "automatic"}
		for i, proof := range merges[id] {
			set[fmt.Sprintf("release_merge_%d", i)] = fmt.Sprintf("%s %s %s %s contained in %s", proof.Producer, proof.Repo, proof.Stream, proof.Trunk, proof.Containing)
		}
		n := happened(NSentinelLanded, c.Row, s.Now, c.ID)
		n.Who = who
		n.What = fmt.Sprintf("sentinel %s landed automatically: %s; consumers merge their recorded trunk before the next deal", c.ID, reasons[id])
		for _, proof := range merges[id] {
			n.What += fmt.Sprintf("; %s head %s contained in %s %s at %s", proof.Producer, proof.Head, proof.Repo, proof.Trunk, proof.Containing)
		}
		n.To = s.Coordinator
		units = append(units, Unit{Key: c.ID, Stream: c.Row,
			Changes: []Change{change(Work, moveEntry(c, c.Row, Landed, set, FieldHeld))},
			Notes:   []Note{n}, Closes: closesFor(s.Open, nil, c.ID),
			Moved: fmt.Sprintf("sentinel %s waiting -> landed (automatic: %s)", c.ID, reasons[id])})
	}
	if n, ok := cutNotice(s, ids, who); ok {
		notes = append(notes, n)
	}
	return units, after, notes, landing
}

// cutNotice groups notification-only automatic releases addressed to the seat.
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
	n.What = fmt.Sprintf("%s released itself: automatic release happened; see each sentinel release notice for its observed trunk commit or producer completion reason", Preview(ids, ", "))
	return n, true
}
