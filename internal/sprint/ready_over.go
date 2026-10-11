package sprint

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// A CARD READY OVER A MINUTE PUSHES THE SEAT (the owner, 2026-10-11 ~01:30Z: "ready
// cards that don't have routes -- that's a push notification you should get"; "or, cards
// in ready for any length of time above 1m. that's a push alert.";
// a-card-ready-over-a-minute-pushes-the-seat). The tick's deal part raises the immediate
// judgments a card stuck in ready falls under (no route serves its tier, no member up, no
// member can launch a route): each names the cards it leaves in ready. But a card can sit
// in ready past those judgments without the seat being told, and on 2026-10-11 twenty-five
// ready cards waited while pro and heavy had one route each and every opencode route was
// disabled, and the seat learned of it from the owner.
//
// The ready-over part raises one judgment per tier once a ready card has sat in ready past
// ReadyMax (the sprint setting, default one minute), the reason computed by the deal itself
// (readyWhy: routeOf and noLauncher, the hold and sentinel checks). The judgment names the
// tier, each route of it that serves no card and why (disabled, resting until paid, resting
// until woken, out of budget), and the remedy verb (routes wake, funded, fleet up,
// release). One judgment per (reason, tier) groups the cards that share the cause, so
// twenty-five cards blocked by one cause are one alert, not twenty-five. It clears itself
// the tick the card is dealt. The model is tla/SprintEvents.tla.

// NReadyOver is the tick's judgment of a card ready past its bound.
const NReadyOver = "a ready card is not dealt"

func init() {
	TickDecisions[NReadyOver] = []string{"look at the card", "drop", "wait"}
}

// routeWhys names each route of the tier that serves no card now, with why: disabled,
// resting until paid, resting until woken, resting until its time, or out of budget (its
// provider's balance at or under zero). A route that serves is not named.
func (s *Snapshot) routeWhys(tier string) []string {
	rests := RouteRests(s.Routes, s.Fleet)
	balances := ProviderBalances(s.Fleet)
	var out []string
	for _, r := range s.Routes {
		if r.Tier != tier {
			continue
		}
		if !r.Enabled {
			out = append(out, r.Name+": disabled")
			continue
		}
		if rest, ok := rests[r.Name]; ok && rest.Resting(s.Now) {
			out = append(out, r.Name+": resting until "+rest.UntilSaid()+": "+rest.Said())
			continue
		}
		if b, ok := balances[r.Provider]; ok && b.Known && b.Balance <= 0 {
			out = append(out, r.Name+": out of budget (balance "+Dollars(b.Balance)+")")
		}
	}
	return out
}

// readyWhy is why a ready primary c is not dealt, the reason the ready-over part names: the
// tier it is judged under and the sentence, "" when the deal would deal it (a card waiting
// only for a member's room). It reuses the deal's own checks (routeOf, noLauncher), so the
// reason the seat sees is the deal's (tla/SprintEvents.tla).
func (s *Snapshot) readyWhy(c *Card) (tier, why string) {
	if IsSentinel(c) {
		return cardTierOf(c), "a sentinel waits for the coordinator's release: nova-sprint release " + c.ID + " --reason <why>"
	}
	if IsHeld(c) {
		return cardTierOf(c), "held by the coordinator: release it: nova-sprint release " + c.ID + " --reason <why>"
	}
	if StreamHeld(s, c.Row) {
		return cardTierOf(c), "its stream " + c.Row + " is held (" + orDash(s.StreamCtl(c.Row).F(FieldHeldReason)) + "): release it: nova-sprint unhold " + c.Row
	}
	ec := escalating(s, c)
	_, tier, rwhy, byFriend := s.routeOf(ec, nil, nil, "")
	if byFriend {
		return tier, "friends alone serve tier " + tier + " and none up has room for it: bring up a friend whose row lists " + tier
	}
	if rwhy != "" {
		if ws := s.routeWhys(tier); len(ws) > 0 {
			return tier, "tier " + tier + " has no serving route (" + strings.Join(ws, "; ") + "): enable a route (nova-config route add <name> --tier " + tier + " ...), wake its routes (nova-sprint routes wake), fund its provider (nova-sprint funded), or bring up a friend whose row lists " + tier
		}
		return tier, rwhy
	}
	if lwhy := s.noLauncher(c, ec, tier, s.UpMembers()); lwhy != "" {
		return tier, lwhy
	}
	return tier, ""
}

// readyOverConds is the ready-over conditions of the tick: one per tier, the ready
// primaries of it that have sat in ready past s.ReadyMax() grouped, with the reason the
// deal leaves them (readyWhy).
func readyOverConds(s *Snapshot, r TickReq) []cond {
	max := s.ReadyMax()
	byTier := map[string][]string{}
	whyOf := map[string]string{}
	for _, c := range s.Work.Column(Ready) {
		if d, ok := r.running(s.Now, c.F(FieldReadyAt)); !ok || d <= max {
			continue
		}
		tier, why := s.readyWhy(c)
		if why == "" {
			continue // the deal would deal it: no ready-over
		}
		byTier[tier] = append(byTier[tier], c.ID)
		if whyOf[tier] == "" {
			whyOf[tier] = why
		}
	}
	var out []cond
	for _, tier := range slices.Sorted(maps.Keys(byTier)) {
		out = append(out, cond{typ: NReadyOver, stream: TierSubject(tier), streamLevel: true, primaries: byTier[tier],
			what: fmt.Sprintf("%d primaries of tier %s sit in ready past %s: %s", len(byTier[tier]), tier, max, whyOf[tier])})
	}
	return out
}

// TickReadyOver is the ready-over part: one judgment per tier of the cards that sat in
// ready past the sprint's ready bound (ReadyMax), naming the tier, each route that does
// not serve it and why, and the remedy verb. It clears itself the tick the card is dealt.
func TickReadyOver(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	return p, notify(&p, s, readyOverConds(s, r), []string{NReadyOver}, r)
}
