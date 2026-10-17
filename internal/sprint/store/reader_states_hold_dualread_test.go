package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReaderStatesHoldDualRead pins the reader-first dual read of the reader
// hold (readerHold) that readerStates reads for each reader (S4, C13): one
// value reads alike from the ReaderHold variable table's wire, the padded
// standard base64 of that wire, the schema JSON text and today's legacy JSON,
// at the record and at the store read site; a damaged value of each form is
// refused by name and never read as a record; the store's writer still writes
// the legacy form; and reads are counted per record and per form on Check. It
// runs on the in-memory backend only (store.NewMem), with the clock at 1000.
func TestReaderStatesHoldDualRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	m := NewMem()
	st := &Store{
		B: m, Names: sprint.Names{Prefix: "reader-hold-dual-read-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	kv, err := st.rootKV()
	require.NoError(t, err)

	codec := st.readerHoldRows()
	require.Same(t, codec, st.readerHoldRows())
	pinned, err := st.pin(ctx)
	require.NoError(t, err)
	require.Same(t, codec, pinned.readerHoldRows(), "a pinned copy counts into the store's reader")

	hold := readerHold{
		Away: true, At: time.Unix(1001, 0).UTC(), By: "coordinator",
		Held: true, Reason: "limit", Return: true,
	}

	// Every form reads alike at the record (S2).
	f := rosterFormsOf(t, codec, hold)

	// The read site reads every form to one state (S2, S4).
	for name, in := range map[string][]byte{
		"binary": f.bin, "base64": f.b64, "json": f.js, "legacy": f.legacy,
	} {
		require.NoError(t, kv.SetKey(ctx, readerAwayKey(name), string(in)))
	}
	readers := []string{"binary", "base64", "json", "legacy"}
	got, err := st.ReaderStates(ctx, readers, now)
	require.NoError(t, err)
	for _, r := range readers {
		assert.Equal(t, sprint.ReaderHeld, got[r], r)
	}

	// The reads are counted per record and per form on Check (S4).
	report, _, err := st.Check(ctx, 1)
	require.NoError(t, err)
	require.Contains(t, report.RowForms, "READERHOLD")
	for form, n := range map[string]uint64{"binary": 2, "base64": 2, "json": 2, "legacy": 2} {
		assert.EqualValues(t, n, report.RowForms["READERHOLD"][form], form)
	}

	// The store's writer writes the ReaderHold table form, which the kept dual
	// reader reads back equal (S4: writers switched, readers kept).
	require.NoError(t, m.RowsAdd(ctx, st.Names.Table(sprint.Readers), []string{"writer"}))
	require.NoError(t, st.SetReaderAway(ctx, "writer", true, "coordinator"))
	raw, ok, err := kv.GetKey(ctx, readerAwayKey("writer"))
	require.NoError(t, err)
	require.True(t, ok)
	want := readerHold{Away: true, At: now, By: "coordinator"}
	stored, storedForm, err := codec.Read([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, schemaread.FormBinary, storedForm)
	assert.Equal(t, want, stored)

	// A damaged value of each form is refused by name, never read as a record.
	assertDamageRefused(t, codec, "READERHOLD", f)

	// A clamped value is refused by name. ReaderHold declares no bounded field,
	// so its wire and legacy text cannot clamp; the schema JSON reader clamps an
	// over-long key, which is the clamped value it refuses (S2).
	clamped := []byte(`{"` + strings.Repeat("k", 300) + `":1}` + "\n")
	assertRefused(t, codec, "READERHOLD", clamped, "json clamped")
	_, _, err = codec.Read(clamped)
	assert.EqualError(t, err, "READERHOLD REFUSED: clamped; run: nova-sprint check")

	// A damaged hold at the read site is counted as a refusal and reads as no
	// hold, which the next reader away or reader up replaces (readers.go).
	before := codec.Counts().Snapshot()
	require.NoError(t, kv.SetKey(ctx, readerAwayKey("broken"), string(f.bin[:len(f.bin)-1])))
	got, err = st.ReaderStates(ctx, []string{"broken"}, now)
	require.NoError(t, err)
	assert.Equal(t, sprint.ReaderDown, got["broken"], "a damaged hold reads as no hold, counted")
	after := codec.Counts().Snapshot()
	assert.EqualValues(t, before["binary"]+1, after["binary"], "the damaged hold's form is counted")
	refused := func(m map[string]uint64) uint64 {
		var n uint64
		for k, v := range m {
			if strings.HasPrefix(k, "refused_") {
				n += v
			}
		}
		return n
	}
	assert.Equal(t, refused(before)+1, refused(after), "the damaged hold's refusal is counted")
}
