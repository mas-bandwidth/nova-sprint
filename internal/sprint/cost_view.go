package sprint

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/cardcost"
	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// Cost visibility (docs/SPEC-SPRINT.md section 1, the where view; the owner, 2026-10-04):
// what the sprint spends by tier and per landed card, counted by the tick from the
// sprint it reads anyway (the where record, store/where.go) and never from per-card
// reads at `where`: the cards' tiers from their briefs' tier lines, each stream's
// dollars per landed card, and each stream's spend split by the tier its attempts ran
// on (the cost records' tier, not the card's ceiling: a flash card escalated to pro
// shows both).

// TierCosts is one stream's tiers and spend as the where view carries them.
type TierCosts struct {
	// Tiers counts the stream's cards by the tier their briefs name (TierWord).
	Tiers map[string]int `json:"tiers,omitempty"`
	// PerLanded is the stream's landed cards' cost per landed card, dollars and cents
	// rounded up (MoneyText); "-" with nothing landed or nothing priced.
	PerLanded string `json:"per_landed"`
	// CostByTier is the stream's spend by the tier each attempt and read ran on, dollars
	// and cents allocated from the rounded-up total, over every card of the stream, every dollar of TotalCost in one
	// tier: a record with no tier takes its route's (runTier), and a card's records past
	// the list's bound (in its total, not its list) take the card's; "no tier" only when
	// none of these names one.
	CostByTier map[string]string `json:"cost_by_tier,omitempty"`
	// TotalCost is the stream's complete recorded spend (StreamSpend): every take, read and
	// decide call of every card of it in any column, landed, open or dropped off the table,
	// at each record's charged figure (the harness's cost, else its tokens at the route's
	// prices), a recut card's spend counted once on its twin, dollars and cents rounded up;
	// "" when nothing of it was priced. The work table's cost column shows it, and the cost
	// tile is its sum over the streams (with the unattributed spend, Unreconciled).
	TotalCost string `json:"total_cost,omitempty"`
	// TotalUSD is TotalCost exact, as the cards' sums give it (cardcost.Sum), and LandedUSD
	// the landed cards' own cost exact (FieldCost), what PerLanded divides: "" for none.
	TotalUSD  string `json:"total_usd,omitempty"`
	LandedUSD string `json:"landed_usd,omitempty"`
	// WorkCost, ReadCost and DecideCost split TotalCost by kind: every take's charged figure,
	// every read's and every decide call's, dollars and cents rounded up; "" when nothing of
	// that kind was priced. The dashboard shows the reads as their own number beside the work.
	WorkCost   string `json:"work_cost,omitempty"`
	ReadCost   string `json:"read_cost,omitempty"`
	DecideCost string `json:"decide_cost,omitempty"`
	// DecideUnpriced counts the stream's decide calls no route priced (ConsumerDecide): their
	// tokens kept, no dollar figure made up.
	DecideUnpriced int `json:"decide_unpriced,omitempty"`
	// DroppedCards counts the stream's cards kept off the table whose cost is in TotalCost,
	// and CarriedCards its twins that carry a replaced card's spend (FieldCostCarried).
	DroppedCards int `json:"dropped_cards,omitempty"`
	CarriedCards int `json:"carried_cards,omitempty"`
	// ReadTokens is the tokens of the stream's subscription reads (WhySubscription), the
	// reads whose cost is their tokens; 0 when none.
	ReadTokens int64 `json:"read_tokens,omitempty"`
	// ReadsToday is the stream's reads that ended on the tick's UTC day, by the route that
	// priced them (WhySubscription for a subscription reader's, "-" for none): the exact
	// dollars, the tokens and the count, which ReadSpendLine sums over the streams.
	ReadsToday map[string]ReadDay `json:"reads_today,omitempty"`
	// UnpricedRuns counts the stream's records that carry no cost at all: runs whose usage
	// never reached the sprint (cardcost.WhyNoTokens), which the total cannot hold.
	UnpricedRuns int `json:"unpriced_runs,omitempty"`
	// ReadsNoTokens counts the stream's reads whose verdict was kept with a usage that
	// reported no token (cardcost.WhyNoTokens): one of UnpricedRuns each, counted apart
	// so a reader whose harness stops reporting is seen.
	ReadsNoTokens int `json:"reads_no_tokens,omitempty"`
	// Readers is each reader's spend on the stream's cards, by the reader that ran the read
	// (readers_spend.go): ReaderSpendsOf sums it over the streams for the readers table.
	Readers map[string]ReaderSpend `json:"readers,omitempty"`
	// InFlight is the stream's spend on cards not yet landed: every take and read of
	// cards in waiting, placed, or merging, dollars and cents rounded up; "-" when none.
	InFlight string `json:"in_flight"`
	// Unreconciled is the SPRINT's, the same on every stream's record: what the providers
	// counted beyond the sprint's records since the epoch began (UnreconciledSpend,
	// cost_reconcile.go), dollars and cents rounded up; "" when nothing is.
	Unreconciled string `json:"unreconciled,omitempty"`
	// Reconciles is the SPRINT's too: each provider's latest reconciliation, its day, its
	// own figure, the records' and the gap (LatestReconciles, cost_reconcile.go).
	Reconciles []CostReconcileRecord `json:"reconciles,omitempty"`
}

// TierWord is the tier a card's brief names on its line 1 (any word the brief carries),
// flash when it names none.
func TierWord(c *Card) string {
	return cmp.Or(briefTierOf(c), cardhdr.RouteFlash)
}

// briefTierOf is the tier the card's brief names on its line 1 (cardhdr.ReadModel), ""
// for none: kept per card while its brief is the one read (tierMemo), so each pass over
// the cards reads no brief's header again (2026-10-17: TierWord and attemptTier read
// every card's header lines once a card a pass).
func briefTierOf(c *Card) string {
	brief := c.F("brief")
	tierMemo.mu.Lock()
	k, ok := tierMemo.cur[c]
	if !ok {
		k, ok = tierMemo.old[c]
	}
	tierMemo.mu.Unlock()
	if ok && k.brief == brief {
		return k.tier
	}
	m, _ := cardhdr.ReadModel(brief)
	tierMemo.mu.Lock()
	if len(tierMemo.cur) >= costMemoCap {
		tierMemo.old, tierMemo.cur = tierMemo.cur, nil
	}
	if tierMemo.cur == nil {
		tierMemo.cur = make(map[*Card]tierKept, costMemoCap)
	}
	tierMemo.cur[c] = tierKept{brief: brief, tier: m.Tier}
	tierMemo.reads++
	tierMemo.mu.Unlock()
	return m.Tier
}

// tierMemo is briefTierOf's kept tiers, by card, in two generations (cardCostMemo).
var tierMemo struct {
	mu       sync.Mutex
	cur, old map[*Card]tierKept
	reads    int // the briefs read (tests)
}

// tierKept is one card's brief tier and the brief it was read from.
type tierKept struct{ brief, tier string }

// TierCounts counts every primary on the work table (sentinels left out) by tier
// (TierWord).
func TierCounts(s *Snapshot) map[string]int {
	out := map[string]int{}
	for _, c := range s.Work.Column(States...) {
		if !IsSentinel(c) {
			out[TierWord(c)]++
		}
	}
	return out
}

// StreamTierCosts is each stream's TierCosts, by stream, over the work table's rows: every
// card of the stream counted, on the table or kept off it (a dropped card keeps its cost),
// a card whose spend was carried to its twin counted on the twin (StreamSpend).
func StreamTierCosts(s *Snapshot) map[string]TierCosts {
	out, _ := streamTierCostsBy(s)
	return out
}

// StreamTierCostsChecked is StreamTierCosts and its check (CostCheckOf), counted over one
// walk of the cards by stream (streamCards).
func StreamTierCostsChecked(s *Snapshot) (map[string]TierCosts, CostCheck) {
	out, by := streamTierCostsBy(s)
	return out, costCheckOf(s, out, by)
}

// streamTierCostsBy is StreamTierCosts and the cards by stream it was counted over. The
// sprint's figures, the same on every stream's record (the routes, the unattributed spend
// and the reconciliations), are counted once, not once a stream.
func streamTierCostsBy(s *Snapshot) (map[string]TierCosts, map[string][]*Card) {
	by := streamCards(s)
	sw := sprintCostsOf(s)
	out := map[string]TierCosts{}
	for _, st := range s.Work.Rows() {
		out[st] = streamTierCosts(s, st, by[st], sw)
	}
	return out, by
}

// sprintCosts is the sprint's figures every stream's TierCosts carries alike.
type sprintCosts struct {
	routes       map[string]Route
	unreconciled string
	reconciles   []CostReconcileRecord
}

func sprintCostsOf(s *Snapshot) sprintCosts {
	sw := sprintCosts{routes: map[string]Route{}}
	for _, r := range s.Routes {
		sw.routes[r.Name] = r
	}
	if unrec := UnreconciledSpend(s); unrec > 0 {
		sw.unreconciled = cardcost.Cents(new(big.Rat).SetFloat64(unrec))
	}
	sw.reconciles = LatestReconciles(s)
	return sw
}

// streamCards is every primary of the work table, placed or kept off it (Table.Cards),
// sentinels left out, by the stream its spend counts in: its own row, or, for a card whose
// spend was carried to its twin (FieldCostCarriedTo), the stream of the card at the end of
// the carry, so a twin admitted to another stream takes the old card's records with it.
func streamCards(s *Snapshot) map[string][]*Card {
	out := map[string][]*Card{}
	for _, c := range s.Work.Cards() {
		if IsSentinel(c) {
			continue
		}
		st := costStream(s, c)
		out[st] = append(out[st], c)
	}
	return out
}

// costStream is the stream a card's spend counts in (streamCards): its row, its stream
// field when it is kept off the table, or the stream of the card its spend was carried to,
// followed to the end of the carry (bounded, so a cycle no step writes ends).
func costStream(s *Snapshot, c *Card) string {
	for range MaxTakes {
		to := c.F(FieldCostCarriedTo)
		if to == "" {
			break
		}
		next := s.Work.Card(to)
		if next == nil {
			break
		}
		c = next
	}
	return cmp.Or(c.Row, c.F("stream"))
}

// StreamSpend is a stream's spend over its cards (streamCards), the exact sum in USD and
// whether anything of it was priced: each card's own charged total and what it carries
// from the cards it replaced (FieldCostCarried), a card whose spend was carried to its twin
// left out (counted on the twin: the once-only rule, FieldCostCarriedTo).
func StreamSpend(cards []*Card) (string, bool) {
	var sum []string
	for _, c := range cards {
		if c.F(FieldCostCarriedTo) != "" {
			continue
		}
		if v := chargedOf(c.F(FieldCostTotal)); v != "" { // CardCostOf(c).Total.Charged
			sum = append(sum, v)
		}
		if v := c.F(FieldCostCarried); v != "" {
			sum = append(sum, v)
		}
	}
	if len(sum) == 0 {
		return "", false
	}
	return cardcost.Sum(sum...)
}

// CardsSpend is the exact sum of every card's own charged total, in USD, over the work
// table's primaries placed or kept (sentinels left out), nothing carried counted: the figure
// the streams' sums must equal (CostCheckOf). "" when nothing was priced.
func CardsSpend(s *Snapshot) string {
	var sum []string
	for _, c := range s.Work.Cards() {
		if IsSentinel(c) {
			continue
		}
		if v := chargedOf(c.F(FieldCostTotal)); v != "" { // CardCostOf(c).Total.Charged
			sum = append(sum, v)
		}
	}
	if v, ok := cardcost.Sum(sum...); ok && len(sum) > 0 {
		return v
	}
	return ""
}

// landedSpend is the exact sum of the stream's landed cards' own cost (FieldCost), what the
// cost per landed card divides, and how many landed: "" when none of them was priced.
func landedSpend(s *Snapshot, stream string) (string, int) {
	var sum []string
	n := 0
	for _, c := range s.Work.Cell(stream, Landed) {
		if IsSentinel(c) {
			continue
		}
		n++
		if v := c.F(FieldCost); v != "" {
			sum = append(sum, v)
		}
	}
	if v, ok := cardcost.Sum(sum...); ok && len(sum) > 0 {
		return v, n
	}
	return "", n
}

func streamTierCosts(s *Snapshot, stream string, cards []*Card, sw sprintCosts) TierCosts {
	t := TierCosts{Tiers: map[string]int{}, PerLanded: "-", InFlight: "-", CostByTier: map[string]string{}, ReadsToday: map[string]ReadDay{}, Readers: map[string]ReaderSpend{}}
	byTier := map[string]*big.Rat{}
	workCost, readCost, decideCost := new(big.Rat), new(big.Rat), new(big.Rat)
	pricedWork, pricedRead, pricedDecide := false, false, false
	day := s.Now.UTC().Format(time.DateOnly)
	routes := sw.routes
	var inFlightCost []string
	for _, c := range cards {
		if c.Placed() {
			t.Tiers[TierWord(c)]++ // the cards by tier: those on the table, as before
		}
		if !c.Placed() {
			t.DroppedCards++
		}
		if c.F(FieldCostCarried) != "" {
			t.CarriedCards++
		}
		if c.Placed() && c.Col != Landed && c.F(FieldCostCarriedTo) == "" {
			// in flight: the spend on the stream's cards on the table and not landed yet,
			// what each carries from the cards it replaced included
			if v := chargedOf(c.F(FieldCostTotal)); v != "" { // CardCostOf(c).Total.Charged
				inFlightCost = append(inFlightCost, v)
			}
			if v := c.F(FieldCostCarried); v != "" {
				inFlightCost = append(inFlightCost, v)
			}
		}
		// the card's whole record, whatever its column: every take, read and decide call
		// behind it, the records past the list's bound included (FieldCostTotal); a card
		// carried to its twin is here too, its records its own, its sum on the twin
		v := CardCostOf(c)
		tot := v.Total
		t.UnpricedRuns += tot.Records - tot.ChargedOf
		listed := new(big.Rat)
		for _, con := range v.Consumers {
			if con.Kind == ConsumerDecide && con.Usage.MarginalUSD() == "" {
				// a decide call with no price is no run: counted apart (DecideUnpriced)
				t.UnpricedRuns--
				t.DecideUnpriced++
			}
			if con.Kind == "read" && subscriptionUsage(con.Usage) {
				// a subscription read's cost is its tokens: priced, never a run unpriced
				if con.Usage.MarginalUSD() == "" {
					t.UnpricedRuns--
				}
				t.ReadTokens += con.Usage.Tokens.Total()
			}
			if con.Kind == "read" && con.Usage.Unpriced == cardcost.WhyNoTokens {
				t.ReadsNoTokens++
			}
			if con.Kind == "read" && strings.HasPrefix(con.At, day) {
				addReadDay(t.ReadsToday, con)
			}
			if con.Kind == "read" {
				rd := t.Readers[cmp.Or(con.Who, "-")]
				rd.addRead(con, s.Now)
				t.Readers[cmp.Or(con.Who, "-")] = rd
			}
			usd, err := amountOf(con.Usage.MarginalUSD())
			if err != nil || usd == nil {
				continue
			}
			switch con.Kind {
			case "read":
				readCost.Add(readCost, usd)
				pricedRead = true
			case ConsumerDecide:
				decideCost.Add(decideCost, usd)
				pricedDecide = true
			default:
				workCost.Add(workCost, usd)
				pricedWork = true
			}
			listed.Add(listed, usd)
			addTier(byTier, runTier(routes, c, con), usd)
		}
		// the records past the list's bound: in the card's total and in no record of its
		// list, so on the card's own tier, that the tiers sum to the total
		if all, err := amountOf(tot.Charged); err == nil && all != nil && all.Cmp(listed) > 0 {
			addTier(byTier, attemptTier(c), new(big.Rat).Sub(all, listed))
		}
	}
	// per landed: the landed cards' own cost over the landed cards, the one figure that is
	// the landed cards' alone
	if sum, landed := landedSpend(s, stream); sum != "" && landed > 0 {
		t.LandedUSD = sum
		if total, err := amountOf(sum); err == nil && total != nil {
			t.PerLanded = cardcost.Cents(total.Quo(total, big.NewRat(int64(landed), 1)))
		}
	}
	if sum, ok := cardcost.Sum(inFlightCost...); ok && len(inFlightCost) > 0 {
		if total, err := amountOf(sum); err == nil && total != nil {
			t.InFlight = cardcost.Cents(total)
		}
	}
	// the stream's cost: every card's spend, carried spend counted once (StreamSpend)
	if sum, ok := StreamSpend(cards); ok {
		if total, err := amountOf(sum); err == nil && total != nil {
			t.TotalUSD = sum
			t.TotalCost = cardcost.Cents(total)
		}
	}
	if pricedWork {
		t.WorkCost = cardcost.Cents(workCost)
	}
	if pricedRead {
		t.ReadCost = cardcost.Cents(readCost)
	}
	if pricedDecide {
		t.DecideCost = cardcost.Cents(decideCost)
	}
	if len(t.ReadsToday) == 0 {
		t.ReadsToday = nil
	}
	if len(t.Readers) == 0 {
		t.Readers = nil
	}
	t.CostByTier = tierCostCents(byTier)
	t.Unreconciled = sw.unreconciled
	t.Reconciles = slices.Clone(sw.reconciles)
	return t
}

// CostCheck is the invariant the cost view keeps (the owner, 2026-10-11: "the total cost of
// all work streams = the total cost of the sprint so far"): the streams' sums (each stream's
// TotalUSD, a recut card's spend on its twin) against every card's own total (CardsSpend),
// both exact. They are equal unless spend was lost or counted twice: a card in no stream
// of the table, a carry that missed a record or a record that came after it. Gap is
// cards less streams, exact; Orphans the cards whose stream is not a row of the table.
type CostCheck struct {
	Streams string   `json:"streams_usd"`
	Cards   string   `json:"cards_usd"`
	Gap     string   `json:"gap_usd,omitempty"`
	OK      bool     `json:"ok"`
	Orphans []string `json:"orphans,omitempty"`
	// Unlisted counts the cards the streams' control cards count dropped and no list names
	// (FieldCostOff, FieldReplaces): dropped before the list was kept, so no read finds them
	// and neither sum holds their cost. Not a gap between the sums; said beside them.
	Unlisted int `json:"unlisted,omitempty"`
}

// CostCheckOf checks the streams' sums (StreamTierCosts, before any stats reset's mark is
// taken off) against the cards' (CardsSpend).
func CostCheckOf(s *Snapshot, streams map[string]TierCosts) CostCheck {
	return costCheckOf(s, streams, streamCards(s))
}

// costCheckOf is CostCheckOf over the cards by stream (streamCards).
func costCheckOf(s *Snapshot, streams map[string]TierCosts, by map[string][]*Card) CostCheck {
	var sum []string
	for _, st := range slices.Sorted(maps.Keys(streams)) {
		if v := streams[st].TotalUSD; v != "" {
			sum = append(sum, v)
		}
	}
	k := CostCheck{Streams: "0", Cards: cmp.Or(CardsSpend(s), "0")}
	if v, ok := cardcost.Sum(sum...); ok && len(sum) > 0 {
		k.Streams = v
	}
	rows := map[string]bool{}
	for _, r := range s.Work.Rows() {
		rows[r] = true
	}
	for st, cs := range by {
		if rows[st] {
			continue
		}
		for _, c := range cs {
			k.Orphans = append(k.Orphans, c.ID)
		}
	}
	slices.Sort(k.Orphans)
	if s.Merge != nil {
		known := map[string]map[string]bool{} // by stream: the ids off the table a list names
		for _, c := range s.Work.Column(States...) {
			for _, id := range Split(c.F(FieldReplaces)) {
				if known[c.Row] == nil {
					known[c.Row] = map[string]bool{}
				}
				known[c.Row][id] = true
			}
		}
		for _, st := range s.Work.Rows() {
			ctl := s.StreamCtl(st)
			if ctl == nil {
				continue
			}
			for _, id := range Split(ctl.F(FieldCostOff)) {
				if known[st] == nil {
					known[st] = map[string]bool{}
				}
				known[st][id] = true
			}
			k.Unlisted += max(0, ctl.Int("dropped")-len(known[st]))
		}
	}
	a, err := amountOf(k.Streams)
	b, err2 := amountOf(k.Cards)
	if err != nil || err2 != nil || a == nil || b == nil {
		return k
	}
	gap := new(big.Rat).Sub(b, a)
	k.OK = gap.Sign() == 0
	if !k.OK {
		k.Gap = cardcost.Text(gap)
	}
	return k
}

// Line is the check as where prints it under the work table: COST OK sum(streams)=total,
// else COST FAIL with the gap and the cards no stream counts.
func (k CostCheck) Line() string {
	money := func(v string) string {
		r, err := amountOf(v)
		if err != nil || r == nil {
			return "$" + v
		}
		if r.Sign() < 0 {
			return "-" + cardcost.Cents(new(big.Rat).Neg(r))
		}
		return cardcost.Cents(r)
	}
	unlisted := ""
	if k.Unlisted > 0 {
		unlisted = fmt.Sprintf("; %d card(s) dropped before the drop list was kept are in neither sum", k.Unlisted)
	}
	if k.OK {
		return fmt.Sprintf("COST OK sum(streams)=total %s (exact %s)%s", money(k.Streams), k.Streams, unlisted)
	}
	line := fmt.Sprintf("COST FAIL sum(streams)=%s total=%s gap=%s (exact %s - %s)", money(k.Streams), money(k.Cards), money(cmp.Or(k.Gap, "0")), k.Cards, k.Streams)
	if len(k.Orphans) > 0 {
		line += fmt.Sprintf("; %d card(s) in no stream of the table: %s", len(k.Orphans), Preview(k.Orphans, ", "))
	}
	return line + unlisted
}

// tierCostCents partitions the rounded-up stream total into displayed tier cents.
// Whole cents stay with their tier; remaining cents go to the largest fractional
// remainders, with alphabetical ties. Recorded exact amounts are never changed.
func tierCostCents(byTier map[string]*big.Rat) map[string]string {
	type allocation struct {
		tier     string
		whole    *big.Int
		fraction *big.Rat
	}
	tiers := make([]string, 0, len(byTier))
	for tier := range byTier {
		tiers = append(tiers, tier)
	}
	sort.Strings(tiers)
	parts := make([]allocation, 0, len(tiers))
	remainders := new(big.Rat)
	for _, tier := range tiers {
		cents := new(big.Rat).Mul(byTier[tier], big.NewRat(100, 1))
		whole := new(big.Int).Quo(cents.Num(), cents.Denom())
		fraction := new(big.Rat).Sub(cents, new(big.Rat).SetInt(whole))
		remainders.Add(remainders, fraction)
		parts = append(parts, allocation{tier, whole, fraction})
	}
	left := new(big.Int).Quo(remainders.Num(), remainders.Denom()).Int64()
	if !remainders.IsInt() {
		left++
	}
	sort.SliceStable(parts, func(i, j int) bool { return parts[i].fraction.Cmp(parts[j].fraction) > 0 })
	out := make(map[string]string, len(parts))
	for i, part := range parts {
		if int64(i) < left {
			part.whole.Add(part.whole, big.NewInt(1))
		}
		out[part.tier] = cardcost.Cents(new(big.Rat).SetFrac(part.whole, big.NewInt(100)))
	}
	return out
}

// runTier is the tier a record's run is counted under: the tier it recorded, else its
// route's (the route row's tier, else the route name's prefix: pro-*, flash-*, heavy-*,
// frontier-*), else its card attempt's (attemptTier); "no tier" only when none of these names one.
func runTier(routes map[string]Route, c *Card, con Consumer) string {
	if con.Tier != "" {
		return con.Tier
	}
	for _, name := range []string{con.Route, con.Usage.Route} {
		if name == "" {
			continue
		}
		if r, ok := routes[name]; ok && r.Tier != "" {
			return r.Tier
		}
		for _, tier := range cardhdr.Routes {
			if strings.HasPrefix(name, tier+"-") {
				return tier
			}
		}
	}
	return attemptTier(c)
}

// attemptTier is the tier the card's attempt is on as the card records it: the tier its
// last deal drew (FieldTierNow), else the tier pinned on it (FieldTier), else the tier its
// brief names; "no tier" when none does (never cardTier's default ceiling, which would
// name a tier nothing recorded).
func attemptTier(c *Card) string {
	if t := cmp.Or(c.F(FieldTierNow), c.F(FieldTier)); t != "" {
		return t // the brief is not read when the card records its tier
	}
	return cmp.Or(briefTierOf(c), "no tier")
}

// addTier adds usd to the tier's sum.
func addTier(byTier map[string]*big.Rat, tier string, usd *big.Rat) {
	if byTier[tier] == nil {
		byTier[tier] = new(big.Rat)
	}
	byTier[tier].Add(byTier[tier], usd)
}

// errBadAmount is a dollar amount that is no decimal.
var errBadAmount = errors.New("not a decimal amount")

// amountOf is a decimal dollar string as a rational, nil for an empty one.
func amountOf(usd string) (*big.Rat, error) {
	if usd == "" {
		return nil, nil
	}
	r, ok := new(big.Rat).SetString(usd)
	if !ok {
		return nil, errBadAmount
	}
	return r, nil
}

// ReadDay is one route's reads of a UTC day: the exact dollars charged (each read's
// actual cost where reported, else its predicted one; "" when none was priced), the
// tokens, and how many reads.
type ReadDay struct {
	USD    string `json:"usd,omitempty"`
	Tokens int64  `json:"tokens,omitempty"`
	Reads  int    `json:"reads"`
}

// addReadDay counts the read con into its route's day: the route that priced it, a
// subscription reader's under WhySubscription, "-" for a read no route priced; a read
// that reported nothing is not counted here (it is one of UnpricedRuns, and of
// ReadsNoTokens).
func addReadDay(days map[string]ReadDay, con Consumer) {
	u := con.Usage
	if !u.Tokens.Reported() && u.Actual == "" && u.Predicted == "" {
		return
	}
	route := cmp.Or(u.Route, "-")
	if subscriptionUsage(u) {
		route = WhySubscription
	}
	d := days[route]
	d.Reads++
	d.Tokens += max(u.Tokens.Total(), 0)
	if usd := u.MarginalUSD(); usd != "" {
		if sum, ok := cardcost.Sum(d.USD, usd); ok {
			d.USD = sum
		}
	}
	days[route] = d
}

// ReadSpendLine is the day's read spend per route over the streams' records, one line
// under the where view's summary: "reads today: pro-a $1.24 12 reads 3456789 tokens ·
// subscription tokens 3 reads 120000 tokens", routes in name order, a priced route's
// dollars rounded up to the cent, a route with nothing priced "unpriced"; "" when no
// read ended today.
func ReadSpendLine(streams map[string]TierCosts) string {
	days := map[string]ReadDay{}
	for _, tc := range streams {
		for route, d := range tc.ReadsToday {
			all := days[route]
			all.Reads += d.Reads
			all.Tokens += d.Tokens
			// a route with nothing priced keeps no dollars: the day's are the priced reads' alone
			if sum, ok := cardcost.Sum(all.USD, d.USD); ok && d.USD != "" {
				all.USD = sum
			}
			days[route] = all
		}
	}
	if len(days) == 0 {
		return ""
	}
	routes := make([]string, 0, len(days))
	for r := range days {
		routes = append(routes, r)
	}
	sort.Strings(routes)
	parts := make([]string, 0, len(routes))
	for _, r := range routes {
		d := days[r]
		cost := MoneyText(d.USD)
		if r == WhySubscription || d.USD == "" {
			cost = "unpriced"
			if r == WhySubscription {
				cost = "tokens"
			}
		}
		parts = append(parts, fmt.Sprintf("%s %s %d reads %d tokens", r, cost, d.Reads, d.Tokens))
	}
	return "reads today: " + strings.Join(parts, " · ")
}
