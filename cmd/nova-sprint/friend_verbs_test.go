package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// The verbs friends lacked that machines have (the owner, 2026-10-04: "What else is like
// this? Missing verbs we need for friends, that machines already have"), on the twin.

// friendWidth is the friend's width as where --json shows it.
func friendWidth(ta *testApp, friend string) string {
	var w whereView
	ta.json("where", &w)
	return cellText(w.Tables[sprint.Friends][friend]["width"])
}

func TestFriendUpWidthSetsHerWidthAsFleetUpWidthSetsAMembers(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy")
	ta.ok("friend sync")
	assert.Equal(t, "8", friendWidth(ta, "amy"))
	out := ta.ok("friend up amy --width 3")
	assert.Contains(t, out, "FRIEND-UP OK amy held=false width=3")
	assert.Equal(t, "3", friendWidth(ta, "amy"))
	ta.ok("friend down amy")
	ta.ok("friend up amy")
	assert.Equal(t, "3", friendWidth(ta, "amy"), "a release without --width leaves it as it is")

	code, _, errs := ta.do("friend up amy --width 0")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "--width: a width wants a whole number from 1 to")
	code, _, _ = ta.do("friend down amy --width 3")
	assert.Equal(t, 2, code, "friend down takes no width")

	// friend sync sets her nova-config row's again, as fleet sync sets a member's
	ta.ok("friend sync")
	assert.Equal(t, "8", friendWidth(ta, "amy"))
}

// whereFriends is where --json's friends, by name.
func whereFriends(ta *testApp) map[string]store.FriendRow {
	var w struct {
		Friends []store.FriendRow `json:"friends"`
	}
	ta.json("where", &w)
	out := map[string]store.FriendRow{}
	for _, f := range w.Friends {
		out[f.Name] = f
	}
	return out
}

// A friend's machinery beats with her counts and her load, as a machine's beats with its
// load; the beat took her name alone and refused them (Johnny, 2026-10-04).
func TestFriendBeatTakesHerCountsAndLoadAsFleetBeatTakesALoad(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy")
	ta.ok("friend sync")
	ta.pong("amy")
	out := ta.ok("friend beat amy --working 2 --queue 3 --width 4 --running s1-1.w1,s1-2.w1 --load 40%")
	assert.Contains(t, out, "FRIEND-BEAT OK amy at=")
	assert.Contains(t, out, " working=2 queue=3 width=4 load=40.0% running=s1-1.w1,s1-2.w1")
	f := whereFriends(ta)["amy"]
	require.NotNil(t, f.Report)
	assert.Equal(t, 2, *f.Report.Working)
	assert.Equal(t, 3, *f.Report.Queue)
	assert.Equal(t, 4, *f.Report.Width)
	assert.Equal(t, []string{"s1-1.w1", "s1-2.w1"}, f.Report.Running)
	assert.Equal(t, 40.0, f.Load)
	assert.Equal(t, 8, f.Width, "her width is the roster's, not her word")
	assert.Equal(t, sprint.Up, f.Status, "on her session's pong, never on the beat")

	// a beat with none reports none: the last one's counts are not kept
	ta.beatUp("amy")
	f = whereFriends(ta)["amy"]
	assert.Nil(t, f.Report)
	assert.Zero(t, f.Load)

	for _, bad := range []string{"--working -1", "--queue x", "--width 0", "--load lots"} {
		code, _, errs := ta.do("friend beat amy " + bad)
		assert.Equal(t, 2, code, bad)
		assert.Contains(t, errs, "wants", bad)
	}
	// and the server runs it with them
	_, _, why := workerVerb([]string{"friend", "beat", "amy", "--working", "2", "--queue", "3", "--width", "4", "--load", "40"})
	assert.Empty(t, why)
	_, _, why = workerVerb([]string{"friend", "beat", "amy", "--width", "0"})
	assert.NotEmpty(t, why)
}

// friend level treats a one-shot friend as her width (one lane per unit of width, the owner
// 2026-10-10): bob, one-shot at width 2, takes both of amy's ready cards.
func TestFriendLevelRespectsOneShotDeliveryMode(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t)
	ta.a.tip = func(_ context.Context, _, _ string) (string, error) { return "", nil }
	_, err := cfg.Insert(context.Background(), config.KindFriend, config.Row{Name: "amy", Fields: map[string]string{"tiers": "flash", "width": "2"}}, "t")
	require.NoError(t, err)
	_, err = cfg.Insert(context.Background(), config.KindFriend, config.Row{Name: "bob", Fields: map[string]string{"tiers": "flash", "width": "2"}}, "t")
	require.NoError(t, err)

	ta.ok("friend sync")
	ta.beatUp("amy")
	ta.beatUp("bob")

	for i := 1; i <= 4; i++ {
		id := fmt.Sprintf("s1-%d", i)
		brief := filepath.Join(t.TempDir(), id+".md")
		require.NoError(t, os.WriteFile(brief, []byte(passingBrief(id+": a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend")), 0o644))
		ta.ok("add --stream s1 --brief-dir " + filepath.Dir(brief))
	}
	ta.ok("start")

	ta.ok("friend down bob")
	ta.ok("tick") // amy (batch, width 2) fills her room of 4, all ready, and starts 2
	ta.startFriend("amy", 2)
	ta.ok("friend sync")

	ta.ok("friend up bob")
	ta.beatUp("bob")

	out := ta.ok("friend level")
	// bob's room is his width's (one-shot as batch), so both of amy's ready cards move
	assert.Contains(t, out, "FRIEND-LEVEL OK moved=2")
	assert.Contains(t, ta.ok("friend level"), "FRIEND-LEVEL OK moved=0")

	f := whereFriends(ta)
	assert.Equal(t, 2, f["amy"].Working)
	assert.Equal(t, 0, f["amy"].Ready)
	assert.Equal(t, 0, f["bob"].Working, "ready on his row until he starts them")
	assert.Equal(t, 2, f["bob"].Ready)
	ta.clean()
}

// A friend's beat proves her session only by naming a check her daemon's run asked: a bare
// time (outside the server's first hour), a nonce never asked, another run's answer, or
// the same answer twice proves nothing (recorded as a beat with no proof); the answer to
// a check her daemon's run asked within fifteen minutes makes her up, and only while her
// beats go on. It does not stop a caller who beats as her from asking and answering at
// once (TestTheBeatTrustsItsCallersActor).
