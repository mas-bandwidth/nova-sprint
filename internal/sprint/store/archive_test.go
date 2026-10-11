package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A sentinel is a stop the coordinator releases, not work: releasing one never
// archives its stream (docs/SPEC-SPRINT.md sections 1 and 16; the owner,
// 2026-10-11: archiving is a manual request only). The tick leaves a stream
// holding a sentinel drawn however many of its cards have landed; the explicit
// stream archive still takes it off the table.
func TestAReleasedSentinelNeverArchivesItsStream(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 2}))
	h.must(AddStep(sprint.AddReq{Stream: "s1", Count: 1, Brief: proBrief}))
	h.must(AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"gate"}, Sentinel: true}))
	h.startMachine()
	h.landThrough("s1", "s1-1")
	h.must(ReleaseStep(sprint.ReleaseReq{IDs: []string{"gate"}, Reason: "the release is ready to cut", Coordinator: h.st.Actor, Who: h.st.Actor}))
	h.machine()
	h.machine()
	require.False(t, h.table().Work.Hidden("s1"),
		"every card landed, but s1 holds a sentinel: the tick leaves it drawn")

	// the explicit verb still archives it
	refused, err := h.st.ArchiveStreams(h.ctx, []string{"s1"})
	require.NoError(t, err)
	require.Empty(t, refused)
	require.True(t, h.table().Work.Hidden("s1"), "stream archive archives it")
}
