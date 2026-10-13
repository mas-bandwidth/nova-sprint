package sprint

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardcost"
	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/pkg/decide"
	"github.com/mas-bandwidth/nova-sprint/pkg/harness"
)

// The card decides the model it runs on.
// Its brief's line 1 names a tier, and the deal resolves
// the tier through the routes nova-config applies to the store (the route kind,
// docs/nova-config/README.md) and the tier's route array (the tier kind: an ordered
// list of route names, a name repeated for more turns). The deal takes the array's
// entry at the tier's rolling index, a uint64 counter modulo the array's length,
// exactly as the deal takes a member (round.go), and moves the index by one for
// each card dealt. The route is written on the work card, so the packet hands the member
// the provider, model, budget and deadline it launches with. A `model:` header line
// pins the card, bypasses the array and leaves the index where it is. A redeal or a
// later attempt leaves out the routes already taken for the card while another
// remains: it takes the next entry that is not left out, and the index moves past
// the entries skipped. The model is tla/RouteIndex.tla; the route guard is
// tla/DirtyTick.tla's.
//
// The rules, in order:
//   - a pinned card runs on its pin (route "pin");
//   - a store with no route at all deals as before, with no route: the member runs
//     its own --model (a twin, a one-machine test);
//   - a frontier card with no pin is not dealt: it is the coordinator's, one
//     judgment for the tier;
//   - a card whose tier (flash when line 1 names none) has no enabled route in its
//     array is not dealt, one judgment for the tier;
//   - a route with first set is drawn before the others of its tier (preferFirst);
//   - a deal to a member draws only a route that member can launch (Launches): a route
//     whose harness is headless (claude, codex, grok) only when the member's control
//     card names it (fleet up --harnesses), else it is walked past as an unserved entry
//     is (fault 10, 2026-10-10; tla/RouteIndex.tla, NeverUnlaunchable).

// FieldHarnesses is a member's control card field: the headless harnesses
// (pkg/harness) its PATH holds and the deal may launch there, comma-separated, as
// fleet up --harnesses declares them; absent, the member runs opencode routes only.
const FieldHarnesses = "harnesses"

// Launches says the member can launch a card on route r: any route for a deal that
// names no member (a check of the tier alone), an opencode route anywhere, and a route
// of a headless harness only on a member whose control card names that harness.
func (s *Snapshot) Launches(member string, r Route) bool {
	if member == "" || !harness.IsHeadless(r.Harness) {
		return true
	}
	ctl := s.MemberCtl(member)
	return ctl != nil && slices.Contains(Split(ctl.F(FieldHarnesses)), r.Harness)
}

// HarnessesWhy is why a --harnesses word is refused, "" when it is one: "", HarnessesNone,
// or a comma list of headless harnesses (pkg/harness Headless).
func HarnessesWhy(words string) string {
	if words == "" || strings.TrimSpace(words) == HarnessesNone {
		return ""
	}
	for _, h := range Split(words) {
		if !harness.IsHeadless(h) {
			return "--harnesses names " + h + ", which is no headless harness (" + strings.Join(harness.Headless, ", ") + "; opencode runs on every member): a comma list of them, or " + HarnessesNone
		}
	}
	return ""
}

// launchersOf is the members of ms the deal can deal the primary c to: those for which
// routeOf draws a route (each a check, moving no index), wc the work card dealt again
// (nil for a new attempt); when none can, why is the first member's refusal. A member is
// chosen from these, never chosen first and then refused, so a card whose route only
// some members can launch goes to one of them (the second cold read of nova-tools#5576).
func (s *Snapshot) launchersOf(c, wc *Card, ms []string) (out []string, why string) {
	for _, m := range ms {
		if _, w, _ := s.routeServed(c, wc, m); w == "" {
			out = append(out, m)
		} else if why == "" {
			why = w
		}
	}
	return out, why
}

// dealPool is the members of up the deal may give the primary c, narrowed as dealPlan
// narrows them: its bench's alone (onlyBench, bench_deal.go), and, for a withdrawn attempt
// dealt again below its redeal bound, less the members that refused it at staging
// (StagingRefusers). wc is that withdrawn attempt (nil for a new attempt or an escalation,
// whose route is drawn afresh), refused the members taken out for it.
func (s *Snapshot) dealPool(c *Card, up []string) (pool []string, wc *Card, refused []string) {
	pool = onlyBench(up, Bench(c))
	if w := s.Fleet.Placed(WorkCardID(c.ID, c.Int("attempt"))); w != nil && w.Col == Withdrawn && !redealBound(w) {
		wc, refused = w, StagingRefusers(w)
		pool = without(pool, refused)
	}
	return pool, wc, refused
}

// noLauncher is why no member the deal may give the primary c (dealPool over up: its
// bench, less the members that refused it at staging) can launch a route of its tier,
// tier being routeOf's for ec, c as its next deal draws it (escalating), with routeOf's
// why "" and no friend serving it (the callers' checks): "" when one of them can, or when
// the pool is empty (no member up, its bench down, every member refused it: each its own
// hold or judgment). The tick raises it as the tier's one judgment (TickDeal, NNoRoute)
// and the no-stall rule names it (held.go), so a card whose routes all run under a harness
// no member of its pool names waits under a judgment, never quietly (the cold reads of
// nova-sprint#51; tla/RouteIndex.tla, StrandedIsJudged).
func (s *Snapshot) noLauncher(c, ec *Card, tier string, up []string) string {
	pool, wc, refused := s.dealPool(c, up)
	if len(pool) == 0 {
		return ""
	}
	if ms, _ := s.launchersOf(ec, wc, pool); len(ms) > 0 {
		return ""
	}
	// a member of the pool that refused the card's repository at staging for want of a
	// bench mirror (stagingRepoWhy) is why the judgment is raised: the mirror, what to
	// add and where, never unlaunchableWhy's generic harness sentence (the card: "the
	// no-launcher judgment says so with that why"). The owner, 2026-10-11: "cards only
	// may accrue per-fleet or friend, if there are not other machines/friends ready to
	// do that work right now!"
	if w := s.stagingRefusalWhy(ec, pool); w != "" {
		return w
	}
	return s.unlaunchableWhy(c, tier, pool, refused)
}

// unlaunchableWhy is the sentence for a primary c no member of pool can launch a route of
// tier for: the routes and their harnesses, the members, and what the coordinator can do,
// said for a bench card as its bench allows (it runs on no other member).
func (s *Snapshot) unlaunchableWhy(c *Card, tier string, pool, refused []string) string {
	var routes []string
	for _, r := range s.Routes {
		if r.Tier == tier && r.Enabled && harness.IsHeadless(r.Harness) {
			routes = append(routes, r.Name+" (runs under "+r.Harness+")")
		}
	}
	slices.Sort(routes)
	who := "no member up (" + strings.Join(pool, ", ") + ")"
	if len(Bench(c)) > 0 {
		who = "no member of its bench up (" + strings.Join(pool, ", ") + "; its BENCH line names " + strings.Join(Bench(c), ", ") + ", and it runs on no other member)"
	}
	if len(refused) > 0 {
		who += " that did not refuse it at staging (refused: " + strings.Join(refused, ", ") + ")"
	}
	do := "run nova-sprint fleet up <member> --harnesses <h,...> for each member with it on its PATH, or enable an opencode route of the tier"
	if len(Bench(c)) > 0 {
		do = "run nova-sprint fleet up <member> --harnesses <h,...> for a member of its bench with it on its PATH, brief it with a BENCH line naming a member that can, or enable an opencode route of the tier"
	}
	return who + " can launch a route of tier " + tier + ": " + strings.Join(routes, ", ") + ", and no such member's control card names that harness: " + do
}

// notLaunching is the members of up that cannot launch the work card c on the route it
// carries (Launches): what a move of a dealt card (a member down, the level, the
// rebalance), which keeps its route, avoids as it avoids a member that refused it.
func notLaunching(s *Snapshot, up []string, c *Card) []string {
	r := Route{Harness: c.F(FieldHarness)}
	var out []string
	for _, m := range up {
		if !s.Launches(m, r) || s.stagingRepoWhy(m, c) != "" {
			// a member that refused the card's repository at staging for want of a bench
			// mirror (stagingRepoWhy) is avoided as one that cannot launch its route is
			out = append(out, m)
		}
	}
	return out
}

// Route is one route of a model tier as the store holds it (config.RouteKey).
type Route struct {
	Name     string `json:"name"`
	Tier     string `json:"tier"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// Harness is the harness a card on it runs under (pkg/harness: opencode, the
	// default, or a headless program of the heavy tier); the member launches by it.
	Harness string `json:"harness,omitempty"`
	Tokens  int    `json:"tokens"` // 0 is unmetered
	// USD is the route's dollar budget per card, a canonical decimal ("0.5"), "" for none:
	// the harness's reported cost at which native stops the card (nova-tools #5094).
	USD      string `json:"usd,omitempty"`
	Deadline int    `json:"deadline"` // seconds
	Enabled  bool   `json:"enabled"`
	// First is the route's first field. A route with it set is drawn before the
	// others of its tier (preferFirst; docs/SPEC-SPRINT.md, the deal).
	First bool `json:"first"`
	// Prices is the route's price sheet (cardcost.PricesOf), what a card that ran on it
	// is priced by (cost.go); every price "" when the route has none.
	Prices cardcost.Prices `json:"prices"`
	// Share is the route's share of new takes, computed once per tick on the tick's
	// first read (route_pick.go, SetRouteShares) and carried on the shared route record
	// so every draw reads the one share (route.go, drawRoute). A zero Share is a route
	// whose share is not yet computed: a computed share is never zero, the floor (0.1)
	// is the least it holds.
	Share float64 `json:"-"`
}

// The work card's route fields, written at each deal and redeal: the route taken
// (or RoutePin), the model id the member launches (provider/model), its budget and
// its deadline in seconds; the primary's FieldRoutes is every route taken for it,
// in order, the exclusion's history.
const (
	FieldRoute    = "route"
	FieldModel    = "model"
	FieldTokens   = "tokens"
	FieldUSD      = "usd"
	FieldHarness  = "harness"
	FieldDeadline = "deadline"
	FieldRoutes   = "routes"
	FieldUsage    = "usage"
	RoutePin      = "pin"
	// FieldTier is the primary's tier when the coordinator pinned it to one (rework
	// --tier): every later deal and read of the card draws from it, over its brief's
	// line 1, and the machine never escalates it (cardTier, NextTier); on a work or read
	// card, the tier its route was drawn from, which its packet hands the child or reader,
	// its JOB.md names and its cost record keeps.
	FieldTier = "tier"
	// FieldTierNow is the tier the primary is on, written by every deal on a route:
	// flash first on every card, then the tier the machine escalated it to (NextTier);
	// the tier its brief's line 1 names is its ceiling. Add writes pro on a card whose
	// gate's measured wall is over the flash bound (gate_wall.go).
	FieldTierNow = "tier_now"
	// FieldReadFallbackNote is the note a read card carries when its read is drawn one
	// tier down: the card's tier no reader serves, so the read falls to the strongest
	// tier at or below it a reader up serves ("read at heavy: no frontier reader").
	FieldReadFallbackNote = "read_fallback_note"
	// FieldFallbackTier is the primary's fallback tier: the sprint's unserved-tier-falls-to
	// setting gave a card whose brief tier no route serves and no up friend serves the
	// strongest served tier at or below the setting (unserved_tier.go). It is a field of its
	// own, never the brief's line 1 and never a pin the seat made (FieldTier); the deal draws
	// its route from it and DealtTier names it, and the tick clears it when the brief tier is
	// served again and the card is not yet taken.
	FieldFallbackTier = "fallback_tier"
)

// modelRead is the memo of one brief's model lines (cardhdr.ReadModel): the model
// m it read and its why ("" when the lines read). ReadModel is a pure read of the
// brief, and the deal walks every ready primary each tick, resolving the tier of
// each more than once (routeOf in the machines' pass, then dealTierOf in the
// friends'), so a brief is parsed many times a tick; its parse is a regexp over
// line 1 with an allocation each call, and it is the deal's dominant cost at load,
// what keeps the tick past its one-second gate. The brief a card carries is
// immutable on the table (a re-cut writes a new brief), so one read a brief is
// every read.
type modelRead struct {
	m   cardhdr.Model
	why string
}

// modelCache is ReadModel memoized by brief: each parsed brief's model, kept for the
// process (a brief never names a different model once written), bounded by the number
// of distinct briefs the sprint has ever held, never by the number of ticks that read
// them.
var modelCache sync.Map // brief string -> modelRead

// modelReads counts the briefs ReadModel was actually parsed for (a memo miss), a
// diagnostic for the tick's cost gate: TestTheTickDealReadsEachBriefOnceAtScale holds
// it to the number of distinct briefs the deal walked, whatever the number of parts
// that resolved one card's tier in the tick (routeOf, then the friends' dealTierOf).
// The machine's own path never reads it.
var modelReads atomic.Int64

// resetModelMemo empties the brief memo and its parse counter, so a test that asserts
// on the count starts from a clean process whatever a prior test or run cached (a memo
// is a process global; a snapshot of the memo's age would read the count of its
// neighbours). The machine's own run never resets it.
func resetModelMemo() {
	modelCache.Clear()
	modelReads.Store(0)
}

// modelOf is a card's model lines, read once per brief: every later read of the same
// brief is the memo, so the deal, the ask and the cap deal that all resolve one card's
// tier in a tick parse it once, not once each.
func modelOf(c *Card) (cardhdr.Model, string) {
	brief := c.F("brief")
	if v, ok := modelCache.Load(brief); ok {
		r := v.(modelRead)
		return r.m, r.why
	}
	m, why := cardhdr.ReadModel(brief)
	modelCache.Store(brief, modelRead{m: m, why: why})
	modelReads.Add(1)
	return m, why
}

// PropRouteIndex is the fleet table's property that holds the tier's route index
// (route_index_flash, route_index_pro), beside deal_index: a uint64 counter read
// with the step's tables and written in its batch, guarded on the value read
// (round.go, roundWrites).
func PropRouteIndex(tier string) string { return "route_index_" + tier }

// NNoRoute is the tick's judgment of a tier no route serves, once per tier (its
// subject is the tier's, StreamSubject(TierSubject(tier))), closed when the tier is
// served or no card of it waits.
const NNoRoute = "no route serves the tier"

// TierSubject is the stream word a tier's judgment is filed under: no stream id has
// a colon, so it is never a stream's.
func TierSubject(tier string) string { return "tier:" + tier }

// noRoute is why a primary's tier is not served: "" when a route serves it (or the store
// has no route at all) or a friend up does (tierServed), else the tier it is judged under
// and the sentence. It moves no index.
func (s *Snapshot) noRoute(c *Card) (tier, why string) {
	tier, why, byFriend := s.routeServed(c, nil, "")
	if byFriend {
		return tier, ""
	}
	return tier, why
}

// tierServed is the one check of every verb that validates a tier, asked once no enabled
// route of it can be drawn (rested: the tier's routes resting, said first). A tier is
// served by an enabled fleet route or by a friend up (friendDealable: not held, not down)
// whose row lists it (friendTakes; the owner, 2026-10-04: "Fleet flash only; pro to
// friends"). up is the friends up who serve it: its cards are then the friends' deal's,
// never a machine's, and why says so to a machine's deal. With none, why is the refusal,
// naming both ways and the friends whose row lists the tier but who are held or down.
func (s *Snapshot) tierServed(tier string, rested []string) (up []string, why string) {
	var off []string
	for _, f := range s.Friends {
		switch {
		case !friendTakes(s, f, tier):
		case friendDealable(s, f):
			up = append(up, f.Name)
		default:
			off = append(off, f.Name)
		}
	}
	if len(up) > 0 {
		return up, "no enabled route serves tier " + tier + " on a machine; a friend up serves it (" + strings.Join(up, ", ") + "): the friends' deal deals it"
	}
	why = "no enabled route and no up friend serves tier " + tier + ": enable a route (nova-config route add <name> --tier " + tier + " ..., name it in nova-config tier set " + tier + " --routes <name,...>, then nova-config apply) or bring up a friend whose row lists " + tier + "; or pin the card with a model: <provider>/<model> line"
	if len(rested) > 0 {
		why = "tier " + tier + " cannot be dealt: every route rests (" + strings.Join(rested, "; ") + ") and no up friend serves it: the deal draws one when its rest ends; or run nova-config route add <name> --tier " + tier + " ..., bring up a friend whose row lists " + tier + ", or pin the card with a model: <provider>/<model> line"
	}
	for _, side := range [][3]string{{PropFleetTiers, "the fleet's", "--fleet-tiers"}, {PropFriendsTiers, "the friends'", "--friends-tiers"}} {
		if !s.sideTakes(side[0], tier) {
			v, _ := s.Work.Prop(side[0])
			why += " (" + side[1] + " tiers are " + v + ": nova-sprint set " + side[2] + " all, or a list with " + tier + ")"
		}
	}
	switch n := len(off); {
	case n > 0 && s.FriendsOff():
		why += " (the friends' work is off: nova-sprint set --friends on)"
	case n == 1:
		why += " (" + off[0] + " serves " + tier + " but is held/down)"
	case n > 1:
		why += " (" + strings.Join(off[:n-1], ", ") + " and " + off[n-1] + " serve " + tier + " but are held/down)"
	}
	return nil, why
}

// tierArray is the tier's route array as the deal reads it: the tier kind's list
// when nova-config applied one, else the tier's enabled routes in name order, each once
// (tla/RouteIndex.tla, Arr).
func (s *Snapshot) tierArray(tier string) []string {
	if a := s.Tiers[tier]; len(a) > 0 {
		return a
	}
	var out []string
	for _, r := range s.Routes {
		if r.Tier == tier && r.Enabled {
			out = append(out, r.Name)
		}
	}
	return out
}

// TierNotLive reports whether a tier has no live route: its route array is not empty,
// every route in it rests at s.Now, and no up friend serves it.
// tla/SprintRules.tla OneTierJudgment, QuietWhileUnfunded.
func (s *Snapshot) TierNotLive(tier string) (reasons []string, ok bool) {
	arr := s.tierArray(tier)
	if len(arr) == 0 {
		return nil, false
	}
	s, _ = s.withRests()
	for _, f := range s.Friends {
		if friendTakes(s, f, tier) && friendDealable(s, f) {
			return nil, false
		}
	}
	var out []string
	for _, name := range arr {
		rest, ok := s.resting(name)
		if !ok || !rest.Resting(s.Now) {
			return nil, false
		}
		out = append(out, name+" until "+rest.UntilSaid()+": "+rest.Said())
	}
	return out, true
}

// CardTierBlocked reports whether card c (a primary or work card) is blocked
// because its escalating tier has no live route.
// tla/SprintRules.tla QuietWhileUnfunded.
func (s *Snapshot) CardTierBlocked(c *Card) bool {
	if c == nil {
		return false
	}
	pr := c
	if p := c.F("primary"); p != "" {
		if placed := s.Work.Placed(p); placed != nil {
			pr = placed
		} else if card := s.Work.Card(p); card != nil {
			pr = card
		}
	}
	ec := escalating(s, pr)
	tier := cardTierOf(ec)
	if tier == "" {
		return false
	}
	_, notLive := s.TierNotLive(tier)
	return notLive
}

// everyHeldCardTierBlocked reports whether every card in cards is tier-blocked.
// tla/SprintRules.tla QuietWhileUnfunded.
func (s *Snapshot) everyHeldCardTierBlocked(cards []string) bool {
	if len(cards) == 0 {
		return false
	}
	for _, id := range cards {
		c := s.Work.Card(id)
		if c == nil {
			c = s.Fleet.Card(id)
		}
		if c == nil || !s.CardTierBlocked(c) {
			return false
		}
	}
	return true
}

// TierBlockedStreams returns a map of streams whose every ready or waiting primary
// is tier-blocked and which have no cards working, in review, or merging.
// tla/SprintRules.tla QuietWhileUnfunded.
func TierBlockedStreams(s *Snapshot) map[string]string {
	if s == nil || s.Work == nil {
		return nil
	}
	out := map[string]string{}
	for _, st := range s.Streams() {
		// Nothing working, in review, or merging
		if len(s.Work.Cell(st, Working)) > 0 || len(s.Work.Cell(st, Review)) > 0 || len(s.Work.Cell(st, Merging)) > 0 {
			continue
		}
		ready := s.Work.Cell(st, Ready)
		waiting := s.Work.Cell(st, Waiting)
		var blockedTier string
		allBlocked := true
		count := 0
		for _, c := range append(ready, waiting...) {
			if IsSentinel(c) {
				continue
			}
			count++
			ec := escalating(s, c)
			tier := cardTierOf(ec)
			if tier == "" {
				allBlocked = false
				break
			}
			if _, notLive := s.TierNotLive(tier); !notLive {
				allBlocked = false
				break
			}
			if blockedTier == "" {
				blockedTier = tier
			}
		}
		if allBlocked && count > 0 {
			out[st] = blockedTier
		}
	}
	return out
}

// TierBlockedStreams is the snapshot's tier-blocked streams.
func (s *Snapshot) TierBlockedStreams() map[string]string { return TierBlockedStreams(s) }

// routeIndex is one tier's rolling index over its route array (tla/RouteIndex.tla,
// ridx): a counter round (round.go) in steps, and the steps each unit moved it.
type routeIndex struct {
	r     *round
	moves roundMoves
}

// routeIndexes are the tiers' route indexes a dealing step moves, by tier.
type routeIndexes map[string]*routeIndex

// routeIndexesOf is the route indexes at the counters the fleet table's properties hold.
func routeIndexesOf(s *Snapshot) routeIndexes {
	out := routeIndexes{}
	for _, t := range tierLadder {
		r := tableRound(s.Fleet, PropRouteIndex(t), nil)
		r.steps = true
		out[t] = &routeIndex{r: r, moves: roundMoves{}}
	}
	return out
}

// write writes where the plan's kept units left each tier's index, in its batch
// (roundWrites; tla/RouteIndex.tla, Deal and Redeal).
func (ri routeIndexes) write(p *Plan) {
	for _, t := range tierLadder {
		roundWrites(p, ri[t].r, ri[t].moves)
	}
}

// routeByName is the route in served named name; ok false when none is. It is the reads'
// lookup over a slice (readRouteOf), which a map used to be: a lookup over the tier's few
// routes allocates no map. The deal's own lookup is servedRoute, which scans the store's
// routes directly so it builds no map or slice per ready primary each tick.
func routeByName(served []Route, name string) (Route, bool) {
	for _, r := range served {
		if r.Name == name {
			return r, true
		}
	}
	return Route{}, false
}

// preferFirst is the entry a draw takes from the tier's array at counter at.
// A route with first set is drawn before the others of its tier: the walk takes
// the first served entry whose first is set, and an entry without it only when
// none such remains drawable. steps is every entry walked to reach the one
// taken, and the amount the index moves (docs/SPEC-SPRINT.md, the deal).
// RouteFair (tla/RouteIndex.tla) is this walk when no route of the tier has
// first set. hold leaves skip out while another entry remains drawable, as a
// redeal leaves out routes already taken. served is the name lookup: the deal's
// (servedRoute) scans the store's few routes so it builds no map or slice per
// ready primary each tick, which routeOf's map did (the deal's dominant
// allocation at load); the reads' (routeByName) looks a slice up.
func preferFirst(arr []string, served func(string) (Route, bool), skip []string, hold bool, at uint64) (r Route, steps uint64, ok bool) {
	n := uint64(len(arr))
	take := func(wantFirst bool) (Route, uint64, bool) {
		for i := uint64(0); i < n; i++ {
			cur, ok := served(arr[(at+i)%n])
			if !ok || hold && contains(skip, cur.Name) {
				continue
			}
			if cur.First != wantFirst {
				continue
			}
			return cur, i + 1, true
		}
		return Route{}, 0, false
	}
	if r, steps, ok = take(true); ok {
		return r, steps, true
	}
	return take(false)
}

// routeOf is the route fields of one deal of the primary c; wc is the work card dealt
// again (nil for a new attempt), whose own route is left out too. With ri the tier's
// index moves past the entry taken and every entry skipped before it, recorded under
// c's unit; nil reads the index and moves nothing (tla/RouteIndex.tla: Deal, Redeal, Pin).
// An entry that names no enabled route of the tier (a route disabled or removed since
// the array was set) is skipped as an excluded one is. member is the member the deal
// draws for: an entry whose route that member cannot launch (Launches) is skipped as an
// unserved one is, and a tier with no route it can launch is not dealt to it, why naming
// the routes and the member; "" draws every route (a check of the tier alone).
func (s *Snapshot) routeOf(c, wc *Card, ri routeIndexes, member string) (set map[string]string, tier, why string, byFriend bool) {
	tier, why, byFriend, done, m := s.routeTier(c)
	if done {
		return nil, tier, why, byFriend
	}
	if w := s.stagingRepoWhy(member, c); w != "" {
		return nil, tier, w, false
	}
	if m.Pin != "" {
		return map[string]string{FieldRoute: RoutePin, FieldModel: m.Pin, FieldTokens: m.Tokens, FieldUSD: "", FieldDeadline: strconv.Itoa(m.Deadline)}, "", "", false
	}
	if len(s.Routes) == 0 {
		return nil, "", "", false
	}
	r, steps, ok, rested, unlaunchable, full := s.drawRoute(c, wc, ri, member, tier)
	if ok {
		if ri != nil {
			ri[tier].r.count += steps
			ri[tier].moves[c.ID] = strconv.FormatUint(steps, 10)
		}
		set := map[string]string{FieldRoute: r.Name, FieldModel: r.Provider + "/" + r.Model, FieldTokens: tokensWord(r.Tokens), FieldUSD: r.USD, FieldHarness: r.Harness,
			FieldDeadline: strconv.Itoa(r.Deadline), FieldTier: tier, FieldRoutes: strings.Join(append(Split(c.F(FieldRoutes)), r.Name), ",")}
		if !pinnedTier(c, m) {
			set[FieldTierNow] = tier // the primary is on the tier drawn (cardTier)
		}
		maps.Copy(set, rateFields(s, r))
		if ri != nil {
			s.budgets.dealt(r.Provider)
		}
		return set, tier, "", false
	}
	if len(full) > 0 {
		// a coordinator's rework or redo is never told it waits ready: the rework or redo
		// did not happen, and no deal resumes by itself, so the words say the card was not
		// dealt, name the backoff's end, and say to give it again after that time
		// (docs/SPEC-SPRINT.md section 5, the deal; tla/RateBackoff.tla, BackoffEnds). Only
		// a soft draw (a rework's or a redo's) reaches this: the soft fallback past a full
		// provider (drawRoute) never draws a route backing off, so a soft draw that fails
		// has full only of backoffs. A deal's own words are the waiting ones below.
		if s.budgets != nil && s.budgets.soft {
			return nil, tier, "every route of tier " + tier + " that serves is backing off from a rate limit (" + strings.Join(full, "; ") + "): a rework or redo is never dealt onto a backing-off route, so the card was not dealt; give it again after that time", false
		}
		// every route of the tier that serves is at its provider's budget: the card waits
		// ready, never an error, never an attempt spent, and the next deal draws it when a
		// slot frees (the owner, 2026-10-11: "if nothing else is available, you wait and
		// try again later"), and so does every route backing off from a rate limit
		// (unservedWhy: a backoff ends by itself; tla/RateBackoff.tla)
		return nil, tier, "every route of tier " + tier + " that serves is at its provider's concurrency budget or backing off from a rate limit (" + strings.Join(full, "; ") + "): the card waits ready and is dealt when a slot frees or the backoff ends", false
	}
	up, why := s.tierServed(tier, rested)
	if len(up) == 0 && len(unlaunchable) > 0 {
		why = "member " + member + " can launch no route of tier " + tier + " that serves: " + strings.Join(unlaunchable, ", ") + " and its control card names no such harness (fleet up " + member + " --harnesses <h,...> declares the ones on its PATH); the deal deals it to a member that can"
	}
	return nil, tier, why, len(up) > 0
}

// routeServed is routeOf's check: whether a route serves a deal of c to member, and why
// none does, without the route fields. The tick's machines' pass (TickDeal) and the checks
// that pick a member (launchersOf) and name the tier's judgment (noRoute) walk every ready
// primary each tick asking only whether a route is drawn, never its fields, so they pay no
// set; routeOf builds the fields only for the callers that deal (deal, redeal, the
// rebalance), which move at most the width's cards a tick. A full provider (rate_budget.go)
// is never a judgment here, as in routeOf's check of the tier alone.
func (s *Snapshot) routeServed(c, wc *Card, member string) (tier, why string, byFriend bool) {
	tier, why, byFriend, done, m := s.routeTier(c)
	if done {
		return tier, why, byFriend
	}
	if w := s.stagingRepoWhy(member, c); w != "" {
		return tier, w, false
	}
	if m.Pin != "" {
		return "", "", false // a pinned card runs on its pin: served
	}
	if len(s.Routes) == 0 {
		return "", "", false
	}
	if _, _, ok, rested, unlaunchable, _ := s.drawRoute(c, wc, nil, member, tier); ok {
		return tier, "", false
	} else {
		up, why := s.tierServed(tier, rested)
		if len(up) == 0 && len(unlaunchable) > 0 {
			why = "member " + member + " can launch no route of tier " + tier + " that serves: " + strings.Join(unlaunchable, ", ") + " and its control card names no such harness (fleet up " + member + " --harnesses <h,...> declares the ones on its PATH); the deal deals it to a member that can"
		}
		return tier, why, len(up) > 0
	}
}

// routeTier is routeOf's and routeServed's shared opening: the tier a deal of c draws
// from, and the cases that end before the draw — a brief whose model lines do not read, a
// frontier card, and a tier the fleet's set leaves out (tierServed). done says one of
// those settled the answer (the returned tier, why and byFriend are it, with no set); else
// the caller goes on to the pin, a store with no route, or the draw. m is the card's
// model, for the pin's fields and the tier a deal writes on the primary.
func (s *Snapshot) routeTier(c *Card) (tier, why string, byFriend, done bool, m cardhdr.Model) {
	var bad string
	m, bad = modelOf(c)
	tier = drawTier(c, m)
	if tier == "" {
		tier = s.startTier(c, m) // its first deal on a route: flash first, or its grade's pro (decide.go)
	}
	if bad != "" {
		tier = ceilingTier(c, m)
		// a card admitted before the lint read its lines: judged under the tier it
		// names (an unknown word too), else flash's
		return tier, "its brief's model lines: " + bad, false, true, m
	}
	if fb := fallbackTier(c, m); fb != "" {
		// the sprint's unserved-tier-falls-to setting gave the card a fallback tier
		// (unserved_tier.go): the deal draws from it, never the unserved brief tier
		tier = fb
		if !s.FleetTakes(tier) {
			up, why := s.tierServed(tier, nil)
			return tier, why, len(up) > 0, true, m
		}
		return tier, "", false, false, m
	}
	if tier == cardhdr.RouteFrontier && m.Pin == "" {
		// By design a frontier card is the friends' (docs/SPEC-SPRINT.md, tiers and
		// routes): a friend up whose row lists frontier serves it as a route would
		// (tierServed), and the friends' deal deals it. With no friend up and a store that
		// holds routes, it waits for the coordinator; the refusal names the routes it
		// looked at and the friends whose row lists frontier but who are held or down
		// (2026-10-11: the coordinator's own friend row was up at width 8 with
		// tiers=frontier while rework of 36 frontier cards was refused for want of a route).
		up, why := s.tierServed(tier, nil)
		if len(up) > 0 {
			return tier, why, true, true, m
		}
		if len(s.Routes) > 0 {
			return tier, "no route and no friend serves tier frontier; a frontier card waits for the coordinator: run it, or pin it with a model: <provider>/<model> line (" + why + ")", false, true, m
		}
	}
	if !s.FleetTakes(tier) {
		// the fleet's tiers leave it out (set --fleet-tiers): no machine draws it, whatever
		// its routes or a model pin (the set is the owner's switch); the friends' deal deals
		// it when a friend up serves it
		up, why := s.tierServed(tier, nil)
		return tier, why, len(up) > 0, true, m
	}
	return tier, "", false, false, m
}

// drawRoute is the draw from the tier's array for one deal of c to member: the entry at
// the tier's rolling index, past the entries skipped (preferFirst), the index moved past it
// (ri, when given). ok says a route is drawn; else rested and unlaunchable name the routes
// left out, and full the routes at their provider's concurrency budget (only when ri is
// given, a draw; a check of the tier alone never judges a full provider), for the tier's
// judgment. It builds no set of route fields and no map or slice of served routes
// (servedRoute scans the store's few), so the tick's per-card walk pays for none of the
// allocations routeOf's served map and set once did (the deal's dominant cost at load).
func (s *Snapshot) drawRoute(c, wc *Card, ri routeIndexes, member, tier string) (r Route, steps uint64, ok bool, rested, unlaunchable, full []string) {
	arr := s.tierArray(tier)
	drawn := Split(c.F(FieldRoutes))
	if wc != nil && wc.F(FieldRoute) != "" {
		drawn = append(drawn, wc.F(FieldRoute))
	}
	drawing := ri != nil
	served := func(name string) (Route, bool) { return s.servedRoute(tier, member, name, drawing) }
	// every entry served is left out: the exclusion lapses, as when the tier has one route
	fresh := false
	for _, name := range arr {
		_, isServed := served(name)
		fresh = fresh || isServed && !contains(drawn, name)
	}
	var at uint64
	if drawing {
		at = ri[tier].r.count
	} else if v, ok := s.Fleet.Prop(PropRouteIndex(tier)); ok {
		at, _ = strconv.ParseUint(v, 10, 64)
	}
	// the per-route shares were computed once on the tick's first read and shared
	// by every part's snapshot (RouteShareMap, route_pick.go); this draw reads them.
	// A snapshot handed straight to a caller computes them on the first draw.
	s.ensureRouteShares()
	r, steps, ok = preferFirstWeighted(arr, served, drawn, fresh, at)
	if !ok && drawing && s.budgets != nil && s.budgets.soft {
		// a coordinator's rework or redo is never refused for a full provider: with no route
		// of room it is dealt on a full one anyway, and the take holds it ready until a slot
		// frees (takeBudgetWhy)
		soft := func(name string) (Route, bool) {
			if rest, ok := s.resting(name); ok && rest.Cause == RestRateBackoff {
				return Route{}, false // a full provider is soft; a rate-limit backoff is not
			}
			return s.servedRoute(tier, member, name, false)
		}
		fresh = false
		for _, name := range arr {
			_, isServed := soft(name)
			fresh = fresh || isServed && !contains(drawn, name)
		}
		r, steps, ok = preferFirstWeighted(arr, soft, drawn, fresh, at)
	}
	if !ok {
		rested, unlaunchable, full = s.unservedWhy(tier, member, drawing)
	}
	return r, steps, ok, rested, unlaunchable, full
}

// servedRoute is the route named name when it serves a deal to member on tier: an
// enabled route of the tier that does not rest (route_rest.go) and, for a named member,
// that member can launch (Launches). It is the deal's name lookup, scanning the store's
// few routes, so no map or slice is built for every ready primary each tick, which the
// per-card served map did (the deal's dominant allocation at load). A draw (drawing) also
// leaves out a route whose provider is at its concurrency budget (rate_budget.go), as it
// leaves out a resting one; a check of the tier (not drawing) does not, so a full provider
// is never a judgment. A route disabled or removed since the tier's array was set, a
// resting one, and one the member cannot launch are not served.
func (s *Snapshot) servedRoute(tier, member, name string, drawing bool) (Route, bool) {
	for _, r := range s.Routes {
		if r.Name != name || r.Tier != tier || !r.Enabled {
			continue
		}
		if rest, ok := s.resting(r.Name); ok && (drawing || rest.Cause != RestRateBackoff) {
			// a rate-limit backoff is a stall, never an unserved tier: the draw leaves the
			// route out, a check of the tier does not, so a backoff is never a judgment, as a
			// full provider is not (a-rate-limited-refusal-never-fails-the-cardc;
			// tla/RateBackoff.tla)
			return Route{}, false
		}
		if !s.Launches(member, r) {
			return Route{}, false
		}
		if drawing {
			if isFull, _ := s.budgets.full(r.Provider); isFull {
				return Route{}, false
			}
		}
		return r, true
	}
	return Route{}, false
}

// backoffWait is the wait of a ready card of tier while a rate limit backs the tier off:
// at least one enabled route of the tier backs off (a rest of cause RestRateBackoff the
// dealing step settled, withRests) and no route of the tier can be drawn for any of
// members. The card is waiting out a stall that ends by itself, never stalled work and
// never a judgment (held.go, rebalance.go; tla/RateBackoff.tla, BackoffEnds). "" when no
// route of the tier backs off or one can be drawn.
//
// Drawable is decided with the deal's own view, never a looser one (docs/SPEC-SPRINT.md
// section 5, the deal): the walk is the tier's route array (tierArray), as drawRoute's is,
// so an enabled route the configured array leaves out is never counted; a route counts
// only when servedRoute serves it for a draw to one of members, the draw's own test (not
// resting for any cause, the member launches it, its provider has a slot); and the
// provider budgets are the dealing step's (withBudgets) when the caller holds none, since
// the no-stall rule and the rebalance test a snapshot the deal never budgeted. A nil plan
// reads as "no provider is full", which once counted a full provider as drawable and raised
// NStalled on a card the deal had just refused (rate-limit-never-fails-the-card-v2).
func (s *Snapshot) backoffWait(tier string, members []string) string {
	if s.budgets == nil {
		s = s.withBudgets() // a copy, its settled rests kept
	}
	var backs []string
	drawable := false
	for _, name := range s.tierArray(tier) {
		if rest, ok := s.resting(name); ok && rest.Cause == RestRateBackoff && s.enabledRoute(tier, name) {
			backs = append(backs, name+" until "+rest.UntilSaid())
			continue
		}
		for _, m := range members {
			if _, ok := s.servedRoute(tier, m, name, true); ok {
				drawable = true
				break
			}
		}
	}
	if drawable || len(backs) == 0 {
		return ""
	}
	return "waits for the rate-limit backoff on " + strings.Join(backs, ", ") + ": the deal resumes by itself"
}

// enabledRoute says name is an enabled route of tier (a configured tier array may name a
// route since disabled or moved; the deal never draws one, backoffWait never waits on one).
func (s *Snapshot) enabledRoute(tier, name string) bool {
	for _, r := range s.Routes {
		if r.Name == name && r.Tier == tier && r.Enabled {
			return true
		}
	}
	return false
}

// unservedWhy names the routes of tier no deal to member draws, for the tier's judgment
// when none serves: those resting (route_rest.go), those the member cannot launch
// (Launches), and those at their provider's concurrency budget (rate_budget.go; only for a
// draw, where a full provider is a judgment). It is built only when no route is drawn, so
// the per-card walk pays nothing for it.
func (s *Snapshot) unservedWhy(tier, member string, drawing bool) (rested, unlaunchable, full []string) {
	for _, r := range s.Routes {
		if r.Tier != tier || !r.Enabled {
			continue
		}
		if rest, ok := s.resting(r.Name); ok {
			if rest.Cause == RestRateBackoff {
				// a backoff waits as a full provider does: the card waits ready, never a
				// judgment (servedRoute)
				if drawing {
					full = append(full, r.Name+" ("+rest.Said()+", until "+rest.UntilSaid()+")")
				}
				continue
			}
			rested = append(rested, r.Name+" until "+rest.UntilSaid()+": "+rest.Said())
			continue
		}
		if !s.Launches(member, r) {
			unlaunchable = append(unlaunchable, r.Name+" (runs under "+r.Harness+")")
			continue
		}
		if drawing {
			if isFull, said := s.budgets.full(r.Provider); isFull {
				full = append(full, r.Name+" ("+said+")")
			}
		}
	}
	return rested, unlaunchable, full
}

// Flash first on every card (the owner, 2026-10-02, cost rule 1 of nova-tools#5174,
// agreed after "The cost of the sprint at $5,400 seems excessive.": "Flash first on
// every card; pro only on escalation"). The tier a brief's line 1 names is the card's
// ceiling, what it likely needs, never its first deal: every card is dealt on flash, and
// one that reaches its bound below its ceiling is escalated by the machine to the next
// tier of the ladder and dealt a new attempt there, no judgment raised; at its ceiling
// the bound is the coordinator's judgment as before. A frontier card is the
// coordinator's and is never dealt (it climbs no ladder); a pinned model runs on its pin;
// a tier the coordinator pinned (rework --tier, FieldTier) is the card's tier and its
// ceiling both.
var tierLadder = []string{cardhdr.RouteFlash, cardhdr.RoutePro, cardhdr.RouteHeavy}

// cardTier is the tier the primary c is on, what its reads, its read count and its
// escalation go by: the tier its last deal on a route drew (FieldTierNow, which every
// such deal writes: flash first, then the tier the machine escalated it to); before
// any (a card not dealt yet, or a store with no route, where its member runs its own
// model and no deal draws a tier) and for a pinned tier or model or a frontier card,
// its ceiling.
func cardTier(c *Card, m cardhdr.Model) string {
	if t := c.F(FieldTierNow); t != "" && !pinnedTier(c, m) {
		return t
	}
	return ceilingTier(c, m)
}

// drawTier is the tier the deal draws the primary c's route from: its ceiling when its
// tier is pinned (pinnedTier), else the tier the machine escalated it to, else "": its first
// deal on a route, whose tier is the snapshot's to say (startTier: flash first, or pro by
// its grade).
func drawTier(c *Card, m cardhdr.Model) string {
	if pinnedTier(c, m) {
		return ceilingTier(c, m)
	}
	if fb := fallbackTier(c, m); fb != "" {
		return fb
	}
	return c.F(FieldTierNow)
}

// pinnedTier says the primary c climbs no ladder: the coordinator pinned its tier
// (FieldTier, rework --tier), its brief pins a model, or it is a frontier card
// (unless the sprint gave it a fallback tier, unserved_tier.go: a fallback is no pin
// and the card is dealt on it).
func pinnedTier(c *Card, m cardhdr.Model) bool {
	if m.Pin != "" || c.F(FieldTier) != "" {
		return true
	}
	if m.Tier == cardhdr.RouteFrontier {
		return c.F(FieldFallbackTier) == ""
	}
	return false
}

// ceilingTier is the highest tier the machine escalates the primary c to: the tier the
// coordinator pinned, else the tier its brief's line 1 names, flash when it names none.
func ceilingTier(c *Card, m cardhdr.Model) string {
	if t := c.F(FieldTier); t != "" {
		return t
	}
	if t, ok := criticalTier(c, m); ok {
		return t // a critical card runs on pro from its first deal (weight.go)
	}
	if m.Tier == "" {
		return cardhdr.RouteFlash
	}
	return m.Tier
}

// CardTiers is the tier the primary is on (cardTier) and its ceiling, as `card` prints
// them.
func CardTiers(c *Card) (now, ceiling string) {
	m, _ := modelOf(c)
	return cardTier(c, m), ceilingTier(c, m)
}

// cardTierOf is the tier the primary c is on (cardTier).
func cardTierOf(c *Card) string {
	now, _ := CardTiers(c)
	return now
}

// DealTier is the one tier resolution of a card every friend decision reads
// (docs/SPEC-SPRINT.md section 1, "One tier for every friend decision",
// friend-deal-one-tier-bb.w2): the friends' deal, the level, the take back of a re-tiered
// card (retierTakeBacks) and the packet's tier (DealtTier, through the primary's tier
// field) all call it, so no two of them can disagree about a card. It is the tier a deal
// draws (dealTierOf), and the dealer's default, flash, when there is no primary.
func (s *Snapshot) DealTier(c *Card) string {
	if c == nil {
		return cardhdr.RouteFlash
	}
	return s.dealTierOf(c)
}

// dealTierOf is the tier a deal of the primary c draws, as the machines' deal draws it
// (routeOf): the tier it is on once a deal drew one (drawTier), its ceiling when its tier
// is pinned, and before its first deal its start tier (startTier: flash first, pro for a
// brief that says pro), never its ceiling; a brief whose model lines cannot be read is
// judged under its ceiling. A card with no tier line is flash. The friends' deal and
// level gate on it (friendTakes), so a card a machine would be dealt on flash is a
// flash friend's too (docs/SPEC-SPRINT.md section 1, a friend's card; 2026-10-06: a
// friend of tier flash sat empty for an hour while every brief whose line 1 said
// tier: heavy was read as heavy for her and dealt on flash to the machines).
func (s *Snapshot) dealTierOf(c *Card) string {
	m, bad := modelOf(c)
	if bad != "" {
		return ceilingTier(c, m)
	}
	if t := drawTier(c, m); t != "" {
		return t
	}
	return s.startTier(c, m)
}

// NextTier is the tier the primary c escalates to when it reaches its bound: the next tier
// of the ladder above the one it is on, up to its ceiling; "" at its ceiling (a pinned
// model or tier is on its ceiling, cardTier), for brief lines that cannot be read, and in
// a store with no route (a twin: its member runs its own model whatever the tier). One
// function decides it for every bound that escalates: the redeal bound (Deal) and the
// failure bounds.
func (s *Snapshot) NextTier(c *Card) string {
	m, bad := modelOf(c)
	if bad != "" || len(s.Routes) == 0 {
		return ""
	}
	i, top := slices.Index(tierLadder, cardTier(c, m)), slices.Index(tierLadder, ceilingTier(c, m))
	if i < 0 || i >= top {
		return ""
	}
	return tierLadder[i+1]
}

// readTierOf is the tier a primary's reads are drawn from: the tier of the work
// being read, as the deal draws it (cardTier; the owner, 2026-10-01: "i think
// readers being conservatively the same tier as the work being done seems
// fine?"), raised to the read tier set for its stream or the sprint when that is
// stronger (settings.go; nova-tools#5096 item 27), never lowered. A card that pins
// a model and names no tier is read on flash. A heavy card is read on heavy (on pro under
// the interim rule below: the owner, 2026-10-06 7:41 PM ET, "let pro do it"). A frontier
// card, a tier no route serves, is read on heavy, the strongest tier a route serves: a
// read on pro would be weaker than the writer, which item 27 refuses; the one weaker read is
// the interim rule's, a pro card read on flash while no enabled route serves pro and one
// serves flash (the owner, 2026-10-06: "let flash read pro"). The value returned
// is that collapse: a route drawn for the card is named from it. The deal measures every
// reader, friend or member, against the tier before this collapse (friendReadTier,
// readTierDistance), so a member whose read is drawn lower ranks by how far below it
// really reads (docs/SPEC-SPRINT.md section 6, who reads).
func (s *Snapshot) readTierOf(pr *Card) string {
	m, _ := modelOf(pr)
	t := cardTier(pr, m)
	if t == cardhdr.RouteFrontier {
		t = cardhdr.RouteHeavy
	}
	if set := s.readTierSetting(pr.Row); set != "" {
		t = stronger(t, set)
	}
	// Workaround (the coordinator, 2026-10-06 7:30 PM ET; the owner: 7:11 PM: "let flash read pro"): a pro
	// read is drawn on flash while no enabled route serves pro and one serves flash, so the
	// fleet's flash readers read pro cards while the pro routes are off. Read cards replace
	// this.
	if t == cardhdr.RoutePro && len(s.Routes) > 0 && !s.tierRouted(t) && s.tierRouted(cardhdr.RouteFlash) {
		t = cardhdr.RouteFlash
	}
	// Workaround (the owner, 2026-10-06 7:41 PM ET: "let pro do it"): a heavy card is read on pro,
	// so the fleet's pro readers are its second reader beside the heavy friend, who is asked
	// before this collapse (friendReadTier). Read cards replace this.
	if t == cardhdr.RouteHeavy {
		t = cardhdr.RoutePro
	}
	return t
}

// readTierDrawn is the tier a machine read of the primary is drawn on: readTierOf's
// collapse, and when no reader up serves that tier, the strongest tier at or below the
// card's own that a reader up serves — a card at a tier no reader serves is read one tier
// down (docs/SPEC-SPRINT.md section 6, who reads), so a frontier card is read at heavy
// while a heavy reader is up and no frontier reader is. Friends read at friendReadTier,
// never here, so the friend path is untouched. With no readers table (a core snapshot
// holds every reader up) it is readTierOf's own return.
func (s *Snapshot) readTierDrawn(pr *Card) string {
	t := s.readTierOf(pr)
	if s.anyReaderUpServes(t) {
		return t
	}
	own := friendReadTier(s, pr)
	if i := slices.Index(capLadder, own); i >= 0 {
		for j := i; j >= 0; j-- {
			if cand := capLadder[j]; s.anyReaderUpServes(cand) {
				return cand
			}
		}
	}
	return t
}

// anyReaderUpServes says a reader up serves tier: a fleet reader with its row's tiers,
// a friend's reader with its own model, exactly as readerServesTier decides for one
// reader. With no readers table (a core snapshot holds every reader up) it is always
// served, so the fallback above never fires there.
func (s *Snapshot) anyReaderUpServes(tier string) bool {
	if s.Readers == nil {
		return true
	}
	for _, r := range s.UpReaders() {
		if s.readerServesTier(r, tier) {
			return true
		}
	}
	return false
}

// readRouteOf is the route fields of one read card the ask creates for the
// primary pr: the read is drawn as a work card is, from the array of pr's read tier
// (readTierOf, written on the read card as FieldTier) at that tier's rolling index, the index moved past the entry
// taken and every entry skipped (an entry naming no enabled route), the moves
// summed under pr's unit, so the deal and the reads of a tier share one
// rotation (tla/RouteIndex.tla, THE READS). The routes avoid (those a read of
// the primary returned on) are left out while another of the tier is served,
// as a redeal leaves out the routes already taken, and so is a route that rests
// at s.Now (s.resting; the ask settles the rests once for the whole ask,
// readCardsAskFull, so no read scans the fleet table): a read is never asked onto
// a resting route while another of the tier serves (docs/SPEC-SPRINT.md section 6,
// the reads). The tier alone when the store holds
// no route or none serves the tier: the read carries no route and its reader runs its
// own --model.
func (s *Snapshot) readRouteOf(ri routeIndexes, pr *Card, avoid []string) map[string]string {
	tier, key := s.readTierDrawn(pr), pr.ID
	base := map[string]string{FieldTier: tier}
	if s.readTierOf(pr) != tier {
		if own := friendReadTier(s, pr); own != "" {
			base[FieldReadFallbackNote] = "read at " + tier + ": no " + own + " reader"
		}
	}
	if len(s.Routes) == 0 || ri[tier] == nil {
		return base
	}
	served := make([]Route, 0, len(s.Routes))
	for _, r := range s.Routes {
		if r.Tier == tier && r.Enabled {
			served = append(served, r)
		}
	}
	arr := s.tierArray(tier)
	// a resting route is left out as an avoided one is: skip holds both, and other
	// asks whether any served entry is left drawable, so with every route resting the
	// walk falls back to the first of them as it did before the ask knew of rests
	skip := slices.Clone(avoid)
	for _, r := range served {
		if _, rests := s.resting(r.Name); rests {
			skip = append(skip, r.Name)
		}
	}
	other := false
	for _, name := range arr {
		_, ok := routeByName(served, name)
		other = other || ok && !contains(skip, name)
	}
	at := ri[tier].r.count
	r, steps, ok := preferFirst(arr, func(name string) (Route, bool) { return routeByName(served, name) }, skip, other, at)
	if !ok {
		return base
	}
	ri[tier].r.count += steps
	was, _ := strconv.ParseUint(ri[tier].moves[key], 10, 64)
	ri[tier].moves[key] = strconv.FormatUint(was+steps, 10)
	out := map[string]string{FieldRoute: r.Name, FieldModel: r.Provider + "/" + r.Model, FieldTokens: tokensWord(r.Tokens), FieldUSD: r.USD, FieldHarness: r.Harness,
		FieldDeadline: strconv.Itoa(r.Deadline), FieldTier: tier}
	if note := base[FieldReadFallbackNote]; note != "" {
		out[FieldReadFallbackNote] = note
	}
	// a read on a limited route carries its limits too: its reader's requests take the
	// same budget (rate_budget.go), and its take is held by the provider's concurrency
	maps.Copy(out, rateFields(s, r))
	return out
}

// readRouteMissing is the tier of the primary pr's reads (readTierOf), and why
// no reader can read it: "" when the store holds no route at all (reads run on the
// reader's own model), an enabled route of the tier is in its array, or a reader up
// that brings its own model (a friend's or a bud's, read_route.go) reads the tier:
// a read needs a reader, not a route; the member's side only while the fleet's tiers hold
// the read tier, a friend only while the friends' tiers do (set --fleet-tiers,
// --friends-tiers). The deal's tick raises the tier's judgment for the reads waiting
// (TickDeal, NNoRoute) only when neither serves it.
func (s *Snapshot) readRouteMissing(pr *Card) (tier, why string) {
	tier = s.readTierOf(pr)
	// a member reads only a read tier the fleet's tiers hold (set --fleet-tiers); a friend
	// only one the friends' tiers hold (tierServed, friendTakes)
	if s.FleetTakes(tier) && (s.tierRouted(tier) || s.ownModelReaderUp(tier)) {
		return tier, ""
	}
	up, why := s.tierServed(tier, nil)
	if len(up) > 0 {
		return tier, ""
	}
	return tier, why + "; " + tier + " is the tier of the work its reads read, and no reader up that brings its own model reads it, so its reads have no reader: or start a friend's or a bud's reader of tier " + tier
}

// tokensWord is a route's budget as native's --tokens takes it.
func tokensWord(n int) string {
	if n <= 0 {
		return "unmetered"
	}
	return strconv.Itoa(n)
}

// splitRoute moves the route fields of set (the deal's draw) to the work card's
// changes and the primary's: the history to the primary, the rest to the work card.
func splitRoute(route map[string]string) (work, primary map[string]string) {
	work, primary = map[string]string{}, map[string]string{}
	for k, v := range route {
		if k == FieldRoutes || k == FieldTierNow {
			primary[k] = v
			continue
		}
		work[k] = v
	}
	return work, primary
}

// TierRoutes is how many enabled routes each tier has, as `where` shows it:
// "flash=1 pro=3", "" when the store has none.
func TierRoutes(routes []Route) string {
	n := map[string]int{}
	for _, r := range routes {
		if r.Enabled {
			n[r.Tier]++
		}
	}
	if len(routes) == 0 {
		return ""
	}
	var out []string
	for _, t := range tierLadder {
		out = append(out, fmt.Sprintf("%s=%d", t, n[t]))
	}
	return strings.Join(out, " ")
}

// AttemptLine is one attempt's record as `card <id>` prints it: the work card's
// route, model and tier, its member, when it was dealt, taken and finished, how it
// ended, the head it pushed (head=, "-" when none) and what it spent.
func AttemptLine(wc *Card) string {
	end := "in flight (" + wc.Col + ")"
	switch {
	case wc.F("ok") == "yes":
		end = "ok"
	case wc.F("ok") == "no":
		end = "failed: " + wc.F("report")
		if len(end) > 160 {
			end = end[:160]
		}
	case wc.Col == Withdrawn:
		end = "withdrawn"
	case !wc.Placed():
		end = "retired"
	}
	return fmt.Sprintf("ATTEMPT %s card=%s gen=%s route=%s model=%s tier=%s member=%s dealt=%s taken=%s finished=%s head=%s%s ran=%s usage=%s end=%s",
		orDash(wc.F("attempt")), wc.ID, orDash(wc.F("gen")), orDash(wc.F(FieldRoute)), orDash(wc.F(FieldModel)), orDash(wc.F(FieldTier)), orDash(wc.F("member")),
		orDash(wc.F("dealt")), orDash(wc.F("taken")), orDash(wc.F("finished")), orDash(PushedHead(wc)), DecidedWords(wc.F(FieldDecided), wc.F(FieldDecidedUsed) == "yes"), orDash(wc.F(FieldRanAttempts)), orDash(wc.F(FieldUsage)), end)
}

// DecidedWords is a work card's attempt decision as `card` prints it on its ATTEMPT line,
// " decided=<class>:<p>", with " decided_used=yes" when it routed the finish; "" when the
// card carries none.
func DecidedWords(line string, used bool) string {
	d, ok := decide.ParseDecided(line)
	if !ok {
		return ""
	}
	w := " decided=" + d.Value + ":" + strconv.FormatFloat(d.P, 'f', 2, 64)
	if used {
		w += " decided_used=yes"
	}
	return w
}

// ProviderTake is the record of one take of a work card the provider failed, kept on the
// card beside the takes after it (FieldProviderTake plus the take's number: the card's
// redeals when it ended plus one for a take that ended, returnTakeNumber for a take
// returned untouched): the route and model it ran on, the member, when it
// ended, what it spent and the error line. `card <id>` prints one ATTEMPT line for each,
// and the route stats count it against its route. Taken is when the member took the card for
// the take, its child launched ("" in a record written before it was kept): a provider's
// refusal is read against the rest window its launch fell in (provider_funds.go).
type ProviderTake struct{ Route, Model, Member, Finished, Usage, Error, Taken string }

// String is the take as the card's field holds it: tab separated, the line, whose tabs are
// blanks, before the launch's time, so a record written before Taken reads as it did.
func (t ProviderTake) String() string {
	v := strings.Join([]string{t.Route, t.Model, t.Member, t.Finished, t.Usage, strings.ReplaceAll(t.Error, "\t", " ")}, "\t")
	if t.Taken != "" {
		v += "\t" + t.Taken
	}
	return v
}

// returnTakeNumber is the number of the record a take returned untouched leaves (an
// out-of-credit refusal on a limited route, a provider 5xx: rateLimitedReturned), its n-th
// such return (FieldRateReturns after it, 1..MaxRateReturns). It is past every number an
// ended take can hold (redeals + 1, at most MaxRedeals + 1), so a return spends no redeal
// and still never overwrites an ended take's record or another return's: each counts once
// toward the route's rest (routeEnds; tla/RouteRest.tla, Counts).
func returnTakeNumber(n int) int { return MaxRedeals + 1 + n }

// providerTakeFields is each record's field name by its number (FieldProviderTake plus the
// number), built once: the tick reads every work card's records (routeEnds), and the range
// is now MaxRedeals + 1 + MaxRateReturns names long, so naming them allocates nothing per card.
var providerTakeFields = func() []string {
	out := make([]string, returnTakeNumber(MaxRateReturns)+1)
	for n := 1; n < len(out); n++ {
		out[n] = FieldProviderTake + itoa(n)
	}
	return out
}()

// ProviderTakes is the takes of the work card the provider failed, in the order they ended
// (a take returned untouched sits among them by when it ended), each with its number.
func ProviderTakes(wc *Card) (takes []ProviderTake, numbers []int) {
	for n := 1; n < len(providerTakeFields); n++ {
		v := wc.F(providerTakeFields[n])
		if v == "" {
			continue
		}
		takes, numbers = append(takes, parseTake(v)), append(numbers, n)
	}
	if len(takes) < 2 {
		return takes, numbers
	}
	// each Finished is stamp's RFC3339 in UTC, so its text orders as its time; a stable
	// order keeps the numbers' order between takes that ended at one stamp
	order := make([]int, len(takes))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return strings.Compare(takes[a].Finished, takes[b].Finished) })
	sorted, nums := make([]ProviderTake, len(takes)), make([]int, len(takes))
	for i, j := range order {
		sorted[i], nums[i] = takes[j], numbers[j]
	}
	return sorted, nums
}

// AttemptLines is the work card's lines as `card <id>` prints them: one for each take of it
// that ended with no work to judge (end=provider failure: the line, or end=no result: the
// line, with the route, model, member and usage of that take), one for each launch refused at staging (end=staging refused: the
// reason, with its generation and member), then the card's own (AttemptLine), the take it is
// on or ended.
func AttemptLines(wc *Card) []string {
	var out []string
	takes, numbers := ProviderTakes(wc)
	for i, t := range takes {
		// a take whose child left no result carries its own kind in its line (takeEnded)
		end := cardhdr.EndProvider + ": " + t.Error
		if strings.HasPrefix(t.Error, cardhdr.EndNoResult+":") {
			end = t.Error
		}
		out = append(out, fmt.Sprintf("ATTEMPT %s card=%s take=%d route=%s model=%s member=%s finished=%s usage=%s end=%s",
			orDash(wc.F("attempt")), wc.ID, numbers[i], orDash(t.Route), orDash(t.Model), orDash(t.Member), orDash(t.Finished), orDash(t.Usage), end))
	}
	// each launch refused at staging (StagingTakes), at the generation it was dealt
	staged, gens := StagingTakes(wc)
	for i, t := range staged {
		out = append(out, fmt.Sprintf("ATTEMPT %s card=%s gen=%d route=%s model=%s member=%s finished=%s usage=%s end=%s: %s",
			orDash(wc.F("attempt")), wc.ID, gens[i], orDash(t.Route), orDash(t.Model), orDash(t.Member), orDash(t.Finished), orDash(t.Usage), cardhdr.EndStaging, t.Error))
	}
	return append(out, AttemptLine(wc))
}

// RouteStat is one route's record over the work cards dealt on it. A pinned
// model is a row of its own, Pinned, named pin:<model>.
type RouteStat struct {
	Route    Route  `json:"route"`
	Pinned   bool   `json:"pinned,omitempty"`
	Attempts int    `json:"attempts"`
	OK       int    `json:"ok"`
	Failed   int    `json:"failed"`
	Provider int    `json:"provider_failures"`
	MeanWall string `json:"mean_wall"` // taken to finished, over the finished; "-" when none
	// RestedUntil is when the route's rest ends while it rests (rule 3, route_rest.go):
	// RFC3339, "" when it does not rest; `routes` fills it at its clock.
	RestedUntil string `json:"rested_until,omitempty"`
	// RestedFor is why it rests (RouteRest.Cause and its words), and Balance and
	// BalanceAt its provider's balance as the balance poll last read it (balance.go):
	// dollars and cents rounded up, or "unknown"; "" before any read.
	RestedFor string `json:"rested_for,omitempty"`
	Balance   string `json:"balance,omitempty"`
	BalanceAt string `json:"balance_at,omitempty"`
}

// RouteStats is every route's record over the fleet table's work cards, the
// routes in name order, then each pinned model (pin:<model>) and each route the
// store no longer holds, as the cards name them, in the order first met.
func RouteStats(routes []Route, fleet *Table) []RouteStat {
	at := map[string]int{}
	out := make([]RouteStat, 0, len(routes))
	for _, r := range routes {
		at[r.Name] = len(out)
		out = append(out, RouteStat{Route: r})
	}
	walls := map[string][]time.Duration{}
	// statOf is the row of a route's name, made on first meeting.
	statOf := func(name string, pinned bool, model string) *RouteStat {
		i, ok := at[name]
		if !ok {
			i = len(out)
			at[name] = i
			provider, m, _ := strings.Cut(model, "/")
			out = append(out, RouteStat{Route: Route{Name: name, Provider: provider, Model: m}, Pinned: pinned})
		}
		return &out[i]
	}
	for _, c := range fleet.Column(Ready, Working, Finished, DoneOK, DoneFailed, DoneDefect, Withdrawn) {
		name := c.F(FieldRoute)
		if name == "" {
			continue
		}
		pinned := name == RoutePin
		if pinned {
			name = RoutePin + ":" + c.F(FieldModel)
		}
		// each take of the card the provider failed is an attempt of the route it ran on,
		// failed, and counted apart (ProviderTake; the card was dealt again after it)
		takes, _ := ProviderTakes(c)
		for _, t := range takes {
			if t.Route == "" {
				continue
			}
			f, model := name, c.F(FieldModel)
			if t.Route != RoutePin {
				f = t.Route
				if f != name {
					model = "" // a route the card was drawn before: its row is the store's
				}
			}
			st := statOf(f, pinned && t.Route == RoutePin, model)
			st.Attempts++
			st.Failed++
			st.Provider++
		}
		if c.Col == Withdrawn && c.F(FieldProviderError) != "" {
			continue // dealt again at the next deal: no take of it is on this route now
		}
		if c.Col == DoneDefect {
			continue // a brief defect (brief_defect.go): the brief's, never the route's
		}
		st := statOf(name, pinned, c.F(FieldModel))
		st.Attempts++
		switch c.F("ok") {
		case "yes":
			st.OK++
		case "no":
			st.Failed++
		}
		t0, e0 := time.Parse(time.RFC3339, c.F("taken"))
		t1, e1 := time.Parse(time.RFC3339, c.F("finished"))
		if e0 == nil && e1 == nil && !t1.Before(t0) {
			walls[name] = append(walls[name], t1.Sub(t0))
		}
	}
	for i := range out {
		out[i].MeanWall = "-"
		if ws := walls[out[i].Route.Name]; len(ws) > 0 {
			var sum time.Duration
			for _, w := range ws {
				sum += w
			}
			out[i].MeanWall = (sum / time.Duration(len(ws))).Round(time.Second).String()
		}
	}
	return out
}
