package store

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Reproduce the pre-advance clear paused while START holds the old fence and
// has passed its restore guard. Advance must not cross that owned fence.
// The exact calls are injected at pause points; no sleep or scheduler guess.
func scenarioStartGuardRacingAdvance(t *testing.T, h *harness) {
	upMembers(h)
	es, err := h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	var advanced bool
	st := *h.st
	st.afterStartGuard = func() {
		var err error
		advanced, err = st.B.AdvanceEpoch(h.ctx, es.N, h.now)
		require.NoError(t, err)
		if advanced {
			require.NoError(t, st.B.SettleEpoch(h.ctx, es.N+1))
		}
	}
	_, _, _, err = st.SetMachine(h.ctx, true)
	if advanced {
		assert.Error(t, err, "a START whose epoch changed must refuse")
	} else {
		require.NoError(t, err)
	}
	assert.False(t, advanced, "clear must not advance through START's machine fence")
	current, err := h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	m, _, err := h.st.Machine(h.ctx)
	require.NoError(t, err)
	pending, err := h.st.RestorePending(h.ctx)
	require.NoError(t, err)
	assert.False(t, m.Running() && pending, "RUNNING must never coexist with restore-pending")
	if !advanced {
		assert.Equal(t, es.N, current.N)
		assert.True(t, m.Running())
	}
}

// START completely wins after clear's first STOP. Clear's final fenced STOP
// must stop that winner before exposing the next restore-pending epoch.
func scenarioCompletedStartBeforeClearAdvance(t *testing.T, h *harness) {
	upMembers(h)
	st := *h.st
	st.beforeAdvance = func() { _, _, _, err := h.st.SetMachine(h.ctx, true); require.NoError(t, err) }
	st.beforeRestore = func(*Store) error { return errors.New("crash before restoring rows") }
	_, err := st.Clear(h.ctx)
	require.Error(t, err)
	pending, err := h.st.RestorePending(h.ctx)
	require.NoError(t, err)
	require.True(t, pending)
	m, _, err := h.st.Machine(h.ctx)
	require.NoError(t, err)
	assert.False(t, m.Running())
	// The old fence was released even after advance, so recovery can finish.
	_, err = h.st.Clear(h.ctx)
	require.NoError(t, err)
	pending, err = h.st.RestorePending(h.ctx)
	require.NoError(t, err)
	assert.False(t, pending)
}

func TestStartGuardAndClearAdvanceShareFence(t *testing.T) {
	t.Parallel()
	scenarioStartGuardRacingAdvance(t, newHarness(t))
}
func TestClearStopsAnInterveningCompletedStart(t *testing.T) {
	t.Parallel()
	scenarioCompletedStartBeforeClearAdvance(t, newHarness(t))
}

// A machine write whose fence owner retired with its lock — a transition or a
// noop write under a RUNNING machine — is refused with errFenceMoved and
// changes nothing.
func TestMachineWritesRequireTheirFenceOwner(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		noop bool // a noop write under a RUNNING machine, beside a transition
		lock string
	}{
		{"a transition", false, "retired-start-lock"},
		{"a noop write", true, "retired-noop-lock"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			upMembers(h)
			if tc.noop {
				_, _, _, err := h.st.SetMachine(h.ctx, true)
				require.NoError(t, err)
			}
			st, err := h.st.Pinned(h.ctx)
			require.NoError(t, err)
			f, err := st.B.ReadFence(h.ctx)
			require.NoError(t, err)
			lock := OpRecord{ID: tc.lock, Verb: "machine transition lock", Lock: true, At: h.now}
			ok, err := st.B.Acquire(h.ctx, f.Gen, lock)
			require.NoError(t, err)
			require.True(t, ok)
			require.NoError(t, st.B.Release(h.ctx, lock, false))
			before, _, err := st.Machine(h.ctx)
			require.NoError(t, err)
			view := h.viewState()
			_, _, err = st.machineRecord(h.ctx, before, h.snap(), true, st.Actor, "", time.Time{}, lock.ID)
			require.ErrorIs(t, err, errFenceMoved)
			after, _, err := st.Machine(h.ctx)
			require.NoError(t, err)
			assert.Equal(t, before, after)
			if tc.noop {
				assert.Equal(t, view, h.viewState())
			}
		})
	}
}

func scenarioAdvanceHonorsFromEpochFence(t *testing.T, h *harness) {
	upMembers(h)
	// Test a nonzero FROM so a root backend checking only epoch 0 cannot pass.
	_, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	st, err := h.st.Pinned(h.ctx)
	require.NoError(t, err)
	require.Greater(t, st.epoch, uint64(0))
	f, err := st.B.ReadFence(h.ctx)
	require.NoError(t, err)
	lock := OpRecord{ID: "from-epoch-lock", Verb: "clear advance lock", Lock: true, At: h.now}
	ok, err := st.B.Acquire(h.ctx, f.Gen, lock)
	require.NoError(t, err)
	require.True(t, ok)
	for _, owners := range [][]string{nil, {"foreign-owner"}} {
		ok, err = st.root.AdvanceEpoch(h.ctx, st.epoch, h.now, owners...)
		require.NoError(t, err)
		assert.False(t, ok)
	}
	es, err := h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, st.epoch, es.N)
	ok, err = st.root.AdvanceEpoch(h.ctx, st.epoch, h.now, lock.ID)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, st.B.Release(h.ctx, lock, false), "release the OLD epoch fence after advancing")
	es, err = h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, st.epoch+1, es.N)
}
func TestAdvanceHonorsFromEpochFence(t *testing.T) {
	t.Parallel()
	scenarioAdvanceHonorsFromEpochFence(t, newHarness(t))
}

// A writer resumed after its old lock was retired must not claim the epoch
// that a clear advanced meanwhile, even if it passed its guard before pausing.
func scenarioStartRefusesAfterEpochMoved(t *testing.T, h *harness) {
	upMembers(h)
	st := *h.st
	st.afterStartGuard = func() {
		pinned, err := h.st.Pinned(h.ctx)
		require.NoError(t, err)
		f, err := pinned.B.ReadFence(h.ctx)
		require.NoError(t, err)
		require.NotNil(t, f.Pending)
		require.NoError(t, pinned.B.Release(h.ctx, *f.Pending, false), "retire the paused owner's old lock")
		clear := *h.st
		clear.beforeRestore = func(*Store) error { return errors.New("crash before restoring new epoch") }
		_, err = clear.Clear(h.ctx)
		require.Error(t, err)
		pending, err := h.st.RestorePending(h.ctx)
		require.NoError(t, err)
		require.True(t, pending)
	}
	_, _, _, err := st.SetMachine(h.ctx, true)
	assert.Error(t, err, "a stale START must refuse instead of writing in the new epoch")
	m, _, err := h.st.Machine(h.ctx)
	require.NoError(t, err)
	pending, err := h.st.RestorePending(h.ctx)
	require.NoError(t, err)
	require.True(t, pending)
	assert.False(t, m.Running(), "a stale start may not write RUNNING while restore-pending")
}
func TestStartRefusesAfterEpochMoved(t *testing.T) {
	t.Parallel()
	scenarioStartRefusesAfterEpochMoved(t, newHarness(t))
}
