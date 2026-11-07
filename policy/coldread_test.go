package policy

// Cold-read probes (2026-11-06), written by the cold reader of e33eea61
// (~/rowan-working/children/nsv2-l5-coldread), copied here unchanged. Each
// TestColdreadDefect* fails at the head it was written against and names the
// design line it holds the code to. No clock, no socket: store.Fake and
// hand-built snapshots only.

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// A policy file named "default" (policy check ./default, or policy set) is
// compiled with label "default" and version 0, which engine.call takes for
// the built-in and caches under the built-in's keys: the built-in fallback
// then answers with the file's answers. Design: "the whole tick is answered
// by ... the built-in default"; guarantee 6, "the cache is exact".
func TestColdreadDefectFileNamedDefaultPoisonsBuiltinCache(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	snap := w.snap()
	e := NewEngine()
	body, _ := override("reads", "def reads(card):\n    return 5")
	_, fs := e.runFile("default", body, snap)
	require.Empty(t, fs)
	a, fs := e.Answer(Versions{}, snap, FullQuestions(snap, nil))
	require.Empty(t, fs)
	require.Equal(t, 0, a.Version)
	assert.Equal(t, 1, a.Reads["p5"], "the built-in's reads is 1; a checked file's answer must not answer for it")
}

// The built-in fails the validator's joint check on a legal snapshot: a heavy
// card at 2 bounces wants frontier, the only frontier row serving heavy has
// width 0, a heavy row has width 2. The default names only the frontier row,
// the joint check refuses it, and even the built-in fails (Version -1).
// Design: "The default policy ... answers every snapshot the suite builds";
// 5.6 "no default leaves the sprint unable to deal".
func TestColdreadDefectDefaultRefusedByJointCheck(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.apply(store.PutRow{Name: "r5", Kind: "kin", Width: 0, Tiers: []string{"heavy", "frontier"}, Model: "m-frontier"})
	w.apply(store.SetRowState{Row: "r5", State: "up"})
	snap := w.snap()
	a, fs := NewEngine().Answer(Versions{}, snap, FullQuestions(snap, nil))
	assert.Empty(t, fs, "the built-in must answer")
	assert.Equal(t, 0, a.Version)
}

// A read reported "fault" is a done read (card/move.go report); a producer in
// review whose only done read faulted is asked verdict (FullQuestions asks
// verdict for any done read), and no answer is in domain: "yes" needs an ok
// read, "fix" a fix read. Every version fails, the built-in included.
// Design: the default "answers every snapshot shape"; guarantee 4.
func TestColdreadDefectVerdictOnFaultedReadUnanswerable(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.apply(store.PutCard{ID: "p5-r4", Kind: "read", Of: "p5", Stream: "s1", Tier: "pro", Holder: "r4"})
	w.apply(store.SetCardState{Card: "p5-r4", State: "done"}, store.SetCardResult{Card: "p5-r4", Result: "fault"})
	snap := w.snap()
	a, fs := NewEngine().Answer(Versions{}, snap, FullQuestions(snap, nil))
	assert.Empty(t, fs, "the built-in must answer")
	assert.Equal(t, 0, a.Version)
}

// A failure's text goes into the policy judgement's question unescaped: a
// NUL from fail() or from a refused rows name makes Judgements build a
// PutJudgement the store refuses with ErrInvalid, so the op carrying the
// tick's judgement can never enter the store. Design: guarantee 4, "A
// runtime error never stalls a tick"; FuzzAnswers, "the store never
// returning ErrInvalid".
func TestColdreadDefectJudgementTextNotStorable(t *testing.T) {
	t.Parallel()
	failNUL, _ := override("limits", "def limits():\n    fail(\"bad\" + chr(0))")
	rowsNUL, _ := override("rows", "def rows(card, kind, candidates):\n    return [\"r\" + chr(0)]")
	for name, body := range map[string]string{"fail": failNUL, "rows": rowsNUL} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := fakeWorld(t)
			snap := w.snap()
			_, fs := NewEngine().Answer(Versions{Active: pv(2, body)}, snap, FullQuestions(snap, nil))
			require.NotEmpty(t, fs)
			js := Judgements(snap, fs)
			require.NotEmpty(t, js)
			id, at := w.next()
			_, err := w.st.Apply(w.ctx(), store.Op{ID: id, Epoch: snap.Epoch, Actor: "test", Kind: "tick", At: at, Writes: []store.Write{js[0]}})
			assert.NoError(t, err, "the judgement policy builds must be storable")
		})
	}
}

// Work inside one builtin call is one step: a 512 MiB string, or sorting
// millions of ints, passes the step budget and the answer is used. Design:
// "bounded steps"; a huge allocation or unbounded work must fail the version
// (open a judgement), never stall the tick or exhaust the process.
func TestColdreadDefectBuiltinWorkUnbounded(t *testing.T) {
	t.Parallel()
	alloc, _ := override("reads", "def reads(card):\n    return min(1, len(\"x\" * (1 << 29)))")
	sorting, _ := override("reads", "def reads(card):\n    return min(1, len(sorted(range(5000000), reverse = True)))")
	for name, body := range map[string]string{"alloc 512 MiB": alloc, "sort 5M": sorting} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := fakeWorld(t)
			snap := w.snap()
			a, fs := NewEngine().Answer(Versions{Active: pv(2, body)}, snap, FullQuestions(snap, nil))
			assert.NotEmpty(t, fs, "the version must fail")
			assert.NotEqual(t, 2, a.Version, "a version doing unbounded work in one step must not answer")
		})
	}
}

// The step budget is per call (Built), so a tick's policy time grows with
// the number of subjects: 200 producers at 30,000 loop iterations each (each
// call under the budget) are over 6,000,000 steps in one tick and no
// failure. At 5,000 producers and a call just under 1,000,000 steps, a tick
// is 5,000,000,000 steps. Design, "The embedding": "on one thread per tick, with
// thread.SetMaxExecutionSteps(budget)"; guarantee 4.
func TestColdreadDefectTickStepsUnbounded(t *testing.T) {
	t.Parallel()
	snap := store.Snapshot{Epoch: 1, Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{},
		Rows: map[string]store.Row{"r": {Name: "r", Kind: "worker", Width: 2, State: "up", Tiers: store.Tiers}}}
	for i := range 200 {
		id := fmt.Sprintf("p%03d", i)
		snap.Cards[id] = store.Card{ID: id, Kind: "producer", Stream: "s", State: "ready", Tier: "pro", Score: i, Priority: "normal"}
	}
	body, _ := override("order", "def order(card):\n    for _i in range(30000):\n        pass\n    return (card.score,)")
	a, fs := NewEngine().Answer(Versions{Active: pv(2, body)}, snap, FullQuestions(snap, nil))
	require.Len(t, fs, 1, "a tick over CallSteps in total must fail the version: %v", fs)
	assert.Contains(t, fs[0].Msg, "step budget")
	assert.NotEqual(t, 2, a.Version)
}

// ReadCatalog of an unknown version returns the store's ErrNotFound, not a
// refusal naming the fix (coordinator's brief for ReadCatalog: "refuse an
// unknown version naming the fix"; the verbs' own form, version()).
func TestColdreadDefectReadCatalogUnknownVersion(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	_, err := ReadCatalog(w.ctx(), w.st, 99)
	var r *Refusal
	require.ErrorAs(t, err, &r, "got %v", err)
	assert.Contains(t, r.Error(), "nova-sprint policy show --list")
}

// Not a defect probe: Answer from two goroutines on one engine, for -race.
func TestColdreadConcurrentAnswer(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	snap := w.snap()
	q := FullQuestions(snap, nil)
	e := NewEngine()
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 30 {
				a, fs := e.Answer(Versions{Active: pv(1+g%2, defaultBody)}, snap, q)
				assert.Empty(t, fs)
				assert.Equal(t, 1+g%2, a.Version)
			}
		}()
	}
	wg.Wait()
}
