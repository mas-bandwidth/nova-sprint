package run

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoDealNamesTheStoredVersionToActivate: with no policy active the deal's
// gate names the fix: policy activate of the newest stored version when
// versions are stored and none is active (the live store of 2026-11-07, set
// before the first set activated; defect D3), policy set when none is
// stored. An active version that answered lifts the gate. Unit: store.Fake.
func TestNoDealNamesTheStoredVersionToActivate(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "no policy is active (v3 is stored, none active): nova-sprint policy activate v3 --reason <why>",
		noDeal(policy.Versions{}, policy.Answers{}, nil, 3))
	assert.Equal(t, "no policy is active: nova-sprint policy set <file> --reason <why> (the first set activates; nova-sprint up installs the default)",
		noDeal(policy.Versions{}, policy.Answers{}, nil, 0))
	active := store.PolicyVersion{Version: 2, Active: true}
	assert.Equal(t, "", noDeal(policy.Versions{Active: &active}, policy.Answers{Version: 2}, nil, 3))

	f := store.NewFake() // nothing stored, nothing active
	must(t, f, "start", store.SetStopped{Stopped: false, Reason: "test"})
	tk, err := NewTicker(context.Background(), f, &healthy{}, &answering{}, t0)
	require.NoError(t, err)
	assert.Equal(t, 0, tk.storedPolicy(context.Background()), "nothing stored")
	for i := range 3 {
		must(t, f, "policy "+string(rune('0'+i)), store.PutPolicy{Kind: "script", Name: "p", Body: "p" + string(rune('0'+i))})
	}
	assert.Equal(t, 3, tk.storedPolicy(context.Background()), "the newest stored version")
}
