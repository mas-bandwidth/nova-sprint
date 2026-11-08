// Package run coordinates the single sprint chooser. Its state machine is
// specified in tla/Run.tla; the bus transport is supplied by the edge layer.
package run

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// HookLease owns the sprint bus name for the life of the chooser.
type HookLease interface {
	Check(context.Context) error
	Close(context.Context) error
}

// Hook acquires the one bus name; implementations must not leave it held on error.
type Hook interface {
	Acquire(context.Context, string) (HookLease, error)
}

type chooserSession interface {
	Try(context.Context) (bool, error)
	Holder(context.Context) (string, error)
	Check(context.Context) error
	Close(context.Context) error
}

// ErrLockLost means the service must stop before another Apply.
var ErrLockLost = errors.New("LOCK LOST")

// Chooser holds the dedicated database session and bus hook until Close.
type Chooser struct {
	session chooserSession
	hook    HookLease
}

// acquireChooser takes the database lock before the bus name. DO NOT REMOVE:
// a chooser starts only with both; after observing loss it stops applying.
func acquireChooser(ctx context.Context, session chooserSession, bus Hook) (*Chooser, error) {
	locked, err := session.Try(ctx)
	if err != nil {
		_ = session.Close(ctx)
		return nil, fmt.Errorf("run: acquire chooser lock: %w", err)
	}
	if !locked {
		holder, findErr := session.Holder(ctx)
		_ = session.Close(ctx)
		if findErr != nil {
			return nil, fmt.Errorf("run: identify chooser: %w", findErr)
		}
		return nil, fmt.Errorf("SPRINT REFUSED run: run already chooses on %s: stop it there, or nova-sprint show run", holder)
	}
	lease, err := bus.Acquire(ctx, "sprint")
	if err != nil {
		_ = session.Close(ctx)
		return nil, fmt.Errorf("SPRINT REFUSED run: sprint is already hooked or cannot be hooked: stop the other hook, or nova-sprint show run: %w", err)
	}
	return &Chooser{session: session, hook: lease}, nil
}

// Check observes the session and hook before applying a tick. It is separate
// from store.Apply and cannot make that observation atomic with its commit.
func (c *Chooser) Check(ctx context.Context) error {
	if err := c.session.Check(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %v", ErrLockLost, err)
	}
	if err := c.hook.Check(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: sprint hook: %v", ErrLockLost, err)
	}
	return nil
}

// Close releases the hook and database session, even if one close fails.
func (c *Chooser) Close(_ context.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hookErr := c.hook.Close(ctx)
	sessionErr := c.session.Close(ctx)
	if hookErr != nil {
		return hookErr
	}
	return sessionErr
}
