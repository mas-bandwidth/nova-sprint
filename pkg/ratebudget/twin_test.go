package ratebudget

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/testredis"
)

// The twin: the in-process Mem backend must agree with Redis, the backend the fleet
// runs, on every scripted sequence under the same explicit now. A test run on the twin
// that passed where the fleet would refuse (or the reverse) would be a false green, so
// each sequence runs against both and requires the same Grant (granted, wait, error kind)
// at every step and the same Use count after (tla/RateBudget.tla).

// result is what one call answered.
type result struct {
	ok   bool
	wait time.Duration
	err  error
}

// cmpCall runs one identical call on Redis and Mem and requires equal grant and error.
func cmpCall(t *testing.T, red, mem Backend, what string, now time.Time, rf, mf func(Backend) result) {
	t.Helper()
	r, m := rf(red), rf(mem)
	if (r.err == nil) != (m.err == nil) {
		t.Fatalf("%s at %s: err red=%v mem=%v", what, now, r.err, m.err)
	}
	if r.err != nil {
		return
	}
	if r.ok != m.ok {
		t.Fatalf("%s at %s: ok red=%v mem=%v", what, now, r.ok, m.ok)
	}
	if !r.ok {
		d := r.wait - m.wait
		if d < 0 {
			d = -d
		}
		if d > time.Millisecond {
			t.Fatalf("%s at %s: wait red=%s mem=%s", what, now, r.wait, m.wait)
		}
	}
}

// cmpUse requires the same Use count on both backends.
func cmpUse(t *testing.T, red, mem Backend, what, key string, window time.Duration, now time.Time) {
	t.Helper()
	rc, re := red.Use(context.Background(), key, window, now)
	mc, me := mem.Use(context.Background(), key, window, now)
	if re != nil || me != nil {
		t.Fatalf("%s use(%q,%s) at %s: err red=%v mem=%v", what, key, window, now, re, me)
	}
	if rc != mc {
		t.Fatalf("%s use(%q,%s) at %s: red=%d mem=%d", what, key, window, now, rc, mc)
	}
}

type opKind int

const (
	opAcquire opKind = iota
	opRelease
	opTakeRate
)

// scriptStep is one step of a scripted sequence: the call, and (optionally) the key to
// read Use over after the step.
type scriptStep struct {
	name   string
	kind   opKind
	key    string
	holder string
	n      int
	window time.Duration
	ttl    time.Duration
	id     string
	now    time.Time
	useKey string
	useWin time.Duration
}

// runScript runs a scripted sequence against Redis and Mem, comparing every step.
func runScript(t *testing.T, red, mem Backend, steps []scriptStep) {
	t.Helper()
	for i, s := range steps {
		what := fmt.Sprintf("step %d %s", i, s.name)
		cmpCall(t, red, mem, what, s.now, func(b Backend) result {
			switch s.kind {
			case opAcquire:
				g, err := b.Acquire(context.Background(), s.key, s.holder, s.n, s.ttl, s.now)
				return result{g.OK, g.Wait, err}
			case opRelease:
				err := b.Release(context.Background(), s.key, s.holder)
				return result{false, 0, err}
			case opTakeRate:
				g, err := b.TakeRate(context.Background(), s.key, s.n, s.window, s.now, s.id)
				return result{g.OK, g.Wait, err}
			}
			return result{}
		}, func(b Backend) result {
			switch s.kind {
			case opAcquire:
				g, err := b.Acquire(context.Background(), s.key, s.holder, s.n, s.ttl, s.now)
				return result{g.OK, g.Wait, err}
			case opRelease:
				err := b.Release(context.Background(), s.key, s.holder)
				return result{false, 0, err}
			case opTakeRate:
				g, err := b.TakeRate(context.Background(), s.key, s.n, s.window, s.now, s.id)
				return result{g.OK, g.Wait, err}
			}
			return result{}
		})
		if s.useKey != "" {
			cmpUse(t, red, mem, what, s.useKey, s.useWin, s.now)
		}
	}
}

// TestTwinAgreesWithRedis is the differential oracle: every scripted sequence runs
// against Redis (the fleet's backend) and a fresh Mem, with the same explicit now values,
// and must agree.
func TestTwinAgreesWithRedis(t *testing.T) {
	t.Parallel()
	addr := testredis.Start(t)

	run := func(name string, seq func(red, mem Backend)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			red := Redis{C: client(t, addr)}
			mem := &Mem{}
			seq(red, mem)
		})
	}

	run("acquire-up-to-n-then-refused", func(red, mem Backend) {
		p := LeaseKey("", "p1")
		runScript(t, red, mem, []scriptStep{
			{name: "acquire h1", kind: opAcquire, key: p, holder: "h1", n: 2, ttl: LeaseTTL, now: t0, useKey: p, useWin: 0},
			{name: "acquire h2", kind: opAcquire, key: p, holder: "h2", n: 2, ttl: LeaseTTL, now: t0, useKey: p, useWin: 0},
			{name: "acquire h3 refused", kind: opAcquire, key: p, holder: "h3", n: 2, ttl: LeaseTTL, now: t0, useKey: p, useWin: 0},
		})
	})

	run("release-by-holder-then-acquire", func(red, mem Backend) {
		q := LeaseKey("", "p2")
		runScript(t, red, mem, []scriptStep{
			{name: "acquire h1", kind: opAcquire, key: q, holder: "h1", n: 1, ttl: LeaseTTL, now: t0, useKey: q, useWin: 0},
			{name: "release h1", kind: opRelease, key: q, holder: "h1", now: t0.Add(time.Second), useKey: q, useWin: 0},
			{name: "acquire h2", kind: opAcquire, key: q, holder: "h2", n: 1, ttl: LeaseTTL, now: t0.Add(time.Second), useKey: q, useWin: 0},
		})
	})

	run("release-by-a-holder-that-holds-nothing", func(red, mem Backend) {
		q := LeaseKey("", "p3")
		runScript(t, red, mem, []scriptStep{
			{name: "release h9 nothing", kind: opRelease, key: q, holder: "h9", now: t0},
			{name: "acquire h1", kind: opAcquire, key: q, holder: "h1", n: 1, ttl: LeaseTTL, now: t0, useKey: q, useWin: 0},
		})
	})

	run("release-by-a-different-holder", func(red, mem Backend) {
		q := LeaseKey("", "p4")
		runScript(t, red, mem, []scriptStep{
			{name: "acquire h1", kind: opAcquire, key: q, holder: "h1", n: 1, ttl: LeaseTTL, now: t0, useKey: q, useWin: 0},
			{name: "release h2 not holder", kind: opRelease, key: q, holder: "h2", now: t0.Add(time.Second)},
			{name: "acquire h3 refused", kind: opAcquire, key: q, holder: "h3", n: 1, ttl: LeaseTTL, now: t0.Add(time.Second), useKey: q, useWin: 0},
		})
	})

	run("same-holder-twice-reentrant", func(red, mem Backend) {
		q := LeaseKey("", "p5")
		runScript(t, red, mem, []scriptStep{
			{name: "acquire h1", kind: opAcquire, key: q, holder: "h1", n: 1, ttl: LeaseTTL, now: t0, useKey: q, useWin: 0},
			{name: "renew h1", kind: opAcquire, key: q, holder: "h1", n: 1, ttl: LeaseTTL, now: t0.Add(time.Second), useKey: q, useWin: 0},
			{name: "acquire h2 refused", kind: opAcquire, key: q, holder: "h2", n: 1, ttl: LeaseTTL, now: t0.Add(2 * time.Second), useKey: q, useWin: 0},
		})
	})

	run("crashed-holder-lease-reclaimed", func(red, mem Backend) {
		q := LeaseKey("", "p6")
		runScript(t, red, mem, []scriptStep{
			{name: "acquire crashed", kind: opAcquire, key: q, holder: "crashed", n: 1, ttl: LeaseTTL, now: t0, useKey: q, useWin: 0},
			{name: "acquire next while live", kind: opAcquire, key: q, holder: "next", n: 1, ttl: LeaseTTL, now: t0.Add(LeaseTTL - time.Second), useKey: q, useWin: 0},
			{name: "acquire next after expiry", kind: opAcquire, key: q, holder: "next", n: 1, ttl: LeaseTTL, now: t0.Add(LeaseTTL + time.Millisecond), useKey: q, useWin: 0},
		})
	})

	run("n-zero-and-negative", func(red, mem Backend) {
		q := LeaseKey("", "p7")
		r := RateKey("", "m0")
		runScript(t, red, mem, []scriptStep{
			{name: "acquire n=0", kind: opAcquire, key: q, holder: "h1", n: 0, ttl: LeaseTTL, now: t0},
			{name: "acquire n=-1", kind: opAcquire, key: q, holder: "h2", n: -1, ttl: LeaseTTL, now: t0},
			{name: "takeRate n=0", kind: opTakeRate, key: r, n: 0, window: Window, now: t0, id: "a"},
			{name: "takeRate n=-1", kind: opTakeRate, key: r, n: -1, window: Window, now: t0, id: "b"},
		})
	})

	run("take-rate-at-the-window-edge", func(red, mem Backend) {
		r := RateKey("", "m1")
		runScript(t, red, mem, []scriptStep{
			{name: "takeRate at start", kind: opTakeRate, key: r, n: 1, window: Window, now: t0, id: "a", useKey: r, useWin: Window},
			{name: "takeRate 1ms before edge", kind: opTakeRate, key: r, n: 1, window: Window, now: t0.Add(Window - time.Millisecond), id: "b", useKey: r, useWin: Window},
			{name: "takeRate at edge", kind: opTakeRate, key: r, n: 1, window: Window, now: t0.Add(Window), id: "c", useKey: r, useWin: Window},
		})
	})

	run("two-keys-never-share-a-count", func(red, mem Backend) {
		rA := RateKey("", "mA")
		rB := RateKey("", "mB")
		runScript(t, red, mem, []scriptStep{
			{name: "takeRate rA", kind: opTakeRate, key: rA, n: 1, window: Window, now: t0, id: "a", useKey: rA, useWin: Window},
			{name: "takeRate rB", kind: opTakeRate, key: rB, n: 1, window: Window, now: t0, id: "b", useKey: rB, useWin: Window},
			{name: "takeRate rA refused", kind: opTakeRate, key: rA, n: 1, window: Window, now: t0, id: "c", useKey: rA, useWin: Window},
			{name: "takeRate rB refused", kind: opTakeRate, key: rB, n: 1, window: Window, now: t0, id: "d", useKey: rB, useWin: Window},
		})
	})
}

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
