package run

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// ConfiguredRow is the authoritative width and tiers from nova-config.
type ConfiguredRow struct {
	Name  string
	Width int
	Tiers []string
}
type Presence interface {
	Who(context.Context) ([]string, error)
}
type Configuration interface {
	Rows(context.Context) ([]ConfiguredRow, error)
}
type Clock interface {
	Now() time.Time
	Every(time.Duration) Ticker
}
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// Publisher must enqueue without blocking on dashboard or network work.
type Publisher interface {
	Offer(store.Snapshot, time.Time)
}
type TickMetrics struct {
	Number        int64
	At            time.Time
	Took          time.Duration
	Seq           int64
	PlannedPolicy int
	Err           error
}
type TickLogger interface{ Log(TickMetrics) }

// TickDeadline supplies the configured policy time budget.
type TickDeadline interface {
	Begin(context.Context) (context.Context, context.CancelFunc)
}
type Results interface{ Drain() []EdgeResult }
type observation struct {
	at       time.Time
	presence []string
	config   []ConfiguredRow
}

// Scheduler owns observations and calls Engine serially. Deadline policy is
// supplied by its caller's context; the engine never reads a wall clock.
type Scheduler struct {
	engine       *Engine
	clock        Clock
	presence     Presence
	config       Configuration
	publish      Publisher
	logger       TickLogger
	results      Results
	deadline     TickDeadline
	refreshEvery int64
	count        int64
	lastRefresh  int64
	loaded       bool
	rows         []ConfiguredRow
	queued       []EdgeResult
}

func NewScheduler(e *Engine, c Clock, p Presence, config Configuration, pub Publisher, log TickLogger, results Results, deadline TickDeadline, refreshEvery int64) (*Scheduler, error) {
	if refreshEvery < 1 {
		return nil, fmt.Errorf("run: config refresh interval must be positive")
	}
	return &Scheduler{engine: e, clock: c, presence: p, config: config, publish: pub, logger: log, results: results, deadline: deadline, refreshEvery: refreshEvery}, nil
}

// Step is one externally scheduled tick. Its caller bounds ctx to policy's deadline.
func (s *Scheduler) Step(ctx context.Context, incoming []EdgeResult) (err error) {
	start := s.clock.Now()
	s.count++
	s.queued = mergeInbox(s.queued, incoming)
	attempted := false
	defer func() {
		snap, fresh := s.engine.FreshState()
		if attempted && fresh {
			s.publish.Offer(snap, s.clock.Now())
		}
		s.logger.Log(TickMetrics{Number: s.count, At: start, Took: s.clock.Now().Sub(start), Seq: snap.Seq, PlannedPolicy: s.engine.plannedPolicy(), Err: err})
	}()
	if err = s.engine.guard.Check(ctx); err != nil {
		return err
	}
	who, err := s.presence.Who(ctx)
	if err != nil {
		return fmt.Errorf("run: presence: %w", err)
	}
	if !s.loaded || s.count-s.lastRefresh >= s.refreshEvery {
		rows, readErr := s.config.Rows(ctx)
		if readErr != nil {
			return fmt.Errorf("run: configuration: %w", readErr)
		}
		s.rows = copyConfig(rows)
		s.loaded = true
		s.lastRefresh = s.count
	}
	inbox := s.queued
	s.queued = nil
	attempted = true
	return s.engine.tick(ctx, inbox, observation{at: start, presence: who, config: s.rows})
}

// Run waits for the injected one-second ticker; results are drained once per tick.
func (s *Scheduler) Run(ctx context.Context) error {
	ticker := s.clock.Every(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-ticker.C():
			if !ok {
				return nil
			}
			tickCtx, cancel := s.deadline.Begin(ctx)
			err := s.Step(tickCtx, s.results.Drain())
			cancel()
			if err != nil && errors.Is(err, ErrLockLost) {
				return err
			}
		}
	}
}
func copyConfig(rows []ConfiguredRow) []ConfiguredRow {
	out := make([]ConfiguredRow, len(rows))
	for i, r := range rows {
		out[i] = r
		out[i].Tiers = append([]string(nil), r.Tiers...)
	}
	return out
}
