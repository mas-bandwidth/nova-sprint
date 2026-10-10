package main

import (
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
	assert.NotContains(t, out, "MOVED")

	// her progress names the server's epoch and her lane; another lane's is refused
	code, out = ta.served("progress", "--as", "friend.amy", "s1-3.w1@1", "--epoch", epoch, "--lane", "1")
	require.Equal(t, 0, code, out)
	code, out = ta.served("progress", "--as", "friend.amy", "s1-3.w1@1", "--epoch", epoch, "--lane", "3")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, out, "held by lane 1 of friend.amy, not lane 3")

	// a lane's take names nothing else
	code, out = ta.served("take", "--as", "friend.amy", "--lane", "1", "s1-4.w1@1")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, out, "names its member and its lane and nothing else")

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
func TestAStopReturnIsAWorkersVerbTheServerServes(t *testing.T) {
	t.Parallel()
	as, kind, why := workerVerb([]string{"stop-return", "--as", "friend.amy", "s1-3.w1@1", "--epoch", "0", "--reason", "stopped"})
	assert.Empty(t, why)
	assert.Equal(t, "friend.amy", as)
	assert.Equal(t, 1, kind)
}
