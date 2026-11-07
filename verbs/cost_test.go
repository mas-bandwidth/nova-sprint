package verbs

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/cost"
)

// fixedReader answers each read with the next balance.
type fixedReader struct{ usd []string }

func (f *fixedReader) Read(context.Context) cost.Reading {
	u := f.usd[0]
	f.usd = f.usd[1:]
	return cost.Reading{Provider: "acme", Known: true, USD: u}
}

func TestCostReconcile(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.e.CostReaders = []cost.Reader{&fixedReader{usd: []string{"50", "40"}}, cost.NoBalance{Provider: "zeta"}}
	want(t, w.do("carol", "cost", "reconcile"), 0, "COST acme balance=$50: the first read", "COST zeta unknown")
	want(t, w.do("carol", "cost", "reconcile"), 0, "provider=$10 recorded=$0 gap=$10 judgement=cost-gap.carol.1", "next: nova-sprint judge cost-gap.carol.1 ack|retry")
	if j := w.snap(t).Judgements["cost-gap.carol.1"]; j.State != "open" || j.Kind != "cost-gap" {
		t.Fatalf("judgement %+v", j)
	}
	want(t, w.do("alice", "cost", "reconcile"), 1)
}
