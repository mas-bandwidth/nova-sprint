package forge

import (
	"strings"
	"testing"
)

// parseRuns_internal reads GitHub's actions/runs page: each run's id,
// branch, head, status, event, creation and pull requests; a page that is
// not JSON is refused with what it was.
func TestParseRuns(t *testing.T) {
	t.Parallel()
	runs, err := parseRuns_internal(`{"total_count":2,"workflow_runs":[
		{"id":11,"head_branch":"sprint-group/s1","head_sha":"aaa","status":"queued","event":"pull_request","created_at":"2026-11-15T10:00:00Z","pull_requests":[{"number":3520}]},
		{"id":12,"head_branch":"main","head_sha":"bbb","status":"in_progress","event":"push","created_at":"2026-11-15T09:00:00Z","pull_requests":[]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].ID != 11 || runs[0].Branch != "sprint-group/s1" || runs[0].HeadSHA != "aaa" || runs[0].Status != "queued" ||
		runs[0].Event != "pull_request" || runs[0].Created.Hour() != 10 || len(runs[0].PRs) != 1 || runs[0].PRs[0] != 3520 || len(runs[1].PRs) != 0 {
		t.Fatalf("runs = %+v", runs)
	}
	if _, err := parseRuns_internal("<html>"); err == nil || !strings.Contains(err.Error(), "forge: decode actions/runs") {
		t.Fatalf("err = %v, want the decode refusal", err)
	}
}

// A cancel of a run that finished meanwhile is no failure; any other
// answer is.
func TestCancelFinished(t *testing.T) {
	t.Parallel()
	for out, want := range map[string]bool{
		"gh: Cannot cancel a workflow run that is completed. (HTTP 409)": true,
		"HTTP 409: Conflict":                true,
		"HTTP 403: Resource not accessible": false,
	} {
		if finished_internal(out) != want {
			t.Errorf("finished_internal(%q) = %v, want %v", out, !want, want)
		}
	}
}
