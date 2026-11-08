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

// lockGit is a clone whose ls-remote runs onLsRemote once (where another
// process, check, takes the clone's lock), counting clones and fetches; a
// land never goes past the fetch.
type lockGit struct {
	git.Git
	onLsRemote      func()
	cloned, fetched int
}

func (g *lockGit) Clone(context.Context, string, string) error { g.cloned++; return nil }
func (g *lockGit) Fetch(context.Context, string, ...string) error {
	g.fetched++
	return nil
}
func (g *lockGit) IsAncestor(context.Context, string, string, string) (bool, error) {
	return false, errors.New("lockGit: past the fetch")
}
func (g *lockGit) LsRemote(context.Context, string, string) (string, bool, error) {
	if f := g.onLsRemote; f != nil {
		g.onLsRemote = nil
		f()
	}
	return strings.Repeat("a", 40), true, nil
}

// landForge is a forge LandPass never reaches here.
type landForge struct{ forge.Forge }

// TestCloneAndFetchTakeTheClonesLock: run's clone and LandPass's fetch hold
// the clone's lock (<clone>.lock beside it, the lock nova-sprint check takes
// in the same repos dir), so they never run git fetch in one repository at
// once with another process ("cannot lock ref"). With the lock held
// elsewhere, a land whose ctx ends does not fetch and faults, and a heads
// read does not clone. git is a fake (no process); the lock file is in a
// temp dir.
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
	ctx, cancel := context.WithCancel(context.Background())
	g := &lockGit{onLsRemote: func() {
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
			t.Error(err)
		}
		cancel()
	}}
	e := &Edges{Git: g, Forge: landForge{}, ReposDir: dir}
	s := store.Stream{Name: "s1", Repo: url, Base: "main", Forge: "github"}
	e.land(ctx, steps.EdgeRequest{ID: "land.s1.x", Kind: "land", Stream: "s1", Heads: []string{strings.Repeat("b", 40)}, Branches: []string{"card/x"}}, s)
	res := e.Results()
	if g.fetched != 0 || len(res) != 1 || res[0].Outcome != "fault" || !strings.Contains(res[0].Reason, context.Canceled.Error()) {
		t.Fatalf("a land fetched %d times with the clone locked elsewhere: %+v", g.fetched, res)
	}
	if h := e.readHead(ctx, s); !errors.Is(h.err, context.Canceled) || g.cloned != 1 {
		t.Fatalf("a heads read with the clone locked elsewhere: %v, %d clones", h.err, g.cloned)
	}
}

// pushGit records whether each push ran with the clone's lock held.
type pushGit struct {
	git.Git
	t      *testing.T
	locked []bool
}

func (g *pushGit) Push(_ context.Context, dir, _, _, _ string) error {
	g.locked = append(g.locked, cloneLocked(g.t, dir))
	return nil
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

// TestLandPushTakesTheClonesLock: git push updates refs/remotes/origin/<ref>
// in the clone, so LandPass's push holds the clone's lock as its fetch does.
func TestLandPushTakesTheClonesLock(t *testing.T) {
	t.Parallel()
	g := &pushGit{t: t}
	dir := filepath.Join(t.TempDir(), "clone")
	if err := (lockedClone{g}).Push(context.Background(), dir, strings.Repeat("a", 40), "refs/heads/main", ""); err != nil {
		t.Fatal(err)
	}
	if len(g.locked) != 1 || !g.locked[0] {
		t.Fatalf("pushes with the lock held: %v, want [true]", g.locked)
	}
}
