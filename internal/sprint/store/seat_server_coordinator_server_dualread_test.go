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

// TestSeatServerCoordinatorServerDualRead pins the reader-first dual read of the server record
// as SeatServerCoordinator reads it (S4, C13): the record reads alike from the table wire, the
// padded standard base64 of that wire, the schema JSON text and today's legacy JSON, at the
// record and at the read site, while the writer still writes legacy; a damaged or clamped value
// of each form is refused by name, never read as absent, empty or zero; reads are counted per
// record and per form. It runs on the in-memory backend only (store.NewMem), with the clock at
// 1000.
func TestSeatServerCoordinatorServerDualRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "seat-server-dual-read-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	kv, err := st.rootKV()
	require.NoError(t, err)
	rows := st.serverRows()
	require.Same(t, rows, st.serverRows())

	want := ServerRecord{
		Actor: "coord", At: now.Add(time.Second), Version: "v1.5.0", Commit: "abc123def",
		Started: now.Add(-time.Second), Land: true, PID: 4242, Host: "box-1",
	}
	f := rosterFormsOf(t, rows, want)

	// rosterFormsOf read each form once through the codec (S2).
	counts := rows.Counts().Snapshot()
	assert.EqualValues(t, 1, counts["binary"])
	assert.EqualValues(t, 1, counts["base64"])
	assert.EqualValues(t, 1, counts["json"])
	assert.EqualValues(t, 1, counts["legacy"])

	seatLegacy, err := json.Marshal(sprint.SeatChange{Holder: "coord", Generation: 1, By: "coord", At: now})
	require.NoError(t, err)
	setServer := func(in []byte) {
		require.NoError(t, kv.SetKey(ctx, keySeat, string(seatLegacy)))
		require.NoError(t, kv.SetKey(ctx, keyServer, string(in)))
		require.NoError(t, kv.SetKey(ctx, keyCoordinator, "coord"))
	}

	// The read site reads every form of the server record (S2).
	for _, form := range []struct {
		in   []byte
		what string
	}{{f.bin, "binary"}, {f.b64, "base64"}, {f.js, "json"}, {f.legacy, "legacy"}} {
		setServer(form.in)
		_, _, got, fresh, coord, err := st.SeatServerCoordinator(ctx)
		require.NoError(t, err, form.what)
		assert.Equal(t, want, got, form.what)
		assert.True(t, fresh, form.what)
		assert.Equal(t, "coord", coord, form.what)
	}

	// Reads are counted per record and per form (S4): one from rosterFormsOf and one from
	// each read site.
	counts = rows.Counts().Snapshot()
	assert.EqualValues(t, 2, counts["binary"])
	assert.EqualValues(t, 2, counts["base64"])
	assert.EqualValues(t, 2, counts["json"])
	assert.EqualValues(t, 2, counts["legacy"])

	// The writer still writes the legacy form (S4: readers first).
	require.NoError(t, st.SetServer(ctx, want))
	raw, ok, err := kv.GetKey(ctx, keyServer)
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, LegacyObject([]byte(raw)), raw)

	// A damaged value of each form is refused by name, never read as none.
	assertDamageRefused(t, rows, "SERVERRECORD", f)

	// A damaged server record is refused by name at the read site.
	setServer(f.legacy[:len(f.legacy)-1])
	_, _, _, _, _, err = st.SeatServerCoordinator(ctx)
	assert.EqualError(t, err, "SERVERRECORD REFUSED: legacy_json_unreadable; run: nova-sprint check")

	// A clamped value is refused.
	_, _, err = rows.Read([]byte("{\"pid\": 99999999999999999999}\n"))
	assert.EqualError(t, err, "SERVERRECORD REFUSED: clamped; run: nova-sprint check")
	assertRefused(t, rows, "SERVERRECORD", []byte(`{"pid":99999999999999999999}`), "legacy clamped")
}
