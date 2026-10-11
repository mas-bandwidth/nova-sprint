package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/hostload"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Working means a live run (v1.2.6; tla/LiveRuns.tla). 2026-10-10: the sprint STOPPED at
// ~15:00Z showed 30 working on six fleet hosts and 29 on friend.zhi at ~18:25Z with no
// harness run anywhere; and a stop-returned card stayed STOP-owned, so hold refused its row.

// liveBeat is one beat of member carrying its live set (fleet beat --live), at the clock now.
func (h *harness) liveBeat(member string, live ...string) sprint.Beat {
	h.t.Helper()
	zero := 0.0
	set := []string{}
	if len(live) > 0 {
		set = live
	}
	b, err := h.st.BeatLive(h.ctx, member, &zero, hostload.Source{}, nil, "", set)
	require.NoError(h.t, err)
	return b
}

// clock moves the clock alone: no member beats (harness.tick beats every live member with
// no live set, which leaves its row unreconciled).
func (h *harness) clock(d time.Duration) {
	h.mu.Lock()
	h.now = h.now.Add(d)
	h.mu.Unlock()
}

// taken is the setup's first work card taken by its member, at its generation.
func (h *harness) taken() (*sprint.Card, int) {
	h.t.Helper()
	h.setup(1)
	h.startMachine()
	h.machine()
	s := h.snap()
	wc := s.Fleet.Card(s.Work.Card("s1-1").F("work"))
	require.NotNil(h.t, wc)
	gen := max(wc.Int("gen"), 1)
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: gen}}))
	require.Equal(h.t, sprint.Working, h.snap().Fleet.Card(wc.ID).Col)
	return wc, gen
}

// The STOPPED machine reconciles (Defect A; tla/LiveRuns.tla WorkingIsLive, Broken
// "stoppednoreconcile"): a take its member's live set stops naming goes back ready within
// the grace, with the STOP's receipt, and start then finds the debt settled.
func TestAStoppedTickReturnsATakeWhoseRunIsGone(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	wc, gen := h.taken()
	h.liveBeat(wc.Row, sprint.LiveKey(wc.ID, gen))
	_, _, _, err := h.st.StopUntil(h.ctx, "operator stop", h.now.Add(time.Hour))
	require.NoError(t, err)

	h.clock(30 * time.Second)
	h.liveBeat(wc.Row) // its child is gone: the live set is empty
	res := h.machine()
	assert.Equal(t, Stopped, res.State)
	assert.Equal(t, sprint.Working, h.snap().Fleet.Card(wc.ID).Col, "inside the grace a take stays working")

	h.clock(sprint.LiveGrace)
	h.liveBeat(wc.Row)
	res = h.machine()
	back := h.snap().Fleet.Card(wc.ID)
	require.NotNil(t, back)
	assert.Equal(t, sprint.Ready, back.Col, "a STOPPED tick returns the take whose run is gone")
	assert.Equal(t, wc.Row, back.Row)
	assert.Equal(t, gen+1, back.Int("gen"))
	assert.Equal(t, gen, back.Int("stopped_from_gen"), "the return is the STOP's receipt")
	assert.Equal(t, gen, back.Int("lost_from_gen"))
	assert.Contains(t, back.F("lost_reason"), "no live run")
	require.NotEmpty(t, res.Parts)
	assert.Equal(t, sprint.PartLive, res.Parts[0].Name)
	_, running, _, err := h.st.SetMachine(h.ctx, true)
	require.NoError(t, err, "the reconcile's receipt settles the STOP debt")
	assert.True(t, running.Running())
}

// A running machine reconciles as one of its tick's parts (TickLive): the take is returned
// at its next generation, the old run's finish is fenced.
func TestARunningTickReturnsATakeWhoseRunIsGone(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	wc, gen := h.taken()
	h.liveBeat(wc.Row, sprint.LiveKey(wc.ID, gen))
	h.clock(sprint.LiveGrace + time.Second)
	h.liveBeat(wc.Row)
	h.machine()
	back := h.snap().Fleet.Card(wc.ID)
	require.NotNil(t, back)
	assert.Greater(t, back.Int("gen"), gen, "the take whose run is gone is returned at its next generation")
	assert.Equal(t, gen, back.Int("lost_from_gen"))
	assert.Empty(t, back.F("stopped_from_gen"), "a running machine's return is no STOP receipt")
	late := h.run(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: gen}}))
	assert.NotEmpty(t, late.Refused, "the gone run's late finish is stale")
}

// A take the live set names is never returned, however long it runs; and a member whose
// beat carries no live set (a worker from before v1.2.6) is left as it was.
func TestALiveTakeAndAnUnreportingMemberAreLeftWorking(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	wc, gen := h.taken()
	_, _, _, err := h.st.StopUntil(h.ctx, "operator stop", h.now.Add(time.Hour))
	require.NoError(t, err)
	for range 5 {
		h.clock(sprint.LiveGrace)
		h.liveBeat(wc.Row, sprint.LiveKey(wc.ID, gen))
		h.machine()
	}
	assert.Equal(t, sprint.Working, h.snap().Fleet.Card(wc.ID).Col, "a live run stays working")
	h.clock(10 * sprint.LiveGrace)
	h.beat() // no live set: the row is not reconciled
	h.machine()
	assert.Equal(t, sprint.Working, h.snap().Fleet.Card(wc.ID).Col, "a beat with no live set is no evidence")
}

// A silent member is no evidence (tla/LiveRuns.tla Silence, Broken "staleasabsent"; the cold
// read of the first cut): a member whose beats stop arriving may still run its children
// (a partition, a hung beat, a store stall), so the STOPPED tick returns nothing on its row
// and start still waits on its stop-return; its next fresh beat is evidence again.
func TestASilentMemberIsNoEvidenceOfAGoneRun(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	wc, gen := h.taken()
	h.liveBeat(wc.Row) // a fresh beat that names nothing: the clock of absence starts
	_, _, _, err := h.st.StopUntil(h.ctx, "operator stop", h.now.Add(time.Hour))
	require.NoError(t, err)
	for range 5 {
		h.clock(sprint.LiveGrace) // the member is silent: no beat arrives
		h.machine()
	}
	assert.Equal(t, sprint.Working, h.snap().Fleet.Card(wc.ID).Col, "a stale beat returns nothing")
	_, _, _, err = h.st.SetMachine(h.ctx, true)
	require.ErrorContains(t, err, wc.Row+":"+wc.ID+"@1", "start still waits on the silent owner")
	h.liveBeat(wc.Row) // it speaks again, and names nothing
	h.machine()
	back := h.snap().Fleet.Card(wc.ID)
	assert.Equal(t, sprint.Ready, back.Col, "a fresh beat that does not name it, past the grace, returns it")
	assert.Equal(t, gen, back.Int("stopped_from_gen"))
}

// A finished report held while STOPPED (tla/LiveRuns.tla NoReportLost, Broken "heldnotlive"):
// the live set names it held, the reconcile leaves it, start is not held back by it, and the
// report is taken at its own generation once the machine runs.
func TestAHeldReportCrossesTheStopAndIsTaken(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	wc, gen := h.taken()
	_, _, _, err := h.st.StopUntil(h.ctx, "operator stop", h.now.Add(time.Hour))
	require.NoError(t, err)
	held := sprint.LiveKey(wc.ID, gen) + ":" + sprint.LiveHeld
	refused := h.run(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: gen}}))
	require.NotEmpty(t, refused.Refused, "STOPPED refuses the report: the worker keeps it")
	// the words the member reads as a refusal for the STOP alone (pkg/member stoppedRefusal)
	assert.Contains(t, refused.Refused[0].Why, "the machine is STOPPED")
	for range 3 {
		h.clock(sprint.LiveGrace)
		h.liveBeat(wc.Row, held)
		h.machine()
	}
	assert.Equal(t, sprint.Working, h.snap().Fleet.Card(wc.ID).Col, "a held report is not returned")
	_, running, _, err := h.st.SetMachine(h.ctx, true)
	require.NoError(t, err, "a held report does not hold the start back")
	assert.True(t, running.Running())
	h.must(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: gen}}))
	assert.NotEqual(t, sprint.Working, h.snap().Fleet.Card(wc.ID).Col, "the held report is taken at its generation")
}

// The stale STOP (Defect B; tla/LiveRuns.tla StopMarkerIsWorking, Broken "debtbyid"): a
// stop-returned card is no longer STOP-owned, so a coordinator's move of it (fleet down
// here, hold for a friend) is not refused before start, and start still finds the debt
// settled after the move. Before v1.2.6 the debt matched by id until start.
func TestAStopReturnedCardIsNoLongerStopOwned(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	wc, gen := h.taken()
	_, _, _, err := h.st.StopUntil(h.ctx, "operator stop", h.now.Add(time.Hour))
	require.NoError(t, err)
	refused := h.run(FleetStep(sprint.FleetReq{Op: "down", Member: wc.Row}))
	require.NotEmpty(t, refused.Refused, "an unreturned lease is still STOP-owned")
	assert.Contains(t, refused.Refused[0].Why, "STOP owns")
	h.must(StopReturnStep(sprint.StopReturnReq{As: wc.Row, IDs: []string{wc.ID}, Gens: map[string]int{wc.ID: gen}, Reason: "child exited"}))
	down := h.run(FleetStep(sprint.FleetReq{Op: "down", Member: wc.Row}))
	for _, r := range down.Refused {
		assert.NotContains(t, r.Why, "STOP owns", "a returned card is not STOP-owned")
	}
	_, running, _, err := h.st.SetMachine(h.ctx, true)
	require.NoError(t, err, "the receipt settles the debt wherever the returned card went")
	assert.True(t, running.Running())
}

// A returned card moved to another row keeps its receipt (the hold of 2026-10-10 moved
// every card of Zhi's row); an unreturned card moved out of band carries none, and start
// still refuses it.
func TestStartReadsTheReceiptNotTheRow(t *testing.T) {
	t.Parallel()
	for _, returned := range []bool{true, false} {
		t.Run(fmt.Sprint("returned=", returned), func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			wc, gen := h.taken()
			_, _, _, err := h.st.StopUntil(h.ctx, "operator stop", h.now.Add(time.Hour))
			require.NoError(t, err)
			if returned {
				h.must(StopReturnStep(sprint.StopReturnReq{As: wc.Row, IDs: []string{wc.ID}, Gens: map[string]int{wc.ID: gen}, Reason: "child exited"}))
			}
			require.NoError(t, h.m.RowsAdd(h.ctx, "t-fleet", []string{"other-owner"}))
			h.poke(sprint.Fleet, ntable.BatchMemberEntry{ID: wc.ID,
				Move: &ntable.MemberMoveOp{Row: "other-owner", Col: sprint.Ready},
				Set:  map[string]string{"gen": fmt.Sprint(gen + 1)}})
			_, _, _, err = h.st.SetMachine(h.ctx, true)
			if returned {
				require.NoError(t, err, "a returned card moved since is settled by its receipt")
				return
			}
			require.ErrorContains(t, err, wc.Row+":"+wc.ID+"@1", "a card moved with no receipt still owes its stop-return")
		})
	}
}

// The where record keeps each fleet row's working cards (sprint.RowWorkingKeys), which where
// counts against the rows' beats at each read: the dashboard's working cell is the live
// runs, and a take whose run is gone shows stale, not working, from the next read.
func TestWhereCountsWorkingAgainstTheLiveSets(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	wc, gen := h.taken()
	h.liveBeat(wc.Row, sprint.LiveKey(wc.ID, gen))
	h.machine()
	f, err := h.st.WhereFacts(h.ctx, h.snap().Work.Revision)
	require.NoError(t, err)
	require.Equal(t, []string{sprint.LiveKey(wc.ID, gen)}, f.RowWorking[wc.Row])
	beats, err := h.st.FleetBeats(h.ctx)
	require.NoError(t, err)
	run, held, stale := sprint.LiveCountKeys(f.RowWorking[wc.Row], sprint.RowBeat(beats, wc.Row), h.now)
	assert.Equal(t, [3]int{1, 0, 0}, [3]int{run, held, stale})
	h.liveBeat(wc.Row) // the run is gone: the next read shows it, before any tick
	beats, err = h.st.FleetBeats(h.ctx)
	require.NoError(t, err)
	run, held, stale = sprint.LiveCountKeys(f.RowWorking[wc.Row], sprint.RowBeat(beats, wc.Row), h.now)
	assert.Equal(t, [3]int{0, 0, 1}, [3]int{run, held, stale})
}
