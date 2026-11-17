package forge

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
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

// Merge retries gh pr merge up to 3 times when GitHub refuses with "is not
// mergeable" while gh pr view still reports mergeable UNKNOWN.
func TestMergeRetriesWhenMergeableIsUnknown(t *testing.T) {
	t.Parallel()
	merges := 0
	mergedSHA := strings.Repeat("c", 40)
	headSHA := strings.Repeat("a", 40)
	c := New(Config{
		Identity: Identity{Name: "sprint", Token: "tok"},
		Now:      func() int64 { return 1000 },
		Sleep:    func(context.Context, time.Duration) error { return nil },
		Exec: ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
			args := strings.Join(cmd.Argv[1:], " ")
			switch {
			case strings.HasPrefix(args, "repo view "):
				return edge.Result{Stdout: `{"mergeCommitAllowed":true}`}, nil
			case strings.HasPrefix(args, "pr merge "):
				merges++
				if merges < 3 {
					return edge.Result{ExitCode: 1, Stderr: "Pull Request #42 is not mergeable: the merge commit cannot be cleanly created."}, nil
				}
				return edge.Result{}, nil
			case strings.Contains(args, "--json mergeable"):
				return edge.Result{Stdout: `{"mergeable":"UNKNOWN"}`}, nil
			case strings.Contains(args, "--json state,mergeCommit"):
				return edge.Result{Stdout: `{"state":"MERGED","mergeCommit":{"oid":"` + mergedSHA + `"}}`}, nil
			}
			t.Fatalf("unexpected gh call: %v", cmd.Argv)
			return edge.Result{}, nil
		}),
	})
	got, err := c.Merge(context.Background(), "o/r", 42, headSHA)
	if err != nil || got != mergedSHA || merges != 3 {
		t.Fatalf("Merge = %q, %v (merges=%d), want %s after 3 merge attempts", got, err, merges, mergedSHA)
	}
}

func TestRerunFailed(t *testing.T) {
	t.Parallel()
	var calls []string
	code, stderr := 0, ""
	c := New(Config{
		Identity: Identity{Name: "sprint", Token: "tok"},
		Now:      func() int64 { return 1000 },
		Exec: ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
			calls = append(calls, strings.Join(cmd.Argv[1:], " "))
			return edge.Result{ExitCode: code, Stderr: stderr}, nil
		}),
	})
	if err := c.RerunFailed(context.Background(), "https://github.com/o/r.git", 77); err != nil {
		t.Fatalf("RerunFailed 201: %v", err)
	}
	if len(calls) != 1 || calls[0] != "api --method POST repos/o/r/actions/runs/77/rerun-failed-jobs" {
		t.Fatalf("calls = %v", calls)
	}
	code, stderr = 1, "gh: This workflow run is already running (HTTP 409)"
	if err := c.RerunFailed(context.Background(), "o/r", 77); err != nil {
		t.Fatalf("RerunFailed 409 must be nil, got %v", err)
	}
	code, stderr = 1, "gh: HTTP 403: Resource not accessible"
	if err := c.RerunFailed(context.Background(), "o/r", 77); err == nil {
		t.Fatal("RerunFailed 403 must return error")
	}
}
