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

// TestDoneRefusesLegacyJSON pins that a done page stored in the legacy JSON form
// is refused by name, never decoded and never rewritten (S4).
func TestDoneRefusesLegacyJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "done-legacy-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	kv, err := st.kv()
	require.NoError(t, err)

	stored, err := json.Marshal(DoneRecord{
		Epoch: st.epoch, Cursor: "legacy-cursor",
		Done: []sprint.Completion{{
			Row: "reader", Kind: sprint.DoneKindRead, Card: "card.r1.emma", At: time.Unix(1001, 0).UTC(),
		}},
	})
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(ctx, keyDoneRecord, string(stored)))

	got, pages, why := st.decodeDonePage(string(stored))
	assert.Equal(t, "legacy_json_retired", why)
	assert.Equal(t, DoneRecord{}, got)
	assert.Zero(t, pages)

	report, _, err := st.Check(ctx, 1)
	require.NoError(t, err)
	counts := report.RowForms["DONERECORD"]
	assert.EqualValues(t, 1, counts["legacy"])
	assert.EqualValues(t, 1, counts["refused_legacy_json_retired"])

	_, err = st.readDoneRecord(ctx, kv)
	require.NoError(t, err)
	after, ok, err := kv.GetKey(ctx, keyDoneRecord)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, string(stored), after, "the refused legacy page is left as stored")
}
