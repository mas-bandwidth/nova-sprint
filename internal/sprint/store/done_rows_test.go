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

func TestDoneReadsCountPerForm(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "done-forms-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	zero := st.doneRows().Counts().Snapshot()
	assert.Zero(t, zero["binary"])
	assert.Zero(t, zero["legacy"])
	report, _, err := st.Check(ctx, 1)
	require.NoError(t, err)
	require.Contains(t, report.RowForms, "DONERECORD")
	assert.Zero(t, report.RowForms["DONERECORD"]["binary"])
	assert.Zero(t, report.RowForms["DONERECORD"]["legacy"])

	want := DoneRecord{Epoch: 7, Cursor: "cursor-7", Done: []sprint.Completion{{
		Row: "reader", Kind: sprint.DoneKindRead, Card: "card.r1.emma", At: time.Unix(1001, 0).UTC(),
	}}}
	pages, _, err := encodeDonePages(want)
	require.NoError(t, err)
	require.Len(t, pages, 1)
	got, count, why := st.decodeDonePage(pages[0])
	require.Empty(t, why)
	assert.Equal(t, uint32(1), count)
	assert.Equal(t, want.Epoch, got.Epoch)
	assert.Equal(t, want.Cursor, got.Cursor)
	assert.Equal(t, want.Done, got.Done)

	legacy, err := json.Marshal(want)
	require.NoError(t, err)
	got, count, why = st.decodeDonePage(string(legacy))
	assert.Equal(t, "legacy_json_retired", why)
	assert.Zero(t, count)
	assert.Equal(t, DoneRecord{}, got)

	_, _, why = st.decodeDonePage(`{"epoch":`)
	assert.Equal(t, "legacy_json_retired", why)

	report, _, err = st.Check(ctx, 1)
	require.NoError(t, err)
	counts := report.RowForms["DONERECORD"]
	assert.EqualValues(t, 1, counts["binary"])
	assert.EqualValues(t, 2, counts["legacy"])
	assert.EqualValues(t, 2, counts["refused_legacy_json_retired"])
}

func TestDoneRewriteCounted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	m := NewMem()
	st := &Store{
		B: m, Names: sprint.Names{Prefix: "done-rewrite-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	pinned, err := st.pin(ctx)
	require.NoError(t, err)
	kv, err := pinned.kv()
	require.NoError(t, err)

	codec := st.doneRows()
	require.Same(t, codec, pinned.doneRows())
	legacy, err := json.Marshal(DoneRecord{
		Epoch: pinned.epoch, Cursor: "legacy-cursor",
		Done: []sprint.Completion{{Row: "reader", Kind: sprint.DoneKindRead, Card: "card.r1.emma", At: time.Unix(1001, 0).UTC()}},
	})
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(ctx, keyDoneRecord, string(legacy)))

	first, err := pinned.readDoneRecord(ctx, kv)
	require.NoError(t, err)
	assert.Equal(t, DoneRecord{Epoch: pinned.epoch}, first)
	counts := codec.Counts().Snapshot()
	assert.EqualValues(t, 1, counts["legacy"])
	assert.Zero(t, counts["binary"])
	assert.EqualValues(t, 1, counts["refused_legacy_json_retired"])
	stored, ok, err := kv.GetKey(ctx, keyDoneRecord)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, string(legacy), stored, "a legacy page is left as stored, never rewritten")

	second, err := pinned.readDoneRecord(ctx, kv)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	counts = codec.Counts().Snapshot()
	assert.EqualValues(t, 2, counts["legacy"])
	assert.Zero(t, counts["binary"])
	assert.EqualValues(t, 2, counts["refused_legacy_json_retired"])
	stored, ok, err = kv.GetKey(ctx, keyDoneRecord)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, string(legacy), stored, "a second read leaves the legacy page as stored too")
}
