package main

import (
	"context"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
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
			seats, err := (&store.Store{B: ta.m, Now: ta.a.now}).FriendSeats(context.Background(), ta.a.now())
			require.NoError(t, err)
			require.Len(t, seats, 1)
			assert.Equal(t, want, seats[0].Pronouns, "the actual status/coordinator view carries the synced setting")

		})
	}
}

// The take command names a running card on the actual refusal/NOTE path.
func TestFriendTakeSaysTheSyncedPronounsWhenTheBeatNamesARunningCard(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ set, want string }{{"she/her", "her"}, {"he/him", "his"}, {"they/them", "their"}, {"", "their"}} {
		t.Run(tc.set, func(t *testing.T) {
			ta, root := takeApp(t, 1, nil, "pat")
			ta.ok("tick")
			readConfig := ta.a.friends
			ta.a.friends = func(ctx context.Context, pg string) ([]config.Row, error) {
				rows, err := readConfig(ctx, pg)
				for i := range rows {
					rows[i].Fields["pronouns"] = tc.set
				}
				return rows, err
			}
			ta.ok("friend sync --root " + root)
			ta.ok("friend beat pat --running s1-1.w1")
			out := ta.ok("friend take pat --all-unstarted")
			assert.Contains(t, out, tc.want+" beat names it running")
			assert.NotContains(t, out, "{their}")
		})
	}
}

// Persisted kind IDs stay byte-for-byte while the production inbox renderer
// gives the reader neutral labels. Old stored judgments still match the ID.
func TestHistoricalFriendNoteTypesAreNeutralOnlyAtTheDisplayBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ id, label string }{
		{"a card pinned to one friend waits while she is not up", "a card pinned to one friend waits while the friend is not up"},
		{"a friend was not told of her card", "a friend was not told of a card"},
		{"a friend's row disagrees with her QUEUE.json", "a friend's row disagrees with the friend's QUEUE.json"},
		{"a friend's row agrees with her QUEUE.json again", "a friend's row agrees with the friend's QUEUE.json again"},
	} {
		g := sprint.Group{ID: "old-note", Kind: sprint.Happened, Type: tc.id, Oldest: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)}
		line := groupLine(g, g.Oldest)
		assert.Contains(t, line, tc.label)
		assert.NotContains(t, line, tc.id)
		assert.Equal(t, tc.id, g.Type, "rendering must not rewrite the stored/API identity")
	}
}

// Manual steps load metadata without populating routing/admission seats.
func TestManualFriendTakeSaysTheConfiguredPronounsWhenItFillsTheFreedLane(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ set, want string }{{"she/her", "her"}, {"he/him", "his"}, {"they/them", "their"}, {"", "their"}} {
		t.Run(tc.set, func(t *testing.T) {
			ta, root := takeApp(t, 2, nil, "pat")
			readConfig := ta.a.friends
			ta.a.friends = func(ctx context.Context, pg string) ([]config.Row, error) {
				rows, err := readConfig(ctx, pg)
				for i := range rows {
					rows[i].Fields["pronouns"] = tc.set
				}
				return rows, err
			}
			ta.ok("friend sync --root " + root)
			ta.ok("tick")
			ta.ok("take --as friend.pat s1-1.w1@1")
			out := ta.ok("friend take pat s1-1")
			assert.Contains(t, out, "s1-2.w1 ready -> working ("+tc.want+" next, taken now)")
		})
	}
}
