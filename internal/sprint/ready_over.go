package sprint

import (
	"fmt"
	"maps"
	"regexp"
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
// The ready-over part raises one judgment per (reason, tier) once a ready card has sat in
// ready past ReadyMax (the sprint setting, default one minute), the reason computed by the
// deal itself (readyWhy: routeOf and noLauncher, the hold and sentinel checks). The
// judgment names the tier, each route of it that serves no card and why (disabled, resting
// until paid, resting until woken, out of budget), and the remedy verb (routes wake,
// funded, fleet up, release). One judgment per (reason, tier) groups the cards that share
// the cause, so twenty-five cards blocked by one cause are one alert, not twenty-five. It
// clears itself the tick the card is dealt. The model is tla/ReadyOver.tla.

// NReadyOver is the tick's judgment of a card ready past its bound.
const NReadyOver = "a ready card is not dealt"

func init() {
	TickDecisions[NReadyOver] = []string{"look at the card", "drop"} // never wait: it holds its card (wait_refused.go)
}

// routeWhys names each route of the tier that serves no card now, with why: disabled,
// resting until paid, resting until woken, resting until its time, or out of budget (its
// provider's balance at or under zero). A route that serves is not named. The rest a route
// is named for is the deal's own settled rest (withRests, route_rest.go), the rests due
// this tick included, so the route the judgment names as resting is exactly one the deal
// does not draw; it is never recomputed here. The model is tla/ReadyOver.tla, OutOfBudget;
// the judgment is the ready-over part, docs/SPEC-SPRINT.md section 8 (Notifications).
func (s *Snapshot) routeWhys(tier string) []string {
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
		if rest, ok := s.resting(r.Name); ok {
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
// tier it is judged under and the sentence, "" when the deal would deal it. It reuses the
// deal's own checks (routeOf, noLauncher) and adds the room check (noFreeWidth), so the
// reason the seat sees is the deal's (tla/ReadyOver.tla).
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
		// A brief whose model lines do not read is the card's own cause, not the tier's
		// routes: keep its reason, so it and an unserved tier are two judgments, never
		// merged under the tier's route sentence (tla/ReadyOver.tla, OnePerCause).
		if _, bad := modelOf(ec); bad == "" {
			if ws := s.routeWhys(tier); len(ws) > 0 {
				return tier, "tier " + tier + " has no serving route (" + strings.Join(ws, "; ") + "): enable a route (nova-config route add <name> --tier " + tier + " ...), wake its routes (nova-sprint routes wake), fund its provider (nova-sprint funded), or bring up a friend whose row lists " + tier
			}
		}
		return tier, rwhy
	}
	if lwhy := s.noLauncher(c, ec, tier, s.UpMembers()); lwhy != "" {
		return tier, lwhy
	}
	if rwhy := s.noFreeWidth(c, ec, tier, s.UpMembers()); rwhy != "" {
		return tier, rwhy
	}
	return tier, ""
}

// noFreeWidth is why no up member that can launch a route of the primary c's tier has free
// width (its room, DealAhead times its width less what it holds): "" when some member that
// can launch it has room, or when none is up or none can launch (noLauncher's reason). The
// deal's own room check (steps_work.go, noRoomWhy) never names it to the seat, so a ready
// card waiting only for a member's room raises nothing after the bound unless the ready-over
// part names it: it does, the "fleet up" reason (tla/ReadyOver.tla).
func (s *Snapshot) noFreeWidth(c, ec *Card, tier string, up []string) string {
	pool, wc, _ := s.dealPool(c, up)
	if len(pool) == 0 {
		return "" // no member up, or its bench down: the deal's own judgments
	}
	ms, _ := s.launchersOf(ec, wc, pool)
	if len(ms) == 0 {
		return "" // no member can launch a route: noLauncher's reason
	}
	q, widths := memberLoads(s, up), memberWidths(s, up)
	for _, m := range ms {
		if q[m] < widths[m] {
			return "" // a member can launch and has room: the deal deals it
		}
	}
	return "no member up has free width to launch a route of tier " + tier + ": every member that can launch it is at its room (DealAhead times its width): fleet up a member (nova-sprint fleet up <member> --width <n>) or widen one; the tick deals it when one has room"
}

// readyOverWhy is the reason part of a ready-over what sentence ("<n> primaries of tier
// <t> sit in ready past <max>: <why>"): the stable cause the judgment is keyed on, so
// distinct causes of one tier are distinct judgments, never merged
// (tla/ReadyOver.tla, OnePerCause). The figures the cause names that move while it stands
// -- a route's balance ("out of budget (balance $x)") and a rest's end ("resting until
// <stamp>") -- are taken out of the key, so a balance or a rest that moves keys the same
// judgment and is rewritten in place with the latest figures; the What keeps them
// (notify's update). The open rests' words ("paid", "woken") are not figures and stay.
func readyOverWhy(what string) string {
	_, why, ok := strings.Cut(what, ": ")
	if !ok {
		return what
	}
	why = readyOverBalanceRE.ReplaceAllString(why, "(balance)")
	why = readyOverRestEndRE.ReplaceAllString(why, "resting until")
	return why
}

// readyOverBalanceRE matches a route's out-of-budget balance figure: "(balance $x)"; the
// key keeps the words, never the figure.
var readyOverBalanceRE = regexp.MustCompile(`\(balance [^)]*\)`)

// readyOverRestEndRE matches a rest's end stamp ("resting until <stamp>", the stamp a
// year-first RFC3339, route_rest.go UntilSaid); the key keeps the words, never the
// stamp. An open rest's "paid"/"woken" is no figure and is left to stand.
var readyOverRestEndRE = regexp.MustCompile(`resting until [0-9][^ ]*`)

// readyOverConds is the ready-over conditions of the tick: one per (reason, tier), the
// ready primaries of it that have sat in ready past s.ReadyMax() grouped, with the reason
// the deal leaves them (readyWhy). The grouping key is the reason and the tier together, so
// two ready cards of one tier with different causes are two judgments, never merged under
// one card's text (tla/ReadyOver.tla, OnePerCause).
func readyOverConds(s *Snapshot, r TickReq) []cond {
	max := s.ReadyMax()
	type key struct{ tier, why string }
	byWhy := map[key][]string{}
	for _, c := range s.Work.Column(Ready) {
		if d, ok := r.running(s.Now, c.F(FieldReadyAt)); !ok || d <= max {
			continue
		}
		tier, why := s.readyWhy(c)
		if why == "" {
			continue // the deal would deal it: no ready-over
		}
		k := key{tier, why}
		byWhy[k] = append(byWhy[k], c.ID)
	}
	keys := slices.SortedFunc(maps.Keys(byWhy), func(a, b key) int {
		if c := strings.Compare(a.tier, b.tier); c != 0 {
			return c
		}
		return strings.Compare(a.why, b.why)
	})
	var out []cond
	for _, k := range keys {
		out = append(out, cond{typ: NReadyOver, stream: TierSubject(k.tier), streamLevel: true, primaries: byWhy[k],
			what: fmt.Sprintf("%d primaries of tier %s sit in ready past %s: %s", len(byWhy[k]), k.tier, max, k.why)})
	}
	return out
}

// TickReadyOver is the ready-over part: one judgment per (reason, tier) of the cards that
// sat in ready past the sprint's ready bound (ReadyMax), naming the tier, each route that
// does not serve it and why, and the remedy verb. It clears itself the tick the card is
// dealt. The rests are settled once, as the deal settles them (withRests, route_rest.go),
// on the snapshot the conditions are counted on, so the reason the judgment names is the
// deal's own (the rests it would not draw, those due this tick included); the ready-over
// part runs on the end part's own snapshot (TickDeadlines, steps_tick.go), which never
// settles it itself. The judgment is the ready-over part, docs/SPEC-SPRINT.md section 8
// (Notifications).
// tla/ReadyOver.tla Raise
func TickReadyOver(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	s, _ = s.withRests()
	return p, notify(&p, s, readyOverConds(s, r), []string{NReadyOver}, r)
}
