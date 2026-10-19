package sprint

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// ONE PROVIDER FAILING NEVER STALLS THE FLEET: IT FALLS BACK TO THE NEXT ROUTE IN LINE (the
// owner, 2026-10-19, after Ultramarine's 429s spent every reader and stalled reviews
// fleet-wide for hours). The model is tla/ProviderFallback.tla; the rules are
// docs/SPEC-SPRINT.md section 5, "Provider fallback".
//
// (1) THE LINE. Per tier, the routes in line are the tier's array (tierArray) in order,
// across providers, less each route that does not serve (servedRoute: disabled, resting,
// unlaunchable, its provider at its concurrency budget) and each route of a COOLING
// provider (providerCooling). Cooling is per provider, never per route: a rate-limit
// backoff, a transient rest (a 5xx or a timeout) or an out-of-credit rest on any route of
// the provider, the provider's own cool record (PropProviderCool, a read's fault), or the
// provider applied with funded=false (unfunded: out until the owner funds it, no expiry)
// takes every route of it out of line (ProviderFallback.tla Avail, NoDealToCooling).
//
// (2) A PROVIDER FAULT NEVER SPENDS. A take or a read that ends with a provider fault
// (providerFaultClass: rate-limited, provider-5xx, timeout, out-of-credit) cools its
// provider and is retried at once on the next route in line: the same card, the same
// attempt, the same read; no attempt, redeal, reader or MaxRateReturns spent
// (ProviderFallback.tla ProviderFault, Fallback, FaultSpendsNothing).
//
// (3) ONLY AN ALL-COOLING TIER WAITS, with a server-computed deadline (the earliest end of
// its providers' cooling, or FallbackRecheck from now when every one is out until funded)
// and one judgment per tier naming each provider (TierCooling; ProviderFallback.tla
// WaitHasDeadline). The wait ends by itself when any provider stops cooling (Wake,
// NeverLeftUnworked).
//
// (4) The deal never draws a cooling provider: drawRoute (servedRoute), readRouteOf and
// tierRouted read the line of (1).

// FallbackRecheck is the deadline of a wait whose every provider is out until the owner
// funds it: no cooling of the tier ends by itself, so the tick's re-check is the deadline
// (ProviderFallback.tla WaitHasDeadline).
const FallbackRecheck = time.Minute

// coolUnfunded is the cooling of a provider nova-config applied with funded=false.
const coolUnfunded = "unfunded"

// coolWords opens the words of a provider's own cool record (coolWrite), and of the rest
// withRests settles on each route of a cooling provider from it.
const coolWords = "cooling of"

// providerUnfunded says nova-config applied the provider with funded=false (a provider with
// no applied row is unknown, never unfunded): out of line until the owner funds it.
func (s *Snapshot) providerUnfunded(provider string) bool {
	p, ok := s.Providers[provider]
	return ok && p.Problem == "" && !p.Funded
}

// providerCools is each provider's own cool record the fleet table holds (PropProviderCool).
func providerCools(fleet *Table) map[string]RouteRest {
	out := map[string]RouteRest{}
	if fleet == nil {
		return out
	}
	for name, v := range fleet.Props() {
		p, ok := strings.CutPrefix(name, PropProviderCool(""))
		if !ok {
			continue
		}
		if rest, ok := parseRest(v); ok {
			rest.Provider = p
			out[p] = rest
		}
	}
	return out
}

// coolRests settles, for a dealing step (withRests), the cooling of each provider on every
// route of it that has no rest of its own (1): a provider's own cool record, or a cooling
// rest (a rate-limit backoff, a 5xx or timeout rest, out of credit) of another route of it.
// Each is settled as a rate-limit backoff, a wait that ends by itself, so a draw leaves the
// route out and a check of the tier does not judge it (servedRoute); the words name the
// provider's cooling. The tests' "nocooling" witness settles none.
func coolRests(s *Snapshot, rests map[string]RouteRest) {
	if fallbackSeam(s, "nocooling") {
		return
	}
	byProvider := map[string]RouteRest{}
	for _, r := range s.Routes {
		// a route's own rate-limit backoff from its take records stays the route's
		// (tla/RateBackoff.tla); a fault the provider answered cools the provider by its own
		// record (coolWrite), which is settled below
		if rest, ok := rests[r.Name]; ok && coolingCause(rest.Cause) && rest.Cause != RestRateBackoff && r.Provider != "" {
			if old, had := byProvider[r.Provider]; !had || rest.Until.After(old.Until) {
				byProvider[r.Provider] = rest
			}
		}
	}
	for p, rest := range providerCools(s.Fleet) {
		if !rest.Resting(s.Now) {
			continue
		}
		if old, had := byProvider[p]; !had || rest.Until.After(old.Until) {
			byProvider[p] = rest
		}
	}
	for _, r := range s.Routes {
		if _, has := rests[r.Name]; has {
			continue
		}
		c, ok := byProvider[r.Provider]
		if !ok {
			continue
		}
		why := c.Why
		if !strings.HasPrefix(why, coolWords) {
			why = coolWords + " its provider " + r.Provider + ": " + c.Said()
		}
		rests[r.Name] = RouteRest{Route: r.Name, Provider: r.Provider, At: c.At, Until: c.Until, Cards: c.Cards, Cause: RestRateBackoff, Why: why}
	}
}

// PropProviderCool is the fleet table's property of a provider's own cooling, written when a
// read ends with a provider fault or a take falls back from a 5xx or a timeout: the rest
// value (RouteRest.value) whose window the provider's words name (retryAfter) or
// RateBackoff(k) gives, k carried in its words (coolK).
func PropProviderCool(provider string) string { return "provider_cool_" + provider }

// providerFaultClass is the class of a provider fault a take's or a read's line names:
// RestCredit (out of credit, a 402), classRateLimited, class5xx or classTimeout; "" for any
// other line (a no-result line, an auth refusal, the card's own failure). The line is read
// as provider_funds.go's causeRE reads it.
func providerFaultClass(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || IsNoResult(line) {
		return ""
	}
	if outOfCredit(line) {
		return RestCredit
	}
	if IsRateLimited(line) {
		return classRateLimited
	}
	if m := causeRE.FindStringSubmatch(line); m != nil && (m[1] == class5xx || m[1] == classTimeout) {
		return m[1]
	}
	return ""
}

// retryAfterRE reads the provider's own words for when to try again: `retry after 30 s`,
// `try again in 12s`, `Retry-After: 20`.
var retryAfterRE = regexp.MustCompile(`(?i)(?:retry[- ]after:?|try again in)\s*(\d+(?:\.\d+)?)\s*(ms|milliseconds?|s|secs?|seconds?|m|mins?|minutes?)?\b`)

// retryAfter is the window the provider's words name, capped at RateBackoffMax
// (RateBackoff.tla BackoffBounded) and never under one second; ok false when they name none.
func retryAfter(line string) (time.Duration, bool) {
	m := retryAfterRE.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil || n < 0 {
		return 0, false
	}
	unit := time.Second
	switch u := strings.ToLower(m[2]); {
	case strings.HasPrefix(u, "ms"), strings.HasPrefix(u, "milli"):
		unit = time.Millisecond
	case strings.HasPrefix(u, "m"):
		unit = time.Minute
	}
	d := time.Duration(n * float64(unit))
	return min(max(d, time.Second), RateBackoffMax), true
}

// coolKRE reads k from a cool record's words.
var coolKRE = regexp.MustCompile(`\(k=(\d+)\)`)

// coolK is the count a provider's cool record carries (1 when it names none).
func coolK(r RouteRest) int {
	if m := coolKRE.FindStringSubmatch(r.Why); m != nil {
		if k, err := strconv.Atoi(m[1]); err == nil && k > 0 {
			return k
		}
	}
	return 1
}

// providerCoolRest is the fleet table's own cool record of provider, as written.
func providerCoolRest(s *Snapshot, provider string) (RouteRest, bool) {
	if s.Fleet == nil {
		return RouteRest{}, false
	}
	v, ok := s.Fleet.Prop(PropProviderCool(provider))
	if !ok {
		return RouteRest{}, false
	}
	r, ok := parseRest(v)
	r.Provider = provider
	return r, ok
}

// coolingCause says a rest of the cause cools its provider (1): a rate-limit backoff, a
// transient rest (5xx or timeout) and an out-of-credit rest. A key refusal or the
// coordinator's rest rests the route as before; it is not the provider's cooling.
func coolingCause(cause string) bool {
	return cause == RestRateBackoff || cause == RestProvider || cause == RestCredit
}

// fallbackSeams holds the tests' reversed witnesses, keyed by a world's fleet table, so a
// witness changes no other test running beside it: "spend" counts a fault as an attempt
// (FaultSpendsNothing, MCProviderFallbackBrokenSpend), "nocooling" leaves cooling out of
// the line (NoDealToCooling, MCProviderFallbackBrokenDealCooling).
var fallbackSeams sync.Map

func fallbackSeam(s *Snapshot, name string) bool {
	if s == nil || s.Fleet == nil {
		return false
	}
	v, ok := fallbackSeams.Load(s.Fleet)
	return ok && slices.Contains(v.([]string), name)
}

// providerCooling is the cooling that holds the provider out of line at s.Now, the one that
// ends last when several hold (Until is OpenUntil for one that ends only when the owner
// funds it), and whether one holds (1; ProviderFallback.tla Avail). It reads the rests a
// dealing step settled (withRests) for each route of the provider, the provider's own cool
// record, its rest property when no rests were settled, and the provider row nova-config
// applied (funded=false is unfunded; a provider with no applied row is unknown and never
// cooling).
func (s *Snapshot) providerCooling(provider string) (RouteRest, bool) {
	if provider == "" || fallbackSeam(s, "nocooling") {
		return RouteRest{}, false
	}
	var best RouteRest
	found := false
	consider := func(r RouteRest) {
		if !found || r.Until.After(best.Until) {
			best, found = r, true
		}
	}
	if p, ok := s.Providers[provider]; ok && p.Problem == "" && !p.Funded {
		consider(RouteRest{Provider: provider, At: s.Now, Until: OpenUntil, Cause: coolUnfunded, Why: "unfunded (the owner's to fund)"})
	}
	if s.rests != nil {
		for _, r := range s.Routes {
			if r.Provider != provider {
				continue
			}
			if rest, ok := s.resting(r.Name); ok && coolingCause(rest.Cause) {
				rest.Provider = provider
				consider(rest)
			}
		}
	} else if s.Fleet != nil {
		if v, ok := s.Fleet.Prop(PropProviderRest(provider)); ok {
			if rest, ok := parseRest(v); ok && rest.Resting(s.Now) && coolingCause(rest.Cause) {
				rest.Provider = provider
				consider(rest)
			}
		}
	}
	if s.rests == nil {
		// a dealing step settled the provider's own record onto its routes (coolRests)
		if rest, ok := providerCoolRest(s, provider); ok && rest.Resting(s.Now) {
			consider(rest)
		}
	}
	return best, found
}

// coolingSaid is a provider's cooling as the tier's judgment names it.
func coolingSaid(r RouteRest) string {
	switch r.Cause {
	case coolUnfunded:
		return r.Provider + " unfunded (the owner's to fund)"
	case RestCredit:
		return r.Provider + " out of credit (the owner's to fund)"
	case RestProvider:
		return r.Provider + " failing (5xx or timeout) until " + r.UntilSaid()
	}
	return r.Provider + " rate-limited until " + r.UntilSaid()
}

// nextInLine is the route a fallback from the route from takes for member on tier: the
// first route after from in the tier's array (wrapping round) that is in line (1) for a
// draw, the provider faulted counted as cooling though its record is not yet written
// (ProviderFallback.tla Fallback, Pick: the first route in line).
func (s *Snapshot) nextInLine(tier, member, from, faulted string) (Route, bool) {
	arr := s.tierArray(tier)
	start := slices.Index(arr, from) + 1 // 0 when from is not in the array
	for i := range arr {
		name := arr[(start+i)%len(arr)]
		r, ok := s.servedRoute(tier, member, name, true)
		if !ok || name == from {
			continue
		}
		if r.Provider == faulted && !fallbackSeam(s, "nocooling") {
			continue
		}
		return r, true
	}
	return Route{}, false
}

// TierCooling is the wait of a tier whose every route is out of line for its provider's
// cooling (3): the one judgment's words naming each provider and its cooling, the
// server-computed deadline (the earliest end of a cooling that ends by itself, else
// FallbackRecheck from now), and ok. ok is false while any enabled route of the tier's array
// serves, or none of them cools (a tier merely rested keeps its own words, tierServed).
// ProviderFallback.tla WaitHasDeadline.
func (s *Snapshot) TierCooling(tier string) (why string, deadline time.Time, ok bool) {
	why, deadline, ok, _ = s.tierCooling(tier)
	return why, deadline, ok
}

// tierCooling is TierCooling, and novel: some provider's cooling is one the deal's own words
// do not already say (unfunded, or the provider's own cool record). A tier out of line only
// for its routes' rests keeps the tier's rest judgment, and one out of line only for
// rate-limit backoffs stays a wait that ends by itself, never a judgment (the owner,
// 2026-10-11; tla/RateBackoff.tla BackoffEnds): those words are the deal's as before.
func (s *Snapshot) tierCooling(tier string) (why string, deadline time.Time, ok, novel bool) {
	if len(s.Routes) == 0 {
		return "", time.Time{}, false, false
	}
	// the first pass decides, allocating nothing: the deal's per-card check (routeServed)
	// asks it of every ready primary each tick
	// the tier's array as tierArray reads it, walked in place (its default, the tier's
	// enabled routes, would build a slice per card)
	names := s.Tiers[tier]
	n := len(names)
	if n == 0 {
		n = len(s.Routes)
	}
	cooled := false
	for i := 0; i < n; i++ {
		var r Route
		isRoute := false
		if len(names) > 0 {
			r, isRoute = s.enabledRouteOf(tier, names[i])
		} else {
			r = s.Routes[i]
			isRoute = r.Tier == tier && r.Enabled
		}
		if !isRoute {
			continue
		}
		c, cools := s.providerCooling(r.Provider)
		if !cools {
			if _, rests := s.resting(r.Name); rests {
				continue // rested for its own cause: out of line, but not the provider's cooling
			}
			return "", time.Time{}, false, false
		}
		cooled = true
		novel = novel || c.Cause == coolUnfunded || strings.HasPrefix(c.Why, coolWords)
		if !c.Open() && (deadline.IsZero() || c.Until.Before(deadline)) {
			deadline = c.Until
		}
	}
	if !cooled {
		return "", time.Time{}, false, false
	}
	var said []string
	seen := map[string]bool{}
	for _, name := range s.tierArray(tier) {
		r, isRoute := s.enabledRouteOf(tier, name)
		if !isRoute || seen[r.Provider] {
			continue
		}
		if c, cools := s.providerCooling(r.Provider); cools {
			seen[r.Provider] = true
			said = append(said, coolingSaid(c))
		}
	}
	if deadline.IsZero() {
		deadline = s.Now.Add(FallbackRecheck)
	}
	why = fmt.Sprintf("tier %s has no route in line: %s; its cards wait, never failed, until %s and are dealt by themselves when a provider stops cooling (docs/SPEC-SPRINT.md section 5, provider fallback)",
		tier, strings.Join(said, ", "), stamp(deadline))
	return why, deadline, true, novel
}

// enabledRouteOf is the enabled route of tier named name.
func (s *Snapshot) enabledRouteOf(tier, name string) (Route, bool) {
	for _, r := range s.Routes {
		if r.Name == name && r.Tier == tier && r.Enabled {
			return r, true
		}
	}
	return Route{}, false
}

// coolWrite is the guarded write of a provider's own cool record for a fault of class with
// the provider's line: its window from the provider's words (retryAfter), else
// RateBackoff(k), k one more than the record's while it was written within RateBackoffMax
// of its end, else 1 (RateBackoff.tla Backoff, BackoffBounded). A record already written in
// the plan for the provider is kept (one write per provider per plan).
func coolWrite(p *Plan, s *Snapshot, provider, class, line, card string) {
	if provider == "" || s.Fleet == nil {
		return
	}
	name := PropProviderCool(provider)
	for _, pw := range p.Props {
		if pw.Table == Fleet && pw.Name == name {
			return
		}
	}
	was, had := s.Fleet.Prop(name)
	k := 1
	if prev, ok := providerCoolRest(s, provider); ok && !s.Now.After(prev.Until.Add(RateBackoffMax)) {
		k = coolK(prev) + 1
	}
	d := RateBackoff(k)
	if ra, ok := retryAfter(line); ok {
		d = ra
	}
	cause, word := RestRateBackoff, "rate limited"
	if class == class5xx || class == classTimeout {
		cause, word = RestProvider, class
	}
	r := RouteRest{Provider: provider, At: s.Now, Until: s.Now.Add(d), Cards: []string{card}, Cause: cause,
		Why: fmt.Sprintf("%s %s (k=%d): the provider %s %s", coolWords, d, k, word, card)}
	p.Props = append(p.Props, PropWrite{Table: Fleet, Name: name, Value: r.value(), Was: was, WasAbsent: !had})
}

// creditRestWrite rests the provider for want of credit, as a refused take's record rests
// it (providerRestsDue), when a fault falls back and so leaves no take record: until the
// owner funds it (OpenUntil; balance.go and Funded end it). A rest that holds is kept.
func creditRestWrite(p *Plan, s *Snapshot, provider, line, card string) {
	if provider == "" || s.Fleet == nil {
		return
	}
	name := PropProviderRest(provider)
	for _, pw := range p.Props {
		if pw.Table == Fleet && pw.Name == name {
			return
		}
	}
	was, had := s.Fleet.Prop(name)
	if old, ok := parseRest(was); had && ok && old.Resting(s.Now) {
		return
	}
	r := RouteRest{Provider: provider, At: s.Now, Until: OpenUntil, Cards: []string{card}, Cause: RestCredit, Why: oneLine(line)}
	if b, ok := ProviderBalances(s.Fleet)[provider]; ok {
		r.Balance, r.HasBalance = b.Balance, true
	}
	p.Props = append(p.Props, PropWrite{Table: Fleet, Name: name, Value: r.value(), Was: was, WasAbsent: !had})
}

// faultProvider is the provider of a card's route: the route's, else its model line's.
func faultProvider(s *Snapshot, c *Card) string {
	if p := routeProvider(s, c.F(FieldRoute)); p != "" {
		return p
	}
	p, _, _ := strings.Cut(c.F(FieldModel), "/")
	return p
}

// fallbackMembers is the members a fallback of the work card c may deal it to, in order:
// its own member first (the take that faulted freed its lane), then each other member up
// that is not held by an environment fault, not quiet, on the card's bench, did not refuse it
// at staging, and has room (DealAhead times its width).
func fallbackMembers(s *Snapshot, c, pr *Card) []string {
	up := s.UpMembers()
	up = slices.DeleteFunc(up, s.EnvHeld)
	up = notQuiet(s, up)
	if b := Bench(pr); len(b) > 0 {
		up = onlyBench(up, b)
	}
	up = without(up, StagingRefusers(c))
	out := []string{}
	if slices.Contains(up, c.Row) {
		out = append(out, c.Row)
	}
	for _, m := range up {
		if m == c.Row {
			continue
		}
		if s.Fleet.Count(m, Ready)+s.Fleet.Count(m, Working) < DealAhead*s.Width(m) {
			out = append(out, m)
		}
	}
	return out
}

// fallbackUnit is (2) for a work take: the take c ended with a provider fault of class, and
// the card is dealt at once, in this step, on the next route in line, to a member that
// launches it (fallbackMembers): the same work card at the same attempt, its generation
// moved, no take ended, no redeal and no MaxRateReturns spent, its primary left working
// (ProviderFallback.tla Fallback, FaultSpendsNothing). The fault's record is kept as the
// return keeps it (a rate-limited take's FieldRateTake, so the route backs off; the provider
// line), and the provider's cooling is written in the plan (coolWrite for a 5xx or timeout,
// creditRestWrite for out of credit). ok is false when no route is in line for any member:
// the caller keeps today's return (rateLimitedReturned), the card waiting as (3) says.
func fallbackUnit(p *Plan, s *Snapshot, c, pr *Card, r FinishReq, class string) (Unit, bool) {
	if IsFriendRow(c.Row) || pr == nil || c.F(FieldRoute) == "" || c.F(FieldRoute) == RoutePin {
		return Unit{}, false
	}
	tier := c.F(FieldTier)
	if tier == "" {
		return Unit{}, false
	}
	faulted := faultProvider(s, c)
	ss, _ := s.withRests()
	ss = ss.withBudgets()
	var route Route
	member := ""
	for _, m := range fallbackMembers(ss, c, pr) {
		if rt, ok := ss.nextInLine(tier, m, c.F(FieldRoute), faulted); ok {
			route, member = rt, m
			break
		}
	}
	if member == "" {
		return Unit{}, false
	}
	line := cutText(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(r.Report, cardhdr.EndProvider), ":")), MaxProviderErrorBytes)
	set := nextGen(c, member, s.Now)
	set[FieldProviderError] = line
	unset := []string{"taken", FieldLane, FieldLaneAt, "withdrawn", FieldTakeEnded, FieldDecided, FieldDecidedUsed}
	dealt, taken := takeStamps(c)
	rec := costRecord(s, r.Usage, c.F(FieldRoute), c.F(FieldModel), false, dealt, taken)
	if class == classRateLimited {
		// the rate-limited take's own record: the route backs off from it (rateBackoffs),
		// and with it, its provider (providerCooling)
		rates, _ := RateTakes(c)
		set[FieldRateTake+itoa(len(rates)+1)] = ProviderTake{Route: c.F(FieldRoute), Model: c.F(FieldModel), Member: c.Row,
			Finished: stamp(s.Now), Error: line, Taken: taken}.String()
	}
	if fallbackSeam(s, "spend") {
		// the reversed witness: the fault counted as an ended take (FaultSpendsNothing)
		set[FieldTakeEnded] = stamp(s.Now)
		set["redeals"] = itoa(c.Int("redeals") + 1)
		unset = slices.DeleteFunc(unset, func(k string) bool { return k == FieldTakeEnded })
	}
	fields := ss.routeFields(pr, route, tier)
	work, primary := splitRoute(fields)
	ss.dealDeadline(member, work)
	for k, v := range work {
		if v == "" {
			unset = append(unset, k)
			continue
		}
		set[k] = v
	}
	for _, k := range []string{FieldRateRPM, FieldRateConcurrent} {
		if _, ok := work[k]; !ok && c.F(k) != "" {
			unset = append(unset, k)
		}
	}
	prSet := map[string]string{}
	maps.Copy(prSet, primary)
	if r.Usage != "" {
		addConsumer(pr, prSet, workConsumer(s, c, 0, class, rec))
	}
	switch class {
	case classRateLimited, class5xx, classTimeout:
		coolWrite(p, s, faulted, class, line, c.ID)
	case RestCredit:
		creditRestWrite(p, s, faulted, line, c.ID)
	}
	return Unit{Key: c.ID, Stream: pr.Row, Changes: []Change{
		change(Fleet, moveEntry(c, member, Ready, set, unset...)),
		change(Work, setEntry(pr, prSet)),
	}, Moved: fmt.Sprintf("%s working -> ready gen=%d member=%s, the provider faulted the take on %s (%s): fallen back at once to %s (the same attempt, nothing spent)",
		c.ID, c.Int("gen")+1, member, c.F(FieldRoute), class, route.Name)}, true
}

// readFallbackSet is (2) for a read: the read card c, returned with a provider fault of
// class, is asked again at once of its own reader on the next route in line of its tier
// after the route it ran on (nextInLine, the faulted provider out), never counted as a
// return of its reader (no FieldReasked, no FieldReturned: the reader is not spent and its
// ok% is not touched), and the provider's cooling is written in the plan (coolWrite, or
// creditRestWrite for out of credit). ok is false when no route of the tier is in line: the
// caller keeps today's return. tla/ProviderFallback.tla Fallback, FaultSpendsNothing.
func readFallbackSet(p *Plan, s *Snapshot, c, pr *Card, reason, class string) (map[string]string, bool) {
	if pr == nil || c.F(FieldRoute) == "" || c.F(FieldRoute) == RoutePin {
		return nil, false
	}
	faulted := faultProvider(s, c)
	tier := cmpOr(c.F(FieldTier), s.readTierOf(pr))
	ss, _ := s.withRests()
	ss = ss.withBudgets()
	r, ok := ss.nextInLine(tier, "", c.F(FieldRoute), faulted)
	if !ok {
		return nil, false
	}
	switch class {
	case RestCredit:
		creditRestWrite(p, s, faulted, reason, c.ID)
	default:
		coolWrite(p, s, faulted, class, reason, c.ID)
	}
	set := ss.readRouteFields(r, tier)
	set["asked"] = stamp(s.Now)
	return set, true
}
