package run

import (
	"context"
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// State returns an isolated snapshot for a dashboard or pusher.
func (e *Engine) State() store.Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return snapshotCopy(e.state)
}

// activePolicy is immutable by version; activation may change separately.
func (e *Engine) activePolicy(ctx context.Context) (*store.PolicyVersion, error) {
	version := e.state.Policy
	if version == 0 {
		return nil, nil
	}
	p, ok := e.policy[version]
	if !ok {
		var err error
		p, err = e.store.Policy(ctx, version)
		if err != nil {
			return nil, fmt.Errorf("run: policy %d: %w", version, err)
		}
		e.policy[version] = p
	}
	return &p, nil
}

// Planned policy is trace data, distinct from Event.Policy at commit time.
func annotatePolicy(ops []store.Op, version int) []store.Op {
	out := append([]store.Op(nil), ops...)
	for i := range out {
		marker := fmt.Sprintf("planned_policy=%d", version)
		if out[i].Note != "" {
			out[i].Note += "; "
		}
		out[i].Note += marker
	}
	return out
}
func (e *Engine) plannedPolicy() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastPlannedPolicy
}

// FreshState says whether the latest tick completed its latest event fold.
func (e *Engine) FreshState() (store.Snapshot, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return snapshotCopy(e.state), e.folded
}
