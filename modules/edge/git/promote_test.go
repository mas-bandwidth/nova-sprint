package git

import (
	"context"
	"slices"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
)

// promoteWorld_internal is main at root and a workstream base sprint/w one
// card's merge (m1) ahead of it.
func promoteWorld_internal() (*Fake, *fakeForge_internal, string, string) {
	gf := NewFake(func() int64 { return 1000 })
	root := gf.AddCommit("", map[string]string{"README.md": "v2\n"}, "main")
	m1 := gf.AddCommit(root, map[string]string{"a.go": "package a\n"}, "sprint/w")
	ff := newFakeForge_internal()
	ff.OnMerge = func(sha string) (string, error) { gf.SetOriginRef("main", sha); return sha, nil }
	return gf, ff, root, m1
}

func promote_internal(merges ...string) PromoteRequest {
	return PromoteRequest{Repo: "o/r", Stream: "s1", Base: "sprint/w", Merges: merges}
}

// A promotion gates the one PR base -> main at base's tip, merges it green,
// and lands the merges then on main (LANDING.md, "Landed means on main");
// a merge already on main lands at once, even with its base deleted.
func TestPromoteGatesTheBaseToMain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gf, ff, root, m1 := promoteWorld_internal()
	res, err := PromotePass(ctx, gf, ff, promote_internal(m1))
	if err != nil || res.Outcome != OutcomeAbsent || res.GatedSHA != m1 || res.BaseTip != root {
		t.Fatalf("pass 1 = %+v err=%v, want ci_absent gated at the base tip %s", res, err, m1)
	}
	if ff.prs[0].HeadRef != "sprint/w" || ff.prs[0].BaseRef != "main" {
		t.Fatalf("the PR is %+v, want sprint/w -> main", ff.prs[0])
	}
	ff.SetCheck("o/r", m1, forge.CheckRollup{State: forge.CheckGreen})
	res, err = PromotePass(ctx, gf, ff, promote_internal(m1))
	if err != nil || res.Outcome != OutcomeLanded || res.MergeSHA != m1 || !slices.Equal(res.LandedHeads, []string{m1}) {
		t.Fatalf("pass 2 = %+v err=%v, want landed at %s", res, err, m1)
	}
	gf.RemoveFromOrigin("refs/heads/sprint/w")
	if res, err = PromotePass(ctx, gf, ff, promote_internal(m1)); err != nil || res.Outcome != OutcomeLanded || res.MergeSHA != m1 {
		t.Fatalf("a pass with the merge on main and the base deleted = %+v err=%v, want landed", res, err)
	}
}

// A base behind main, a merge no longer on base, a red gate and a deleted
// base whose merge is not on main are never landed: each is the
// coordinator's judgement (or a fault, which is one too).
func TestPromoteNeverLandsOffMain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gf, ff, root, m1 := promoteWorld_internal()
	ff.SetCheck("o/r", m1, forge.CheckRollup{State: forge.CheckRed, Failing: []string{"unit"}})
	if res, err := PromotePass(ctx, gf, ff, promote_internal(m1)); err != nil || res.Outcome != OutcomeRed {
		t.Fatalf("red = %+v err=%v, want ci_red", res, err)
	}
	gf.AddCommit(root, map[string]string{"b.go": "package b\n"}, "main")
	if res, err := PromotePass(ctx, gf, ff, promote_internal(m1)); err != nil || res.Outcome != OutcomeBehind {
		t.Fatalf("main moved = %+v err=%v, want base_behind", res, err)
	}
	gf.SetOriginRef("sprint/w", root)
	if res, err := PromotePass(ctx, gf, ff, promote_internal(m1)); err != nil || res.Outcome != OutcomeMissing {
		t.Fatalf("base reset = %+v err=%v, want missing_head", res, err)
	}
	gf.RemoveFromOrigin("refs/heads/sprint/w")
	if res, err := PromotePass(ctx, gf, ff, promote_internal(m1)); err == nil || res.Outcome == OutcomeLanded {
		t.Fatalf("base deleted, merge off main = %+v err=%v, want a fault", res, err)
	}
}
