package git

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
)

func TestLandPassSingleHeadReuseAndFastForward(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)
	gf := NewFake(func() int64 { return now })
	ff := newFakeForge_internal()

	base := gf.AddCommit("", map[string]string{"README.md": "v2\n"}, "v2")
	head1 := gf.AddCommit(base, map[string]string{"a.go": "package a\n"}, "sprint/s1/c1")

	res, err := LandPass(ctx, gf, ff, LandRequest{
		Repo:       "mas-bandwidth/nova-sprint",
		Stream:     "s1",
		Base:       "v2",
		Heads:      []string{head1},
		PassedSHAs: map[string]bool{head1: true},
	})
	if err != nil {
		t.Fatalf("LandPass: %v", err)
	}
	if res.Outcome != OutcomeLanded || !res.ReusedCI || res.BaseTip != head1 || res.StreamTip != head1 {
		t.Fatalf("unexpected single-head LandResult: %+v", res)
	}
}

func TestLandPassRemergeWhenBaseMoved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)
	gf := NewFake(func() int64 { return now })
	ff := newFakeForge_internal()

	base0 := gf.AddCommit("", map[string]string{"README.md": "v2\n"}, "v2")
	head1 := gf.AddCommit(base0, map[string]string{"a.go": "package a\n"}, "sprint/s1/c1")
	head2 := gf.AddCommit(base0, map[string]string{"b.go": "package b\n"}, "sprint/s1/c2")
	base1 := gf.AddCommit(base0, map[string]string{"other.go": "package other\n"}, "")

	movedOnce := false
	ff.OnCall = func(op string) {
		if op == "Checks" {
			gf.world.mu.Lock()
			for sha := range gf.world.commits {
				ff.SetCheck("mas-bandwidth/nova-sprint", sha, forge.CheckRollup{State: forge.CheckGreen})
			}
			gf.world.mu.Unlock()
			if !movedOnce {
				movedOnce = true
				gf.SetOriginRef("v2", base1)
			}
		}
	}

	res, err := LandPass(ctx, gf, ff, LandRequest{
		Repo:   "mas-bandwidth/nova-sprint",
		Stream: "s1",
		Base:   "v2",
		Heads:  []string{head1, head2},
	})
	if err != nil {
		t.Fatalf("LandPass: %v", err)
	}
	if res.Outcome != OutcomeLanded || !res.Remerged || gf.OriginRef("v2") != res.BaseTip || gf.OriginRef("sprint/s1/group") != res.BaseTip {
		t.Fatalf("unexpected remerge LandResult: %+v", res)
	}
	for _, h := range []string{base1, head1, head2} {
		if ok, _ := gf.IsAncestor(ctx, "", h, res.BaseTip); !ok {
			t.Fatalf("expected %s to be ancestor of landed base %s", h, res.BaseTip)
		}
	}
}

func TestLedgerMergeConflictAndMissingHeadRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)
	gf := NewFake(func() int64 { return now })
	ff := newFakeForge_internal()
	ff.OnCall = func(op string) {
		if op == "Checks" {
			gf.world.mu.Lock()
			for sha := range gf.world.commits {
				ff.SetCheck("mas-bandwidth/nova-sprint", sha, forge.CheckRollup{State: forge.CheckGreen})
			}
			gf.world.mu.Unlock()
		}
	}

	base := gf.AddCommit("", map[string]string{"tla/RUNS.tsv": "row0\n", "code.go": "v0\n"}, "v2")
	h1 := gf.AddCommit(base, map[string]string{"tla/RUNS.tsv": "row0\nrow1\n"}, "sprint/s1/c1")
	h2 := gf.AddCommit(base, map[string]string{"tla/RUNS.tsv": "row0\nrow2\n"}, "sprint/s1/c2")

	res, err := LandPass(ctx, gf, ff, LandRequest{Repo: "mas-bandwidth/nova-sprint", Stream: "s1", Base: "v2", Heads: []string{h1, h2}})
	if err != nil || res.Outcome != OutcomeRung3Conflict || len(res.Conflicts) != 1 || res.Conflicts[0] != "tla/RUNS.tsv" {
		t.Fatalf("expected Rung3Conflict on tla/RUNS.tsv, got %+v err=%v", res, err)
	}

	res, err = LandPass(ctx, gf, ff, LandRequest{
		Repo:    "mas-bandwidth/nova-sprint",
		Stream:  "s1",
		Base:    "v2",
		Heads:   []string{h1, h2},
		Ledgers: []LedgerRule{{Path: "tla/RUNS.tsv"}},
	})
	if err != nil || res.Outcome != OutcomeLanded {
		t.Fatalf("expected ledger merge to land, got %+v err=%v", res, err)
	}

	missingSHA := "9999999999999999999999999999999999999999"
	res, err = LandPass(ctx, gf, ff, LandRequest{Repo: "mas-bandwidth/nova-sprint", Stream: "s1", Base: "v2", Heads: []string{missingSHA}})
	if err != nil || res.Outcome != OutcomeMissingHead || res.MissingHead != missingSHA || !strings.Contains(res.Reason, "not on origin") {
		t.Fatalf("expected OutcomeMissingHead, got %+v err=%v", res, err)
	}

	if err := gf.Push(ctx, "", h1, "refs/heads/v2", h2); err == nil || !strings.Contains(err.Error(), "force-with-lease is forbidden on base branch") {
		t.Fatalf("expected force-with-lease refusal on base branch, got %v", err)
	}
}

func TestBisectRedGroupAndFaultNoBlame(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)
	gf := NewFake(func() int64 { return now })
	ff := newFakeForge_internal()

	base := gf.AddCommit("", map[string]string{"base.txt": "1\n"}, "v2")
	h1 := gf.AddCommit(base, map[string]string{"f1.txt": "1\n"}, "sprint/s1/c1")
	h2 := gf.AddCommit(base, map[string]string{"f2.txt": "2\n"}, "sprint/s1/c2")
	h3 := gf.AddCommit(base, map[string]string{"f3.txt": "3\n"}, "sprint/s1/c3")

	ff.OnCall = func(op string) {
		if op == "Checks" {
			gf.world.mu.Lock()
			for sha, c := range gf.world.commits {
				st := forge.CheckGreen
				if c.Files["f2.txt"] == "2\n" {
					st = forge.CheckRed
				}
				ff.SetCheck("mas-bandwidth/nova-sprint", sha, forge.CheckRollup{State: st, Failing: []string{"unit"}})
			}
			gf.world.mu.Unlock()
		}
	}

	req := LandRequest{Repo: "mas-bandwidth/nova-sprint", Stream: "s1", Base: "v2", Heads: []string{h1, h2, h3}, BisectOnRed: true}
	res, err := LandPass(ctx, gf, ff, req)
	if err != nil || res.Outcome != OutcomeCIRed || res.BlamedHead != h2 || len(res.GreenHeads) != 2 {
		t.Fatalf("expected BisectRedGroup to blame h2 (%s), got %+v err=%v", h2, res, err)
	}

	ff.OnCall = func(op string) {
		if op == "Checks" {
			gf.world.mu.Lock()
			for sha := range gf.world.commits {
				ff.SetCheck("mas-bandwidth/nova-sprint", sha, forge.CheckRollup{State: forge.CheckFault, Reason: "runner cancelled"})
			}
			gf.world.mu.Unlock()
		}
	}
	resFault, err := LandPass(ctx, gf, ff, req)
	if err != nil || resFault.Outcome != OutcomeCIFault || resFault.BlamedHead != "" {
		t.Fatalf("gate fault must blame no head, got %+v err=%v", resFault, err)
	}
}

func TestRemoteLandHostFailover(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	runner := ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		for _, a := range cmd.Argv {
			if a == "amd-1" {
				return edge.Result{Stderr: "ssh: connect timed out", ExitCode: 255}, nil
			}
			if a == "vision" {
				return edge.Result{Stdout: "LANDED ok"}, nil
			}
		}
		return edge.Result{Stderr: "unreachable", ExitCode: 255}, nil
	})

	res, err := RunRemoteLandStep(ctx, runner, []string{"amd-1", "vision", "hulk"}, 10, 30*time.Second, "--stream", "s1")
	if err != nil || res.Host != "vision" || len(res.SkippedHosts) != 1 || res.SkippedHosts[0] != "amd-1" {
		t.Fatalf("unexpected failover result: %+v err=%v", res, err)
	}
	if _, err := RunRemoteLandStep(ctx, runner, []string{"amd-1"}, 10, 30*time.Second, "--stream", "s1"); !errors.Is(err, ErrNoLandingHost) {
		t.Fatalf("expected ErrNoLandingHost, got %v", err)
	}
}

func TestLandPassRestartIdempotence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)
	gitWorld := NewFakeWorld()
	forgeWorld := newFakeForgeWorld_internal()

	g1 := NewFakeWithWorld(gitWorld, func() int64 { return now })
	f1 := newFakeForgeWithWorld_internal(forgeWorld)
	base := g1.AddCommit("", map[string]string{"a.txt": "0\n"}, "v2")
	h1 := g1.AddCommit(base, map[string]string{"b.txt": "1\n"}, "sprint/s1/c1")
	f1.SetCheck("mas-bandwidth/nova-sprint", h1, forge.CheckRollup{State: forge.CheckGreen})

	lReq := LandRequest{Repo: "mas-bandwidth/nova-sprint", Stream: "s1", Base: "v2", Heads: []string{h1}}
	res1, err := LandPass(ctx, g1, f1, lReq)
	if err != nil || res1.Outcome != OutcomeLanded {
		t.Fatalf("g1 LandPass: %+v err=%v", res1, err)
	}

	g2 := NewFakeWithWorld(gitWorld, func() int64 { return now })
	f2 := newFakeForgeWithWorld_internal(forgeWorld)
	res2, err := LandPass(ctx, g2, f2, lReq)
	if err != nil || res1.Outcome != res2.Outcome || res1.BaseTip != res2.BaseTip {
		t.Fatalf("restart idempotence mismatch: res1=%+v res2=%+v err=%v", res1, res2, err)
	}
}

func TestStreamGroupAndCardRefHierarchy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gf := NewFake(func() int64 { return 1000 })
	base := gf.AddCommit("", map[string]string{"README.md": "v2\n"}, "v2")
	h1 := gf.AddCommit(base, map[string]string{"a.go": "package a\n"}, "sprint/s1/p1")

	if StreamBranch("s1") != "sprint/s1/group" || StreamRef("s1") != "refs/heads/sprint/s1/group" {
		t.Fatalf("unexpected StreamBranch/StreamRef: %s / %s", StreamBranch("s1"), StreamRef("s1"))
	}
	if err := ValidateCardBranch("s1", "sprint/s1/p1"); err != nil {
		t.Fatalf("ValidateCardBranch(sprint/s1/p1): %v", err)
	}
	for _, bad := range []string{"group", "sprint/s1/group", "refs/heads/sprint/s1/group", "sprint/s1"} {
		if err := ValidateCardBranch("s1", bad); err == nil {
			t.Fatalf("expected ValidateCardBranch(%q) to be refused", bad)
		}
		if _, err := LandPass(ctx, gf, nil, LandRequest{Stream: "s1", Base: "v2", Heads: []string{bad}}); err == nil {
			t.Fatalf("expected LandPass with head %q to be refused", bad)
		}
	}
	if err := gf.Push(ctx, "", h1, "refs/heads/sprint/s1", ""); err == nil {
		t.Fatalf("expected bare stream ref push refs/heads/sprint/s1 to be refused")
	}
}
