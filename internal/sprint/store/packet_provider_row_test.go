package store

import (
	"encoding/json"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// TestAPacketCarriesItsRouteProvidersCustomRow: a card on a route whose provider row has a
// base_url carries the row's base_url, key_env and npm_adapter in its packet; a catalogued
// provider (empty base_url) and a provider with no row carry none of the three, and the
// json omits them. The key_env is a fake variable NAME; no key value exists in this test.
func TestAPacketCarriesItsRouteProvidersCustomRow(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.m.SetRoutes([]sprint.Route{
		{Name: "pro-ultramarine1flash-direct", Tier: "pro", Provider: "paperinstruments", Model: "ultramarine-1-flash", Enabled: true},
		{Name: "pro-catalogued", Tier: "pro", Provider: "catalogued", Model: "m", Enabled: true},
		{Name: "pro-norow", Tier: "pro", Provider: "norow", Model: "m", Enabled: true},
	})
	h.m.SetProviders(map[string]sprint.Provider{
		"paperinstruments": {Name: "paperinstruments", Billing: "metered", Funded: true,
			BaseURL: "https://api.paperinstruments.example/v1", KeyEnv: "PAPERINSTRUMENTS_API_KEY", NPMAdapter: "@ai-sdk/openai-compatible"},
		"catalogued": {Name: "catalogued", Billing: "metered", Funded: true},
	})
	card := func(id, kind, route string) *sprint.Card {
		return &sprint.Card{ID: id, Fields: map[string]string{"primary": "p-" + id, "kind": kind, "attempt": "1", sprint.FieldRoute: route}}
	}
	cards := []*sprint.Card{
		card("um.w1", "work", "pro-ultramarine1flash-direct"),
		card("um.r1", "read", "pro-ultramarine1flash-direct"),
		card("cat.w1", "work", "pro-catalogued"),
		card("norow.w1", "work", "pro-norow"),
		card("pin.w1", "work", sprint.RoutePin),
	}
	packets, err := h.st.Packets(h.ctx, cards)
	require.NoError(t, err)
	require.Len(t, packets, len(cards))
	for i, p := range packets {
		if i < 2 {
			require.Equal(t, "https://api.paperinstruments.example/v1", p.ProviderBaseURL, p.Card)
			require.Equal(t, "PAPERINSTRUMENTS_API_KEY", p.ProviderKeyEnv, p.Card)
			require.Equal(t, "@ai-sdk/openai-compatible", p.ProviderNPMAdapter, p.Card)
			continue
		}
		require.Empty(t, p.ProviderBaseURL+p.ProviderKeyEnv+p.ProviderNPMAdapter, p.Card)
		body, err := json.Marshal(p)
		require.NoError(t, err)
		var wire map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &wire))
		for _, k := range []string{"provider_base_url", "provider_key_env", "provider_npm_adapter"} {
			require.NotContains(t, wire, k, p.Card)
		}
	}
}

func TestProviderOfCarriesTheCustomBlock(t *testing.T) {
	t.Parallel()
	block := map[string]string{"base_url": "https://api.paperinstruments.example/v1", "key_env": "PAPERINSTRUMENTS_API_KEY", "npm_adapter": "@ai-sdk/openai-compatible"}
	for _, tc := range []struct {
		name, funded, problem string
	}{
		{"funded", "true", ""},
		{"bad funded", "maybe", `funded field is invalid: "maybe"`},
	} {
		h := map[string]string{"billing": "metered", "funded": tc.funded}
		for k, v := range block {
			h[k] = v
		}
		p, err := ProviderOf("paperinstruments", h)
		require.NoError(t, err, tc.name)
		require.Equal(t, block["base_url"], p.BaseURL, tc.name)
		require.Equal(t, block["key_env"], p.KeyEnv, tc.name)
		require.Equal(t, block["npm_adapter"], p.NPMAdapter, tc.name)
		require.Equal(t, tc.problem, p.Problem, tc.name)
	}
}
