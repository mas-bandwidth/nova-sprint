package verbs

import (
	"cmp"
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
// harness a kin's subscription seat), and an API run is priced by the
// active policy's prices(). A row the roster does not name is billed as
// API; a roster or active policy that cannot be read records an API run
// unpriced=roster-unreadable or unpriced=policy-unreadable, never priced
// from another sheet and never a silent $0.
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

// Why a report is unpriced when what prices it cannot be read.
const (
	whyPolicyUnreadable = "policy-unreadable"
	whyRosterUnreadable = "roster-unreadable"
)

// billingOf is the row's billing and the active policy's price sheet, and
// why they cannot be trusted ("" when they can): the active policy or the
// roster could not be read.
func (e *Engine) billingOf(ctx context.Context, snap store.Snapshot, row string) (string, usage.Sheet, string) {
	billing, why := usage.BillingAPI, ""
	if e.Answers == nil {
		return billing, nil, why
	}
	vs := policy.Versions{Seq: snap.Seq}
	if snap.Policy > 0 {
		if pv, err := e.Store.Policy(ctx, snap.Policy); err == nil {
			vs.Active = &pv
		} else {
			why = whyPolicyUnreadable
		}
	}
	a, failed := e.Answers.AnswerContext(ctx, vs, snap, roster.Roster{}, policy.Questions{})
	if len(failed) > 0 || a.Version < 0 {
		why = whyPolicyUnreadable
	}
	if a.Version < 0 {
		return billing, nil, why
	}
	ro, err := e.roster(ctx, a.Limits, snap)
	if err != nil {
		return billing, Sheet(a.Prices), cmp.Or(why, whyRosterUnreadable)
	}
	return rowBilling(ro, row), Sheet(a.Prices), why
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
