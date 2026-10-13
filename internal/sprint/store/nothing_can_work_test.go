package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

func TestTheStoreStopsAndStartRefusesUntilAWorkedTierIsLive(t *testing.T) {
	t.Parallel()
	routes := []sprint.Route{
		providerRoute("flash-a", "flash", "flashprov"),
		providerRoute("deepseek-direct", "pro", "deepseek"),
		providerRoute("pro-auth", "pro", "opencodepro"),
		providerRoute("heavy-a", "heavy", "heavyprov"),
	}
	h := routeHarness(t, routes...)
	h.m.SetTiers(map[string][]string{"flash": {"flash-a"}, "pro": {"deepseek-direct", "pro-auth"}, "heavy": {"heavy-a"}})
	h.addReady("s1", 2, briefOf("pro", ""))
	h.addReady("s1", 1, briefOf("heavy", ""))
	for _, p := range []string{"deepseek", "opencodepro", "heavyprov"} {
		h.must(RouteRestStep(sprint.RouteRestReq{Target: p, Reason: "rested for the test", Who: "boss"}))
	}
	h.startMachine()
	res := h.machine()
	m := h.machineRecord()
	assert.False(t, m.Running(), "the tick stopped the machine: %v", res)
	assert.Equal(t, sprint.NothingFundedCause, m.Cause)
	assert.Equal(t, "machine: STOPPED (nothing funded can work)", MachineLine(h.now, m, Heartbeat{}))
	assert.Contains(t, res.Halted, "pro")
	assert.Contains(t, res.Halted, "heavy")

	why, err := h.st.NothingFunded(h.ctx)
	require.NoError(t, err)
	assert.Contains(t, why, sprint.NothingFundedCause, "a start is refused with this line")
	assert.Contains(t, why, "pro")

	// two simulated hours stopped: no deadline, stale or overdue judgment is raised
	for range 4 {
		h.tick(30 * time.Minute)
		h.machine()
	}
	assert.Empty(t, h.openOf(sprint.NWorkLate))
	assert.Empty(t, h.openOf(sprint.NStreamStale))
	assert.Empty(t, h.openOf(sprint.NOverdue))
	assert.False(t, h.machineRecord().Running())

	// one pro route's rest ends: a funded route is live, and the owner's start runs
	h.must(RouteWakeStep(sprint.RouteWakeReq{Target: "deepseek", Reason: "paid", Who: "boss"}))
	why, err = h.st.NothingFunded(h.ctx)
	require.NoError(t, err)
	assert.Empty(t, why)
	h.startMachine()
	assert.True(t, h.machineRecord().Running())
	assert.Len(t, h.noteWhats(sprint.NFundedRouteLiveAgain), 1, "one live-again note")
	h.machine()
}
