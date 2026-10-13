package main

import (
	"encoding/json"
	"fmt"
	"strings"
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

func TestWhereNamesMalformedProviderOnceWithoutCallingItUnfunded(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.m.SetProviders(map[string]sprint.Provider{
		"broken":   {Name: "broken", Billing: "plan", Problem: "funded field is missing or empty"},
		"unfunded": {Name: "unfunded", Billing: "plan", Funded: false},
	})
	text := ta.ok("where")
	assert.Contains(t, text, "broken unknown (funded field is missing or empty; remedy=nova-config apply --kind provider)")
	assert.NotContains(t, text, "broken unfunded")
	assert.Equal(t, 1, strings.Count(text, "funded field is missing or empty"))
	var v whereView
	require.NoError(t, json.Unmarshal([]byte(ta.ok("where --json")), &v))
	require.Len(t, v.Providers, 2)
	assert.Nil(t, v.Providers[0].Funded)
	assert.NotEmpty(t, v.Providers[0].Problem)
	require.NotNil(t, v.Providers[1].Funded)
	assert.False(t, *v.Providers[1].Funded)
	assert.Empty(t, v.Providers[1].Problem)
	require.Len(t, v.ProviderGaps.InvalidProviders, 1)
	assert.Equal(t, "broken", v.ProviderGaps.InvalidProviders[0].Name)
	assert.Contains(t, text, providersLine(v.Providers, v.ProviderGaps)+"\n")
}

func TestStartRefusesChangedProviderFundingAfterCleanCheck(t *testing.T) {
	t.Parallel()
	ta := gateOn(t)
	ta.m.SetRoutes([]sprint.Route{{Name: "r", Tier: "heavy", Provider: "p", Enabled: true}})
	ta.m.SetTiers(map[string][]string{"heavy": {"r"}})
	ta.m.SetProviders(map[string]sprint.Provider{"p": {Name: "p", Billing: "plan", Funded: true}})
	ta.ok("check cards")
	ta.m.SetProviders(map[string]sprint.Provider{"p": {Name: "p", Billing: "plan", Funded: false}})
	code, out, errs := ta.do("start")
	assert.Equal(t, 1, code, out+errs)
	assert.Contains(t, errs, "CHECK routes tier:heavy holds no route of a funded provider")
	assert.Contains(t, errs, "nothing was written")
	assert.False(t, ta.machineRunning())
	ta.m.SetProviders(map[string]sprint.Provider{"p": {Name: "p", Billing: "plan", Funded: true}})
	ta.ok("check cards")
	ta.ok("start")
}

func TestFullCheckNamesNoProviderAndMalformedProvider(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.m.SetProviders(nil) // this case explicitly has no applied declarations
	ta.m.SetRoutes(nil)
	code, out, errs := ta.do("check cards")
	assert.Equal(t, 1, code, out+errs)
	assert.Contains(t, out, "CHECK routes - no provider rows applied remedy=nova-config apply --kind provider")
	ta.m.SetProviders(map[string]sprint.Provider{"broken": {Name: "broken", Problem: "funded field is invalid"}})
	code, out, errs = ta.do("check cards")
	assert.Equal(t, 1, code, out+errs)
	assert.Equal(t, 1, strings.Count(out, "CHECK routes provider:broken funded field is invalid"))
	assert.NotContains(t, out, "no provider rows applied")
	var object checkCardsOut
	require.NoError(t, json.Unmarshal([]byte(func() string { _, o, _ := ta.do("check cards --json"); return o }()), &object))
	require.Len(t, object.Findings, 1)
	assert.Equal(t, "routes", object.Findings[0].Kind)
}
