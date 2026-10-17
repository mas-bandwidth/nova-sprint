package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/hostload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBeatFactsWriteFixed pins the writer switch of the member beat (S4): BeatFacts
// (presence.go) writes the table form (the variable table wire, form byte 1) instead of
// today's legacy JSON, and what it writes reads back equal through the dual reader; the
// writer never falls back to the legacy form; a value the table cannot hold is refused by
// name before a byte is written; and the legacy fixtures of the same record still read while
// old readers remain (the dual reader, kept). It runs on the in-memory backend only
// (store.NewMem), with the clock at 1000.
func TestBeatFactsWriteFixed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	clock := now
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "beat-facts-write-"}, Actor: "tester",
		Now: func() time.Time { return clock }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	kv, err := st.rootKV()
	require.NoError(t, err)

	given := 42.5
	want, err := st.BeatFacts(ctx, "m1", &given, hostload.Source{}, nil, "", []string{"card@1"}, true, []string{"linux", "amd64"})
	require.NoError(t, err)

	// 1. The writer writes the table wire (form byte 1, the variable table), never the legacy
	// form, and what it writes reads back equal through the dual reader.
	raw, ok, err := kv.GetKey(ctx, beatKey("m1"))
	require.NoError(t, err)
	require.True(t, ok)
	require.False(t, LegacyObject([]byte(raw)), "BeatFacts must not fall back to the legacy form")
	require.Equal(t, byte(1), raw[0], "the member beat is written as the variable table wire")
	got, _, err := st.presenceRows().beat.Read([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, want, got)

	// The read sites read what the switched writer wrote: Beats is the store's member beat
	// read, through the same dual reader.
	beats, err := st.Beats(ctx, []string{"m1"})
	require.NoError(t, err)
	assert.Equal(t, want, beats["m1"])

	// 2. A legacy fixture of the same record still reads through the dual reader: the old
	// readers keep reading during the writer switch (S4).
	legacy, err := json.Marshal(want)
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(ctx, beatKey("m2"), string(legacy)))
	old, err := st.Beats(ctx, []string{"m2"})
	require.NoError(t, err)
	assert.Equal(t, want, old["m2"])

	// 3. A value the table cannot hold (a beat time outside the schema's int64 nanoseconds)
	// is refused by name before a byte is written, never written in the legacy form as a
	// fallback (S1, S2).
	clock = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err = st.BeatFacts(ctx, "m3", &given, hostload.Source{}, nil, "", nil, false, nil)
	require.Error(t, err)
	assert.Regexp(t, `^BEAT REFUSED: time_out_of_range; run: nova-sprint check$`, err.Error())
	_, ok, err = kv.GetKey(ctx, beatKey("m3"))
	require.NoError(t, err)
	assert.False(t, ok, "a refused beat writes no byte")
}
