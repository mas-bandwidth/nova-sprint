package sprintdash

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheCopyReadsPerMachineLanes: the `lanes` array `nova-sprint where --json --cards`
// prints (verb-lane-take-give) is read into the copy, each machine's holders and waiters.
// The live page (live_test.go) draws no Lanes panel, so the lanes are data alone: the pull
// routes' /api/sprint carries them whole. The fixture is a where --json --cards body with
// lanes.
func TestTheCopyReadsPerMachineLanes(t *testing.T) {
	t.Parallel()
	c := copyOf(t, fixture(t))
	require.Len(t, c.Lanes, 1, "the fixture carries one machine's lanes")
	assert.Equal(t, LaneRow{Kind: "go", Machine: "bench-a", Width: 1, Held: []string{"amy"}, Waiting: []string{"bob"}}, c.Lanes[0],
		"the machine's lane: its kind, width, holder and waiter")
}
