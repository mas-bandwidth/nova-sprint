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

// A coordinator review reconstructs the catalog from existing retained logs,
// including dropped records and earlier epochs, without a new stored copy.
func TestProducerPlanningCatalogReadsEarlierEpochLogs(t *testing.T) {
 r:=newAlarmRig(t)
 brief:="RESULT: old tier: heavy\nREPO: r\nCLASS: parser\n\nDone is: escapes parse\n"
 op:=store.OpRecord{ID:"catalog-fixture",At:r.now,Verb:"fixture",Log:[]sprint.Line{
  {Kind:sprint.LineMove,Table:sprint.Work,Card:"old",To:"s:plan",Text:map[string]string{"brief":brief},Set:map[string]string{sprint.FieldRepo:"r"}},
  {Kind:sprint.LineMove,Table:sprint.Fleet,Card:"old.w1",Primary:"old",To:"m:finished",Set:map[string]string{"ok":"no"},Text:map[string]string{"report":"escaped delimiter failed\nsecond line"}},
  {Kind:sprint.LineMove,Table:sprint.Work,Card:"old",To:"s:landed"},
 }}
 f,err:=r.m.ReadFence(r.ctx);require.NoError(t,err)
 ok,err:=r.m.Acquire(r.ctx,f.Gen,op); require.NoError(t,err);require.True(t,ok)
 require.NoError(t,r.m.Release(r.ctx,op,true))
 _,err=r.st.Clear(r.ctx); require.NoError(t,err)
 history,err:=r.st.PlanningHistory(r.ctx);require.NoError(t,err)
 require.Len(t,history,1);require.True(t,history[0].Landed);require.Equal(t,uint64(0),history[0].Epoch)
 require.Equal(t,[]string{"old.w1: escaped delimiter failed"},history[0].Failures)
 r.must(store.AddStep(sprint.AddReq{Stream:"s",IDs:[]string{"next"},Brief:brief}))
 s:=r.snap();s.PlanHistory=history
 row:=sprint.TriageSet(s,[]string{"next"})[0]
 require.False(t,row.Passed);require.Contains(t,row.Precedents[0],"escaped delimiter failed")
}
