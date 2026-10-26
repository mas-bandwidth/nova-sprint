package store

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/hostload"
	"github.com/stretchr/testify/require"
)

func TestBenchLeaseAutoPickIsOneStepOnTheLanesRecord(t *testing.T) {
	// This test pins the feature to the lease rather than the named-machine lock.
	// It sets a package variable, so it intentionally does not call t.Parallel.
	wasDisabled := sprint.LanesDisabled
	sprint.LanesDisabled = true
	t.Cleanup(func() { sprint.LanesDisabled = wasDisabled })

	h := newHarness(t)
	h.mu.Lock()
	h.now = time.Unix(1000, 0).UTC()
	h.mu.Unlock()
	for _, m := range []string{"m1", "m2", "m3", "m4", "m5"} {
		h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: m, Width: 2}))
	}
	h.mu.Lock()
	h.live = []string{"m1", "m2", "m3", "m4", "m5"}
	h.mu.Unlock()
	beat := func() {
		t.Helper()
		zero := 0.0
		for _, m := range []string{"m1", "m2", "m3", "m4", "m5"} {
			_, err := h.st.Beat(h.ctx, m, &zero, hostload.Source{NCPU: 16})
			require.NoError(t, err)
		}
	}
	beat()
	// m2 is present and beating but unavailable by its fleet hold. m1 is the
	// running server and is excluded as self.
	h.must(FleetStep(sprint.FleetReq{Op: "hold", Member: "m2", Who: "tester"}))
	allow := func(m string) bool { return m != "m5" }
	pick := func(who string) sprint.BenchPick {
		t.Helper()
		answer, err := h.st.LanePick(h.ctx, sprint.LaneGo, who, "m1", allow)
		require.NoError(t, err)
		return answer
	}
	first, second := pick("w1"), pick("w2")
	require.True(t, first.Granted, "%+v", first)
	require.True(t, second.Granted, "%+v", second)
	require.Equal(t, "m3", first.Machine, "self m1 and held m2 must not be picked")
	require.Equal(t, "m4", second.Machine, "a second lease spreads by committed/slots")
	third, fourth := pick("w3"), pick("w4")
	require.True(t, third.Granted, "%+v", third)
	require.True(t, fourth.Granted, "%+v", fourth)

	kv, err := h.st.rootKV()
	require.NoError(t, err)
	before, ok, err := kv.GetKey(h.ctx, laneKey(sprint.LaneGo))
	require.NoError(t, err)
	require.True(t, ok)
	wait := pick("waiter")
	require.False(t, wait.Granted)
	require.Equal(t, h.now.Add(sprint.LaneWaitFor), wait.RetryAt)
	require.Len(t, wait.Full, 2)
	require.Equal(t, 2, wait.Full[0].Busy, "full shows committed lease holders")
	after, ok, err := kv.GetKey(h.ctx, laneKey(sprint.LaneGo))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, before, after, "a WAIT with no expiry must not write any bytes")

	h.tick(time.Second)
	beat()
	renewed := pick("w1")
	require.True(t, renewed.Granted)
	require.True(t, renewed.Renewed)
	require.Equal(t, first.Machine, renewed.Machine)
	gave, err := h.st.LaneGiveAll(h.ctx, sprint.LaneGo, "w1")
	require.NoError(t, err)
	require.True(t, gave)
	freed := pick("w5")
	require.True(t, freed.Granted, "%+v", freed)

	h.tick(sprint.LaneHoldFor + time.Second)
	beat()
	expired := pick("w6")
	require.True(t, expired.Granted, "%+v", expired)
	require.ElementsMatch(t, []string{"w2", "w3", "w4", "w5"}, expired.Released)

	// Lease operations remain live while the separate named-machine lock is disabled.
	disabledPick := pick("disabled")
	require.True(t, disabledPick.Granted)
	disabledGive, err := h.st.LaneGiveAll(h.ctx, sprint.LaneGo, "disabled")
	require.NoError(t, err)
	require.True(t, disabledGive)

	// Invalid ids are rejected before any storage read.
	_, err = h.st.LanePick(h.ctx, "bad", "w", "m1", allow)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "lane kind"))
	_, err = h.st.LaneGiveAll(h.ctx, sprint.LaneGo, "bad/who/extra")
	require.Error(t, err)
}
