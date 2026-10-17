package store

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLaneStepWriteFixed pins the writer switch of the lanes record (sprint.Lanes,
// lane.go LaneStep, S4: writers second): LaneStep writes the record in the variable-table
// form (S1, S2) — the JSON object keyed by machine the reader keeps (presenceRowMap),
// each machine's value the schema JSON text of its Lane table — what it writes reads back
// equal through the dual reader, a value the table cannot hold is refused by name before
// a byte is written (never written in the legacy form as a fallback), and the legacy
// fixtures still read through the kept dual reader. It runs on the in-memory backend only
// (store.NewMem), with the clock at 1000.
func TestLaneStepWriteFixed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "lane-step-write-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	kv, err := st.rootKV()
	require.NoError(t, err)
	rows := st.lanesRows()

	// A take writes the record in the table form: each machine's value is the schema JSON
	// text of its Lane table, not today's legacy JSON.
	_, err = st.LaneStep(ctx, sprint.LaneGo, "m1", "a", false)
	require.NoError(t, err)
	want := sprint.Lanes{"m1": {
		Holders: []sprint.LaneHold{{Who: "a", Since: now, Seen: now, Claimed: true}},
		Queue:   []sprint.LaneWait{},
	}}
	raw, ok, err := kv.GetKey(ctx, laneKey(sprint.LaneGo))
	require.NoError(t, err)
	require.True(t, ok)
	var vals map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &vals))
	wantLane := want["m1"]
	tableText, err := rows.Write(&wantLane, schemaread.FormJSON)
	require.NoError(t, err)
	require.Contains(t, vals, "m1")
	var wantVal, gotVal any
	require.NoError(t, json.Unmarshal(tableText, &wantVal))
	require.NoError(t, json.Unmarshal(vals["m1"], &gotVal))
	assert.Equal(t, wantVal, gotVal, "the store holds the Lane table's schema JSON text")

	// The record reads back equal through the dual reader.
	got, _, err := st.lanes(ctx, sprint.LaneGo)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	// A give rewrites the record and still reads back equal.
	_, err = st.LaneStep(ctx, sprint.LaneGo, "m1", "a", true)
	require.NoError(t, err)
	got, _, err = st.lanes(ctx, sprint.LaneGo)
	require.NoError(t, err)
	assert.Equal(t, sprint.Lanes{}, got, "a give removes the lane and leaves no record")

	// The dual reader still reads a legacy fixture of the same record (S4: the legacy
	// form is read, never written).
	legacyRaw, err := json.Marshal(want)
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(ctx, laneKey(sprint.LaneGo), string(legacyRaw)))
	got, _, err = st.lanes(ctx, sprint.LaneGo)
	require.NoError(t, err)
	assert.Equal(t, want["m1"].Holders, got["m1"].Holders, "a legacy fixture reads back equal")
	assert.Empty(t, got["m1"].Queue)

	// A value the table cannot hold (a time the table text does not read back) is refused
	// by name before a byte is written, never written in the legacy form as a fallback.
	bad := sprint.Lanes{"m9": {
		Holders: []sprint.LaneHold{{Who: "w", Since: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}},
	}}
	err = st.writeLanes(ctx, kv, "rust", bad)
	require.Error(t, err)
	assert.Regexp(t, regexp.MustCompile(`^LANES REFUSED: [a-z_]+; run: nova-sprint check$`), err.Error())
	_, keyOk, err := kv.GetKey(ctx, laneKey("rust"))
	require.NoError(t, err)
	assert.False(t, keyOk, "a refused value writes no byte")
}
