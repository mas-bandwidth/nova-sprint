package forge

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// ghClient_internal is a Client whose gh is answer (unit: no process, clock
// from now), with every argv it was asked kept in argv.
func ghClient_internal(now *int64, answer func(argv []string) edge.Result) (*Client, *[]string) {
	var calls []string
	c := New(Config{Identity: Identity{Name: HostLogin}, Now: func() int64 { return *now },
		Exec: ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
			calls = append(calls, strings.Join(cmd.Argv[1:], " "))
			return answer(cmd.Argv[1:]), nil
		})})
	return c, &calls
}

// E: a primary rate limit (gh's message carries no reset time) rests the
// identity until the bucket's reset as gh api rate_limit reads it, not 60
// s; GraphQL and REST rest apart, so a REST read is not refused while
// GraphQL rests. A limit the reset does not explain (remaining above 0, a
// secondary limit with no Retry-After) rests 60 s.
func TestPrimaryRateLimitRestsUntilTheReset(t *testing.T) {
	t.Parallel()
	now := int64(1000)
	remaining := 0
	c, calls := ghClient_internal(&now, func(argv []string) edge.Result {
		switch argv[0] {
		case "pr":
			return edge.Result{ExitCode: 1, Stderr: "GraphQL: API rate limit exceeded for user ID 1. (HTTP 403)"}
		case "api":
			b, _ := json.Marshal(map[string]any{"resources": map[string]any{
				"graphql": map[string]any{"remaining": remaining, "reset": 4600},
				"core":    map[string]any{"remaining": 4000, "reset": 4700}}})
			return edge.Result{Stdout: string(b)}
		}
		return edge.Result{Stdout: "--- FAIL: TestX\n"}
	})
	_, _, err := c.PR(context.Background(), "o/r", "sprint-group/s1", "main")
	var fe *FaultError
	if !errors.As(err, &fe) || fe.Kind != FaultRateLimit || fe.RetryAfter != 4600 || c.RestedUntil() != 4600 {
		t.Fatalf("err %v, rested until %d: want a rest until the GraphQL reset 4600", err, c.RestedUntil())
	}
	now = 1100
	if _, _, err := c.PR(context.Background(), "o/r", "sprint-group/s1", "main"); !errors.As(err, &fe) || fe.RetryAfter != 4600 {
		t.Fatalf("err %v at 1100, want GraphQL still resting until 4600", err)
	}
	if lines, err := c.FailedLines(context.Background(), "o/r", "https://github.com/o/r/actions/runs/1/job/2"); err != nil || len(lines) == 0 {
		t.Fatalf("a REST read while GraphQL rests: %v %v, want it made", lines, err)
	}
	if n := len(slices.DeleteFunc(slices.Clone(*calls), func(a string) bool { return a != "api rate_limit" })); n != 1 {
		t.Fatalf("calls %q: want the reset read once", *calls)
	}
	now, remaining = 5000, 12
	if _, _, err := c.PR(context.Background(), "o/r", "sprint-group/s1", "main"); !errors.As(err, &fe) || fe.RetryAfter != 5060 {
		t.Fatalf("err %v, want a 60 s rest when the reset does not explain the limit", err)
	}
}

func TestStopOfReadsGitHubsOwnWordsOnly(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		conclusion string
		ann        []string
		want       string
	}{
		{"FAILURE", []string{"Process completed with exit code 137."}, "killed (exit code 137)"},
		{"FAILURE", []string{"Process completed with exit code 1.", "CI rule: per-commit jobs finish in 2m", "Process completed with exit code 137."}, "killed (exit code 137)"},
		{"FAILURE", []string{"Process completed with exit code 143."}, "terminated (exit code 143)"},
		{"FAILURE", []string{"The job running on runner r1 has exceeded the maximum execution time of 5 minutes."}, "timed out (the job's time limit)"},
		{"FAILURE", []string{"The action 'unit' has timed out after 2 minutes."}, "timed out (a step's time limit)"},
		{"FAILURE", []string{"The operation was canceled."}, "cancelled"},
		{"CANCELLED", nil, "cancelled"},
		{"TIMED_OUT", nil, "timed out"},
		{"FAILURE", []string{"Process completed with exit code 1."}, ""},
		{"FAILURE", []string{"ran past 110s", "CI rule: per-commit jobs finish in 2m"}, ""}, // a fleet's own wording is not read
		{"FAILURE", []string{"Process completed with exit code 1370."}, ""},
		{"STARTUP_FAILURE", nil, ""},
	} {
		if got := StopOf(c.conclusion, c.ann); got != c.want {
			t.Errorf("StopOf(%s, %q) = %q, want %q", c.conclusion, c.ann, got, c.want)
		}
	}
}

// Failures reads every completed check that did not pass, with its
// failure annotations, page by page, and says how each was stopped.
func TestFailuresReadsEachChecksAnnotations(t *testing.T) {
	t.Parallel()
	now := int64(1000)
	page := func(nodes []any, next bool) edge.Result {
		b, _ := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{"c0": map[string]any{
			"statusCheckRollup": map[string]any{"contexts": map[string]any{"pageInfo": map[string]any{"hasNextPage": next, "endCursor": "p2"}, "nodes": nodes}}}}}})
		return edge.Result{Stdout: string(b)}
	}
	run := func(name, conclusion, url string, msgs ...string) map[string]any {
		var ann []any
		for _, m := range msgs {
			ann = append(ann, map[string]any{"message": m, "annotationLevel": "FAILURE"})
		}
		ann = append(ann, map[string]any{"message": "exit code 137 in a warning", "annotationLevel": "WARNING"})
		return map[string]any{"__typename": "CheckRun", "name": name, "status": "COMPLETED", "conclusion": conclusion, "detailsUrl": url, "annotations": map[string]any{"nodes": ann}}
	}
	c, calls := ghClient_internal(&now, func(argv []string) edge.Result {
		if strings.Contains(argv[len(argv)-1], `after: "p2"`) {
			return page([]any{run("lua", "CANCELLED", "u3"), map[string]any{"__typename": "StatusContext", "context": "ext", "state": "FAILURE", "targetUrl": "u4"}}, false)
		}
		return page([]any{run("corpus", "FAILURE", "u1", "Process completed with exit code 137."), run("unit", "FAILURE", "u2", "Process completed with exit code 1."), run("lint", "SUCCESS", "u0")}, true)
	})
	got, err := c.Failures(context.Background(), "o/r", breakSHA)
	want := []Failure{{"corpus", "u1", "killed (exit code 137)"}, {"unit", "u2", ""}, {"lua", "u3", "cancelled"}, {"ext", "u4", ""}}
	if err != nil || !slices.Equal(got, want) || len(*calls) != 2 {
		t.Fatalf("Failures = %+v (%v) in %d calls, want %+v in 2", got, err, len(*calls), want)
	}
}

// Rerun asks GitHub to run the failed jobs of the run a check's url names,
// one REST call; a url that names no run is refused without a call.
func TestRerunRunsTheFailedJobsOfTheRun(t *testing.T) {
	t.Parallel()
	now := int64(1000)
	c, calls := ghClient_internal(&now, func([]string) edge.Result { return edge.Result{} })
	if err := c.Rerun(context.Background(), "o/r", "https://github.com/o/r/actions/runs/42/job/7"); err != nil || !slices.Equal(*calls, []string{"run rerun 42 --failed --repo o/r"}) {
		t.Fatalf("Rerun: %v, calls %q", err, *calls)
	}
	if err := c.Rerun(context.Background(), "o/r", "https://ci.example/status/1"); err == nil || len(*calls) != 1 {
		t.Fatalf("a url with no run: %v, calls %q", err, *calls)
	}
}

// A red or faulted rollup with checks still queued or running is Waiting,
// with how many (the landing reads it as unfinished, never as red).
func TestRollupSaysHowManyStillRun(t *testing.T) {
	t.Parallel()
	nodes := []gqlNode{{Typename: "CheckRun", Name: "lint", Status: "COMPLETED", Conclusion: "FAILURE"}, {Typename: "CheckRun", Name: "unit", Status: "QUEUED"}, {Typename: "CheckRun", Name: "tlc", Status: "IN_PROGRESS"}}
	if r := classifyRollup_internal(breakSHA, "PENDING", nodes); r.State != CheckRed || !r.Waiting || r.Pending != 2 {
		t.Fatalf("%+v, want red, waiting on 2", r)
	}
	nodes[0].Conclusion = "CANCELLED"
	if r := classifyRollup_internal(breakSHA, "PENDING", nodes); r.State != CheckFault || !r.Waiting || r.Pending != 2 {
		t.Fatalf("%+v, want a fault, waiting on 2", r)
	}
	if r := classifyRollup_internal(breakSHA, "FAILURE", nodes[:1]); r.Waiting || r.Pending != 0 {
		t.Fatalf("%+v, want a completed fault", r)
	}
}
