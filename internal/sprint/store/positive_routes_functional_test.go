//go:build functional

package store

import (
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/stretchr/testify/require"
)

// positiveLiveRoutes is opt-in configuration for lifecycle/index fixtures that
// execute native fleet work. It writes the real nova-config keys, never a twin
// replacement or a fallback on missing configuration.
func positiveLiveRoutes(h *harness) {
	h.t.Helper()
	c := h.st.B.(*Redis).C
	require.NoError(h.t, c.SAdd(h.ctx, config.ProvidersKey, "fixture").Err())
	require.NoError(h.t, c.HSet(h.ctx, config.ProviderKey("fixture"), "billing", "metered", "funded", "true").Err())
	for _, tier := range []string{"flash", "pro", "heavy"} {
		name := "fixture-" + tier
		require.NoError(h.t, c.SAdd(h.ctx, config.RoutesKey, name).Err())
		require.NoError(h.t, c.HSet(h.ctx, config.RouteKey(name), "name", name, "tier", tier, "provider", "fixture", "model", "fixture", "tokens", "1000", "deadline", "600", "enabled", "true").Err())
		require.NoError(h.t, c.HSet(h.ctx, config.TierKey(tier), "name", tier, "routes", name).Err())
	}
	// Validate the configuration through the production Redis reader.
	set, _, err := h.st.B.(RouteReader).Routes(h.ctx)
	require.NoError(h.t, err)
	require.Len(h.t, set.Routes, 3)
	require.True(h.t, set.Providers["fixture"].Funded)
}
