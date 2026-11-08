package policy

import (
	"context"
	"time"
)

// Clock is a tick's time (cold read 3's ruling): Now and After, as package
// time has them. The deadline (TickTime) and the early fallback
// (fallbackAfter) are measured on it. RealClock is the binary's, which
// layer 6 wires; every unit test passes a fake, so no unit test reads the
// wall clock (the timing probes, behind NOVA_LOAD, are the exception).
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// RealClock is the wall clock.
type RealClock struct{}

// Now is time.Now.
func (RealClock) Now() time.Time { return time.Now() }

// After is time.After.
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// deadline is ctx cancelled once budget has passed on e's clock since start,
// or with ctx; budget is TickTime, or what a parent left its evaluator. A
// deadline already passed when it is made cancels ctx before it returns,
// not when the watcher goroutine is next scheduled: whether the tick's
// inputs alone spent it is the clock's answer, never the scheduler's.
func (e *Engine) deadline(ctx context.Context, start time.Time) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	fire := e.clock.After(e.budget - e.clock.Now().Sub(start))
	select {
	case <-fire:
		cancel()
		return ctx, cancel
	default:
	}
	go func() {
		select {
		case <-fire:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
