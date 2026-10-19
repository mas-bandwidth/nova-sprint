package store

import (
	"encoding/json"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A work card on a route whose provider is a custom (non-catalogued) row carries
// that row's provider_base_url, provider_key_env and provider_npm_adapter in its
// packet, so the member can build opencode's custom provider block. A catalogued
// provider (empty row) or a provider with no row at all carries none: the three
// are filled together or not at all, and a key_env is a name, never resolved.
func TestAPacketCarriesItsRouteProvidersCustomRow(t *testing.T) {
	t.Parallel()
	custom := sprint.Provider{Name: "paperinstruments", Billing: "metered", Funded: true,
		BaseURL: "https://api.paperinstruments.example/v1", KeyEnv: "PAPERINSTRUMENTS_API_KEY", NPMAdapter: "@ai-sdk/openai-compatible"}
	catalogued := sprint.Provider{Name: "opencode", Billing: "credits", Funded: true}
	h := newHarness(t)
	h.m.SetRoutes([]sprint.Route{
		{Name: "pro-ultramarine1flash-direct", Tier: "pro", Provider: "paperinstruments", Model: "ultramarine-1-flash", Enabled: true},
		{Name: "pro-opencode-a", Tier: "pro", Provider: "opencode", Model: "opencode/x", Enabled: true},
		{Name: "pro-missing-a", Tier: "pro", Provider: "absent-provider", Model: "absent/m", Enabled: true},
	})
	h.m.SetProviders(map[string]sprint.Provider{"paperinstruments": custom, "opencode": catalogued})

	packetFor := func(route string) sprint.Packet {
		t.Helper()
		c := &sprint.Card{ID: "s1-1.w1", Fields: map[string]string{
			"primary": "s1-1", "kind": "work", "attempt": "1", "route": route}}
		ps, err := h.st.Packets(h.ctx, []*sprint.Card{c})
		require.NoError(t, err)
		return ps[0]
	}

	t.Run("a custom provider row rides in the packet", func(t *testing.T) {
		t.Parallel()
		p := packetFor("pro-ultramarine1flash-direct")
		assert.Equal(t, "https://api.paperinstruments.example/v1", p.ProviderBaseURL)
		assert.Equal(t, "PAPERINSTRUMENTS_API_KEY", p.ProviderKeyEnv)
		assert.Equal(t, "@ai-sdk/openai-compatible", p.ProviderNPMAdapter)
	})
	t.Run("a catalogued provider carries none, and its json has no provider keys", func(t *testing.T) {
		t.Parallel()
		p := packetFor("pro-opencode-a")
		assert.Empty(t, p.ProviderBaseURL+p.ProviderKeyEnv+p.ProviderNPMAdapter)
		body, err := json.Marshal(p)
		require.NoError(t, err)
		var wire map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &wire))
		assert.NotContains(t, wire, "provider_base_url")
		assert.NotContains(t, wire, "provider_key_env")
		assert.NotContains(t, wire, "provider_npm_adapter")
	})
	t.Run("a provider with no row carries none", func(t *testing.T) {
		t.Parallel()
		p := packetFor("pro-missing-a")
		assert.Empty(t, p.ProviderBaseURL+p.ProviderKeyEnv+p.ProviderNPMAdapter)
	})
	t.Run("ProviderOf round-trips the three and keeps them on malformed funding", func(t *testing.T) {
		t.Parallel()
		p, err := ProviderOf("paperinstruments", map[string]string{
			"billing": "metered", "funded": "true",
			"base_url": "https://api.paperinstruments.example/v1", "key_env": "PAPERINSTRUMENTS_API_KEY",
			"npm_adapter": "@ai-sdk/openai-compatible"})
		require.NoError(t, err)
		assert.Equal(t, custom.BaseURL, p.BaseURL)
		assert.Equal(t, custom.KeyEnv, p.KeyEnv)
		assert.Equal(t, custom.NPMAdapter, p.NPMAdapter)
		assert.True(t, p.Funded)
		assert.Empty(t, p.Problem)
		bad, err := ProviderOf("paperinstruments", map[string]string{
			"billing": "metered", "funded": "garbage",
			"base_url": "https://api.paperinstruments.example/v1", "key_env": "PAPERINSTRUMENTS_API_KEY",
			"npm_adapter": "@ai-sdk/openai-compatible"})
		require.NoError(t, err)
		assert.Equal(t, custom.BaseURL, bad.BaseURL)
		assert.Equal(t, custom.KeyEnv, bad.KeyEnv)
		assert.Equal(t, custom.NPMAdapter, bad.NPMAdapter)
		assert.False(t, bad.Funded)
		assert.NotEmpty(t, bad.Problem)
	})
}
