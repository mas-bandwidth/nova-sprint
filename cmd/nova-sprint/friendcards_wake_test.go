package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/config"
)

// The inbox files are the record. Every delivered card wakes one one-shot lane,
// including when an old stored row still spells the retired batch mode. A failed
// send leaves its file in place and records a note for that card.
func TestFriendSyncWakesEachDeliveredCardInOneShotMode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, mode string
		cards      int
		fail       bool
	}{
		{name: "default one-shot", cards: 3},
		{name: "historical batch row", mode: config.FriendModeBatch, cards: 3},
		{name: "explicit one-shot", mode: config.FriendModeOneShot, cards: 3},
		{name: "no delivery"},
		{name: "failed card wakes", cards: 3, fail: true},
		{name: "thirteen distinct ids", cards: 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ta, cfg := friendApp(t)
			_, err := cfg.Insert(context.Background(), config.KindFriend, config.Row{Name: "amy", Fields: map[string]string{"width": "16", "tiers": "flash"}}, "t")
			require.NoError(t, err)
			root := t.TempDir()
			ta.ok("friend sync --root " + root)
			ta.beatUp("amy")
			briefs := t.TempDir()
			for i := range tc.cards {
				id := fmt.Sprintf("s1-%02d", i+1)
				require.NoError(t, os.WriteFile(filepath.Join(briefs, id+".md"), []byte(passingBrief(id+": a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: only friend amy")), 0o644))
			}
			if tc.cards > 0 {
				ta.ok("add --stream s1 --brief-dir " + briefs)
				ta.ok("start")
				ta.ok("tick")
			}
			// Cards already on her row stay there after a mode change. Sync
			// reads the store row as it is now, not the mode at deal time.
			if tc.mode != "" {
				_, _, err = cfg.Update(context.Background(), config.KindFriend, "amy", map[string]string{"mode": tc.mode}, "t")
				require.NoError(t, err)
			}
			var sent []bus.Message
			ta.a.bus = func(_ context.Context, m bus.Message, _ func(string)) error {
				sent = append(sent, m)
				if tc.fail {
					return errors.New("injected send failure")
				}
				return nil
			}
			out := ta.ok("friend sync --root " + root)
			assert.Len(t, sent, tc.cards)
			assert.Equal(t, tc.cards, strings.Count(out, "FRIEND-CARD DELIVERED"))
			for i := range tc.cards {
				_, err := os.Stat(filepath.Join(root, "amy-working", "inbox", fmt.Sprintf("s1-%02d.w1", i+1), "BRIEF.md"))
				assert.NoError(t, err, "the delivered file stands")
			}
			if len(sent) == tc.cards {
				seen := map[string]int{}
				for _, m := range sent {
					assert.Equal(t, "coordinator", m.From)
					assert.Equal(t, []string{"amy"}, m.To)
					assert.Contains(t, m.Subject, " dealt: FRIEND-CARD DELIVERED")
					assert.Contains(t, m.Body, "BRIEF.md")
					for i := 1; i <= tc.cards; i++ {
						id := fmt.Sprintf("s1-%02d.w1", i)
						if strings.Contains(m.Subject, "card "+id+" dealt:") {
							seen[id]++
						}
					}
				}
				assert.Len(t, seen, tc.cards, "one message names each card")
				for i := range tc.cards {
					assert.Equal(t, 1, seen[fmt.Sprintf("s1-%02d.w1", i+1)])
				}
			}
			notes, _, err := ta.m.NotesSince(context.Background(), "", 100)
			require.NoError(t, err)
			var failed []sprint.Note
			for _, n := range notes {
				if n.Type == sprint.NFriendNotWoken {
					failed = append(failed, n)
				}
			}
			if tc.fail {
				assert.Len(t, failed, tc.cards)
				assert.Equal(t, tc.cards, strings.Count(out, "injected send failure"))
				assert.Equal(t, tc.cards, strings.Count(out, "FRIEND-CARD NOTE"))
				if len(failed) == tc.cards {
					for i, n := range failed {
						assert.Equal(t, []string{fmt.Sprintf("s1-%02d", i+1)}, n.Primaries)
						assert.Contains(t, n.What, fmt.Sprintf("s1-%02d.w1", i+1))
					}
				}
			} else {
				assert.Empty(t, failed)
			}
			ta.ok("friend sync --root " + root)
			assert.Len(t, sent, tc.cards, "a pass with no new files has no wake")
			ta.clean()
		})
	}
}
