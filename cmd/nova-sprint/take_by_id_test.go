package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// take --as <friend> <card>@<gen> is the lane take's move with the card named (docs/SPEC-SPRINT.md,
// take): the card goes to working as her start, so the next tick does not put it back ready and
// her progress is taken. The by-id and by-lane forms of one verb do the same thing (the owner,
// 2026-10-11: "How can we distribute work to friends, if you can't even distribute it to
// yourself?"; code read at nova-sprint main 1405d26).

// (i) a card dealt to friend.f, taken by id, moves to working with the taker recorded, and the
// tick that follows leaves it working (her take is her start), so progress is accepted.
func TestTakeByIDStartsAFriendsCardAsALaneTakeDoes(t *testing.T) {
	t.Parallel()
	ta, _, _ := friendReadyApp(t)
	ta.a.serveAddr = "mem:0"
	c := freshCard(ta, "s1-3")
	require.Equal(t, sprint.FriendRow("amy"), c.Work[0].Row)
	require.Equal(t, sprint.Ready, c.Work[0].Col, "ready on her row in front of a free lane")

	code, out := ta.served("take", "--as", "friend.amy", "s1-3.w1@1", "--epoch", "0")
	require.Equal(t, 0, code, out)
	c = freshCard(ta, "s1-3")
	require.Equal(t, sprint.Working, c.Work[0].Col, "her take moves it to working")
	assert.Equal(t, sprint.FriendRow("amy"), c.Work[0].Row, "the taker is recorded")
	assert.NotEmpty(t, c.Work[0].F("taken"), "taken now")
	assert.NotEmpty(t, c.Work[0].F(sprint.FieldStarted), "her take is her start, as a lane take's")

	// the tick does not put it back: a working card on her row with no start of hers would be
	// returned ready, and progress then refused "not working"
	ta.ok("tick")
	c = freshCard(ta, "s1-3")
	require.Equal(t, sprint.Working, c.Work[0].Col, "still working after the tick")
	code, out = ta.served("progress", "--as", "friend.amy", "s1-3.w1@1", "--epoch", "0")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "s1-3.w1 progress at")

	// asked again, the card is not taken again, and her finish by id is accepted at generation 1
	code, out = ta.served("take", "--as", "friend.amy", "s1-3.w1@1", "--epoch", "0")
	assert.Contains(t, out, "not in friend.amy ready (it is friend.amy:working)")
	ta.clean()
}

// (ii) the same with --lane <n>: the lane is recorded on the working card, a LANE line is said
// for it, and her finish naming the lane is accepted.
func TestTakeByIDNamesALaneAndFinishAcceptsIt(t *testing.T) {
	t.Parallel()
	ta, _, _ := friendReadyApp(t)
	ta.a.serveAddr = "mem:0"

	code, out := ta.served("take", "--as", "friend.amy", "s1-3.w1@1", "--epoch", "0", "--lane", "1")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "NOTE LANE friend.amy", "the by-id take says the lane, as a lane take does")
	c := freshCard(ta, "s1-3")
	require.Equal(t, sprint.Working, c.Work[0].Col)
	assert.Equal(t, "1", c.Work[0].F(sprint.FieldLane), "the lane is recorded")
	assert.NotEmpty(t, c.Work[0].F(sprint.FieldLaneAt), "the lane is heard from")

	code, out = ta.served("progress", "--as", "friend.amy", "s1-3.w1@1", "--epoch", "0", "--lane", "1")
	require.Equal(t, 0, code, out)
	code, out = ta.served("finish", "--as", "friend.amy", "s1-3.w1@1", "--head", landHead, "--report", "done", "--epoch", "0", "--lane", "1")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "s1-3.w1 work")
	ta.clean()
}

// (iii) a take naming a generation that is not the live one is refused, naming both, and nothing
// is moved. This pins the guard the by-id move keeps by going through takeOne (liveGen): a take
// that names a stale generation must not start the card.
func TestTakeByIDRefusesAStaleGenerationAndMovesNothing(t *testing.T) {
	t.Parallel()
	ta, _, _ := friendReadyApp(t)
	ta.a.serveAddr = "mem:0"

	code, out := ta.served("take", "--as", "friend.amy", "s1-3.w1@2", "--epoch", "0")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, out, "generation 2 is not the live one (1)", "it names both generations")
	c := freshCard(ta, "s1-3")
	assert.Equal(t, sprint.Ready, c.Work[0].Col, "nothing moved")
	ta.clean()
}
