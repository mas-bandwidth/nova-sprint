package run

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// moveGit is a stream's origin whose base moves on every pass (each
// LsRemote of the base is a new tip), with a group branch it records the
// pushes to; card branches are at their heads and contain no base tip.
type moveGit struct {
	onGit
	base   int
	group  string
	pushes []string
}

func (g *moveGit) LsRemote(_ context.Context, _, ref string) (string, bool, error) {
	switch {
	case ref == "refs/heads/main":
		g.base++
		return fmt.Sprintf("%040x", g.base), true, nil
	case ref == git.GroupRef("s1"):
		return g.group, g.group != "", nil
	case strings.HasSuffix(ref, "/y"):
		return strings.Repeat("d", 40), true, nil
	}
	return strings.Repeat("a", 40), true, nil
}

func (g *moveGit) IsAncestor(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func (g *moveGit) MergeGroup(_ context.Context, _, onto string, _ []string) (git.MergeResult, error) {
	return git.MergeResult{Kind: git.MergeOK, SHA: "c" + onto[1:]}, nil
}

func (g *moveGit) Push(_ context.Context, _, sha, _, _ string) error {
	g.group = sha
	g.pushes = append(g.pushes, sha)
	return nil
}

// slowForge is a forge whose CI at the group's tip runs for passes polls,
// then is red; its PR's head is the group branch's tip.
type slowForge struct {
	forge.Forge
	g      *moveGit
	passes int
}

func (f *slowForge) PR(context.Context, string, string, string) (forge.PRInfo, bool, error) {
	return forge.PRInfo{Number: 1, HeadSHA: f.g.group, Mergeable: "MERGEABLE"}, true, nil
}

func (f *slowForge) Checks(_ context.Context, _ string, shas []string) (map[string]forge.CheckRollup, error) {
	out := map[string]forge.CheckRollup{}
	for _, s := range shas {
		out[s] = forge.CheckRollup{SHA: s, State: forge.CheckGreen}
		if s == f.g.group {
			if f.passes--; f.passes >= 0 {
				out[s] = forge.CheckRollup{SHA: s, State: forge.CheckPending, Pending: 3}
			} else {
				out[s] = forge.CheckRollup{SHA: s, State: forge.CheckRed, Failing: []string{"unit"}, URL: "https://github.com/o/r/actions/runs/1/job/2"}
			}
		}
	}
	return out, nil
}

func (f *slowForge) Failures(context.Context, string, string) ([]forge.Failure, error) {
	return []forge.Failure{{Name: "unit", URL: "https://github.com/o/r/actions/runs/1/job/2"}}, nil
}

func (f *slowForge) FailedLines(context.Context, string, string) ([]string, error) {
	return []string{"FAIL"}, nil
}

// C: a group whose CI is still running is never rebuilt or pushed again,
// however often the base moves under it (live 2026-11-10: every pass
// rebuilt the group on the new base tip and its CI never finished): the
// one gate is read to its verdict, judged on the base it was built on.
func TestLandKeepsTheGroupWhileItsCIRuns(t *testing.T) {
	t.Parallel()
	g := &moveGit{}
	f := &slowForge{g: g, passes: 5}
	e := &Edges{Git: g, Forge: f, ReposDir: t.TempDir(), Pause: func(context.Context, time.Duration) error { return nil }}
	e.land(context.Background(), steps.EdgeRequest{ID: "land.s1.x", Kind: "land", Stream: "s1",
		Heads: []string{strings.Repeat("a", 40), strings.Repeat("d", 40)}, Branches: []string{"sprint/s1/x", "sprint/s1/y"}},
		store.Stream{Name: "s1", Repo: "o/r", Base: "main", Forge: "github"})
	got := e.Results()
	if len(g.pushes) != 1 {
		t.Fatalf("the group was pushed %d times (%v) while its CI ran, want once", len(g.pushes), g.pushes)
	}
	if len(got) != 1 || got[0].Outcome != "ci_red" || !strings.Contains(got[0].Reason, "base tip "+fmt.Sprintf("%040x", 1)) {
		t.Fatalf("results %+v, want ci_red on the gate built on the first base tip", got)
	}
}

// switchGit is a stream's origin with two still bases, main at tip 1 and
// sprint-s1 at tip 0x100, and moveGit's group branch.
type switchGit struct{ moveGit }

func (g *switchGit) LsRemote(ctx context.Context, dir, ref string) (string, bool, error) {
	switch ref {
	case "refs/heads/main":
		return fmt.Sprintf("%040x", 1), true, nil
	case "refs/heads/sprint-s1":
		return fmt.Sprintf("%040x", 0x100), true, nil
	}
	return g.moveGit.LsRemote(ctx, dir, ref)
}

// stream set --base while a group is in flight: the landing reads the
// stream's base before each pass, lets go of the gate built on the old base
// and builds the group on the new one; nothing built on the old base lands
// or is judged.
func TestLandLetsGoOfTheGateWhenTheStreamsBaseChanges(t *testing.T) {
	t.Parallel()
	g := &switchGit{}
	f := &slowForge{g: &g.moveGit, passes: 4}
	reads := 0
	e := &Edges{Git: g, Forge: f, ReposDir: t.TempDir(), Pause: func(context.Context, time.Duration) error { return nil },
		Streams: func() map[string]store.Stream {
			base := "main"
			if reads++; reads > 2 {
				base = "sprint-s1"
			}
			return map[string]store.Stream{"s1": {Name: "s1", Repo: "o/r", Base: base, Forge: "github"}}
		}}
	e.land(context.Background(), steps.EdgeRequest{ID: "land.s1.x", Kind: "land", Stream: "s1",
		Heads: []string{strings.Repeat("a", 40), strings.Repeat("d", 40)}, Branches: []string{"sprint/s1/x", "sprint/s1/y"}},
		store.Stream{Name: "s1", Repo: "o/r", Base: "main", Forge: "github"})
	got := e.Results()
	onNew := "c" + fmt.Sprintf("%040x", 0x100)[1:]
	if len(g.pushes) != 2 || g.pushes[1] != onNew {
		t.Fatalf("pushes %v, want the group built on main, then once on sprint-s1 after the switch", g.pushes)
	}
	if len(got) != 1 || got[0].Outcome != "ci_red" || !strings.Contains(got[0].Reason, "base tip "+fmt.Sprintf("%040x", 0x100)) {
		t.Fatalf("results %+v, want the verdict on the gate built on sprint-s1", got)
	}
}
