package git

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
)

func group_internal(c1, c2 string) LandRequest {
	return LandRequest{Repo: "o/r", Stream: "s1", Base: "v2", Heads: []string{c1, c2}, Branches: []string{"sprint/s1/c1", "sprint/s1/c2"}}
}

// C: a gate kept by the caller (LandRequest.Gate) is read as it is while
// its CI has no verdict and the base has not moved. A base that moved
// while the gate's CI is only queued lets it go (Glenn 2026-11-15): the
// same pass rebuilds the group on the new tip, pushes it, and cancels the
// replaced head's runs. Once a runner took it (a run in progress), it is
// read to its verdict on the base it was built on (live 2026-11-10).
func TestLandKeptGateIsRebuiltWhenTheBaseMoves(t *testing.T) {
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
	req.Gate = res.Kept()
	res, err = LandPass(ctx, gf, ff, req)
	if err != nil || res.Outcome != OutcomePending || res.GatedSHA != g1 || gf.OriginRef(GroupRef("s1")) != g1 || res.BaseTip != root || res.Kept() != req.Gate {
		t.Fatalf("pass 2 = %+v err=%v, want pending on the kept gate %s built on %s, nothing pushed", res, err, g1, root)
	}
	moved := gf.AddCommit(root, map[string]string{"m.go": "package m\n"}, "v2")
	ff.SetRuns(forge.Run{ID: 8, Branch: GroupBranch("s1"), HeadSHA: g1, Status: "queued"}, forge.Run{ID: 9, Branch: GroupBranch("s1"), HeadSHA: g1, Status: "in_progress"})
	if res, err = LandPass(ctx, gf, ff, req); err != nil || res.Outcome != OutcomePending || res.GatedSHA != g1 || res.BaseTip != root || len(ff.Cancelled()) != 0 {
		t.Fatalf("started = %+v err=%v, want the started gate %s read on %s, nothing cancelled", res, err, g1, root)
	}
	for name, set := range map[string]func(){
		"no run yet": func() { ff.SetRuns() },
		"a run finished": func() {
			ff.SetRuns(forge.Run{ID: 8, Branch: GroupBranch("s1"), HeadSHA: g1, Status: "queued"}, forge.Run{ID: 6, Branch: GroupBranch("s1"), HeadSHA: g1, Status: "completed"})
		},
		"runs cannot be read": func() {
			ff.SetRuns(forge.Run{ID: 9, Branch: GroupBranch("s1"), HeadSHA: g1, Status: "queued"})
			ff.world.runsErr = errors.New("HTTP 502")
		},
	} {
		set()
		if res, err = LandPass(ctx, gf, ff, req); err != nil || res.GatedSHA != g1 || res.BaseTip != root {
			t.Fatalf("%s = %+v err=%v, want the kept gate %s read on %s", name, res, err, g1, root)
		}
		ff.world.runsErr = nil
	}
	ff.SetRuns(forge.Run{ID: 9, Branch: GroupBranch("s1"), HeadSHA: g1, Status: "queued"})
	res, err = LandPass(ctx, gf, ff, req)
	if err != nil || res.GatedSHA == g1 || res.BaseTip != moved || gf.OriginRef(GroupRef("s1")) != res.GatedSHA || res.Kept().Base != moved {
		t.Fatalf("pass 3 = %+v err=%v, want the group rebuilt on %s and pushed", res, err, moved)
	}
	if ok, _ := gf.IsAncestor(ctx, "", moved, res.GatedSHA); !ok {
		t.Fatal("the rebuilt group does not contain the moved base")
	}
	if !slices.Equal(ff.Cancelled(), []int64{9}) {
		t.Fatalf("cancelled %v, want the replaced head's run 9", ff.Cancelled())
	}
}

// C: a kept rung-1 gate (the card's own branch, a hand landing's) whose CI
// is only queued on a base that moved is let go too: its head no longer
// contains the tip, so the pass gates the group built on the new tip.
func TestLandKeptRungOneGateLetGoWhenTheBaseMoves(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gf, ff, root, c1, _ := landWorld_internal()
	req := one_internal(c1)
	res, _ := LandPass(ctx, gf, ff, req)
	moved := gf.AddCommit(root, map[string]string{"m.go": "package m\n"}, "v2")
	req.Gate = res.Kept()
	ff.SetRuns(forge.Run{ID: 7, Branch: "sprint/s1/c1", HeadSHA: c1, Status: "queued"})
	res, err := LandPass(ctx, gf, ff, req)
	if err != nil || res.Rung != 2 || res.GatedSHA == c1 || res.BaseTip != moved || gf.OriginRef(GroupRef("s1")) != res.GatedSHA {
		t.Fatalf("pass = %+v err=%v, want the group gated on %s", res, err, moved)
	}
	if !slices.Equal(ff.Cancelled(), []int64{7}) {
		t.Fatalf("cancelled %v, want the replaced rung-1 gate's run 7", ff.Cancelled())
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
