package run

import (
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Admission is the re-proof of add's admission at each stream's base as it
// is now (2026-11-08: 45 waiting cards failed admission after their
// stream's base moved, their PATHS gone, and only check run by hand showed
// it). When a stream's head moves (Edges.Heads; at the cold start, every
// stream with one), Prove runs check's admission of that stream's waiting
// and ready producers off the tick, and Unadmitted is the refusals the next
// tick passes to the steps (Inputs.Unadmitted), which deal none of them and
// open one admission judgement per stream. A stream is proved once per head;
// a proof that fails is said (EDGE FAULT) and the last answer stands until
// the base moves again; a proof that answers after a newer one began is
// dropped.
type Admission struct {
	// Admit is check's admission of stream's waiting and ready producers in
	// snap: each refused one's refusal, by id (verbs.Engine.AdmitStream).
	Admit func(ctx context.Context, snap store.Snapshot, stream string) (map[string]string, error)
	Fault func(what string, err error)

	mu      sync.Mutex
	proving map[string]string            // stream -> the head its latest proof is of
	refused map[string]map[string]string // stream -> card -> refusal
	wg      sync.WaitGroup
}

// Prove starts a proof of each stream whose head is not the one last
// proved; it never blocks the tick.
func (a *Admission) Prove(ctx context.Context, snap store.Snapshot, heads map[string]string) {
	if a == nil || a.Admit == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.proving == nil {
		a.proving, a.refused = map[string]string{}, map[string]map[string]string{}
	}
	for _, s := range slices.Sorted(maps.Keys(heads)) {
		h := heads[s]
		if h == "" || a.proving[s] == h {
			continue
		}
		a.proving[s] = h
		a.wg.Go(func() {
			refused, err := a.Admit(ctx, snap, s)
			if err != nil {
				if a.Fault != nil {
					a.Fault("admission stream "+s, err)
				}
				return
			}
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.proving[s] == h {
				a.refused[s] = refused
			}
		})
	}
}

// Wait waits for the proofs in flight (tests; run never waits).
func (a *Admission) Wait() { a.wg.Wait() }

// Unadmitted is every refused producer of the latest proofs, by id.
func (a *Admission) Unadmitted() map[string]string {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]string{}
	for _, m := range a.refused {
		maps.Copy(out, m)
	}
	return out
}
