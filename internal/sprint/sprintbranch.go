package sprint

import (
	"slices"
	"strings"
)

// LandingFlow is the landing flow: stream branches hold cards in a batch, then
// merge serially into dev; a card never lands on dev or main (docs/SPEC-SPRINT.md
// section 7, the sprint branch).
const LandingFlow = "The landing flow: card heads land in a batch on their stream's branch sprint/<stream>, oldest first, one writer per stream; stream branches merge one at a time into dev (main where a repository has no dev); a card never lands on dev or main."

// Every stream lands on the sprint branch, and promotion alone reaches dev
// (docs/SPEC-SPRINT.md section 7, the sprint branch): a card cut on dev is merged by
// the lander straight onto it and ejects the merge queue's promotion run, so add admits
// a card based on dev only into the promotion stream, and the lander refuses a landing
// on dev outside it (ProtectedLandWhy, the same mark). Once the coordinator records the
// sprint's base (set --base, PropSprintBase), add and brief hold every card to it: a
// card cut on any other branch, a temporary or a personal one, is admitted only into the
// promotion stream (found 2026-10-04, when cards cut on such branches landed there and
// were folded back onto the base by hand through 17 conflicts).

// DevBranch is the development branch, which promotion alone reaches.
const DevBranch = "dev"

// PropSprintBase is the work table's property naming the sprint's base, the branch every
// stream lands on, written by `nova-sprint set --base <branch>` (WithSprintBase) and taken
// off by `--base default`.
const PropSprintBase = "sprint_base"

// SprintBase is the sprint's base the coordinator recorded, "" when none is (or it was
// taken off: the property then holds default).
func SprintBase(s *Snapshot) string {
	if v, _ := s.Work.Prop(PropSprintBase); v != ReadTierDefault {
		return v
	}
	return ""
}

// StreamBranch is the branch a stream's cards land on, as its control card records it
// (FieldStreamBranch): sprint/<stream> when it records none (docs/SPEC-SPRINT.md section 7,
// the sprint branch).
func StreamBranch(s *Snapshot, stream string) string {
	if b := s.StreamCtl(stream).F(FieldStreamBranch); b != "" {
		return b
	}
	return "sprint/" + stream
}

// Trunk is the trunk a stream's branch is cut from, as its control card records it
// (FieldTrunk): dev, or main where the repository has no dev, dev when it records none
// (docs/SPEC-SPRINT.md section 7, the sprint branch).
func Trunk(s *Snapshot, stream string) string {
	if t := s.StreamCtl(stream).F(FieldTrunk); t != "" {
		return t
	}
	return DevBranch
}

// branchAndTrunk is a stream's branch and trunk from its control card's recorded fields
// and an add's cards' bases: the trunk is the protected branch a card names (main when any
// names main, else dev), and the branch is the recorded one, or sprint/<stream> when none
// is, or a non-protected branch a card names, which the stream keeps (docs/SPEC-SPRINT.md
// section 7, the sprint branch).
func branchAndTrunk(ctl *Card, stream string, bases []string) (branch, trunk string) {
	branch, trunk = "sprint/"+stream, DevBranch
	if ctl != nil {
		if b := ctl.F(FieldStreamBranch); b != "" {
			branch = b
		}
		if t := ctl.F(FieldTrunk); t != "" {
			trunk = t
		}
	}
	for _, b := range bases {
		switch {
		case b == "main":
			trunk = "main"
		case b == DevBranch:
		case b != "":
			branch = b // a non-protected branch: the stream keeps it as its branch
		}
	}
	return branch, trunk
}

// IsPromotionStream says the stream is the promotion stream: its control card carries
// the mark FieldLandProtected, which only the coordinator's `nova-sprint stream set <s>
// --land-protected <owner/name,...|any>` writes (Set) and `--land-protected default`
// takes off (docs/SPEC-SPRINT.md section 7, the sprint branch).
func IsPromotionStream(s *Snapshot, stream string) bool {
	return s.StreamCtl(stream).F(FieldLandProtected) != ""
}

// SprintBranchWhy is why add or brief refuses card into stream, "" when it may: a card cut
// on a protected branch (dev or main) is admitted and re-cut on the stream's branch, and a
// card naming no BASE, the sprint's base, or in the promotion stream is admitted as it is
// (docs/SPEC-SPRINT.md section 7, the sprint branch). Only a card cut on a non-protected
// branch other than the sprint's base is refused, and only once the coordinator recorded
// the sprint's base (set --base); with none recorded, every branch is admitted. The remedy
// re-cuts the card on the sprint's base, or marks the stream.
func SprintBranchWhy(s *Snapshot, stream, base, card string) string {
	sprintBase := SprintBase(s)
	if base == "" || slices.Contains(ProtectedBranches, base) || base == sprintBase || IsPromotionStream(s, stream) {
		return ""
	}
	if sprintBase == "" {
		return ""
	}
	mark := ", or, for the promotion stream, run: nova-sprint stream set " + stream + " --land-protected <owner/name,...|any>"
	return "card " + card + " is cut on " + base + ", not the sprint's base " + sprintBase + ", and stream " + stream + " is not the promotion stream: every stream lands on its branch, and promotion alone reaches " + DevBranch +
		"; nothing was written; re-cut the card with BASE: " + sprintBase + mark
}

// WithSprintBase is the set step's plan with the sprint's base written too: a branch
// name, or default to take it off; dev and main are refused, since promotion alone
// reaches them. The coordinator's alone, as every setting. An empty value, or a plan
// refused for any reason but that it had nothing else to set, is returned as it is.
func WithSprintBase(p Plan, s *Snapshot, v string) Plan {
	if v == "" {
		return p
	}
	for _, x := range p.Refused {
		if !strings.HasPrefix(x.Why, "nothing to set") {
			return p
		}
	}
	p.Refused = nil
	if v != ReadTierDefault && (slices.Contains(ProtectedBranches, v) || strings.HasPrefix(v, "-") || strings.ContainsAny(v, " \t@:")) {
		return Plan{Refused: []Refusal{{Key: "set", Why: "--base wants the sprint's base branch, the branch every stream lands on (sprint/<name>), never " +
			strings.Join(ProtectedBranches, " or ") + ", which promotion alone reaches, or " + ReadTierDefault + " to take it off; found " + v}}}
	}
	word := v
	if v == ReadTierDefault {
		word = "default (none: add holds dev alone)"
	}
	was, had := s.Work.Prop(PropSprintBase)
	p.Props = append(p.Props, PropWrite{Table: Work, Name: PropSprintBase, Value: v, Was: was, WasAbsent: !had})
	for i := range p.Units {
		if p.Units[i].Key == "set" {
			if strings.TrimSpace(p.Units[i].Moved) == "sprint" {
				p.Units[i].Moved = "sprint base " + word
			} else {
				p.Units[i].Moved += ", base " + word
			}
			return p
		}
	}
	p.Units = append(p.Units, Unit{Key: "set", Moved: "sprint base " + word})
	return p
}

// WithBriefBases is the brief step's plan with every new BASE held to SprintBranchWhy: a
// brief that changes a card's BASE (baseOf reads a brief's) to one add would refuse in
// its stream is refused, naming it, and its change is not written; the step applies all
// or none (docs/SPEC-SPRINT.md section 7, the sprint branch).
func WithBriefBases(p Plan, s *Snapshot, r BriefReq, baseOf func(brief string) string) Plan {
	cards := r.Cards
	if len(cards) == 0 && r.Tier == "" {
		cards = []BriefCard{{ID: r.ID, Brief: r.Brief}}
	}
	for _, b := range cards {
		c := s.Work.Placed(b.ID)
		if c == nil {
			continue // Brief names it
		}
		base := baseOf(b.Brief)
		if base == baseOf(c.F("brief")) {
			continue
		}
		if why := SprintBranchWhy(s, c.Row, base, b.ID); why != "" {
			p.Units = slices.DeleteFunc(p.Units, func(u Unit) bool { return u.Key == b.ID })
			p.refuse(b.ID, why)
		}
	}
	return p
}
