package git

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
)

// landWorld_internal is base v2 at root and two cards cut from it, c1 and
// c2, on their branches; base has not moved.
func landWorld_internal() (*Fake, *fakeForge_internal, string, string, string) {
	gf := NewFake(func() int64 { return 1000 })
	root := gf.AddCommit("", map[string]string{"README.md": "v2\n"}, "v2")
	c1 := gf.AddCommit(root, map[string]string{"a.go": "package a\n"}, "sprint/s1/c1")
	c2 := gf.AddCommit(root, map[string]string{"b.go": "package b\n"}, "sprint/s1/c2")
	ff := newFakeForge_internal()
	ff.OnMerge = func(sha string) (string, error) { gf.SetOriginRef("v2", sha); return sha, nil }
	return gf, ff, root, c1, c2
}

func one_internal(c1 string) LandRequest {
	return LandRequest{Repo: "o/r", Stream: "s1", Base: "v2", Heads: []string{c1}, Branches: []string{"sprint/s1/c1"}}
}

// Rung 1: one head that contains the base tip is gated on its own branch's
// PR at that exact head and merged when green.
func TestLandRungOneGatesTheCardsOwnPR(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gf, ff, root, c1, _ := landWorld_internal()
	res, err := LandPass(ctx, gf, ff, one_internal(c1))
	if err != nil || res.Outcome != OutcomeAbsent || res.GatedSHA != c1 || res.Rung != 1 || res.BaseTip != root {
		t.Fatalf("pass 1 = %+v err=%v, want ci_absent at %s", res, err, c1)
	}
	if ff.prs[0].HeadRef != "sprint/s1/c1" || ff.prs[0].BaseRef != "v2" {
		t.Fatalf("the PR is %+v, want sprint/s1/c1 -> v2", ff.prs[0])
	}
	ff.SetCheck("o/r", c1, forge.CheckRollup{State: forge.CheckPending})
	if res, _ = LandPass(ctx, gf, ff, one_internal(c1)); res.Outcome != OutcomePending {
		t.Fatalf("pass 2 = %+v, want ci_pending", res)
	}
	ff.SetCheck("o/r", c1, forge.CheckRollup{State: forge.CheckGreen})
	res, err = LandPass(ctx, gf, ff, one_internal(c1))
	if err != nil || res.Outcome != OutcomeLanded || res.MergeSHA != c1 || !slices.Equal(res.LandedHeads, []string{c1}) || len(ff.prs) != 1 {
		t.Fatalf("pass 3 = %+v err=%v, want landed with one PR", res, err)
	}
	if again, _ := LandPass(ctx, gf, ff, one_internal(c1)); again.Outcome != OutcomeLanded || again.MergeSHA != c1 {
		t.Fatalf("a pass after the merge = %+v, want landed (the head is on base)", again)
	}
}

// Rung 2: more than one head are merged onto the base tip in the group
// branch (never under sprint/), one PR from it, landed together.
func TestLandRungTwoGatesTheGroup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gf, ff, root, c1, c2 := landWorld_internal()
	req := LandRequest{Repo: "o/r", Stream: "s1", Base: "v2", Heads: []string{c1, c2}, Branches: []string{"sprint/s1/c1", "sprint/s1/c2"}}
	res, err := LandPass(ctx, gf, ff, req)
	if err != nil || res.Outcome != OutcomeAbsent || res.Rung != 2 || res.GatedSHA == "" || gf.OriginRef(GroupRef("s1")) != res.GatedSHA {
		t.Fatalf("pass 1 = %+v err=%v, want ci_absent on the pushed group", res, err)
	}
	if ff.prs[0].HeadRef != "sprint-group/s1" {
		t.Fatalf("the PR's head is %s, want sprint-group/s1", ff.prs[0].HeadRef)
	}
	gated := res.GatedSHA
	ff.SetCheck("o/r", gated, forge.CheckRollup{State: forge.CheckGreen})
	res, err = LandPass(ctx, gf, ff, req)
	if err != nil || res.Outcome != OutcomeLanded || res.GatedSHA != gated || res.MergeSHA != gated || len(res.LandedHeads) != 2 {
		t.Fatalf("pass 2 = %+v err=%v, want landed at the same gated %s", res, err, gated)
	}
	if ok, _ := gf.IsAncestor(ctx, "", root, gated); !ok {
		t.Fatal("the group does not contain the base it merged onto")
	}
}

// One head behind base goes through the group, so the gated sha contains
// the base tip it merges onto.
func TestLandOneHeadBehindBaseGoesThroughTheGroup(t *testing.T) {
	t.Parallel()
	gf, ff, root, c1, _ := landWorld_internal()
	moved := gf.AddCommit(root, map[string]string{"m.go": "package m\n"}, "v2")
	res, err := LandPass(context.Background(), gf, ff, one_internal(c1))
	if err != nil || res.Outcome != OutcomeAbsent || res.Rung != 2 || res.GatedSHA == c1 || ff.prs[0].HeadRef != "sprint-group/s1" {
		t.Fatalf("pass = %+v err=%v, want the group gated", res, err)
	}
	if ok, _ := gf.IsAncestor(context.Background(), "", moved, res.GatedSHA); !ok {
		t.Fatal("the gated sha does not contain the moved base")
	}
}

// Anything not green-and-merge is an outcome with its facts, and nothing
// lands: red (with its new red's jobs and lines), a CI fault, a conflict, a PR that
// cannot be opened, a refused merge, a moved branch, a missing branch.
func TestLandEveryOtherOutcomeLandsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := []struct {
		name string
		set  func(gf *Fake, ff *fakeForge_internal, c1 string)
		want LandOutcome
	}{
		{"red", func(_ *Fake, ff *fakeForge_internal, c1 string) {
			ff.SetCheck("o/r", c1, forge.CheckRollup{State: forge.CheckRed, Failing: []string{"unit"}, URL: "https://github.com/o/r/actions/runs/1/job/2"})
		}, OutcomeRed},
		{"fault", func(_ *Fake, ff *fakeForge_internal, c1 string) {
			ff.SetCheck("o/r", c1, forge.CheckRollup{State: forge.CheckFault})
		}, OutcomeCIFault},
		{"conflicting", func(_ *Fake, ff *fakeForge_internal, _ string) { ff.Mergeable = "CONFLICTING" }, OutcomeConflict},
		{"unopenable", func(_ *Fake, ff *fakeForge_internal, _ string) { ff.OpenErr = errors.New("HTTP 403") }, OutcomeUnopenable},
		{"refused", func(_ *Fake, ff *fakeForge_internal, c1 string) {
			ff.SetCheck("o/r", c1, forge.CheckRollup{State: forge.CheckGreen})
			ff.OnMerge = func(string) (string, error) { return "", errors.New("required reviews missing") }
		}, OutcomeRefused},
		{"moved", func(gf *Fake, _ *fakeForge_internal, c1 string) {
			gf.SetOriginRef("sprint/s1/c1", gf.AddCommit(c1, map[string]string{"z": "z\n"}, ""))
		}, OutcomeMoved},
		{"missing", func(gf *Fake, _ *fakeForge_internal, _ string) { gf.RemoveFromOrigin("refs/heads/sprint/s1/c1") }, OutcomeMissing},
	}
	for _, c := range cases {
		gf, ff, root, c1, _ := landWorld_internal()
		c.set(gf, ff, c1)
		res, err := LandPass(ctx, gf, ff, one_internal(c1))
		if err != nil || res.Outcome != c.want || res.Reason == "" || gf.OriginRef("v2") != root {
			t.Fatalf("%s: %+v err=%v, want %s with a reason and base unmoved", c.name, res, err, c.want)
		}
		if c.want == OutcomeRed && (!slices.Equal(res.FailingCI, []string{"unit"}) || len(res.NewRed) != 1 || len(res.NewRed[0].Jobs[0].Lines) == 0) {
			t.Fatalf("red carries no failing checks, new red or lines: %+v", res)
		}
	}
}

// A group whose heads do not merge onto base is a conflict naming the files.
func TestLandGroupConflict(t *testing.T) {
	t.Parallel()
	gf, ff, root, c1, _ := landWorld_internal()
	c3 := gf.AddCommit(root, map[string]string{"a.go": "package other\n"}, "sprint/s1/c3")
	req := LandRequest{Repo: "o/r", Stream: "s1", Base: "v2", Heads: []string{c1, c3}, Branches: []string{"sprint/s1/c1", "sprint/s1/c3"}}
	res, err := LandPass(context.Background(), gf, ff, req)
	if err != nil || res.Outcome != OutcomeConflict || !slices.Equal(res.Conflicts, []string{"a.go"}) || len(ff.prs) != 0 {
		t.Fatalf("pass = %+v err=%v, want a conflict on a.go and no PR", res, err)
	}
}

// A group (rung 2) whose card branch has moved past the head the read
// passed is head_moved, as one head is: nothing is gated and no group
// branch is pushed.
func TestLandGroupMovedHead(t *testing.T) {
	t.Parallel()
	gf, ff, root, c1, _ := landWorld_internal()
	c3 := gf.AddCommit(root, map[string]string{"c.go": "package c\n"}, "sprint/s1/c3")
	gf.SetOriginRef("sprint/s1/c3", gf.AddCommit(c3, map[string]string{"z": "z\n"}, ""))
	req := LandRequest{Repo: "o/r", Stream: "s1", Base: "v2", Heads: []string{c1, c3}, Branches: []string{"sprint/s1/c1", "sprint/s1/c3"}}
	res, err := LandPass(context.Background(), gf, ff, req)
	if err != nil || res.Outcome != OutcomeMoved || !strings.Contains(res.Reason, "sprint/s1/c3") || len(ff.prs) != 0 || gf.OriginRef(GroupBranch("s1")) != "" {
		t.Fatalf("pass = %+v err=%v, want head_moved naming sprint/s1/c3, no PR and no group branch", res, err)
	}
}

// Base moving between the gate and the merge merges nothing.
func TestLandBaseMovedAfterTheGate(t *testing.T) {
	t.Parallel()
	gf, ff, root, c1, _ := landWorld_internal()
	ff.SetCheck("o/r", c1, forge.CheckRollup{State: forge.CheckGreen})
	ff.OnCall = func(op string) {
		if op == "Checks" {
			gf.AddCommit(root, map[string]string{"m.go": "package m\n"}, "v2")
		}
	}
	res, err := LandPass(context.Background(), gf, ff, one_internal(c1))
	if err != nil || res.Outcome != OutcomeBaseMoved || slices.Contains(ff.calls_internal(), "Merge") {
		t.Fatalf("pass = %+v err=%v, want base_moved and no merge", res, err)
	}
}

// The group branch's name never collides with a base sprint/<stream> or a
// card branch sprint/<stream>/<card>; what blocks it is named.
func TestGroupBranchNamespace(t *testing.T) {
	t.Parallel()
	if GroupBranch("s1") != "sprint-group/s1" || GroupRef("s1") != "refs/heads/sprint-group/s1" {
		t.Fatalf("GroupBranch = %s, GroupRef = %s", GroupBranch("s1"), GroupRef("s1"))
	}
	for _, ok := range []string{"sprint/s1/p1", "sprint/s1"} {
		if err := ValidateCardBranch("s1", ok); err != nil {
			t.Fatalf("ValidateCardBranch(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", "sprint-group/s1", "refs/heads/sprint-group/s1/x"} {
		if err := ValidateCardBranch("s1", bad); err == nil {
			t.Fatalf("ValidateCardBranch(%q) was not refused", bad)
		}
	}
	heads := map[string]string{"refs/heads/sprint/s1": "a", "refs/heads/sprint-group/s2": "b"}
	if b := GroupBlocker("s1", heads); b != "" {
		t.Fatalf("GroupBlocker = %q with only a base sprint/s1 and another stream's group", b)
	}
	heads["refs/heads/sprint-group"] = "c"
	heads["refs/heads/sprint-group/s1/x"] = "d"
	if b := GroupBlocker("s1", heads); b != "sprint-group, sprint-group/s1/x" {
		t.Fatalf("GroupBlocker = %q", b)
	}
	for _, ok := range []string{"s1", "a.b", "a_b-c"} {
		if p := GroupNameProblem(ok); p != "" {
			t.Fatalf("GroupNameProblem(%q) = %q", ok, p)
		}
	}
	for _, bad := range []string{"", ".s", "s.", "a..b", "s.lock"} {
		if GroupNameProblem(bad) == "" {
			t.Fatalf("GroupNameProblem(%q) said nothing", bad)
		}
	}
}

// With a token, every git call answers credentials with it alone: the
// host's helpers are reset (an empty credential.helper) and the token is in
// the environment, never in argv (the landing's push was the
// host's own login). Without one, nothing is added.
func TestTokenIsTheOnlyCredential(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 40)
	var seen []edge.Cmd
	runner := ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		seen = append(seen, cmd)
		return edge.Result{}, nil
	})
	c := New(Config{Exec: runner, Token: "tok-sprint"})
	if err := c.Push(context.Background(), "/repo", sha, GroupRef("s1"), ""); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(seen) != 1 || strings.Contains(strings.Join(seen[0].Argv, " "), "tok-sprint") {
		t.Fatalf("push argv %v", seen)
	}
	env := strings.Join(seen[0].Env, "\n")
	for _, want := range []string{"NOVA_SPRINT_GIT_TOKEN=tok-sprint", "GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=credential.helper\nGIT_CONFIG_VALUE_0=\n", "GIT_CONFIG_KEY_1=credential.helper", `password=$NOVA_SPRINT_GIT_TOKEN`} {
		if !strings.Contains(env+"\n", want) {
			t.Fatalf("push env lacks %q:\n%s", want, env)
		}
	}
	seen = nil
	if err := New(Config{Exec: runner}).Push(context.Background(), "/repo", sha, GroupRef("s1"), ""); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if strings.Contains(strings.Join(seen[0].Env, "\n"), "credential.helper") {
		t.Fatalf("a client with no token changed the credential helpers: %v", seen[0].Env)
	}
}
