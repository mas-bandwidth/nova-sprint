package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestABriefOverTheBoundIsRefusedWhileRunning checks that a brief over the bound is
// refused at the step on a RUNNING machine, with no queue entry and no log line.
func TestABriefOverTheBoundIsRefusedWhileRunning(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 1, briefOf("flash", ""))
	h.must(DealStep(sprint.DealReq{}))
	// Take and finish to get card to Review state
	snap := h.snap()
	wc := snap.Fleet.Card("s1-1.w1")
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}}))
	h.must(FinishStep(sprint.FinishReq{Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}}))
	// Card is now in Review. Start the machine so it becomes RUNNING.
	h.beat()
	// Verify card is in review
	snap = h.snap()
	pr := snap.Work.Card("s1-1")
	require.NotNil(t, pr)
	require.Equal(t, sprint.Review, pr.Col)
	// Try to brief the card with an over-bound brief (17,000 bytes)
	overBrief := strings.Repeat("x", 17000)
	result := h.run(BriefStep(sprint.BriefReq{
		ID:    "s1-1",
		Brief: overBrief,
		Who:   "tester",
	}))
	// Should be refused at the step - this is what we're fixing
	t.Logf("Result: refused=%v tables=%v", result.Refused, result.Tables)
	require.NotEmpty(t, result.Refused, "brief over bound should be refused at step")
	// Check the refusal message mentions the bound
	assert.Contains(t, result.Refused[0].Why, "17000 bytes", "should mention actual size")
	assert.Contains(t, result.Refused[0].Why, "16384 bytes", "should mention bound")
	// Check no queue entry was created (the work table's queued count should be 0)
	assert.Equal(t, 0, result.Tables["work"], "no queue entry should be created")
}
