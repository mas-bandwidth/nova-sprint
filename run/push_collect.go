package run

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

const maxPushBytes = 4096
const maxPushLines = 20

type pushKey struct{ subject, group string }
type pushBatch struct {
	subject, group string
	lines          []string
	bytes, omitted int
}
type pushBuilder struct {
	recipient string
	batches   []pushBatch
	indexes   map[pushKey]int
}

// collect reads a bounded prefix, grouping notices by subject and judgement group.
func (p *Pusher) collect(ctx context.Context, recipient string, position pushPosition, through int64, epoch int) (int64, []pushBatch, pushPosition, error) {
	position = copyPosition(position)
	end := position.state.Seq
	b := pushBuilder{recipient: recipient, indexes: map[pushKey]int{}}
	for page := 0; page < maxFoldPages && end < through; page++ {
		if err := ctx.Err(); err != nil {
			return end, nil, position, err
		}
		events, err := p.store.Events(ctx, end, store.MaxEvents)
		if err != nil {
			return end, nil, position, fmt.Errorf("run: push events: %w", err)
		}
		if len(events) == 0 {
			return end, nil, position, fmt.Errorf("run: push event gap after %d", end)
		}
		for _, event := range events {
			if event.Seq > through {
				return end, b.finish(), position, nil
			}
			if event.Seq != end+1 {
				return end, nil, position, fmt.Errorf("run: push event gap: got %d, expected %d", event.Seq, end+1)
			}
			applyHistory(&position.state, event)
			if event.Accepted {
				if err := p.collectEvent(&b, &position, event, recipient, epoch); err != nil {
					return end, nil, position, err
				}
			}
			end = event.Seq
		}
	}
	return end, b.finish(), position, nil
}
func (p *Pusher) collectEvent(b *pushBuilder, position *pushPosition, event store.Event, recipient string, epoch int) error {
	if event.After != nil && event.After.Epoch > event.Epoch {
		n := position.dropped[event.Epoch]
		delete(position.dropped, event.Epoch)
		return b.add(Notice{Subject: "EPOCH CLEARED", Line: fmt.Sprintf("epoch %d cleared: %d pending notices dropped (some may have reached you); run nova-sprint next", event.Epoch, n)})
	}
	notices, err := p.notices.ForEvent(event, position.state, recipient)
	if err != nil {
		return err
	}
	if event.Epoch < epoch {
		position.dropped[event.Epoch] += len(notices)
		return nil
	}
	for _, notice := range notices {
		if err = b.add(notice); err != nil {
			return err
		}
	}
	return nil
}
func (b *pushBuilder) add(n Notice) error {
	if n.Subject == "" || n.Line == "" || len(n.Subject) > 256 || len(n.Group) > 128 || strings.ContainsAny(n.Subject+n.Group+n.Line, "\r\n") {
		return fmt.Errorf("run: invalid notification")
	}
	key := pushKey{n.Subject, n.Group}
	i, ok := b.indexes[key]
	if !ok {
		i = len(b.batches)
		b.indexes[key] = i
		b.batches = append(b.batches, pushBatch{subject: n.Subject, group: n.Group})
	}
	batch := &b.batches[i]
	reserve := len(fmt.Sprintf("and %d more: nova-sprint next --as %s", int64(math.MaxInt64), b.recipient)) + 1
	if batch.group != "" {
		reserve += len(fmt.Sprintf("group=%s n=%d", batch.group, int64(math.MaxInt64))) + 1
	}
	size := len(n.Line)
	if len(batch.lines) > 0 {
		size++
	}
	if len(batch.lines) >= maxPushLines || batch.bytes+size+reserve > maxPushBytes {
		batch.omitted++
		return nil
	}
	batch.lines = append(batch.lines, n.Line)
	batch.bytes += size
	return nil
}
func (b *pushBuilder) finish() []pushBatch {
	for i := range b.batches {
		batch := &b.batches[i]
		if batch.group != "" {
			batch.lines = append([]string{fmt.Sprintf("group=%s n=%d", batch.group, len(batch.lines)+batch.omitted)}, batch.lines...)
		}
		if batch.omitted > 0 {
			batch.lines = append(batch.lines, fmt.Sprintf("and %d more: nova-sprint next --as %s", batch.omitted, b.recipient))
		}
	}
	return b.batches
}
