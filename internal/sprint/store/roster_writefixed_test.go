package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRosterWriteFixed pins the roster writer's switch to the friend-entry table form (S4):
// putRoster writes each entry as the variable table (the padded standard base64 of its wire)
// in one JSON object, and what it writes reads back equal through the dual reader; the
// legacy form still reads. It runs on the in-memory backend only (store.NewMem), with the
// clock at 1000.
func TestRosterWriteFixed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "roster-write-fixed-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	kv, err := st.rootKV()
	require.NoError(t, err)

	entry := friendEntry{
		Held: true, At: now.Add(time.Second), By: "coord", Width: 2, Class: "flash,pro", Mode: "batch", Streams: "v15-*", Kinds: "feature",
		ConfigDir: "/home/f/.claude", Pronouns: "she/her", Session: sprint.FriendOneShot, TokenCapSet: true,
		Roles: "builder,reader", Billing: "plan", Reason: "away", Until: time.Unix(2000, 5).UTC(), Return: true,
	}
	want := map[string]friendEntry{"ann": entry, "dee": {Class: "pro", Width: 1}}

	// The writer writes each entry in the table form: a JSON object whose values are the
	// padded standard base64 of the variable table's wire, never a legacy entry.
	require.NoError(t, putRoster(ctx, kv, want))
	raw, ok, err := kv.GetKey(ctx, keyFriends)
	require.NoError(t, err)
	require.True(t, ok)
	var obj map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &obj))
	require.Len(t, obj, len(want))
	for _, v := range obj {
		assert.Equal(t, byte('"'), v[0], "each roster entry is stored as base64, not legacy JSON")
	}

	// What it writes reads back equal through the dual reader.
	got, _, err := st.roster(ctx)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	// A legacy fixture still reads through the same reader (old readers keep working through
	// the switch).
	legacy, err := json.Marshal(map[string]friendEntry{"leg": entry, "dee": {Class: "pro", Width: 1}})
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(ctx, keyFriends, string(legacy)))
	fromLegacy, _, err := st.roster(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]friendEntry{"leg": entry, "dee": {Class: "pro", Width: 1}}, fromLegacy)
}
