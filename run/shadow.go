package run

import (
	"context"
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// ShadowResult reports a snapshot-local dry run. It cannot model concurrent
// writers or recognize operation IDs used before the snapshot.
type ShadowResult struct {
	Plan          TickPlan
	Valid         bool
	Validation    string
	Seq           int64
	Epoch         int
	PlannedPolicy int
	Receipts      []store.Receipt
	Failure       string
	FailedOp      string
}

func Shadow(ctx context.Context, s store.Store, steps Steps, input TickInput) (ShadowResult, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return ShadowResult{}, fmt.Errorf("shadow: snapshot: %w", err)
	}
	policies, err := loadPolicyHistory(ctx, s, snap.Seq)
	if err != nil {
		return ShadowResult{}, err
	}
	streamPolicies := map[string]streamPolicy{}
	collectStreamPolicies(streamPolicies, []store.Event{{Accepted: true, After: &store.State{Policies: policies}}})
	input.Streams, err = streamSettingsFor(snap, streamPolicies)
	if err != nil {
		return ShadowResult{}, err
	}
	if snap.Policy != 0 {
		policy, ok := policies[snap.Policy]
		if !ok {
			return ShadowResult{}, fmt.Errorf("shadow: active policy missing from snapshot history")
		}
		input.Policy = &policy
	}
	plan, err := steps.Tick(ctx, snapshotCopy(snap), input)
	if err != nil {
		return ShadowResult{}, fmt.Errorf("shadow: steps: %w", err)
	}
	versions := make([]store.PolicyVersion, 0, len(policies))
	for _, p := range policies {
		versions = append(versions, p)
	}
	fake, err := store.NewFakeFrom(snap, versions...)
	if err != nil {
		return ShadowResult{}, fmt.Errorf("shadow: initialize: %w", err)
	}
	return validateShadow(ctx, fake, snap, plan)
}
func validateShadow(ctx context.Context, fake *store.Fake, snap store.Snapshot, plan TickPlan) (ShadowResult, error) {
	result := ShadowResult{Plan: plan, Valid: true, Seq: snap.Seq, Epoch: snap.Epoch, PlannedPolicy: snap.Policy, Validation: fmt.Sprintf("valid at seq %d; concurrency and IDs used before the snapshot are not modelled", snap.Seq)}
	for _, op := range plan.Ops {
		receipt, err := fake.Apply(ctx, op)
		if err != nil {
			result.Valid = false
			result.FailedOp = op.ID
			result.Failure = err.Error()
			result.Validation = fmt.Sprintf("invalid op %s at seq %d; concurrency and earlier IDs are not modelled", op.ID, snap.Seq)
			return result, nil
		}
		result.Receipts = append(result.Receipts, receipt)
		if !receipt.Accepted && result.Valid {
			result.Valid = false
			result.FailedOp = op.ID
			result.Failure = receipt.Reason
			result.Validation = fmt.Sprintf("refused op %s at seq %d; concurrency and earlier IDs are not modelled", op.ID, snap.Seq)
		}
	}
	return result, nil
}
