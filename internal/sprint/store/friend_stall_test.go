package store

import (
	"strconv"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// On the twin store a friend whose beats name running jobs, and who sends no session
// activity, is never stalled through an hour of the machine's ticks, while a friend
// beside her holding dealt cards with no evidence of work climbs the ladder
// (docs/SPEC-SPRINT.md section friend-stall-ladder-r.w1, evidence of work;
// tla/StallLadder.tla, NeverDownWhileWorking).
func TestTwinStoreFriendBeatingRunningJobsIsNeverStalled(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{
		{Name: "bob", Width: 2, Class: "flash,pro"},
		{Name: "freddy", Width: 2, Class: "flash,pro"},
	})
	require.NoError(t, err)
	for _, f := range []string{"bob", "freddy"} {
		_, err := h.st.FriendBeat(h.ctx, f)
		require.NoError(t, err)
	}
	h.must(AddStep(sprint.AddReq{Stream: "s1", Cards: friendCards("s1", 4)}))
	h.startMachine()
	h.machine()
	snap := h.snap()
	require.Equal(t, 2, snap.Fleet.Count(sprint.FriendRow("freddy"), sprint.Working))
	require.Equal(t, 2, snap.Fleet.Count(sprint.FriendRow("bob"), sprint.Working))

	bobClimbed := 0
	for m := 5; m <= 60; m += 5 {
		h.tick(5 * time.Minute)
		// freddy's daemon beats naming a one-shot lane job running, and no session write
		_, err := h.st.FriendBeatReport(h.ctx, "freddy", sprint.FriendReport{Running: []string{"lane-1/job-" + strconv.Itoa(m)}}, nil)
		require.NoError(t, err)
		// bob is awake and reports nothing: awake is not working
		_, err = h.st.FriendBeat(h.ctx, "bob")
		require.NoError(t, err)
		h.machine()
		snap := h.snap()
		rung, _ := snap.Fleet.Prop(sprint.PropFriendStallRung("freddy"))
		assert.Empty(t, rung, "minute %d: freddy stays at rung 0", m)
		down, _ := snap.Fleet.Prop(sprint.PropFriendStallDown("freddy"))
		assert.Empty(t, down, "minute %d: freddy is never marked down", m)
		assert.Equal(t, 2, snap.Fleet.Count(sprint.FriendRow("freddy"), sprint.Working), "minute %d: nothing of freddy's taken back", m)
		if r, _ := snap.Fleet.Prop(sprint.PropFriendStallRung("bob")); r != "" {
			n, _ := strconv.Atoi(r)
			bobClimbed = max(bobClimbed, n)
		}
	}
	assert.GreaterOrEqual(t, bobClimbed, 1, "bob, with dealt cards and no evidence, climbs")
}
