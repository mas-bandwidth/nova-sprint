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

// TestSetServerWriteFixed pins the writer switch of the server record (S4): SetServer
// (seat.go) writes the table form (the variable table wire, form byte 1) instead of today's
// legacy JSON, and what it writes reads back equal through the dual reader; the writer never
// falls back to the legacy form; and the legacy fixtures of the same record still read while
// old readers remain (the dual reader, kept). It runs on the in-memory backend only
// (store.NewMem), with the clock at 1000.
func TestSetServerWriteFixed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "set-server-write-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	kv, err := st.rootKV()
	require.NoError(t, err)

	want := ServerRecord{
		Actor: "emma", At: now.Add(time.Second), Version: "v1.5.0",
		Commit: "abc123", Started: now.Add(-time.Second), Land: true, PID: 4242, Host: "h1",
	}

	// 1. The writer writes the table wire (form byte 1, the variable table), never the legacy
	// form, and what it writes reads back equal through the dual reader.
	require.NoError(t, st.SetServer(ctx, want))
	raw, ok, err := kv.GetKey(ctx, keyServer)
	require.NoError(t, err)
	require.True(t, ok)
	require.False(t, LegacyObject([]byte(raw)), "SetServer must not fall back to the legacy form")
	require.Equal(t, byte(1), raw[0], "the server record is written as the variable table wire")
	want.At = now // SetServer stamps At with the clock's reading
	got, _, err := st.serverRows().Read([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, want, got)

	// 2. A legacy fixture of the same record still reads through the dual reader: the old
	// readers keep reading during the writer switch (S4).
	legacy, err := json.Marshal(want)
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(ctx, keyServer, string(legacy)))
	server, ok, err := st.Server(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, want, server)
}
