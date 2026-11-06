// Package dash serves a local, incrementally updated view of the sprint store.
package dash

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Cache starts at one consistent snapshot and applies every later event.
type Cache struct {
	mu    sync.RWMutex
	state store.Snapshot
	at    time.Time
	err   error
	view  envelope
}

func New(ctx context.Context, s store.Store) (*Cache, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	c := &Cache{state: snap, at: time.Now()}
	c.view = mapSnapshot(c.state, c.at)
	return c, nil
}

// Advance reads a committed prefix in pages and keeps the last good copy on error.
func (c *Cache) Advance(ctx context.Context, s store.Store) error {
	for {
		c.mu.RLock()
		seq := c.state.Seq
		c.mu.RUnlock()
		events, err := s.Events(ctx, seq, store.MaxEvents)
		if err != nil {
			c.mu.Lock()
			c.err = err
			c.mu.Unlock()
			return err
		}
		c.mu.Lock()
		for i, e := range events {
			if e.Seq != c.state.Seq+int64(i)+1 {
				gap := fmt.Errorf("event gap after %d: got %d", c.state.Seq, e.Seq)
				c.err = gap
				c.mu.Unlock()
				return gap
			}

		}
		if len(events) > 0 {
			c.state = advanceSnapshot(c.state, events)
		}
		c.at = time.Now()
		c.err = nil
		c.view = mapSnapshot(c.state, c.at)
		c.mu.Unlock()
		if len(events) < store.MaxEvents {
			return nil
		}
	}
}

func advanceSnapshot(s store.Snapshot, events []store.Event) store.Snapshot {
	policies := map[int]store.PolicyVersion{}
	if s.Policy != 0 {
		policies[s.Policy] = store.PolicyVersion{Active: true}
	}
	image := &store.State{Epoch: s.Epoch, Streams: s.Streams, Cards: s.Cards, Rows: s.Rows, Judgements: s.Judgements, Cursors: s.Cursors, Policies: policies}
	base := store.Event{Seq: s.Seq, Accepted: true, After: image}
	batch := make([]store.Event, 0, len(events)+1)
	batch = append(batch, base)
	batch = append(batch, events...)
	return store.Rebuild(batch)
}

func (c *Cache) Run(ctx context.Context, s store.Store) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = c.Advance(ctx, s)
		}
	}
}
