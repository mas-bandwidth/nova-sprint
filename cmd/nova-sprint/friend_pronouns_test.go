package main

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The production sync reads the configured PostgreSQL row; its beat transports
// the explicit setting to nova-friend without giving the daemon store access.
func TestFriendPronounsTravelFromConfigThroughSyncAndBeat(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t, "amy")
	root := t.TempDir()
	for _, choice := range []string{"", "he/him", "she/her", "they/them"} {
		t.Run("pronouns="+choice, func(t *testing.T) {
			if choice != "" {
				_, _, err := cfg.Update(context.Background(), config.KindFriend, "amy", map[string]string{"pronouns": choice}, "test")
				require.NoError(t, err)
			}
			ta.ok("friend sync --root " + root)
			want := choice
			if want == "" {
				want = config.DefaultFriendPronouns
			}
			answer := ta.ok("friend beat amy")
			assert.Contains(t, answer, " row_pronouns="+want)
			var facts map[string]any
			ta.json("friend beat amy", &facts)
			assert.Equal(t, want, facts["row_pronouns"])
		})
	}
}
