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
	c.roster = copyRoster(r)
	c.mu.Unlock()
}

const startupSnapshotDeadline = 30 * time.Second

func New(ctx context.Context, s store.Store) (*Cache, error) {
	ctx, cancel := context.WithTimeout(ctx, startupSnapshotDeadline)
	defer cancel()
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard startup snapshot (30s deadline): check PostgreSQL connectivity and schema: %w", err)
	}
	c := &Cache{state: snap, at: time.Now(), now: time.Now, pending: snap.Seq > 0, initialized: true, gate: make(chan struct{}, 1)}
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
	gateCtx, gateCancel := context.WithTimeout(ctx, AdvanceDeadline)
	defer gateCancel()
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-gateCtx.Done():
		return gateCtx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	start := c.clock()
	workCtx := gateCtx
	if !c.initialized {
		snapshotCtx, snapshotCancel := context.WithTimeout(ctx, startupSnapshotDeadline)
		snap, err := s.Snapshot(snapshotCtx)
		if err == nil {
			err = snapshotCtx.Err()
		}
		snapshotCancel()
		if err != nil {
			return c.failed(err)
		}
		c.state = snap
		c.initialized = true
		c.pending = snap.Seq > 0
		var cancel context.CancelFunc
		workCtx, cancel = context.WithTimeout(ctx, AdvanceDeadline)
		defer cancel()
	}
	ctx = workCtx
	if err := c.checkHistory(ctx, s); err != nil {
		return c.failed(err)
	}
	if err := c.readAvailable(ctx, s); err != nil {
		return c.failed(err)
	}
	var cursorErr error
	if c.pending {
		var intervened bool
		intervened, cursorErr = c.saveCursor(ctx, s)
		if cursorErr == nil {
			c.pending = intervened
		}
	}
	c.publishRead(start)
	if cursorErr != nil {
		log.Printf("dashboard cursor warning: %v", cursorErr)
		return cursorErr
	}
	return nil
}

// publishRead maps the gate-serialized state outside the reader lock.
func (c *Cache) publishRead(start time.Time) {
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
	if c.view.Epoch != view.Epoch {
		c.samples = nil
	}
	c.view = view
	c.addSample(at, view.Landed)
	c.mu.Unlock()
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
// It verifies continuity, not the authenticity of the entire log.
func (c *Cache) checkHistory(ctx context.Context, s store.Store) error {
	seq := c.state.Seq
	if seq == 0 {
		return nil
	}
	op, epoch, found, err := s.EventIdentity(ctx, seq)
	if err != nil {
		return err
	}
	if found {
		if c.anchor.seq != seq {
			c.anchor = historyAnchor{seq: seq, op: op, epoch: epoch}
			return nil
		}
		if c.anchor.op == op && c.anchor.epoch == epoch {
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
	c.pending = snap.Seq > 0
	return nil
}

func advanceSnapshot(s store.Snapshot, events []store.Event) store.Snapshot {
	policies := map[int]store.PolicyVersion{}
	if s.Policy != 0 {
		policies[s.Policy] = store.PolicyVersion{Active: true}
	}
	image := &store.State{Epoch: s.Epoch, Streams: s.Streams, Cards: s.Cards, Rows: s.Rows, Judgements: s.Judgements, Cursors: s.Cursors, Policies: policies, Stop: &store.Stop{Stopped: s.Stopped, Reason: s.StopReason}}
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
