package sprint

import (
	"slices"
	"strings"
)

// Shape is what a sprint keeps across a clear: its streams, its readers and
// the tiers they read, and its fleet members with their status and the
// coordinator's holds. The tables, their columns and the view are the table
// layer's and carry across by themselves.
type Shape struct {
	Streams []string          `json:"streams"`
	Readers []string          `json:"readers"`
	Members []string          `json:"members"`
	Status  map[string]string `json:"status"` // member -> up or down
	Held    []string          `json:"held,omitempty"`
	// HeldBy is who made a member's hold when it was not the coordinator
	// (held_by, fleet_sync.go).
	HeldBy map[string]string `json:"held_by,omitempty"`
	// HeldFields is each held member's hold beyond held and held_by: its cause, its
	// reason, who made it (HeldCarried, hold_cause.go). A clear carries them, so a hold
	// never crosses a clear without its cause (the cold read of #206: an idle-reap hold
	// crossed with held_by alone and nothing ever ended or reported it).
	HeldFields map[string]map[string]string `json:"held_fields,omitempty"`
	// Width is unused. A clear carries no width (docs/SPEC-SPRINT.md section 5;
	// tla/Level.tla Width): a width is nova-config's machine:<m> hash, read
	// again on the next tick. The field stays so an old shape still decodes.
	Width map[string]string `json:"width,omitempty"`
	// ReaderTiers is each reader's tiers cell (reader_tiers.go): a reader
	// with the default (an empty cell) is not in it.
	ReaderTiers map[string]string `json:"reader_tiers,omitempty"`
}

// MemberReaderRows is the reader row of every up fleet member the readers
// table lacks: reader-<member> for each member up and not held. Init, a
// member's join and a clear make these rows when missing, so every up
// member is a reader with no seat action (read_cards.go memberReads;
// tla/ReaderTiers.tla EveryUpMemberIsAReader).
func MemberReaderRows(s *Snapshot) []string {
	if s == nil || s.Readers == nil {
		return nil
	}
	var out []string
	for _, m := range s.UpMembers() {
		if rd := ReaderPrefix + m; !s.Readers.HasRow(rd) {
			out = append(out, rd)
		}
	}
	return out
}

// ShapeOf is a snapshot's shape.
func ShapeOf(s *Snapshot) Shape {
	sh := Shape{Readers: append([]string(nil), s.Readers.Rows()...), Members: s.Members(), Status: map[string]string{}}
	// a clear keeps every up member a reader: a member reader row the old
	// epoch lacked is created by the clear, as init and a join create it
	// (tla/ReaderTiers.tla Clear, EveryUpMemberIsAReader)
	sh.Readers = append(sh.Readers, MemberReaderRows(s)...)
	for _, r := range append(append([]string(nil), s.Work.Rows()...), s.Merge.Rows()...) {
		if !contains(sh.Streams, r) {
			sh.Streams = append(sh.Streams, r)
		}
	}
	for _, m := range sh.Members {
		st := s.MemberCtl(m).F("status")
		if st == "" {
			st = Down
		}
		sh.Status[m] = st
		if s.MemberCtl(m).F("held") != "" {
			sh.Held = append(sh.Held, m)
			if by := s.MemberCtl(m).F(FieldHeldBy); by != "" {
				if sh.HeldBy == nil {
					sh.HeldBy = map[string]string{}
				}
				sh.HeldBy[m] = by
			}
			for _, f := range HeldCarried {
				if v := s.MemberCtl(m).F(f); v != "" {
					if sh.HeldFields == nil {
						sh.HeldFields = map[string]map[string]string{}
					}
					if sh.HeldFields[m] == nil {
						sh.HeldFields[m] = map[string]string{}
					}
					sh.HeldFields[m][f] = v
				}
			}
		}
	}
	for _, rd := range sh.Readers {
		if tiers := s.readerTiersStored(rd); strings.TrimSpace(tiers) != "" {
			if sh.ReaderTiers == nil {
				sh.ReaderTiers = map[string]string{}
			}
			sh.ReaderTiers[rd] = tiers
		}
	}
	return sh
}

// RestoreShape is the plan that gives a new epoch its control cards: each
// stream waiting, each member with its status and no work counted. A control
// card the epoch has already is left as it is.
func RestoreShape(s *Snapshot, sh Shape) Plan {
	var p Plan
	now := stamp(s.Now)
	for _, st := range sh.Streams {
		if s.Merge.Card(CtlID(st)) != nil {
			continue
		}
		p.Units = append(p.Units, Unit{Key: CtlID(st), Stream: st, Changes: []Change{change(Merge, createEntry(CtlID(st), st, Ctl, 0,
			map[string]string{"kind": "stream", "state": StreamWaiting}))}, Moved: "stream " + st + " waiting"})
	}
	for _, m := range sh.Members {
		if s.Fleet.Card(CtlID(m)) != nil {
			continue
		}
		fields := map[string]string{"kind": "member", "status": sh.Status[m], "since": now}
		if contains(sh.Held, m) {
			fields["held"] = now
			if by := sh.HeldBy[m]; by != "" {
				fields[FieldHeldBy] = by
			}
			for f, v := range sh.HeldFields[m] {
				if slices.Contains(HeldCarried, f) {
					fields[f] = v
				}
			}
		}
		p.Units = append(p.Units, Unit{Key: CtlID(m), Changes: []Change{change(Fleet, createEntry(CtlID(m), m, Ctl, 0, fields))}, Moved: "member " + m + " " + sh.Status[m]})
	}
	return p
}
