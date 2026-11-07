package verbs

import (
	"context"

	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/usage"
)

// priceReport is the engine pricing a row's report (package usage; the
// owner, 2026-11-07: "api rate work should be priced *exactly*. friend sub
// rate should be free."). The verb carries the row's words and its --cost;
// the price is made here, never by the row: the holder row's harness in the
// roster decides the billing (nova-worker is a bench on API keys; any other
// harness a kin's subscription seat), and an API run is priced by the
// active policy's prices(). A row the roster does not name, or a roster or
// policy that cannot be read, is billed as API: its run is then priced from
// its tokens or recorded unpriced, never a silent $0.
func (e *Engine) priceReport(ctx context.Context, snap store.Snapshot, row string, o outcome) (usage.Priced, error) {
	billing, sheet := e.billingOf(ctx, snap, row)
	charged := ""
	if o.costGiven && o.priced {
		charged = usage.USD(o.cost)
	}
	p, err := usage.Price(o.usage, billing, charged, sheet)
	if err != nil {
		return p, RefusalOr(err, misuse(o.verb, "--usage: "+err.Error()+" (the words are input=<n> cache_read=<n> cache_write=<n> output=<n> reasoning=<n> model=<provider/model>)"))
	}
	return p, nil
}

// billingOf is the row's billing and the active policy's price sheet.
func (e *Engine) billingOf(ctx context.Context, snap store.Snapshot, row string) (string, usage.Sheet) {
	billing := usage.BillingAPI
	if e.Answers == nil {
		return billing, nil
	}
	vs := policy.Versions{Seq: snap.Seq}
	if snap.Policy > 0 {
		if pv, err := e.Store.Policy(ctx, snap.Policy); err == nil {
			vs.Active = &pv
		}
	}
	a, _ := e.Answers.AnswerContext(ctx, vs, snap, roster.Roster{}, policy.Questions{})
	if a.Version >= 0 {
		if ro, err := e.roster(ctx, a.Limits, snap); err == nil {
			billing = rowBilling(ro, row)
		}
	}
	return billing, Sheet(a.Prices)
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

// Sheet is the policy's prices() as package usage reads them.
func Sheet(p map[string]policy.Price) usage.Sheet {
	out := usage.Sheet{}
	for m, r := range p {
		out[m] = usage.Rate{In: r.In, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite, Out: r.Out}
	}
	return out
}
