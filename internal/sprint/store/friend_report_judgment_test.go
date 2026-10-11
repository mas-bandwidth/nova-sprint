package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

func TestASeatJudgmentDoesNotRewriteAFriendsSuccessfulFinish(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 1, briefOf("flash", ""))
	h.must(DealStep(sprint.DealReq{}))
	wc := h.snap().Fleet.Card(h.snap().Work.Card("s1-1").F("work"))
	require.NotNil(t, wc)
	gens := map[string]int{wc.ID: wc.Int("gen")}
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: gens}))
	const head = "0123456789abcdef0123456789abcdef01234567"
	h.must(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: gens,
		Head: head, Report: "friend amy LAND: pushed", SeatJudgment: "the report needs a read"}))

	pr := h.snap().Work.Card("s1-1")
	assert.Equal(t, sprint.Review, pr.Col)
	assert.Equal(t, head, pr.F("head"))
	assert.Zero(t, h.snap().Fleet.Count(wc.Row, sprint.DoneFailed), "a seat judgment is not worker failure")
	judgments := h.openOf(sprint.NReportJudgment)
	require.Len(t, judgments, 1)
	assert.Equal(t, []string{"read the report"}, judgments[0].Note.Decisions)
	assert.Equal(t, "the report needs a read", judgments[0].Note.What)
	h.clean("a report judgment beside successful work")
}
