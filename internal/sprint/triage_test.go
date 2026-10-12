package sprint

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func triageCard(w *world, id, state, brief string) {
	w.s.Work.SetRows([]string{"s"})
	w.s.Work.Put(&Card{ID: id, Row: "s", Col: state, Rev: 1, Fields: map[string]string{"brief": brief, "kind": "primary"}})
}

func TestSetTriageSeesTheWholeSet(t *testing.T) {
	w := newWorld(t)
	triageCard(w, "a", "plan", "RESULT: a tier: heavy\nREPO: r\nPATHS: internal/a.go\nTOOLCHAINS: go\nDone is: a works")
	triageCard(w, "b", "plan", "RESULT: b tier: heavy\nREPO: r\nPATHS: internal/a.go\nTOOLCHAINS: go\nDEPENDS-ON: missing\nDone is: b works")
	rows := TriageSet(w.s, []string{"a", "b"})
	require.Len(t, rows, 2)
	require.False(t, rows[0].Passed)
	require.Contains(t, triageReasons(rows[0]), "shared-paths")
	require.Contains(t, triageReasons(rows[1]), "dependencies")
	require.Equal(t, rows, TriageSet(w.s, []string{"b", "a", "a"}), "selection order and repeats cannot change the table")
	raw, err := json.Marshal(rows)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"checks"`)
}

func TestSetTriageRowsAndPrecedent(t *testing.T) {
	w := newWorld(t)
	brief := "RESULT: a tier: pro\nCLASS: model-work\nREPO: r\nPATHS: tla/A.tla\nDone is: the property holds\n\n"
	triageCard(w, "a", "plan", brief+strings.Repeat("repeat this paragraph\n", 800))
	triageCard(w, "landed", Landed, "RESULT: landed\nREPO: r\nDone is: the property holds\n\n")
	triageCard(w, "old", Review, "RESULT: old\nCLASS: model-work\n")
	w.s.Fleet.Put(&Card{ID: "old.w1", Row: "m", Col: "failed", Fields: map[string]string{"primary": "old", "report": "first failure\nsecond line"}})
	rows := TriageSet(w.s, []string{"a"})
	require.Len(t, rows, 1)
	why := triageReasons(rows[0])
	for _, key := range []string{"bench", "tier", "size", "landed"} {
		require.Contains(t, why, key)
	}
	require.Greater(t, rows[0].RepeatedBytes, 0)
	require.Contains(t, strings.Join(rows[0].Precedents, "\n"), "first failure")
	require.NotContains(t, strings.Join(rows[0].Precedents, "\n"), "second line")
}

func TestSetTriageCycleAndConformance(t *testing.T) {
	w := newWorld(t)
	triageCard(w, "a", "plan", "RESULT: a tier: heavy\nDEPENDS-ON: b\n")
	triageCard(w, "b", "plan", "RESULT: b tier: heavy\nDEPENDS-ON: a\n")
	triageCard(w, "c", "plan", "RESULT: c tier: heavy\nCLASS: conformance\n")
	for _, row := range TriageSet(w.s, []string{"a", "b", "c"}) {
		require.Contains(t, triageReasons(row), "dependencies")
	}
}

func TestSetTriagePassesExplicitBenchAndLedgerSharing(t *testing.T) {
	w := newWorld(t)
	triageCard(w, "a", "plan", "RESULT: a tier: frontier\nREPO: r\nPATHS: tla/A.tla, docs/fixes.sexp\nSHARED: docs/fixes.sexp\nTOOLCHAINS: java\nTLC: cardmachine\nDone is: A holds\n")
	triageCard(w, "b", "plan", "RESULT: b tier: heavy\nREPO: r\nPATHS: internal/b.go, docs/fixes.sexp\nSHARED: docs/fixes.sexp\nTOOLCHAINS: go\nDEPENDS-ON: a\nDone is: B holds\n")
	for _, row := range TriageSet(w.s, []string{"a", "b"}) {
		require.True(t, row.Passed, "%+v", row)
	}
}

func TestSetTriageDoesNotAcceptMissingOrUnplacedCards(t *testing.T) {
	w := newWorld(t)
	triageCard(w, "gone", "", "RESULT: gone\n")
	for _, row := range TriageSet(w.s, []string{"gone", "absent"}) {
		require.False(t, row.Passed)
	}
}

func triageReasons(row TriageRow) string {
	var out []string
	for _, check := range row.Checks {
		if !check.Passed {
			out = append(out, check.Name+": "+check.Detail)
		}
	}
	return strings.Join(out, "; ")
}

func TestPlanReviewPromoteAndEditInvalidatesApproval(t *testing.T) {
	w := newWorld(t)
	w.do(Add(w.s, AddReq{Stream: "s", IDs: []string{"a"}, Brief: "RESULT: a tier: heavy\nDone is: a works\n"}))
	require.Equal(t, Planned, w.state("a"))
	require.NotEmpty(t, PromotePlan(w.s, TriageReq{IDs: []string{"a"}}).Refused)
	p, rows := ReviewPlan(w.s, TriageReq{AllPlan: true})
	require.True(t, rows[0].Passed)
	w.must(p)
	require.Equal(t, Checked, w.state("a"))
	w.must(Brief(w.s, BriefReq{ID: "a", Brief: "RESULT: a tier: heavy\nDone is: a works better\n"}))
	require.Equal(t, Planned, w.state("a"))
	p, _ = ReviewPlan(w.s, TriageReq{AllPlan: true})
	w.must(p)
	w.must(PromotePlan(w.s, TriageReq{Stream: "s"}))
	require.Equal(t, Waiting, w.state("a"))
	w.must(Resolve(w.s, ResolveReq{}))
	require.Equal(t, Ready, w.state("a"))
}

func TestPlanReviewCannotMoveFailingSiblingOrUseStaleSet(t *testing.T) {
	w := newWorld(t)
	triageCard(w, "a", Planned, "RESULT: a\nREPO: r\nPATHS: a.txt\n")
	triageCard(w, "b", Planned, "RESULT: b\nDEPENDS-ON: missing\n")
	p, rows := ReviewPlan(w.s, TriageReq{AllPlan: true})
	require.Len(t, rows, 2)
	require.Len(t, p.Refused, 1)
	w.do(p)
	require.Equal(t, Checked, w.state("a"))
	require.Equal(t, Planned, w.state("b"))
	triageCard(w, "new", Planned, "RESULT: new\nREPO: r\nPATHS: a.txt\n")
	require.NotEmpty(t, PromotePlan(w.s, TriageReq{IDs: []string{"a"}}).Refused)
	w.s.Actor = "not-coordinator"
	require.NotEmpty(t, PromotePlan(w.s, TriageReq{IDs: []string{"a"}}).Refused)
}

func TestPlanningDeadlineIsOneJudgmentAndCloses(t *testing.T) {
	w := newWorld(t)
	w.do(Add(w.s, AddReq{Stream: "s", IDs: []string{"a", "b"}}))
	w.tick(31 * time.Minute)
	p, _ := TickDeadlines(w.s, TickReq{})
	w.do(p)
	n := 0
	for _, o := range w.s.Open {
		if o.Note.Type == NPlanLate {
			n++
			require.Contains(t, o.Note.What, "2 cards in plan or check")
		}
	}
	require.Equal(t, 1, n)
	w.do(Add(w.s, AddReq{Stream: "s", IDs: []string{"c"}}))
	w.tick(31 * time.Minute)
	p, _ = TickDeadlines(w.s, TickReq{})
	require.Empty(t, p.Notes)
	require.Len(t, p.Updates, 1)
	w.do(p)
	p, _ = ReviewPlan(w.s, TriageReq{AllPlan: true})
	w.must(p)
	w.must(PromotePlan(w.s, TriageReq{Stream: "s"}))
	p, _ = TickDeadlines(w.s, TickReq{})
	require.Len(t, p.Closes, 1)
}

// A failed worker waits in finished, and its retained findings survive consumer
// cleanup (docs/SPEC-SPRINT.md, plan and check columns).
func TestSetTriagePrecedentIncludesFinishedFailureAndKeptFindings(t *testing.T) {
	w := newWorld(t)
	triageCard(w, "next", Planned, "RESULT: next tier: heavy\nCLASS: parser\n")
	triageCard(w, "old", Review, "RESULT: old tier: heavy\nCLASS: parser\n")
	w.s.Work.Card("old").Fields[FieldFindings] = "attempt 1: kept parser failure"
	w.s.Fleet.Put(&Card{ID: "old.w2", Row: "m", Col: Finished, Fields: map[string]string{"primary": "old", "ok": "no", "report": "current parser failure\nnot the first line"}})
	got := strings.Join(TriageSet(w.s, []string{"next"})[0].Precedents, "\n")
	require.Contains(t, got, "kept parser failure")
	require.Contains(t, got, "current parser failure")
	require.NotContains(t, got, "not the first line")
}


func TestSetTriageUsesHistoricalCatalogAndTaskClass(t *testing.T) {
 w:=newWorld(t)
 triageCard(w,"next",Planned,"RESULT: next tier: heavy\nREPO: r\n\nTHE TASK. CLASS: parser repair\n\nDone is: the parser handles escapes\n")
 w.s.PlanHistory=[]PlanningRecord{{Epoch:2,ID:"old",Repo:"r",Brief:"RESULT: old tier: heavy\nREPO: r\n\nTHE TASK. CLASS: parser repair\n\nDone is: the parser handles escapes\n",Landed:true,Failures:[]string{"first attempt failed"}}}
 row:=TriageSet(w.s,[]string{"next"})[0]
 require.Contains(t,triageReasons(row),"landed")
 require.Contains(t,strings.Join(row.Precedents,"\n"),"first attempt failed")
}

func TestSetTriageModelClassNeedsCapableTierAndOrderedPathsPass(t *testing.T) {
 w:=newWorld(t)
 triageCard(w,"a",Planned,"RESULT: a tier: flash\nCLASS: model\n")
 require.Contains(t,triageReasons(TriageSet(w.s,[]string{"a"})[0]),"tier")
 triageCard(w,"a",Planned,"RESULT: a tier: heavy\nCLASS: model\nREPO: r\nPATHS: a.txt\n")
 triageCard(w,"b",Planned,"RESULT: b tier: heavy\nREPO: r\nPATHS: a.txt\nDEPENDS-ON: a\n")
 for _,row:=range TriageSet(w.s,[]string{"a","b"}) { require.True(t,row.Passed,"%+v",row) }
}
