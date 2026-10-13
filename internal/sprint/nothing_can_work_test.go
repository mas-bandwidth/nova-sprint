package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nothingSnapshot(now time.Time, proLive bool, withWork bool) *Snapshot {
	work := NewTable(Work)
	work.SetRows([]string{"s1"})
	if withWork {
		work.Put(&Card{ID: "p1", Row: "s1", Col: Ready, Fields: map[string]string{"kind": "primary", "stream": "s1", "attempt": "1", FieldTierNow: "pro", "brief": "c: one (s1) tier: pro\n\nTask.\n"}})
		work.Put(&Card{ID: "p2", Row: "s1", Col: Ready, Fields: map[string]string{"kind": "primary", "stream": "s1", "attempt": "1", FieldTierNow: "pro", "brief": "c: two (s1) tier: pro\n\nTask.\n"}})
		work.Put(&Card{ID: "h1", Row: "s1", Col: Ready, Fields: map[string]string{"kind": "primary", "stream": "s1", "attempt": "1", FieldTierNow: "heavy", "brief": "c: three (s1) tier: heavy\n\nTask.\n"}})
	}
	fleet := NewTable(Fleet)
	credit := RouteRest{At: now.Add(-time.Hour), Until: OpenUntil, Cause: RestCredit, Cards: []string{"x1"}, Why: "out of credit: provider deepseek refused"}
	auth := RouteRest{At: now.Add(-time.Hour), Until: OpenUntil, Cause: RestAuth, Cards: []string{"x2"}, Why: "auth refused"}
	seat := RouteRest{At: now.Add(-time.Hour), Until: now.Add(time.Hour), Cause: RestCoordinator, Why: "rested by the seat"}
	fleet.SetProp(PropProviderRest("deepseek"), credit.value())
	if !proLive {
		fleet.SetProp(PropProviderRest("opencodepro"), auth.value())
	}
	fleet.SetProp(PropProviderRest("heavyprov"), seat.value())
	routes := []Route{
		{Name: "flash-a", Tier: "flash", Provider: "flashprov", Enabled: true},
		{Name: "deepseek-direct", Tier: "pro", Provider: "deepseek", Enabled: true},
		{Name: "pro-auth", Tier: "pro", Provider: "opencodepro", Enabled: true},
		{Name: "heavy-a", Tier: "heavy", Provider: "heavyprov", Enabled: true},
	}
	return &Snapshot{Now: now, Work: work, Fleet: fleet, Merge: NewTable(Merge), Readers: NewTable(Readers), Routes: routes,
		Tiers: map[string][]string{"flash": {"flash-a"}, "pro": {"deepseek-direct", "pro-auth"}, "heavy": {"heavy-a"}}, Coordinator: "coordinator"}
}

func TestEveryWorkedTierUnfundedStopsTheSprint(t *testing.T) {
	t.Parallel()
	now := time.Date(2030, 1, 2, 12, 0, 0, 0, time.UTC)
	s := nothingSnapshot(now, false, true)
	conds, stop := providerConds(s)
	require.NotEmpty(t, stop, "every worked tier unfunded stops")
	assert.Contains(t, stop, NothingFundedCause)
	assert.Contains(t, stop, "sprint stopped: nothing funded can work")
	assert.Contains(t, stop, "pro")
	assert.Contains(t, stop, "heavy")
	assert.Contains(t, stop, "deepseek-direct")
	assert.Contains(t, stop, "out of credit")
	assert.Contains(t, stop, "pro-auth")
	assert.Contains(t, stop, "auth refused")
	assert.Contains(t, stop, "heavy-a")
	assert.Contains(t, stop, "rested by the seat")
	assert.NotContains(t, stop, "flash-a")
	found := false
	for _, c := range conds {
		if c.typ == NNothingFundedCanWork {
			found = true
			assert.Contains(t, c.what, "pro")
		}
	}
	assert.True(t, found, "one sprint-level judgment")
}

func TestALiveWorkedTierKeepsTheSprintRunning(t *testing.T) {
	t.Parallel()
	now := time.Date(2030, 1, 2, 12, 0, 0, 0, time.UTC)
	s := nothingSnapshot(now, true, true)
	_, stop := providerConds(s)
	assert.Empty(t, stop)
}

func TestNoWorkNoStop(t *testing.T) {
	t.Parallel()
	now := time.Date(2030, 1, 2, 12, 0, 0, 0, time.UTC)
	s := nothingSnapshot(now, false, false)
	_, stop := providerConds(s)
	assert.Empty(t, stop)
}
