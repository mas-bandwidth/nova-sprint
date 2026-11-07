package verbs

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Choices persists non-secret setup answers as inactive policy data versions.
// It never activates data, so the active script stays active. See tla/Setup.tla.
type Choices struct {
	Store store.Store
	Actor string
	Now   func() time.Time
}

func choiceName(question string) (string, error) {
	if !slices.Contains([]string{"coordinator", "dashboard", "services", "run-on"}, question) {
		return "", fmt.Errorf("invalid non-secret question")
	}
	return "setup:" + question, nil
}

// Get returns the newest durable global answer from policy after-images.
func (c Choices) Get(ctx context.Context, question string) (string, bool, error) {
	name, err := choiceName(question)
	if err != nil {
		return "", false, err
	}
	snap, err := c.Store.Snapshot(ctx)
	if err != nil {
		return "", false, err
	}
	return c.getAt(ctx, name, snap)
}

func (c Choices) getAt(ctx context.Context, name string, snap store.Snapshot) (string, bool, error) {
	cursor := int64(0)
	value := ""
	found := false
	for cursor < snap.Seq {
		events, err := c.Store.Events(ctx, cursor, store.MaxEvents)
		if err != nil {
			return "", false, err
		}
		if len(events) == 0 {
			return "", false, fmt.Errorf("event history ended before snapshot")
		}
		before := cursor
		for _, event := range events {
			if event.Seq > snap.Seq {
				break
			}
			if event.Seq != cursor+1 {
				return "", false, fmt.Errorf("event history is not contiguous")
			}
			cursor = event.Seq
			if !event.Accepted || event.After == nil {
				continue
			}
			for _, p := range event.After.Policies {
				if p.Kind == "data" && p.Name == name {
					value, found = p.Body, true
				}
			}
		}
		if cursor == before {
			return "", false, fmt.Errorf("event history made no progress")
		}
	}
	return value, found, nil
}

// Save writes one immutable inactive data version; a stale snapshot is refused
// so a concurrent writer must re-read the durable answer before asking again.
func (c Choices) Save(ctx context.Context, question, value string) error {
	name, err := choiceName(question)
	if err != nil {
		return err
	}
	if value == "" || c.Actor == "" || c.Now == nil {
		return fmt.Errorf("invalid setup answer")
	}
	snap, err := c.Store.Snapshot(ctx)
	if err != nil {
		return err
	}
	existing, found, err := c.getAt(ctx, name, snap)
	if err != nil {
		return err
	}
	if found {
		if existing == value {
			return nil
		}
		return fmt.Errorf("setup answer already stored")
	}
	id := consumerOpID("choice:"+c.Actor, snap.Epoch, name+"\x00"+value, int(snap.Seq))
	op := store.Op{ID: id, Epoch: snap.Epoch, Actor: c.Actor, Kind: "setup answer", At: c.Now(), Expect: []store.Expect{store.AtSeq{Seq: snap.Seq}}, Writes: []store.Write{store.PutPolicy{Kind: "data", Name: name, Body: value}}}
	r, err := applyOnce(ctx, c.Store, op, c.Store.Apply)
	if err != nil {
		return err
	}
	if !r.Accepted {
		return fmt.Errorf("setup answer refused: %s", r.Reason)
	}
	return nil
}
