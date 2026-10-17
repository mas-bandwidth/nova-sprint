package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReaderHoldWriteFixed pins the writer switch of the reader hold (S4): the
// coordinator's hold (setReaderHold, through SetReaderAway and
// SetReaderRetired) writes the ReaderHold variable table's wire, and what it
// writes reads back equal through the kept dual reader; a legacy fixture of
// the same record still reads; the store's read site (ReaderStates) reads the
// switched writers' writes and the legacy fixture alike; and the writer never
// falls back to the legacy form (S1, S2, S4). It runs on the in-memory
// backend only (store.NewMem), with the clock at 1000.
func TestReaderHoldWriteFixed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	m := NewMem()
	st := &Store{
		B: m, Names: sprint.Names{Prefix: "reader-hold-write-fixed-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	require.NoError(t, m.RowsAdd(ctx, st.Names.Table(sprint.Readers), []string{"hold", "release", "retired"}))
	kv, err := st.rootKV()
	require.NoError(t, err)
	codec := st.readerHoldRows()

	read := func(reader string) (readerHold, schemaread.Form) {
		t.Helper()
		raw, ok, err := kv.GetKey(ctx, readerAwayKey(reader))
		require.NoError(t, err)
		require.True(t, ok, reader)
		h, form, err := codec.Read([]byte(raw))
		require.NoError(t, err, reader)
		return h, form
	}

	// a legacy fixture of the same record still reads through the kept dual reader (S4)
	legacy := readerHold{Away: true, At: time.Unix(1001, 0).UTC(), By: "coordinator", Held: true, Reason: "limit", Return: true}
	legacyJSON, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(ctx, readerAwayKey("legacy"), string(legacyJSON)))
	h, form := read("legacy")
	assert.Equal(t, schemaread.FormLegacy, form, "legacy fixture read as legacy")
	assert.Equal(t, legacy, h)

	// each writer writes the table form, and what it writes reads back equal (S4)
	require.NoError(t, st.SetReaderAway(ctx, "hold", true, "coordinator"))
	h, form = read("hold")
	assert.Equal(t, schemaread.FormBinary, form, "a hold is the table wire")
	assert.Equal(t, readerHold{Away: true, At: now, By: "coordinator"}, h)

	require.NoError(t, st.SetReaderAway(ctx, "release", false, "coordinator"))
	h, form = read("release")
	assert.Equal(t, schemaread.FormBinary, form, "a release is the table wire")
	assert.Equal(t, readerHold{}, h)

	require.NoError(t, st.SetReaderRetired(ctx, "retired", "coordinator"))
	h, form = read("retired")
	assert.Equal(t, schemaread.FormBinary, form, "a retire is the table wire")
	assert.Equal(t, readerHold{Away: true, Retired: true, At: now, By: "coordinator"}, h)

	// the store's read site reads the switched writers' writes and the legacy
	// fixture alike (S4: the dual reader, kept)
	states, err := st.ReaderStates(ctx, []string{"hold", "release", "retired", "legacy"}, now)
	require.NoError(t, err)
	assert.Equal(t, sprint.ReaderAway, states["hold"])
	assert.Equal(t, sprint.ReaderDown, states["release"])
	assert.Equal(t, sprint.ReaderRetired, states["retired"])
	assert.Equal(t, sprint.ReaderHeld, states["legacy"])

	// the reads are counted per record and per form on Check (S4)
	report, _, err := st.Check(ctx, 1)
	require.NoError(t, err)
	require.Contains(t, report.RowForms, "READERHOLD")
}
