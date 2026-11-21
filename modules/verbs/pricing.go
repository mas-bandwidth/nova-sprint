package verbs

import (
	"context"

	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/mas-bandwidth/nova-sprint/modules/usage"
)

// priceReport is the engine pricing a row's report (package usage; the
// owner, 2026-11-07: "api rate work should be priced *exactly*. friend sub
// rate should be free."). The verb carries the row's words and its --cost;
// the price is made here, never by the row: the holder row's harness in the
// roster decides the billing (nova-worker is a bench on API keys; any other
// harness a kin's subscription seat), and an API run is priced by its
// model's nova-config model record (nova-config model set <provider>/<model>
// --price_in <n> --price_out <n>). A row the roster does not name is billed
// as API; a roster that cannot be read records an API run
// unpriced=roster-unreadable, never priced from another sheet and never a
// silent $0.
func (e *Engine) priceReport(ctx context.Context, snap store.Snapshot, row string, o outcome) (usage.Priced, error) {
	billing, sheet, why := e.billingOf(ctx, snap, row)
	charged := ""
	if o.costGiven && o.priced {
		charged = usage.USD(o.cost)
	}
	p, err := usage.Price(o.usage, billing, charged, sheet)
	if err != nil {
		return p, RefusalOr(err, misuse(o.verb, "--usage: "+err.Error()+" (the words are input=<n> cache_read=<n> cache_write=<n> output=<n> reasoning=<n> model=<provider/model>)"))
	}
	if why != "" && p.Record.Billing == usage.BillingAPI {
		p.Record.Unpriced, p.Cost, p.Priced = why, 0, false
	}
	return p, nil
}

// whyRosterUnreadable is why a report is unpriced when the roster, which
// carries both the billing and the prices, cannot be read.
const whyRosterUnreadable = "roster-unreadable"

// billingOf is the row's billing and the model records' price sheet (the
// roster's Prices), and why they cannot be trusted ("" when they can): the
// roster could not be read. The active policy gives only the roster's width
// cap, which moves no billing and no price, so a policy that cannot be read
// reads the roster at cap 1.
func (e *Engine) billingOf(ctx context.Context, snap store.Snapshot, row string) (string, usage.Sheet, string) {
	lim := policy.Limits{}
	if a, err := e.answers(ctx, snap); err == nil {
		lim = a.Limits
	}
	ro, err := e.roster(ctx, lim, snap)
	if err != nil {
		return usage.BillingAPI, nil, whyRosterUnreadable
	}
	return rowBilling(ro, row), Sheet(ro.Prices), ""
}

// rowBilling is the billing of row's runs: its harness's, and API for a
// row the roster does not name.
func rowBilling(ro roster.Roster, row string) string {
	if k, ok := ro.Kin[row]; ok {
		return usage.Billing(k.Harness)
	}
	if w, ok := ro.Workers[row]; ok {
		return usage.Billing(w.Harness)
	}
	return usage.BillingAPI
}

// Sheet is the model records' prices as package usage reads them: input,
// cache read (the record's price_cache_read, its input price when unset;
// run.ConfigPrices) and output. Cache writes are input tokens priced at the
// input price: the record holds no cache-write rate, and a guessed one
// would not be exact.
func Sheet(p map[string]roster.Price) usage.Sheet {
	out := usage.Sheet{}
	for m, r := range p {
		out[m] = usage.Rate{In: r.In, CacheRead: r.CacheRead, CacheWrite: r.In, Out: r.Out}
	}
	return out
}
