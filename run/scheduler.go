package run

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Presence reads nova-bus once per tick.
type Presence interface {
	Who(context.Context) ([]string, error)
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
}

// Scheduler owns observations and calls Engine serially. Deadline policy is
// supplied by its caller's context; the engine never reads a wall clock.
type Scheduler struct {
	engine   *Engine
	clock    Clock
	presence Presence
	publish  Publisher
	logger   TickLogger
	results  Results
	deadline TickDeadline
	count    int64
	queued   []EdgeResult
}

func NewScheduler(e *Engine, c Clock, p Presence, pub Publisher, log TickLogger, results Results, deadline TickDeadline) (*Scheduler, error) {
	if e == nil || c == nil || p == nil || pub == nil || log == nil || results == nil || deadline == nil {
		return nil, fmt.Errorf("run: scheduler dependencies are required")
	}
	return &Scheduler{engine: e, clock: c, presence: p, publish: pub, logger: log, results: results, deadline: deadline}, nil
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
	if !containsName(who, "sprint") {
		return fmt.Errorf("%w: sprint is not present on nova-bus", ErrLockLost)
	}
	inbox := s.queued
	s.queued = nil
	attempted = true
	return s.engine.tick(ctx, inbox, observation{at: start, presence: who})
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
func containsName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
