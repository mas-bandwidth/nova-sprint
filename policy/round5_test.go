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
// TestMain) answers as the in-process engine does, check and the catalog
// included; a version whose one builtin step outlasts the deadline is
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
	c, err := ReadCatalog(w.ctx(), w.st, e, 1)
	require.NoError(t, err)
	assert.NotEmpty(t, c.Harnesses)
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
