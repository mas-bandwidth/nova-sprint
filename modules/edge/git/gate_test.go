package git

import (
	"context"
	"slices"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
)

func group_internal(c1, c2 string) LandRequest {
	return LandRequest{Repo: "o/r", Stream: "s1", Base: "v2", Heads: []string{c1, c2}, Branches: []string{"sprint/s1/c1", "sprint/s1/c2"}}
}

// C: a gate kept by the caller (LandRequest.Gate) is read as it is while
// its CI has no verdict: a base that moved meanwhile neither rebuilds nor
// pushes it. Green on a moved base lets it go (base_moved, nothing kept),
// and the next pass builds the group on the new tip.
func TestLandKeptGateIsNotRebuiltWhileCIRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gf, ff, root, c1, c2 := landWorld_internal()
	req := group_internal(c1, c2)
	res, err := LandPass(ctx, gf, ff, req)
	g1 := res.GatedSHA
	if err != nil || res.Outcome != OutcomeAbsent || g1 == "" || res.Kept().SHA != g1 || res.Kept().Base != root || res.Kept().Rung != 2 {
		t.Fatalf("pass 1 = %+v (kept %+v) err=%v, want the group gated and kept", res, res.Kept(), err)
	}
	ff.SetCheck("o/r", g1, forge.CheckRollup{State: forge.CheckPending, Pending: 4})
	moved := gf.AddCommit(root, map[string]string{"m.go": "package m\n"}, "v2")
	req.Gate = res.Kept()
	res, err = LandPass(ctx, gf, ff, req)
	if err != nil || res.Outcome != OutcomePending || res.GatedSHA != g1 || gf.OriginRef(GroupRef("s1")) != g1 || res.BaseTip != root || res.Kept() != req.Gate {
		t.Fatalf("pass 2 = %+v err=%v, want pending on the kept gate %s built on %s, nothing pushed", res, err, g1, root)
	}
	ff.SetCheck("o/r", g1, forge.CheckRollup{State: forge.CheckGreen})
	res, err = LandPass(ctx, gf, ff, req)
	if err != nil || res.Outcome != OutcomeBaseMoved || res.Kept() != (Gate{}) || gf.OriginRef("v2") != moved {
		t.Fatalf("pass 3 = %+v err=%v, want base_moved, the gate let go and nothing merged", res, err)
	}
	req.Gate = res.Kept()
	res, err = LandPass(ctx, gf, ff, req)
	if err != nil || res.GatedSHA == g1 || res.BaseTip != moved {
		t.Fatalf("pass 4 = %+v err=%v, want the group rebuilt on %s", res, err, moved)
	}
	if ok, _ := gf.IsAncestor(ctx, "", moved, res.GatedSHA); !ok {
		t.Fatal("the rebuilt group does not contain the moved base")
	}
}

// C: a kept rung-1 gate (the card's own branch) is read to its verdict on
// the base it was gated on, never moved to the group because base moved.
func TestLandKeptRungOneGateIsJudgedOnItsBase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gf, ff, root, c1, _ := landWorld_internal()
	req := one_internal(c1)
	res, _ := LandPass(ctx, gf, ff, req)
	gf.AddCommit(root, map[string]string{"m.go": "package m\n"}, "v2")
	ff.SetCheck("o/r", c1, forge.CheckRollup{State: forge.CheckRed, Failing: []string{"unit"}, URL: "https://github.com/o/r/actions/runs/1/job/2"})
	req.Gate = res.Kept()
	res, err := LandPass(ctx, gf, ff, req)
	if err != nil || res.Outcome != OutcomeRed || res.Rung != 1 || res.GatedSHA != c1 || res.BaseTip != root || gf.OriginRef(GroupRef("s1")) != "" {
		t.Fatalf("pass = %+v err=%v, want ci_red at rung 1 on %s, no group", res, err, root)
	}
}

// C: a kept gate is let go and rebuilt in the same pass when it can no
// longer be what lands: GitHub says its PR conflicts with a base that
// moved, or its group branch is no longer at it.
func TestLandKeptGateLetGoWhenItCannotLand(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gf, ff, root, c1, c2 := landWorld_internal()
	req := group_internal(c1, c2)
	res, _ := LandPass(ctx, gf, ff, req)
	req.Gate = res.Kept()
	gf.AddCommit(root, map[string]string{"b.go": "package bb\n"}, "v2") // conflicts with c2
	ff.Mergeable = "CONFLICTING"
	res, err := LandPass(ctx, gf, ff, req)
	if err != nil || res.Outcome != OutcomeConflict || res.ConflictHead != c2 || !slices.Equal(res.Conflicts, []string{"b.go"}) {
		t.Fatalf("conflicting = %+v err=%v, want the group rebuilt and its merge conflict on %s", res, err, c2)
	}

	gf, ff, root, c1, c2 = landWorld_internal()
	req = group_internal(c1, c2)
	res, _ = LandPass(ctx, gf, ff, req)
	req.Gate = res.Kept()
	gf.SetOriginRef(GroupRef("s1"), root) // the group branch left the gate
	moved := gf.AddCommit(root, map[string]string{"m.go": "package m\n"}, "v2")
	res, err = LandPass(ctx, gf, ff, req)
	if err != nil || res.GatedSHA == req.Gate.SHA || res.BaseTip != moved || gf.OriginRef(GroupRef("s1")) != res.GatedSHA {
		t.Fatalf("group branch moved = %+v err=%v, want the group rebuilt on %s and pushed", res, err, moved)
	}
}

// F (live 2026-11-11: 42 cards, one conflicting file): a group merge that
// conflicts names the head whose merge failed, so that card alone is the
// coordinator's.
func TestLandGroupConflictNamesTheHead(t *testing.T) {
	t.Parallel()
	gf, ff, root, c1, c2 := landWorld_internal()
	gf.AddCommit(root, map[string]string{"b.go": "package bb\n"}, "v2")
	res, err := LandPass(context.Background(), gf, ff, group_internal(c1, c2))
	if err != nil || res.Outcome != OutcomeConflict || res.ConflictHead != c2 || !slices.Equal(res.Conflicts, []string{"b.go"}) {
		t.Fatalf("pass = %+v err=%v, want a conflict naming %s and b.go", res, err, c2)
	}
}
