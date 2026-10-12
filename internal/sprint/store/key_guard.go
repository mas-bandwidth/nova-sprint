package store

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// KeyGuard binds a state-changing operation to the exact external presence
// evidence it read. Presence keys do not advance the sprint's table fence.
// Acquire and Relock check these guards in the transaction that takes the
// operation, so a newer proof before that transaction invalidates its plan.
type KeyGuard struct {
	Key     string `json:"key"`
	Value   string `json:"value,omitempty"`
	Present bool   `json:"present"`
}

func (r *Redis) guardedKeys(keys []string, guards []KeyGuard) []string {
	for _, g := range guards {
		keys = append(keys, r.Names.Key(g.Key))
	}
	return keys
}

func (r *Redis) checkKeyGuards(ctx context.Context, tx *redis.Tx, guards []KeyGuard) error {
	if len(guards) == 0 {
		return nil
	}
	values, err := tx.MGet(ctx, r.guardedKeys(nil, guards)...).Result()
	if err != nil {
		return err
	}
	for i, g := range guards {
		value, present := values[i].(string)
		if present != g.Present || present && value != g.Value {
			return errFenceMoved
		}
	}
	return nil
}

// keyGuardsMatch is called only while the memory backend's mutex is held.
func (m *Mem) keyGuardsMatch(guards []KeyGuard) bool {
	for _, g := range guards {
		value, present := m.kv[g.Key]
		if present != g.Present || present && value != g.Value {
			return false
		}
	}
	return true
}
