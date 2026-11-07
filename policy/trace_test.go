package policy

// Trace validation of the policy engine (tla/README.md, "Trace
// validation"). The policy walk drives the verbs and the tick on the Fake
// (up, then set, activate, a snapshot that drifts, and ticks that write
// their judgements and move one card as layer 3 would) and records, after
// every step, the step and the state projected by TraceState;
// tla/PolicyTrace.tla checks the recording is a behaviour of tla/Policy.tla.
// The walk writes its traces only when NOVA_TRACE_DIR names a directory
// (tla/make-traces.sh sets it); TestTraceRefusesDivergence always runs.

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/store"
)

const traceEnv = "NOVA_TRACE_DIR"

// The walk's fixed seed set: 10 seeds, 300 steps each, versions up to 4.
const traceSeeds, traceSteps, traceMax = 10, 300, 4

// TestTraceWalk writes the policy walk's traces (policy-walk-<seed>).
func TestTraceWalk(t *testing.T) {
	out := os.Getenv(traceEnv)
	if out == "" {
		t.Skip("trace walks run under tla/make-traces.sh (" + traceEnv + " unset)")
	}
	for seed := uint64(1); seed <= traceSeeds; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			t.Parallel()
			tr := newPolicyTrace(t)
			rng := rand.New(rand.NewPCG(seed, 0))
			for range traceSteps {
				tr.walkStep(rng)
			}
			require.NoError(t, tr.write(filepath.Join(out, fmt.Sprintf("policy-walk-%d", seed)), "matched"))
		})
	}
}

// breaking is the default body with a width that fails (a division by
// zero) while row x<k> has width 2: version k's, and the built-in's at 0.
func breaking(k int) string {
	body, _ := override("width", fmt.Sprintf("def width(row):\n    return {\"work\": row.width // (0 if row.name == \"x%d\" and row.width == 2 else 1), \"ahead\": row.width}", k))
	return body
}

// policyTrace is the walk's world and its recording.
type policyTrace struct {
	w    *world
	e    *Engine
	tick *Answers
	prev ModelState
	recs []string
}

// newPolicyTrace is the walk's world, up, recorded as Init: stream s1; rows
// x0..x<traceMax>, up, in the roster as pro workers of width 1; producer p1 (s1, pro, ready), the card.
func newPolicyTrace(t testing.TB) *policyTrace {
	w := &world{t: t, st: store.NewFake(), ro: handRoster()}
	writes := []store.Write{store.PutStream{Name: "s1", Repo: "git@example.com:r.git", Base: "main", Pos: 1},
		store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Score: 1, Brief: "do p1", Prose: "do p1"}}
	for i := range traceMax + 1 {
		writes = append(writes, store.PutRow{Name: fmt.Sprintf("x%d", i)})
		w.configure(rc{fmt.Sprintf("x%d", i), "worker", 1, []string{"pro"}, ""})
	}
	w.apply(writes...)
	writes = []store.Write{store.SetCardState{Card: "p1", State: "ready"}}
	for i := range traceMax + 1 {
		writes = append(writes, store.SetRowState{Row: fmt.Sprintf("x%d", i), State: "up"})
	}
	w.apply(writes...)
	w.up()
	tr := &policyTrace{w: w, e: testEngine()}
	tr.e.builtin = breaking(0)
	tr.record("Init")
	return tr
}

// walkStep is one step drawn from rng: set, activate, drift or tick.
func (tr *policyTrace) walkStep(rng *rand.Rand) {
	switch n := rng.IntN(20); {
	case n < 3:
		tr.set()
	case n < 6:
		tr.activate(1 + rng.IntN(len(tr.prev.Stored)))
	case n < 12:
		tr.drift(rng.IntN(traceMax + 1))
	default:
		tr.doTick(rng.IntN(2) == 0, false)
	}
}

// refused requires err to be a verb's refusal: the step changed nothing.
func (tr *policyTrace) refused(err error) {
	var r *Refusal
	require.True(tr.w.t, errors.As(err, &r), "%v", err)
}

func (tr *policyTrace) set() {
	k := len(tr.prev.Stored) + 1
	if k > traceMax {
		return
	}
	_, err := Set(tr.w.ctx(), tr.w.st, tr.w.ro, tr.e, tr.w.caller(), tr.w.file(fmt.Sprintf("v%d.star", k), breaking(k)), "walk")
	if err != nil {
		tr.refused(err)
		return
	}
	tr.record("Set", "v", strconv.Itoa(k))
}

func (tr *policyTrace) activate(v int) {
	if _, err := Activate(tr.w.ctx(), tr.w.st, tr.w.ro, tr.e, tr.w.caller(), v, "walk"); err != nil {
		tr.refused(err)
		return
	}
	tr.record("Activate", "v", strconv.Itoa(v))
}

// drift toggles row x<i>'s width in the roster between 1 and 2 (nova-config
// changing under the sprint, an outside event of the model); a drift that
// changes no version's outcome changes no variable of the model, a stutter,
// not recorded.
func (tr *policyTrace) drift(i int) {
	x := tr.w.ro.Workers[fmt.Sprintf("x%d", i)]
	x.Width = 3 - x.Width
	tr.w.ro.Workers[x.Name] = x
	tr.record("Drift")
}

// doTick answers a tick and applies, in one op, the judgements its failures
// open and, when advance, the card's next state; a deal needs an answer.
// The breaker drops the previous active from the versions (one line).
func (tr *policyTrace) doTick(advance, broken bool) {
	snap := tr.w.snap()
	vs, err := LoadVersions(tr.w.ctx(), tr.w.st, snap)
	require.NoError(tr.w.t, err)
	if broken {
		vs.Prev = nil
	}
	a, fs := tr.e.Answer(vs, snap, tr.w.ro, FullQuestions(snap, nil))
	var writes []store.Write
	for _, j := range Judgements(snap, tr.w.ro, fs) {
		writes = append(writes, j)
	}
	next := map[string]string{"ready": "working", "working": "done", "done": "ready"}[snap.Cards["p1"].State]
	if advance && (next != "working" || a.Version != -1) {
		writes = append(writes, store.SetCardState{Card: "p1", State: next})
	}
	if len(writes) > 0 {
		tr.w.apply(writes...)
	}
	tr.tick = &a
	tr.record("Tick")
}

// record appends the step and the state after it; a Drift that leaves the
// projected state as it was is a stutter and is not recorded.
func (tr *policyTrace) record(action string, kv ...string) {
	m, err := TraceState(tr.w.ctx(), tr.w.st, tr.w.ro, tr.e, tr.tick, "p1", traceMax)
	require.NoError(tr.w.t, err)
	if action == "Drift" && reflect.DeepEqual(m, tr.prev) {
		return
	}
	f := []string{fmt.Sprintf("i |-> %d", len(tr.recs)+1), fmt.Sprintf("action |-> %q", action)}
	for i := 0; i < len(kv); i += 2 {
		f = append(f, kv[i]+" |-> "+kv[i+1])
	}
	judged := make([]string, len(m.Judgements))
	for v, n := range m.Judgements {
		judged[v] = fmt.Sprintf("%d :> %d", v, n)
	}
	f = append(f, "stored |-> "+tlaInts(m.Stored), fmt.Sprintf("active |-> %d, prev |-> %d", m.Active, m.Prev),
		"breaks |-> "+tlaInts(m.Breaks), "everActive |-> "+tlaInts(m.EverActive), fmt.Sprintf("answeredBy |-> %d", m.AnsweredBy),
		"judgements |-> ("+strings.Join(judged, " @@ ")+")", fmt.Sprintf("card |-> %q", m.Card))
	tr.recs = append(tr.recs, "["+strings.Join(f, ", ")+"]")
	tr.prev = m
}

func tlaInts(ns []int) string {
	s := make([]string, len(ns))
	for i, n := range slices.Sorted(slices.Values(ns)) {
		s[i] = strconv.Itoa(n)
	}
	return "{" + strings.Join(s, ", ") + "}"
}

// write writes PolicyTraceLog.tla and PolicyTrace.cfg into dir.
func (tr *policyTrace) write(dir, expect string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	log := "---- MODULE PolicyTraceLog ----\n\\* Generated by policy/trace_test.go; do not edit.\nEXTENDS Integers, TLC\n\n" +
		"TraceLog == <<\n  " + strings.Join(tr.recs, ",\n  ") + "\n>>\n====\n"
	if err := os.WriteFile(filepath.Join(dir, "PolicyTraceLog.tla"), []byte(log), 0o644); err != nil {
		return err
	}
	cfg := fmt.Sprintf("\\* Generated by policy/trace_test.go; do not edit.\n\\* expect: %s\nCONSTANT MaxVersion = %d\n", expect, traceMax) +
		"SPECIFICATION TraceSpec\nPROPERTY TraceMatched\nCHECK_DEADLOCK FALSE\n"
	return os.WriteFile(filepath.Join(dir, "PolicyTrace.cfg"), []byte(cfg), 0o644)
}

// breakerWalk is up, v2 set and activated, row x2 at width 2 (v2 breaks),
// then a tick: the honest engine falls back to v1, the previous active; the
// breaker, with the previous active dropped, to the built-in.
func breakerWalk(t *testing.T, broken bool) *policyTrace {
	tr := newPolicyTrace(t)
	tr.set()
	tr.activate(2)
	tr.drift(2)
	tr.doTick(false, broken)
	return tr
}

// TestTraceRefusesDivergence holds the breaker: its trace is the fixture
// tla/traces/policy-breaker, which check-trace.sh must refuse at its last
// record, the tick (the trace job runs it).
func TestTraceRefusesDivergence(t *testing.T) {
	t.Parallel()
	honest, broken := breakerWalk(t, false), breakerWalk(t, true)
	require.Len(t, honest.recs, 5)
	require.Contains(t, honest.recs[4], `action |-> "Tick"`)
	require.Contains(t, honest.recs[4], "answeredBy |-> 1,")
	require.Contains(t, broken.recs[4], "answeredBy |-> 0,")
	expect := fmt.Sprintf("forbidden at %d", len(broken.recs))
	if out := os.Getenv(traceEnv); out != "" {
		require.NoError(t, broken.write(filepath.Join(out, "policy-breaker"), expect))
	}
	dir := t.TempDir()
	require.NoError(t, broken.write(dir, expect))
	for _, name := range []string{"PolicyTraceLog.tla", "PolicyTrace.cfg"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		want, err := os.ReadFile(filepath.Join("..", "tla", "traces", "policy-breaker", name))
		require.NoError(t, err, "the fixture: NOVA_TRACE_DIR=tla/traces go test -run TestTraceRefusesDivergence ./policy/ writes it")
		require.Equal(t, string(want), string(got), "tla/traces/policy-breaker/%s is stale", name)
	}
}
