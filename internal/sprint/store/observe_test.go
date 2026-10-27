package store

import (
	"context"
	"time"
)

// The tests' own reads of the store: each observes what a production path
// wrote, and none is called outside the tests.

// RestorePending says the sprint's epoch is owed its step (EpochState.DefaultsOwed) and its
// restore's marker is absent: the members and the readers are not restored yet.
func (st *Store) RestorePending(ctx context.Context) (bool, error) {
	pinned, err := st.pin(ctx)
	if err != nil {
		return false, err
	}
	es, err := pinned.EpochNow(ctx)
	if err != nil {
		return false, err
	}
	if es.N != pinned.epoch || !es.DefaultsOwed {
		return false, nil
	}
	done, err := pinned.membersRestored(ctx)
	return !done, err
}

// RecordStoreRTT records one store round trip d that began at the store's now.
func (st *Store) RecordStoreRTT(ctx context.Context, d time.Duration) error {
	kv, err := st.kv()
	if err != nil {
		return err
	}
	v, ok, err := kv.GetKey(ctx, keyStoreRTT)
	if err != nil {
		return err
	}
	return st.keepStoreRTT(ctx, kv, readStoreRTT(v, ok), st.now(), d)
}

// StoppedTotal is the total time the machine has been STOPPED, until now.
func (m Machine) StoppedTotal(now time.Time) time.Duration {
	d := m.StoppedFor
	if n := len(m.Spans); n > 0 && m.Spans[n-1].To.IsZero() && now.After(m.Spans[n-1].From) {
		d += now.Sub(m.Spans[n-1].From)
	}
	return d
}
