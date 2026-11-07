package store

// Trace validation of the store (tla/README.md, "Trace validation"). A
// seeded random walk applies ops to a store and records, after every step,
// the step (Apply, LoseRequest or Replay of an op) and the store's state
// projected by TraceState; tla/StoreTrace.tla checks the recording is a
// behaviour of tla/Store.tla. The walks write their traces only when
// NOVA_TRACE_DIR names a directory (tla/make-traces.sh sets it), so the
// normal suite runs none of them; TestTraceRefusesDivergence is the one
// that always runs.

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// traceEnv names the directory the walks write their traces into.
const traceEnv = "NOVA_TRACE_DIR"

// The walk's names: Store.tla's universes in the trace (TCards and the
// others in StoreTraceLog.tla).
var (
	twProducers  = []string{"p1", "p2"}
	twCards      = []string{"p1", "p2", "t1", "u1", "u2", "u3"}
	twRows       = []string{"r1", "r2"}
	twJudgements = []string{"j1", "j2"}
	twCursors    = []string{"c1"}
	twBodies     = []string{"a", "b"}
	twTiers      = []string{"flash", "pro"}
)

// traceSeeds is the fixed seed set of each walk, and its length.
var traceSeeds = map[string][]uint64{"fake": {1, 2, 3, 4, 5, 6, 7, 8}, "pg": {1, 2}}

const traceSteps = 300

// TestTraceWalk writes the Fake walk's traces (store-fake-<seed>).
func TestTraceWalk(t *testing.T) {
	dir := os.Getenv(traceEnv)
	if dir == "" {
		t.Skip("trace walks run under tla/make-traces.sh (" + traceEnv + " unset)")
	}
	for _, seed := range traceSeeds["fake"] {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			t.Parallel()
			f := NewFake()
			w := newStoreWalk(t, f, f, seed)
			w.run(traceSteps)
			require.NoError(t, w.tr.write(filepath.Join(dir, fmt.Sprintf("store-fake-%d", seed)), "matched"))
		})
	}
}

// TestFunctionalTraceWalk is the same walk on PostgreSQL, with no transport
// faults (store-pg-<seed>).
func TestFunctionalTraceWalk(t *testing.T) {
	dir := os.Getenv(traceEnv)
	if dir == "" {
		t.Skip("trace walks run under tla/make-traces.sh (" + traceEnv + " unset)")
	}
	for _, seed := range traceSeeds["pg"] {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			t.Parallel()
			w := newStoreWalk(t, openPG(t, startPG(t)), nil, seed)
			w.run(traceSteps)
			require.NoError(t, w.tr.write(filepath.Join(dir, fmt.Sprintf("store-pg-%d", seed)), "matched"))
		})
	}
}

// guardDropped is the divergence breaker: a store that drops every RowHeld
// condition before the Fake sees the op, as a Fake whose RowHeld guard was
// deleted would. The trace still records the op as sent.
type guardDropped struct{ *Fake }

func (g guardDropped) Apply(ctx context.Context, op Op) (Receipt, error) {
	op.Expect = slices.DeleteFunc(slices.Clone(op.Expect), func(e Expect) bool { _, ok := e.(RowHeld); return ok })
	return g.Fake.Apply(ctx, op)
}

// The breaker's script: a row is held, then a deal fenced on it not being
// held. The honest Fake refuses the deal; the breaker lands it.
func breakerScript(w *storeWalk) {
	w.script(nil, PutStream{Name: "s1", Repo: "git@example.com:r.git", Base: "main", Pos: 1}, PutRow{Name: "r1", Kind: "worker", Width: 1, Tiers: []string{"pro"}},
		SetRowState{Row: "r1", State: "up"}, PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro"})
	w.script(nil, SetRowHeld{Row: "r1", Held: true, Reason: "h"})
	w.script([]Expect{RowHeld{Row: "r1", Held: false}}, PutCard{ID: "u1", Kind: "work", Of: "p1", Stream: "s1", Tier: "pro", Holder: "r1"}, SetCardState{Card: "u1", State: "working"})
}

// TestTraceRefusesDivergence holds the breaker: the honest Fake's trace
// refuses the fenced deal (it stays a refusal), the breaker's lands it, and
// the breaker's trace is the fixture tla/traces/store-breaker, which
// check-trace.sh must refuse at its last step (the trace job runs it).
func TestTraceRefusesDivergence(t *testing.T) {
	honest := newStoreWalk(t, NewFake(), nil, 0)
	breakerScript(honest)
	f := NewFake()
	broken := newStoreWalk(t, guardDropped{f}, nil, 0)
	breakerScript(broken)
	require.Contains(t, honest.tr.recs[len(honest.tr.recs)-1], "accepted |-> FALSE")
	require.Contains(t, broken.tr.recs[len(broken.tr.recs)-1], "accepted |-> TRUE")
	dir := t.TempDir()
	require.NoError(t, broken.tr.write(dir, fmt.Sprintf("forbidden at %d", len(broken.tr.recs))))
	for _, name := range []string{"StoreTraceLog.tla", "StoreTrace.cfg"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		want, err := os.ReadFile(filepath.Join("..", "tla", "traces", "store-breaker", name))
		require.NoError(t, err, "the fixture: NOVA_TRACE_DIR=tla/traces go test -run TestTraceRefusesDivergence ./store/ writes it")
		require.Equal(t, string(want), string(got), "tla/traces/store-breaker/%s is stale", name)
	}
	if out := os.Getenv(traceEnv); out != "" {
		require.NoError(t, broken.tr.write(filepath.Join(out, "store-breaker"), fmt.Sprintf("forbidden at %d", len(broken.tr.recs))))
	}
}

// storeWalk is one walk: the store, the Fake under it when transport faults
// are on, the trace, and the walk's bookkeeping.
type storeWalk struct {
	t       *testing.T
	s       Store
	fake    *Fake // nil: no transport faults
	rng     *rand.Rand
	n       int
	tr      *storeTrace
	applied map[string]bool // ops with an event
	lost    map[string]int
}

func newStoreWalk(t *testing.T, s Store, fake *Fake, seed uint64) *storeWalk {
	w := &storeWalk{t: t, s: s, fake: fake, rng: rand.New(rand.NewPCG(seed, 7)), applied: map[string]bool{}, lost: map[string]int{}}
	w.tr = newStoreTrace(w.now())
	return w
}

// now is the store's state in the model's terms.
func (w *storeWalk) now() ModelState {
	ctx := context.Background()
	s, err := w.s.Snapshot(ctx)
	require.NoError(w.t, err)
	var ps []PolicyVersion
	for v := 1; ; v++ {
		p, err := w.s.Policy(ctx, v)
		if errors.Is(err, ErrNotFound) {
			break
		}
		require.NoError(w.t, err)
		ps = append(ps, p)
	}
	return TraceState(s, ps)
}

func (w *storeWalk) snap() Snapshot {
	s, err := w.s.Snapshot(context.Background())
	require.NoError(w.t, err)
	return s
}

// script applies one op of the given conditions and writes, in the epoch.
func (w *storeWalk) script(expect []Expect, writes ...Write) {
	w.n++
	op := Op{ID: fmt.Sprintf("o%d", w.n), Epoch: w.snap().Epoch, Actor: "walk", Kind: "walk", At: t0.Add(time.Duration(w.n) * time.Second), Expect: expect, Writes: writes}
	w.settle(op, false)
}

// run takes steps random ops.
func (w *storeWalk) run(steps int) {
	for range steps {
		op, clear := w.next()
		if err := w.valid(op, clear); err != nil {
			continue // ErrInvalid never enters the store, so it is no step
		}
		w.settle(op, clear)
	}
}

func (w *storeWalk) valid(op Op, clear bool) error {
	if clear {
		return checkClear(op)
	}
	return check(op)
}

// settle applies op until the caller knows its outcome: a transport fault
// is followed by a Replay or by the Apply again, as a caller does.
func (w *storeWalk) settle(op Op, clear bool) {
	for {
		landed, dropped := w.fault(op)
		r, err := w.do(op, clear)
		switch {
		case dropped && !landed:
			require.ErrorIs(w.t, err, ErrUnknown)
			w.tr.step("LoseRequest", op, clear, nil, w.now())
			continue
		case dropped:
			require.ErrorIs(w.t, err, ErrUnknown)
			w.applied[op.ID] = true
			w.tr.step("Apply", op, clear, nil, w.now())
			if w.rng.IntN(2) == 0 {
				r, ok, err := w.s.Replay(context.Background(), op.ID)
				require.NoError(w.t, err)
				require.True(w.t, ok)
				w.tr.step("Replay", op, clear, &r.Accepted, w.now())
				return
			}
			continue
		}
		require.NoError(w.t, err, "%s", op.ID)
		w.applied[op.ID] = true
		w.tr.step("Apply", op, clear, &r.Accepted, w.now())
		return
	}
}

// fault arms a transport fault on the Fake now and then: lost before the
// store sees the op, or after it committed (only on an op with no event,
// since a replayed receipt has no transport of its own in the model).
func (w *storeWalk) fault(op Op) (landed, dropped bool) {
	if w.fake == nil || w.lost[op.ID] >= traceMaxLoss || w.rng.IntN(15) > 0 {
		return false, false
	}
	landed = !w.applied[op.ID] && w.rng.IntN(2) == 0
	w.fake.Drop(op.ID, landed)
	w.lost[op.ID]++
	return landed, true
}

func (w *storeWalk) do(op Op, clear bool) (Receipt, error) {
	if clear {
		return w.s.Clear(context.Background(), op)
	}
	return w.s.Apply(context.Background(), op)
}

// traceMaxLoss is MaxLoss in the trace's cfg: transport faults per op.
const traceMaxLoss = 2
