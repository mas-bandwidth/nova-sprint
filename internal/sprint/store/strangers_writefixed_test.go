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

// TestStrangersWriteFixed pins the writer switch of the strangers record (S4: readers
// adopted first, then writers; C30): putStrangers writes each stranger entry in the table
// form, the padded standard base64 of the Stranger table's wire (friendrows.schema Stranger,
// S2), and never in the legacy per-entry JSON form; what it writes reads back equal through the
// dual reader (presenceRowMap), a legacy whole-record fixture still reads through the same
// reader, and the store's unit tests run on the in-memory backend only (store.NewMem).
func TestStrangersWriteFixed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "t-strangers-fixed-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	kv, err := st.rootKV()
	require.NoError(t, err)

	s := map[string]stranger{
		"box-1": {Seen: now.Add(5 * time.Second), Told: true},
		"box-2": {Seen: now.Add(6 * time.Second)},
	}

	// The writer writes the table form; the dual reader reads it back equal.
	require.NoError(t, st.putStrangers(ctx, s))
	raw, ok, err := kv.GetKey(ctx, keyStrangers)
	require.NoError(t, err)
	require.True(t, ok)
	// The stored value is an object of base64 table wires, one per name: a per-entry legacy
	// JSON object would not decode into a map of strings.
	var vals map[string]string
	require.NoError(t, json.Unmarshal([]byte(raw), &vals), "each stranger entry is written in the table form (base64 of its wire)")
	require.Len(t, vals, 2)
	for name, v := range vals {
		require.NotEmpty(t, v, name)
		assert.NotEqual(t, '{', v[0], name+" is base64, not a legacy per-entry object")
	}
	got, err := st.strangers(ctx)
	require.NoError(t, err)
	assert.Equal(t, s, got)

	// A legacy whole-record fixture still reads through the dual reader (S4: readers first,
	// kept, during the writer switch).
	legacy, err := json.Marshal(map[string]stranger{"box-9": {Seen: now.Add(9 * time.Second), Told: false}})
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(ctx, keyStrangers, string(legacy)))
	legacyGot, err := st.strangers(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]stranger{"box-9": {Seen: now.Add(9 * time.Second), Told: false}}, legacyGot)
}
