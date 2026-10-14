package store

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestStreamNeverAutoArchives verifies that the tick does not archive streams
// when their last card lands. Streams are archived only by the explicit
// stream archive command (docs/SPEC-SPRINT.md section 16; the owner,
// 2026-10-11: archiving is a manual request only).
func TestStreamNeverAutoArchives(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.m.SetMachineWidth("m1", strconv.Itoa(2))
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1"}))
	h.must(AddStep(sprint.AddReq{Stream: "s1", Count: 1, Brief: proBrief}))
	h.startMachine()
	h.landThrough("s1", "s1-1")
	// Two ticks pass: the stream should still be drawn, not archived
	h.machine()
	h.machine()
	require.False(t, h.table().Work.Hidden("s1"),
		"every card landed but the tick never archives: stream stays drawn")
	// The explicit verb still archives it
	refused, err := h.st.ArchiveStreams(h.ctx, []string{"s1"})
	require.NoError(t, err)
	require.Empty(t, refused)
	require.True(t, h.table().Work.Hidden("s1"), "stream archive archives it")
}

// TestArchivedStreamDrawsOnNewWork verifies that an archived stream is drawn
// again when it takes a new card, whether the machine runs or not.
func TestArchivedStreamDrawsOnNewWork(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.m.SetMachineWidth("m1", strconv.Itoa(2))
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1"}))
	h.must(AddStep(sprint.AddReq{Stream: "s1", Count: 1, Brief: proBrief}))
	h.must(AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"gate"}, Sentinel: true}))
	h.startMachine()
	h.landThrough("s1", "s1-1")
	h.must(ReleaseStep(sprint.ReleaseReq{IDs: []string{"gate"}, Reason: "the release is ready to cut", Coordinator: h.st.Actor, Who: h.st.Actor}))
	h.machine()
	h.machine()
	// Manually archive the stream
	refused, err := h.st.ArchiveStreams(h.ctx, []string{"s1"})
	require.NoError(t, err)
	require.Empty(t, refused)
	require.True(t, h.table().Work.Hidden("s1"), "stream archive archives it")
	// Add a new card: the next tick draws it again
	h.must(AddStep(sprint.AddReq{Stream: "s1", Count: 1, Brief: proBrief}))
	h.machine()
	require.False(t, h.table().Work.Hidden("s1"),
		"new card on archived stream: tick draws it again")
}

// TestNoStreamArchivedNote verifies that the tick does not write
// NStreamArchived notes when streams land.
func TestNoStreamArchivedNote(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.m.SetMachineWidth("m1", strconv.Itoa(2))
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1"}))
	h.must(AddStep(sprint.AddReq{Stream: "s1", Count: 1, Brief: proBrief}))
	h.startMachine()
	h.landThrough("s1", "s1-1")
	h.machine()
	// Check that no streams archived note was written
	res, err := h.st.B.ReadFence(h.ctx)
	require.NoError(t, err)
	require.Less(t, res.Queued, 1, "no notes queued by the tick")
}
