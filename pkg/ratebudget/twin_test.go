package ratebudget

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestWaiterOverMemGrantedAfterRefusal: a caller over a full Mem budget stalls once, logs
// the wait, and is granted when the oldest grant leaves the window.
func TestWaiterOverMemGrantedAfterRefusal(t *testing.T) {
	t.Parallel()
	clk := &fakeClock{now: t0}
	mem := &Mem{}
	key := RateKey("", "m-w1")
	win := time.Second
	if g, _ := mem.TakeRate(context.Background(), key, 1, win, t0, "x"); !g.OK {
		t.Fatal("prefill refused")
	}
	var lines []string
	w := Waiter{Now: clk.Now, Sleep: clk.Sleep, Log: func(l string) { lines = append(lines, l) }, What: "rpm 1"}
	out := w.Wait(context.Background(), func(ctx context.Context) (Grant, error) {
		return mem.TakeRate(ctx, key, 1, win, clk.Now(), "late")
	})
	if !out.Granted || out.Open || out.Ended {
		t.Fatalf("outcome %+v, want granted", out)
	}
	if out.Waits != 1 {
		t.Fatalf("waits %d, want 1", out.Waits)
	}
	if out.Waited < win {
		t.Fatalf("waited %s, want about one window (%s)", out.Waited, win)
	}
	if len(lines) < 2 || !strings.Contains(lines[0], "RATE BUDGET WAIT") || !strings.Contains(lines[len(lines)-1], "RATE BUDGET GRANTED") {
		t.Fatalf("lines %q: want the wait visible, then the grant", lines)
	}
}

// TestWaiterOverMemEndsWhenContextEnds: the stall ends when the caller's context ends.
func TestWaiterOverMemEndsWhenContextEnds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	w := Waiter{Sleep: func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}}
	out := w.Wait(ctx, func(context.Context) (Grant, error) { return Grant{Wait: time.Minute}, nil })
	if !out.Ended || out.Granted || out.Open {
		t.Fatalf("outcome %+v, want ended", out)
	}
}

// TestWaiterOverMemOpensOnStoreError: a store error opens the budget, logging exactly one
// line, with the reason flattened to one line.
func TestWaiterOverMemOpensOnStoreError(t *testing.T) {
	t.Parallel()
	mem := &Mem{Fail: errors.New("the store said no\nand more\rhere")}
	var lines []string
	w := Waiter{Log: func(l string) { lines = append(lines, l) }, What: "rpm 1"}
	out := w.Wait(context.Background(), func(ctx context.Context) (Grant, error) {
		return mem.Acquire(ctx, "k", "h", 1, LeaseTTL, t0)
	})
	if !out.Open || out.Granted || out.Ended {
		t.Fatalf("outcome %+v, want open", out)
	}
	if len(lines) != 1 {
		t.Fatalf("lines %q, want exactly one", lines)
	}
	if !strings.Contains(lines[0], "RATE BUDGET OPEN") {
		t.Fatalf("line %q not an OPEN line", lines[0])
	}
	if strings.ContainsAny(lines[0], "\n\r") {
		t.Fatalf("the OPEN line holds a raw newline or CR: %q", lines[0])
	}
}

// TestWaiterOverMemLogsFirstAndEveryLogEvery: one WAIT line at the first wait, then one
// every LogEvery.
func TestWaiterOverMemLogsFirstAndEveryLogEvery(t *testing.T) {
	t.Parallel()
	clk := &fakeClock{now: t0}
	var lines []string
	sleeps := 0
	ctx, cancel := context.WithCancel(context.Background())
	w := Waiter{
		Now: clk.Now,
		Sleep: func(c context.Context, d time.Duration) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			clk.now = clk.now.Add(d)
			sleeps++
			if sleeps == 7 {
				cancel()
				return context.Canceled
			}
			return nil
		},
		Log:      func(l string) { lines = append(lines, l) },
		LogEvery: 3 * time.Second,
		What:     "x",
	}
	w.Wait(ctx, func(context.Context) (Grant, error) { return Grant{Wait: time.Second}, nil })
	if len(lines) < 3 {
		t.Fatalf("lines %q, want the first wait and one per LogEvery", lines)
	}
	for i, l := range lines {
		if !strings.Contains(l, "RATE BUDGET WAIT") {
			t.Fatalf("line %d %q is not a WAIT line", i, l)
		}
	}
}

// TestOneLineFlattensNewlinesAndCaps: oneLine turns every newline and CR into a space and
// caps at 200 bytes.
func TestOneLineFlattensNewlinesAndCaps(t *testing.T) {
	t.Parallel()
	s := oneLine("a\nb\r\nc\rd")
	if strings.ContainsAny(s, "\n\r") {
		t.Fatalf("oneLine kept a newline or CR: %q", s)
	}
	if long := oneLine(strings.Repeat("x", 300)); len(long) != 200 {
		t.Fatalf("oneLine length %d, want 200", len(long))
	}
}
