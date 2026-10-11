package ratebudget

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/testredis"
	"github.com/redis/go-redis/v9"
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
			if err != nil {
				t.Fatalf("take at %s: %v", now, err)
			}
			if g.OK {
				granted = append(granted, now)
			} else if g.Wait <= 0 {
				t.Fatalf("a refused take at %s names no wait", now)
			}
		}
	}
	if most := overWindow(granted, Minute); most > 10 {
		t.Fatalf("%d grants in one minute across two members, budget 10", most)
	}
	// five minutes at a window of 62 s: at least four full windows' worth
	if len(granted) < 40 {
		t.Fatalf("only %d grants in five minutes: the budget was not used", len(granted))
	}
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
				if err != nil {
					t.Errorf("take: %v", err)
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
	if ok != 10 {
		t.Fatalf("%d grants at one instant, budget 10", ok)
	}
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
		if g, err := b.Acquire(ctx, key, lane, 2, LeaseTTL, t0); err != nil || !g.OK {
			t.Fatalf("lane %s: %+v %v", lane, g, err)
		}
	}
	g, err := b.Acquire(ctx, key, "c", 2, LeaseTTL, t0)
	if err != nil || g.OK || g.Wait <= 0 || g.Wait > MaxPoll {
		t.Fatalf("lane c with two in flight: %+v %v, want a wait of at most %s", g, err, MaxPoll)
	}
	if n, _ := b.Use(ctx, key, 0, t0); n != 2 {
		t.Fatalf("in flight %d, want 2", n)
	}
	if g, _ := b.Acquire(ctx, key, "a", 2, LeaseTTL, t0.Add(time.Second)); !g.OK {
		t.Fatalf("a holder renewing its own slot waited: %+v", g)
	}
	if err := b.Release(ctx, key, "a"); err != nil {
		t.Fatal(err)
	}
	if g, err := b.Acquire(ctx, key, "c", 2, LeaseTTL, t0.Add(2*time.Second)); err != nil || !g.OK {
		t.Fatalf("lane c after a released: %+v %v", g, err)
	}
}

// A holder that crashes never releases: its slot is reclaimed when its lease expires, so
// no lane is stranded (tla/RateBudget.tla, NoSlotLost).
func TestCrashedHoldersLeaseIsReclaimed(t *testing.T) {
	t.Parallel()
	addr := testredis.Start(t)
	b := Redis{C: client(t, addr)}
	key := LeaseKey("", "deepseek")
	ctx := context.Background()
	if g, _ := b.Acquire(ctx, key, "crashed", 1, LeaseTTL, t0); !g.OK {
		t.Fatal("first holder refused")
	}
	if g, _ := b.Acquire(ctx, key, "next", 1, LeaseTTL, t0.Add(LeaseTTL-time.Second)); g.OK {
		t.Fatal("a live lease was taken over")
	}
	if g, err := b.Acquire(ctx, key, "next", 1, LeaseTTL, t0.Add(LeaseTTL+time.Millisecond)); err != nil || !g.OK {
		t.Fatalf("the crashed holder's slot was not reclaimed at its expiry: %+v %v", g, err)
	}
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
	if !out.Open || out.Granted || out.Ended {
		t.Fatalf("outcome %+v, want open", out)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "RATE BUDGET OPEN") {
		t.Fatalf("lines %q, want one OPEN line", lines)
	}
	mem := &Mem{Fail: errors.New("store gone")}
	if out := w.Wait(context.Background(), func(ctx context.Context) (Grant, error) {
		return mem.Acquire(ctx, "k", "h", 1, LeaseTTL, t0)
	}); !out.Open {
		t.Fatalf("a failing lease store did not open: %+v", out)
	}
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
		if g, _ := mem.TakeRate(context.Background(), key, 10, Window, clk.Now(), "x"); !g.OK {
			t.Fatal("budget refused below 10")
		}
	}
	var lines []string
	w := Waiter{Now: clk.Now, Sleep: clk.Sleep, Log: func(l string) { lines = append(lines, l) }, What: "rpm 10"}
	out := w.Wait(context.Background(), func(ctx context.Context) (Grant, error) {
		return mem.TakeRate(ctx, key, 10, Window, clk.Now(), "late")
	})
	if !out.Granted || out.Open || out.Ended {
		t.Fatalf("outcome %+v, want granted", out)
	}
	if out.Waited < Window-time.Second || out.Waited > Window+MaxPoll {
		t.Fatalf("waited %s, want about one window (%s)", out.Waited, Window)
	}
	if len(lines) < 2 || !strings.Contains(lines[0], "RATE BUDGET WAIT") || !strings.Contains(lines[len(lines)-1], "RATE BUDGET GRANTED") {
		t.Fatalf("lines %q: want the wait visible, then the grant", lines)
	}
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
	if !out.Ended || out.Granted || out.Open {
		t.Fatalf("outcome %+v, want ended", out)
	}
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
	if most := overWindow(granted, Minute); most > 10 {
		t.Fatalf("%d in one minute", most)
	}
}
