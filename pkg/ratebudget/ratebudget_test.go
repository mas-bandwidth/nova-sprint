package ratebudget

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/testredis"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 10, 11, 1, 30, 0, 0, time.UTC)

func client(t *testing.T, addr string) *redis.Client {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() {
		// ignored: a test client's close; the test's assertions are the report
		_ = c.Close()
	})
	return c
}

// overWindow is the most grants of times in any span of w: the window invariant read off
// the grants themselves (tla/RateBudget.tla, NeverOverRate).
func overWindow(times []time.Time, w time.Duration) int {
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	most := 0
	for i := range times {
		n := 0
		for j := i; j < len(times) && times[j].Sub(times[i]) < w; j++ {
			n++
		}
		most = max(most, n)
	}
	return most
}

// Two members, each its own client of one Redis, ask for Mercury 3's budget of 10 a
// minute every half second for five minutes: together they are granted at most 10 in
// any minute, and the budget is used (10 a window, not fewer).
func TestTwoMembersShareOneWindowNeverOverTenAMinute(t *testing.T) {
	t.Parallel()
	addr := testredis.Start(t)
	members := []Redis{{C: client(t, addr)}, {C: client(t, addr)}}
	key := RateKey("", "inception/mercury-3-preview-1002")
	ctx := context.Background()
	var granted []time.Time
	for tick := range 600 {
		now := t0.Add(time.Duration(tick) * 500 * time.Millisecond)
		for i, m := range members {
			g, err := m.TakeRate(ctx, key, 10, Window, now, ID(fmt.Sprintf("m%d", i)))
			require.NoError(t, err, "take at %s", now)
			if g.OK {
				granted = append(granted, now)
			} else {
				require.Positive(t, g.Wait, "a refused take at %s names no wait", now)
			}
		}
	}
	most := overWindow(granted, Minute)
	require.LessOrEqual(t, most, 10, "%d grants in one minute across two members, budget 10", most)
	// five minutes at a window of 62 s: at least four full windows' worth
	require.GreaterOrEqual(t, len(granted), 40, "only %d grants in five minutes: the budget was not used", len(granted))
}

// Many callers race for the last grants at the same instant through their own clients:
// the WATCH guard lets exactly the budget through (a read and an unguarded write would
// let them all: MCRateBudgetBrokenRace).
func TestRacingCallersTakeExactlyTheBudget(t *testing.T) {
	t.Parallel()
	addr := testredis.Start(t)
	key := RateKey("", "deepseek/x")
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := Redis{C: client(t, addr)}
			for range 20 {
				g, err := m.TakeRate(context.Background(), key, 10, Window, t0, ID(fmt.Sprint(i)))
				if !assert.NoError(t, err, "take") {
					return
				}
				if g.OK {
					mu.Lock()
					ok++
					mu.Unlock()
					return
				}
				if g.Wait > RaceWait {
					return // spent: told to wait the window out
				}
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 10, ok, "%d grants at one instant, budget 10", ok)
}

// A concurrency budget of 2 with three lanes: the third waits, and takes the slot the
// first releases.
func TestConcurrencyTwoThirdLaneWaits(t *testing.T) {
	t.Parallel()
	addr := testredis.Start(t)
	b := Redis{C: client(t, addr)}
	key := LeaseKey("", "deepseek")
	ctx := context.Background()
	for _, lane := range []string{"a", "b"} {
		g, err := b.Acquire(ctx, key, lane, 2, LeaseTTL, t0)
		require.NoError(t, err, "lane %s", lane)
		require.True(t, g.OK, "lane %s refused: %+v", lane, g)
	}
	g, err := b.Acquire(ctx, key, "c", 2, LeaseTTL, t0)
	require.NoError(t, err)
	require.False(t, g.OK, "lane c with two in flight was granted: %+v", g)
	require.Positive(t, g.Wait, "lane c names no wait")
	require.LessOrEqual(t, g.Wait, MaxPoll, "lane c wait %s, want at most %s", g.Wait, MaxPoll)
	n, err := b.Use(ctx, key, 0, t0)
	require.NoError(t, err)
	require.Equal(t, 2, n, "in flight %d, want 2", n)
	g, err = b.Acquire(ctx, key, "a", 2, LeaseTTL, t0.Add(time.Second))
	require.NoError(t, err)
	require.True(t, g.OK, "a holder renewing its own slot waited: %+v", g)
	require.NoError(t, b.Release(ctx, key, "a"))
	g, err = b.Acquire(ctx, key, "c", 2, LeaseTTL, t0.Add(2*time.Second))
	require.NoError(t, err)
	require.True(t, g.OK, "lane c after a released: %+v", g)
}

// A holder that crashes never releases: its slot is reclaimed when its lease expires, so
// no lane is stranded (tla/RateBudget.tla, NoSlotLost).
func TestCrashedHoldersLeaseIsReclaimed(t *testing.T) {
	t.Parallel()
	addr := testredis.Start(t)
	b := Redis{C: client(t, addr)}
	key := LeaseKey("", "deepseek")
	ctx := context.Background()
	g, err := b.Acquire(ctx, key, "crashed", 1, LeaseTTL, t0)
	require.NoError(t, err)
	require.True(t, g.OK, "first holder refused")
	g, err = b.Acquire(ctx, key, "next", 1, LeaseTTL, t0.Add(LeaseTTL-time.Second))
	require.NoError(t, err)
	require.False(t, g.OK, "a live lease was taken over")
	g, err = b.Acquire(ctx, key, "next", 1, LeaseTTL, t0.Add(LeaseTTL+time.Millisecond))
	require.NoError(t, err)
	require.True(t, g.OK, "the crashed holder's slot was not reclaimed at its expiry: %+v", g)
}

// The store is down: the waiter proceeds as if unlimited, logs one line, and returns no
// error (the owner, 2026-10-11: "i don't want this rate limiter causing errors").
func TestStoreDownProceedsUnlimited(t *testing.T) {
	t.Parallel()
	dead := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 200 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() {
		// ignored: a test client's close
		_ = dead.Close()
	})
	b := Redis{C: dead}
	var lines []string
	w := Waiter{Log: func(l string) { lines = append(lines, l) }, What: "rpm 10 for inception/mercury-3"}
	out := w.Wait(context.Background(), func(ctx context.Context) (Grant, error) {
		return b.TakeRate(ctx, RateKey("", "inception/mercury-3"), 10, Window, time.Now(), ID("m"))
	})
	require.True(t, out.Open, "outcome %+v, want open", out)
	require.False(t, out.Granted, "outcome %+v, want open", out)
	require.False(t, out.Ended, "outcome %+v, want open", out)
	require.Len(t, lines, 1)
	require.Contains(t, lines[0], "RATE BUDGET OPEN")
	mem := &Mem{Fail: errors.New("store gone")}
	out = w.Wait(context.Background(), func(ctx context.Context) (Grant, error) {
		return mem.Acquire(ctx, "k", "h", 1, LeaseTTL, t0)
	})
	require.True(t, out.Open, "a failing lease store did not open: %+v", out)
}

// fakeClock is a waiter's clock that a sleep advances.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (f *fakeClock) Now() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now }
func (f *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
	return nil
}

// A caller over the budget stalls, logs the wait, and is granted when the oldest grant
// leaves the window (tla/RateBudget.tla, StalledIsGranted); it never fails.
func TestStalledCallerIsEventuallyGranted(t *testing.T) {
	t.Parallel()
	clk := &fakeClock{now: t0}
	mem := &Mem{}
	key := RateKey("", "inception/mercury-3")
	for range 10 {
		g, _ := mem.TakeRate(context.Background(), key, 10, Window, clk.Now(), "x")
		require.True(t, g.OK, "budget refused below 10")
	}
	var lines []string
	w := Waiter{Now: clk.Now, Sleep: clk.Sleep, Log: func(l string) { lines = append(lines, l) }, What: "rpm 10"}
	out := w.Wait(context.Background(), func(ctx context.Context) (Grant, error) {
		return mem.TakeRate(ctx, key, 10, Window, clk.Now(), "late")
	})
	require.True(t, out.Granted, "outcome %+v, want granted", out)
	require.False(t, out.Open, "outcome %+v, want granted", out)
	require.False(t, out.Ended, "outcome %+v, want granted", out)
	require.GreaterOrEqual(t, out.Waited, Window-time.Second, "waited %s, want about one window (%s)", out.Waited, Window)
	require.LessOrEqual(t, out.Waited, Window+MaxPoll, "waited %s, want about one window (%s)", out.Waited, Window)
	require.GreaterOrEqual(t, len(lines), 2, "lines %q: want the wait visible, then the grant", lines)
	require.Contains(t, lines[0], "RATE BUDGET WAIT")
	require.Contains(t, lines[len(lines)-1], "RATE BUDGET GRANTED")
}

// A stall is bounded only by the caller's context (the card's deadline): its end ends the
// wait, never with an error.
func TestStallEndsWithTheCardsDeadline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	w := Waiter{Sleep: func(context.Context, time.Duration) error {
		n++
		if n == 3 {
			cancel()
			return context.Canceled
		}
		return nil
	}}
	out := w.Wait(ctx, func(context.Context) (Grant, error) { return Grant{Wait: time.Minute}, nil })
	require.True(t, out.Ended, "outcome %+v, want ended", out)
	require.False(t, out.Granted, "outcome %+v, want ended", out)
	require.False(t, out.Open, "outcome %+v, want ended", out)
}

// The in-process backend keeps the same window: never more than n in any span.
func TestMemWindowNeverOver(t *testing.T) {
	t.Parallel()
	m := &Mem{}
	var granted []time.Time
	for s := range 600 {
		now := t0.Add(time.Duration(s) * 250 * time.Millisecond)
		if g, _ := m.TakeRate(context.Background(), "k", 10, Window, now, "x"); g.OK {
			granted = append(granted, now)
		}
	}
	most := overWindow(granted, Minute)
	require.LessOrEqual(t, most, 10, "%d in one minute", most)
}
