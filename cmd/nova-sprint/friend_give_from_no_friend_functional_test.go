//go:build functional

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFriendGiveFromNoFriendOnAStore replays the bounce the coordinator met on 2026-10-15 on a
// real store: a card on a friend's row, its brief replaced in flight, her finish bounces it
// (the work card Done with head_superseded, the primary ready, nothing written to
// taken_from), and `friend give` refused "<id>.w1 was never taken back from friend fay (taken
// from no friend)" on a card the friend deal would have dealt. tla/FriendGive.tla.
func TestFriendGiveFromNoFriendOnAStore(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	ctx := context.Background()
	require.NoError(t, fn.Load(ctx, c))

	ta := newFunctionalTestApp(t, addr)
	cfg := config.NewMem()
	_, err := cfg.Insert(ctx, config.KindFriend, config.Row{Name: "fay", Fields: map[string]string{
		"tiers": "flash", "roles": "builder,reader", "width": "2", "session": config.FriendSessionOneShot, "pronouns": "she/her"}}, "t")
	require.NoError(t, err)
	ta.app().friends = func(ctx context.Context, _ string) ([]config.Row, error) { return cfg.List(ctx, config.KindFriend) }
	ta.app().tip = tipIs(t, landHead)

	ta.runVerb("init", "--readers", "reader-a,reader-b", "--members", "m1,m2")
	ta.runVerb("friend", "sync", "--root", t.TempDir())
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s1-1.md"), []byte(passingBrief("s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend fay")), 0o644))
	ta.runVerb("add", "--stream", "s1", "--brief-dir", dir)
	var out, errb bytes.Buffer
	require.Zero(t, runOnStore(ta.a, ta.store, []string{"start"}, &out, &errb), "%s%s", out.String(), errb.String())
	ta.runVerb("fleet", "beat", "friend.fay", "--load", "5")
	ta.runVerb("tick")
	ta.runVerb("take", "--as", "friend.fay", "--limit", "1")

	// her card's brief is replaced in flight, and her finish bounces it
	newBrief := filepath.Join(t.TempDir(), "new.md")
	require.NoError(t, os.WriteFile(newBrief, []byte(passingBrief("s1-1: a different task\nREPO: mas-bandwidth/nova-tools\nWHO: friend fay")), 0o644))
	ta.runVerb("brief", "s1-1", "--replace-in-flight", "--brief-file", newBrief)
	ta.runVerb("tick") // a running machine applies a coordinator verb at its tick
	ta.runVerb("finish", "--as", "friend.fay", "s1-1.w1@1", "--epoch", "0", "--report", "the earlier task")
	// the coordinator holds her, so the deal places the card nowhere (as on 2026-10-15, no route served
	// it); a give to a friend not up is no refusal, the card waits on her row
	ta.runVerb("friend", "down", "fay")
	ta.runVerb("tick")

	var v cardView
	require.NoError(t, json.Unmarshal([]byte(ta.runVerb("card", "s1-1", "--json")), &v))
	require.Len(t, v.Work, 1)
	require.Equal(t, sprint.Finished, v.Work[0].Col, "the bounced work card is finished (the column the TLA module calls Done)")
	require.Empty(t, v.Work[0].F(sprint.FieldTakenFrom), "a bounce writes no taken_from")
	require.Equal(t, sprint.Ready, v.Column)

	// the card is handed to her at its next attempt (at base: the verbatim refusal)
	outG, code, errG := ta.runVerbOut("friend", "give", "fay", "s1-1")
	require.Zero(t, code, "friend give refused: %s%s", outG, errG)
	assert.Contains(t, outG, "FRIEND-GIVE OK moved=1 refused=0")
	assert.NotContains(t, errG, "was never taken back")
	ta.runVerb("tick") // a running machine applies a coordinator verb at its tick
	var after cardView
	require.NoError(t, json.Unmarshal([]byte(ta.runVerb("card", "s1-1", "--json")), &after))
	require.Equal(t, sprint.Working, after.Column)
	var w2 *sprint.Card
	for _, wc := range after.Work {
		if wc.ID == "s1-1.w2" {
			w2 = wc
		}
	}
	require.NotNil(t, w2, "the next attempt's work card")
	assert.Equal(t, sprint.FriendRow("fay"), w2.Row)

	// and now it is dealt: a second give is refused (the planner test pins the live work card's
	// own refusal, naming its row)
	_, code, errG = ta.runVerbOut("friend", "give", "fay", "s1-1")
	assert.Equal(t, 1, code)
	assert.Contains(t, errG, "not ready or waiting")
}
