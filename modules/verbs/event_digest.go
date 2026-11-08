package verbs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// EventReader reads the immutable event history in sequence order.
type EventReader interface {
	Events(context.Context, int64, int) ([]store.Event, error)
}

// EventPrefixDigest hashes the retained events through seq, independent of
// later events. JSON's stable map-key ordering makes this deterministic.
func EventPrefixDigest(ctx context.Context, st EventReader, seq int64) (string, error) {
	return digestEvents(ctx, st, seq, nil)
}
func digestEvents(ctx context.Context, st EventReader, seq int64, fold *eventFold) (string, error) {
	if seq < 0 {
		return "", fmt.Errorf("invalid event sequence")
	}
	h := sha256.New()
	encoder := json.NewEncoder(h)
	cursor := int64(0)
	for cursor < seq {
		batch, err := st.Events(ctx, cursor, store.MaxEvents)
		if err != nil {
			return "", err
		}
		if len(batch) == 0 {
			return "", fmt.Errorf("event prefix is incomplete")
		}
		advanced := false
		for _, event := range batch {
			if event.Seq > seq {
				break
			}
			if event.Seq <= cursor {
				return "", fmt.Errorf("event order is invalid")
			}
			if err = encoder.Encode(event); err != nil {
				return "", err
			}
			if fold != nil {
				fold.add(event)
			}
			cursor = event.Seq
			advanced = true
		}
		if !advanced {
			return "", fmt.Errorf("event prefix is incomplete")
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
