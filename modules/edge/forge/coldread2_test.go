package forge

import (
	"context"
	"testing"
)

// Cold read 2, 2026-11-08, at 032afd657: one failing probe per confirmed
// defect.

// actions.go Inheritance judged inherited red per workflow: a workflow red
// on the base in one job (corpus) and red at the head in another (fuzz)
// was all inherited ("not this card's: land it"), and the head's new
// failure was never read. A job red at the head and not on the base is new
// red, its log read, whatever the rest of its workflow did.
func TestColdread2InheritedRedIsJudgedPerJob(t *testing.T) {
	t.Parallel()
	var paths []string
	canned := actionsCanned_internal()
	r := "repos/o/r/"
	canned[r+"actions/runs/11/jobs?per_page=100"] = `[{"id":111,"name":"corpus","conclusion":"failure","runner_name":"bench-a"},{"id":112,"name":"fuzz","conclusion":"failure","runner_name":"bench-b"}]`
	canned[r+"actions/runs/21/jobs?per_page=100"] = `[{"id":211,"name":"corpus","conclusion":"failure","runner_name":"bench-a"},{"id":212,"name":"fuzz","conclusion":"success","runner_name":"bench-b"}]`
	canned[r+"actions/jobs/112/logs"] = "2026-11-07T08:00:00.0000000Z --- FAIL: FuzzParse (0.20s)\n"
	split, err := actionsGH_internal(canned, &paths).Inheritance(context.Background(), "o/r", "sprint/schema-100", actTip, actHead)
	if err != nil {
		t.Fatalf("Inheritance: %v", err)
	}
	var fuzz *WorkflowRed
	for i, w := range split.New {
		if w.Workflow == "hostile-corpus" {
			fuzz = &split.New[i]
		}
	}
	if fuzz == nil || len(fuzz.Jobs) != 1 || fuzz.Jobs[0].Name != "fuzz" || len(fuzz.Jobs[0].Lines) == 0 {
		t.Fatalf("new %+v: want hostile-corpus's fuzz job, red at the head and green on the base, as new red with its log read", split.New)
	}
	for _, w := range split.Inherited {
		if w.Workflow == "hostile-corpus" && (len(w.Jobs) != 1 || w.Jobs[0].Name != "corpus") {
			t.Fatalf("inherited %+v: want only the corpus job, red on the base too", w)
		}
	}
}
