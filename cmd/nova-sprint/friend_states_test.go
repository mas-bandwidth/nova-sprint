package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// later moves the test's clock on.
func (ta *testApp) later(d time.Duration) {
	ta.mu.Lock()
	ta.now = ta.now.Add(d)
	ta.mu.Unlock()
}

// On the live path a friend's state is up, down or held and nothing else (the owner,
// 2026-10-14, v1.3.6: "there is only up, down, held from the sprint POV"). A friend up on
// the hook record who has finished nothing in forty-five minutes stays up in where --json
// and is dealt the next card by the tick: no judgment of her work changes her state or
// keeps a card from her (on 2026-10-14 Stella was up on the hook record and "stalled" in the
// sprint, and the deal skipped her until a manual friend up).
func TestATickDealsAnUpFriendWhoHasFinishedNothing(t *testing.T) {
	t.Parallel()
	ta, _ := friendCardApp(t, "friend amy", "amy")
	ta.ok("tick")
	var c cardView
	ta.json("card s1-1", &c)
	require.Len(t, c.Work, 1)
	require.Equal(t, sprint.FriendRow("amy"), c.Work[0].Row, "the card is dealt to amy")

	for range 3 {
		ta.later(15 * time.Minute)
		ta.ok("tick")
	}
	assert.Equal(t, map[string]string{"amy": sprint.Up}, ta.friendStatus(), "quiet for forty-five minutes, she is up: the hook record says so")

	brief := filepath.Join(t.TempDir(), "s1-2.md")
	require.NoError(t, os.WriteFile(brief, []byte(passingBrief("s1-2: another friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend amy")), 0o644))
	ta.ok("add --stream s1 --brief-dir " + filepath.Dir(brief))
	ta.ok("tick")
	ta.json("card s1-2", &c)
	require.Len(t, c.Work, 1, "s1-2 is dealt")
	assert.Equal(t, sprint.FriendRow("amy"), c.Work[0].Row, "an up friend with no recent finish is dealt")
}

// A friend whose daemon beats but who has no hook record is down, and the tick deals her
// nothing: a beat or a work report is never the hook record.
func TestATickDealsNothingToAnUnhookedFriend(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy")
	ta.a.tip = tipIs(t, landHead)
	ta.ok("friend sync --root " + t.TempDir())
	ta.ok("friend beat amy") // her daemon beats; nova-hook has no record of her
	brief := filepath.Join(t.TempDir(), "s1-1.md")
	require.NoError(t, os.WriteFile(brief, []byte(passingBrief("s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend amy")), 0o644))
	ta.ok("add --stream s1 --brief-dir " + filepath.Dir(brief))
	ta.ok("start")
	ta.ok("tick")
	assert.Equal(t, map[string]string{"amy": sprint.Down}, ta.friendStatus())
	var c cardView
	ta.json("card s1-1", &c)
	for _, wc := range c.Work {
		assert.NotEqual(t, sprint.FriendRow("amy"), wc.Row, "a down friend is dealt nothing")
	}
}
