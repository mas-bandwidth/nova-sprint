package main

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// Only tests explicitly selecting a native execution tier use this fixture.
// The server constructor and negative route/tier tests keep their original config.
func configurePositiveServerTier(t *testing.T, r *serverRig, tier string, readers ...string) {
	t.Helper()
	ctx := context.Background()
	st, err := r.a.store(common{redis: r.a.serveAddr, actor: "boss"})
	require.NoError(t, err)
	mem := r.a.twins[r.a.serveAddr].mem
	mem.SetRoutes([]sprint.Route{{Name: "fixture-" + tier, Tier: tier, Provider: "fixture", Model: "fixture", Tokens: 1000, Deadline: 600, Enabled: true}})
	mem.SetTiers(map[string][]string{tier: {"fixture-" + tier}})
	mem.SetProviders(map[string]sprint.Provider{"fixture": {Name: "fixture", Billing: "metered", Funded: true}})
	if len(readers) > 0 {
		require.NoError(t, st.EnsureReaderTiers(ctx))
		for _, reader := range readers {
			require.NoError(t, st.B.RowSet(ctx, st.Names.Table(sprint.Readers), reader, map[string]string{sprint.ReaderTiers: tier}))
		}
	}
}
