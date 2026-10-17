package store

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSeatChangeCoordinatorDualRead pins the reader-first dual read of the seat change record
// (S4, C13): the record SeatServerCoordinator reads as its first value reads alike from the
// table wire, the padded standard base64 of that wire, the schema JSON text and today's legacy
// JSON, while the writer still writes the legacy form; a damaged or clamped value of each form
// is refused by name and never read as none; reads are counted per record and per form on
// Check. It runs on the in-memory backend only (store.NewMem), with the clock at 1000.
func TestSeatChangeCoordinatorDualRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "seat-dual-read-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	kv, err := st.rootKV()
	require.NoError(t, err)
	rows := st.seatRows()
	require.Same(t, rows.seat, st.seatRows().seat)
	pinned, err := st.pin(ctx)
	require.NoError(t, err)
	require.Same(t, rows.seat, pinned.seatRows().seat, "a pinned copy counts into the store's readers")

	seat := sprint.SeatChange{
		Holder: "emma", Generation: 3, From: "liam", At: time.Unix(1001, 0).UTC(),
		By: "owner", Taken: true, ApprovedBy: "glenn", Reason: "rotate",
	}
	f := rosterFormsOf(t, rows.seat, seat)

	// The seat read site of SeatServerCoordinator reads every form from keySeat.
	for i, in := range [][]byte{f.bin, f.b64, f.js, f.legacy} {
		require.NoError(t, kv.SetKey(ctx, keySeat, string(in)))
		got, moved, _, _, _, err := st.SeatServerCoordinator(ctx)
		require.NoError(t, err, "form %d", i)
		assert.True(t, moved, "form %d", i)
		assert.Equal(t, seat, got, "form %d", i)
	}

	// Reads are counted per record and per form on Check (S4): four forms read twice.
	report, _, err := st.Check(ctx, 1)
	require.NoError(t, err)
	require.Contains(t, report.RowForms, "SEATCHANGE")
	for form, n := range map[string]uint64{
		"binary": 2, "base64": 2, "json": 2, "legacy": 2,
	} {
		assert.EqualValues(t, n, report.RowForms["SEATCHANGE"][form], "SEATCHANGE "+form)
	}

	// A damaged value of each form is refused by name, never read as none.
	assertDamageRefused(t, rows.seat, "SEATCHANGE", f)

	// A clamped value is refused: an integer past uint64 in the schema JSON text is clamped
	// by the table reader, and today's decoder refuses it in legacy text.
	assertRefused(t, rows.seat, "SEATCHANGE", []byte("{\"generation\": 99999999999999999999}\n"), "seat json clamped")
	assertRefused(t, rows.seat, "SEATCHANGE", []byte(`{"generation":99999999999999999999}`), "seat legacy clamped")

	// A damaged record is refused by name at the read site: SeatServerCoordinator hands the
	// refusal to the caller, never a zero record.
	before := rows.seat.Counts().Snapshot()
	require.NoError(t, kv.SetKey(ctx, keySeat, string(f.bin[:len(f.bin)-1])))
	_, _, _, _, _, err = st.SeatServerCoordinator(ctx)
	require.Error(t, err)
	assert.Regexp(t, `^SEATCHANGE REFUSED: [a-z_]+; run: nova-sprint check$`, err.Error())
	after := rows.seat.Counts().Snapshot()
	assert.EqualValues(t, before["binary"]+1, after["binary"], "the damaged wire's read is counted")
	refusedBefore, refusedAfter := uint64(0), uint64(0)
	for k, v := range before {
		if len(k) > 8 && k[:8] == "refused_" {
			refusedBefore += v
		}
	}
	for k, v := range after {
		if len(k) > 8 && k[:8] == "refused_" {
			refusedAfter += v
		}
	}
	assert.Equal(t, refusedBefore+1, refusedAfter, "the damaged wire's refusal is counted")
}
