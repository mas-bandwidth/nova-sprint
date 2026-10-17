package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestViewRowsWhereDualRead is the dual-read acceptance for the where, stop,
// check and stuck rows (Aspect 8): each is read alike from its fixed binary
// image, the padded standard base64 of that image, and the legacy form it has
// today, while every writer still writes the legacy form; a fuzzed malformed
// value of each form is refused; reads are counted per record and per form.
// S2, S4.
func TestViewRowsWhereDualRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "dualread-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))

	// The four records register their readers on this Store; each counts zero
	// before the first read (S4).
	stuck := st.stuckRows()
	require.Equal(t, "STUCK", stuck.rowName())
	assert.Zero(t, stuck.Counts().Snapshot()["binary"])
	assert.Zero(t, stuck.Counts().Snapshot()["legacy"])
	check := st.checkRows()
	require.Equal(t, "CHECKRECORD", check.rowName())
	stops := st.stopsRows()
	require.Equal(t, "STOPSRECORD", stops.rowName())
	where := st.whereRows()
	require.Equal(t, "WHERERECORD", where.rowName())

	stuckVal := Stuck{Op: "op-1", Verb: "read", At: now, Seen: now, Why: "grace", Repaired: "ok", RepairedAt: now.Add(time.Second)}
	assertDualRead(t, stuck, stuckPage{rec: stuckVal}, stuckVal, `{"op":`)

	checkVal := CheckRecord{Epoch: 1, At: now, By: "tester", Clean: true,
		Streams: []string{"s1", "s2"}, Kinds: "", Findings: []string{"f1"},
		Cards:  []sprint.CheckedCard{{ID: "c1", Stream: "s1", Brief: "abc"}},
		Digest: "d1"}
	assertDualRead(t, check, checkPage{rec: checkVal}, checkVal, `{"epoch":`)

	stopsVal := StopsRecord{Epoch: 1, At: now,
		Seat:  sprint.SeatWaits{Judgments: 2, Overdue: 1, OldestID: "c1", OldestAt: now, Level: 1},
		Stops: []sprint.Stop{{Kind: "member-down", Subject: "row1", Since: now, What: "w", Effect: "e"}}}
	assertDualRead(t, stops, stopsPage{rec: stopsVal}, stopsVal, `{"epoch":`)

	// The where record's binary form is still the pending writer-switch
	// mapping; its legacy form reads through the codec today (S2, S4).
	whereVal := WhereRecord{Epoch: 1, Rev: 1, Held: 3, ReadsWaiting: 1}
	whereLegacy, err := json.Marshal(whereVal)
	require.NoError(t, err)
	page, _, err := where.Read(whereLegacy)
	require.NoError(t, err)
	assert.Equal(t, whereVal, page.rec)
	_, _, err = where.Read(whereSaveWireRec(whereVal))
	require.Error(t, err)
}

// assertDualRead pins one record's three forms: binary, base64 and legacy each
// read back equal, a truncated and a flipped-byte binary image and a malformed
// legacy text are refused, and the reads are counted under their forms.
func assertDualRead[T any](t *testing.T, codec *RowCodec[T], page T, record any, badLegacy string) {
	t.Helper()
	wire, err := codec.Write(&page, schemaread.FormBinary)
	require.NoError(t, err)
	require.NotEmpty(t, wire)
	b64 := base64.StdEncoding.EncodeToString(wire)

	got, _, err := codec.Read(wire)
	require.NoError(t, err)
	assert.Equal(t, page, got)

	got64, _, err := codec.Read([]byte(b64))
	require.NoError(t, err)
	assert.Equal(t, page, got64)

	legacy, err := json.Marshal(record)
	require.NoError(t, err)
	gotL, _, err := codec.Read(legacy)
	require.NoError(t, err)
	assert.Equal(t, page, gotL)

	trunc := wire[:len(wire)-1]
	_, _, err = codec.Read(trunc)
	require.Error(t, err)

	flipped := append([]byte(nil), wire...)
	flipped[len(flipped)-1] ^= 0xff
	_, _, err = codec.Read(flipped)
	require.Error(t, err)

	_, _, err = codec.Read([]byte(badLegacy))
	require.Error(t, err)
}
