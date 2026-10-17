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

// TestReaderHoldWriteFixed pins the writer switch of the reader hold
// (readerHold, setReaderHold): the hold is written as the ReaderHold variable
// table's wire, never as the legacy JSON; the written value reads back equal
// through the dual reader, the old reader kept for the switch (S4); a legacy
// fixture of the same record still reads; a value the table cannot hold is
// refused by name before any key is written, with no legacy fallback; and the
// per-form counter counts the writes' form. It runs on the in-memory backend
// only (store.NewMem), with the clock at 1000.
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
	require.NoError(t, m.RowsAdd(ctx, st.Names.Table(sprint.Readers), []string{"held", "released", "legacy", "refused"}))
	kv, err := st.rootKV()
	require.NoError(t, err)
	codec := st.readerHoldRows()

	// The writer writes the variable table's wire, never the legacy JSON (S1, S4).
	require.NoError(t, st.SetReaderAway(ctx, "held", true, "coordinator"))
	raw, ok, err := kv.GetKey(ctx, readerAwayKey("held"))
	require.NoError(t, err)
	require.True(t, ok)
	assert.False(t, LegacyObject([]byte(raw)), "the writer writes no legacy JSON: %q", raw)
	require.NotEmpty(t, raw)
	assert.Equal(t, byte(1), raw[0], "the variable table's form byte")

	// The dual reader kept for the switch reads what the switched writer writes (S4).
	want := readerHold{Away: true, At: now, By: "coordinator"}
	got, form, err := codec.Read([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, schemaread.FormBinary, form)
	assert.Equal(t, want, got)

	// Releasing writes the table form too.
	require.NoError(t, st.SetReaderAway(ctx, "held", false, "coordinator"))
	raw, ok, err = kv.GetKey(ctx, readerAwayKey("held"))
	require.NoError(t, err)
	require.True(t, ok)
	assert.False(t, LegacyObject([]byte(raw)), "a release writes no legacy JSON: %q", raw)
	got, _, err = codec.Read([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, readerHold{}, got)

	// A legacy fixture of the same record still reads (S4).
	legacy, err := json.Marshal(readerHold{
		Away: true, At: now, By: "old", Held: true, Reason: "limit", Return: true,
	})
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(ctx, readerAwayKey("legacy"), string(legacy)))
	got, form, err = codec.Read(legacy)
	require.NoError(t, err)
	assert.Equal(t, schemaread.FormLegacy, form)
	assert.Equal(t, readerHold{
		Away: true, At: now, By: "old", Held: true, Reason: "limit", Return: true,
	}, got)

	// A value the table cannot hold is refused by name before a key is written,
	// and the legacy form is no fallback (S2, S4): a NUL is not table text.
	bad := readerHold{Away: true, Held: true, Reason: "gap\x00holder"}
	err = st.setReaderHold(ctx, "refused", bad, "coordinator")
	require.Error(t, err)
	assert.Regexp(t, `^READERHOLD REFUSED: [a-z_]+; run: nova-sprint check$`, err.Error())
	var refusal *schemaread.Refusal
	assert.ErrorAs(t, err, &refusal)
	_, ok, err = kv.GetKey(ctx, readerAwayKey("refused"))
	require.NoError(t, err)
	assert.False(t, ok, "a refused hold writes no key, legacy or table")

	// Reads and the writer's form are counted per record on Check (S4).
	report, _, err := st.Check(ctx, 1)
	require.NoError(t, err)
	require.Contains(t, report.RowForms, "READERHOLD")
	assert.Positive(t, report.RowForms["READERHOLD"]["binary"], "the table form's reads are counted")
	assert.EqualValues(t, 1, report.RowForms["READERHOLD"]["legacy"], "the legacy fixture's read is counted")
}
