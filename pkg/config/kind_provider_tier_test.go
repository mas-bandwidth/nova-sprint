package config

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestKindProviderAndRouteTierMatchNovaTools pins KindProvider and the route
// tier field to nova-tools pkg/config/kind.go at
// c402e160d7272bc1f5475b8f7c4a072aba9fd62e (paired subscription producer,
// 2026-10-13). The fixture is those three spans copied from that file at that
// sha: the KindProvider constant, the provider kind literal, and the route
// tier field. The rest of the two files is not required to match. No network,
// no clock, no socket.
func TestKindProviderAndRouteTierMatchNovaTools(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("kind.go")
	require.NoError(t, err)
	raw, err := os.ReadFile("testdata/nova-tools-kind-provider-route-tier-c402e160.txt")
	require.NoError(t, err)
	sections := kindToolsFixture(t, string(raw))
	got := string(src)
	var misses []string
	for _, name := range []string{"kind-provider-const", "kind-provider-block", "route-tier-field"} {
		span, ok := sections[name]
		require.Truef(t, ok && span != "", "fixture is missing %s", name)
		if !strings.Contains(got, span) {
			misses = append(misses, name)
		}
	}
	require.Empty(t, misses, "kind.go does not match nova-tools at c402e160 field for field")

	provider, ok := Lookup("provider")
	require.True(t, ok, "KindProvider is a kind")
	require.Equal(t, "provider", provider.Name)
	require.Equal(t, "providers", provider.Table)
	require.Equal(t, []string{"billing", "funded", "balance_usd", "balance_as_of", "console_url", "note"}, kindFieldNames(provider))
	billing, ok := provider.Field("billing")
	require.True(t, ok)
	require.Equal(t, TypeEnum, billing.Type)
	require.Equal(t, []string{"metered", "plan", "both", "subscription"}, billing.Enum)
	require.True(t, billing.Required)
	funded, ok := provider.Field("funded")
	require.True(t, ok)
	require.Equal(t, TypeBool, funded.Type)
	require.True(t, funded.Required)
	require.False(t, provider.Check == nil, "the provider kind's Check is checkProvider")

	route, ok := Lookup(KindRoute)
	require.True(t, ok)
	tier, ok := route.Field("tier")
	require.True(t, ok)
	require.Equal(t, TypeEnum, tier.Type)
	require.True(t, tier.Required)
	require.Equal(t, Tiers, tier.Enum, "the route tier field is Tiers, frontier included")
	require.Contains(t, tier.Enum, "frontier")
	require.Equal(t, "the tier it serves: one of "+strings.Join(Tiers, ", "), tier.Help)
}

// kindToolsFixture splits the pinned nova-tools spans. A section is the bytes
// after a "--- name ---" line, through the line before the next marker.
func kindToolsFixture(t *testing.T, raw string) map[string]string {
	t.Helper()
	sections := map[string]string{}
	var name string
	var b strings.Builder
	flush := func() {
		if name != "" {
			sections[name] = b.String()
			b.Reset()
		}
	}
	for _, line := range strings.SplitAfter(raw, "\n") {
		trim := strings.TrimRight(line, "\n")
		if strings.HasPrefix(trim, "--- ") && strings.HasSuffix(trim, " ---") {
			flush()
			name = strings.TrimSuffix(strings.TrimPrefix(trim, "--- "), " ---")
			continue
		}
		b.WriteString(line)
	}
	flush()
	return sections
}

func kindFieldNames(k *Kind) []string {
	out := make([]string, len(k.Fields))
	for i, f := range k.Fields {
		out[i] = f.Name
	}
	return out
}
