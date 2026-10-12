package sprint

import (
	"slices"
	"strings"
	"time"
)

// FieldFacts is a fleet member's control card field: the gate facts its machine carries,
// comma-separated (docs/SPEC-SPRINT.md, the gate-host rule): "linux" and "darwin" the OS,
// "landlock" the kernel sandbox, "tla" the TLA jar (/opt/tla/tla2tools.jar), and a
// toolchain name the fleet's toolchain facts carry by name (pkg/swarm toolchain.go). The
// deal holds a card's GATE-NEEDS (GateNeeds) against these.
const FieldFacts = "facts"

// MemberFacts reads a member's gate facts from its control card (FieldFacts), Split into
// its comma list; nil when the member has no control card or names none.
func (s *Snapshot) MemberFacts(member string) []string {
	if s == nil {
		return nil
	}
	return Split(s.MemberCtl(member).F(FieldFacts))
}

// GateHostPool narrows ms to the members whose facts satisfy every need of the card's
// GATE-NEEDS line (GateNeeds). A card with no GATE-NEEDS keeps every member of ms, so a
// card that names no gate need is dealt as before. why is "" while at least one member
// satisfies; when none does it names the need the members lack and the facts each member
// carries, so the deal raises one judgment naming the need (never a silent wait).
func (s *Snapshot) GateHostPool(c *Card, ms []string) (out []string, why string) {
	if c == nil {
		return ms, ""
	}
	needs := GateNeeds(c.F("brief"))
	if len(needs) == 0 {
		return ms, ""
	}
	for _, m := range ms {
		if memberSatisfies(s.MemberFacts(m), needs) {
			out = append(out, m)
		}
	}
	if len(out) == 0 && len(ms) > 0 {
		return nil, gateNeedsWhy(needs, ms, s)
	}
	return out, ""
}

// memberSatisfies says every need is among the member's facts.
func memberSatisfies(facts, needs []string) bool {
	for _, n := range needs {
		if !slices.Contains(facts, n) {
			return false
		}
	}
	return true
}

// gateNeedsWhy is the sentence for a card no member satisfies: the first need none
// carries, and the members the deal looked at with their facts.
func gateNeedsWhy(needs []string, ms []string, s *Snapshot) string {
	var members []string
	for _, m := range ms {
		members = append(members, m+" (facts: "+strings.Join(s.MemberFacts(m), ",")+")")
	}
	return "no member up satisfies GATE-NEEDS " + needs[0] + " (" + strings.Join(members, "; ") + ")"
}

// NGateNeeds is the deal's judgment that no member up satisfies a card's GATE-NEEDS line
// (docs/SPEC-SPRINT.md, the gate-host rule): the card is never dealt, and the judgment names
// the need at once, never a silent wait.
const NGateNeeds = "no member up satisfies a card's GATE-NEEDS"

// init registers the gate-host judgment in the two tables that live beside the tick and the
// wait verb (steps_tick.go's TickDecisions, wait_refused.go's WaitRefused), which this card
// does not own: its decisions and the action wait names instead, so the coordinator can answer
// it by brief, look or drop, and a wait can never park it.
func init() {
	TickDecisions[NGateNeeds] = []string{"brief", "look at the card", "drop"}
	WaitRefused[NGateNeeds] = "brief the card with a GATE-NEEDS line a member up satisfies, bring up a member whose facts carry the need, or drop it"
}

// gateNeedsJudgment is the one judgment the deal raises for a card no member up satisfies its
// GATE-NEEDS (why, gateNeedsWhy, names the need): the card stays ready, never dealt, and the
// coordinator answers it at once, never a silent wait.
func gateNeedsJudgment(c *Card, what, who string, now time.Time) Note {
	if who == "" {
		who = MachineActor
	}
	return Note{Kind: Judgment, Type: NGateNeeds, Stream: c.Row, Primaries: []string{c.ID}, Count: 1,
		At: now, Who: who, Marked: true, What: what,
		Decisions: append([]string(nil), TickDecisions[NGateNeeds]...)}
}

// PropReadDealIndex is the fleet table's persisted round-robin cursor for reads at a tier.
// The table property is epoch-scoped, so a restart keeps its place and clear starts the next
// epoch at the first reader (docs/SPEC-SPRINT.md section 1, the read-card deal).
func PropReadDealIndex(tier string) string { return "read_deal_index_" + tier }

// readDealRound is the cursor over units that may receive reads (docs/SPEC-SPRINT.md
// section 1; README.md, free capacity gets another ready card). Its tier-specific fleet
// property advances atomically with the read cards, and ineligible or full units are skipped.
func readDealRound(s *Snapshot, tier string, units []readUnit) *round {
	names := make([]string, 0, len(units))
	for _, u := range units {
		names = append(names, u.name)
	}
	return tableRound(s.Fleet, PropReadDealIndex(tier), names)
}
