package run

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// prepare reconstructs a recipient's cursor position only on cold start or
// when it falls behind an externally advanced cursor. Catch-up is bounded.
type pushPosition struct {
	state   store.Snapshot
	dropped map[int]int
}

func (p *Pusher) prepare(ctx context.Context, recipient string, cursor int64) (pushPosition, error) {
	position, ok := p.views[recipient]
	if !ok {
		position = pushPosition{state: store.Rebuild(nil), dropped: map[int]int{}}
	}
	for page := 0; page < maxFoldPages && position.state.Seq < cursor; page++ {
		if err := ctx.Err(); err != nil {
			return position, err
		}
		events, err := p.store.Events(ctx, position.state.Seq, store.MaxEvents)
		if err != nil {
			return position, fmt.Errorf("run: push history: %w", err)
		}
		if len(events) == 0 {
			return position, fmt.Errorf("run: push history gap after %d", position.state.Seq)
		}
		next := copyPosition(position)
		for _, event := range events {
			if event.Seq > cursor {
				break
			}
			if event.Seq != next.state.Seq+1 {
				return position, fmt.Errorf("run: push history gap: got %d, expected %d", event.Seq, next.state.Seq+1)
			}
			applyHistory(&next.state, event)
		}
		position = next
		p.views[recipient] = position
	}
	if position.state.Seq < cursor {
		return position, fmt.Errorf("run: push history catch-up continues next tick")
	}
	return position, nil
}
func copyPosition(p pushPosition) pushPosition {
	return pushPosition{state: snapshotCopy(p.state), dropped: maps.Clone(p.dropped)}
}

// applyHistory updates a private copy using the same after-image and clear
// rules as store.Rebuild. It never holds or changes the store's own maps.
func applyHistory(s *store.Snapshot, e store.Event) {
	s.Seq = e.Seq
	if !e.Accepted || e.After == nil {
		return
	}
	a := e.After
	if a.Epoch != 0 {
		s.Epoch = a.Epoch
		s.Streams = map[string]store.Stream{}
		s.Cards = map[string]store.Card{}
		s.Rows = map[string]store.Row{}
		s.Judgements = map[string]store.Judgement{}
	}
	for k, v := range a.Streams {
		s.Streams[k] = v
	}
	for k, v := range a.Cards {
		v.Needs = slices.Clone(v.Needs)
		v.After = slices.Clone(v.After)
		v.Paths = slices.Clone(v.Paths)
		v.DoneWhen = slices.Clone(v.DoneWhen)
		s.Cards[k] = v
	}
	for k, v := range a.Rows {
		v.Tiers = slices.Clone(v.Tiers)
		s.Rows[k] = v
	}
	for k, v := range a.Judgements {
		s.Judgements[k] = v
	}
	for k, v := range a.Cursors {
		s.Cursors[k] = v
	}
	activated := 0
	deactivated := false
	for k, v := range a.Policies {
		if v.Active {
			activated = k
		} else if k == s.Policy {
			deactivated = true
		}
	}
	if deactivated {
		s.Policy = 0
	}
	if activated != 0 {
		s.Policy = activated
	}
}
