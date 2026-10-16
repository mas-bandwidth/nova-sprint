package store

import (
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func (h *harness) seat() SeatState {
	h.t.Helper()
	s, err := h.st.SeatState(h.ctx)
	require.NoError(h.t, err)
	return s
}

// give moves the seat from its holder to name, as the holder.

func (h *harness) give(from, to string) {
	h.t.Helper()
	res, err := h.st.Run(h.ctx, SeatStep(sprint.SeatReq{To: to, Who: from, Reason: "handover"}))
	require.NoError(h.t, err)
	require.Empty(h.t, res.Refused, "the seat moves")
}

// health is one observation by who, at the harness's clock.

func (h *harness) friendStatus(name string) string {
	h.t.Helper()
	// the server's pass first: FriendRows shows the word the server wrote
	require.NoError(h.t, h.st.PublishFriendStatus(h.ctx, h.now))
	rows, err := h.st.FriendRows(h.ctx, h.now)
	require.NoError(h.t, err)
	for _, r := range rows {
		if r.Name == name {
			return r.Status
		}
	}
	require.Fail(h.t, "no row for "+name)
	return ""
}

// The initial seat has generation 1 and no record; a step that is not the
// seat's keeps it; each accepted handover takes the next; the same op again
// is the recorded result, not a second increment.

func TestSeatGenerationFromInitThroughHandovers(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	assert.Equal(t, SeatState{Holder: "tester", Epoch: 0, Generation: 1}, h.seat(), "the first init's seat, no record")
	_, moved, err := h.st.Seat(h.ctx)
	require.NoError(t, err)
	assert.False(t, moved)

	h.must(AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"a"}}))
	assert.Equal(t, uint64(1), h.seat().Generation, "an unrelated write keeps the generation")

	step := SeatStep(sprint.SeatReq{To: "stella", Who: "tester", Reason: "handover"})
	step.CallerOp = "seat-1"
	res, err := h.st.Run(h.ctx, step)
	require.NoError(t, err)
	require.Empty(t, res.Refused)
	assert.Equal(t, SeatState{Holder: "stella", Generation: 2}, h.seat())
	res, err = h.st.Run(h.ctx, step)
	require.NoError(t, err)
	assert.True(t, res.Replay, "the same op again replays")
	assert.Equal(t, uint64(2), h.seat().Generation, "no double increment")

	h.give("stella", "tester")
	assert.Equal(t, SeatState{Holder: "tester", Generation: 3}, h.seat(), "A->B->A: A's second seat is generation 3")
	last, moved, err := h.st.Seat(h.ctx)
	require.NoError(t, err)
	assert.True(t, moved)
	assert.Equal(t, uint64(3), last.Generation, "the record carries it")
}

// A first valid observation makes the friend up at once; at exactly ten
// seconds with no newer proof she is down; a newer proof restores her.
