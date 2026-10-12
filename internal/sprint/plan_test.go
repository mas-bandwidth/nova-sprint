package sprint_test

import (
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProducerAdmissionStartsInPlan(t *testing.T) {
	r := newAlarmRig(t)
	r.must(store.AddStep(sprint.AddReq{Stream: "plan-set", IDs: []string{"producer"}}))
	require.Equal(t, "plan", r.snap().Work.Card("producer").Col, "add must not bypass coordinator plan/check")
	r.ticks(2)
	require.Equal(t, "plan", r.snap().Work.Card("producer").Col, "the deal must not consume planning cards")
}

func TestProducerPlanCheckClearAndViews(t *testing.T) {
	r := newAlarmRig(t)
	r.must(store.AddStep(sprint.AddReq{Stream: "plan-set", IDs: []string{"planned", "checked"}}))
	r.must(store.Step{Verb: "check", Load: store.All, Plan: func(s *sprint.Snapshot) sprint.Plan {
		p, _ := sprint.ReviewPlan(s, sprint.TriageReq{IDs: []string{"checked"}})
		return p
	}})
	s := r.snap()
	require.Equal(t, sprint.Checked, s.Work.Card("checked").Col)
	r.ticks(2)
	require.Equal(t, sprint.Checked, r.snap().Work.Card("checked").Col, "the deal must not consume checked cards")
	rows := sprint.CardRows(s, sprint.CardsFilter{})
	counts, ok := sprint.CardsBy(rows, "col")
	require.True(t, ok)
	require.Equal(t, 1, counts["plan"])
	require.Equal(t, 1, counts["check"])
	res, err := r.st.Clear(r.ctx)
	require.NoError(t, err)
	require.Greater(t, res.To, res.From)
	require.Empty(t, r.snap().Work.Cards())
}
