package card

// Trace validation of the card machine (tla/README.md, "Trace
// validation"). The card walk (machineWalk, TestMachineWalk's) records, after
// every accepted op, the move and the store's state projected by TraceState;
// tla/CardMachineTrace.tla checks the recording is a behaviour of
// tla/CardMachine.tla. The walk writes its traces only when NOVA_TRACE_DIR
// names a directory (tla/make-traces.sh sets it); TestTraceRefusesDivergence
// always runs.

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/require"
)

const traceEnv = "NOVA_TRACE_DIR"

// The walk's fixed seed set (20 seeds, TestMachineWalk's 800 steps each).
const traceSeeds, traceSteps = 20, 800

// TestTraceMachineWalk writes the card walk's traces (card-walk-<seed>).
func TestTraceMachineWalk(t *testing.T) {
	if os.Getenv(traceEnv) == "" {
		t.Skip("trace walks run under tla/make-traces.sh (" + traceEnv + " unset)")
	}
	for seed := int64(1); seed <= traceSeeds; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			t.Parallel()
			machineWalkTraced(t, seed, traceSteps, true)
		})
	}
}

// startWithoutAttempt is the divergence breaker: Start's plan with its
// attempt write dropped, as a machine whose Start forgot the line would
// make it. The consumer is still cut at the next attempt.
func startWithoutAttempt(plan Plan) Plan {
	plan.Writes = slices.DeleteFunc(slices.Clone(plan.Writes), func(x store.Write) bool { _, ok := x.(store.SetCardAttempt); return ok })
	return plan
}

// divergentWalk is p1 resolved and started on r1; broken drops Start's
// attempt write.
func divergentWalk(t *testing.T, broken bool) *cardTrace {
	w := newWorld(t, producer("p1"))
	tr := newCardTrace(w, true)
	for _, e := range []Event{Resolve{}, Start{Row: "r1", Branch: "b-p1", Base: base}} {
		plan, r := w.plan("p1", e)
		require.Nil(t, r)
		if _, ok := e.(Start); ok && broken {
			plan = startWithoutAttempt(plan)
		}
		w.op(kind(e), plan)
		w.ev = e
		tr.step(name(e)+" p1", plan)
	}
	return tr
}

// TestTraceRefusesDivergence holds the breaker: the honest machine's Start
// cuts p1.w1 at attempt 1 with p1 at attempt 1; the breaker's leaves p1 at
// attempt 0, and its trace is the fixture tla/traces/card-breaker, which
// check-trace.sh must refuse at its last record (the trace job runs it).
func TestTraceRefusesDivergence(t *testing.T) {
	honest, broken := divergentWalk(t, false), divergentWalk(t, true)
	require.Contains(t, honest.recs[len(honest.recs)-1], `"p1" :> [st |-> "working", att |-> 1`)
	require.Contains(t, broken.recs[len(broken.recs)-1], `"p1" :> [st |-> "working", att |-> 0`)
	expect := fmt.Sprintf("forbidden at %d", len(broken.recs))
	if out := os.Getenv(traceEnv); out != "" {
		require.NoError(t, broken.write(filepath.Join(out, "card-breaker"), expect))
	}
	dir := t.TempDir()
	require.NoError(t, broken.write(dir, expect))
	for _, name := range []string{"CardMachineTraceLog.tla", "CardMachineTrace.cfg"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		want, err := os.ReadFile(filepath.Join("..", "tla", "traces", "card-breaker", name))
		require.NoError(t, err, "the fixture: NOVA_TRACE_DIR=tla/traces go test -run TestTraceRefusesDivergence ./card/ writes it")
		require.Equal(t, string(want), string(got), "tla/traces/card-breaker/%s is stale", name)
	}
}

// traceHeads is the walk's head table: the base is head 0.
var traceHeads = []string{base, h1, h2, machineH3}

// cardTrace is a recording in tla/CardMachineTrace.tla's shape.
type cardTrace struct {
	w      *world
	recs   []string
	prev   ModelState
	init   store.Snapshot
	errors []string
	bounds map[string]int // K, MaxAttempt, MaxBounce: the most seen
}

// newCardTrace starts the recording at the world's state; nil when off.
func newCardTrace(w *world, on bool) *cardTrace {
	if !on {
		return nil
	}
	tr := &cardTrace{w: w, init: w.snap(), bounds: map[string]int{}}
	st := tr.state()
	tr.prev = ModelState{Ps: map[string]ModelProd{}, Cs: map[Slot]ModelCons{}, Rs: map[string]ModelRow{}}
	tr.add([]string{`action |-> "Init"`}, st)
	return tr
}

func (tr *cardTrace) state() ModelState {
	st, err := TraceState(tr.w.snap(), "s1", traceHeads, tr.w.width)
	require.NoError(tr.w.t, err)
	for _, p := range st.Ps {
		tr.bounds["K"] = max(tr.bounds["K"], p.Ncut)
		tr.bounds["MaxAttempt"] = max(tr.bounds["MaxAttempt"], p.Att)
		tr.bounds["MaxBounce"] = max(tr.bounds["MaxBounce"], p.Bounces)
	}
	return st
}

// step records the accepted op the walk just landed: its move and the
// state after it.
func (tr *cardTrace) step(label string, plan Plan) {
	if tr == nil {
		return
	}
	args, err := tr.args(label, plan)
	if err != nil {
		tr.errors = append(tr.errors, err.Error())
		return
	}
	tr.add(args, tr.state())
}

// done writes the walk's trace as card-walk-<seed>.
func (tr *cardTrace) done(seed int64) {
	if tr == nil {
		return
	}
	dir := filepath.Join(os.Getenv(traceEnv), fmt.Sprintf("card-walk-%d", seed))
	require.NoError(tr.w.t, tr.write(dir, "matched"))
}

func (tr *cardTrace) add(f []string, st ModelState) {
	f = append([]string{fmt.Sprintf("i |-> %d", len(tr.recs)+1)}, f...)
	f = append(f, "ps |-> "+tlaDiff(tr.prev.Ps, st.Ps, ModelProd.tla, quoted),
		"cs |-> "+tlaDiff(tr.prev.Cs, st.Cs, ModelCons.tla, Slot.tla),
		"rs |-> "+tlaDiff(tr.prev.Rs, st.Rs, ModelRow.tla, quoted),
		"held |-> "+tlaBool(st.Held))
	tr.recs = append(tr.recs, "["+strings.Join(f, ", ")+"]")
	tr.prev = st
}
