package run

import (
	"context"
	"sort"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type pushUpdate struct {
	snapshot store.Snapshot
	at       time.Time
}
type PushFailures interface{ PushFailed(string) }

// PushWorker is the single owner of the pusher, off the scheduler's path.
// Replacing an unsent view is safe: Push scans every event through the newer seq.
type PushWorker struct {
	pusher      *Pusher
	coordinator string
	deadline    TickDeadline
	log         PushFailures
	updates     chan pushUpdate
}

func NewPushWorker(p *Pusher, coordinator string, d TickDeadline, log PushFailures) *PushWorker {
	return &PushWorker{pusher: p, coordinator: coordinator, deadline: d, log: log, updates: make(chan pushUpdate, 1)}
}
func (w *PushWorker) Offer(s store.Snapshot, at time.Time) {
	update := pushUpdate{snapshot: s, at: at}
	select {
	case w.updates <- update:
		return
	default:
	}
	select {
	case <-w.updates:
	default:
	}
	select {
	case w.updates <- update:
	default:
	}
}
func (w *PushWorker) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case update := <-w.updates:
			for _, name := range pushRecipients(update.snapshot, w.coordinator) {
				if err := ctx.Err(); err != nil {
					return err
				}
				pushCtx, cancel := w.deadline.Begin(ctx)
				err := w.pusher.Push(pushCtx, name, update.snapshot, update.at)
				cancel()
				if err != nil && ctx.Err() == nil {
					w.log.PushFailed(name)
				}
			}
		}
	}
}
func pushRecipients(s store.Snapshot, coordinator string) []string {
	names := map[string]bool{}
	for name := range s.Rows {
		if name != "" && name != "sprint" {
			names[name] = true
		}
	}
	if coordinator != "" && coordinator != "sprint" {
		names[coordinator] = true
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
