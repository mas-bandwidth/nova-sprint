package sprint

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEverySetFlagHasASettingsRow holds the settings view to the set verb:
// every flag cmdSet defines has exactly one row, whose default is the
// Defaults row's value, and no row names a flag set does not define
// (attempts is retired: nova-config's sprint row, listed in NotSettings).
func TestEverySetFlagHasASettingsRow(t *testing.T) {
	t.Parallel()
	flags := []string{
		"alarm-fleet", "alarm-merging", "alarm-ready", "alarm-review",
		"base", "ci-queued-bound", "dealt-max", "fleet", "fleet-tiers", "friend-finish",
		"friend-idle", "friends", "friends-tiers", "go-lanes",
		"landing-stall-bound", "pegged-bound", "read-cards", "read-tier",
		"reads", "rework-priority", "unserved-tier-falls-to",
	}
	rows := SettingsRows()
	seen := map[string]int{}
	for _, r := range rows {
		seen[r.Flag]++
		d, ok := DefaultOf(r.Prop)
		require.True(t, ok, "row %s prop %s has no Defaults row", r.Flag, r.Prop)
		assert.Equal(t, d.Value, r.Default, "row %s default is its Defaults value", r.Flag)
		assert.NotEmpty(t, r.Accessor, "row %s names the accessor the tick reads it through", r.Flag)
	}
	for _, f := range flags {
		assert.Equal(t, 1, seen[f], "flag --%s has exactly one settings row", f)
	}
	for f := range seen {
		assert.True(t, slices.Contains(flags, f), "row --%s names no flag set defines", f)
	}

	s := settingsSnapshot(nil)
	got := SettingsOf(s)
	require.Len(t, got, len(rows))
	for _, g := range got {
		assert.Equal(t, g.Default, g.Value, "fresh %s reads at its default", g.Flag)
		assert.False(t, g.Set, "fresh %s is not set", g.Flag)
	}
	s.Work.SetProp(PropReadTier, "pro")
	for _, g := range SettingsOf(s) {
		if g.Flag == "read-tier" {
			assert.Equal(t, "pro", g.Value)
			assert.True(t, g.Set)
		}
	}
}
