package dash

import (
	"cmp"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/mas-bandwidth/nova-sprint/modules/usage"
)

type tierPart struct {
	name      string
	cents     *big.Int
	remainder int64
}

// partitionCents assigns the rounded total by largest fractional cent, with
// alphabetical ties. Inputs are nonnegative micro-dollar amounts.
func partitionCents(micros map[string]*big.Int) (*big.Int, map[string]*big.Int) {
	parts := make([]tierPart, 0, len(micros))
	total := new(big.Int)
	whole := new(big.Int)
	unit := big.NewInt(10000)
	for name, micro := range micros {
		q, r := new(big.Int), new(big.Int)
		q.QuoRem(micro, unit, r)
		parts = append(parts, tierPart{name, q, r.Int64()})
		total.Add(total, micro)
		whole.Add(whole, q)
	}
	rounded, rem := new(big.Int), new(big.Int)
	rounded.QuoRem(total, unit, rem)
	if rem.Sign() > 0 {
		rounded.Add(rounded, big.NewInt(1))
	}
	left := new(big.Int).Sub(rounded, whole).Int64()
	sort.Slice(parts, func(i, j int) bool {
		if parts[i].remainder == parts[j].remainder {
			return parts[i].name < parts[j].name
		}
		return parts[i].remainder > parts[j].remainder
	})
	result := make(map[string]*big.Int, len(parts))
	for i, p := range parts {
		if int64(i) < left {
			p.cents.Add(p.cents, big.NewInt(1))
		}
		result[p.name] = p.cents
	}
	return rounded, result
}

// finishCosts partitions the rounded stream total among its tiers.
func finishCosts(w *workRow) {
	if w.Cost == nil {
		return
	}
	w.Cost, w.ByTier = partitionCents(w.tierMicros)
}

// costSummary is the sprint's whole spend as the top strip shows it: the API
// spend (the owner, 2026-11-07: "api rate work should be priced *exactly*.
// friend sub rate should be free."), beside the tokens kin spent on their
// subscription seats. Amounts are currency strings; "-" where no priced API
// run exists, never a false $0.
type costSummary struct {
	Total       string            `json:"total"`    // every priced API run's cost
	PerCard     string            `json:"per_card"` // total over every landed card (seat cards at $0)
	ByTier      map[string]string `json:"by_tier"`
	Priced      int               `json:"priced_runs"`   // API runs priced from their tokens
	Unpriced    int               `json:"unpriced_runs"` // finished API runs with no price: NoTokens + NoPrice
	NoTokens    int               `json:"unpriced_no_tokens"`
	NoPrice     int               `json:"unpriced_no_price"`
	Missing     []string          `json:"missing_prices"` // the models of the no-price runs, which prices() leaves out
	Landed      int               `json:"landed_cards"`   // cards landed in this run
	PricedCards int               `json:"priced_cards"`   // of those, the cards whose own API runs were priced
	APITokens   string            `json:"api_tokens"`     // tokens of the priced API runs, compact (1.2M)
	SeatRuns    int               `json:"seat_runs"`      // runs on subscription seats: free
	SeatTokens  string            `json:"seat_tokens"`    // their tokens, compact; "0" when none reported
}

// summarizeCost sums every priced API consumer (work and read) in any state,
// by tier, and divides by every card landed in this run, a card with no
// priced API run (a seat card) counting at $0 (imported v1 landings are left
// out of the divisor). A subscription run adds its tokens, never
// dollars. A record from before round 25 (no usage) counts as it was
// recorded: priced or not.
func summarizeCost(s store.Snapshot) *costSummary {
	out := &costSummary{ByTier: map[string]string{}, Missing: []string{}}
	micros := map[string]*big.Int{}
	total := new(big.Int)
	pricedOf := map[string]bool{}
	missing := map[string]bool{}
	var apiTokens, seatTokens int64
	for _, c := range s.Cards {
		if c.Kind != "work" && c.Kind != "read" {
			continue
		}
		r := usage.MustParse(c.Usage)
		switch {
		case r.Billing == usage.BillingSubscription:
			out.SeatRuns++
			seatTokens += r.Tokens.Total()
		case c.Priced:
			pricedOf[c.Of] = true
			out.Priced++
			apiTokens += r.Tokens.Total()
			if micros[c.Tier] == nil {
				micros[c.Tier] = new(big.Int)
			}
			micros[c.Tier].Add(micros[c.Tier], big.NewInt(c.Cost))
			total.Add(total, big.NewInt(c.Cost))
		case c.State == "done" || c.State == "fail":
			out.Unpriced++
			if r.Unpriced == usage.WhyNoPrice {
				out.NoPrice++
				missing[cmp.Or(r.Model, "(no model)")] = true
			} else {
				out.NoTokens++
			}
		}
	}
	out.Missing = append(out.Missing, slices.Sorted(maps.Keys(missing))...)
	out.APITokens, out.SeatTokens = usage.Compact(apiTokens), usage.Compact(seatTokens)
	out.Landed, out.PricedCards = landedCards(s, pricedOf)
	if out.Priced == 0 {
		out.Total, out.PerCard = "-", "-"
		return out
	}
	rounded, split := partitionCents(micros)
	out.Total = moneyCell(rounded)
	for tier, cents := range split {
		if tier != "" {
			out.ByTier[tier] = moneyCell(cents)
		}
	}
	// Glenn 2026-11-07: the true average, API spend over every landed card,
	// seat cards at $0 ("with sub cards being free").
	out.PerCard = "-"
	if out.Landed > 0 {
		out.PerCard = perCardCell(micros, out.Landed)
	}
	return out
}

// landedCards counts the cards landed in this run (imported v1 landings left
// out), and of those the cards with a priced API run (pricedOf).
func landedCards(s store.Snapshot, pricedOf map[string]bool) (landed, priced int) {
	for _, c := range s.Cards {
		if c.Kind == "producer" && c.State == "landed" && !strings.HasPrefix(c.Reason, store.ImportedReason) {
			landed++
			if pricedOf[c.ID] {
				priced++
			}
		}
	}
	return landed, priced
}

// perCardCell is the average in micro-dollars per card, to the nearest cent, or
// to the nearest hundredth of a cent when the average is under a cent (Glenn
// 2026-11-07: never rounded up; $0.28 over 237 cards is not "$0.01").
func perCardCell(micros map[string]*big.Int, cards int) string {
	total := new(big.Int)
	for _, m := range micros {
		total.Add(total, m)
	}
	n := big.NewInt(int64(cards))
	per := new(big.Int).Div(new(big.Int).Add(total, new(big.Int).Div(n, big.NewInt(2))), n)
	if per.Cmp(big.NewInt(10000)) < 0 {
		hundredths := new(big.Int).Div(new(big.Int).Add(per, big.NewInt(50)), big.NewInt(100))
		return fmt.Sprintf("$0.%04d", hundredths.Int64())
	}
	cents := new(big.Int).Div(new(big.Int).Add(per, big.NewInt(5000)), big.NewInt(10000))
	return moneyCell(cents)
}
