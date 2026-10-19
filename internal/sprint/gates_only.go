package sprint

import (
	"slices"
	"strings"
)

// FieldGatesOnly is a fleet member's control card field: the gate words, comma separated,
// that alone bring work to the member (Glenn 2026-10-19: native Windows members, captain and
// titan, that take ONLY the cards whose GATE-NEEDS names windows). nova-config's machine row
// carries it (gates_only) and fleet sync writes it here (FleetDrift, DriftGates); empty is a
// member dealt any card its facts satisfy. tla/GatesOnly.tla models the rule.
const FieldGatesOnly = "gates_only"

// MemberGatesOnly reads a member's gates from its control card (FieldGatesOnly), Split into
// its comma list; nil when the member has no control card or names none.
func (s *Snapshot) MemberGatesOnly(member string) []string {
	if s == nil {
		return nil
	}
	return Split(s.MemberCtl(member).F(FieldGatesOnly))
}

// gatesOnlyAdmits says a member with these gates may be dealt a card with these GATE-NEEDS:
// a member with no gates admits every card; one with gates admits a card only when its
// GATE-NEEDS names one of them, so a generic card (no GATE-NEEDS) never reaches it
// (tla/GatesOnly.tla, Admits; GenericNeverToGatesOnly).
func gatesOnlyAdmits(gates, needs []string) bool {
	if len(gates) == 0 {
		return true
	}
	return slices.ContainsFunc(gates, func(g string) bool { return slices.Contains(needs, g) })
}

// AdmitGatesOnly narrows ms to the members that may be dealt the primary c by their gates
// (gatesOnlyAdmits): every place that picks a member for a card narrows by it first, then
// holds the card's needs against the facts (GateHostPool). A nil c names no need, so no
// gates-only member admits it. ms is never changed; with no gates-only member in ms it is
// returned as it is.
func (s *Snapshot) AdmitGatesOnly(c *Card, ms []string) []string {
	if !slices.ContainsFunc(ms, func(m string) bool { return s.MemberCtl(m).F(FieldGatesOnly) != "" }) {
		return ms
	}
	needs := GateNeeds(c.F("brief"))
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		if gatesOnlyAdmits(s.MemberGatesOnly(m), needs) {
			out = append(out, m)
		}
	}
	return out
}

// gatesOnlyWhy is why a card waits ready when every member its bench and room allow is a
// gates-only member whose gates its GATE-NEEDS does not name: it waits for a member that
// takes it, never a judgment (no need is unmet; the members up are reserved).
func gatesOnlyWhy(s *Snapshot, ms []string) string {
	var named []string
	for _, m := range ms {
		named = append(named, m+" (gates_only: "+strings.Join(s.MemberGatesOnly(m), ",")+")")
	}
	return "no member up takes it: every member up is gates-only and its GATE-NEEDS names none of their gates (" + strings.Join(named, "; ") + ")"
}

// gateEligible says the member m may hold an attempt of the primary c by its gates and its
// facts together: its gates admit the card (gatesOnlyAdmits) and its facts carry every need
// of the card's GATE-NEEDS line (memberSatisfies). It is the one rule every place that puts
// an attempt on a member holds: the first deal (GateHostPool) and every move after it, the
// next attempt's deal, the redeal of a member gone down or held, the level and the
// rebalance. Before v1.6.8 the moves held the gates alone or nothing, so a card the first
// deal gave the only darwin member was moved to a Linux bench to even the queues, every
// attempt there ended HOLD and the bound marked a sound brief defective (a card dealt to a
// member that can never satisfy it: a livelock). A nil c names no need. tla/GatesOnly.tla, Move.
func (s *Snapshot) gateEligible(c *Card, m string) bool {
	var needs []string
	if c != nil {
		needs = GateNeeds(c.F("brief"))
	}
	return gatesOnlyAdmits(s.MemberGatesOnly(m), needs) && memberSatisfies(s.MemberFacts(m), needs)
}

// AdmitGate narrows ms to the members gateEligible for the primary c; ms is never changed.
func (s *Snapshot) AdmitGate(c *Card, ms []string) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		if s.gateEligible(c, m) {
			out = append(out, m)
		}
	}
	return out
}

// primaryOfWork is the primary a work card is an attempt of, nil when it has none.
func (s *Snapshot) primaryOfWork(wc *Card) *Card {
	if s == nil || s.Work == nil || wc == nil {
		return nil
	}
	return s.Work.Placed(wc.F("primary"))
}

// admitGateWork is AdmitGate for a work card: the needs are its primary's brief's.
func (s *Snapshot) admitGateWork(wc *Card, ms []string) []string {
	return s.AdmitGate(s.primaryOfWork(wc), ms)
}

// notGateWork is the members of up the work card wc may not move to by gateEligible: the
// avoid list the level holds beside its bench and its staging refusers.
func (s *Snapshot) notGateWork(wc *Card, up []string) []string {
	return without(up, s.admitGateWork(wc, up))
}
