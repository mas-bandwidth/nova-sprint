package policy

// Second cold read (2026-11-06, head ed68a22d), written by the cold reader
// (~/rowan-working/children/nsv2-l5-coldread2) and copied here; Rowan
// changed two in round 3, each marked "Round 3": the cache probe answers
// strings inside the ruling's 4 MB a tick and then over it (its own text
// says how), and the broken-catalog probe breaks the built-in through
// defaultBody alone (the built-in's catalog is no longer a package value)
// and also holds the tick to Version -1. Each TestColdread2Defect*
// fails at that head and names the design or Built line it holds the code
// to. The probes of the wall clock read real time, so they are not run
// parallel (a timing contends for the cores) and allow a margin of 100 ms
// (times raceSlowdown) past TickTime; the rest use store.Fake and
// hand-built snapshots only. TestColdread2RaceVerbsAndAnswer is not a defect
// probe: it holds the cache exact across activations, for -race.

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/store"
)

const coldread2Margin = 100 * time.Millisecond * raceSlowdown

// One builtin call is one step and is not interrupted: sorted(range(50M))
// runs to its end (1.32 s on amd-3 without -race, 2.64 s at 100M; linear, so
// no bound but memory) and is refused only after it. Built, fix 5 and 6: "a
// per-tick wall clock TickTime = 250 ms ... enforced by Thread.Cancel";
// design guarantee 4, "A runtime error never stalls a tick".
func TestColdread2DefectSingleStepOverrunsTickTime(t *testing.T) {
	w := fakeWorld(t)
	snap := w.snap()
	body, _ := override("reads", "def reads(card):\n    return min(1, len(sorted(range(50000000))))")
	start := time.Now()
	a, fs := NewEngine().Answer(Versions{Active: pv(2, body)}, snap, FullQuestions(snap, nil))
	took := time.Since(start)
	require.NotEmpty(t, fs)
	assert.Equal(t, 0, a.Version)
	assert.Less(t, took, TickTime+coldread2Margin, "the tick must end near TickTime, took %s", took)
}

// A stored version's top level runs in the tick (Engine.program, the first
// tick that uses it in this process) with the per-call step budget only: no
// wall clock, under the engine's mutex. Many cheap steps (20,000 sorts of
// 2,000, about 120,000 steps) take 0.83 s on amd-3 and the version answers.
// Built, fix 5 and 6: "a tick in which every version spends its budget takes
// at most three budgets".
func TestColdread2DefectCompileOutsideTickTime(t *testing.T) {
	w := fakeWorld(t)
	snap := w.snap()
	body := "_X = len([0 for _i in range(20000) if sorted(range(2000))])\n" + defaultBody
	start := time.Now()
	_, _ = NewEngine().Answer(Versions{Active: pv(2, body)}, snap, FullQuestions(snap, nil)) // ignored: only the time is probed
	took := time.Since(start)
	assert.Less(t, took, TickTime+coldread2Margin, "compiling a version in a tick must be inside TickTime, took %s", took)
}

// An answer's refusal text is built from the whole answer (show quotes a 100
// MB string: 0.56 s) after the call, outside the clock, and storable converts
// the whole text before it cuts (0.69 s on 100 MB): about 1.25 s for one
// verdict, with the clock already stopped. Built, fix 4 ("at most 4,096
// bytes") and fix 5 and 6 (the per-tick wall clock).
func TestColdread2DefectJudgementTextCostUnbounded(t *testing.T) {
	w := fakeWorld(t)
	snap := w.snap()
	body, _ := override("verdict", "def verdict(card, reads):\n    return \"\\x00\" * 100000000")
	start := time.Now()
	_, fs := NewEngine().Answer(Versions{Active: pv(2, body)}, snap, FullQuestions(snap, nil))
	js := Judgements(snap, fs)
	took := time.Since(start)
	require.Len(t, js, 1)
	assert.LessOrEqual(t, len(js[0].Question), maxQuestion)
	assert.Less(t, took, TickTime+coldread2Margin, "a refused answer and its judgement must cost no more than the tick's budget, took %s", took)
}

// The answer cache is bounded by entries (200,000), not bytes, and an order
// answer may be a string of any size: a policy whose every answer is in
// domain and inside every budget pins each tick's answers, so the memory it
// holds grows with the ticks until the 200,000th entry. Built, fix 5 and 6:
// "the wall clock is the only bound on what a policy allocates". Round 3:
// the ruling caps a tick's answers, and so the cache, at 4 MB (over it is a
// refusal), so the probe answers 60,000-byte strings, 3 MB a tick, for ten
// ticks, holds the cache to maxTickBytes after each, and then answers
// 100,000-byte strings, 5 MB, which are refused.
func TestColdread2DefectCacheRetainsUnboundedBytes(t *testing.T) {
	t.Parallel()
	snap := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{},
		Rows: map[string]store.Row{"r": {Name: "r", Kind: "worker", Width: 2, State: "up", Tiers: store.Tiers}}}
	body, _ := override("order", "def order(card):\n    return (\"x\" * 60000 + card.id,)")
	e := NewEngine()
	for tick := range 10 {
		for i := range 50 {
			id := fmt.Sprintf("p%02d", i)
			snap.Cards[id] = store.Card{ID: id, Kind: "producer", Stream: "s", State: "ready", Tier: "pro", Score: tick*1000 + i, Priority: "normal"}
		}
		a, fs := e.Answer(Versions{Active: pv(2, body)}, snap, FullQuestions(snap, nil))
		require.Empty(t, fs)
		require.Equal(t, 2, a.Version)
		assert.LessOrEqual(t, cachedBytes(e), maxTickBytes, "tick %d", tick)
	}
	over, _ := override("order", "def order(card):\n    return (\"x\" * 100000 + card.id,)")
	_, fs := e.Answer(Versions{Active: pv(3, over)}, snap, FullQuestions(snap, nil))
	require.NotEmpty(t, fs)
	assert.Contains(t, fs[0].Msg, fmt.Sprintf("over %d bytes", maxTickBytes))
}

// cachedBytes is what the engine's answer caches hold, by the engine's count.
func cachedBytes(e *Engine) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, p := range e.progs {
		for _, c := range p.answers {
			n += c.weight + entryBytes
		}
	}
	return n
}

// Diff is a longest-common-subsequence table of len(a) x len(b) ints: two
// stored versions of 8,000 lines each allocate 508 MiB (amd-3); at the
// 64 KiB limit (65,536 newlines) it is about 32 GiB. Design, "The verbs":
// policy diff is the text diff of two bodies, every body at most 64 KiB.
func TestColdread2DefectDiffQuadraticMemory(t *testing.T) {
	w := fakeWorld(t)
	w.up()
	pad := strings.Repeat("\n", 8000)
	a := w.set("a.star", defaultBody+pad+"# a\n")
	b := w.set("b.star", defaultBody+pad+"# b\n")
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	_, err := Diff(w.ctx(), w.st, fmt.Sprint(a), fmt.Sprint(b))
	runtime.ReadMemStats(&m1)
	require.NoError(t, err)
	alloc := m1.TotalAlloc - m0.TotalAlloc
	assert.Less(t, alloc, uint64(64<<20), "diff of two %d-byte bodies allocated %d MiB", len(defaultBody)+8004, alloc>>20)
}

// When the built-in fails (the model's Drift may break Default; the design:
// "If even the built-in errors (a bug in this binary), the tick still applies
// reports ..."), a version without catalog() takes builtinCatalog, which
// panics (catalog.go:62), and sync.OnceValue repeats the panic on every call:
// every tick of that version panics. Model: TickAlwaysAnswered with NoAnswer.
// The probe stands in the failing built-in for the embedded one; it is restored.
func TestColdread2DefectBrokenBuiltinCatalogPanics(t *testing.T) {
	saved := defaultBody
	t.Cleanup(func() { defaultBody = saved })
	active, _ := override("catalog", "")
	broken, _ := override("catalog", "def catalog():\n    return 1")
	defaultBody = broken // Round 3: the engine takes the built-in from defaultBody when it is made
	w := fakeWorld(t)
	snap := w.snap()
	var a Answers
	assert.NotPanics(t, func() { a, _ = NewEngine().Answer(Versions{Active: pv(2, active)}, snap, FullQuestions(snap, nil)) }) // ignored: the failures are the fallback's
	assert.Equal(t, -1, a.Version, "the built-in's catalog failing is the last resort: the tick deals nothing")
}

// Not a defect probe: Answer from three goroutines on one engine while Set
// and Activate move the active version between v2 (reads 2) and v3 (reads 3)
// and back; every answer is the answering version's own (the cache is keyed
// by version), under -race.
func TestColdread2RaceVerbsAndAnswer(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	b2, _ := override("reads", "def reads(card):\n    return 2")
	b3, _ := override("reads", "def reads(card):\n    return 3")
	v2, v3 := w.set("two.star", b2), w.set("three.star", b3)
	want := map[int]int{0: 1, 1: 1, v2: 2, v3: 3}
	e := NewEngine()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				vs, err := LoadVersions(w.ctx(), w.st)
				if !assert.NoError(t, err) {
					return
				}
				snap, err := w.st.Snapshot(w.ctx())
				if !assert.NoError(t, err) {
					return
				}
				a, fs := e.Answer(vs, snap, FullQuestions(snap, nil))
				assert.Empty(t, fs)
				assert.Equal(t, want[a.Version], a.Reads["p5"], "answered by v%d", a.Version)
			}
		}()
	}
	for i := range 12 {
		v := []int{v2, v3, 1}[i%3]
		_, err := Activate(w.ctx(), w.st, e, w.caller(), v, "race")
		require.NoError(t, err)
	}
	close(stop)
	wg.Wait()
}
