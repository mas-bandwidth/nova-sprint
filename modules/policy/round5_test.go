package policy

// Round 5's own tests: the clock seam, the evaluator, and the exported
// helpers cold read 3 found untested (DefaultBody, CompareOrder).

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeadlineOnTheClock: a tick's deadline is its clock's. Expired before
// the tick, every version tried fails, one failure each (the model's
// Failing), and nothing answers; the judgements are one per version.
func TestDeadlineOnTheClock(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	c := newFakeClock()
	c.expire()
	snap := w.snap()
	a, fs := newLocal(c).Answer(Versions{Active: pv(3, defaultBody), Prev: pv(2, defaultBody)}, snap, w.ro, FullQuestions(snap, nil))
	assert.Equal(t, -1, a.Version)
	require.Len(t, fs, 3)
	for i, label := range []string{"v3", "v2", "default"} {
		assert.Equal(t, label, fs[i].Label)
		assert.Contains(t, fs[i].Msg, "over the tick's time budget")
	}
	assert.Len(t, Judgements(snap, w.ro, fs), 3)
}

// TestDefaultBodyIsTheBuiltIn: DefaultBody is the embedded default, which
// answers the seeded world as version 0.
func TestDefaultBodyIsTheBuiltIn(t *testing.T) {
	t.Parallel()
	assert.Equal(t, defaultBody, DefaultBody())
	assert.True(t, strings.HasPrefix(DefaultBody(), "# nova-sprint default policy."))
	for _, key := range []string{`"ready_nag": 1,`, `"ready_take": 180,`, `"chain_limit": 3,`} { // named, not the older-version fallback (checkTimeouts)
		assert.Contains(t, DefaultBody(), key)
	}
	w := fakeWorld(t)
	a, fs := testEngine().Answer(Versions{}, w.snap(), w.ro, FullQuestions(w.snap(), nil))
	require.Empty(t, fs)
	assert.Equal(t, 0, a.Version)
}

// TestCompareOrder: element by element, an int before a string, a shorter
// prefix first.
func TestCompareOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		a, b []any
		want int
	}{
		{[]any{int64(1)}, []any{int64(2)}, -1},
		{[]any{int64(2)}, []any{int64(1)}, 1},
		{[]any{int64(9)}, []any{"a"}, -1},
		{[]any{"a"}, []any{int64(9)}, 1},
		{[]any{"a"}, []any{"b"}, -1},
		{[]any{int64(1)}, []any{int64(1), "x"}, -1},
		{[]any{int64(1), "x"}, []any{int64(1), "x"}, 0},
	} {
		assert.Equal(t, tc.want, CompareOrder(tc.a, tc.b), "%v %v", tc.a, tc.b)
	}
}

// TestFunctionalEvaluator: a process engine (its child is this test binary,
// TestMain) answers as the in-process engine does, check included; a version whose one builtin step outlasts the deadline is
// abandoned in the child, which says so and is killed, and the next tick
// starts a new child. Functional: it starts processes, and the child's
// deadline is the wall clock's.
func TestFunctionalEvaluator(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	snap := w.snap()
	e := NewEngine(newFakeClock())
	want, wfs := testEngine().Answer(Versions{}, snap, w.ro, FullQuestions(snap, nil))
	got, gfs := e.Answer(Versions{}, snap, w.ro, FullQuestions(snap, nil))
	require.Empty(t, wfs)
	require.Empty(t, gfs)
	assert.Equal(t, want, got, "the child answers as this process does")
	rep, err := Check(w.ctx(), w.st, w.ro, e, "default")
	require.NoError(t, err)
	assert.Empty(t, rep.Refusals)
	hung, _ := override("stitch", "def stitch(stream):\n    x = []\n    for _i in range(30000):\n        x = [[[[[[[[[[x]]]]]]]]]]\n    return len(str(x)) > 0")
	a, fs := e.Answer(Versions{Active: pv(2, hung)}, snap, w.ro, FullQuestions(snap, nil))
	assert.Equal(t, 0, a.Version, "the built-in answers behind the hung version: %v", fs)
	require.NotEmpty(t, fs)
	assert.Contains(t, fs[0].Msg, "over the tick's time budget")
	e.proc.mu.Lock()
	gone := e.proc.cmd == nil
	e.proc.mu.Unlock()
	assert.True(t, gone, "the child with an abandoned run is killed")
	a, fs = e.Answer(Versions{}, snap, w.ro, FullQuestions(snap, nil))
	require.Empty(t, fs)
	assert.Equal(t, 0, a.Version, "the next tick starts a new child")
}

// steppingClock moves 100 ms on every Now and records each After's wait; no
// After fires.
type steppingClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
}

func (c *steppingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(100 * time.Millisecond)
	return c.now
}

func (c *steppingClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, d)
	return nil
}

// TestFallbackCountsFromTheFirstByte (rowan2's ruling 6): the deadline and
// the early fallback are counted from the tick's first byte, before its
// inputs are built, so time spent on the inputs shortens both waits.
func TestFallbackCountsFromTheFirstByte(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	c := &steppingClock{now: t0}
	_, fs := newLocal(c).Answer(Versions{}, w.snap(), w.ro, FullQuestions(w.snap(), nil))
	require.Empty(t, fs)
	require.Len(t, c.waits, 2, "the deadline, then the fallback")
	assert.Equal(t, TickTime-100*time.Millisecond, c.waits[0], "the deadline: one step of the clock after the first byte")
	assert.Equal(t, fallbackAfter-200*time.Millisecond, c.waits[1], "the fallback: two steps after the first byte, the inputs between")
}

// TestFunctionalEvaluatorMemoryCap: a version that allocates past the
// evaluator's memory cap (Glenn's ruling, 2026-11-07, default 512 MiB) ends
// the child. As for any child that dies, every version it ran fails, naming
// the cap (the tick applies the moves that need no answer and Judgements
// opens one per failing version); the next tick starts a new child.
// Functional: it starts processes.
func TestFunctionalEvaluatorMemoryCap(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	snap := w.snap()
	e := NewEngine(newFakeClock())
	// The production path at a 64 MiB cap: three 32 MiB strings cross it in
	// milliseconds. At 512 MiB, building the strings under -race on a loaded
	// host (about 1 s per 256 MiB) runs out the tick's 1.5 s budget first.
	e.proc.memory = 64 << 20
	big, _ := override("stitch", "def stitch(stream):\n    x = []\n    for _i in range(3):\n        x.append(\"a\" * (1 << 25))\n    return len(x) > 0")
	a, fs := e.Answer(Versions{Active: pv(2, big)}, snap, w.ro, FullQuestions(snap, nil))
	assert.Equal(t, -1, a.Version, "a dead child answers nothing: %v", fs)
	require.NotEmpty(t, fs)
	assert.Equal(t, "v2", fs[0].Label)
	assert.Contains(t, fs[0].Msg, "over the evaluator's memory cap of 64 MiB")
	a, fs = e.Answer(Versions{}, snap, w.ro, FullQuestions(snap, nil))
	require.Empty(t, fs)
	assert.Equal(t, 0, a.Version, "the next tick starts a new child")
}

// waitingClock is a fake clock that signals its first After: the evaluator
// is then waiting on its child.
type waitingClock struct {
	*fakeClock
	waiting chan struct{}
	once    sync.Once
}

func (c *waitingClock) After(d time.Duration) <-chan time.Time {
	c.once.Do(func() { close(c.waiting) })
	return c.fakeClock.After(d)
}

// TestFunctionalEvaluatorReleaseMidAnswer: the collector's cleanup of an engine
// whose child is still answering (release) and the answer's own stop must not
// race; run with -race. The cleanup waits for the answer, then stops the child.
func TestFunctionalEvaluatorReleaseMidAnswer(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	snap := w.snap()
	hung, _ := override("stitch", "def stitch(stream):\n    x = []\n    for _i in range(30000):\n        x = [[[[[[[[[[x]]]]]]]]]]\n    return len(str(x)) > 0")
	clock := &waitingClock{fakeClock: newFakeClock(), waiting: make(chan struct{})}
	e := NewEngine(clock)
	done := make(chan []Failure, 1)
	go func() {
		_, fs := e.Answer(Versions{Active: pv(2, hung)}, snap, w.ro, FullQuestions(snap, nil))
		done <- fs
	}()
	<-clock.waiting
	released := make(chan struct{})
	go func() {
		e.proc.release()
		close(released)
	}()
	clock.expire()
	require.NotEmpty(t, <-done)
	<-released
	e.proc.mu.Lock()
	gone := e.proc.cmd == nil
	e.proc.mu.Unlock()
	assert.True(t, gone)
}

// TestChainLimitDefaultsForAnOlderVersion: a limits() without chain_limit
// (a version stored before it) answers 3, the default's value.
func TestChainLimitDefaultsForAnOlderVersion(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	body := strings.Replace(defaultBody, `"chain_limit": 3,`, "", 1)
	require.NotEqual(t, defaultBody, body)
	a, fs := testEngine().Answer(Versions{Active: pv(2, body)}, w.snap(), w.ro, FullQuestions(w.snap(), nil))
	require.Empty(t, fs)
	assert.Equal(t, 2, a.Version)
	assert.Equal(t, 3, a.Limits.ChainLimit)
}
