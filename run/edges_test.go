package run

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// fakeLandForge is GitHub as the landing sees it: PRs by head branch, checks
// by sha, and the merges asked of it. No network.
type fakeLandForge struct {
	prs      map[string]*forge.PRState // head branch -> PR
	checks   map[string]forge.CheckRollup
	behind   int
	findErr  error
	mergeErr error
	ff       []string // shas fast-forwarded (Merge)
	merged   []string // MergePR methods' first choice, per merge
	opened   int
}

func newFakeLandForge() *fakeLandForge {
	return &fakeLandForge{prs: map[string]*forge.PRState{}, checks: map[string]forge.CheckRollup{}}
}

func (f *fakeLandForge) PR(_ context.Context, _, head, _ string) (forge.PRInfo, bool, error) {
	if p, ok := f.prs[head]; ok && p.State == "OPEN" {
		return forge.PRInfo{Number: p.Number, HeadSHA: p.HeadSHA}, true, nil
	}
	return forge.PRInfo{}, false, nil
}
func (f *fakeLandForge) OpenPR(_ context.Context, _, head, base, _, _ string) (forge.PRInfo, error) {
	f.opened++
	f.prs[head] = &forge.PRState{Number: 7, URL: "https://github.com/o/r/pull/7", State: "OPEN", HeadSHA: f.tip(head)}
	return forge.PRInfo{Number: 7}, nil
}
func (f *fakeLandForge) tip(head string) string {
	if strings.HasPrefix(head, "sprint/s1/group") {
		return sha40("g")
	}
	return sha40("a")
}
func (f *fakeLandForge) Checks(_ context.Context, _ string, shas []string) (map[string]forge.CheckRollup, error) {
	out := map[string]forge.CheckRollup{}
	for _, s := range shas {
		out[s] = f.checks[s]
	}
	return out, nil
}
func (f *fakeLandForge) Merge(_ context.Context, _ string, _ int, sha, _ string) error {
	if f.mergeErr != nil {
		return f.mergeErr
	}
	f.ff = append(f.ff, sha)
	return nil
}
func (f *fakeLandForge) FindPR(_ context.Context, _, head, _ string) (forge.PRState, bool, error) {
	if f.findErr != nil {
		return forge.PRState{}, false, f.findErr
	}
	p, ok := f.prs[head]
	if !ok {
		return forge.PRState{}, false, nil
	}
	return *p, true, nil
}
func (f *fakeLandForge) MergePR(_ context.Context, _ string, _ int, _ string, methods []string) (string, error) {
	if f.mergeErr != nil {
		return "", f.mergeErr
	}
	f.merged = append(f.merged, methods[0])
	return sha40("m"), nil
}
func (f *fakeLandForge) Behind(context.Context, string, string, string) (int, error) {
	return f.behind, nil
}
func (f *fakeLandForge) FailingLines(_ context.Context, _, url string, _ int) []string {
	if url == "" {
		return nil
	}
	return []string{"--- FAIL: TestBudgets (0.00s)", "boundary_test.go:343: bus is 3030 non-test .go lines, budget 3025"}
}

func sha40(c string) string { return strings.Repeat(c, 40) }

// landEdges is Edges on the fakes with a fake clock: each Pause moves the
// clock by its duration and counts.
func landEdges(f LandForge, g Git) (*Edges, *int) {
	now := time.Unix(1_000_000, 0)
	pauses := 0
	e := &Edges{Forge: f, Git: g, Now: func() time.Time { return now },
		Pause: func(_ context.Context, d time.Duration) error { pauses++; now = now.Add(d); return nil }}
	return e, &pauses
}

var s1 = store.Stream{Name: "s1", Repo: "https://github.com/o/r", Base: "sprint/s1-v1", Mode: "auto", Forge: "github"}

func card1() steps.EdgeRequest {
	return steps.EdgeRequest{ID: "land.s1.x", Kind: "land", Stream: "s1", Cards: []string{"p1"}, Heads: []string{sha40("a")}, Branch: "sprint/s1/p1", Base: "b"}
}

func one(t *testing.T, e *Edges) steps.InboxItem {
	t.Helper()
	got := e.Results()
	if len(got) != 1 {
		t.Fatalf("want one result, got %+v", got)
	}
	if got[0].Request != "land.s1.x" || got[0].Kind != "land" {
		t.Fatalf("result %+v", got[0])
	}
	return got[0]
}

// Rung 1: no PR yet, the landing opens it; green and up to date with the
// base, the base fast-forwards to the exact sha the checks passed.
func TestLandRung1OpensPRAndFastForwardsGreen(t *testing.T) {
	t.Parallel()
	f := newFakeLandForge()
	f.checks[sha40("a")] = forge.CheckRollup{State: forge.CheckGreen, Runs: 3}
	e, _ := landEdges(f, nil)
	e.land(context.Background(), card1(), s1)
	it := one(t, e)
	if it.Outcome != "landed" || !slices.Equal(it.Landed, []string{sha40("a")}) || f.opened != 1 || !slices.Equal(f.ff, []string{sha40("a")}) {
		t.Fatalf("got %+v opened=%d ff=%v", it, f.opened, f.ff)
	}
}

// Rung 1 behind the base: merged by the repository's method, merge commit
// first; the merge sha is in the facts.
func TestLandRung1BehindMergesByMethod(t *testing.T) {
	t.Parallel()
	f := newFakeLandForge()
	f.behind = 2
	f.checks[sha40("a")] = forge.CheckRollup{State: forge.CheckGreen, Runs: 3}
	e, _ := landEdges(f, nil)
	e.land(context.Background(), card1(), s1)
	it := one(t, e)
	if it.Outcome != "landed" || len(f.ff) != 0 || !slices.Equal(f.merged, []string{"--merge"}) || !strings.Contains(it.Facts, sha40("m")) {
		t.Fatalf("got %+v ff=%v merged=%v", it, f.ff, f.merged)
	}
}

// A PR already merged at the head (a restart after the merge) is landed.
func TestLandRung1AlreadyMergedIsLanded(t *testing.T) {
	t.Parallel()
	f := newFakeLandForge()
	f.prs["sprint/s1/p1"] = &forge.PRState{Number: 7, State: "MERGED", HeadSHA: sha40("a"), Merge: sha40("m")}
	e, _ := landEdges(f, nil)
	e.land(context.Background(), card1(), s1)
	if it := one(t, e); it.Outcome != "landed" || f.opened != 0 {
		t.Fatalf("got %+v", it)
	}
}

// Red is an escalate carrying the failing checks, their first failing
// lines and the command for the rest.
func TestLandRung1RedEscalatesWithLines(t *testing.T) {
	t.Parallel()
	f := newFakeLandForge()
	f.checks[sha40("a")] = forge.CheckRollup{State: forge.CheckRed, Runs: 7, Failing: []string{"unit (linux, x64, bench)", "ci-ok"},
		URL: "https://github.com/o/r/actions/runs/42347621761/job/128328774832"}
	e, _ := landEdges(f, nil)
	e.land(context.Background(), card1(), s1)
	it := one(t, e)
	for _, want := range []string{"unit (linux, x64, bench), ci-ok", "--- FAIL: TestBudgets", "gh run view --job 128328774832 -R o/r --log-failed"} {
		if it.Outcome != "escalate" || !strings.Contains(it.Facts, want) {
			t.Fatalf("want escalate with %q, got %+v", want, it)
		}
	}
	if len(f.ff)+len(f.merged) != 0 {
		t.Fatalf("a red PR merged: ff=%v merged=%v", f.ff, f.merged)
	}
}

// No check run ever starts (the workflow has no trigger for this PR): after
// NoChecksAfter the landing escalates naming the workflow fix; it does not
// wait forever.
func TestLandRung1NoChecksEscalatesWithWorkflowFix(t *testing.T) {
	t.Parallel()
	f := newFakeLandForge()
	e, pauses := landEdges(f, nil)
	e.land(context.Background(), card1(), s1)
	it := one(t, e)
	if it.Outcome != "escalate" || !strings.Contains(it.Facts, "no check run on "+sha40("a")) || !strings.Contains(it.Facts, "pull_request: {branches: ['sprint/s1-v1']}") {
		t.Fatalf("got %+v", it)
	}
	if want := int(NoChecksAfter/LandPoll) + 1; *pauses > want {
		t.Fatalf("waited %d polls, the bound is %d", *pauses, want)
	}
}

// Checks that run but never finish are bounded by PendingMax.
func TestLandRung1PendingIsBounded(t *testing.T) {
	t.Parallel()
	f := newFakeLandForge()
	f.checks[sha40("a")] = forge.CheckRollup{State: forge.CheckPending, Runs: 2}
	e, _ := landEdges(f, nil)
	e.land(context.Background(), card1(), s1)
	if it := one(t, e); it.Outcome != "escalate" || !strings.Contains(it.Facts, "still pending") {
		t.Fatalf("got %+v", it)
	}
}

// A refused merge is an escalate with the refusal verbatim.
func TestLandRung1MergeRefusedEscalatesVerbatim(t *testing.T) {
	t.Parallel()
	f := newFakeLandForge()
	f.checks[sha40("a")] = forge.CheckRollup{State: forge.CheckGreen, Runs: 1}
	f.mergeErr = errors.New("GraphQL: Required status check \"ci-ok\" is expected.")
	e, _ := landEdges(f, nil)
	e.land(context.Background(), card1(), s1)
	if it := one(t, e); it.Outcome != "escalate" || !strings.Contains(it.Facts, `Required status check "ci-ok" is expected.`) {
		t.Fatalf("got %+v", it)
	}
}

// gh failing pass after pass (no credential, the network) escalates after
// LandTries with the error; it never loops silently (the night's defect).
func TestLandFailingPassesEscalate(t *testing.T) {
	t.Parallel()
	f := newFakeLandForge()
	f.findErr = errors.New("forge refused: neither GH_TOKEN nor GH_CONFIG_DIR is set for identity sprint")
	e, pauses := landEdges(f, nil)
	e.land(context.Background(), card1(), s1)
	if it := one(t, e); it.Outcome != "escalate" || !strings.Contains(it.Facts, "neither GH_TOKEN") || *pauses != LandTries-1 {
		t.Fatalf("got %+v after %d pauses", it, *pauses)
	}
}

// A hand stream escalates at once: the coordinator lands it.
func TestLandHandStreamEscalates(t *testing.T) {
	t.Parallel()
	e, _ := landEdges(newFakeLandForge(), nil)
	hand := s1
	hand.Mode = "hand"
	e.land(context.Background(), card1(), hand)
	if it := one(t, e); it.Outcome != "escalate" || !strings.Contains(it.Facts, "lands by hand") {
		t.Fatalf("got %+v", it)
	}
}

// fakeGroupGit is the git edge for one rung-2 pass: the heads are on
// origin, none is in the base, the merge is clean, pushes succeed.
type fakeGroupGit struct{ pushed []string }

func (g *fakeGroupGit) Clone(context.Context, string, string) error    { return nil }
func (g *fakeGroupGit) Fetch(context.Context, string, ...string) error { return nil }
func (g *fakeGroupGit) LsRemote(_ context.Context, _, ref string) (string, bool, error) {
	if ref == git.StreamRef("s1") {
		return "", false, nil
	}
	switch ref { // the card branches' tips are the heads the request names
	case "refs/heads/sprint/s1/p1":
		return sha40("a"), true, nil
	case "refs/heads/sprint/s1/p2":
		return sha40("c"), true, nil
	}
	return sha40("b"), true, nil
}
func (g *fakeGroupGit) IsAncestor(context.Context, string, string, string) (bool, error) {
	return false, nil
}
func (g *fakeGroupGit) TreeHash(context.Context, string, string) (string, error) { return "", nil }
func (g *fakeGroupGit) MergeGroup(context.Context, string, string, []string, []git.LedgerRule) (git.MergeResult, error) {
	return git.MergeResult{Kind: git.MergeOK, SHA: sha40("g")}, nil
}
func (g *fakeGroupGit) Rebase(context.Context, string, string) (git.RebaseResult, error) {
	return git.RebaseResult{}, nil
}
func (g *fakeGroupGit) Push(_ context.Context, _, sha, ref, _ string) error {
	g.pushed = append(g.pushed, sha+":"+ref)
	return nil
}

// Rung 2: a red group is one escalate with the failing checks and no head
// blamed; nothing reaches the base.
func TestLandRung2RedGroupEscalatesNoBlame(t *testing.T) {
	t.Parallel()
	f := newFakeLandForge()
	f.checks[sha40("g")] = forge.CheckRollup{State: forge.CheckRed, Runs: 2, Failing: []string{"functional"}}
	g := &fakeGroupGit{}
	e, _ := landEdges(f, g)
	e.ReposDir = t.TempDir()
	r := card1()
	r.Cards, r.Heads, r.Branch = []string{"p1", "p2"}, []string{sha40("a"), sha40("c")}, ""
	e.land(context.Background(), r, s1)
	it := one(t, e)
	if it.Outcome != "escalate" || it.Blamed != "" || !strings.Contains(it.Facts, "group sprint/s1/group") || !strings.Contains(it.Facts, "functional") {
		t.Fatalf("got %+v", it)
	}
	if !slices.Equal(g.pushed, []string{sha40("g") + ":" + git.StreamRef("s1")}) {
		t.Fatalf("pushed %v: only the group branch", g.pushed)
	}
}
