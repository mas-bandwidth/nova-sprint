package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The check record is the epoch's (start-refuses-until-a-clean-check): written and read back
// in its epoch, a failed run over a clean one, and none after the epoch advances, as a clear
// leaves it (tla/MachineReady.tla ClearAdvance, recFresh; the reversed witness clearkeeps).
func TestTheCheckRecordIsTheEpochs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	m := NewMem()
	st := &Store{B: m, Now: func() time.Time { return at }}
	require.NoError(t, st.Init(ctx))

	_, ok, err := st.CardCheck(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "no record before the first check")

	cards := []sprint.CheckedCard{{ID: "a1", Stream: "s1", Brief: "00"}}
	clean, err := st.PutCardCheck(ctx, CheckRecord{At: at, By: "coordinator", Clean: true, Cards: cards, Digest: sprint.CardsDigest(cards)})
	require.NoError(t, err)
	got, ok, err := st.CardCheck(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, clean, got)
	assert.Equal(t, uint64(0), got.Epoch)

	// a failed run overwrites the clean one, its findings cut to the bound
	lines := make([]string, MaxCheckFindings+5)
	for i := range lines {
		lines[i] = "CHECK lint a1 fails the card lint remedy=nova-sprint brief a1"
	}
	_, err = st.PutCardCheck(ctx, CheckRecord{At: at, By: "coordinator", Kinds: "lint:205", Findings: lines, Cards: cards, Digest: sprint.CardsDigest(cards)})
	require.NoError(t, err)
	got, ok, err = st.CardCheck(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	assert.False(t, got.Clean)
	assert.Equal(t, "lint:205", got.Kinds)
	assert.Len(t, got.Findings, MaxCheckFindings)

	// the epoch advances (a clear): the old record reads as none
	advanced, err := m.AdvanceEpoch(ctx, 0, at.Add(time.Minute))
	require.NoError(t, err)
	require.True(t, advanced)
	next := &Store{B: m, Now: func() time.Time { return at }}
	_, ok, err = next.CardCheck(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "another epoch's record is no record")

	_, ok = CheckRecordOf(`{"epoch":1,"clean":true}`, true, 1)
	assert.True(t, ok)
	_, ok = CheckRecordOf(`not json`, true, 1)
	assert.False(t, ok, "an unreadable record is none")
}
