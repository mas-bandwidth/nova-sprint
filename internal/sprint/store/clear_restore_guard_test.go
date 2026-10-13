package store

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// While an epoch's restore is pending (the flag set, the marker absent), no verb edits or
// removes a member's or a reader's row and no start lands, so the clear run again never
// restores over what the epoch changed; once the restore's marker is written the verbs
// work again, and the run again restores nothing (tla/ClearDefaults.tla EditRow, DeleteRow,
// RestoreDone, RerunKeepsEpochWrites; witness noguard).
func TestNoRowEditLandsWhileTheRestoreIsPending(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	upMembers(h)
	setTiersRaw(h, "reader-a", "pro")
	var advanced atomic.Bool
	h.m.Fail = func(p string) error {
		if p == "advance" {
			advanced.Store(true)
		}
		if advanced.Load() && p == "guarded-rows" {
			return errors.New("crashed at the restore's first write")
		}
		return nil
	}
	_, err := h.st.Clear(h.ctx)
	require.Error(t, err)
	h.m.Fail = nil

	pending, err := h.st.RestorePending(h.ctx)
	require.NoError(t, err)
	require.True(t, pending, "the crash was before the restore's marker")
	every := ""
	for what, err := range map[string]error{
		"set --tiers":   h.st.SetReaderTiers(h.ctx, []string{"reader-a"}, "heavy"),
		"reader add":    h.st.AddReaders(h.ctx, []string{"reader-z"}, &every),
		"reader remove": h.st.RemoveReaders(h.ctx, []string{"reader-b"}),
		"start":         func() error { _, _, _, err := h.st.SetMachine(h.ctx, true); return err }(),
	} {
		assert.ErrorIs(t, err, ErrRestorePending, "%s is refused while the restore is pending", what)
	}
	// the sync's and the tick's drops find nothing of the epoch to drop (no control card record
	// was written, no reader row): they delete nothing
	dropped, err := h.st.DropMembers(h.ctx, []string{"m1"})
	require.NoError(t, err)
	assert.Empty(t, dropped)
	require.NoError(t, h.st.DropReadersRetired(h.ctx, []string{"m2"}))

	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.True(t, res.Owed)
	pending, err = h.st.RestorePending(h.ctx)
	require.NoError(t, err)
	assert.False(t, pending)
	assert.Equal(t, "pro", readerTiersOf(h, "reader-a"), "the restore wrote the tiers the epoch did not hold")
	_, marked := h.snap().Fleet.Prop(sprint.PropMembersRestored)
	assert.True(t, marked, "the restore's marker is in the epoch")
	require.NoError(t, h.st.SetReaderTiers(h.ctx, []string{"reader-a"}, "heavy"), "after the restore the verb works")
	assert.Equal(t, "heavy", readerTiersOf(h, "reader-a"))
}

// An epoch with no flag (epoch 0, a completed clear) is never restore pending.
func TestAnEpochWithNoFlagIsNeverRestorePending(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	pending, err := h.st.RestorePending(h.ctx)
	require.NoError(t, err)
	assert.False(t, pending, "epoch 0")
	_, err = h.st.Clear(h.ctx)
	require.NoError(t, err)
	pending, err = h.st.RestorePending(h.ctx)
	require.NoError(t, err)
	assert.False(t, pending, "a completed clear")
}
