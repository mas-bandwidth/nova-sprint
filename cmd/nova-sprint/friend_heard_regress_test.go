package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The deadlock of 2026-10-13: friends who work by hand read down, and every take of theirs was
// refused "no session evidence ... beat N s ago is not evidence": they could not take while
// down, and stayed down until a card finished, which they could not take to do. Her take, as her
// session sends it through the server, is a valid event of hers (docs/SPEC-SPRINT.md section 1,
// a friend's health; the owner: "any valid packet from that client resets the timeout"): it
// brings her up, and is taken. This test names no constant of the last-heard rule, so it runs
// on the code before it, where it fails.
func TestAFriendReadDownTakesAndHerTakeBringsHerUp(t *testing.T) {
	t.Parallel()
	ta, _, _ := friendReadyApp(t)
	ta.a.serveAddr = "mem:0"
	ta.a.sleep(40 * time.Minute) // past every window of every evidence she had
	assert.Equal(t, sprint.Down, whereFriends(ta)["amy"].Status, "silent past every window: down")

	code, out := ta.served("take", "--as", "friend.amy", "s1-3.w1@1", "--epoch", "0")
	require.Equal(t, 0, code, "a take of a friend read down is refused for her silence: %s", out)
	assert.Contains(t, out, "s1-3.w1 fleet ready -> working member=friend.amy gen=1")
	c := freshCard(ta, "s1-3")
	assert.Equal(t, sprint.Working, c.Work[0].Col)
	assert.Equal(t, sprint.Up, whereFriends(ta)["amy"].Status, "the take is a valid event of hers: she is up")
	ta.clean()
}
