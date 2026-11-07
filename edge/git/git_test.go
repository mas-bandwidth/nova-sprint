package git

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
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
	ff := forge.NewFake(forge.Identity{Name: "sprint", ExpectedLogin: "rowan-claude", Token: "tok"}, func() int64 { return now })

	base := gf.AddCommit("", map[string]string{"README.md": "v2\n"}, "v2")
	head1 := gf.AddCommit(base, map[string]string{"a.go": "package a\n"}, "card/s1/c1")
	tree1, _ := gf.TreeHash(ctx, "", head1)

	res, err := LandPass(ctx, gf, ff, LandRequest{
		Repo:        "mas-bandwidth/nova-sprint",
		Stream:      "s1",
		Base:        "v2",
		Heads:       []string{head1},
		PassedTrees: map[string]bool{tree1: true},
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
	ff := forge.NewFake(forge.Identity{Name: "sprint", ExpectedLogin: "rowan-claude", Token: "tok"}, func() int64 { return now })

	base0 := gf.AddCommit("", map[string]string{"README.md": "v2\n"}, "v2")
	head1 := gf.AddCommit(base0, map[string]string{"a.go": "package a\n"}, "card/s1/c1")
	head2 := gf.AddCommit(base0, map[string]string{"b.go": "package b\n"}, "card/s1/c2")
	base1 := gf.AddCommit(base0, map[string]string{"other.go": "package other\n"}, "")

	movedOnce := false
	ff.OnCall = func(op string) {
		if op == "Checks" && !movedOnce {
			movedOnce = true
			gf.SetOriginRef("v2", base1)
		}
	}
	for seq := 1; seq <= 20; seq++ {
		ff.SetCheck("mas-bandwidth/nova-sprint", formatSeqSHA_internal(seq), forge.CheckRollup{State: forge.CheckGreen})
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
	if res.Outcome != OutcomeLanded || !res.Remerged || gf.OriginRef("v2") != res.BaseTip || gf.OriginRef("sprint/s1") != res.BaseTip {
		t.Fatalf("unexpected remerge LandResult: %+v", res)
	}
	for _, h := range []string{base1, head1, head2} {
		if ok, _ := gf.IsAncestor(ctx, "", h, res.BaseTip); !ok {
			t.Fatalf("expected %s to be ancestor of landed base %s", h, res.BaseTip)
		}
	}
}

func formatSeqSHA_internal(seq int) string {
	const hex = "0123456789abcdef"
	buf := make([]byte, 40)
	for i := range buf {
		buf[i] = '0'
	}
	v := seq
	for i := 39; i >= 0 && v > 0; i-- {
		buf[i] = hex[v&0xf]
		v >>= 4
	}
	return string(buf)
}

func TestLedgerMergeConflictAndMissingHeadRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)
	gf := NewFake(func() int64 { return now })
	ff := forge.NewFake(forge.Identity{Name: "sprint", ExpectedLogin: "rowan-claude", Token: "tok"}, func() int64 { return now })
	for seq := 1; seq <= 20; seq++ {
		ff.SetCheck("mas-bandwidth/nova-sprint", formatSeqSHA_internal(seq), forge.CheckRollup{State: forge.CheckGreen})
	}

	base := gf.AddCommit("", map[string]string{"tla/RUNS.tsv": "row0\n", "code.go": "v0\n"}, "v2")
	h1 := gf.AddCommit(base, map[string]string{"tla/RUNS.tsv": "row0\nrow1\n"}, "card/s1/c1")
	h2 := gf.AddCommit(base, map[string]string{"tla/RUNS.tsv": "row0\nrow2\n"}, "card/s1/c2")

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

	if err := gf.Push(ctx, "", h1, "refs/heads/v2", base); err == nil || !strings.Contains(err.Error(), "force-with-lease is forbidden on base branch") {
		t.Fatalf("expected force-with-lease refusal on base branch, got %v", err)
	}
}

func TestBisectRedGroupAndFaultNoBlame(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)
	gf := NewFake(func() int64 { return now })
	ff := forge.NewFake(forge.Identity{Name: "sprint", ExpectedLogin: "rowan-claude", Token: "tok"}, func() int64 { return now })

	base := gf.AddCommit("", map[string]string{"base.txt": "1\n"}, "v2")
	h1 := gf.AddCommit(base, map[string]string{"f1.txt": "1\n"}, "card/s1/c1")
	h2 := gf.AddCommit(base, map[string]string{"f2.txt": "2\n"}, "card/s1/c2")
	h3 := gf.AddCommit(base, map[string]string{"f3.txt": "3\n"}, "card/s1/c3")

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

func TestLanderRestartIdempotenceAndInflightDedup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := int64(1000)
	gitWorld := NewFakeWorld()
	forgeWorld := forge.NewFakeWorld("rowan-claude")
	id := forge.Identity{Name: "sprint", ExpectedLogin: "rowan-claude", Token: "tok"}

	g1 := NewFakeWithWorld(gitWorld, func() int64 { return now })
	f1 := forge.NewFakeWithWorld(forgeWorld, id, func() int64 { return now })
	for seq := 1; seq <= 20; seq++ {
		f1.SetCheck("mas-bandwidth/nova-sprint", formatSeqSHA_internal(seq), forge.CheckRollup{State: forge.CheckGreen})
	}
	base := g1.AddCommit("", map[string]string{"a.txt": "0\n"}, "v2")
	h1 := g1.AddCommit(base, map[string]string{"b.txt": "1\n"}, "card/s1/c1")

	lander1 := NewLander(g1, f1)
	eReq := edge.EdgeRequest{Kind: edge.FactLand, Card: "c1", SHA: h1, After: []string{"op-10"}}
	lReq := LandRequest{Repo: "mas-bandwidth/nova-sprint", Stream: "s1", Base: "v2", Heads: []string{h1}}
	res1, err := lander1.Execute(ctx, eReq, lReq)
	if err != nil || res1.Outcome != string(OutcomeLanded) {
		t.Fatalf("lander1 Execute: %+v err=%v", res1, err)
	}

	g2 := NewFakeWithWorld(gitWorld, func() int64 { return now })
	f2 := forge.NewFakeWithWorld(forgeWorld, id, func() int64 { return now })
	lander2 := NewLander(g2, f2)
	res2, err := lander2.Execute(ctx, eReq, lReq)
	if err != nil || res1.ID != res2.ID || res1.Outcome != res2.Outcome || res1.BaseTip != res2.BaseTip {
		t.Fatalf("restart idempotence mismatch: res1=%+v res2=%+v err=%v", res1, res2, err)
	}
	assertLanderDedup_internal(t, ctx, lander2, eReq, lReq)
}

func assertLanderDedup_internal(t *testing.T, ctx context.Context, l *Lander, eReq edge.EdgeRequest, lReq LandRequest) {
	t.Helper()
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	l.OnStep = func(_ string) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
	}
	var wg sync.WaitGroup
	var outA, outB edge.EdgeResult
	wg.Add(2)
	go func() {
		defer wg.Done()
		outA, _ = l.Execute(ctx, eReq, lReq)
	}()
	<-entered
	go func() {
		defer wg.Done()
		outB, _ = l.Execute(ctx, eReq, lReq)
	}()
	time.Sleep(5 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 || outA.ID != outB.ID {
		t.Fatalf("expected 1 in-flight execution, got %d (outA=%+v outB=%+v)", calls.Load(), outA, outB)
	}
}
