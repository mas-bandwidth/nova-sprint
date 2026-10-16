package sprint

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-tools/pkg/cardcost"
)

// What a card cost (docs/SPEC-SPRINT.md, "What a card cost"): the cost is tracked
// in the card itself, so by the time a producer card lands it holds the history
// of the consumer cards that did work for it, what each cost, and the total for
// the producer. A producer card's
// consumers are its work cards' takes (each run on a fleet member) and its read
// cards' runs (each read by a reader). When a consumer ends, the step that ends it
// writes its usage record (cardcost.Usage: the tokens by class its member or reader
// reported, its waiting and running time, the predicted cost under the price sheet of
// the route that served it with the sheet copied beside it, the harness's own cost
// when it reported one) on the consumer card as before, and appends the consumer's
// record to the primary with the primary's total (addConsumer). The card's history,
// its totals and its landed cost are read from the primary alone (CardCostOf). It
// adds no column, line or row to any table: the records are fields of the cards.

// costRecord is a consumer's usage record at its end: what was reported (usage),
// timed from the stamp it was dealt or asked (from) through the one it was taken or
// begun (began) to now, and priced. A work card is priced by its route (route, by
// name; a pinned or unrouted card by the model it ran), a read by an enabled route of
// the provider/model its harness reported (onlyEnabled). A run that reported no token
// is not priced.
func costRecord(s *Snapshot, usage, route, model string, onlyEnabled bool, from, began string) string {
	u := cardcost.ParseUsage(usage).Timed(from, began, s.Now)
	r, ok := s.priceRoute(route, cmp.Or(u.Model, model), onlyEnabled)
	if subscriptionUsage(u) {
		if ok {
			return u.Priced(r.Name, s.billingPrices(r)).String()
		}
		return u.Priced(route, cardcost.Prices{}).String()
	}
	if ok && s.billingPrices(r).Billing == cardcost.BillingSubscription {
		return u.Priced(r.Name, s.billingPrices(r)).String()
	}
	if !u.Tokens.Reported() {
		u.Unpriced = cardcost.WhyNoTokens
		return u.String()
	}
	if !ok {
		return u.Priced("", cardcost.Prices{}).String()
	}
	return u.Priced(r.Name, s.billingPrices(r)).String()
}

// billingPrices carries declared billing through the same price projection as tokens.
// An explicit route value wins; a missing route value inherits its provider declaration.
func (s *Snapshot) billingPrices(r Route) cardcost.Prices {
	p := r.Prices
	if p.Billing == "" && s.Providers != nil && s.Providers[r.Provider].Billing == cardcost.BillingSubscription {
		p.Billing = cardcost.BillingSubscription
	}
	return p
}

// A read is priced as work is (the owner, 2026-10-05: "do we have the cost for readers
// properly calculated yet in nova sprint?"): by the route its ask drew, the read card's
// own route row, never by a route found again from the model its harness reported. A
// read card with a route is a routed read: its verdict (read --ok or --broken) carries
// the run's usage or is refused (ReadUsageMissing), so no routed reader can leave its
// read unpriced by saying nothing. A usage with no token count (a fleet harness that
// reported none) is permissive in what we read: the verdict is kept, its record says
// unpriced=no-tokens and the stream counts it (TierCosts.ReadsNoTokens). A subscription reader (a bud's claude -p on its own plan, which bills
// no dollar per token) says so in its usage, billing=subscription: its tokens are kept,
// with zero marginal USD and retained harness dollar evidence; its read cost is shown as tokens (CostTokens).
const (
	// UsageSubscription is the usage word a subscription reader adds to its --usage.
	UsageSubscription = "billing=subscription"
	// WhySubscription is a subscription run's unpriced word: its tokens, no dollars.
	WhySubscription = "subscription"
	// CostTokens is the cost a COST line shows for a subscription run: its tokens.
	CostTokens = "tokens"
)

// subscriptionUsage says the usage is a subscription reader's (UsageSubscription).
func subscriptionUsage(u cardcost.Usage) bool { return u.Billing == cardcost.BillingSubscription }

// routedRead says the read card was drawn a route at its ask: what prices it.
func routedRead(c *Card) bool {
	r := c.F(FieldRoute)
	return r != "" && r != RoutePin
}

// readCostRecord is a read run's usage record at its end (costRecord): a routed read
// priced by its card's route, a subscription reader's tokens kept with no dollar figure
// (its harness's reported dollars retained as evidence, with zero marginal charge), and a read with no
// route by an enabled route of the provider/model its harness reported, as before.
func readCostRecord(s *Snapshot, c *Card, usage, from, began string) string {
	u := cardcost.ParseUsage(usage)
	if subscriptionUsage(u) {
		u = u.Timed(from, began, s.Now)
		u = u.Priced(c.F(FieldRoute), cardcost.Prices{Billing: cardcost.BillingSubscription})
		u.Unpriced = WhySubscription
		return u.String()
	}
	if routedRead(c) {
		return costRecord(s, usage, c.F(FieldRoute), c.F(FieldModel), false, from, began)
	}
	return costRecord(s, usage, "", "", true, from, began)
}

// ReadUsageMissing is why a read's verdict on the read card c is refused for its usage:
// a routed read with no --usage at all, which leaves nothing to price it by; "" otherwise.
// A usage with no token count is taken, never refused: a verdict is never lost for its
// harness's accounting (readCostRecord records it unpriced=no-tokens). Its remedy is the
// verdict again with the harness's own token report.
func ReadUsageMissing(c *Card, usage string, verdict string) string {
	if !routedRead(c) || strings.TrimSpace(usage) != "" {
		return ""
	}
	return fmt.Sprintf("a read on route %s is priced as work is, from the run's tokens, and this verdict has no --usage: run nova-sprint read --as %s --%s %s ... --usage '<the harness's own token report: input=<n> cache_read=<n> cache_write=<n> output=<n> model=<provider/model>>'; a subscription reader adds %s to its usage",
		c.F(FieldRoute), c.F("reader"), cmp.Or(verdict, "ok"), c.ID, UsageSubscription)
}

// CostWord is the cost a COST line shows for a record: which of the dollar figures it
// holds (Usage.Present), or tokens for a subscription run.
func CostWord(u cardcost.Usage) string {
	if u.Unpriced == WhySubscription || subscriptionUsage(u) {
		return CostTokens
	}
	return u.Present()
}

// priceRoute is the route whose price sheet prices a run: the route of that name when
// the store still holds it, else the first route in name order that runs the model
// (provider/model), an enabled one when onlyEnabled.
func (s *Snapshot) priceRoute(name, model string, onlyEnabled bool) (Route, bool) {
	if name != "" && name != RoutePin {
		for _, r := range s.Routes {
			if r.Name == name {
				return r, true
			}
		}
	}
	for _, r := range s.Routes {
		if r.Provider+"/"+r.Model == model && (r.Enabled || !onlyEnabled) {
			return r, true
		}
	}
	return Route{}, false
}

// FieldReadTake is the record of one run of a read card that ended without a verdict
// (read --return), keyed by the run's number, 1 for the first, kept on the read card
// beside its usage (the run that gave the verdict); the producer's record of each run
// is on the primary (FieldCostRecord), keyed by the same number.
const FieldReadTake = "read_take_"

// MaxTakes bounds the numbered records of one card a reader looks for.
const MaxTakes = 64

// nextTake is the number the card's next record under prefix takes: one past the
// last it holds.
func nextTake(c *Card, prefix string) int {
	n := 1
	for n <= MaxTakes && c.F(prefix+itoa(n)) != "" {
		n++
	}
	return n
}

// The producer card carries what it cost, for the cost is tracked in the card:
// each consumer that ends appends one record to the primary,
// FieldCostRecord plus the consumer's key, in the same step that ends it, and the
// primary's running total (FieldCostTotal) is updated with it. Everything that shows
// or sums a card's cost reads the primary alone (CardCostOf), so no reader or member
// removed, no read card retired and no consumer record cleaned up can lose cost.
const (
	FieldCostRecord = "cost_record:" // + the consumer's key (Consumer.Key)
	FieldCostTotal  = "cost_total"   // cardcost.Total.String over every record
	FieldCostCut    = "cost_cut"     // how many records past MaxCostRecords were left out of the list
	// MaxCostRecords bounds the history on one card. A card's takes and reads are few;
	// past the bound a record is still added to the total, exactly, and counted in
	// FieldCostCut, and `card <id>` says the list was cut.
	MaxCostRecords = 64
)

// Consumer is one consumer of a producer card: a take of a work card or a run of a
// read card, who ran it (the member or the reader), on what route and model, how it
// ended, when, and its cost record.
type Consumer struct {
	Kind    string         `json:"kind"` // work or read
	Card    string         `json:"card"`
	Attempt int            `json:"attempt"`
	Take    int            `json:"take,omitempty"` // a work card's take the provider failed, a read's run returned; 0 for the card's own
	Gen     int            `json:"gen,omitempty"`  // a work card's generation when it ended
	Who     string         `json:"who"`            // the member or the reader
	Route   string         `json:"route,omitempty"`
	Model   string         `json:"model,omitempty"` // provider/model
	Tier    string         `json:"tier,omitempty"`  // the tier its route was drawn from (flash first, pro on escalation)
	End     string         `json:"end"`
	At      string         `json:"at"`  // when it ended
	Key     string         `json:"key"` // the card and its run: one record per key, set once
	Usage   cardcost.Usage `json:"usage"`
	// Cap and Overrun are a capped take's lane cap and its wall past it (lane_cap.go), ""
	// for a take no cap ended.
	Cap     string `json:"cap,omitempty"`
	Overrun string `json:"overrun,omitempty"`
}

// line is the record as the primary keeps it: the consumer's words, then its usage.
func (c Consumer) line() string {
	w := []string{"kind=" + c.Kind, "card=" + c.Card, "attempt=" + itoa(c.Attempt), "take=" + itoa(c.Take), "gen=" + itoa(c.Gen),
		"who=" + orDash(c.Who), "on_route=" + orDash(c.Route), "on_model=" + orDash(c.Model), "on_tier=" + orDash(c.Tier), "end=" + orDash(strings.ReplaceAll(c.End, " ", "-")), "at=" + orDash(c.At)}
	if c.Cap != "" {
		w = append(w, "lane_cap="+c.Cap, "lane_overrun="+orDash(c.Overrun))
	}
	return strings.Join(w, " ") + " " + c.Usage.String()
}

// parseConsumer reads a record (line) back.
func parseConsumer(key, line string) Consumer {
	c := Consumer{Key: key}
	var rest []string
	undash := func(v string) string {
		if v == "-" {
			return ""
		}
		return v
	}
	for _, w := range strings.Fields(line) {
		k, v, _ := strings.Cut(w, "=")
		switch k {
		case "kind":
			c.Kind = v
		case "card":
			c.Card = v
		case "attempt":
			c.Attempt, _ = strconv.Atoi(v)
		case "take":
			c.Take, _ = strconv.Atoi(v)
		case "gen":
			c.Gen, _ = strconv.Atoi(v)
		case "who":
			c.Who = undash(v)
		case "on_route":
			c.Route = undash(v)
		case "on_model":
			c.Model = undash(v)
		case "on_tier":
			c.Tier = undash(v)
		case "end":
			c.End = strings.ReplaceAll(undash(v), "-", " ")
		case "at":
			c.At = undash(v)
		case "lane_cap":
			c.Cap = undash(v)
		case "lane_overrun":
			c.Overrun = undash(v)
		default:
			rest = append(rest, w)
		}
	}
	c.Usage = cardcost.ParseUsage(strings.Join(rest, " "))
	return c
}

// addConsumer adds the consumer's record to the primary's changes (set, the fields the
// step writes on it), and the record to the primary's total: once per key, whatever is
// already on the card or in set, so a step planned again never counts it twice.
func addConsumer(pr *Card, set map[string]string, c Consumer) {
	key := FieldCostRecord + c.Key
	if pr.F(key) != "" || set[key] != "" {
		return
	}
	total := cardcost.ParseTotal(cmp.Or(set[FieldCostTotal], pr.F(FieldCostTotal)))
	set[FieldCostTotal] = total.Add(c.Usage).String()
	n := 0
	for k := range pr.Fields {
		if strings.HasPrefix(k, FieldCostRecord) {
			n++
		}
	}
	for k := range set {
		if strings.HasPrefix(k, FieldCostRecord) && pr.F(k) == "" {
			n++
		}
	}
	if n >= MaxCostRecords {
		set[FieldCostCut] = itoa(max(pr.Int(FieldCostCut), atoiOr(set[FieldCostCut])) + 1)
		return
	}
	set[key] = c.line()
}

// atoiOr is a count field's value, 0 when unset.
func atoiOr(v string) int {
	n, _ := strconv.Atoi(v)
	return n
}

// workConsumer is a work card's take as it ends: its route, model, member and
// generation, the end, and the cost record (rec, costRecord's line).
func workConsumer(s *Snapshot, c *Card, take int, end, rec string) Consumer {
	u := cardcost.ParseUsage(rec)
	key := c.ID + "#g" + itoa(c.Int("gen"))
	return Consumer{Kind: "work", Card: c.ID, Attempt: c.Int("attempt"), Take: take, Gen: c.Int("gen"), Who: c.Row,
		Route: c.F(FieldRoute), Model: cmp.Or(u.Model, c.F(FieldModel)), Tier: c.F(FieldTier), End: end, At: stamp(s.Now), Key: key, Usage: u}
}

// readConsumer is a read card's run as it ends: run is the returned run's number, 0
// for the run that gave the verdict.
func readConsumer(s *Snapshot, c *Card, run int, end, rec string) Consumer {
	u := cardcost.ParseUsage(rec)
	key := c.ID + "#v"
	if run > 0 {
		key = c.ID + "#r" + itoa(run)
	}
	model := u.Model
	if !subscriptionUsage(u) {
		model = cmp.Or(u.Model, c.F(FieldModel)) // a routed read ran its route's model
	}
	return Consumer{Kind: "read", Card: c.ID, Attempt: c.Int("attempt"), Take: run, Who: c.F("reader"), Route: u.Route, Model: model,
		Tier: c.F(FieldTier), End: end, At: stamp(s.Now), Key: key, Usage: u}
}

// CardCostView is a producer card's cost: each consumer that ended, the totals, and
// how many records past the bound the list leaves out (Cut; the totals hold them).
type CardCostView struct {
	Consumers []Consumer     `json:"consumers"`
	Total     cardcost.Total `json:"total"`
	Cut       int            `json:"cut,omitempty"`
	// Carried is what the card carries from the cards it replaced (FieldCostCarried), exact
	// USD, and From those cards (FieldReplaces); CarriedTo the twin this card's spend went
	// on with (FieldCostCarriedTo), its stream counting it there.
	Carried     string `json:"carried_usd,omitempty"`
	CarriedFrom string `json:"carried_from,omitempty"`
	CarriedTo   string `json:"carried_to,omitempty"`
}

// CardCostOf is the producer's cost as the primary carries it: its records, in the
// order they ended, and its total.
func CardCostOf(pr *Card) CardCostView {
	v := CardCostView{Consumers: []Consumer{}, Total: cardcost.NoTotal()}
	if pr == nil {
		return v
	}
	for k, line := range pr.Fields {
		if key, ok := strings.CutPrefix(k, FieldCostRecord); ok {
			v.Consumers = append(v.Consumers, parseConsumer(key, line))
		}
	}
	sort.Slice(v.Consumers, func(i, j int) bool {
		a, b := v.Consumers[i], v.Consumers[j]
		if a.At != b.At {
			return a.At < b.At
		}
		return a.Key < b.Key
	})
	v.Total = cardcost.ParseTotal(pr.F(FieldCostTotal))
	v.Cut = pr.Int(FieldCostCut)
	if v.Carried = pr.F(FieldCostCarried); v.Carried != "" {
		v.CarriedFrom = pr.F(FieldReplaces)
	}
	v.CarriedTo = pr.F(FieldCostCarriedTo)
	return v
}

// takeStamps are the stamps a work card's current take was dealt and taken at: what
// its waiting and running time are measured from, recorded and shown, judging nothing
// (the one deadline is WorkDeadline's).
func takeStamps(c *Card) (dealt, taken string) { return c.F("dealt"), c.F("taken") }

// CostLines are the producer's cost as `card <id>` prints it, from the primary alone:
// one COST line per consumer and the COST TOTAL line (charged_usd: each consumer's
// actual cost where reported, else its predicted one, summed). A count or time not reported is "-", a cost not
// known is "-", never 0.
func (v CardCostView) CostLines() []string {
	var out []string
	for _, c := range v.Consumers {
		u := c.Usage
		take := ""
		if c.Take > 0 {
			take = " take=" + strconv.Itoa(c.Take)
		}
		actualBy := ""
		if u.Actual != "" {
			actualBy = u.ActualBy
		}
		out = append(out, fmt.Sprintf("COST kind=%s card=%s attempt=%d%s who=%s route=%s model=%s tier=%s end=%s %s wait=%s run=%s predicted_usd=%s actual_usd=%s actual_by=%s cost=%s",
			c.Kind, c.Card, c.Attempt, take, orDash(c.Who), orDash(cmp.Or(c.Route, u.Route)), orDash(c.Model), orDash(c.Tier), strings.ReplaceAll(c.End, " ", "-"),
			tokenWords(u.Tokens), seconds(u.Wait), seconds(u.Run), orDash(u.Predicted), orDash(u.Actual), orDash(actualBy), CostWord(u)))
	}
	t := v.Total
	cut := ""
	if v.Cut > 0 {
		cut = fmt.Sprintf(" cut=%d", v.Cut) // records past the bound: in the totals, not in the list
	}
	out = append(out, fmt.Sprintf("COST TOTAL consumers=%d %s wait=%s run=%s predicted_usd=%s predicted_of=%d/%d actual_usd=%s actual_by=%s actual_of=%d/%d charged_usd=%s%s",
		t.Records, tokenWords(t.Tokens), seconds(t.Wait), seconds(t.Run), orDash(t.Predicted), t.PredOf, t.Records, orDash(t.Actual), orDash(t.ActualBy), t.ActualOf, t.Records,
		orDash(t.Charged), cut))
	if v.Carried != "" {
		out = append(out, fmt.Sprintf("COST CARRIED from=%s usd=%s (the replaced cards' spend, counted in this card's stream)", orDash(v.CarriedFrom), v.Carried))
	}
	if v.CarriedTo != "" {
		out = append(out, fmt.Sprintf("COST CARRIED-TO twin=%s (this card's spend is counted on its twin)", v.CarriedTo))
	}
	return out
}

// tokenWords are the token classes and requests as a COST line prints them.
func tokenWords(t cardcost.Tokens) string {
	n := func(v int64) string {
		if v < 0 {
			return "-"
		}
		return strconv.FormatInt(v, 10)
	}
	return fmt.Sprintf("input=%s cache_read=%s cache_write=%s output=%s reasoning=%s requests=%s",
		n(t.Input), n(t.CacheRead), n(t.CacheWrite), n(t.Output), n(t.Reasoning), n(t.Requests))
}

// seconds is a time in seconds as a COST line prints it: "-" when not known.
func seconds(n int64) string {
	if n < 0 {
		return "-"
	}
	return strconv.FormatInt(n, 10) + "s"
}

// FieldCost is a landed primary's own total cost (its total's charged figure, written as
// it lands): an exact decimal in USD, absent when nothing of it was priced. It is read for
// the stream's cost per landed card alone (TierCosts.PerLanded); the stream's cost is every
// card's (StreamSpend), and no control card carries a sum of its own (the owner, 2026-10-11:
// "capture all cost per-card, on the card itself, and then these cards sum up to the
// stream cost").
const FieldCost = "cost"

// What a card cost, carried (the owner, 2026-10-11: "The cost per-work stream should include
// all costs, including reading, fix, whatever done to land the card, such that the total cost
// of all work streams = the total cost of the sprint so far"). A card recut or replaced by its
// twin (Replace, Recut) is dropped "replaced by <twin>" and its spend goes on with the work:
// the twin is created with FieldCostCarried, the exact sum of each old card's own charged
// total and what that card carried in turn, and each old card is marked FieldCostCarriedTo
// the twin. The once-only rule: a card marked carried-to is left out of its stream's sum
// (StreamSpend), its spend counted once, on the twin that carries it. A card dropped by any
// other path keeps its cost and is summed where it stands, off the table or not.
const (
	FieldCostCarried   = "cost_carried"    // exact USD the card carries from the cards it replaced
	FieldCostCarriedTo = "cost_carried_to" // on a replaced card: the twin its spend was carried to
)

// FieldCostOff is a stream's control card's list of its primaries taken off the work table
// (Drop, and the drop of a replace), comma separated: a card off the table is no cell's, so
// the snapshot the cost view is counted from reads it by id (CostOffIDs), and its cost stays
// in its stream's sum. The ids, not a sum: the cost is the card's, read from the card.
const FieldCostOff = "cost_off"

// CostOffIDs is every primary off the work table whose cost its stream counts, for the
// read that counts the cost view (the where record, a stats reset's mark): the ids each
// stream's control card lists (FieldCostOff), and the cards each card on the table replaced
// (FieldReplaces; a twin from before the list was kept names its old card), less those on
// the table. Named over the placed cards alone, as the read's extras are (a fresh read names
// them before it reads any record off the table), so the twin and a fresh read agree.
func CostOffIDs(s *Snapshot) []string {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		if id != "" && !seen[id] && s.Work.Placed(id) == nil {
			seen[id] = true
			out = append(out, id)
		}
	}
	if s.Merge != nil {
		for _, st := range s.Work.Rows() {
			if ctl := s.StreamCtl(st); ctl != nil {
				for _, id := range Split(ctl.F(FieldCostOff)) {
					add(id)
				}
			}
		}
	}
	for _, c := range s.Work.Column(States...) {
		for _, id := range Split(c.F(FieldReplaces)) {
			add(id)
		}
	}
	slices.Sort(out)
	return out
}

// ConsumerDecide is the kind of a nova-decide call's record on the card it decided (a
// brief decision at add, a landed card's score, a red gate's share): what the decision
// layer spent for the card, beside its work and its reads.
const ConsumerDecide = "decide"

// DecideModel is the model a decide record names, provider/model: a route of that model
// in the store prices it (priceRoute), else it is kept unpriced=no-route with its tokens.
const DecideModel = "typesafe/jev-latest"

// UsageEstimated is the usage word a decide record carries when its backend reported no
// token and the input is estimated from the text asked over (EstimateTokens).
const UsageEstimated = "tokens=estimated"

// EstimateTokens is the input tokens a text is estimated at when a backend reports none:
// one token for every four bytes, rounded up. An estimate is marked (UsageEstimated); it is
// a count, never a price.
func EstimateTokens(text string) int64 { return int64((len(text) + 3) / 4) }

// DecideUsage is a decide call's usage line (cardcost.ParseUsage): the tokens the backend
// reported, else the input estimated from the state it asked over, marked so.
func DecideUsage(input, output int, state string) string {
	if input > 0 || output > 0 {
		return fmt.Sprintf("input=%d output=%d model=%s", input, output, DecideModel)
	}
	return fmt.Sprintf("input=%d model=%s %s", EstimateTokens(state), DecideModel, UsageEstimated)
}

// SplitDecideUsage splits one decide call's usage (DecideUsage's line) over n cards: each
// token class divided, the remainder to the first cards, so the parts sum to the whole.
func SplitDecideUsage(line string, n int) []string {
	if n <= 0 {
		return nil
	}
	u := cardcost.ParseUsage(line)
	part := func(v int64, i int) int64 {
		if v < 0 {
			return v
		}
		q := v / int64(n)
		if int64(i) < v%int64(n) {
			q++
		}
		return q
	}
	out := make([]string, n)
	for i := range out {
		w := []string{}
		if v := part(u.Tokens.Input, i); v >= 0 {
			w = append(w, "input="+strconv.FormatInt(v, 10))
		}
		if v := part(u.Tokens.Output, i); v >= 0 {
			w = append(w, "output="+strconv.FormatInt(v, 10))
		}
		w = append(w, "model="+cmp.Or(u.Model, DecideModel))
		w = append(w, u.Extra...)
		out[i] = strings.Join(w, " ")
	}
	return out
}

// decideConsumer is one decide call's record for the card id: what it decided (what: brief,
// score, gate), its op in the decision record, its usage (DecideUsage) priced by a route of
// its model where the store holds one, never by a price made up. The key is the card's and
// the op's, so a replay of the same call records nothing twice (addConsumer).
func decideConsumer(s *Snapshot, id, what, op, usage string) Consumer {
	u := cardcost.ParseUsage(usage)
	model := cmp.Or(u.Model, DecideModel)
	var priced cardcost.Usage
	if r, ok := s.priceRoute("", model, false); ok && u.Tokens.Reported() && !slices.Contains(u.Extra, UsageEstimated) {
		// an estimated count is kept as tokens, never priced: a cost is never guessed
		priced = u.Priced(r.Name, r.Prices)
	} else {
		priced = u.Priced("", cardcost.Prices{})
	}
	priced.Model = model
	sum := sha256.Sum256([]byte(op))
	return Consumer{Kind: ConsumerDecide, Card: id, Who: "nova-decide", Model: model, Tier: ConsumerDecide, End: what, At: stamp(s.Now),
		Key: id + "#d" + hex.EncodeToString(sum[:6]), Usage: priced}
}

// DecideCostReq is decide calls' spend reported on the cards they decided: one usage line
// per card (a gate's call split over its batch beforehand, SplitDecideUsage).
type DecideCostReq struct {
	Stream string
	What   string            // brief, score or gate
	Op     string            // the call's op in its decision record
	Usage  map[string]string // card id -> its usage line
	Who    string
}

// RecordDecideCost writes each card's decide record on it (addConsumer): a card no longer
// on the table is refused, by name, its share not written.
func RecordDecideCost(s *Snapshot, r DecideCostReq) Plan {
	var p Plan
	for _, id := range slices.Sorted(maps.Keys(r.Usage)) {
		c := s.Work.Placed(id)
		if c == nil {
			p.refuse(id, "not on the table ("+placeWord(orEmpty(s.Work.Card(id), id))+"); its share of the decide call "+r.Op+" was not written")
			continue
		}
		set := map[string]string{}
		addConsumer(c, set, decideConsumer(s, id, r.What, r.Op, r.Usage[id]))
		if len(set) == 0 {
			continue // recorded already
		}
		if v := cardcost.ParseTotal(set[FieldCostTotal]).Charged; c.Col == Landed && v != "" && set[FieldCostTotal] != "" {
			set[FieldCost] = v // a landed card's own cost, which per landed divides
		}
		p.Units = append(p.Units, Unit{Key: id, Stream: c.Row, Changes: []Change{change(Work, setEntry(c, set))},
			Moved: id + " decide " + r.What + " cost recorded"})
	}
	return p
}

// CostCell is a stream's whole spend (TierCosts.TotalCost, dollars and cents) as the work
// table's cost cell shows it: "-" when nothing of it was priced.
func CostCell(totalCost string) string { return cmp.Or(totalCost, "-") }

// MoneyText is a cost as the work table's cost cell shows it: US dollars and cents,
// rounded up to the next cent ("$1.24" for 1.2345; nothing past the cent is
// shown), "-"
// when there is none. The card keeps the exact figure (FieldCost, FieldCostTotal); only
// what is shown is rounded.
func MoneyText(usd string) string {
	if usd == "" {
		return "-"
	}
	r, ok := new(big.Rat).SetString(usd)
	if !ok {
		return "-"
	}
	return cardcost.Cents(r)
}

// receiptBillingRefusal fences typed billing activation at the durable consumer write.
// The declared route is authoritative; raw evidence never chooses its own billing.
func (s *Snapshot) receiptBillingRefusal(c *Card, usage string) string {
	u, err := cardcost.ParseUsageChecked(usage)
	if err != nil {
		return "usage receipt refused: " + err.Error()
	}
	rt, ok := s.priceRoute(c.F(FieldRoute), c.F(FieldModel), false)
	if !ok && c.F(FieldBilling) == "" {
		if u.ReceiptVersion == 2 {
			return "receipt contract 2 needs an applied route billing declaration; adopt the paired config and consumer before reporting"
		}
		return ""
	}
	// A launch's declaration is immutable through later configuration changes.
	billing := c.F(FieldBilling)
	if billing == "" {
		billing = s.billingPrices(rt).Billing
	}
	if u.ReceiptVersion == 0 && u.Billing != "" && c.F(FieldBilling) != "" && u.Billing != c.F(FieldBilling) {
		return "legacy receipt billing disagrees with the original launch declaration; resend the original receipt without changing its billing provenance"
	}
	if billing == cardcost.BillingSubscription && u.ReceiptVersion != 2 {
		return "typed subscription route requires receipt contract 2; adopt paired tools/sprint before activating subscription, then resend the original receipt"
	}
	if u.ReceiptVersion == 2 {
		if _, friend := FriendOfRow(c.Row); friend {
			return "receipt contract 2 is fleet execution only; retain the existing friend receipt contract rather than claim fleet raw provenance"
		}
		if billing == "" || u.Billing != billing {
			return "receipt billing disagrees with applied route/provider declaration; report the billing declared by the original launch packet after paired adoption"
		}
		if cmp.Or(c.F(FieldHarness), rt.Harness) == "muse" && u.Raw == nil {
			return "Muse receipt contract 2 requires raw_usage provenance; resend the original durable native receipt"
		}
	}
	return ""
}
