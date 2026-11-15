package policy

// The evaluator charges a version only for the child's own run
// (2026-11-15): the parent's deadline-plus-evalGrace timer starts when the
// child has begun the request, so a slow transfer (a large snapshot decoded
// on a loaded host) never fails a fast policy, while a child that hangs, a
// child that never begins and a runaway script are still stopped. Unit
// tests: the child is a goroutine on in-memory pipes (evaluator.spawn), and
// both clocks are timerClocks the test moves; no process, socket or wall
// clock.

import (
	"encoding/gob"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// timerClock is a clock the test moves: After fires once advance has moved
// it past the wait, counted from when After was called; every wait is sent
// on waits as it is made.
type timerClock struct {
	mu      sync.Mutex
	elapsed time.Duration
	timers  []clockTimer
	waits   chan time.Duration
}

type clockTimer struct {
	at time.Duration
	ch chan time.Time
}

func newTimerClock() *timerClock { return &timerClock{waits: make(chan time.Duration, 64)} }

func (c *timerClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return t0.Add(c.elapsed)
}

func (c *timerClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- t0.Add(c.elapsed)
	} else {
		c.timers = append(c.timers, clockTimer{c.elapsed + d, ch})
	}
	c.waits <- d
	return ch
}

// advance moves the clock by d and fires every timer now due.
func (c *timerClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.elapsed += d
	kept := c.timers[:0]
	for _, tm := range c.timers {
		if tm.at <= c.elapsed {
			tm.ch <- t0.Add(c.elapsed)
		} else {
			kept = append(kept, tm)
		}
	}
	c.timers = kept
}

// fakeChild is an evaluator child on in-memory pipes: it decodes one
// request, waits for begin (the transfer, as slow as the test makes it),
// says begun, then answers on an in-process engine on clock, unless hang.
type fakeChild struct {
	begin chan struct{}
	hang  bool
	clock Clock
}

func (f *fakeChild) spawn() (io.WriteCloser, io.Reader, func(), error) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	dead, hang, begin, clock := make(chan struct{}), f.hang, f.begin, f.clock
	go func() {
		dec, enc := gob.NewDecoder(inR), gob.NewEncoder(outW)
		var req request
		if dec.Decode(&req) != nil {
			return
		}
		select {
		case <-begin:
		case <-dead:
			return
		}
		if enc.Encode(begun{Begun: true}) != nil || hang {
			return
		}
		_ = enc.Encode(newLocal(clock).serve(req)) // ignored: a killed child's write fails
	}()
	kill := func() {
		close(dead)
		_ = inR.Close()  // ignored: closing a pipe end does not fail
		_ = outW.Close() // ignored: as above
	}
	return inW, outR, kill, nil
}

// childEngine is a process engine on parent whose child is f.
func childEngine(parent Clock, f *fakeChild) *Engine {
	e := newLocal(parent)
	e.proc = &evaluator{spawn: f.spawn}
	return e
}

// answerAsync is e's answer to the world's full tick, on a goroutine.
func answerAsync(e *Engine, w *world, vs Versions) <-chan tickResult {
	snap := w.snap()
	out := make(chan tickResult, 1)
	go func() {
		a, fs := e.Answer(vs, snap, w.ro, FullQuestions(snap, nil))
		out <- tickResult{a, fs}
	}()
	return out
}

type tickResult struct {
	a  Answers
	fs []Failure
}

// TestEvaluatorSlowBeginIsNotThePolicys: the studio's defect (2026-11-15).
// The child takes longer than the whole deadline plus evalGrace to begin
// (to read and decode the snapshot on a loaded host); once begun it answers
// at once. The version answers, and nothing fails: before the fix the
// parent's timer ran from the write, killed the child and failed every
// version, "over the tick's time budget of 500ms: the policy evaluator was
// stopped".
func TestEvaluatorSlowBeginIsNotThePolicys(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	parent := newTimerClock()
	f := &fakeChild{begin: make(chan struct{}), clock: newFakeClock()}
	e := childEngine(parent, f)
	got := answerAsync(e, w, Versions{Active: pv(2, defaultBody)})
	<-parent.waits // the parent is waiting on its child
	parent.advance(TickTime + evalGrace + time.Millisecond)
	close(f.begin)
	r := <-got
	require.Empty(t, r.fs, "a slow transfer is not the policy's")
	assert.Equal(t, 2, r.a.Version)
	assert.NotNil(t, e.proc.kill, "the child is kept")
}

// TestEvaluatorHungChildIsStopped: a child that has begun and does not
// answer by the deadline plus evalGrace, counted from its begin, is killed
// and every version fails; the next request starts a new child.
func TestEvaluatorHungChildIsStopped(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	parent := newTimerClock()
	f := &fakeChild{begin: make(chan struct{}), hang: true, clock: newFakeClock()}
	close(f.begin)
	e := childEngine(parent, f)
	got := answerAsync(e, w, Versions{Active: pv(2, defaultBody)})
	assert.Equal(t, evalBegin, <-parent.waits, "the transfer's bound")
	assert.Equal(t, TickTime+evalGrace, <-parent.waits, "the run's bound, from the begin: the whole budget")
	parent.advance(TickTime + evalGrace)
	r := <-got
	assert.Equal(t, -1, r.a.Version)
	require.Len(t, r.fs, 2)
	for i, label := range []string{"v2", "default"} {
		assert.Equal(t, label, r.fs[i].Label)
		assert.Equal(t, overTime()+": the policy evaluator was stopped", r.fs[i].Msg)
	}
	assert.Nil(t, e.proc.kill, "the hung child is killed")
	f.hang = false
	got = answerAsync(e, w, Versions{})
	r = <-got
	require.Empty(t, r.fs)
	assert.Equal(t, 0, r.a.Version, "the next request starts a new child")
}

// TestEvaluatorThatNeverBeginsIsStopped: the transfer has its own bound,
// evalBegin; a child that never begins is killed there and every version
// fails, naming the bound.
func TestEvaluatorThatNeverBeginsIsStopped(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	parent := newTimerClock()
	f := &fakeChild{begin: make(chan struct{}), clock: newFakeClock()}
	e := childEngine(parent, f)
	got := answerAsync(e, w, Versions{})
	<-parent.waits
	parent.advance(evalBegin)
	r := <-got
	assert.Equal(t, -1, r.a.Version)
	require.Len(t, r.fs, 1)
	assert.Equal(t, "default", r.fs[0].Label)
	assert.Contains(t, r.fs[0].Msg, "the policy evaluator did not begin the request within")
	assert.Nil(t, e.proc.kill)
}

// TestEvaluatorRunawayScriptStillStopped: a runaway script is stopped by
// the child's own deadline, on the child's clock from its begin, with the
// whole budget: the version fails over the time budget, reported by the
// child (not a kill), however long the transfer took.
func TestEvaluatorRunawayScriptStillStopped(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	parent, child := newTimerClock(), newTimerClock()
	f := &fakeChild{begin: make(chan struct{}), clock: child}
	e := childEngine(parent, f)
	hung, _ := override("stitch", "def stitch(stream):\n    x = []\n    for _i in range(30000):\n        x = [[[[[[[[[[x]]]]]]]]]]\n    return len(str(x)) > 0")
	got := answerAsync(e, w, Versions{Active: pv(2, hung)})
	<-parent.waits
	parent.advance(time.Second) // a slow transfer, charged to no version
	close(f.begin)
	assert.Equal(t, TickTime, <-child.waits, "the child's deadline: the whole budget")
	child.advance(TickTime)
	r := <-got
	require.NotEmpty(t, r.fs)
	assert.Equal(t, "v2", r.fs[0].Label)
	assert.Contains(t, r.fs[0].Msg, overTime())
	assert.NotContains(t, r.fs[0].Msg, "the policy evaluator was stopped")
}
