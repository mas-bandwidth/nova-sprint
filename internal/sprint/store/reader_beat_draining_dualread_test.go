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

// TestReaderBeatDrainingDualRead pins the reader-first dual read of the reader beat (S4, C13)
// at its second read site: the prior beat (sprint.Beat) ReaderBeatDraining reads to keep a
// drain's start (sprint.DrainingFrom) reads alike from the table wire, the padded standard
// base64 of that wire, the schema JSON text and today's legacy JSON, at
// internal/sprint/store/readers.go (ReaderBeatDraining), while the beat it writes stays the
// legacy form; a damaged or clamped value of each form is refused by name and never read as
// absent; reads are counted per form on the BEAT record, and a damaged prior beat at the read
// site is a counted refusal read as a first beat. It runs on the in-memory backend only
// (store.NewMem), with the clock at 1000.
func TestReaderBeatDrainingDualRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	since := now.Add(-100 * time.Second)
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "reader-beat-draining-dual-read-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	names := []string{"bin", "b64", "js", "legacy", "damaged"}
	require.NoError(t, st.B.(*Mem).RowsAdd(ctx, st.Names.Table(sprint.Readers), names))
	kv, err := st.rootKV()
	require.NoError(t, err)
	rows := st.presenceRows()
	all := func() map[string]uint64 { return rows.beat.Counts().Snapshot() }

	// the prior beat drains since 900: a read of it keeps that start, a first beat starts now
	prev := sprint.Beat{At: now.Add(-5 * time.Second), Load: 10, Cores: 4, How: "cpu", Draining: true, DrainingSince: since}
	f := rosterFormsOf(t, rows.beat, prev)
	written := func(reader string) sprint.Beat {
		t.Helper()
		raw, ok, err := kv.GetKey(ctx, readerBeatKey(reader))
		require.NoError(t, err)
		require.True(t, ok)
		var b sprint.Beat
		require.NoError(t, json.Unmarshal([]byte(raw), &b), "the beat written stays legacy JSON")
		return b
	}
	// stateReads is what ReaderStates alone adds to the counts for one reader, so the
	// read site's own read is measured apart from it
	stateReads := func(reader string) map[string]uint64 {
		t.Helper()
		before := all()
		_, err := st.ReaderStates(ctx, []string{reader}, now)
		require.NoError(t, err)
		return countsDelta(before, all())
	}

	// Each form of the prior beat reads alike at the read site (S2): the drain's start is kept.
	for name, tc := range map[string]struct {
		in   []byte
		form string
	}{"bin": {f.bin, "binary"}, "b64": {f.b64, "base64"}, "js": {f.js, "json"}, "legacy": {f.legacy, "legacy"}} {
		require.NoError(t, kv.SetKey(ctx, readerBeatKey(name), string(tc.in)))
		states := stateReads(name)
		before := all()
		wrote, err := st.ReaderBeatDraining(ctx, name, "", true)
		require.NoError(t, err, name)
		require.True(t, wrote, name)
		delta := countsDelta(before, all())
		// Reads are counted per form under BEAT (S4): the read site reads one of its form.
		assert.EqualValues(t, states[tc.form]+1, delta[tc.form], name+": the read site's read is counted under "+tc.form)
		assert.Zero(t, refusedTotal(delta), name)
		b := written(name)
		assert.True(t, b.Draining, name)
		assert.True(t, since.Equal(b.DrainingSince), "%s: the prior beat's drain start is kept, got %v", name, b.DrainingSince)
	}

	// A damaged or clamped value of each form is refused by name, never read as none; the
	// codec's read is the read the read site makes.
	assertDamageRefused(t, rows.beat, "BEAT", f)
	assertRefused(t, rows.beat, "BEAT", []byte("{\"cores\": -99999999999999999999}\n"), "beat json clamped")
	assertRefused(t, rows.beat, "BEAT", []byte(`{"cores":-99999999999999999999}`), "beat legacy clamped")

	// A damaged prior beat at the read site is a first beat, on purpose (S4): its refusal is
	// counted by name, and the beat written replaces it with a drain that starts now.
	for _, in := range [][]byte{f.bin[:len(f.bin)-1], f.b64[:len(f.b64)-2], f.legacy[:len(f.legacy)-1]} {
		require.NoError(t, kv.SetKey(ctx, readerBeatKey("damaged"), string(in)))
		states := stateReads("damaged")
		before := all()
		wrote, err := st.ReaderBeatDraining(ctx, "damaged", "", true)
		require.NoError(t, err)
		require.True(t, wrote)
		delta := countsDelta(before, all())
		assert.Equal(t, refusedTotal(states)+1, refusedTotal(delta), "the damaged prior beat's refusal is counted")
		b := written("damaged")
		assert.True(t, b.Draining)
		assert.True(t, now.Equal(b.DrainingSince), "a damaged prior beat is a first beat, got %v", b.DrainingSince)
	}
}

// countsDelta is after minus before, per count name.
func countsDelta(before, after map[string]uint64) map[string]uint64 {
	d := map[string]uint64{}
	for k, v := range after {
		if v != before[k] {
			d[k] = v - before[k]
		}
	}
	return d
}
