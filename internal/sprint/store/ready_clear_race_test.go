package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A start that lands inside a clear, between the clear's stop and its epoch advance,
// records the old epoch and runs on into the new one; a later stop of it says it ran
// there: STOPPED, never READY, so its due moves are judged and a second stop is not
// refused (docs/SPEC-SPRINT.md section 14, "READY"; tla/MachineReady.tla, the
// reversed witness nostopstamp). The interleaving is played step by step on the
// in-memory store: the clear's stop, the start, then the clear's advance.
func TestAStartInsideAClearThenStoppedIsStoppedNotReady(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SetMachine(h.ctx, true)
	require.NoError(t, err)
	h.tick(time.Second)

	// the clear's stop (clear.go: SetMachine false, before the advance)
	_, _, _, err = h.st.SetMachine(h.ctx, false)
	require.NoError(t, err)
	// a start lands before the advance: it records the epoch it read
	_, _, _, err = h.st.SetMachine(h.ctx, true)
	require.NoError(t, err)
	es, err := h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	// the clear's advance
	h.tick(time.Second)
	ok, err := h.m.AdvanceEpoch(h.ctx, es.N, h.now)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, h.m.SettleEpoch(h.ctx, es.N+1))

	m, _, err := h.st.Machine(h.ctx)
	require.NoError(t, err)
	require.True(t, m.Running(), "the start ran on into the new epoch: %+v", m)

	// a stop by hand: it ran in the new epoch, so it is STOPPED there
	h.tick(time.Second)
	_, after, _, err := h.st.StopUntil(h.ctx, "a bench", h.now.Add(time.Hour))
	require.NoError(t, err)
	require.False(t, after.Running())
	m, _, err = h.st.Machine(h.ctx)
	require.NoError(t, err)
	require.False(t, m.Ready(), "a machine that ran in this epoch and stopped is STOPPED, not READY: %+v", m)
	require.Contains(t, h.st.MachineLine(h.ctx), "STOPPED", "the machine line")
	_, _, _, err = h.st.StopUntil(h.ctx, "again", h.now.Add(2*time.Hour))
	require.NoError(t, err, "a second stop of a STOPPED machine is not refused as READY")
}
