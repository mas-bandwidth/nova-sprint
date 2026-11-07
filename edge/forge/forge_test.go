package forge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

const (
	shaA = "1111111111111111111111111111111111111111"
	shaB = "2222222222222222222222222222222222222222"
	shaC = "3333333333333333333333333333333333333333"
	shaD = "4444444444444444444444444444444444444444"
)

func TestCredentialsRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)

	missing := New(Config{
		Identity: Identity{Name: "amd-1.ds"},
		Now:      func() int64 { return now },
		Exec: ExecFunc(func(_ context.Context, _ edge.Cmd) (edge.Result, error) {
			t.Fatal("gh must not run without credentials")
			return edge.Result{}, nil
		}),
	})
	_, _, err := missing.PR(ctx, "mas-bandwidth/nova-sprint", "sprint/s1/group", "v2")
	var refusal *edge.Refusal
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Problem, "no GitHub token for amd-1.ds") || !strings.Contains(refusal.Fix, "nova-secrets exec --only GH_GATE_TOKEN") {
		t.Fatalf("a missing token is not a refusal with its fix: %v", err)
	}
	if missing.Ready() == nil {
		t.Fatal("Ready accepted a client with no token")
	}

	glennClient := New(Config{
		Identity: Identity{Name: "sprint", ExpectedLogin: "rowan-claude", Token: "tok-rowan"},
		Now:      func() int64 { return now },
		Exec: ExecFunc(func(_ context.Context, _ edge.Cmd) (edge.Result, error) {
			return edge.Result{Stdout: "  ✓ Logged in to github.com account gafferongames (GH_TOKEN)"}, nil
		}),
	})
	_, _, err = glennClient.PR(ctx, "mas-bandwidth/nova-sprint", "sprint/s1/group", "v2")
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Problem, "gafferongames's, expected rowan-claude") {
		t.Fatalf("the human's login was not refused: %v", err)
	}

	wrongFake := NewFake(Identity{Name: "sprint", ExpectedLogin: "rowan-claude", Token: "tok-rowan"}, func() int64 { return now })
	wrongFake.SetAuthLogin("stella-codex")
	_, _, err = wrongFake.PR(ctx, "mas-bandwidth/nova-sprint", "sprint/s1/group", "v2")
	if err == nil || !strings.Contains(err.Error(), "is stella-codex's, expected rowan-claude") {
		t.Fatalf("unexpected wrong login error: %v", err)
	}
}

// The forge acts only as its token: a GH_TOKEN, GITHUB_TOKEN or
// GH_CONFIG_DIR the process inherited never reaches gh, so the host's own
// login is never the identity (defect 39), and the token is in the
// environment only, never in argv.
func TestForgeActsOnlyAsItsToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "inherited-token")
	t.Setenv("GITHUB_TOKEN", "inherited-github")
	t.Setenv("GH_CONFIG_DIR", "/home/someone/.config/gh")
	var seen []edge.Cmd
	c := New(Config{
		Identity: Identity{Name: "sprint", ExpectedLogin: "rowan-claude", Token: "tok-sprint"},
		Now:      func() int64 { return 1000 },
		Exec: ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
			seen = append(seen, cmd)
			if cmd.Argv[1] == "auth" {
				return edge.Result{Stdout: "Logged in to github.com account rowan-claude (GH_TOKEN)"}, nil
			}
			return edge.Result{Stdout: "[]"}, nil
		}),
	})
	if _, _, err := c.PR(context.Background(), "mas-bandwidth/nova-sprint", "sprint/s1/group", "v2"); err != nil {
		t.Fatalf("PR: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("gh ran %d times, want auth status then pr list", len(seen))
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
		Identity: Identity{Name: "sprint", ExpectedLogin: "rowan-claude", Token: "tok-rowan"},
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

func TestMergeExactSHA(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)
	f := NewFake(Identity{Name: "sprint", ExpectedLogin: "rowan-claude", Token: "tok-rowan"}, func() int64 { return now })
	repo := "mas-bandwidth/nova-sprint"

	pr, err := f.OpenPR(ctx, repo, "sprint/s1/group", "v2", "sprint/s1 batch", "body")
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}
	f.SetBaseTip(repo, "v2", shaA)
	f.SetPRHead(repo, pr.Number, shaB, shaA)

	if err := f.Merge(ctx, repo, pr.Number, shaC, "v2"); !errors.Is(err, ErrStaleHead) {
		t.Fatalf("expected ErrStaleHead on wrong SHA, got %v", err)
	}
	if err := f.Merge(ctx, repo, pr.Number, shaB, "main"); !errors.Is(err, ErrBaseMoved) {
		t.Fatalf("expected ErrBaseMoved on wrong base, got %v", err)
	}
	f.SetBaseTip(repo, "v2", shaD)
	if err := f.Merge(ctx, repo, pr.Number, shaB, "v2"); !errors.Is(err, ErrBaseMoved) {
		t.Fatalf("expected ErrBaseMoved when base tip moved since gate, got %v", err)
	}
	f.SetBaseTip(repo, "v2", shaA)
	if err := f.Merge(ctx, repo, pr.Number, shaB, "v2"); err != nil {
		t.Fatalf("Merge at matching SHA and base: %v", err)
	}
}

func TestRateLimitRestsIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)

	c := New(Config{
		Identity: Identity{Name: "amd-1.ds", ExpectedLogin: "rowan-claude", Token: "tok-1"},
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
	id := Identity{Name: "sprint", ExpectedLogin: "rowan-claude", Token: "tok-sprint"}
	world := NewFakeWorld("rowan-claude")
	repo := "mas-bandwidth/nova-sprint"

	edge1 := NewFakeWithWorld(world, id, func() int64 { return now })
	edge1.SetCheck(repo, shaA, CheckRollup{SHA: shaA, State: CheckGreen})

	res1, err := edge1.Checks(ctx, repo, []string{shaA})
	if err != nil {
		t.Fatalf("edge1 Checks: %v", err)
	}
	edge2 := NewFakeWithWorld(world, id, func() int64 { return now })
	res2, err := edge2.Checks(ctx, repo, []string{shaA})
	if err != nil || res1[shaA].State != res2[shaA].State || res2[shaA].State != CheckGreen {
		t.Fatalf("restart query mismatch: res1=%+v res2=%+v err=%v", res1, res2, err)
	}
}
