package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A friend's lane asks the server what it holds and takes its next card in one verb, through
// the server as her daemon sends it, naming her row and its lane and nothing else (lanetake.go;
// nova-tools tla/FriendLane.tla). The faults of 2026-10-10 on a one-shot friend host: "took" logged while the
// server still showed the card ready (the take stamped no start of hers and the tick put it
// back), and progress sent at epoch 0 for a job whose directory named none.

var laneLineRE = regexp.MustCompile(`LANE friend\.amy lane=(\d+) epoch=(\d+) holds=(\S+)`)

func laneHolds(t *testing.T, out string) (lane, epoch, holds string) {
	t.Helper()
	m := laneLineRE.FindStringSubmatch(out)
	require.NotNil(t, m, "no LANE line: %s", out)
	return m[1], m[2], m[3]
}

func TestAFriendsLaneTakesThroughTheServerAndTheServerSaysWhatItHolds(t *testing.T) {
	t.Parallel()
	ta, _, _ := friendReadyApp(t)
	ta.a.serveAddr = "mem:0"

	// no epoch named: the server picks its own and says it
	code, out := ta.served("take", "--as", "friend.amy", "--lane", "3")
	require.Equal(t, 0, code, out)
	lane, epoch, holds := laneHolds(t, out)
	assert.Equal(t, "3", lane)
	assert.Equal(t, "0", epoch)
	// her two cards working from before lanes asked (no lane holds them): the oldest is lane 3's
	require.Equal(t, "s1-1.w1@1", holds, "a working card no lane held, given to lane 3")
	assert.Contains(t, out, "no lane held it")
	assert.Contains(t, out, "kind=work job=s1-1.w1")
	code, out = ta.served("take", "--as", "friend.amy", "--lane", "2")
	require.Equal(t, 0, code, out)
	_, _, two := laneHolds(t, out)
	require.Equal(t, "s1-2.w1@1", two)
	// her ready card, taken by the next lane: ready -> working, her start
	code, out = ta.served("take", "--as", "friend.amy", "--lane", "1")
	require.Equal(t, 0, code, out)
	_, _, one := laneHolds(t, out)
	require.Equal(t, "s1-3.w1@1", one)
	assert.Contains(t, out, "s1-3.w1 fleet ready -> working member=friend.amy gen=1 lane=1")

	// the tick keeps it working: the lane's take is her start
	ta.ok("tick")
	c := freshCard(ta, "s1-3")
	require.Equal(t, sprint.Working, c.Work[0].Col, "still working after the tick")
	assert.Equal(t, "1", c.Work[0].F(sprint.FieldLane))

	// asked again, the same answer and nothing moved
	code, out = ta.served("take", "--as", "friend.amy", "--lane", "1")
	require.Equal(t, 0, code, out)
	_, _, again := laneHolds(t, out)
	assert.Equal(t, one, again)
	assert.NotContains(t, out, "MOVED s1-3.w1 ", "nothing moved")
	assert.Contains(t, out, "MOVED lane 1 of friend.amy heard: it holds s1-3.w1", "the ask is the lane heard from")

	// her progress names the server's epoch and her lane; another lane's is refused
	code, out = ta.served("progress", "--as", "friend.amy", "s1-3.w1@1", "--epoch", epoch, "--lane", "1")
	require.Equal(t, 0, code, out)
	code, out = ta.served("progress", "--as", "friend.amy", "s1-3.w1@1", "--epoch", epoch, "--lane", "3")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, out, "held by lane 1 of friend.amy, not lane 3")

	// a lane's take that names a card is the by-id form: through the server it names the
	// epoch its worker holds, as a take by id does (the by-id and by-lane forms are one move)
	code, out = ta.served("take", "--as", "friend.amy", "--lane", "1", "s1-4.w1@1")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, out, "names the epoch its worker holds")

	// her finish by the server's ids: the lane holds nothing after it
	code, out = ta.served("finish", "--as", "friend.amy", "s1-3.w1@1", "--head", landHead, "--report", "done", "--epoch", epoch, "--lane", "1")
	require.Equal(t, 0, code, out)
	code, out = ta.served("take", "--as", "friend.amy", "--lane", "1")
	require.Equal(t, 0, code, out)
	_, _, after := laneHolds(t, out)
	assert.NotEqual(t, one, after, "the finished card is no longer the lane's")
	ta.clean()
}

// A worker's stop-return goes through the server (2026-10-10 21:55Z on a one-shot friend host: every one was
// refused "the server runs the workers' verbs only"): it is served as a worker's verb.
var laneFRE = regexp.MustCompile(`LANE friend\.f lane=(\d+) epoch=(\d+) holds=(\S+)`)

func laneHoldsF(t *testing.T, out string) (lane, epoch, holds string) {
	t.Helper()
	m := laneFRE.FindStringSubmatch(out)
	require.NotNil(t, m, "no LANE line: %s", out)
	return m[1], m[2], m[3]
}

// laneCard is one card in a lane order test: its stream and its id.
type laneCard struct{ stream, id string }

// laneRankApp is a friend f up at her default width with the named cards added to their
// streams, not yet dealt: a caller sets a priority or ranks a card and then ticks once, so
// every card lands on her row in the order the deal fills it, none started and every lane
// free.
func laneRankApp(t *testing.T, cards ...laneCard) *testApp {
	t.Helper()
	ta, _ := friendApp(t, "f")
	ta.a.tip = tipIs(t, landHead)
	root := t.TempDir()
	ta.ok("friend sync --root " + root)
	ta.beatUp("f")
	for _, c := range cards {
		dir := t.TempDir()
		brief := filepath.Join(dir, c.id+".md")
		body := passingBrief(c.id + ": a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend f")
		require.NoError(t, os.WriteFile(brief, []byte(body), 0o644))
		ta.ok("add --stream " + c.stream + " --brief-dir " + dir)
	}
	ta.ok("start")
	return ta
}

// A lane take honours the rank, and the rank never jumps the priority ladder: a rank --first
// card leads its stream's score line, but the lane's next card is the critical one. The deal
// fills a friend's dealt set and the lane take follows the one order (the ladder, then the
// stream turns, then work order), so it never draws a rank-first card over a critical one
// (docs/SPEC-SPRINT.md, rank and take). Three cards are dealt to friend.f in dealt order
// s1-1, s1-2, s1-3; s1-3 is ranked first and s1-1 is critical.
func TestALaneTakeHonoursRankFirstBeforeDealtOrder(t *testing.T) {
	t.Parallel()
	ta := laneRankApp(t,
		laneCard{"s1", "s1-1"}, laneCard{"s1", "s1-2"}, laneCard{"s1", "s1-3"})
	ta.ok("priority s1-1 --critical --reason 'the release waits on it'")
	ta.ok("rank s1-3 --first")
	ta.ok("tick")
	ta.a.serveAddr = "mem:0"
	code, out := ta.served("take", "--as", "friend.f", "--lane", "1")
	require.Equal(t, 0, code, out)
	_, _, holds := laneHoldsF(t, out)
	assert.Equal(t, "s1-1.w1@1", holds, "the critical card, not the rank-first s1-3")
}

// Priority is honoured before the dealt order: a critical card dealt last is taken first.
// The deal offers a friend her cards by the ladder (priority.go); the lane take follows the
// same order, so the critical card is no longer passed over (docs/SPEC-SPRINT.md, rank and
// take).
func TestALaneTakeHonoursPriorityBeforeDealtOrder(t *testing.T) {
	t.Parallel()
	ta := laneRankApp(t,
		laneCard{"s1", "s1-1"}, laneCard{"s1", "s1-2"}, laneCard{"s1", "s1-3"})
	ta.ok("priority s1-3 --critical --reason 'the release waits on it'")
	ta.ok("tick")
	ta.a.serveAddr = "mem:0"
	code, out := ta.served("take", "--as", "friend.f", "--lane", "1")
	require.Equal(t, 0, code, out)
	_, _, holds := laneHoldsF(t, out)
	assert.Equal(t, "s1-3.w1@1", holds, "the critical card, dealt last")
}

// Across streams the lane take follows the order the deal uses: priority first, then the
// stream order, then the work order within a stream (rank). A critical card of a later
// stream goes before a normal card of an earlier one (docs/SPEC-SPRINT.md, rank and take).
func TestALaneTakeFollowsTheDealsOrderAcrossStreams(t *testing.T) {
	t.Parallel()
	ta := laneRankApp(t,
		laneCard{"s1", "s1-1"}, laneCard{"s2", "s2-1"}, laneCard{"s2", "s2-2"})
	// s2-2 is dealt late and sits behind an earlier normal card of the earlier stream s1;
	// its critical level outranks both.
	ta.ok("priority s2-2 --critical --reason 'the release waits on it'")
	ta.ok("tick")
	ta.a.serveAddr = "mem:0"
	code, out := ta.served("take", "--as", "friend.f", "--lane", "1")
	require.Equal(t, 0, code, out)
	_, _, holds := laneHoldsF(t, out)
	assert.Equal(t, "s2-2.w1@1", holds, "the critical card of the later stream")
}

func TestAStopReturnIsAWorkersVerbTheServerServes(t *testing.T) {
	t.Parallel()
	as, kind, why := workerVerb([]string{"stop-return", "--as", "friend.amy", "s1-3.w1@1", "--epoch", "0", "--reason", "stopped"})
	assert.Empty(t, why)
	assert.Equal(t, "friend.amy", as)
	assert.Equal(t, 1, kind)
}
