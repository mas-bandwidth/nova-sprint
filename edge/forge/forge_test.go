package forge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	_, _, err := missing.PR(ctx, "mas-bandwidth/nova-sprint", "sprint/s1", "v2")
	if err == nil || !strings.Contains(err.Error(), "forge refused: neither GH_TOKEN nor GH_CONFIG_DIR is set for identity amd-1.ds: run nova-secrets exec or set GH_CONFIG_DIR") {
		t.Fatalf("unexpected missing cred error: %v", err)
	}

	glennClient := New(Config{
		Identity: Identity{Name: "sprint", ExpectedLogin: "rowan-claude", ConfigDir: "/path/rowan-gh"},
		Now:      func() int64 { return now },
		Exec: ExecFunc(func(_ context.Context, _ edge.Cmd) (edge.Result, error) {
			return edge.Result{Stdout: "  ✓ Logged in to github.com account gafferongames (GH_TOKEN)"}, nil
		}),
	})
	_, _, err = glennClient.PR(ctx, "mas-bandwidth/nova-sprint", "sprint/s1", "v2")
	wantGlenn := "forge refused: gh auth login is gafferongames, expected rowan-claude (Glenn's credentials are never used): set GH_CONFIG_DIR for rowan-claude"
	if err == nil || err.Error() != wantGlenn {
		t.Fatalf("got %v, want %s", err, wantGlenn)
	}

	wrongFake := NewFake(Identity{Name: "sprint", ExpectedLogin: "rowan-claude", ConfigDir: "/path/rowan-gh"}, func() int64 { return now })
	wrongFake.SetAuthLogin("stella-codex")
	_, _, err = wrongFake.PR(ctx, "mas-bandwidth/nova-sprint", "sprint/s1", "v2")
	if err == nil || !strings.Contains(err.Error(), "gh auth login is stella-codex, expected rowan-claude") {
		t.Fatalf("unexpected wrong login error: %v", err)
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

func TestMergeExactSHAAndBaseProtection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)
	f := NewFake(Identity{Name: "sprint", ExpectedLogin: "rowan-claude", ConfigDir: "/rowan/gh"}, func() int64 { return now })
	repo := "mas-bandwidth/nova-sprint"

	if err := f.CheckBaseProtection(ctx, repo, "v2"); err == nil || !strings.Contains(err.Error(), "SPRINT REFUSED stream add: base branch v2 on mas-bandwidth/nova-sprint does not require branches to be up to date") {
		t.Fatalf("expected base protection refusal, got %v", err)
	}
	f.SetBaseProtection(repo, "v2", true)
	if err := f.CheckBaseProtection(ctx, repo, "v2"); err != nil {
		t.Fatalf("CheckBaseProtection: %v", err)
	}

	pr, err := f.OpenPR(ctx, repo, "sprint/s1", "v2", "sprint/s1 batch", "body")
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

	_, _, err := c.PR(ctx, "mas-bandwidth/nova-sprint", "sprint/s1", "v2")
	var fe *FaultError
	if !errors.As(err, &fe) || fe.Kind != FaultRateLimit || fe.RetryAfter != 1045 || c.RestedUntil() != 1045 {
		t.Fatalf("expected FaultRateLimit until 1045, got %v (RestedUntil=%d)", err, c.RestedUntil())
	}

	now = 1020
	if _, _, err := c.PR(ctx, "mas-bandwidth/nova-sprint", "sprint/s1", "v2"); !errors.As(err, &fe) || fe.RetryAfter != 1045 {
		t.Fatalf("expected still resting at 1020, got %v", err)
	}

	now = 1046
	if _, ok, err := c.PR(ctx, "mas-bandwidth/nova-sprint", "sprint/s1", "v2"); err != nil || ok {
		t.Fatalf("expected recovery after rest window at 1046, got ok=%v err=%v", ok, err)
	}
}

func TestRestartRequeryAndInflightDedup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)
	id := Identity{Name: "sprint", ExpectedLogin: "rowan-claude", Token: "tok-sprint"}
	world := NewFakeWorld("rowan-claude")
	repo := "mas-bandwidth/nova-sprint"

	edge1 := NewFakeWithWorld(world, id, func() int64 { return now })
	edge1.SetCheck(repo, shaA, CheckRollup{SHA: shaA, State: CheckGreen})
	req := edge.EdgeRequest{Kind: edge.FactCI, Card: "card-1", SHA: shaA, After: []string{"op-1", "op-2"}}

	res1, err := edge1.Query(ctx, repo, req)
	if err != nil {
		t.Fatalf("edge1 Query: %v", err)
	}
	edge2 := NewFakeWithWorld(world, id, func() int64 { return now })
	res2, err := edge2.Query(ctx, repo, req)
	if err != nil || res1.ID != res2.ID || res1.Outcome != res2.Outcome || res2.Outcome != string(CheckGreen) {
		t.Fatalf("restart query mismatch: res1=%+v res2=%+v err=%v", res1, res2, err)
	}
	assertInflightDedup_internal(t, ctx, edge2, repo, req)
}

func assertInflightDedup_internal(t *testing.T, ctx context.Context, f *Fake, repo string, req edge.EdgeRequest) {
	t.Helper()
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	f.OnCall = func(op string) {
		if op == "Checks" {
			if calls.Add(1) == 1 {
				close(entered)
			}
			<-release
		}
	}
	var wg sync.WaitGroup
	var outA, outB edge.EdgeResult
	wg.Add(2)
	go func() {
		defer wg.Done()
		outA, _ = f.Query(ctx, repo, req)
	}()
	<-entered
	go func() {
		defer wg.Done()
		outB, _ = f.Query(ctx, repo, req)
	}()
	time.Sleep(5 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 || outA.ID != outB.ID || outA.Outcome != outB.Outcome {
		t.Fatalf("in-flight dedup calls=%d outA=%+v outB=%+v", calls.Load(), outA, outB)
	}
}
