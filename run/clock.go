package run

import (
	"context"
	"time"
)

// Clock is run's time: now, and a beat every d. Unit tests inject an inert
// one; production is WallClock.
type (
	Clock interface {
		Now() time.Time
		Every(d time.Duration) Beat
	}
	Beat interface {
		C() <-chan time.Time
		Stop()
	}
)

// WallClock is the production clock.
type WallClock struct{}

func (WallClock) Now() time.Time             { return time.Now() }
func (WallClock) Every(d time.Duration) Beat { return wallTicker{time.NewTicker(d)} }

type wallTicker struct{ ticker *time.Ticker }

func (t wallTicker) C() <-chan time.Time { return t.ticker.C }
func (t wallTicker) Stop()               { t.ticker.Stop() }

// BudgetDeadline takes the current policy duration from the caller each tick.
// The built-in default is 600 ms until a validated policy supplies a value.
type BudgetDeadline struct{ Duration func() time.Duration }

func (b BudgetDeadline) Begin(ctx context.Context) (context.Context, context.CancelFunc) {
	budget := 600 * time.Millisecond
	if b.Duration != nil {
		if configured := b.Duration(); configured > 0 {
			budget = configured
		}
	}
	return context.WithTimeout(ctx, budget)
}
