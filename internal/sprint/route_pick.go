package sprint

import (
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardcost"
)

// routeRand is the weighted draw's source: a *rand.Rand the caller can seed
// (routeRand.Seed), so a test pins the deal's counts. The process-global
// math/rand source's top-level Seed is a no-op on this tree's Go (go.mod
// go 1.26.6), so the draw holds its own seedable source; seeded from the clock
// once, the machine's draws vary run to run.
var routeRand = rand.New(rand.NewSource(time.Now().UnixNano()))

// routeShareFloor is the smallest share a route can hold in a draw, so a route
// whose measured outcome is poor — a high cost per landed card, a low ok rate,
// or no landings at all — is still sampled. A floor of 0.1 leaves a route that
// never wins a weighted draw a better-than-1-in-1000 chance over a hundred
// picks; a lower floor (0.01) left a route unsampled in 37% of 100-pick runs.
const routeShareFloor = 0.1

// routeShareScale is the cost-per-landed value at which a route's ok-rate term
// of its share halves. $10 per landed card is a flash route's rough target
// spend, so a route that lands a card for $10 keeps half its ok-rate share and
// one that lands for $30 a third. Naming the dollar figure keeps the fall of
// the share as cost rises explicit in the model (tla/RouteIndex.tla, Share).
const routeShareScale = 10.0

// routeShare is the share of new takes one route draws: the floor, plus the
// share of the remaining (1 - floor) its ok rate earns, shrunk as its cost per
// landed card rises. okRate is OK over attempts (RouteStats); costPerLanded is
// the route's total charged spend on work takes divided by its DoneOK landings.
// A route with no landings has no measured cost to divide by and holds the
// floor, as the rule says. The rule is docs/SPEC-SPRINT.md, the deal, "route
// share"; the model is tla/RouteIndex.tla (Share, and the
// FloorKeepsEveryRouteSampled and ShareFallsWithCost invariants).
func routeShare(stats RouteStat, landings int, spend string) float64 {
	if landings == 0 {
		return routeShareFloor
	}
	okRate := 0.0
	if stats.Attempts > 0 {
		okRate = float64(stats.OK) / float64(stats.Attempts)
	}
	costPerLanded := 0.0
	if f, err := strconv.ParseFloat(spend, 64); err == nil {
		costPerLanded = f / float64(landings)
	}
	return routeShareFloor + (1-routeShareFloor)*okRate/(1+costPerLanded/routeShareScale)
}

// routeOutcome is one route's measured outcome over its work takes: its DoneOK
// landings and the charged spend of every take (the COST records' charged
// figure, CardCostOf(c).Total.Charged, as cost_view.go sums the same figure).
type routeOutcome struct {
	landings int
	spend    []string
}

// chargedOf reads a producer's total charged figure off its COST TOTAL line
// (CardCostOf(c).Total.Charged is the same field) without building the
// CardCostView or its total's maps, so the deal's once-per-snapshot share walk
// allocates nothing per card.
func chargedOf(total string) string {
	rest := total
	for rest != "" {
		var w string
		if i := strings.IndexByte(rest, ' '); i >= 0 {
			w, rest = rest[:i], rest[i+1:]
		} else {
			w, rest = rest, ""
		}
		if v, ok := strings.CutPrefix(w, "charged_usd="); ok {
			return v
		}
	}
	return ""
}

// routeOutcomes is every route's landings and charged spend over the fleet's
// work cards, walked once. A route with no landings keeps its zero count, so
// the share holds it at the floor however much it spent.
func routeOutcomes(fleet *Table) map[string]*routeOutcome {
	m := map[string]*routeOutcome{}
	for _, c := range fleet.Column(Ready, Working, Finished, DoneOK, DoneFailed, DoneDefect, Withdrawn) {
		route := c.F(FieldRoute)
		if route == "" {
			continue
		}
		o := m[route]
		if o == nil {
			o = &routeOutcome{}
			m[route] = o
		}
		if c.Col == DoneOK {
			o.landings++
		}
		if v := chargedOf(c.F(FieldCostTotal)); v != "" {
			o.spend = append(o.spend, v)
		}
	}
	return m
}

// RouteShareMap computes each route's share of new takes from the routes and
// the fleet's ended work cards, once per tick on the tick's first read: the one
// source every draw reads. Every routeOf/drawRoute caller — the friends' ladder
// and the machines' deal, rebalance, redo, review and held — reads the one map,
// never a stale one. It reuses RouteStats (never reimplements it).
func RouteShareMap(routes []Route, fleet *Table) map[string]float64 {
	stats := RouteStats(routes, fleet)
	outcomes := routeOutcomes(fleet)
	shares := make(map[string]float64, len(stats))
	for _, st := range stats {
		spend := ""
		landings := 0
		if o, ok := outcomes[st.Route.Name]; ok {
			landings = o.landings
			if sum, ok := cardcost.Sum(o.spend...); ok {
				spend = sum
			}
		}
		shares[st.Route.Name] = routeShare(st, landings, spend)
	}
	return shares
}

// SetRouteShares computes each route's share of new takes from the routes and the
// fleet's ended work cards, once, and writes it onto each route record in place
// (Route.Share). The store computes it on the tick's first read (store/tick.go), so
// the shared route slice carries the one share the deal, the rebalance, the redo,
// the review and the held draws all read; a snapshot handed straight to a caller
// computes it on the first draw (ensureRouteShares), never per draw.
func SetRouteShares(routes []Route, fleet *Table) {
	m := RouteShareMap(routes, fleet)
	for i := range routes {
		routes[i].Share = m[routes[i].Name]
	}
}

// ensureRouteShares computes this snapshot's route shares once (SetRouteShares), the
// one source every draw reads, when they are not already set: the store sets them on
// the shared route slice at the tick's first read, and a snapshot handed straight to a
// caller (the in-memory store's tests) has them unset, so the first draw computes them.
// A computed share is never zero — the floor is 0.1 — so a zero Share marks the routes
// uncomputed and makes the check allocation-free for the steady tick's every card.
func (s *Snapshot) ensureRouteShares() {
	if len(s.Routes) == 0 || s.Routes[0].Share != 0 {
		return
	}
	SetRouteShares(s.Routes, s.Fleet)
}

// preferFirstWeighted is preferFirst (route.go) with a weighted draw: it walks
// the tier's array in place — no map or slice of served routes — counting
// positions for the index, and draws by the precomputed shares (Route.Share,
// SetRouteShares). First routes are still drawn first, and a card that already
// drew one takes the next; the index advances past every entry taken or skipped.
// The draw uses math/rand through the package's seedable source (routeRand), so a
// caller can seed it. The share is the route's own record's, the same for every
// caller; when every candidate shares equally (no data, or equal outcome) the walk
// is deterministic, as preferFirst's, and takes the first valid entry in array
// order, so the deal keeps its other rules even when the weights do not decide.
// The deal's rule is docs/SPEC-SPRINT.md, the deal, "route share"; the model is
// tla/RouteIndex.tla.
func preferFirstWeighted(arr []string, served func(string) (Route, bool), skip []string, hold bool, at uint64) (r Route, steps uint64, ok bool) {
	n := uint64(len(arr))
	take := func(wantFirst bool) (Route, uint64, bool) {
		// one pass gathers the first valid entry, its share, the total share and
		// whether every valid entry shares the first's, so the deterministic walk
		// never allocates and the weighted draw is a second pass only when the
		// shares differ.
		var firstIdx uint64
		firstShare, total := 0.0, 0.0
		equal, have := true, false
		for i := uint64(0); i < n; i++ {
			cur, ok := served(arr[(at+i)%n])
			if !ok || hold && slices.Contains(skip, cur.Name) {
				continue
			}
			if cur.First != wantFirst {
				continue
			}
			sh := cur.Share
			if !have {
				have, firstIdx, firstShare = true, i, sh
			} else if sh != firstShare {
				equal = false
			}
			total += sh
		}
		if !have {
			return Route{}, 0, false
		}
		if equal || total <= 0 {
			r, _ := served(arr[(at+firstIdx)%n])
			return r, firstIdx + 1, true
		}
		x := routeRand.Float64() * total
		cum := 0.0
		var last Route
		var lastIdx uint64
		for i := uint64(0); i < n; i++ {
			cur, ok := served(arr[(at+i)%n])
			if !ok || hold && slices.Contains(skip, cur.Name) {
				continue
			}
			if cur.First != wantFirst {
				continue
			}
			last, lastIdx = cur, i
			cum += cur.Share
			if cum > x {
				return cur, i + 1, true
			}
		}
		return last, lastIdx + 1, true
	}
	if r, steps, ok = take(true); ok {
		return r, steps, true
	}
	return take(false)
}
