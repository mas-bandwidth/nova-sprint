package main

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The routes view carries the applied tier arrays the deal already reads. An
// empty array means every enabled route of that tier; a nonempty one excludes
// enabled routes not named by it (docs/SPEC-SPRINT.md, routes).
func TestRoutesJSONCarriesTheAppliedTierArray(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.m.SetRoutes([]sprint.Route{
		{Name: "pro-a", Tier: "pro", Provider: "subscription-codex", Model: "a", Enabled: true},
		{Name: "pro-b", Tier: "pro", Provider: "subscription-codex", Model: "b", Enabled: true},
	})
	for _, tc := range []struct {
		name string
		list []string
	}{
		{name: "selected", list: []string{"pro-b"}},
		{name: "all enabled", list: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ta.m.SetTiers(map[string][]string{"pro": tc.list})
			var view struct {
				Tiers      string              `json:"tiers"`
				TierRoutes map[string][]string `json:"tier_routes"`
				Routes     []sprint.RouteStat  `json:"routes"`
			}
			ta.json("routes", &view)
			require.Contains(t, view.TierRoutes, "pro")
			assert.Equal(t, tc.list, view.TierRoutes["pro"])
			assert.Len(t, view.Routes, 2, "the view still includes enabled routes outside the array")
			assert.NotEmpty(t, view.Tiers, "the existing summary field remains")
		})
	}
}
