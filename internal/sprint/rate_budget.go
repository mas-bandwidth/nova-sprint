package sprint

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// THE GLOBAL RATE LIMITER (the owner, 2026-10-11: "Please prioritize getting that mercury 3
// rate limit up ASAP"; "the rate limited should just stall out delay, not fail"; "i don't
// want this rate limiter causing errors"; "i want it just automatically falling back to
// something else, OR if nothing else is available, you wait and try again later";
// tla/RateBudget.tla).
//
// Two budgets, set by the coordinator (routes limit) as fleet table properties, one per
// model or provider, so a clear's tabula rasa clears them with the rest of the sprint:
//
//   - a model's requests per minute (PropModelRPM): one shared window per provider/model in
//     the sprint's store (pkg/ratebudget), taken before EVERY model request by every caller
//     on every machine through the server's verb, budget take; a request over it waits;
//   - a provider's concurrency (PropProviderConcurrency): the work and read cards in flight
//     on the provider's routes, dealt or taken, across the whole fleet. The deal never
//     deals past it: it falls back to the next route of the tier whose provider has room
//     (routeOf, skipping a full provider as it skips a resting route), and with no such
//     route the card waits ready and is dealt when a slot frees, never an error and never
//     an attempt spent. The take holds it again at the one writer (takeOne), so a slot
//     freed and filled between a deal and a take never puts one more in flight. A card's
//     slot is its place in the fleet table: a member that dies is put down by its beat and
//     its cards withdrawn, so no slot outlives its holder (the lease's expiry).
//
// AN RPM BUDGET NEVER MAKES A ROUTE FULL: it paces requests, it refuses no card. With rpm
// alone, k cards on the route share the minute's requests and most may end by deadline, so
// an rpm-limited route is given a concurrency budget too (routes limit <provider>
// --concurrent n): the deal then falls back past it once n cards are on its provider. No
// concurrency is implied by an rpm (a second, hidden setting); set both.
//
// A route with neither set is not limited: no field is stamped on its cards and its member
// never asks the server (zero code path change). A limit is stamped on the cards dealt on
// its route (FieldRateRPM, FieldRateConcurrent) so the member installs its request gate;
// the value is read live by budget take on every request, so a limit changed later holds
// at once on the cards that carry it. Set the limit BEFORE enabling a route.

// PropModelRPM is the fleet table's property that holds a model's requests-per-minute
// budget ("10"), one per provider/model however many routes run it; absent is unlimited.
// A property's name is letters, digits, _ . and -: the model's first slash is written as a
// dot (a provider's name holds none) and any other character as _ (propModel).
func PropModelRPM(model string) string { return "rate_rpm_" + propModel(model) }

// propModel is a provider/model id as a property name's part: provider.model, any character
// a property may not hold as _.
func propModel(model string) string {
	provider, rest, _ := strings.Cut(model, "/")
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
				return r
			}
			return '_'
		}, s)
	}
	if rest == "" {
		return clean(provider)
	}
	return strings.ReplaceAll(clean(provider), ".", "_") + "." + clean(rest)
}

// modelOfProp is a model's id as PropModelRPM wrote it, its first dot read back as the slash.
func modelOfProp(p string) string { return strings.Replace(p, ".", "/", 1) }

// PropProviderConcurrency is the fleet table's property that holds a provider's
// concurrency budget ("8"), one per provider; absent is unlimited.
func PropProviderConcurrency(provider string) string { return "provider_concurrency_" + provider }

// The work and read card fields of a limited route, stamped by the deal from the
// properties: the member's request gate is installed when one is set. FieldRateReturns
// counts the takes of the card a rate limit returned untouched (rateLimitedReturned): at
// MaxRateReturns the next one ends as any provider failure does, so no card loops forever.
const (
	FieldRateRPM        = "rate_rpm"
	FieldRateConcurrent = "rate_concurrent"
	FieldRateReturns    = "rate_returns"
	MaxRateReturns      = 5
)

// rateLimitedRoute says the card was dealt on a limited route (routes limit): its member
// took the budget before each request and retried a 429 in place, so a 429 that still
// reaches the sprint is the limiter's to absorb, not the route's to rest for. A card on an
// unlimited route has neither field, and its 429 ends the take and counts toward the
// route's rest as before.
func rateLimitedRoute(c *Card) bool {
	return c.F(FieldRateRPM) != "" || c.F(FieldRateConcurrent) != ""
}

// NRouteLimited is the happened note of a budget the coordinator set or cleared.
const NRouteLimited = "a model's or provider's rate budget set by the coordinator"

// RouteLimitReq is routes limit: a model's requests per minute (RPM, the route's model) or
// a provider's concurrency (Concurrent, the route's provider or the provider named); 0
// clears; exactly one of them is given (Set names which).
type RouteLimitReq struct {
	Target     string
	RPM        int
	Concurrent int
	Set        string // "rpm" or "concurrent"
	Reason     string
	Who        string
}

// budgetOf reads a budget property: n above zero, or 0 for unlimited.
func budgetOf(t *Table, name string) int {
	if t == nil {
		return 0
	}
	v, _ := t.Prop(name)
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// ModelRPM is the model's requests-per-minute budget, 0 for unlimited.
func ModelRPM(s *Snapshot, model string) int { return budgetOf(s.Fleet, PropModelRPM(model)) }

// ProviderConcurrency is the provider's concurrency budget, 0 for unlimited.
func ProviderConcurrency(s *Snapshot, provider string) int {
	return budgetOf(s.Fleet, PropProviderConcurrency(provider))
}

// cardProvider is the provider a fleet card runs on: its model id's first word; "" for a
// card with no model, a pinned card, and a friend's card (on her own model).
func cardProvider(c *Card) string {
	if IsFriendRow(c.Row) || c.F(FieldRoute) == "" || c.F(FieldRoute) == RoutePin {
		return ""
	}
	p, _, ok := strings.Cut(c.F(FieldModel), "/")
	if !ok {
		return ""
	}
	return p
}

// InFlight is the fleet's cards on the provider's routes in cols (Working for a take's
// count, Ready and Working for a deal's: a card dealt is a slot promised).
func InFlight(s *Snapshot, provider string, cols ...string) int {
	n := 0
	for _, c := range s.Fleet.Column(cols...) {
		if cardProvider(c) == provider {
			n++
		}
	}
	return n
}

// budgetPlan is a dealing step's view of the providers' concurrency: the budget and the
// cards in flight (dealt or taken) of each provider with a budget, counted once, and the
// cards the step has dealt on it so far.
type budgetPlan struct {
	budget, inFlight map[string]int
	// soft is a coordinator's rework or redo: it falls back past a full provider while
	// another route has room, and is dealt on a full one when none has (never refused)
	soft bool
}

// withSoftBudgets is withBudgets for a coordinator's rework or redo (budgetPlan.soft).
func (s *Snapshot) withSoftBudgets() *Snapshot {
	n := s.withBudgets()
	if n.budgets != nil {
		n.budgets.soft = true
	}
	return n
}

// withBudgets is s with its dealing step's concurrency view: nil when no provider has a
// budget, so an unlimited sprint deals as before.
func (s *Snapshot) withBudgets() *Snapshot {
	n := *s
	n.budgets = nil
	var limited []string
	for _, r := range s.Routes {
		if r.Provider != "" && ProviderConcurrency(s, r.Provider) > 0 && !contains(limited, r.Provider) {
			limited = append(limited, r.Provider)
		}
	}
	if len(limited) == 0 {
		return &n
	}
	bp := &budgetPlan{budget: map[string]int{}, inFlight: map[string]int{}}
	for _, p := range limited {
		bp.budget[p] = ProviderConcurrency(s, p)
	}
	for _, c := range s.Fleet.Column(Ready, Working) {
		if p := cardProvider(c); p != "" {
			if _, ok := bp.budget[p]; ok {
				bp.inFlight[p]++
			}
		}
	}
	n.budgets = bp
	return &n
}

// full says the provider has no slot for one more card in this step, and the words.
func (bp *budgetPlan) full(provider string) (bool, string) {
	if bp == nil {
		return false, ""
	}
	b, ok := bp.budget[provider]
	if !ok || bp.inFlight[provider] < b {
		return false, ""
	}
	return true, fmt.Sprintf("%s %d of %d in flight", provider, bp.inFlight[provider], b)
}

// dealt counts one card dealt on the provider in this step.
func (bp *budgetPlan) dealt(provider string) {
	if bp == nil {
		return
	}
	if _, ok := bp.budget[provider]; ok {
		bp.inFlight[provider]++
	}
}

// rateFields are the limit fields of a route's cards: its model's rpm and its provider's
// concurrency, each only when set, so an unlimited route's cards are as before. A card
// redealt from a limited route onto an unlimited one keeps the words; its member then asks
// budget take, which reads no budget and grants at once.
func rateFields(s *Snapshot, r Route) map[string]string {
	out := map[string]string{}
	if n := ModelRPM(s, r.Provider+"/"+r.Model); n > 0 {
		out[FieldRateRPM] = strconv.Itoa(n)
	}
	if n := ProviderConcurrency(s, r.Provider); n > 0 {
		out[FieldRateConcurrent] = strconv.Itoa(n)
	}
	return out
}

// takeBudgetWhy is why a take may not move the card to working now: its provider has its
// budget of cards working across the fleet, counting the ones this take moves before it
// (planned). "" is may. The card stays ready: it is taken when a slot frees.
func takeBudgetWhy(s *Snapshot, c *Card, planned map[string]int) string {
	p := cardProvider(c)
	if p == "" {
		return ""
	}
	b := ProviderConcurrency(s, p)
	if b <= 0 {
		return ""
	}
	if n := InFlight(s, p, Working) + planned[p]; n >= b {
		return fmt.Sprintf("provider %s is at its concurrency budget (%d of %d working across the fleet): the card waits ready and is taken when one ends", p, n, b)
	}
	planned[p]++
	return ""
}

// providerLimit is a provider's budgets as the dashboard's providers table says them:
// "concurrent 3/8 · mercury-3 rpm 10", "" for none.
func providerLimit(provider string, routes []Route, fleet *Table) string {
	var parts []string
	if n := budgetOf(fleet, PropProviderConcurrency(provider)); n > 0 {
		working := 0
		for _, c := range fleet.Column(Working) {
			if cardProvider(c) == provider {
				working++
			}
		}
		parts = append(parts, fmt.Sprintf("concurrent %d/%d", working, n))
	}
	seen := map[string]bool{}
	for _, r := range routes {
		if seen[r.Model] {
			continue
		}
		seen[r.Model] = true
		if n := budgetOf(fleet, PropModelRPM(r.Provider+"/"+r.Model)); n > 0 {
			parts = append(parts, fmt.Sprintf("%s rpm %d", r.Model, n))
		}
	}
	return strings.Join(parts, " · ")
}

// LimitRow is one budget as routes and the dashboard show it.
type LimitRow struct {
	Kind   string `json:"kind"`   // rpm or concurrent
	Target string `json:"target"` // the model (rpm) or the provider (concurrent)
	Budget int    `json:"budget"`
	InUse  int    `json:"in_use"` // grants in the window (rpm, when read) or cards in flight
}

// Limits is every budget set, rpm by model then concurrency by provider, in name order;
// a concurrency row counts the fleet's cards working on the provider.
func Limits(s *Snapshot) []LimitRow {
	if s.Fleet == nil {
		return nil
	}
	var out []LimitRow
	for name := range s.Fleet.Props() {
		switch {
		case strings.HasPrefix(name, "rate_rpm_"):
			if n := budgetOf(s.Fleet, name); n > 0 {
				out = append(out, LimitRow{Kind: "rpm", Target: modelOfProp(strings.TrimPrefix(name, "rate_rpm_")), Budget: n})
			}
		case strings.HasPrefix(name, "provider_concurrency_"):
			if n := budgetOf(s.Fleet, name); n > 0 {
				p := strings.TrimPrefix(name, "provider_concurrency_")
				out = append(out, LimitRow{Kind: "concurrent", Target: p, Budget: n, InUse: InFlight(s, p, Working)})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "rpm"
		}
		return out[i].Target < out[j].Target
	})
	return out
}

// SetRouteLimit is routes limit: the model's rpm (a route names its model) or the
// provider's concurrency (a route names its provider; a provider itself), 0 clearing it;
// refused with no reason, a negative budget, a target that names no route or provider, or
// an rpm on a provider (an rpm is a model's: name a route).
func SetRouteLimit(s *Snapshot, r RouteLimitReq) Plan {
	var p Plan
	if strings.TrimSpace(r.Reason) == "" {
		p.refuse(r.Target, "routes limit wants --reason: why the budget is set, in a few words")
		return p
	}
	n := r.RPM
	if r.Set == "concurrent" {
		n = r.Concurrent
	}
	if n < 0 {
		p.refuse(r.Target, "a budget is a whole number, 0 clearing it, found "+strconv.Itoa(n))
		return p
	}
	provider, routes, isProvider, ok := routeTarget(s, r.Target)
	if !ok {
		p.refuse(r.Target, r.Target+" names no route and no provider of a route (nova-sprint routes lists them)")
		return p
	}
	var name, what string
	switch r.Set {
	case "rpm":
		if isProvider {
			p.refuse(r.Target, "--rpm is a model's budget: name a route of "+provider+" (nova-sprint routes lists them), and every route of that model shares it")
			return p
		}
		model := ""
		for _, x := range s.Routes {
			if x.Name == routes[0] {
				model = x.Provider + "/" + x.Model
			}
		}
		name, what = PropModelRPM(model), "model "+model+" requests per minute"
	case "concurrent":
		name, what = PropProviderConcurrency(provider), "provider "+provider+" concurrency"
	default:
		p.refuse(r.Target, "routes limit wants --rpm <n> or --concurrent <n>")
		return p
	}
	was, had := s.Fleet.Prop(name)
	value, said := strconv.Itoa(n), strconv.Itoa(n)
	if n == 0 {
		value, said = "", "unlimited (cleared)"
	}
	p.Props = append(p.Props, PropWrite{Table: Fleet, Name: name, Value: value, Was: was, WasAbsent: !had})
	note := happened(NRouteLimited, ProviderSubject(provider), s.Now)
	note.To, note.Who = s.Coordinator, r.Who
	note.What = fmt.Sprintf("%s set to %s by %s: %s", what, said, r.Who, oneLine(r.Reason))
	p.Notes = append(p.Notes, note)
	p.Units = append(p.Units, Unit{Key: r.Target, Moved: what + " " + said})
	return p
}
