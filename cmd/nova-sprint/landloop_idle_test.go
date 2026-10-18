package main

import (
	"bytes"
	"context"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// queuedToMergeRig is a sprint with one card queued to merge on s1 (no git: the head is a
// name, never pushed), on the test app's in-memory store.
func queuedToMergeRig(t *testing.T) *testApp {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 2")
	ta.deal(1)
	ta.ok("take --as m1 --limit 100")
	ta.ok("finish --as m1 s1-1.w1@1 --head 0123456789abcdef0123456789abcdef01234567")
	ta.ok("ask")
	ta.ok("read --as reader-a --ok --limit 100")
	ta.ok("accept --read-ok")
	return ta
}

// The merge queue's count reads the merge table's row counts first: an empty queue is one
// exchange, never the table's cards, its open notes or the coordinator; a queue that holds a
// card is loaded and counted as before.
func TestTheMergeQueueCountReadsTheShapeFirst(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 2")
	ctx := context.Background()
	before := maps.Clone(ta.m.Calls)
	n, coord, stream, err := ta.a.mergeQueued(ctx, "mem:0")
	require.NoError(t, err)
	require.Zero(t, n)
	require.Empty(t, coord+stream, "nothing queued names no coordinator and no stream")
	used := map[string]int{}
	for k, v := range ta.m.Calls {
		if d := v - before[k]; d != 0 {
			used[k] = d
		}
	}
	require.Equal(t, map[string]int{"shapes": 1}, used, "an empty merge queue is read as the table's shape alone")

	q := queuedToMergeRig(t)
	n, coord, stream, err = q.a.mergeQueued(ctx, "mem:0")
	require.NoError(t, err)
	require.Equal(t, 1, n, "the queued card is counted")
	require.Equal(t, "coordinator", coord)
	require.Equal(t, "s1", stream)
}

// idleLoop runs the land loop on ta's store for span of the injected clock, every wait
// stepping the clock by what it may take, and returns its output and the times the loop
// read the merge queue. world, when set, is called with the clock at each wait on the log
// and may append a line there in place of the wait.
func idleLoop(t *testing.T, ta *testApp, span time.Duration, world func(now time.Time) bool) (string, []time.Time) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := ta.a.now()
	var mu sync.Mutex
	var looks []time.Time
	b := ta.a.landState()
	b.mu.Lock()
	b.queueSeam = func(ctx context.Context, addr string) (int, string, string, error) {
		mu.Lock()
		looks = append(looks, ta.a.now())
		mu.Unlock()
		return ta.a.mergeQueued(ctx, addr)
	}
	b.mu.Unlock()
	ta.a.sleep = func(d time.Duration) {
		ta.mu.Lock()
		ta.now = ta.now.Add(d)
		over := ta.now.Sub(start) >= span
		ta.mu.Unlock()
		if over {
			cancel()
		}
	}
	ta.m.LogWait = func(d time.Duration) {
		if world != nil && world(ta.a.now()) {
			return
		}
		ta.a.sleep(d)
	}
	var out bytes.Buffer
	ta.a.landLoop(ctx, "mem:0", &out)
	mu.Lock()
	defer mu.Unlock()
	return out.String(), append([]time.Time(nil), looks...)
}

// An idle land loop (nothing queued, nothing in flight, no refusal) waits on the sprint's
// log, not the clock: in ten quiet minutes it looks at the queue once each LandIdleEvery
// (it looked every LandEvery, 300 times, each holding the server's line), and says LAND
// IDLE queued=0 as it goes idle and once a minute after (every look, before).
func TestAnIdleLandLoopWaitsOnTheLogNotTheClock(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	span := 10 * time.Minute
	out, looks := idleLoop(t, ta, span, nil)
	want := int(span / LandIdleEvery)
	require.GreaterOrEqual(t, len(looks), want, "the backstop looks once each %s:\n%s", LandIdleEvery, out)
	require.LessOrEqual(t, len(looks), want+2, "a quiet log is looked at once each %s, not each %s: %d looks", LandIdleEvery, LandEvery, len(looks))
	for i := 2; i < len(looks); i++ {
		require.Equal(t, LandIdleEvery, looks[i].Sub(looks[i-1]), "look %d: the backstop", i)
	}
	idle := strings.Count(out, "LAND IDLE queued=0")
	require.GreaterOrEqual(t, idle, int(span/landIdleSaid), "LAND IDLE queued=0 once a minute:\n%s", out)
	require.LessOrEqual(t, idle, int(span/landIdleSaid)+1, "LAND IDLE queued=0 at most once a minute, not every look:\n%s", out)
	require.NotContains(t, out, "LAND FAILED")
}

// A line on the log wakes the idle loop at once, no sooner than LandEvery after its last
// look: a card queued to merge is a table move, a line on the log, and is landed on the look
// it wakes, never a backstop later.
func TestALineOnTheLogWakesTheIdleLandLoop(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	var at time.Time
	waits := 0
	world := func(now time.Time) bool {
		waits++
		if waits == 4 { // a few quiet slices into a wait
			ta.ok("add --stream s1 --count 2")
			at = now
			return true
		}
		return false
	}
	out, looks := idleLoop(t, ta, 2*time.Minute, world)
	require.False(t, at.IsZero(), "the world wrote its line:\n%s", out)
	var next time.Time
	for _, l := range looks {
		if !l.Before(at) {
			next = l
			break
		}
	}
	require.False(t, next.IsZero(), "a look after the line: %v", looks)
	require.LessOrEqual(t, next.Sub(at), LandEvery, "the line was looked at within LandEvery, not the %s backstop: line %s, looks %v", LandIdleEvery, at, looks)
}

// A standing refusal keeps the LandEvery beat: its line is said on every beat
// (tla/ServerSwitch.tla BeatNamesTheRefusal), never waited out on the log.
func TestAStandingRefusalKeepsTheLandBeat(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	b := ta.a.landState()
	b.mu.Lock()
	b.refusal = &landRefusal{kind: causeRefused, reason: "the git_identity is empty", remedy: "nova-config set", since: ta.a.now(), judged: true}
	b.mu.Unlock()
	out, looks := idleLoop(t, ta, time.Minute, nil)
	require.GreaterOrEqual(t, len(looks), int(time.Minute/LandEvery)-1, "a refusal keeps the %s beat: %d looks:\n%s", LandEvery, len(looks), out)
	require.Contains(t, out, "LAND REFUSED")
	require.NotContains(t, out, "LAND IDLE queued=0")
}

// quietOld is the test's store with epoch 0's log deaf: a wait on it hears no line and takes
// its whole time, and on its second slice the world moves the epoch (a clear, then a card
// added on the new epoch's log), as when the lines of a clear and after it are all on the
// new epoch's log.
type quietOld struct {
	store.Backend
	mu     sync.Mutex
	slices int
	world  func()
	sleep  func(time.Duration)
}

// WaitLog is the store's wait on epoch 0's log (the store waits on the root for epoch 0):
// deaf.
func (q *quietOld) WaitLog(_ context.Context, after string, d time.Duration) (string, bool, error) {
	q.mu.Lock()
	q.slices++
	act := q.slices == 2
	q.mu.Unlock()
	if act {
		q.world()
	}
	q.sleep(d)
	return after, false, nil
}

// The idle wait is on the log of the epoch it began in; a clear moves the epoch. The wait
// reads the epoch again after each slice, so with the old epoch's log quiet a line on the
// new epoch's log (a card queued to merge there) is looked at within a slice and LandEvery,
// never the LandIdleEvery backstop.
func TestAnEpochChangeEndsTheIdleLandWait(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	q := &quietOld{Backend: ta.m, sleep: func(d time.Duration) { ta.a.sleep(d) }}
	plain := ta.a.backend
	wrapped := func(context.Context, string, sprint.Names) (store.Backend, error) { return q, nil }
	var cleared time.Time
	q.world = func() {
		ta.a.backend = plain // the verbs' own store (the loop waits inside this call)
		ta.ok("clear --confirm sprint")
		ta.ok("add --stream s1 --count 2")
		ta.a.backend = wrapped
		cleared = ta.a.now()
	}
	ta.a.backend = wrapped // the land loop's store
	out, looks := idleLoop(t, ta, 2*time.Minute, nil)
	require.False(t, cleared.IsZero(), "the world cleared and added:\n%s\nlooks %v", out, looks)
	var next time.Time
	for _, l := range looks {
		if l.After(cleared) {
			next = l
			break
		}
	}
	require.False(t, next.IsZero(), "a look after the clear: %v", looks)
	require.LessOrEqual(t, next.Sub(cleared), landIdleLook+LandEvery, "the new epoch was looked at within a slice, not the %s backstop: clear %s, looks %v", LandIdleEvery, cleared, looks)
}
