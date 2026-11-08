package forge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

const (
	shaA = "1111111111111111111111111111111111111111"
	shaB = "2222222222222222222222222222222222222222"
	shaC = "3333333333333333333333333333333333333333"
	shaD = "4444444444444444444444444444444444444444"
)

// The forge acts as the host's own gh login (intent rule 32): with no
// override gh gets the process's environment without any GitHub credential
// of its own, so a GH_TOKEN or a GH_CONFIG_DIR there cannot make it someone
// else (directive 59).
func TestForgeActsAsTheHostsGh(t *testing.T) { // unitlint: serial, it sets the process environment (t.Setenv)
	t.Setenv("GH_TOKEN", "inherited-token")
	t.Setenv("GH_CONFIG_DIR", "/home/someone/.config/gh")
	var seen []edge.Cmd
	c := New(Config{
		Identity: Identity{Name: HostLogin},
		Now:      func() int64 { return 1000 },
		Exec: ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
			seen = append(seen, cmd)
			return edge.Result{Stdout: "[]"}, nil
		}),
	})
	if _, _, err := c.PR(context.Background(), "mas-bandwidth/nova-sprint", "sprint-group/s1", "v2"); err != nil {
		t.Fatalf("PR: %v", err)
	}
	if len(seen) != 1 || !seen[0].CleanEnv || slices.ContainsFunc(seen[0].Env, func(kv string) bool {
		return strings.HasPrefix(kv, "GH_TOKEN=") || strings.HasPrefix(kv, "GH_CONFIG_DIR=")
	}) || !slices.Contains(seen[0].Env, "HOME="+os.Getenv("HOME")) {
		t.Fatalf("gh ran %d times; want one pr list with the host's environment less its GitHub credentials", len(seen))
	}
}

// The optional override: a GH_GATE_TOKEN given to the forge replaces any
// GitHub credential the process inherited, in the environment only, never
// in argv.
func TestForgeOverrideToken(t *testing.T) { // unitlint: serial, it sets the process environment (t.Setenv)
	t.Setenv("GH_TOKEN", "inherited-token")
	t.Setenv("GITHUB_TOKEN", "inherited-github")
	t.Setenv("GH_CONFIG_DIR", "/home/someone/.config/gh")
	var seen []edge.Cmd
	c := New(Config{
		Identity: Identity{Name: "sprint", Token: "tok-sprint"},
		Now:      func() int64 { return 1000 },
		Exec: ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
			seen = append(seen, cmd)
			return edge.Result{Stdout: "[]"}, nil
		}),
	})
	if _, _, err := c.PR(context.Background(), "mas-bandwidth/nova-sprint", "sprint-group/s1", "v2"); err != nil {
		t.Fatalf("PR: %v", err)
	}
	for _, cmd := range seen {
		if !cmd.CleanEnv {
			t.Fatalf("%v inherits the environment", cmd.Argv)
		}
		tokens := 0
		for _, kv := range cmd.Env {
			switch {
			case kv == "GH_TOKEN=tok-sprint":
				tokens++
			case strings.HasPrefix(kv, "GH_TOKEN="), strings.HasPrefix(kv, "GITHUB_TOKEN="), strings.HasPrefix(kv, "GH_CONFIG_DIR="):
				t.Fatalf("%v carries the inherited %s", cmd.Argv, kv)
			}
		}
		if tokens != 1 || strings.Contains(strings.Join(cmd.Argv, " "), "tok-sprint") {
			t.Fatalf("%v: the token is not exactly once in the environment and never in argv", cmd.Argv)
		}
	}
}

func TestClientGraphQLBatchAndClassifications(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)

	c := New(Config{
		Identity: Identity{Name: "sprint", Token: "tok-rowan"},
		Now:      func() int64 { return now },
		Exec:     ExecFunc(fakeGraphQLExec_internal),
	})

	rollups, err := c.Checks(ctx, "mas-bandwidth/nova-sprint", []string{shaA, shaB, shaC, shaD})
	if err != nil {
		t.Fatalf("Checks: %v", err)
	}
	if rollups[shaA].State != CheckGreen {
		t.Fatalf("shaA state = %s, want green", rollups[shaA].State)
	}
	if rollups[shaB].State != CheckRed || len(rollups[shaB].Failing) != 1 || rollups[shaB].Failing[0] != "race-gate" || rollups[shaB].URL != "https://ci.example/1" {
		t.Fatalf("shaB = %+v", rollups[shaB])
	}
	if rollups[shaC].State != CheckPending {
		t.Fatalf("shaC state = %s, want pending", rollups[shaC].State)
	}
	if rollups[shaD].State != CheckFault {
		t.Fatalf("cancelled check must be CheckFault (blames no head), got %+v", rollups[shaD])
	}

	many := make([]string, 101)
	for i := range many {
		many[i] = fmt.Sprintf("%040x", i+1)
	}
	if _, err := c.Checks(ctx, "mas-bandwidth/nova-sprint", many); err == nil || !strings.Contains(err.Error(), "exceeds 100 SHAs") {
		t.Fatalf("expected >100 batch refusal, got %v", err)
	}
	if _, err := c.Checks(ctx, "mas-bandwidth/nova-sprint", []string{"abc"}); err == nil || !strings.Contains(err.Error(), "40-character hex") {
		t.Fatalf("expected short SHA refusal, got %v", err)
	}
}

func fakeGraphQLExec_internal(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
	hasToken := false
	for _, kv := range cmd.Env {
		if kv == "GH_TOKEN=tok-rowan" {
			hasToken = true
		}
	}
	if !hasToken {
		return edge.Result{Stderr: "missing GH_TOKEN", ExitCode: 1}, nil
	}
	if len(cmd.Argv) >= 3 && cmd.Argv[1] == "auth" && cmd.Argv[2] == "status" {
		return edge.Result{Stdout: "Logged in to github.com account rowan-claude"}, nil
	}
	if len(cmd.Argv) >= 3 && cmd.Argv[1] == "api" && cmd.Argv[2] == "graphql" {
		return edge.Result{Stdout: `{"data":{"repository":{` +
			`"c0":{"statusCheckRollup":{"state":"SUCCESS","contexts":{"nodes":[{"__typename":"CheckRun","name":"linux-test","status":"COMPLETED","conclusion":"SUCCESS"}]}}},` +
			`"c1":{"statusCheckRollup":{"state":"FAILURE","contexts":{"nodes":[{"__typename":"CheckRun","name":"race-gate","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"https://ci.example/1"}]}}},` +
			`"c2":{"statusCheckRollup":{"state":"PENDING","contexts":{"nodes":[{"__typename":"CheckRun","name":"tlc","status":"IN_PROGRESS","conclusion":""}]}}},` +
			`"c3":{"statusCheckRollup":{"state":"FAILURE","contexts":{"nodes":[{"__typename":"CheckRun","name":"runner-pool","status":"COMPLETED","conclusion":"CANCELLED","detailsUrl":"https://ci.example/cancel"}]}}}` +
			`}}}`}, nil
	}
	return edge.Result{Stderr: "unexpected command", ExitCode: 1}, nil
}

// Merge reads the repository's allowed method once, merges only at the
// gated head (--match-head-commit), and returns the merge commit GitHub
// recorded; a PR left open (a merge queue) is an error, never landed.
func TestMergeByAllowedMethodAtTheGatedHead(t *testing.T) {
	t.Parallel()
	var argv []string
	state := "MERGED"
	c := New(Config{
		Identity: Identity{Name: "sprint"},
		Now:      func() int64 { return 1000 },
		Exec: ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
			a := strings.Join(cmd.Argv[1:], " ")
			argv = append(argv, a)
			switch {
			case strings.HasPrefix(a, "repo view"):
				return edge.Result{Stdout: `{"mergeCommitAllowed":false,"squashMergeAllowed":true,"rebaseMergeAllowed":true}`}, nil
			case strings.HasPrefix(a, "pr view"):
				return edge.Result{Stdout: `{"state":"` + state + `","mergeCommit":{"oid":"` + shaD + `"}}`}, nil
			}
			return edge.Result{}, nil
		}),
	})
	got, err := c.Merge(context.Background(), "o/r", 7, shaB)
	if err != nil || got != shaD {
		t.Fatalf("Merge = %q, %v; want %s", got, err, shaD)
	}
	if !slices.Contains(argv, "pr merge 7 --repo o/r --squash --match-head-commit "+shaB) {
		t.Fatalf("no squash merge at the gated head in %q", argv)
	}
	state = "OPEN"
	if _, err := c.Merge(context.Background(), "o/r", 7, shaB); err == nil || !strings.Contains(err.Error(), "merge queue") {
		t.Fatalf("an unmerged PR was not an error: %v", err)
	}
	if n := len(slices.DeleteFunc(slices.Clone(argv), func(a string) bool { return !strings.HasPrefix(a, "repo view") })); n != 1 {
		t.Fatalf("the merge method was read %d times, want once", n)
	}
}

func TestFirstFailing(t *testing.T) {
	t.Parallel()
	log := "setup\nok 1\n--- FAIL: TestX (0.00s)\n    x_test.go:9: want 1\nFAIL\n"
	if got := FirstFailing(log, 5); !slices.Equal(got, []string{"--- FAIL: TestX (0.00s)", "FAIL"}) {
		t.Fatalf("FirstFailing = %q", got)
	}
	if got := FirstFailing("a\nb\nc", 2); !slices.Equal(got, []string{"b", "c"}) {
		t.Fatalf("no failing line: last lines = %q", got)
	}
}

func TestRateLimitRestsIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)

	c := New(Config{
		Identity: Identity{Name: "amd-1.ds", Token: "tok-1"},
		Now:      func() int64 { return now },
		Exec: ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
			if len(cmd.Argv) >= 3 && cmd.Argv[1] == "auth" {
				return edge.Result{Stdout: "Logged in to github.com account rowan-claude"}, nil
			}
			if now < 1045 {
				return edge.Result{Stderr: "HTTP 429: secondary rate limit exceeded\nRetry-After: 45", ExitCode: 1}, nil
			}
			return edge.Result{Stdout: `[]`}, nil
		}),
	})

	_, _, err := c.PR(ctx, "mas-bandwidth/nova-sprint", "sprint/s1/group", "v2")
	var fe *FaultError
	if !errors.As(err, &fe) || fe.Kind != FaultRateLimit || fe.RetryAfter != 1045 || c.RestedUntil() != 1045 {
		t.Fatalf("expected FaultRateLimit until 1045, got %v (RestedUntil=%d)", err, c.RestedUntil())
	}

	now = 1020
	if _, _, err := c.PR(ctx, "mas-bandwidth/nova-sprint", "sprint/s1/group", "v2"); !errors.As(err, &fe) || fe.RetryAfter != 1045 {
		t.Fatalf("expected still resting at 1020, got %v", err)
	}

	now = 1046
	if _, ok, err := c.PR(ctx, "mas-bandwidth/nova-sprint", "sprint/s1/group", "v2"); err != nil || ok {
		t.Fatalf("expected recovery after rest window at 1046, got ok=%v err=%v", ok, err)
	}
}

func TestRestartRequeryIdempotence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)
	world := NewFakeWorld()
	repo := "mas-bandwidth/nova-sprint"

	edge1 := NewFakeWithWorld(world, func() int64 { return now })
	edge1.SetCheck(repo, shaA, CheckRollup{SHA: shaA, State: CheckGreen})

	res1, err := edge1.Checks(ctx, repo, []string{shaA})
	if err != nil {
		t.Fatalf("edge1 Checks: %v", err)
	}
	edge2 := NewFakeWithWorld(world, func() int64 { return now })
	res2, err := edge2.Checks(ctx, repo, []string{shaA})
	if err != nil || res1[shaA].State != res2[shaA].State || res2[shaA].State != CheckGreen {
		t.Fatalf("restart query mismatch: res1=%+v res2=%+v err=%v", res1, res2, err)
	}
}

// TestAPIPathTakesOwnerRepoFromAURL: a stream whose repository is a clone
// URL gets owner/repo in every api path, never the URL pasted in.
func TestAPIPathTakesOwnerRepoFromAURL(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"o/r":                         "o/r",
		"https://github.com/o/r":      "o/r",
		"https://github.com/o/r.git":  "o/r",
		"https://github.com/o/r.git/": "o/r",
		"git@github.com:o/r.git":      "o/r",
		"ssh://git@github.com/o/r":    "o/r",
		"/srv/git/r.git":              "/srv/git/r.git",
	} {
		if got, err := RepoPath(in); got != want || err != nil {
			t.Errorf("RepoPath(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"https://gitlab.example.com/o/r.git", "git@gitlab.example.com:o/r.git"} { // another host is never github.com's o/r
		if got, err := RepoPath(in); err == nil || !strings.Contains(err.Error(), in) {
			t.Errorf("RepoPath(%q) = %q, %v, want a refusal naming the URL", in, got, err)
		}
	}
}
