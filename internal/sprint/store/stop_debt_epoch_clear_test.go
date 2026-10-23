package store

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// TestStopDebtNamingGoneCardsSettlesAtStart is the 2026-10-23 deadlock: after a clear the
// machine record held STOP debt naming cards with no record in the new epoch, and START,
// stop-return and clear all refused for good. Clear now empties the debt with the cards it
// removes, and START settles (and says) a debt whose card has no record; a card that exists
// and is unreturned still holds START refused. tla/StopReturn.tla GoneNeverBlocksStart
func TestStopDebtNamingGoneCardsSettlesAtStart(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	h.startMachine()
	wc := h.snap().Fleet.Card("s1-1.w1")
	require.NotNil(t, wc)
	row := wc.Row
	h.must(TakeStep(sprint.TakeReq{As: row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: 1}}))
	_, stopped, _, err := h.st.SetMachine(h.ctx, false)
	require.NoError(t, err)
	require.Len(t, stopped.StopDebt, 1)
	h.must(StopReturnStep(sprint.StopReturnReq{As: row, IDs: []string{wc.ID}, Gens: map[string]int{wc.ID: 1}, Reason: "child exited"}))

	// a returned entry the settle kept (as on the live store) goes with the clear
	m, _, err := h.st.Machine(h.ctx)
	require.NoError(t, err)
	m.StopDebt = []StopLease{{Table: sprint.Fleet, Row: row, ID: wc.ID, Gen: 1}}
	require.NoError(t, h.st.putMachine(h.ctx, m))
	_, err = h.st.Clear(h.ctx)
	require.NoError(t, err, "a returned debt does not block clear")
	m, _, err = h.st.Machine(h.ctx)
	require.NoError(t, err)
	require.Empty(t, m.StopDebt, "clear empties the debt with the cards it removes")
	require.Equal(t, uint64(1), h.snap().Epoch)

	// the live store's record: debt carried from epoch 0 naming cards with no record here
	gone := []StopLease{
		{Table: sprint.Fleet, Row: row, ID: "rocketnet-r1-client-destroy-waits-bounded.r10.rowan", Gen: 1},
		{Table: sprint.Fleet, Row: row, ID: "rocketnet-test-report-is-not-tests.w1", Gen: 1},
	}
	m.StopIssued, m.StopDebt = true, gone
	require.NoError(t, h.st.putMachine(h.ctx, m))
	s := h.snap()
	for _, d := range gone {
		require.Nil(t, s.Fleet.Card(d.ID), "the fixture's debt names a card with no record")
	}
	_, after, res, err := h.st.setMachine(h.ctx, true, "coordinator", "", m.Until)
	require.NoError(t, err, "START settles a debt whose card has no record in the epoch")
	require.Equal(t, Running, after.State)
	require.Empty(t, after.StopDebt)
	require.Len(t, res.Said, 1)
	require.Contains(t, res.Said[0], "2 debt(s) settled-gone")
	for _, d := range gone {
		require.Contains(t, res.Said[0], row+":"+d.ID+"@1")
	}

	// the safe side: a card that exists and is unreturned still holds START refused
	h.must(AddStep(sprint.AddReq{Stream: "s2", Count: 1}))
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s2-1"}}}))
	wc = h.snap().Fleet.Card("s2-1.w1")
	require.NotNil(t, wc)
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: 1}}))
	_, stopped, _, err = h.st.SetMachine(h.ctx, false)
	require.NoError(t, err)
	stopped.StopDebt = append(stopped.StopDebt, gone...)
	require.NoError(t, h.st.putMachine(h.ctx, stopped))
	_, _, _, err = h.st.SetMachine(h.ctx, true)
	require.ErrorContains(t, err, "1 STOP-owned work/read jobs lack same-owner cancellation receipts")
	require.ErrorContains(t, err, wc.ID+"@1")
}
