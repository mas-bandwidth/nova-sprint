package store

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// writesOf is how many times a type has reached the coordinator: its first note and
// every rewrite in place (a log line "updated"), the two ways a standing judgment is
// written again.
func (h *harness) writesOf(typ string) int {
	h.t.Helper()
	lines, _, err := h.m.LogSince(h.ctx, "", 100000)
	require.NoError(h.t, err)
	n := 0
	for _, l := range lines {
		if l.Note != nil && l.Note.Type == typ && l.Note.Kind != sprint.Decided && l.Note.Kind != sprint.Acknowledged {
			n++
		}
	}
	return n
}

// A standing judgment is one judgment, keyed by its type and subject, pushed once when
// it rises and again only when its own numbers move: the readers behind is rewritten in
// place when they move, but review's count moves every tick as cards flow through it, and
// a rewrite for that alone is a push a second, the noise of 2026-10-10 (j407, j409, j410
// within a minute). Ten ticks with the readers behind and their numbers unchanged, the
// review queue moving under them, are one judgment and one push (docs/SPEC-SPRINT.md,
// "the readers are behind": one for the sprint, keyed by its type and subject, rewritten
// in place only when the readers' numbers move; the owner: pushed again only when the
// readers' numbers move, not for review).
func TestAStandingReadersBehindJudgmentIsPushedOnce(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	require.NoError(t, h.m.RowsAdd(h.ctx, "t-readers", []string{"reader-m1"}))
	h.beat()
	for _, r := range []string{"reader-a", "reader-b", "reader-c"} {
		require.NoError(t, h.st.SetReaderAway(h.ctx, r, true, "tester"))
	}
	h.must(FleetStep(sprint.FleetReq{Op: "up", Member: "m1", Width: 6}))
	h.must(FleetStep(sprint.FleetReq{Op: "hold", Member: "m2"}))
	h.addReady("s1", 6, briefOf("flash", ""))
	h.startMachine()
	h.machine()
	for i := 1; i <= 6; i++ {
		id := "s1-" + string(rune('0'+i))
		h.machine()
		h.finishAttempt(id, false, "h"+id)
	}
	h.machine()
	require.Len(t, h.snap().Readers.Cell("reader-m1", sprint.Asked), 6, "six reads asked of the one reader up")
	h.must(ReadStep(sprint.ReadReq{As: "reader-m1", Begin: true, Sel: sprint.Sel{Limit: 2}, Who: "reader-m1"}))
	h.machine()
	h.tick(sprint.ReadersWindow)
	h.machine()
	require.Len(t, h.openOf(sprint.NReadersBehind), 1, "the judgment rose once the window passed")
	require.Equal(t, 1, h.writesOf(sprint.NReadersBehind), "one push when it rose")
	// ten ticks with review moving under the readers: a work card arrives and fails each
	// tick (no read is asked, so the readers' numbers hold), the queue grows, and the
	// standing judgment is neither written a second time nor pushed again
	for i := 2; i <= 11; i++ {
		stream := fmt.Sprintf("s%d", i)
		h.addReady(stream, 1, briefOf("flash", ""))
		h.machine()
		h.finishAttempt(stream+"-1", true, "")
		h.machine()
		require.Len(t, h.openOf(sprint.NReadersBehind), 1, "tick %d: one judgment over the moving queue", i-1)
		require.Equal(t, 1, h.writesOf(sprint.NReadersBehind), "tick %d: review moved and the readers did not: the standing judgment is not pushed again", i-1)
	}
	require.Equal(t, 16, len(h.snap().Work.Column(sprint.Review)), "the review queue moved under the judgment")
	// the readers' own numbers move (the reader's width, 6 to 3): a material change, and
	// the standing judgment is rewritten in place and pushed again
	h.must(FleetStep(sprint.FleetReq{Op: "up", Member: "m1", Width: 3}))
	h.machine()
	require.Len(t, h.openOf(sprint.NReadersBehind), 1, "still one judgment")
	require.Equal(t, 2, h.writesOf(sprint.NReadersBehind), "the reader's width moved: rewritten in place and pushed again")
	require.Contains(t, h.openOf(sprint.NReadersBehind)[0].Note.What, "of width 3", "the rewrite carries the latest numbers")
}

// The readers are behind (readers_behind.go): reads asked and not begun on a reader up for
// the window raise one judgment naming review, each reader's reading beside its width (its
// machine row's, Snapshot.ReaderWidth) and what it needs; a reader reading under its width,
// a loop that kept the width it started with, is restarted; a reader reading its width waits
// on its machine; the judgment closes when the reads begin. Injected clock.
func TestTheTickRaisesReadersBehindWhenReadsWaitTheWindow(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	require.NoError(t, h.m.RowsAdd(h.ctx, "t-readers", []string{"reader-m1"}))
	readersBehind(h, h.openOf, h.written)
}

// readersBehind drives the readers behind on a harness whose members m1 and m2 are up, with
// a flash route and the reader reader-m1: open and written read the store's judgments (the
// Mem's or the Redis's, readers_behind_functional_test.go).
func readersBehind(h *harness, open func(string) []sprint.Open, written func(string) int) {
	h.readersTable() // this fixture consumes standalone readers

	t := h.t
	h.beat()
	for _, r := range []string{"reader-a", "reader-b", "reader-c"} {
		require.NoError(t, h.st.SetReaderAway(h.ctx, r, true, "tester"))
	}
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 6}))
	h.must(FleetStep(sprint.FleetReq{Op: "hold", Member: "m2"}))
	h.addReady("s1", 6, briefOf("flash", ""))
	h.startMachine()
	h.machine()
	for i := 1; i <= 6; i++ {
		id := "s1-" + string(rune('0'+i))
		h.machine()
		h.finishAttempt(id, false, "h"+id)
	}
	h.machine()
	require.Len(t, h.snap().Readers.Cell("reader-m1", sprint.Asked), 6, "six reads asked of the one reader up, its width six")
	// its loop reads two, the width it started with
	h.must(ReadStep(sprint.ReadReq{As: "reader-m1", Begin: true, Sel: sprint.Sel{Limit: 2}, Who: "reader-m1"}))
	h.machine()
	assert.Empty(t, open(sprint.NReadersBehind), "not yet the window")
	h.tick(sprint.ReadersWindow)
	h.machine()
	behind := open(sprint.NReadersBehind)
	require.Len(t, behind, 1)
	assert.Equal(t, "the readers are behind: review 6, reads asked and not begun past 10m0s; the readers read 2 of width 6 (reader-m1 reads 2 of width 6, 4 waiting past the window: it reads under its width, restart its loop (nova-config loop show reader-m1))", behind[0].Note.What)
	assert.Equal(t, []string{"restart reader-m1", "wait 10m"}, behind[0].Note.Decisions)
	cmds := h.commandsOf(sprint.NReadersBehind)
	require.Len(t, cmds, 2)
	assert.Contains(t, cmds[0].Lines[0], "nova-config loop show reader-m1")
	assert.Equal(t, "nova-sprint wait "+behind[0].Note.ID+" --for 10m", cmds[1].Lines[0])
	// the machine is narrowed to two: the reader reads its whole width, busy and not behind
	// (a-judgment-checks-the-lane-before-it-rises.w1): the judgment closes, and the tick
	// counts it quiet on its heartbeat
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 2}))
	h.machine()
	assert.Empty(t, open(sprint.NReadersBehind), "a reader reading its whole width is busy, not behind")
	assert.Equal(t, 1, written(sprint.NReadersBehind), "written once, before the narrowing")
	_, late := sprint.ReadersBehind(h.snap())
	assert.False(t, late)
	why, full := sprint.ReadersFull(h.snap())
	require.True(t, full)
	assert.Equal(t, "4 reads asked past 10m0s wait on readers reading their whole width (reader-m1 reads 2 of width 2): busy, not behind", why)
	_, hb, err := h.st.Machine(h.ctx)
	require.NoError(t, err)
	assert.Contains(t, hb.Quiet, sprint.LaneQuiet{Type: sprint.NReadersBehind, Subject: sprint.SprintSubject, Why: why}, "the heartbeat counts it")
	// widened again with the reads still waiting: a reader with room did not begin, and it rises
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 6}))
	h.machine()
	require.Len(t, open(sprint.NReadersBehind), 1, "room and not begun: behind again")
	// the reader begins the rest: the readers are not behind
	h.must(ReadStep(sprint.ReadReq{As: "reader-m1", Begin: true, Sel: sprint.Sel{Limit: 4}, Who: "reader-m1"}))
	h.machine()
	assert.Empty(t, open(sprint.NReadersBehind))
	_, late = sprint.ReadersBehind(h.snap())
	assert.False(t, late)
	h.clean("the readers caught up")
}
