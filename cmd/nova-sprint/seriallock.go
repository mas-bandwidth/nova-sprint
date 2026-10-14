package main

import (
	"context"
	"sync"

	"golang.org/x/sync/semaphore"
)

// serialLock is the read lane's own line of control (servelanes.go): a one-permit
// weighted semaphore whose waiters wait in the order they came and whose wait is given
// up when its caller's context ends, so a caller that has gone never holds it
// (tla/ServerLanes.tla, GoneNeverRuns). The zero value is unlocked.
type serialLock struct {
	once sync.Once
	sem  *semaphore.Weighted
}

// line is the one-permit semaphore, made on first use so the zero value is unlocked.
func (l *serialLock) line() *semaphore.Weighted {
	l.once.Do(func() { l.sem = semaphore.NewWeighted(1) })
	return l.sem
}

// Lock waits for the line for as long as it takes.
func (l *serialLock) Lock() { l.line().Acquire(context.Background(), 1) }

// LockCtx waits for the line until ctx is done; it holds the line only when it returns
// nil, and a caller that has gone never holds it (servelanes.go, the read lane).
func (l *serialLock) LockCtx(ctx context.Context) error { return l.line().Acquire(ctx, 1) }

// TryLock takes the line when it is free and says whether it did.
func (l *serialLock) TryLock() bool { return l.line().TryAcquire(1) }

// Unlock gives the line back; giving back a line not held is a bug, and panics. The probe
// keeps the semaphore's count whole across the panic (Release decrements before it panics).
func (l *serialLock) Unlock() {
	if l.line().TryAcquire(1) { // free: this is an unheld release; put the probe back
		l.line().Release(1)
		panic("nova-sprint: serial unlocked while not held")
	}
	l.line().Release(1)
}
