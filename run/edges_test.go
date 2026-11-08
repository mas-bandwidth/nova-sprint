package run

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// cloneGit records what run clones and ls-remotes.
type cloneGit struct {
	git.Git
	cloned []string
}

func (g *cloneGit) Clone(_ context.Context, repo, _ string) error {
	g.cloned = append(g.cloned, repo)
	return nil
}
func (g *cloneGit) LsRemote(context.Context, string, string) (string, bool, error) {
	return strings.Repeat("a", 40), true, nil
}

// TestHeadsCloneTheStreamsCloneAddress is defect 21 at run: a stream named
// <org>/<repo> with forge github is cloned from GitHub's https address; a
// clone address is cloned as it is; a repo a forge cannot address is the
// stream's heads failure, said once, with no head.
func TestHeadsCloneTheStreamsCloneAddress(t *testing.T) {
	t.Parallel()
	g := &cloneGit{}
	var said []string
	e := &Edges{Git: g, ReposDir: t.TempDir(), Fault: func(what string, err error) { said = append(said, what+": "+err.Error()) }}
	e.readHeads(context.Background(), map[string]store.Stream{
		"tools": {Name: "tools", Repo: "mas-bandwidth/nova-tools", Base: "main", Forge: "github"},
		"url":   {Name: "url", Repo: "https://example.invalid/r.git", Base: "main", Forge: "github"},
		"bad":   {Name: "bad", Repo: "nova-tools", Base: "main", Forge: "github"},
	})
	want := map[string]bool{"https://github.com/mas-bandwidth/nova-tools.git": true, "https://example.invalid/r.git": true}
	if len(g.cloned) != 2 || !want[g.cloned[0]] || !want[g.cloned[1]] {
		t.Fatalf("cloned %q", g.cloned)
	}
	heads := e.Heads()
	if len(heads) != 2 || heads["bad"] != "" {
		t.Fatalf("heads %v", heads)
	}
	if len(said) != 1 || !strings.Contains(said[0], "heads stream bad: clone: git refused") {
		t.Fatalf("said %q", said)
	}
}

// flakyGit answers ls-remote from a script: each call takes the next entry
// (a sha, "" with ok false for a branch not on origin, or an error), and
// the last entry answers every call after it.
type flakyGit struct {
	git.Git
	calls  int
	script []lsAnswer
}

type lsAnswer struct {
	sha string
	ok  bool
	err error
}

func (g *flakyGit) Clone(context.Context, string, string) error { return nil }
func (g *flakyGit) LsRemote(context.Context, string, string) (string, bool, error) {
	a := g.script[min(g.calls, len(g.script)-1)]
	g.calls++
	return a.sha, a.ok, a.err
}

// TestHeadsSharedBaseReadOnceAndKept is defect D6 (2026-11-07): nine
// streams on one repo and base were each read with their own ls-remote,
// and the one whose read timed out lost its head for the pass while the
// others kept theirs, so the deal opened an engine judgement about it. A
// shared repo and base is read once per pass and the answer is every
// stream's; a read that fails keeps the head as last read (SEAMS: "as the
// edge last read it"), said once per stream; only a base that is not on
// origin drops it.
func TestHeadsSharedBaseReadOnceAndKept(t *testing.T) {
	t.Parallel()
	sha, timeout := strings.Repeat("a", 40), errors.New("timed out after 1m0s")
	g := &flakyGit{script: []lsAnswer{{sha: sha, ok: true}, {err: timeout}, {sha: sha, ok: true}, {err: timeout}, {}}}
	var said []string
	e := &Edges{Git: g, ReposDir: t.TempDir(), Fault: func(what string, err error) { said = append(said, what+": "+err.Error()) }}
	streams := map[string]store.Stream{
		"schema-moo":    {Name: "schema-moo", Repo: "mas-bandwidth/schema", Base: "sprint/schema-100", Forge: "github"},
		"schema-strict": {Name: "schema-strict", Repo: "mas-bandwidth/schema", Base: "sprint/schema-100", Forge: "github"},
	}
	both := map[string]string{"schema-moo": sha, "schema-strict": sha}
	// pass 1: one read for the shared base, the head is both streams'
	e.readHeads(context.Background(), streams)
	if g.calls != 1 || !maps.Equal(e.Heads(), both) {
		t.Fatalf("pass 1: %d ls-remotes, heads %v", g.calls, e.Heads())
	}
	// pass 2: the read fails; both keep the head, the fault is said per stream
	e.readHeads(context.Background(), streams)
	if !maps.Equal(e.Heads(), both) {
		t.Fatalf("pass 2: heads %v after a failed read", e.Heads())
	}
	slices.Sort(said)
	if len(said) != 2 || !strings.Contains(said[0], "heads stream schema-moo: ls-remote sprint/schema-100: timed out") || !strings.Contains(said[1], "heads stream schema-strict: ls-remote") {
		t.Fatalf("pass 2 said %q", said)
	}
	// pass 3: read again; pass 4: fails again, said again (it stopped in between)
	e.readHeads(context.Background(), streams)
	e.readHeads(context.Background(), streams)
	if len(said) != 4 || !maps.Equal(e.Heads(), both) {
		t.Fatalf("pass 4: said %d, heads %v", len(said), e.Heads())
	}
	// pass 5: the base is not on origin: a definite answer, the heads go
	e.readHeads(context.Background(), streams)
	if len(e.Heads()) != 0 || len(said) != 6 || !strings.Contains(said[5], "is not on origin") {
		t.Fatalf("pass 5: heads %v, said %q", e.Heads(), said)
	}
}

// lockGit counts clones and fetches.
type lockGit struct {
	git.Git
	cloned, fetched int
}

func (g *lockGit) Clone(context.Context, string, string) error { g.cloned++; return nil }
func (g *lockGit) Fetch(context.Context, string, ...string) error {
	g.fetched++
	return nil
}

// landForge is a forge LandPass never reaches here.
type landForge struct{ forge.Forge }

// TestCloneAndFetchTakeTheClonesLock: run's clone and land take the clone's
// lock (<clone>.lock beside it, the lock nova-sprint check takes in the
// same repos dir), so they never run git in one repository at once with
// another process ("cannot lock ref"). With the lock held elsewhere and
// ctx ended, a land neither clones nor fetches and faults, and a heads read
// does not clone. git is a fake (no process); the lock file is in a temp
// dir.
func TestCloneAndFetchTakeTheClonesLock(t *testing.T) {
	t.Parallel()
	const url = "https://example.invalid/r.git"
	dir := t.TempDir()
	sum := sha256.Sum256([]byte(url))
	f, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("%x.lock", sum[:8])), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := &lockGit{}
	e := &Edges{Git: g, Forge: landForge{}, ReposDir: dir}
	s := store.Stream{Name: "s1", Repo: url, Base: "main", Forge: "github"}
	e.land(ctx, steps.EdgeRequest{ID: "land.s1.x", Kind: "land", Stream: "s1", Heads: []string{strings.Repeat("b", 40)}, Branches: []string{"card/x"}}, s)
	res := e.Results()
	if g.cloned != 0 || g.fetched != 0 || len(res) != 1 || res[0].Outcome != "fault" || !strings.Contains(res[0].Reason, context.Canceled.Error()) {
		t.Fatalf("a land with the clone locked elsewhere: %d clones, %d fetches, %+v", g.cloned, g.fetched, res)
	}
	if h := e.readHead(ctx, s); !errors.Is(h.err, context.Canceled) || g.cloned != 0 {
		t.Fatalf("a heads read with the clone locked elsewhere: %v, %d clones", h.err, g.cloned)
	}
}

// cloneLocked is whether <dir>.lock is flocked by someone else now.
func cloneLocked(t *testing.T, dir string) bool {
	f, err := os.OpenFile(dir+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil
}

// passGit takes a land pass to its group build and push, recording whether
// each step that writes the clone's work tree or refs ran with the clone's
// lock held.
type passGit struct {
	git.Git
	t              *testing.T
	head           string
	merged, pushed []bool
}

func (g *passGit) Clone(context.Context, string, string) error    { return nil }
func (g *passGit) Fetch(context.Context, string, ...string) error { return nil }
func (g *passGit) IsAncestor(context.Context, string, string, string) (bool, error) {
	return false, nil
}
func (g *passGit) LsRemote(context.Context, string, string) (string, bool, error) {
	return g.head, true, nil
}
func (g *passGit) MergeGroup(_ context.Context, dir, _ string, _ []string, _ []git.LedgerRule) (git.MergeResult, error) {
	g.merged = append(g.merged, cloneLocked(g.t, dir))
	return git.MergeResult{Kind: git.MergeOK, SHA: strings.Repeat("c", 40)}, nil
}
func (g *passGit) Push(_ context.Context, dir, _, _, _ string) error {
	g.pushed = append(g.pushed, cloneLocked(g.t, dir))
	return nil
}

// pendingForge answers a PR whose head is not yet the gated sha: pending.
type pendingForge struct{ forge.Forge }

func (pendingForge) PR(context.Context, string, string, string) (forge.PRInfo, bool, error) {
	return forge.PRInfo{Number: 1, HeadSHA: strings.Repeat("d", 40)}, true, nil
}

// cloneFree takes <dir>.lock with a blocking flock and lets it go, so it is
// true once nobody holds the lock. A child process forked meanwhile holds
// the fd only until its exec, and this waits that out (a non-blocking probe
// flaked beside the functional tests); a lock held across the call blocks
// it until the test binary's -timeout, which fails the test.
func cloneFree(t *testing.T, dir string) bool {
	f, err := os.OpenFile(dir+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	if cerr := f.Close(); cerr != nil {
		t.Error(cerr)
	}
	return err == nil
}

// TestLandPassHoldsTheClonesLock is defect 4: streams on one repository
// share one clone, and one land goroutine runs per stream, so a pass's
// build (fetch, MergeGroup's checkout and merge in the work tree, push)
// holds the clone's lock for the whole pass, and never across the Pause
// between passes (a CI wait). The inner fetch and push do not lock again
// (a second flock on another fd would wait forever).
func TestLandPassHoldsTheClonesLock(t *testing.T) {
	t.Parallel()
	g := &passGit{t: t, head: strings.Repeat("b", 40)}
	var freeAtPause []bool
	dir := t.TempDir()
	s := store.Stream{Name: "s1", Repo: "https://example.invalid/r.git", Base: "main", Forge: "github"}
	sum := sha256.Sum256([]byte(s.Repo))
	clone := filepath.Join(dir, fmt.Sprintf("%x", sum[:8]))
	e := &Edges{Git: g, Forge: pendingForge{}, ReposDir: dir, Pause: func(context.Context, time.Duration) error {
		freeAtPause = append(freeAtPause, cloneFree(t, clone))
		return context.Canceled
	}}
	e.land(context.Background(), steps.EdgeRequest{ID: "land.s1.x", Kind: "land", Stream: "s1", Heads: []string{g.head}, Branches: []string{"card/x"}}, s)
	if !slices.Equal(g.merged, []bool{true}) || !slices.Equal(g.pushed, []bool{true}) || !slices.Equal(freeAtPause, []bool{true}) {
		t.Fatalf("locked at merge %v, push %v; free at pause %v; want [true] [true] [true]", g.merged, g.pushed, freeAtPause)
	}
}

// TestHeadsReadSkipsTheLockOnAClone: a clone that is there (its .git
// exists) is read without the clone's lock, so a heads read never waits
// behind a landing on the same repository; only a clone to make takes it.
func TestHeadsReadSkipsTheLockOnAClone(t *testing.T) {
	t.Parallel()
	const url = "https://example.invalid/r.git"
	dir := t.TempDir()
	sum := sha256.Sum256([]byte(url))
	clone := filepath.Join(dir, fmt.Sprintf("%x", sum[:8]))
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(clone+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := &Edges{Git: &cloneGit{}, ReposDir: dir}
	if h := e.readHead(ctx, store.Stream{Name: "s1", Repo: url, Base: "main", Forge: "github"}); h.err != nil || h.sha != strings.Repeat("a", 40) {
		t.Fatalf("a heads read of a clone that is there, locked elsewhere: %q, %v", h.sha, h.err)
	}
}

// TestLandPassWaitsForTheClonesLock: a pass on a clone that is there, with
// the clone's lock held elsewhere (nova-sprint check fetching), waits for
// it; a ctx that ends while it waits faults the landing without a fetch.
func TestLandPassWaitsForTheClonesLock(t *testing.T) {
	t.Parallel()
	const url = "https://example.invalid/r.git"
	dir := t.TempDir()
	sum := sha256.Sum256([]byte(url))
	clone := filepath.Join(dir, fmt.Sprintf("%x", sum[:8]))
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(clone+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := &lockGit{}
	e := &Edges{Git: g, Forge: landForge{}, ReposDir: dir}
	e.land(ctx, steps.EdgeRequest{ID: "land.s1.x", Kind: "land", Stream: "s1", Heads: []string{strings.Repeat("b", 40)}, Branches: []string{"card/x"}}, store.Stream{Name: "s1", Repo: url, Base: "main", Forge: "github"})
	res := e.Results()
	if g.fetched != 0 || len(res) != 1 || res[0].Outcome != "fault" || !strings.Contains(res[0].Reason, context.Canceled.Error()) {
		t.Fatalf("a pass with the clone locked elsewhere: %d fetches, %+v", g.fetched, res)
	}
}
