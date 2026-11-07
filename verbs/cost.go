package verbs

import (
	"context"

	"github.com/mas-bandwidth/nova-sprint/cost"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// runCostReconcile is `cost reconcile` (package cost): each provider's
// balance read and recorded, its fall since the last read set beside the
// store's priced API records of the same window, the gap printed, and one
// cost-gap judgement opened on the coordinator's row when the gap is over 5%
// and over $1. The provider keys come from the environment (run it under
// nova-secrets exec, as the run loop's unit is); nothing reads a secrets file.
func runCostReconcile(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	st, ok := e.Store.(cost.Store)
	if !ok {
		return "", broken("this store keeps no provider balances", "nova-sprint check")
	}
	readers := e.CostReaders
	if readers == nil {
		readers = cost.Readers(nil)
	}
	lines, err := cost.Reconcile(ctx, st, e.Now(), in.actor, readers)
	if err != nil {
		return "", RefusalOr(err, broken("cost reconcile: "+clip(err.Error()), "nova-sprint cost reconcile --as "+in.actor))
	}
	out := make([]string, len(lines))
	next := "nova-sprint next --as " + in.actor
	for i, l := range lines {
		out[i] = l.Text()
		if l.Judgement != "" {
			next = "nova-sprint judge " + l.Judgement + " ack|retry --reason <why>"
		}
	}
	return done(next, out...), nil
}
