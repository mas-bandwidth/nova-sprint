package ratebudget

import (
	"context"
	"errors"
	"net"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

var t0 = time.Date(2026, 10, 11, 1, 30, 0, 0, time.UTC)

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

// The store is down: the waiter proceeds as if unlimited, logs one line, and returns no
// error (the owner, 2026-10-11: "i don't want this rate limiter causing errors"). The
// store's every dial is refused by the client's own dialer, as a store that is down
// refuses it: no socket is opened (RULES 6a), and the waiter's clock is the test's.
func TestStoreDownProceedsUnlimited(t *testing.T) {
	t.Parallel()
	dead := redis.NewClient(&redis.Options{Addr: "store.invalid:6379", MaxRetries: -1,
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("dial tcp store.invalid:6379: connect: connection refused")
		}})
	t.Cleanup(func() {
		// ignored: a test client's close
		_ = dead.Close()
	})
	b := Redis{C: dead}
	var lines []string
	clk := &fakeClock{now: t0}
	w := Waiter{Now: clk.Now, Sleep: clk.Sleep, Log: func(l string) { lines = append(lines, l) }, What: "rpm 10 for inception/mercury-3"}
	out := w.Wait(context.Background(), func(ctx context.Context) (Grant, error) {
		return b.TakeRate(ctx, RateKey("", "inception/mercury-3"), 10, Window, clk.Now(), ID("m"))
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
