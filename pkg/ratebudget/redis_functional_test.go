//go:build functional

// The window and the lease on a real redis-server (testredis), the fleet's backend: a
// functional test (RULES 6a: no external service in a unit test).
package ratebudget

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mas-bandwidth/nova-sprint/pkg/testredis"
)

func client(t *testing.T, addr string) *redis.Client {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() {
		// ignored: a test client's close; the test's assertions are the report
		_ = c.Close()
	})
	return c
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

