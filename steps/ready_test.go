package steps

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTickReadyTimeout: a work consumer ready past timeouts() ready_take is
// withdrawn and its producer dealt, in the same op, to the next row with
// room, never the slow row; one under the deadline is left alone (Glenn
// 2026-11-07: "By leaving cards in ready we are violating this promise").
func TestTickReadyTimeout(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.row("r2", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(2)
	s.requireState("p1.w1", "ready")
	slow := s.card("p1.w1").Holder
	s.now = s.now.Add(time.Duration(Engine.Timeouts.ReadyTake-2) * time.Second)
	s.tick()
	require.Empty(t, s.tickOps(), "under the deadline: left alone")
	s.requireState("p1.w1", "ready")
	s.now = s.now.Add(2 * time.Second)
	s.tick()
	require.Empty(t, s.rejected)
	require.Len(t, s.tickOps(), 1, "the withdraw and the re-deal are one op")
	s.requireState("p1.w1", "fail")
	assert.Regexp(t, `^ready 18\d s on `+slow+`, not taken$`, s.card("p1.w1").Reason)
	s.requireState("p1", "working")
	s.requireState("p1.w2", "ready")
	assert.NotEqual(t, slow, s.card("p1.w2").Holder, "the slow row is excluded")
}
