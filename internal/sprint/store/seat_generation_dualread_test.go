package store

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSeatGenerationDualRead pins the reader-first dual read of the seat change record as
// seatGenerationOf reads it for its generation (S4, C13): the record reads alike from the
// table wire, the padded standard base64 of that wire, the schema JSON text and today's legacy
// JSON, while every writer still writes the legacy form; a damaged or clamped value of each
// form is refused by name and never read as absent, empty or zero; reads are counted per
// record and per form. It runs on the in-memory backend only (store.NewMem), with the clock
// at 1000.
func TestSeatGenerationDualRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "seat-generation-dual-read-"}, Actor: "tester",
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

	// No record reads as the first generation, never as absent or zero: the seat has not
	// moved since init, and a key read that found nothing carries ok false.
	gen, err := st.seatGeneration(ctx)
	require.NoError(t, err)
	assert.Equal(t, sprint.FirstSeatGeneration, gen, "no record reads as the first seat generation")
	gen, err = seatGenerationOf("", false)
	require.NoError(t, err)
	assert.Equal(t, sprint.FirstSeatGeneration, gen, "an absent key reads as the first seat generation")

	seat := sprint.SeatChange{
		Holder: "emma", Generation: 3, From: "liam", At: time.Unix(1001, 0).UTC(),
		By: "owner", Taken: true, ApprovedBy: "glenn", Reason: "rotate",
	}
	f := rosterFormsOf(t, rows.seat, seat)

	// The seat generation read site reads every form from keySeat.
	for i, in := range [][]byte{f.bin, f.b64, f.js, f.legacy} {
		require.NoError(t, kv.SetKey(ctx, keySeat, string(in)))
		gen, err := st.seatGeneration(ctx)
		require.NoError(t, err, "form %d", i)
		assert.Equal(t, seat.Generation, gen, "form %d", i)
	}

	// Reads are counted per record and per form (S4): each of the four forms is read once
	// by rosterFormsOf and once at the read site.
	counts := rows.seat.Counts().Snapshot()
	for _, form := range []string{"binary", "base64", "json", "legacy"} {
		assert.EqualValues(t, 2, counts[form], "SEATCHANGE "+form)
	}
	report, _, err := st.Check(ctx, 1)
	require.NoError(t, err)
	require.Contains(t, report.RowForms, "SEATCHANGE")
	for _, form := range []string{"binary", "base64", "json", "legacy"} {
		assert.GreaterOrEqual(t, report.RowForms["SEATCHANGE"][form], uint64(2), "SEATCHANGE "+form)
	}

	// Every writer still writes the legacy form: the seat record's writer is unchanged.
	legacy, err := seatRecord(&seat)
	require.NoError(t, err)
	assert.Equal(t, string(f.legacy), legacy, "the seat writer still emits today's legacy JSON")

	// A damaged value of each form is refused by name, never read as absent or zero.
	assertDamageRefused(t, rows.seat, "SEATCHANGE", f)

	// A clamped value is refused in each text form.
	assertRefused(t, rows.seat, "SEATCHANGE", []byte("{\"generation\": 99999999999999999999}\n"), "seat json clamped")
	assertRefused(t, rows.seat, "SEATCHANGE", []byte(`{"generation":99999999999999999999}`), "seat legacy clamped")

	// Damage at the read site is handed to the caller by name, never a zero generation.
	before := rows.seat.Counts().Snapshot()
	require.NoError(t, kv.SetKey(ctx, keySeat, string(f.bin[:len(f.bin)-1])))
	_, err = st.seatGeneration(ctx)
	require.Error(t, err)
	assert.Regexp(t, `^SEATCHANGE REFUSED: [a-z_]+( \([a-z_]+\))?; run: nova-sprint check$`, err.Error())
	after := rows.seat.Counts().Snapshot()
	assert.EqualValues(t, before["binary"]+1, after["binary"], "the damaged wire's read is counted")
	var refusedBefore, refusedAfter uint64
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
