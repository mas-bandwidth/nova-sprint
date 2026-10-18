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
	require.Empty(t, why)
	assert.Equal(t, uint32(1), count)
	assert.Equal(t, want.Done, got.Done)

	_, _, why = st.decodeDonePage(`{"epoch":`)
	assert.Equal(t, "legacy_json_unreadable", why)

	report, _, err = st.Check(ctx, 1)
	require.NoError(t, err)
	counts := report.RowForms["DONERECORD"]
	assert.EqualValues(t, 1, counts["binary"])
	assert.EqualValues(t, 2, counts["legacy"])
	assert.EqualValues(t, 1, counts["refused_legacy_json_unreadable"])
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
	assert.Equal(t, "legacy-cursor", first.Cursor)
	// the reads as Check reports them: a page the process decoded before (another run of this
	// test) is a read too (doneReads)
	counts := pinned.doneReadCounts()
	assert.EqualValues(t, 1, counts["legacy"])
	assert.Zero(t, counts["binary"])

	second, err := pinned.readDoneRecord(ctx, kv)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	counts = pinned.doneReadCounts()
	assert.EqualValues(t, 1, counts["legacy"])
	assert.EqualValues(t, 1, counts["binary"])
}
