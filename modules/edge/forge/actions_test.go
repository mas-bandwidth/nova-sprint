package forge

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

const actionsSHA = "0123456789abcdef0123456789abcdef01234567"

// Runs reads every page of a sha's workflow runs (gh api --paginate, one
// JSON object a line as --jq prints them), on the REST bucket.
func TestRunsReadsEveryPage(t *testing.T) {
	t.Parallel()
	now := int64(1000)
	c, calls := ghClient_internal(&now, func([]string) edge.Result {
		return edge.Result{Stdout: `{"id":11,"workflow_id":7,"name":"CI","event":"pull_request","status":"completed","conclusion":"failure"}
{"id":12,"workflow_id":8,"name":"CAA","event":"pull_request","status":"in_progress","conclusion":null}
`}
	})
	runs, err := c.Runs(context.Background(), "https://github.com/o/r.git", actionsSHA)
	if err != nil {
		t.Fatal(err)
	}
	want := []Run{{ID: 11, Workflow: 7, Name: "CI", Event: "pull_request", Status: "completed", Conclusion: "failure"},
		{ID: 12, Workflow: 8, Name: "CAA", Event: "pull_request", Status: "in_progress"}}
	if !slices.Equal(runs, want) {
		t.Fatalf("runs %+v, want %+v", runs, want)
	}
	if got := (*calls)[0]; !strings.HasPrefix(got, "api --paginate repos/o/r/actions/runs?head_sha="+actionsSHA+"&per_page=100 --jq ") {
		t.Fatalf("argv %q", got)
	}
	if b := bucket_internal(strings.Fields((*calls)[0])); b != BucketCore {
		t.Fatalf("bucket %s, want %s", b, BucketCore)
	}
}

// Jobs reads a run's jobs at their latest attempt.
func TestJobsReadsTheLatestAttempt(t *testing.T) {
	t.Parallel()
	now := int64(1000)
	c, calls := ghClient_internal(&now, func([]string) edge.Result {
		return edge.Result{Stdout: `{"id":5,"name":"C# tables leg (debug)","status":"completed","conclusion":"failure"}` + "\n"}
	})
	jobs, err := c.Jobs(context.Background(), "o/r", 11)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Job{{ID: 5, Name: "C# tables leg (debug)", Status: "completed", Conclusion: "failure"}}; !slices.Equal(jobs, want) {
		t.Fatalf("jobs %+v", jobs)
	}
	if got := (*calls)[0]; !strings.HasPrefix(got, "api --paginate repos/o/r/actions/runs/11/jobs?per_page=100&filter=latest --jq ") {
		t.Fatalf("argv %q", got)
	}
}

// Every refusal: a bad sha, no run id, a repository off github.com, output
// that is not JSON, and a rate limit (a FaultError the landing rests on).
func TestRunsAndJobsRefusals(t *testing.T) {
	t.Parallel()
	now := int64(1000)
	answer := edge.Result{Stdout: "not json"}
	c, calls := ghClient_internal(&now, func([]string) edge.Result { return answer })
	ctx := context.Background()
	if _, err := c.Runs(ctx, "o/r", "abc"); err == nil || !strings.Contains(err.Error(), "40-hex sha") {
		t.Fatalf("short sha: %v", err)
	}
	if _, err := c.Jobs(ctx, "o/r", 0); err == nil || !strings.Contains(err.Error(), "run id") {
		t.Fatalf("no run: %v", err)
	}
	if _, err := c.Runs(ctx, "https://gitlab.com/o/r", actionsSHA); err == nil || !strings.Contains(err.Error(), "not on github.com") {
		t.Fatalf("other host: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("a refusal called gh: %q", *calls)
	}
	if _, err := c.Jobs(ctx, "o/r", 3); err == nil || !strings.Contains(err.Error(), "decode gh api repos/o/r/actions/runs/3/jobs") {
		t.Fatalf("bad json: %v", err)
	}
	answer = edge.Result{ExitCode: 1, Stderr: "HTTP 429: too many requests\nRetry-After: 30"}
	_, err := c.Runs(ctx, "o/r", actionsSHA)
	var fe *FaultError
	if !errors.As(err, &fe) || fe.Kind != FaultRateLimit {
		t.Fatalf("rate limit: %v", err)
	}
}

// RunOf is the run an Actions job's url names, and 0 for anything else.
func TestRunOf(t *testing.T) {
	t.Parallel()
	for url, want := range map[string]int64{
		"https://github.com/o/r/actions/runs/43666869124/job/132668086108": 43666869124,
		"https://github.com/o/r/actions/runs/43666869124":                  0,
		"https://example.invalid/status/1":                                 0,
		"":                                                                 0,
	} {
		if got := RunOf(url); got != want {
			t.Fatalf("RunOf(%q) = %d, want %d", url, got, want)
		}
	}
}
