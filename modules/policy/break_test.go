package policy

// Breaker probes (2026-11-06) against v2-l5-policy ed68a22d, by the Opus
// breaker (/Volumes/nova/ai/rowan/working/handoff/l4l5-break/pol), copied
// here unchanged. Each TestBreak*
// names the sequence and the guarantee it holds the code to. Timing probes
// read real time and are functional: they skip under -race and -short.

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// breakTiming skips a timing probe unless NOVA_LOAD is set, and under
// -race and -short (round 5: the timing probes read the wall clock, so they
// run only when asked; every other test is on a fake clock).
func breakTiming(t *testing.T) {
	if os.Getenv("NOVA_LOAD") == "" || raceSlowdown > 1 || testing.Short() {
		t.Skip("a timing probe: set NOVA_LOAD=1, without -race and -short")
	}
}

// timedEngine is a timing probe's engine, on the wall clock.
func timedEngine() *Engine { return NewEngine(RealClock{}) }

// bound is the most a tick may take by the Built section: "a tick in which
// every version spends its budget takes at most three budgets", plus slack.
const bound = 3*TickTime + 250*time.Millisecond

// loadSnap is producers ready producers over rows workers serving every
// tier at width 2, and its roster.
func loadSnap(producers, rows int) (store.Snapshot, roster.Roster) {
	ro := handRoster()
	snap := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{}, Cards: map[string]store.Card{}, Rows: map[string]store.Row{}}
	for i := range 10 {
		snap.Streams[fmt.Sprintf("s%d", i)] = store.Stream{Name: fmt.Sprintf("s%d", i)}
	}
	for i := range rows {
		name := fmt.Sprintf("r%03d", i)
		snap.Rows[name] = store.Row{Name: name, State: "up", Session: "s-" + name, Role: "worker"}
		ro.Workers[name] = roster.Worker{Name: name, Width: 2, Tiers: store.Tiers, Harness: roster.WorkerHarness}
	}
	for i := range producers {
		id := fmt.Sprintf("p%04d", i)
		snap.Cards[id] = store.Card{ID: id, Kind: "producer", Stream: fmt.Sprintf("s%d", i%10), State: "ready", Tier: store.Tiers[i%4], Score: i, Priority: "normal"}
	}
	return snap, ro
}

// The file's top level runs on newThread: CallSteps but no wall clock
// (compile, engine.go). A stored version whose top level does heavy work in
// few steps is compiled inside Answer, under Engine.mu, with no time bound.
// Sequence: Answer with an active version whose top level sorts 30M ints.
// Broken: "the engine must answer in bounded time"; Built, "a tick in which
// every version spends its budget takes at most three budgets".
func TestFunctionalBreakTopLevelHasNoTimeBudget(t *testing.T) {
	t.Parallel()
	breakTiming(t)
	body := defaultBody + "\n_T = len(sorted(range(30000000), reverse = True))\n"
	w := fakeWorld(t)
	snap := w.snap()
	start := time.Now()
	a, fs := timedEngine().Answer(Versions{Active: pv(2, body)}, snap, w.ro, FullQuestions(snap, nil))
	took := time.Since(start)
	t.Logf("top-level sort of 30M: tick took %s, answered by %d, failures %v", took, a.Version, fs)
	assert.Less(t, took, bound, "the tick must be bounded by its budgets")
}

// One builtin call is one step; the timer cancels only between steps, so a
// single call runs to its end (Built admits this for 512 MiB). The bound is
// then set by the largest builtin, not by TickTime: range takes int32, so
// sorted(range(2**31-1)) is reachable. Sequence: limits() sorts 30M ints.
// Broken: the tick bound of three budgets.
func TestFunctionalBreakOneBuiltinOutlastsTickTime(t *testing.T) {
	t.Parallel()
	breakTiming(t)
	body, _ := override("limits", `def limits():
    _n = len(sorted(range(30000000), reverse = True))
    return {"bounces": 6, "faults": 3, "same_finding": 2, "reports_per_tick": 1000, "route_rest_window": 10, "route_rest_after": 3, `+steering+`}`)
	w := fakeWorld(t)
	snap := w.snap()
	start := time.Now()
	a, fs := timedEngine().Answer(Versions{Active: pv(2, body)}, snap, w.ro, FullQuestions(snap, nil))
	took := time.Since(start)
	t.Logf("one sorted(range(30M)) in limits: tick took %s, answered by %d, failures %v", took, a.Version, fs)
	assert.Less(t, took, bound)
}

// The validator runs in Go outside every budget. A refused int is printed in
// decimal by intIn (i.String()), which is superlinear in the int's size.
// Sequence: width answers work = a ~16 Mbit int built in 15 squarings.
// Broken: the tick bound; the refusal is reached only after the print.
func TestFunctionalBreakHugeIntRefusalPrintedOutsideBudget(t *testing.T) {
	t.Parallel()
	breakTiming(t)
	body, _ := override("width", `def width(row):
    x = 1 << 500
    for _i in range(15):
        x = x * x
    return {"work": x, "ahead": 0}`)
	w := fakeWorld(t)
	snap := w.snap()
	start := time.Now()
	a, fs := timedEngine().Answer(Versions{Active: pv(2, body)}, snap, w.ro, FullQuestions(snap, nil))
	took := time.Since(start)
	require.NotEmpty(t, fs)
	t.Logf("huge int in width: tick took %s, answered by %d, failure text %d bytes", took, a.Version, len(fs[0].Msg))
	assert.Less(t, took, bound)
}

// checkCatalog's models and names use slices.Contains over the prefix: the
// validator is quadratic in the list, in Go, outside the Starlark budgets,
// and catalog() is validated every tick, cached or not. Sequence: catalog()
// lists 40,000 distinct models under one provider (about 600k steps, under
// CallSteps). Broken: the tick bound.
func TestFunctionalBreakCatalogValidationQuadratic(t *testing.T) {
	t.Parallel()
	breakTiming(t)
	body, _ := override("catalog", `def catalog():
    return {"models": {"p": [{"name": "m" + str(i), "tier": "flash"} for i in range(40000)]}, "harnesses": ["claude"]}`)
	w := fakeWorld(t)
	snap := w.snap()
	e := timedEngine()
	vs := Versions{Active: pv(2, body)}
	start := time.Now()
	a, fs := e.Answer(vs, snap, w.ro, FullQuestions(snap, nil))
	cold := time.Since(start)
	start = time.Now()
	_, _ = e.Answer(vs, snap, w.ro, FullQuestions(snap, nil))
	warm := time.Since(start)
	t.Logf("catalog of 40k models: cold %s, warm (cached answer, revalidated) %s, answered by %d, failures %v", cold, warm, a.Version, fs)
	assert.Less(t, cold, bound)
	assert.Less(t, warm, bound)
}

// Answers are cached per call, before the tick's validation, and a cache hit
// spends no steps. A version over TickSteps on a snapshot fails tick 1 (the
// fallback answers), and the same version on the same snapshot answers tick
// 2 from the cache it warmed. Sequence: 80 producers, order() spends ~50k
// iterations each (each call under CallSteps, 80 of them over TickSteps);
// Answer six times on one snapshot. Broken: guarantee 6, "one version and one
// input give one answer ... check on a snapshot predicts the tick on that
// snapshot"; answered_by differs for one snapshot.
func TestBreakBudgetOutcomeDependsOnCacheHistory(t *testing.T) {
	t.Parallel()
	snap, ro := loadSnap(80, 2)
	body, _ := override("order", "def order(card):\n    for _i in range(50000):\n        pass\n    return (card.score,)")
	e := testEngine()
	vs := Versions{Active: pv(2, body)}
	q := FullQuestions(snap, nil)
	var by []int
	for range 6 {
		a, fs := e.Answer(vs, snap, ro, q)
		by = append(by, a.Version)
		t.Logf("tick: answered by %d, failures %v", a.Version, fs)
	}
	t.Logf("answered_by over six ticks of one snapshot: %v", by)
	for i := range by {
		assert.Equal(t, by[0], by[i], "one snapshot, one version: one outcome (tick %d)", i+1)
	}
}

// The built-in's rows walks every candidate per card, so its steps grow with
// producers x rows; TickSteps is fixed. At 5,000 ready producers the
// built-in alone spends TickSteps at some row count, every version then
// fails and Answers.Version is -1: the tick deals nothing. Sequence: the
// built-in, cold, at 5,000 producers over 100 rows that all serve every
// tier. Broken: 5.6 "no default leaves the sprint unable to deal"; the
// default "answers every snapshot".
func TestFunctionalBreakDefaultSpendsTickBudgetAt100Rows(t *testing.T) {
	t.Parallel()
	breakTiming(t)
	snap, ro := loadSnap(5000, 100)
	start := time.Now()
	e := timedEngine()
	a, fs := e.Answer(Versions{}, snap, ro, FullQuestions(snap, nil))
	t.Logf("built-in at 5000 producers x 100 rows: %s, answered by %d, failures %v", time.Since(start), a.Version, fs)
	a2, fs2 := e.Answer(Versions{}, snap, ro, FullQuestions(snap, nil))
	t.Logf("same snapshot, next tick: answered by %d, failures %v", a2.Version, fs2)
	assert.Equal(t, 0, a.Version, "the built-in must answer")
}
