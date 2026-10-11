package store

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A primary in review with no read out and an empty readers table is never a
// silent idle: the tick that finds it raises one judgment, "review has no
// reader" (NReviewNoReader). This is the sprint a clear leaves when its
// readers are gone: it must still tell the coordinator.
func TestAReviewCardWithNoReaderRaisesTheJudgmentInOneTick(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	// a store with no reader row at all: init adds none, and nothing else has
	h.m = NewMem()
	h.st.B = h.m
	require.NoError(t, h.st.Init(h.ctx))
	h.beat()
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1"}))
	h.must(AddStep(sprint.AddReq{Brief: proBrief, Stream: "s1", Count: 1}))
	h.startMachine()
	h.machine()
	h.work("m1")
	h.machine()
	require.Equal(t, string(sprint.Review), h.state("s1-1"), "the finished card is in review")
	open := h.openOf(sprint.NReviewNoReader)
	require.Len(t, open, 1, "review has no reader within one tick: %v", open)
	require.Contains(t, open[0].Note.What, "readers table is empty", "the judgment names the cause: %s", open[0].Note.What)
	require.Empty(t, h.openOf(sprint.NFewReaders), "no reader at all is review-has-no-reader, not fewer-than-two")
}

// C4: a tick in flight at a clear, wherever the clear lands in it, writes
// nothing at the new epoch (nor at the old one after the clear): it stops
// that pass, and the machine is STOPPED.
func TestATickInFlightAtAClearWritesNothing(t *testing.T) {
	t.Parallel()
	for _, at := range []struct {
		kind string
		n    int
	}{{"fence", 1}, {"shapes", 1}, {"shapes", 2}, {"fence", 3}, {"fence", 4}, {"acquire", 1}, {"apply", 1}, {"release", 1}} {
		t.Run(at.kind+"-"+itoa(uint64(at.n)), func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.setup(4)
			h.startMachine()
			var img string
			var cleared ClearResult
			loop := *h.st
			loop.Actor = sprint.MachineActor
			loop.B = &clearOnCall{Backend: h.m, kv: h.m, kind: at.kind, n: at.n, clear: func() {
				var err error
				cleared, err = h.st.Clear(h.ctx)
				assert.NoError(t, err, "clear: %v", err)
				img = withoutMachine(h.image())
			}}
			res, err := loop.Tick(h.ctx)
			require.NotEmpty(t, img, "the clear never landed in the tick: %+v %v", res, err)
			require.NoError(t, err, "the tick at a clear: stale %q parts %+v err %v", res.Stale, res.Parts, err)
			require.NotEmpty(t, res.Stale, "the tick at a clear: stale %q parts %+v err %v", res.Stale, res.Parts, err)
			got := withoutMachine(h.image())
			require.Equal(t, img, got, "the tick wrote after the clear:\n%s\nwas\n%s", got, img)
			if m, _, _ := h.st.Machine(h.ctx); m.Running() || cleared.To != 1 {
				require.Failf(t, "", "after the clear: machine %s, %+v", m.StateWord(), cleared)
			}
			h.clean("after the tick")
		})
	}
}
