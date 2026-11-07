package run

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Notice is one bounded line destined for a recipient, grouped by subject.
type Notice struct{ Subject, Group, Line string }

// Notifications derives messages from committed events; it does no I/O.
type Notifications interface {
	ForEvent(store.Event, store.Snapshot, string) ([]Notice, error)
}
type Delivery struct{ Recipient, ID string }

// Sender returns only after the transport exits successfully and parses its receipt.
type Sender interface {
	Send(context.Context, string, string, string) (Delivery, error)
}
type pushPending struct {
	op       store.Op
	target   int64
	position pushPosition
}

// Pusher runs off the tick path and serializes sends across all recipients. Its cursors
// trail confirmed sends; crash recovery starts from the store's cursors.
type Pusher struct {
	mu      sync.Mutex
	store   store.Store
	notices Notifications
	sender  Sender
	views   map[string]pushPosition
	pending map[string]pushPending
}

func NewPusher(s store.Store, n Notifications, b Sender) *Pusher {
	return &Pusher{store: s, notices: n, sender: b, views: map[string]pushPosition{}, pending: map[string]pushPending{}}
}

// Push handles a bounded committed prefix. The supplied snapshot is the
// engine's folded view; no Snapshot or cursor heartbeat is performed here.
func (p *Pusher) Push(ctx context.Context, recipient string, snap store.Snapshot, at time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	name, err := pushCursorName(recipient)
	if err != nil {
		return err
	}
	if pending, ok := p.pending[recipient]; ok {
		return p.resolve(ctx, recipient, pending)
	}
	cursor := snap.Cursors[name]
	view, err := p.prepare(ctx, recipient, cursor)
	if err != nil {
		return err
	}
	if view.state.Seq >= snap.Seq {
		return nil
	}
	end, batches, next, err := p.collect(ctx, recipient, view, snap.Seq, snap.Epoch)
	if err != nil {
		return err
	}
	if len(batches) == 0 {
		p.views[recipient] = next
		return nil
	}
	for _, batch := range batches {
		delivered, err := p.sender.Send(ctx, recipient, batch.subject, strings.Join(batch.lines, "\n"))
		if err != nil {
			return fmt.Errorf("run: push to %s: %w", recipient, err)
		}
		if delivered.Recipient != recipient || !validDeliveryID(delivered.ID) {
			return fmt.Errorf("run: push to %s: missing recipient/id confirmation", recipient)
		}
	}
	sum := sha256.Sum256([]byte(recipient))
	id := fmt.Sprintf("push.%x.%d", sum[:], end)
	item := pushPending{target: end, position: next, op: store.Op{ID: id, Epoch: snap.Epoch, Actor: "sprint", Kind: "push-cursor", At: at, Writes: []store.Write{store.SetCursor{Name: name, Seq: end}}}}
	p.pending[recipient] = item
	return p.resolve(ctx, recipient, item)
}
