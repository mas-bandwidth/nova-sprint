package git

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
)

// Cold read, 2026-11-06, at 649d1b4c. One failing probe per confirmed defect.

const crRepo = "mas-bandwidth/nova-sprint"

func crFakeWorld_internal(t *testing.T) (*Fake, string, string, string) {
	t.Helper()
	gf := NewFake(nil)
	root := gf.AddCommit("", map[string]string{"README": "v2\n"}, "refs/heads/v2")
	h1 := gf.AddCommit(root, map[string]string{"a.txt": "a\n"}, "refs/heads/card/s1/c1")
	h2 := gf.AddCommit(root, map[string]string{"b.txt": "b\n"}, "refs/heads/card/s1/c2")
	return gf, root, h1, h2
}

// Guarantee 4 / LANDING step 6, and liveness of LandEdge.tla ("a conflict-free
// green group lands"). Each LandPass merges the group afresh (land.go:126,
// git.go:204-210) and asks CI once at the new merge sha (land.go:218); the
// forge answers pending until CI has run. The next pass makes another merge
// commit with another sha, so CI that went green at the first sha is never
// read: a multi-head group never lands. The fake makes a fresh sha per merge,
// as real git does once the committer second changes (see the functional probe).
func TestColdreadDefectLandGroupNeverLandsAcrossPasses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gf, _, h1, h2 := crFakeWorld_internal(t)
	ff := newFakeForge_internal()
	req := LandRequest{Repo: crRepo, Stream: "s1", Base: "v2", Heads: []string{h1, h2}}
	first, err := LandPass(ctx, gf, ff, req)
	if err != nil || first.Outcome != OutcomeCIPending {
		t.Fatalf("pass 1: want ci_pending, got %+v err=%v", first, err)
	}
	ff.SetCheck(crRepo, first.GatedSHA, forge.CheckRollup{State: forge.CheckGreen})
	second, err := LandPass(ctx, gf, ff, req)
	if err != nil {
		t.Fatal(err)
	}
	if second.Outcome != OutcomeLanded {
		t.Errorf("CI went green at %s, but pass 2 gated a new merge %s and got %s: the group can never land", first.GatedSHA, second.GatedSHA, second.Outcome)
	}
}

// The same fact on real git: two merges of the same heads onto the same base
// a second apart have different shas (no fixed committer date, git.go:106-113).
func TestFunctionalColdreadDefectRealGitMergeShaChangesPerPass(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	_, workDir, baseSHA, runGit := setupBareAndWorkRepo_internal(t)
	c1 := commitBranch_internal(t, runGit, workDir, "c1", baseSHA, "c1.txt", "1\n", "refs/heads/card/s1/c1")
	c2 := commitBranch_internal(t, runGit, workDir, "c2", baseSHA, "c2.txt", "2\n", "refs/heads/card/s1/c2")
	client := New(Config{})
	m1, err := client.MergeGroup(ctx, workDir, baseSHA, []string{c1, c2}, nil)
	if err != nil || m1.Kind != MergeOK {
		t.Fatalf("merge 1: %+v %v", m1, err)
	}
	time.Sleep(1100 * time.Millisecond) // the fact under test is the committer second
	m2, err := client.MergeGroup(ctx, workDir, baseSHA, []string{c1, c2}, nil)
	if err != nil || m2.Kind != MergeOK {
		t.Fatalf("merge 2: %+v %v", m2, err)
	}
	if m1.SHA != m2.SHA {
		t.Errorf("same group onto same base merged to %s then %s: CI evidence keyed by sha is lost between passes", m1.SHA, m2.SHA)
	}
}

// Guarantee 4: "Base advances only to a sha whose checks passed at that exact
// sha". With a nil forge, gateMergedGroup_internal returns as green without a
// check (land.go:215-216) and LandPass pushes the merge to base.
func TestColdreadDefectLandWithoutForgeIsUngated(t *testing.T) {
	t.Parallel()
	gf, _, h1, h2 := crFakeWorld_internal(t)
	res, err := LandPass(context.Background(), gf, nil, LandRequest{Repo: crRepo, Stream: "s1", Base: "v2", Heads: []string{h1, h2}})
	if err == nil && res.Outcome == OutcomeLanded {
		t.Errorf("landed %s on v2 with no forge and no CI result at that sha", res.GatedSHA)
	}
}

// Ruling: "merges only the exact sha CI passed". A group of one is landed on
// a passed tree (land.go:212-213): the merge commit itself never ran CI.
func TestColdreadDefectLandReusesTreeNotExactSha(t *testing.T) {
	t.Parallel()
	gf, root, h1, _ := crFakeWorld_internal(t)
	moved := gf.AddCommit(root, map[string]string{"m.txt": "m\n"}, "refs/heads/v2")
	twin := gf.AddCommit(moved, map[string]string{"a.txt": "a\n"}, "")
	_ = twin // the tree a twin shares; LandPass takes no tree match any more
	ff := newFakeForge_internal()
	res, err := LandPass(context.Background(), gf, ff, LandRequest{Repo: crRepo, Stream: "s1", Base: "v2", Heads: []string{h1}})
	if err == nil && res.Outcome == OutcomeLanded {
		t.Errorf("base advanced to merge %s on a tree match; no check ever ran at %s", res.GatedSHA, res.GatedSHA)
	}
}

// Design item 1: "argv only, never a shell string". ssh joins the remote
// command words with spaces and hands them to the remote login shell
// (land.go:413-417), so an argument is re-parsed by a shell on the landing host.
func TestColdreadDefectRemoteLandStepArgsReachRemoteShell(t *testing.T) {
	t.Parallel()
	fe := edge.NewFakeExec()
	fe.On("ssh", func(edge.FakeCall) edge.FakeOutcome { return edge.FakeOutcome{} })
	if _, err := RunRemoteLandStep(context.Background(), fe, []string{"vision"}, 10, time.Minute, "--reason", "a b; touch /tmp/x"); err != nil {
		t.Fatal(err)
	}
	argv := fe.Calls()[0].Cmd.Argv
	last := argv[len(argv)-1]
	if !strings.HasPrefix(last, "'") {
		t.Errorf("ssh remote words passed unquoted (%q): the landing host's shell runs `touch /tmp/x`", strings.Join(argv[len(argv)-3:], " "))
	}
}
