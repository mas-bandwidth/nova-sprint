package store

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// recoveryFriends reads a stable presence envelope outside the table fence.
// The operation subsequently guards these exact bytes at Acquire/Relock.
func (st *Store) recoveryFriends(ctx context.Context, s *sprint.Snapshot) ([]sprint.FriendSeat, []KeyGuard, bool, error) {
	kv, ok := st.B.(KV)
	if !ok {
		return nil, nil, true, nil
	}
	raw, present, err := kv.GetKey(ctx, keyFriends)
	if err != nil {
		return nil, nil, false, err
	}
	roster := map[string]friendEntry{}
	if present {
		if err := json.Unmarshal([]byte(raw), &roster); err != nil {
			return nil, nil, false, err
		}
	}
	keys := []string{keyFriends, keySeat}
	var names []string
	for name := range roster {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		keys = append(keys, friendBeatKey(name), friendHealthKey(name), friendFinishKey(name))
	}
	values, exists, err := getKeys(ctx, kv, keys)
	if err != nil {
		return nil, nil, false, err
	}
	if len(values) != len(keys) || len(exists) != len(keys) || exists[0] != present || present && values[0] != raw {
		return nil, nil, false, nil
	}
	seats, err := st.FriendSeats(ctx, s.Now)
	if err != nil {
		return nil, nil, false, err
	}
	after, afterExists, err := getKeys(ctx, kv, keys)
	if err != nil {
		return nil, nil, false, err
	}
	if !slices.Equal(values, after) || !slices.Equal(exists, afterExists) {
		return nil, nil, false, nil
	}
	guards := make([]KeyGuard, len(keys))
	for i, key := range keys {
		guards[i] = KeyGuard{Key: key, Value: values[i], Present: exists[i]}
	}
	return seats, guards, true, nil
}
