package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// A one-shot friend's card taken by her member stays working across ticks: no
// friend-lane machinery (the tick's unstarted-working bounce, which waits for a
// friend daemon's start receipt) moves a member's card on her row. It then
// finishes through the member's own verbs, as a fleet member's card does.
func TestAOneShotFriendsCardStaysWorkingAcrossTicks(t *testing.T) {
	t.Parallel()
	ta, _ := oneShotApp(t)
	ta.a.tip = tipIs(t, landHead)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s1-1.md"), []byte(passingBrief("s1-1: a one-shot friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend fay")), 0o644))
	ta.ok("fleet beat friend.fay --load 5")
	ta.ok("add --stream s1 --brief-dir " + dir)
	ta.ok("start")
	ta.ok("tick")
	take := ta.ok("take --as friend.fay --limit 1")
	assert.Contains(t, take, "s1-1")
	ta.ok("fleet beat friend.fay --load 5")
	ta.ok("tick")
	ta.ok("tick")
	var w whereView
	ta.json("where", &w)
	assert.Equal(t, "1", w.Tables[sprint.Friends]["fay"]["working"], "the member's card is still working on the friend's row after ticks")
	fin := ta.ok("finish --as friend.fay s1-1.w1@1")
	assert.Contains(t, fin, "s1-1.w1 working -> done ok")
}

// Friend sync delivers nothing to a one-shot and starts nothing for her: no
// inbox brief, queue file or bus wake is hers (her member reads the brief from
// the server, nova-swarm member --as friend.fay). A card working on her row
// through her member is left exactly as the member left it.
func TestFriendSyncDeliversNothingToAOneShot(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t)
	_, err := cfg.Insert(context.Background(), config.KindFriend, config.Row{Name: "fay", Fields: map[string]string{
		"tiers": "flash", "roles": "builder,reader", "width": "2", "session": config.FriendSessionOneShot, "pronouns": "she/her"}}, "t")
	require.NoError(t, err)
	root := t.TempDir()
	ta.ok("friend sync --root " + root)
	ta.a.tip = tipIs(t, landHead)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s1-1.md"), []byte(passingBrief("s1-1: a one-shot friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend fay")), 0o644))
	ta.ok("fleet beat friend.fay --load 5")
	ta.ok("add --stream s1 --brief-dir " + dir)
	ta.ok("start")
	ta.ok("tick")
	ta.ok("take --as friend.fay --limit 1")
	out := ta.ok("friend sync --root " + root)
	assert.NotContains(t, out, "DELIVERED", "friend sync delivered nothing to the one-shot")
	_, statErr := os.Lstat(filepath.Join(root, "fay-working"))
	assert.True(t, os.IsNotExist(statErr), "no friend working directory is made for a one-shot: %v", statErr)
	var w whereView
	ta.json("where", &w)
	assert.Equal(t, "1", w.Tables[sprint.Friends]["fay"]["working"], "the member's working card is untouched by friend sync")
}
