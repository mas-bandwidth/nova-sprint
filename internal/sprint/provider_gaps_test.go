package sprint

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func providerGapSnapshot() *Snapshot {
	s := &Snapshot{Providers: map[string]Provider{"opencode": {Name: "opencode", Billing: "credits", Funded: true}, "openrouter": {Name: "openrouter", Funded: false}}, Tiers: map[string][]string{"pro": {"oc-1", "or-1", "or-2"}}}
	for i := 1; i <= 7; i++ {
		s.Routes = append(s.Routes, Route{Name: fmt.Sprintf("oc-%d", i), Provider: "opencode", Tier: "pro", Enabled: true})
	}
	s.Routes = append(s.Routes, Route{Name: "or-1", Provider: "openrouter", Tier: "pro", Enabled: true}, Route{Name: "or-2", Provider: "openrouter", Tier: "pro", Enabled: true})
	return s
}

func TestAFundedRouteInNoTierArrayIsAFinding(t *testing.T) {
	t.Parallel()
	s := providerGapSnapshot()
	routes, tiers := s.ProviderGaps("")
	require.Len(t, routes, 6)
	for i, r := range routes {
		assert.Equal(t, RouteGap{Route: fmt.Sprintf("oc-%d", i+2), Provider: "opencode", Tier: "pro"}, r)
	}
	assert.Empty(t, tiers)
	routes, _ = s.ProviderGaps("heavy")
	assert.Empty(t, routes)
}

func TestAnEmptyArrayTakesEveryEnabledRoute(t *testing.T) {
	t.Parallel()
	s := providerGapSnapshot()
	s.Tiers["pro"] = nil
	routes, tiers := s.ProviderGaps("")
	assert.Empty(t, routes)
	assert.Empty(t, tiers)
}

func TestAnUnfundedProvidersRouteIsNoGap(t *testing.T) {
	t.Parallel()
	s := providerGapSnapshot()
	s.Providers["opencode"] = Provider{Name: "opencode", Funded: false}
	s.Routes = append(s.Routes, Route{Name: "disabled", Tier: "pro", Provider: "paid", Enabled: false}, Route{Name: "unapplied", Tier: "pro", Provider: "missing", Enabled: true})
	s.Providers["paid"] = Provider{Name: "paid", Funded: true}
	routes, _ := s.ProviderGaps("")
	assert.Empty(t, routes)
}

func TestATierWithNoFundedRouteIsAFinding(t *testing.T) {
	t.Parallel()
	s := providerGapSnapshot()
	s.Tiers["pro"] = nil
	s.Tiers["heavy"] = []string{"or-1"}
	routes, tiers := s.ProviderGaps("")
	assert.Empty(t, routes)
	assert.Equal(t, []string{"heavy"}, tiers)
}

func TestARouteInAnotherTiersArrayStillHasItsNativeTierGap(t *testing.T) {
	t.Parallel()
	s := providerGapSnapshot()
	s.Tiers["heavy"] = []string{"oc-2", "oc-3", "oc-4", "oc-5", "oc-6", "oc-7"}
	routes, tiers := s.ProviderGaps("")
	require.Len(t, routes, 6)
	for i, r := range routes {
		assert.Equal(t, RouteGap{Route: fmt.Sprintf("oc-%d", i+2), Provider: "opencode", Tier: "pro"}, r)
	}
	assert.Equal(t, []string{"heavy"}, tiers)
	rows := s.ProviderCoverages()
	require.Len(t, rows, 2)
	assert.Equal(t, []ProviderTier{{Tier: "pro", In: 1, Enabled: 7}}, rows[0].Tiers)
	assert.Equal(t, "providers: opencode funded (pro 1/7); openrouter unfunded WARN 6 funded routes in no tier array (check cards names them) WARN tiers heavy hold no route of a funded provider (check cards names them)", s.ProvidersLine())
	routes, tiers = s.ProviderGaps("pro")
	assert.Len(t, routes, 6)
	assert.Empty(t, tiers)
	routes, tiers = s.ProviderGaps("heavy")
	assert.Empty(t, routes)
	assert.Equal(t, []string{"heavy"}, tiers)
}

func TestProviderCoverageCountsEnabledRoutesOnce(t *testing.T) {
	t.Parallel()
	s := providerGapSnapshot()
	s.Tiers["pro"] = []string{"oc-1", "oc-1", "disabled"}
	s.Routes = append(s.Routes, Route{Name: "disabled", Tier: "pro", Provider: "opencode"})
	assert.Equal(t, "providers: opencode funded (pro 1/7); openrouter unfunded WARN 6 funded routes in no tier array (check cards names them)", s.ProvidersLine())
}

func TestAConfiguredEmptyTierWithoutFundedFallbackIsAFinding(t *testing.T) {
	t.Parallel()
	s := &Snapshot{Routes: []Route{{Name: "or-heavy", Tier: "heavy", Provider: "openrouter", Enabled: true}}, Providers: map[string]Provider{"openrouter": {Name: "openrouter", Funded: false}}, Tiers: map[string][]string{"heavy": nil}}
	routes, tiers := s.ProviderGaps("")
	assert.Empty(t, routes)
	assert.Equal(t, []string{"heavy"}, tiers)
}

func TestAnUnconfiguredFrontierAddsNoFundingFinding(t *testing.T) {
	t.Parallel()
	s := &Snapshot{Routes: []Route{{Name: "or-frontier", Tier: "frontier", Provider: "openrouter", Enabled: true}}, Providers: map[string]Provider{"openrouter": {Name: "openrouter", Funded: false}}}
	routes, tiers := s.ProviderGaps("")
	assert.Empty(t, routes)
	assert.Empty(t, tiers)
}

func TestAConfiguredEmptyTierWithFundedFallbackIsClean(t *testing.T) {
	t.Parallel()
	s := &Snapshot{Routes: []Route{{Name: "oc-heavy", Tier: "heavy", Provider: "opencode", Enabled: true}}, Providers: map[string]Provider{"opencode": {Name: "opencode", Funded: true}}, Tiers: map[string][]string{"heavy": nil}}
	routes, tiers := s.ProviderGaps("")
	assert.Empty(t, routes)
	assert.Empty(t, tiers)
	assert.Equal(t, "providers: opencode funded (heavy 1/1)", s.ProvidersLine())
}

func TestMalformedProviderCannotFundATier(t *testing.T) {
	t.Parallel()
	s := providerGapSnapshot()
	p := s.Providers["opencode"]
	p.Problem = "funded field is invalid"
	s.Providers["opencode"] = p
	routes, tiers := s.ProviderGaps("")
	assert.Empty(t, routes)
	assert.Equal(t, []string{"pro"}, tiers)
	assert.False(t, s.ProviderFunded("opencode"))
	require.Len(t, s.ProviderProblems(), 1)
}
