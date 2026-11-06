// Package dash serves a local, incrementally updated view of the sprint store.
package dash

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Cache starts at one consistent snapshot and applies every later event.
type Cache struct {
	mu          sync.RWMutex
	gate        chan struct{}
	state       store.Snapshot
	at          time.Time
	err         error
	view        envelope
	now         func() time.Time
	samples     []sample
	alarm       bool
	readSeconds float64
	pending     bool
}

func New(ctx context.Context, s store.Store) (*Cache, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard startup snapshot (30s deadline): check PostgreSQL connectivity and schema, then restart dashboard: %w", err)
	}
	c := &Cache{state: snap, at: time.Now(), now: time.Now, pending: true, gate: make(chan struct{}, 1)}
	c.view = mapSnapshot(c.state, c.at)
	c.samples = []sample{{c.at, c.view.Landed}}
	return c, nil
}

// Advance reads a committed prefix in pages and keeps the last good copy on error.
func (c *Cache) Advance(ctx context.Context, s store.Store) error {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	start := c.clock()
	if err := c.readAvailable(ctx, s); err != nil {
		return c.failed(err)
	}
	c.mu.RLock()
	pending := c.pending
	c.mu.RUnlock()
	if pending {
		if err := c.saveCursor(ctx, s); err != nil {
			return c.failed(err)
		}
	}
	c.mu.Lock()
	c.pending = false
	c.at = c.clock()
	c.readSeconds = c.at.Sub(start).Seconds()
	c.err = nil
	c.alarm = false
	c.view = mapSnapshot(c.state, c.at)
	c.addSample(c.at, c.view.Landed)
	c.mu.Unlock()
	return nil
}
func (c *Cache) failed(err error) error {
	c.mu.Lock()
	c.err = err
	c.checkAlarm(c.clock())
	c.mu.Unlock()
	return err
}
func (c *Cache) readAvailable(ctx context.Context, s store.Store) error {
	for {
		c.mu.RLock()
		seq := c.state.Seq
		c.mu.RUnlock()
		events, err := s.Events(ctx, seq, store.MaxEvents)
		if err != nil {
			return err
		}
		for i, e := range events {
			if e.Seq != seq+int64(i)+1 {
				return fmt.Errorf("event gap after %d: got %d", seq, e.Seq)
			}
		}
		if len(events) > 0 {
			c.mu.Lock()
			c.state = advanceSnapshot(c.state, events)
			c.pending = true
			c.mu.Unlock()
		}
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
	go c.monitor(ctx)
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

func (c *Cache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}
func (c *Cache) checkAlarm(now time.Time) {
	if now.Sub(c.at) >= 32*time.Second && !c.alarm {
		log.Printf("ALARM stale: dashboard data is %s old", now.Sub(c.at))
		c.alarm = true
	}
}

func (c *Cache) monitor(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			c.checkAlarm(c.clock())
			c.mu.Unlock()
		}
	}
}
