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
	presence []string
}
type PushFailures interface{ PushFailed(string) }

// PushWorker is the single owner of the pusher, off the scheduler's path.
// Replacing an unsent view is safe: Push scans every event through the newer seq.
type PushWorker struct {
	pusher   *Pusher
	deadline TickDeadline
	log      PushFailures
	updates  chan pushUpdate
}

func NewPushWorker(p *Pusher, _ string, d TickDeadline, log PushFailures) *PushWorker {
	return &PushWorker{pusher: p, deadline: d, log: log, updates: make(chan pushUpdate, 1)}
}
func (w *PushWorker) Offer(s store.Snapshot, at time.Time) {
	w.OfferPresence(s, at, nil)
}
func (w *PushWorker) OfferPresence(s store.Snapshot, at time.Time, who []string) {
	update := pushUpdate{snapshot: s, at: at, presence: append([]string(nil), who...)}
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
			for _, name := range pushRecipients(update.snapshot, update.presence) {
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
func pushRecipients(s store.Snapshot, presence []string) []string {
	names := map[string]bool{}
	for name, row := range s.Rows {
		if name != "" && name != "sprint" && row.Session != "" && row.Role != "" && containsName(presence, name) {
			names[name] = true
		}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
