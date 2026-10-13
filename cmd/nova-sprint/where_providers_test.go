package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func providerCoverageApp(t *testing.T) *testApp {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.m.SetProviders(map[string]sprint.Provider{"opencode": {Name: "opencode", Billing: "both", Funded: true}, "openrouter": {Name: "openrouter", Billing: "metered", Funded: false}, "no-routes": {Name: "no-routes", Funded: true}})
	var routes []sprint.Route
	for i := 1; i <= 7; i++ {
		routes = append(routes, sprint.Route{Name: fmt.Sprintf("oc-%d", i), Provider: "opencode", Tier: "pro", Enabled: true})
	}
	ta.m.SetRoutes(routes)
	ta.m.SetTiers(map[string][]string{"pro": {"oc-1"}})
	return ta
}

func TestWhereShowsAppliedProviderCoverage(t *testing.T) {
	t.Parallel()
	ta := providerCoverageApp(t)
	text := ta.ok("where")
	assert.Contains(t, text, "providers: no-routes funded; opencode funded (pro 1/7); openrouter unfunded WARN 6 funded routes in no tier array (check cards names them)\n")
	var v whereView
	require.NoError(t, json.Unmarshal([]byte(ta.ok("where --json")), &v))
	require.Len(t, v.Providers, 3)
	require.Len(t, v.ProviderGaps.Routes, 6)
	assert.Equal(t, "both", v.Providers[1].Billing)
	require.NotNil(t, v.Providers[2].Funded)
	assert.False(t, *v.Providers[2].Funded)
	assert.Contains(t, text, providersLine(v.Providers, v.ProviderGaps)+"\n")
}

func TestCheckCardsNamesProviderGaps(t *testing.T) {
	t.Parallel()
	ta := providerCoverageApp(t)
	code, check, checkErr := ta.do("check cards")
	assert.Equal(t, 1, code)
	check += checkErr
	for i := 2; i <= 7; i++ {
		assert.Contains(t, check, fmt.Sprintf("CHECK routes oc-%d enabled route of funded provider opencode is in no tier array remedy=nova-config tier set pro --routes oc-1,oc-%d", i, i))
	}
	ta.m.SetTiers(map[string][]string{"pro": {"oc-1", "oc-2", "oc-3", "oc-4", "oc-5", "oc-6", "oc-7"}})
	assert.NotContains(t, ta.ok("where"), "WARN")
	// Other full-check kinds can still fail an otherwise empty fixture.
	_, out, errs := ta.do("check cards")
	assert.NotContains(t, out+errs, "CHECK routes")
}
