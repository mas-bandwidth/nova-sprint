package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A friend's card taken back (docs/SPEC-SPRINT.md section 1; the owner, 2026-10-04: "sounds
// bad, we should fix this"): friend take and friend down, on the twin, with origin's tips
// injected (pushed names the branches origin holds) and no socket.

// takeApp is friendCardApp with more cards for any friend (s1-1 ... s1-n), amy at width 1,
// origin holding only the branches pushed names.
func takeApp(t *testing.T, n int, pushed map[string]string, friends ...string) (*testApp, string) {
	t.Helper()
	ta, root := friendCardApp(t, "friend", friends...)
	// Keep this transport test on friends; preference overflow is tested separately.
	ta.ok("fleet down m1")
	ta.ok("fleet down m2")
	ta.a.tip = func(_ context.Context, _, branch string) (string, error) { return pushed[branch], nil }
	for i := 2; i <= n; i++ {
		id := "s1-" + strconv.Itoa(i)
		brief := filepath.Join(t.TempDir(), id+".md")
		require.NoError(t, os.WriteFile(brief, []byte(passingBrief(id+": a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend")), 0o644))
		ta.ok("add --stream s1 --brief-dir " + filepath.Dir(brief))
	}
	return ta, root
}

// queueStates is a friend's queue file, card -> state.
func queueStates(t *testing.T, root, friend string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, friend+"-working", "inbox", "QUEUE.json"))
	require.NoError(t, err)
	var q friendQueue
	require.NoError(t, json.Unmarshal(b, &q))
	out := map[string]string{}
	for _, task := range q.Tasks {
		out[task.ID] = task.State
	}
	return out
}

func TestFriendTakeTakesBackAnUnstartedCardAndTheTickDealsItToAnother(t *testing.T) {
	t.Parallel()
	ta, root := takeApp(t, 1, nil, "amy", "bob")
	ta.ok("friend down bob")
	ta.ok("tick")
	var c cardView
	ta.json("card s1-1", &c)
	require.Equal(t, sprint.FriendRow("amy"), c.Work[0].Row)
	ta.ok("friend sync --root " + root)
	ta.ok("friend up bob")
	ta.beatUp("bob")

	// s1-1 is dealt to her and not taken (ready on her row): no --working is wanted
	dry := ta.dry("friend take amy s1-1 --dry-run")
	assert.Contains(t, dry, "FRIEND-TAKE OK DRY-RUN changes=1 props=0 refused=0; nothing was written")
	assert.Contains(t, dry, "WOULD s1-1.w1 withdrawn gen=2")
	out := ta.ok("friend take amy s1-1 --reason 'she is on another job'")
	assert.NotContains(t, out, "WARNING")
	assert.Contains(t, out, "s1-1.w1 withdrawn gen=2")
	assert.Contains(t, out, "FRIEND-TAKE OK moved=1 refused=0")
	ta.ok("friend sync --root " + root)
	assert.Equal(t, "taken", queueStates(t, root, "amy")["s1-1.w1"], "her queue file says it is not hers to start")

	ta.ok("tick") // the pump: its primary ready (only the pump writes the work table while it runs)
	ta.ok("tick")
	ta.json("card s1-1", &c)
	require.Len(t, c.Work, 1, "the same card, no second attempt")
	assert.Equal(t, sprint.FriendRow("bob"), c.Work[0].Row, "dealt to the other friend")
	assert.Equal(t, "3", c.Work[0].F("gen"))
	assert.Equal(t, sprint.Working, c.Primary.Col)
	out = ta.ok("friend sync --root " + root)
	assert.Contains(t, out, "FRIEND-CARD DELIVERED friend=bob card=s1-1.w1 job=s1-1.w1.g3 branch=sprint/s1-1.w1.g3.e0")
	assert.Equal(t, "queued", queueStates(t, root, "bob")["s1-1.w1"], "ready on his row until he starts it")
	ta.clean()
}

func TestFriendTakeRefusesAStartedCardAndAnotherFriendsCard(t *testing.T) {
	t.Parallel()
	ta, root := takeApp(t, 2, map[string]string{"sprint/s1-1.w1.g1.e0": landHead}, "amy", "bob")
	ta.ok("friend down bob")
	ta.ok("tick")
	ta.ok("friend sync --root " + root)

	// Default: take what you can, refuse the rest
	code, _, errs := ta.do("friend take amy s1-1 s1-2")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "REFUSED s1-1: s1-1.w1 has started: a push on its branch sprint/s1-1.w1.g1.e0 at "+landHead+"; it stays with friend amy and finishes")
	assert.Contains(t, errs, "FRIEND-TAKE FAILED moved=1 refused=1", "takes s1-2 and refuses s1-1")

	ta.ok("friend beat amy --running s1-2.w1")
	code, _, errs = ta.do("friend take amy s1-2")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "REFUSED s1-2: s1-2.w1 has started: friend amy finished it")

	code, _, errs = ta.do("friend take bob s1-2")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "REFUSED s1-2: s1-2.w1 is not dealt to friend bob")
	code, _, errs = ta.do("friend take cat s1-2")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "no friend cat on the friends table")
	// --all-or-nothing takes none when any is refused
	code, _, errs = ta.do("friend take amy --all-or-nothing s1-1 s1-2")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "REFUSED s1-1")
	assert.Contains(t, errs, "REFUSED s1-2")
	assert.Contains(t, errs, "FRIEND-TAKE FAILED moved=0 refused=2")
	ta.clean()
}

func TestFriendDownGivesBackEveryCardStartedOrNot(t *testing.T) {
	t.Parallel()
	ta, root := takeApp(t, 3, map[string]string{"sprint/s1-1.w1.g1.e0": landHead}, "amy")
	ta.ok("tick") // amy at width 8: all three on her row
	ta.ok("friend sync --root " + root)

	out := ta.ok("friend down amy")
	assert.Contains(t, out, "FRIEND-DOWN OK moved=3 refused=0")
	assert.Contains(t, out, "NOTE friend amy held")
	assert.NotContains(t, out, "keeps", "a held friend keeps no card")
	ta.ok("tick")
	var c cardView
	ta.json("card s1-1", &c)
	assert.Equal(t, sprint.Withdrawn, c.Work[0].Col, "the started one goes back too")
	ta.json("card s1-2", &c)
	assert.Equal(t, sprint.Withdrawn, c.Work[0].Col, "held: nothing is dealt to her")
	assert.Equal(t, sprint.Ready, c.Primary.Col)
	assert.Contains(t, c.Work[0].F(sprint.FieldTakenBack), "the hold of friend amy")

	// released and beating, her cards come back to her
	ta.ok("friend up amy")
	ta.beatUp("amy")
	ta.ok("tick")
	ta.json("card s1-2", &c)
	assert.Equal(t, sprint.FriendRow("amy"), c.Work[0].Row)
	assert.Equal(t, sprint.Working, c.Primary.Col)

	ta.clean()
}

// The server runs a friend's beat with what she reports, each flag once with its value,
// and nothing more.
func TestTheServerRunsAFriendsBeatWithTheCardsSheIsRunning(t *testing.T) {
	t.Parallel()
	as, words, why := workerVerb([]string{"friend", "beat", "amy", "--running", "s1-1.w1,s1-2.w1.g3"})
	assert.Equal(t, "", why)
	assert.Equal(t, "amy", as)
	assert.Equal(t, 2, words)
	for _, argv := range [][]string{
		{"friend", "beat", "amy", "--running"},
		{"friend", "beat", "amy", "--running", "a b"},
		{"friend", "beat", "amy", "--running", "x", "--running", "y"},
		{"friend", "beat", "amy", "--redis", "mem:0"},
	} {
		_, _, why := workerVerb(argv)
		assert.NotEmpty(t, why, "%q", argv)
	}
}

// friend take --all-unstarted never withdraws a working card (2026-10-14: it withdrew two
// friends' working cards mid-flight). amy at width 3 with five cards: three working, two
// ready and not taken. --dry-run lists exactly the two and writes nothing; the take moves
// exactly the two; a working card named wants --working, and with it moves and warns.
func TestFriendTakeAllUnstartedTakesOnlyTheCardsDealtAndNotTaken(t *testing.T) {
	t.Parallel()
	ta, root := takeApp(t, 5, nil, "amy")
	ta.ok("friend up amy --width 3")
	ta.ok("tick")
	ta.ok("friend sync --root " + root)
	// her lanes take three: s1-1..s1-3 working, s1-4 and s1-5 dealt and not taken
	for i := 1; i <= 3; i++ {
		ta.ok("take --as friend.amy s1-" + strconv.Itoa(i) + ".w1@1")
	}
	working, ready := map[string]bool{}, map[string]bool{}
	for i := 1; i <= 5; i++ {
		var c cardView
		ta.json("card s1-"+strconv.Itoa(i), &c)
		require.Len(t, c.Work, 1)
		require.Equal(t, sprint.FriendRow("amy"), c.Work[0].Row)
		switch c.Work[0].Col {
		case sprint.Working:
			working[c.Work[0].ID] = true
		case sprint.Ready:
			ready[c.Work[0].ID] = true
		}
	}
	require.Len(t, working, 3)
	require.Len(t, ready, 2)

	dry := ta.dry("friend take amy --all-unstarted --dry-run --reason rebalance")
	assert.Contains(t, dry, "FRIEND-TAKE OK DRY-RUN changes=2 props=0 refused=0; nothing was written")
	assert.Equal(t, 2, strings.Count(dry, "\nWOULD "), "exactly the two would move")
	for id := range ready {
		assert.Contains(t, dry, "WOULD "+id+" withdrawn")
	}
	for id := range working {
		assert.NotContains(t, dry, "WOULD "+id)
		assert.Contains(t, dry, "NOTE friend amy keeps "+id+": it is working on the friend's row")
	}

	out := ta.ok("friend take amy --all-unstarted --reason rebalance")
	assert.Contains(t, out, "FRIEND-TAKE OK moved=2 refused=0")
	assert.NotContains(t, out, "WARNING")
	for id := range working {
		assert.NotContains(t, out, id+" withdrawn", "a working card is never taken by --all-unstarted")
	}

	code, _, errs := ta.do("friend take amy --all-unstarted --working")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "--working withdraws named working cards only")

	var one string
	for id := range working {
		one = id
		break
	}
	code, _, errs = ta.do("friend take amy " + one)
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "is in flight with friend amy: it is working on the friend's row; withdrawing it mid-flight wants --working")
	out = ta.ok("friend take amy " + one + " --working")
	assert.Contains(t, out, "NOTE WARNING: withdraws 1 working card(s) from friend amy mid-flight: "+one)
	assert.Contains(t, out, one+" withdrawn")
	ta.clean()
}
