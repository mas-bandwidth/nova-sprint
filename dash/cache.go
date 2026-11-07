// Package dash serves a local, incrementally updated view of the sprint store.
package dash

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
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
	initialized bool
	anchor      historyAnchor
	roster      roster.Roster // each row's kind and width (round 22); SetRoster
}

// SetRoster sets the roster the page reads each row's kind and width from
// (round 22: they are nova-config's, never the store's); the next Advance
// shows it. Until it is set every row is in the fleet at width 0.
func (c *Cache) SetRoster(r roster.Roster) {
	c.mu.Lock()
	c.roster = r
	c.mu.Unlock()
}

func New(ctx context.Context, s store.Store) (*Cache, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard startup snapshot (30s deadline): check PostgreSQL connectivity and schema: %w", err)
	}
	c := &Cache{state: snap, at: time.Now(), now: time.Now, pending: true, initialized: true, gate: make(chan struct{}, 1)}
	c.view = mapSnapshot(c.state, c.roster, c.at)
	c.samples = []sample{{c.at, c.view.Landed}}
	return c, nil
}

// AdvanceDeadline bounds one Advance. 500 ms held on the studio; a page from a store an
// ocean away (hetzner2 to the studio, 121 ms RTT, 2026-11-07) did not fit it and the board
// stayed stale, so it is five seconds: a late advance lands on a later tick.
var AdvanceDeadline = 5 * time.Second

// NewRecovering keeps an unavailable cache when the initial snapshot fails.
// The caller may serve it and run the poller while reporting the returned error.
func NewRecovering(ctx context.Context, s store.Store) (*Cache, error) {
	c, err := New(ctx, s)
	if err == nil {
		return c, nil
	}
	c = &Cache{err: err, now: time.Now, gate: make(chan struct{}, 1)}
	c.view = mapSnapshot(store.Snapshot{}, roster.Roster{}, time.Time{})
	return c, err
}

// Advance reads a committed prefix in pages and keeps the last good copy on error.
func (c *Cache) Advance(ctx context.Context, s store.Store) error {
	ctx, cancel := context.WithTimeout(ctx, AdvanceDeadline)
	defer cancel()
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	start := c.clock()
	if !c.initialized {
		snap, err := s.Snapshot(ctx)
		if err != nil {
			return c.failed(err)
		}
		c.state = snap
		c.initialized = true
		c.pending = true
	}
	if err := c.checkHistory(ctx, s); err != nil {
		return c.failed(err)
	}
	if err := c.readAvailable(ctx, s); err != nil {
		return c.failed(err)
	}
	var cursorErr error
	if c.pending {
		cursorErr = c.saveCursor(ctx, s)
		if cursorErr == nil {
			c.pending = false
		}
	}
	// The gate serializes state writers. Build the immutable published view
	// outside the reader lock, including when cursor persistence needs a retry.
	at := c.clock()
	c.mu.RLock()
	ros := c.roster
	c.mu.RUnlock()
	view := mapSnapshot(c.state, ros, at)
	c.mu.Lock()
	c.at = at
	c.readSeconds = at.Sub(start).Seconds()
	c.err = nil
	c.alarm = false
	c.view = view
	c.addSample(at, view.Landed)
	c.mu.Unlock()
	if cursorErr != nil {
		log.Printf("dashboard cursor warning: %v", cursorErr)
		return cursorErr
	}
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
		seq := c.state.Seq
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
			c.state = advanceSnapshot(c.state, events)
			c.anchor = anchorOf(events[len(events)-1])
			c.pending = true
		}
		if len(events) < store.MaxEvents {
			return nil
		}
	}
}

type historyAnchor struct {
	seq   int64
	op    string
	epoch int
}

func anchorOf(e store.Event) historyAnchor { return historyAnchor{e.Seq, e.Op, e.Epoch} }

// checkHistory tests the last folded event's identity before trusting the cache.
// The initial lookup fetches an event; later checks replay its receipt. Replay
// still reads after-images, but cursor receipts are ordinarily small. This is
// continuity detection, not a proof that the entire log is authentic.
func (c *Cache) checkHistory(ctx context.Context, s store.Store) error {
	seq := c.state.Seq
	if seq == 0 {
		return nil
	}
	if c.anchor.seq != seq {
		events, err := s.Events(ctx, seq-1, 1)
		if err != nil {
			return err
		}
		if len(events) != 0 && events[0].Seq == seq {
			c.anchor = anchorOf(events[0])
			return nil
		}
	} else {
		r, found, err := s.Replay(ctx, c.anchor.op)
		if err != nil {
			return err
		}
		if found && r.Seq == seq && r.Op == c.anchor.op && r.Epoch == c.anchor.epoch {
			return nil
		}
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return err
	}
	if snap.Seq >= seq {
		return fmt.Errorf("event %d missing or changed in store history", seq)
	}
	c.state = snap
	c.anchor = historyAnchor{}
	c.pending = true
	return nil
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
