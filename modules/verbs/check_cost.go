package verbs

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/mas-bandwidth/nova-sprint/modules/usage"
)

// checkCost is check's one line of the sprint's spend, the accounting the
// board does not show (the board says "$x per-card" and nothing else): the
// API dollars over every priced run of every kind and result, how many of
// those were priced at the report and how many at read from the built-in
// table (recorded unpriced=no-price when the policy had no price for the
// model), the finished API runs still unpriced and why, with the models no
// sheet prices, and the seat runs with their tokens.
func checkCost(r *checkReport, snap store.Snapshot) {
	var micro, apiTokens, seatTokens int64
	var stored, tabled, seats, noTokens, noPrice, other int
	missing := map[string]bool{}
	for _, c := range snap.Cards {
		if c.Kind != "work" && c.Kind != "read" {
			continue
		}
		rec := usage.MustParse(c.Usage)
		cost, priced := usage.Repriced(c.Usage, c.Cost, c.Priced)
		switch {
		case rec.Billing == usage.BillingSubscription:
			seats++
			seatTokens += rec.Tokens.Total()
		case priced:
			micro += cost
			apiTokens += rec.Tokens.Total()
			if c.Priced {
				stored++
			} else {
				tabled++
			}
		case c.Result != "": // a run that reported; a consumer withdrawn before it ran has no result and spent nothing
			switch rec.Unpriced {
			case usage.WhyNoPrice:
				noPrice++
				missing[cmp.Or(rec.Model, "(no model)")] = true
			case usage.WhyNoTokens:
				noTokens++
			default:
				other++ // policy-unreadable or roster-unreadable at the report
			}
		}
	}
	models := ""
	if len(missing) > 0 {
		models = " (" + strings.Join(slices.Sorted(maps.Keys(missing)), ", ") + ")"
	}
	r.ok("cost: $%s API over %d priced runs (%d priced at the report, %d from the built-in table at read; %s tokens); %d unpriced: %d no tokens, %d no price%s, %d sheet unreadable; %d seat runs, %s tokens",
		usage.USD(micro), stored+tabled, stored, tabled, usage.Compact(apiTokens), noTokens+noPrice+other, noTokens, noPrice, models, other, seats, usage.Compact(seatTokens))
}
