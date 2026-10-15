package store

import (
	"github.com/mas-bandwidth/nova-tools/pkg/redisacl"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvidersRideTheRouteRead(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	p := map[string]sprint.Provider{"opencode": {Name: "opencode", Billing: "credits", Funded: true}, "openrouter": {Name: "openrouter", Billing: "payg", Funded: false}}
	h.m.SetProviders(p)
	delete(p, "opencode")
	s := &sprint.Snapshot{}
	require.NoError(t, h.st.RoutesInto(h.ctx, s))
	require.Len(t, s.Providers, 2)
	assert.True(t, s.Providers["opencode"].Funded)
	assert.False(t, s.Providers["openrouter"].Funded)
	delete(s.Providers, "opencode")
	require.NoError(t, h.st.RoutesInto(h.ctx, s))
	require.Len(t, s.Providers, 2)
}

func TestProviderKeysAreReadableByRouteReaders(t *testing.T) {
	t.Parallel()
	for _, role := range redisacl.Roles() {
		if role.Name == redisacl.Coordinator {
			assert.Contains(t, role.Keys, "~*")
			continue
		}
		assert.Contains(t, role.Keys, "%R~providers", role.Name)
		assert.Contains(t, role.Keys, "%R~provider:*", role.Name)
		assert.NotContains(t, role.Keys, "~providers", role.Name)
		assert.NotContains(t, role.Keys, "~provider:*", role.Name)
	}
}

func TestAnAbsentFundingFieldIsNotUnfunded(t *testing.T) {
	t.Parallel()
	missing, err := ProviderOf("p", map[string]string{"billing": "metered"})
	require.NoError(t, err)
	assert.NotEmpty(t, missing.Problem)
	p, err := ProviderOf("p", map[string]string{"billing": "metered", "funded": "false"})
	require.NoError(t, err)
	assert.False(t, p.Funded)
	assert.Empty(t, p.Problem)
}

func TestMalformedProviderFundingDoesNotAbortRead(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		hash map[string]string
	}{
		{"missing", map[string]string{"billing": "plan"}},
		{"invalid", map[string]string{"billing": "plan", "funded": "garbage"}},
		{"unpopulated", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, err := ProviderOf(tc.name, tc.hash)
			assert.NoError(t, err, "invalid config is a named check finding, not a tick read error")
			assert.False(t, p.Funded)
			assert.NotEmpty(t, p.Problem)
			assert.Equal(t, tc.name, p.Name)
		})
	}
}
