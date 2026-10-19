package sprint

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// ONE ORDERED FALLBACK FOR WORK AND READS (Glenn, 2026-10-19: "one provider
// failing never stalls the fleet, it just falls back to the next one in line").
//
// Per tier the routes in line are the tier's array order (tierArray), across
// providers, filtered to routes that serve: enabled, provider funded, not
// resting, not cooling. A provider is cooling while it has a rate-limit
// backoff (its window from the provider's own words when they name one, else
// RateBackoff), a transient rest (a 5xx or a timeout), or an out-of-credit
// rest / funded=false, which is out until the owner funds it. Cooling is per
// PROVIDER, never per route: every route of a cooling provider leaves the
// line. A provider fault never spends: the same card, attempt or read is
// retried at once on the next route in line. Only when every route of the
// tier is cooling does the card wait, with a deadline and one judgment naming
// each provider. The model is tla/ProviderFallback.tla; docs/SPEC-SPRINT.md
// section 5 (the deal) and section 6 (the reads) state it.

// ProviderFault* are the provider faults that cool a provider (tla/ProviderFallback.tla,
// ProviderFault(p, kind)): a rate limit, the provider's own 5xx, a timeout, and an
// out-of-credit refusal.
const (
	ProviderFaultRate    = "rate-limited"
	ProviderFault5xx     = "provider-5xx"
	ProviderFaultTimeout = "timeout"
	ProviderFaultCredit  = "out-of-credit"
)

// ProviderRecheck is the deadline of a tier every provider of which is unfunded: the
// tick re-checks the owner's funding at its own next period (tla/ProviderFallback.tla,
// Fallback's deadline under OwnerFund).
const ProviderRecheck = RateBackoffFor

// The test-only seams the reversed witnesses set: fault counted as an attempt (the
// fallback then does not happen and the old no-spend return runs), and the line
// ignoring provider cooling. They are fleet table properties, so a test sets one on
// its own snapshot and no other test sees it.
const (
	seamFaultCountsAttempt = "test_fault_counts_attempt"
	seamIgnoreCooling      = "test_ignore_provider_cooling"
)

func testSeam(s *Snapshot, name string) bool {
	if s == nil || s.Fleet == nil {
		return false
	}
	v, _ := s.Fleet.Prop(name)
	return v == "1" || v == "true"
}

// providerCool is one provider out of the line: its fault kind, when its cooling ends
// (until; Open for an unfunded provider, which waits for its owner) and its words.
type providerCool struct {
	Provider string
	Kind     string
	Until    time.Time
	Open     bool
	// Whole says the provider itself rested (PropProviderRest), never one route's
	// backoff: every route of it is out of the line (tla/ProviderFallback.tla,
	// NoDealToCooling).
	Whole bool
	Why   string
}

// cooling says the provider is out of the line at now (tla/ProviderFallback.tla, Avail).
func (c providerCool) cooling(now time.Time) bool { return c.Open || now.Before(c.Until) }

// said is the provider's cooling as a judgment names it: "<p> rate-limited until <t>"
// or "<p> unfunded (the owner's to fund)".
func (c providerCool) said() string {
	switch {
	case c.Open || (c.Kind == ProviderFaultCredit && c.Until.Equal(OpenUntil)):
		return c.Provider + " unfunded (the owner's to fund)"
	case c.Kind == "" || c.Kind == ProviderFaultRate:
		return c.Provider + " rate-limited until " + stamp(c.Until)
	case c.Kind == ProviderFaultCredit:
		return c.Provider + " out of credit until " + stamp(c.Until)
	default:
		return c.Provider + " " + c.Kind + " until " + stamp(c.Until)
	}
}

// providerCoolings is each provider out of the line at s.Now (tla/ProviderFallback.tla,
// cool). It reads the rests a dealing step settled (s.rests; a snapshot without them
// reads RouteRests), the funded declaration, and the rate backoffs the fleet table's
// records give. It reads no clock but s.Now.
func (s *Snapshot) providerCoolings() map[string]providerCool {
	if s.rests != nil && len(s.rests) == 0 {
		// a dealing step settled no rest: only an applied, unfunded provider can cool
		funded := true
		for _, p := range s.Providers {
			if p.Problem == "" && !p.Funded {
				funded = false
				break
			}
		}
		if funded {
			return nil
		}
	}
	out := map[string]providerCool{}
	add := func(c providerCool) {
		old, ok := out[c.Provider]
		if !ok || c.Open || !old.Open && c.Until.After(old.Until) || !old.Whole && c.Whole && (c.Until.Equal(old.Until) || c.Until.After(old.Until)) {
			out[c.Provider] = c
		}
	}
	for name, rest := range ProviderRests(s.Fleet) {
		if !rest.Resting(s.Now) {
			continue
		}
		switch rest.Cause {
		case RestCredit, RestAuth:
			add(providerCool{Provider: name, Kind: ProviderFaultCredit, Open: true, Whole: true, Why: rest.Said()})
		case RestRateBackoff:
			add(providerCool{Provider: name, Kind: ProviderFaultRate, Until: rest.Until, Whole: true, Why: rest.Said()})
		case RestProvider:
			add(providerCool{Provider: name, Kind: ProviderFault5xx, Until: rest.Until, Whole: true, Why: rest.Said()})
		}
	}
	for _, r := range s.Routes {
		if r.Provider == "" {
			continue
		}
		if p, ok := s.Providers[r.Provider]; ok && p.Problem == "" && !p.Funded {
			add(providerCool{Provider: r.Provider, Kind: ProviderFaultCredit, Open: true,
				Why: "provider " + r.Provider + " is unfunded (the owner's to fund)"})
		}
	}
	rests := s.rests
	if rests == nil {
		rests = map[string]RouteRest{}
		for name, r := range RouteRests(s.Routes, s.Fleet) {
			if r.Resting(s.Now) {
				rests[name] = r
			}
		}
		for name, r := range rateBackoffs(s.Fleet) {
			if r.Resting(s.Now) {
				rests[name] = r
			}
		}
	}
	for _, r := range s.Routes {
		rest, ok := rests[r.Name]
		if !ok || !rest.Resting(s.Now) || r.Provider == "" {
			continue
		}
		switch rest.Cause {
		case RestCredit, RestAuth:
			add(providerCool{Provider: r.Provider, Kind: ProviderFaultCredit, Open: true, Why: rest.Said()})
		case RestRateBackoff:
			add(providerCool{Provider: r.Provider, Kind: ProviderFaultRate, Until: rest.Until, Why: rest.Said()})
		case RestProvider:
			add(providerCool{Provider: r.Provider, Kind: ProviderFault5xx, Until: rest.Until, Why: rest.Said()})
		}
	}
	return out
}

// ProviderFaultKind is the provider fault a finish or read report names, "" when it
// names none (tla/ProviderFallback.tla, ProviderFault's kinds). It reads the provider
// line as provider_funds.go's causeRE reads it.
func ProviderFaultKind(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || IsNoResult(line) {
		return ""
	}
	if IsRateLimited(line) {
		return ProviderFaultRate
	}
	m := causeRE.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	switch m[1] {
	case classRateLimited:
		return ProviderFaultRate
	case class5xx:
		return ProviderFault5xx
	case classTimeout:
		return ProviderFaultTimeout
	case RestCredit:
		return ProviderFaultCredit
	}
	if m[2] == "429" && refusal(line) == "" {
		return ProviderFaultRate
	}
	return ""
}

// retryAfter reads the window a provider's own words name: `retry after N s`,
// `try again in Ns`, `Retry-After: N`, N seconds or a Go duration. 0 when they name
// none (tla/ProviderFallback.tla: the window from the provider's words when they name
// one, else RateBackoff).
func retryAfter(line string) time.Duration {
	low := strings.ToLower(line)
	for _, pre := range []string{"retry after ", "try again in ", "retry-after: ", "retry-after="} {
		if i := strings.Index(low, pre); i >= 0 {
			rest := strings.TrimSpace(low[i+len(pre):])
			j := 0
			for j < len(rest) && (rest[j] >= '0' && rest[j] <= '9' || rest[j] == '.') {
				j++
			}
			if j == 0 {
				continue
			}
			n, err := strconv.ParseFloat(rest[:j], 64)
			if err != nil || n <= 0 {
				continue
			}
			if strings.HasPrefix(rest[j:], "ms") {
				return time.Duration(n * float64(time.Millisecond))
			}
			if strings.HasPrefix(rest[j:], "m") {
				return time.Duration(n * float64(time.Minute))
			}
			return time.Duration(n * float64(time.Second))
		}
	}
	return 0
}

// providerRestFor is the provider rest a fault writes: a rate limit backs the provider
// off for its own words' window or RateBackoff(1); an out-of-credit refusal holds it
// open until the owner funds it; a transient fault rests it for RateBackoffFor.
// tla/ProviderFallback.tla, ProviderFault and OwnerFund.
func providerRestFor(provider, kind, line string, now time.Time) RouteRest {
	switch kind {
	case ProviderFaultRate:
		d := retryAfter(line)
		if d <= 0 {
			d = RateBackoff(1)
		}
		return RouteRest{At: now, Until: now.Add(d), Cause: RestRateBackoff,
			Why: "provider " + provider + " rate-limited the take; it cools " + d.String()}
	case ProviderFaultCredit:
		return RouteRest{At: now, Until: OpenUntil, Cause: RestCredit,
			Why: "provider " + provider + " refused for want of credit; it is out until the owner funds it"}
	default:
		return RouteRest{At: now, Until: now.Add(RateBackoffFor), Cause: RestProvider,
			Why: "provider " + provider + " failed the take (" + kind + "); it cools " + RateBackoffFor.String()}
	}
}

// ProviderWait is a tier with no route in line: its earliest cooling end (or, when
// every provider is unfunded, ProviderRecheck from now) and the one judgment naming
// each provider (tla/ProviderFallback.tla, Fallback and WaitHasDeadline).
type ProviderWait struct {
	Tier     string
	Deadline time.Time
	Open     bool
	Judgment string
}

// ProviderWaitFor says the wait of tier when no route of it is in line, and whether
// the tier is in that state. A tier with an enabled route whose provider is not
// cooling has no wait. tla/ProviderFallback.tla, Fallback.
func (s *Snapshot) ProviderWaitFor(tier string) (ProviderWait, bool) {
	cools := s.providerCoolings()
	arr := s.tierArray(tier)
	seen := map[string]bool{}
	var said []string
	open := 0
	var deadline time.Time
	routes := 0
	for _, name := range arr {
		r, ok := s.lookupRoute(name)
		if !ok || !r.Enabled || r.Tier != tier {
			continue
		}
		routes++
		if r.Provider == "" {
			return ProviderWait{}, false
		}
		c, cool := cools[r.Provider]
		if !cool || !c.cooling(s.Now) {
			return ProviderWait{}, false
		}
		if !seen[r.Provider] {
			seen[r.Provider] = true
			said = append(said, c.said())
			if c.Open {
				open++
			} else if deadline.IsZero() || c.Until.Before(deadline) {
				deadline = c.Until
			}
		}
	}
	if routes == 0 || len(seen) == 0 {
		return ProviderWait{}, false
	}
	w := ProviderWait{Tier: tier, Deadline: deadline, Open: open == len(seen)}
	if w.Open {
		w.Deadline = s.Now.Add(ProviderRecheck)
	}
	w.Judgment = fmt.Sprintf("tier %s has no route in line: %s", tier, strings.Join(said, ", "))
	return w, true
}

// routeByName is the enabled route of any tier named name; ok false when none is.
func (s *Snapshot) lookupRoute(name string) (Route, bool) {
	for _, r := range s.Routes {
		if r.Name == name {
			return r, r.Enabled
		}
	}
	return Route{}, false
}

// providerFallbackReturned is a take that ended with a provider fault
// (tla/ProviderFallback.tla, ProviderFault and Fallback): the provider cools, and the
// same work card, at the same attempt and generation, is dealt at once to the next
// route in line, on a member that launches it, spending no redeal and no attempt. It
// writes the take's record on the work card (so the route's rest and backoff rules see
// it, as they did) and the provider's rest. ok is false when no route of the tier is in
// line (then the caller keeps the no-spend return and the card waits) or when the
// test-only seam counts the fault as an attempt.
func providerFallbackReturned(s *Snapshot, c, pr *Card, r FinishReq, label, moved string, p *Plan) (Unit, bool) {
	if testSeam(s, seamFaultCountsAttempt) {
		return Unit{}, false
	}
	kind := ProviderFaultKind(r.Report)
	routeName := c.F(FieldRoute)
	if kind == "" || routeName == "" || routeName == RoutePin {
		return Unit{}, false
	}
	route, ok := s.lookupRoute(routeName)
	if !ok || route.Provider == "" {
		return Unit{}, false
	}
	line := cutText(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(r.Report, cardhdr.EndProvider), ":")), MaxProviderErrorBytes)
	rest := providerRestFor(route.Provider, kind, line, s.Now)
	// a single-route tier keeps the no-spend return it had: there is no next route to
	// fall back to, and the route's own rest and backoff own its wait
	if len(s.tierArray(route.Tier)) <= 1 {
		return Unit{}, false
	}
	// the provider cools before the draw: the cooled snapshot is the plan's own view,
	// and the rest is written in the same plan
	fleet := s.Fleet.Clone()
	was, had := fleet.Prop(PropProviderRest(route.Provider))
	fleet.SetProp(PropProviderRest(route.Provider), rest.value())
	cooled := *s
	cooled.Fleet = fleet
	cooled.rests = nil
	cs, _ := cooled.withRests()
	cs = cs.withBudgets()
	var member string
	for _, m := range s.UpMembers() {
		if _, _, why, _ := cs.routeOf(pr, c, routeIndexesOf(cs), m); why == "" {
			member = m
			break
		}
	}
	if member == "" {
		return Unit{}, false
	}
	u, why := redeal(cs, pr, c, member, map[string]int{}, nil)
	if why != "" {
		return Unit{}, false
	}
	_, taken := takeStamps(c)
	set := map[string]string{}
	if kind == ProviderFaultRate {
		rates, _ := RateTakes(c)
		set[FieldRateTake+itoa(len(rates)+1)] = ProviderTake{Route: routeName, Model: c.F(FieldModel), Member: c.Row,
			Finished: stamp(s.Now), Error: line, Taken: taken}.String()
	} else {
		n := c.Int(FieldRateReturns) + 1
		set[FieldRateReturns] = itoa(n)
		if kind == ProviderFaultCredit || isProvider5xx(line) {
			set[FieldProviderTake+itoa(returnTakeNumber(n))] = ProviderTake{Route: routeName, Model: c.F(FieldModel), Member: c.Row,
				Finished: stamp(s.Now), Error: line, Taken: taken}.String()
		}
	}
	for i := range u.Changes {
		if u.Changes[i].Table == Fleet && u.Changes[i].Entry.ID == c.ID {
			if u.Changes[i].Entry.Set == nil {
				u.Changes[i].Entry.Set = map[string]string{}
			}
			for k, v := range set {
				u.Changes[i].Entry.Set[k] = v
			}
		}
	}
	p.Props = append(p.Props, PropWrite{Table: Fleet, Name: PropProviderRest(route.Provider), Value: rest.value(), Was: was, WasAbsent: !had})
	u.Moved += "; the provider fault falls back to " + u.Changes[0].Entry.Set[FieldRoute] + " on " + member + " at the same attempt (no attempt or redeal spent)"
	return u, true
}
