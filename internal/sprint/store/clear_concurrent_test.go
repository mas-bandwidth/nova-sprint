package store

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// Two clears at once, and a verb racing a clear (the cold read of #190 at 90b5ae7d): the
// restore's rows are written in one exchange with the check that the restore is still
// pending, and a verb's rows in one exchange with the check that it is not, both at the
// epoch they pinned (Backend.GuardedRows). The interleavings are made with pause hooks the
// store calls at the exact points (beforeRestore, beforeGuardedWrite): no sleep, no clock.
// tla/ClearDefaults.tla RestoreRow, RestoreSettled, EditRow, DeleteRow; witness
// nonatomicrestore. The scenarios run on Mem here and on Redis in the functional tier.

// scenarioTwoClearsOneRestore: an owed epoch with nothing restored; clear B reads the marker
// absent and pauses before its restore; clear A restores, writes the marker and stops before
// its defaults; reader-b is removed; B resumes. B writes nothing of the restore (the marker
// is there), finishes the defaults, and reader-b stays removed.
func scenarioTwoClearsOneRestore(t *testing.T, h *harness) {
	upMembers(h)
	advanceUntilRestore(t, h)
	epoch := epochOf(t, h)
	pending, err := h.st.RestorePending(h.ctx)
	require.NoError(t, err)
	require.True(t, pending)

	paused := 0
	b := *h.st
	b.beforeRestore = func(*Store) error {
		paused++
		if paused > 1 {
			return nil
		}
		a := *h.st
		a.beforeDefaults = func(*Store) error { return errors.New("clear A stops after its restore") }
		_, err := a.Clear(h.ctx)
		require.Error(t, err, "clear A stopped before its defaults")
		_, marked := h.snap().Fleet.Prop(sprint.PropMembersRestored)
		require.True(t, marked, "clear A wrote the restore's marker")
		require.NoError(t, removeReader(h, "reader-b"), "the restore is done: a reader may be removed")
		return nil
	}
	res, err := b.Clear(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, paused, "clear B paused once, between its read of the marker and its restore")
	assert.True(t, res.Owed)
	assert.Equal(t, epoch, epochOf(t, h), "no advance")
	s := h.snap()
	assert.NotContains(t, s.Readers.Rows(), "reader-b", "clear B never restores a row the epoch removed")
	assert.Contains(t, s.Readers.Rows(), "reader-a")
	assert.True(t, s.Fleet.HasRow("m1"))
	pending, err = h.st.RestorePending(h.ctx)
	require.NoError(t, err)
	assert.False(t, pending)
	es, err := h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	assert.False(t, es.DefaultsOwed, "clear B finished the defaults and cleared the flag")
}

func TestTwoClearsAtOnceRestoreOnce(t *testing.T) {
	t.Parallel()
	scenarioTwoClearsOneRestore(t, newHarness(t))
}

// scenarioAVerbRacingAClearWritesNothing: a verb pins the settled epoch, a clear advances
// and stops after its advance (the new epoch restore pending), then the verb's guarded
// exchange runs: it finds the sprint at another epoch and writes nothing, at either epoch.
func scenarioAVerbRacingAClearWritesNothing(t *testing.T, h *harness, verb func(h *harness) error) {
	upMembers(h)
	setTiersRaw(h, "reader-a", "pro")
	_, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	settled := epochOf(t, h)
	h.st.beforeGuardedWrite = func() {
		h.st.beforeGuardedWrite = nil
		advanceUntilRestore(t, h)
	}
	err = verb(h)
	require.ErrorIs(t, err, ErrClearedUnderVerb, "the verb's check and write are one exchange at the epoch it pinned")
	require.Equal(t, settled+1, epochOf(t, h))
	s := h.snap()
	assert.Empty(t, s.Readers.Rows(), "nothing was written at the new epoch, whose restore is pending")
	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.True(t, res.Owed)
	assert.Equal(t, "pro", readerTiersOf(h, "reader-a"), "the restore wrote the old epoch's tiers")
	assert.Contains(t, h.snap().Readers.Rows(), "reader-b")
}

func TestASetTiersRacingAClearWritesNothing(t *testing.T) {
	t.Parallel()
	scenarioAVerbRacingAClearWritesNothing(t, newHarness(t), func(h *harness) error {
		return h.st.SetReaderTiers(h.ctx, []string{"reader-a"}, "")
	})
}

func TestAReaderRemoveRacingAClearWritesNothing(t *testing.T) {
	t.Parallel()
	scenarioAVerbRacingAClearWritesNothing(t, newHarness(t), func(h *harness) error {
		return h.st.RemoveReaders(h.ctx, []string{"reader-b"})
	})
}

func TestAStartRacingAClearIsRefused(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	upMembers(h)
	_, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	h.st.beforeGuardedWrite = func() {
		h.st.beforeGuardedWrite = nil
		advanceUntilRestore(t, h)
	}
	_, _, _, err = h.st.SetMachine(h.ctx, true)
	require.Error(t, err, "a start whose epoch a clear left is refused under the machine lock")
	m, _, err := h.st.Machine(h.ctx)
	require.NoError(t, err)
	assert.False(t, m.Running(), "the machine did not start")
}

// An intentional empty tiers set after the restore is the epoch's own choice: a clear run
// again after a crash at the settle never writes the old tiers over it (the field is present
// and empty).
func TestAnEmptyTiersSetAfterTheRestoreSurvivesTheRerun(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	upMembers(h)
	setTiersRaw(h, "reader-a", "pro")
	crashAtSettle(h)
	require.Equal(t, "pro", readerTiersOf(h, "reader-a"))
	require.NoError(t, h.st.SetReaderTiers(h.ctx, []string{"reader-a"}, ""), "every tier, on purpose")
	_, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	tiers, ok := h.snap().Readers.Texts["reader-a"][sprint.ReaderTiers]
	assert.True(t, ok, "the field is present")
	assert.Equal(t, "", tiers, "the epoch's empty tiers stay")
}
