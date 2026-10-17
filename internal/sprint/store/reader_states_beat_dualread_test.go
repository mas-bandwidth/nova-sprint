package store

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReaderStatesBeatDualRead pins the reader-first dual read of the reader beat (S4, C13):
// the reader beat (sprint.Beat) readerStates reads for each reader reads alike from the table
// wire, the padded standard base64 of that wire, the schema JSON text and today's legacy JSON,
// at the read site internal/sprint/store/readers.go (readerStates), while every writer still
// writes the legacy form; a damaged or clamped value of each form is refused by name and never
// read as absent; reads are counted per form on the BEAT record. It runs on the in-memory
// backend only (store.NewMem), with the clock at 1000.
func TestReaderStatesBeatDualRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "reader-beat-dual-read-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	kv, err := st.rootKV()
	require.NoError(t, err)
	rows := st.presenceRows()
	require.Same(t, rows.beat, st.presenceRows().beat, "the beat reader is one per store")

	all := func() map[string]uint64 { return rows.beat.Counts().Snapshot() }
	for _, form := range []string{"binary", "base64", "json", "legacy"} {
		assert.Zero(t, all()[form])
	}
	assert.Zero(t, refusedTotal(all()))

	beat := sprint.Beat{At: now.Add(-5 * time.Second), Load: 10, Cores: 4, How: "cpu"}
	f := rosterFormsOf(t, rows.beat, beat)

	// The reader beat read site reads every form alike (S2): each form decodes to the same
	// fresh beat, so the reader is up.
	for name, in := range map[string][]byte{"bin": f.bin, "b64": f.b64, "js": f.js, "legacy": f.legacy} {
		require.NoError(t, kv.SetKey(ctx, readerBeatKey(name), string(in)))
	}
	states, err := st.ReaderStates(ctx, []string{"bin", "b64", "js", "legacy"}, now)
	require.NoError(t, err)
	for _, name := range []string{"bin", "b64", "js", "legacy"} {
		assert.Equal(t, sprint.ReaderUp, states[name], name+" reads the beat")
	}

	// Reads are counted per form under BEAT (S4): the form writes above read one each, and
	// the read site read one of each form.
	counts := all()
	for _, form := range []string{"binary", "base64", "json", "legacy"} {
		assert.EqualValues(t, 2, counts[form], form)
	}

	// A damaged value of each form is refused by name, never read as none; a clamped value is
	// refused. The codec's read is the same read the read site makes.
	assertDamageRefused(t, rows.beat, "BEAT", f)
	assertRefused(t, rows.beat, "BEAT", []byte("{\"cores\": -99999999999999999999}\n"), "beat json clamped")
	assertRefused(t, rows.beat, "BEAT", []byte(`{"cores":-99999999999999999999}`), "beat legacy clamped")

	// A damaged beat at the read site is no beat, on purpose (S2, S4): the refusal is counted
	// and the reader reads down, never as a zero-fresh up.
	before := all()
	beforeRefused := refusedTotal(before)
	require.NoError(t, kv.SetKey(ctx, readerBeatKey("damaged"), string(f.bin[:len(f.bin)-1])))
	states, err = st.ReaderStates(ctx, []string{"damaged"}, now)
	require.NoError(t, err)
	assert.Equal(t, sprint.ReaderDown, states["damaged"])
	after := all()
	assert.EqualValues(t, before["binary"]+1, after["binary"], "the damaged binary read is counted")
	assert.Equal(t, beforeRefused+1, refusedTotal(after), "the damaged beat's refusal is counted")
}

// refusedTotal sums a record's refused reads (refused_<reason>).
func refusedTotal(m map[string]uint64) uint64 {
	var total uint64
	for k, v := range m {
		if len(k) > 8 && k[:8] == "refused_" {
			total += v
		}
	}
	return total
}
