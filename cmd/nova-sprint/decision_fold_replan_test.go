package main

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// The engine calls a step's Plan once per attempt (internal/sprint/store/engine.go, the
// retry loop around `plan := sprint.Applied(snap, step.Plan(snap))`), so a value a refused
// attempt set on the receipt step's captured state survives into the next attempt that does
// not set it. The receipt plan assigns foldOwner, ownerReports and receiptApplied; a later
// attempt on a changed world that no longer folds must leave them at their value before the
// step, never at an earlier attempt's.
func TestTheFoldReceiptPlanKeepsOnlyItsLastAttempt(t *testing.T) {
	t.Parallel()
	at := time.Unix(1000, 0).UTC()
	stopped := func(time.Time, time.Time) time.Duration { return 0 }
	d := sprint.DecisionDelivery{Lane: "coordinator", To: "seat-a", Token: "tok1", Set: ""}
	job := sprint.Note{ID: "j1"}
	coordNote := sprint.Note{ID: "j1", Type: sprint.NDecisionDelivery, At: at,
		DecisionDelivery: &sprint.DecisionDelivery{Lane: "coordinator", To: "seat-a", Token: "tok1", Set: ""}}
	first := &sprint.Snapshot{
		Now:   at,
		Acked: []sprint.Open{{Key: "j1|sprint:done", Note: coordNote}},
	}
	fold := decisionFold{to: "seat-a", set: "", token: "tok1", attempted: true}
	var receiptApplied bool

	// First attempt: the fold applies (the owner set matches the coordinator's), so the plan
	// sets foldOwner and marks the receipt applied.
	decisionReceiptPlan(first, "coordinator", job, d, "seat-a", "", stopped, &fold, &receiptApplied)
	require.True(t, fold.foldOwner, "the first attempt folds")
	require.True(t, receiptApplied, "the first attempt applies the receipt")

	// Second attempt on a changed world: the coordinator note is gone, so the fold does not
	// apply and nothing assigns the captured fields. They must hold this attempt's value, not
	// the first attempt's stale one.
	second := &sprint.Snapshot{Now: at.Add(time.Second)}
	decisionReceiptPlan(second, "coordinator", job, d, "seat-a", "", stopped, &fold, &receiptApplied)
	require.False(t, fold.foldOwner, "a later attempt that does not fold leaves foldOwner false")
	require.Empty(t, fold.ownerReports, "a later attempt that does not fold leaves ownerReports empty")
	require.False(t, receiptApplied, "a later attempt that applies no receipt leaves receiptApplied false")
}
