package dash

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// CursorName is the one dashboard subscriber's durable position.
const CursorName = "dashboard"

func (c *Cache) saveCursor(ctx context.Context, s store.Store) error {
	c.mu.RLock()
	seq, epoch := c.state.Seq, c.state.Epoch
	old, exists := c.state.Cursors[CursorName]
	c.mu.RUnlock()
	if exists && seq == old {
		return nil
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	op := store.Op{ID: fmt.Sprintf("dash-%x", id), Epoch: epoch, Actor: "dashboard", Kind: "cursor", At: c.clock(), Writes: []store.Write{store.SetCursor{Name: CursorName, Seq: seq}}}
	r, err := s.Apply(ctx, op)
	if errors.Is(err, store.ErrUnknown) {
		r, err = recoverCursor(ctx, s, op)
	}
	if err != nil {
		return err
	}
	if !r.Accepted {
		return fmt.Errorf("dashboard cursor refused: %s", r.Reason)
	}
	return c.catchCursor(ctx, s, r.Seq)
}
func recoverCursor(ctx context.Context, s store.Store, op store.Op) (store.Receipt, error) {
	r, found, err := s.Replay(ctx, op.ID)
	if err != nil {
		return r, err
	}
	if found {
		return r, nil
	}
	return s.Apply(ctx, op)
}
func (c *Cache) catchCursor(ctx context.Context, s store.Store, target int64) error {
	for {
		c.mu.RLock()
		seq := c.state.Seq
		c.mu.RUnlock()
		if seq >= target {
			return nil
		}
		events, err := s.Events(ctx, seq, store.MaxEvents)
		if err != nil {
			return err
		}
		batch := make([]store.Event, 0, len(events))
		for _, e := range events {
			if e.Seq > target {
				break
			}
			if e.Seq != seq+int64(len(batch))+1 {
				return fmt.Errorf("dashboard cursor event gap after %d", seq)
			}
			batch = append(batch, e)
		}
		if len(batch) == 0 {
			return fmt.Errorf("dashboard cursor event %d missing", target)
		}
		c.mu.Lock()
		c.state = advanceSnapshot(c.state, batch)
		c.mu.Unlock()
	}
}
