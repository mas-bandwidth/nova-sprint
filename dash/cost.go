package dash

import (
	"math/big"
	"sort"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
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

// costSummary is the sprint's whole spend as the top strip shows it. Amounts
// are currency strings; "-" where no priced run exists, never a false $0.
type costSummary struct {
	Total       string            `json:"total"`
	PerCard     string            `json:"per_card"` // total over the priced landed cards
	ByTier      map[string]string `json:"by_tier"`
	Priced      int               `json:"priced_runs"`
	Unpriced    int               `json:"unpriced_runs"` // finished runs (done or fail) that reported no cost
	Landed      int               `json:"landed_cards"`  // cards landed in this run
	PricedCards int               `json:"priced_cards"`  // of those, the cards whose own runs carried cost: the per-card divisor
}

// summarizeCost sums every priced consumer (work and read) in any state, by
// tier, and divides by the landed cards whose own runs carried cost (imported
// v1 landings, and landed cards no run of which reported a cost, are left out
// of the divisor).
func summarizeCost(s store.Snapshot) *costSummary {
	out := &costSummary{ByTier: map[string]string{}}
	micros := map[string]*big.Int{}
	total := new(big.Int)
	pricedOf := map[string]bool{}
	for _, c := range s.Cards {
		switch c.Kind {
		case "work", "read":
			if c.Priced {
				pricedOf[c.Of] = true
				out.Priced++
				if micros[c.Tier] == nil {
					micros[c.Tier] = new(big.Int)
				}
				micros[c.Tier].Add(micros[c.Tier], big.NewInt(c.Cost))
				total.Add(total, big.NewInt(c.Cost))
			} else if c.State == "done" || c.State == "fail" {
				out.Unpriced++
			}
		}
	}
	for _, c := range s.Cards {
		if c.Kind == "producer" && c.State == "landed" && !strings.HasPrefix(c.Reason, store.ImportedReason) {
			out.Landed++
			if pricedOf[c.ID] {
				out.PricedCards++
			}
		}
	}
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
	out.PerCard = "-"
	if out.PricedCards > 0 {
		per := new(big.Int).Add(rounded, big.NewInt(int64(out.PricedCards-1)))
		out.PerCard = moneyCell(per.Div(per, big.NewInt(int64(out.PricedCards))))
	}
	return out
}
