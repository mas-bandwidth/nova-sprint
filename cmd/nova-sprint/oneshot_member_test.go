package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/hook"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/config"
)

// oneShotApp is a sprint whose roster names fay, a one-shot friend (nova-config friend
// --session one-shot: no live session between cards) who builds and reads, synced; the
// hook record is read through hooksAsked, which records every name it is asked for.
func oneShotApp(t *testing.T) (ta *testApp, hooksAsked *[]string) {
	t.Helper()
	ta, cfg := friendApp(t)
	_, err := cfg.Insert(context.Background(), config.KindFriend, config.Row{Name: "fay", Fields: map[string]string{
		"tiers": "flash", "roles": "builder,reader", "width": "2", "session": config.FriendSessionOneShot, "pronouns": "she/her"}}, "t")
	require.NoError(t, err)
	var asked []string
	prev := ta.a.readFriendHooks
	ta.a.readFriendHooks = func(ctx context.Context, names ...string) ([]hook.Status, time.Time, error) {
		asked = append(asked, names...)
		return prev(ctx, names...)
	}
	ta.ok("friend sync --root " + t.TempDir())
	return ta, &asked
}

// A one-shot friend is a member lane with an identity prompt (the v1.3 friend lane
// spec, guarantee 12; nova-tools tla/FriendLane.tla, OneShot): the
// member that serves the friend's row (nova-swarm member --as friend.fay --prompt
// <file>) beats, is shown the dealt card by queue, takes it by count, says progress and
// finishes it with the member's own verbs, and the card is done on the friend's row, the
// finish recorded as the friend's (the server's claim, never the run's identity). The
// friend's status is the member's beat by the fleet's rule, never a hook: no hook record is
// read for the one-shot and no presence exists for him. Before the member beats, he is down
// and dealt nothing.
func TestAOneShotFriendIsDealtAndFinishesThroughTheMemberPath(t *testing.T) {
	t.Parallel()
	ta, asked := oneShotApp(t)
	ta.a.tip = tipIs(t, landHead)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s1-1.md"), []byte(passingBrief("s1-1: a one-shot friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend fay")), 0o644))
	ta.ok("tick")
	assert.Contains(t, ta.friendStatus()["fay"], "down (no member beat on friend.fay)", "no member beat: the one-shot is down")

	assert.Contains(t, ta.ok("fleet beat friend.fay --load 5"), "FLEET-BEAT OK friend.fay")
	ta.ok("add --stream s1 --brief-dir " + dir)
	ta.ok("start")
	ta.ok("tick")
	assert.Equal(t, "up", ta.friendStatus()["fay"], "the member's beat is the one-shot's status")
	assert.Contains(t, ta.ok("queue --as friend.fay"), "s1-1", "the card is dealt to the friend's row")
	assert.Contains(t, ta.ok("queue --as friend.fay --json --packets 0"), `"width":2`, "the member runs at the friend's width, the roster's")

	take := ta.ok("take --as friend.fay --limit 1")
	assert.Contains(t, take, "s1-1")
	ta.ok("progress --as friend.fay --epoch 0 s1-1.w1")
	fin := ta.ok("finish --as friend.fay s1-1.w1@1")
	assert.Contains(t, fin, "s1-1.w1 working -> done ok", "the member's finish lands the card done on the friend's row")
	assert.Contains(t, fin, "s1-1 working -> review", "and its primary goes to review as any member's does")
	var w whereView
	ta.json("where", &w)
	assert.Equal(t, "0", w.Tables[sprint.Friends]["fay"]["working"], "nothing is left working on the friend's row")
	assertWorkerRecorded(t, ta, "finish", sprint.FriendRow("fay"))
	assert.False(t, slices.Contains(*asked, "fay"), "no hook record is read for a one-shot: %v", *asked)
}

// No friend lane, daemon beat or link serves a one-shot: a lane's take for the friend's row
// and a friend daemon's beat are refused, naming the member that serves the row, and write
// nothing; a live friend's row never beats as a member.
func TestTheFriendLanePathIsRefusedForAOneShot(t *testing.T) {
	t.Parallel()
	ta, _ := oneShotApp(t)
	ta.ok("fleet beat friend.fay --load 5")
	before := ta.applies()
	code, out, errs := ta.do("friend beat fay")
	assert.NotEqual(t, 0, code, "friend beat for a one-shot: %s%s", out, errs)
	assert.Contains(t, out+errs, "fay is a one-shot")
	assert.Contains(t, out+errs, "nova-swarm member --as friend.fay")
	_, out, errs = ta.do("take --as friend.fay --lane 1")
	assert.Contains(t, out+errs, "fay is a one-shot", "a lane's take for a one-shot's row is refused")
	assert.Equal(t, before, ta.applies(), "a refused friend-lane verb wrote nothing")

	code, out, errs = ta.do("fleet beat friend.nobody --load 5")
	assert.NotEqual(t, 0, code, "a row no friend names never beats: %s%s", out, errs)
}
