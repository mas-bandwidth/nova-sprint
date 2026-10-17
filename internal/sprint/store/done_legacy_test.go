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

// TestDoneRefusesLegacyJSON pins that the done record's legacy JSON form is
// retired (S4): a page whose first byte is '{' is recognised and counted, but
// refused by name as legacy_json_retired, never decoded and never rewritten.
func TestDoneRefusesLegacyJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "done-legacy-retired-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	pinned, err := st.pin(ctx)
	require.NoError(t, err)
	kv, err := pinned.kv()
	require.NoError(t, err)

	legacy, err := json.Marshal(DoneRecord{
		Epoch:  pinned.epoch,
		Cursor: "legacy-cursor",
		Done: []sprint.Completion{{
			Row: "reader", Kind: sprint.DoneKindRead, Card: "card.r1.emma", At: time.Unix(1001, 0).UTC(),
		}},
	})
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(ctx, keyDoneRecord, string(legacy)))

	dec, _, why := pinned.decodeDonePage(string(legacy))
	assert.Equal(t, "legacy_json_retired", why)
	assert.Equal(t, DoneRecord{}, dec)

	report, _, err := st.Check(ctx, 1)
	require.NoError(t, err)
	counts := report.RowForms["DONERECORD"]
	require.Contains(t, counts, "refused_legacy_json_retired")
	assert.EqualValues(t, 1, counts["refused_legacy_json_retired"])

	got, err := pinned.readDoneRecord(ctx, kv)
	require.NoError(t, err)
	assert.Equal(t, DoneRecord{Epoch: pinned.epoch}, got)

	stored, ok, err := kv.GetKey(ctx, keyDoneRecord)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, string(legacy), stored, "a legacy page is left as stored, never rewritten")
}
