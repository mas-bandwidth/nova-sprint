package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// secondRepo makes a second bare repository beside the rig's and a worker clone of it, so a
// pass holds two clones of two repositories at once (landpass.go, the lane per clone).
func (r *landRig) secondRepo(t *testing.T) (remote, worker string) {
	t.Helper()
	remote, worker = filepath.Join(r.dir, "remote2.git"), filepath.Join(r.dir, "worker2")
	r.git("", "init", "-q", "--bare", "-b", "main", remote)
	r.git("", "clone", "-q", remote, worker)
	require.NoError(t, os.WriteFile(filepath.Join(worker, "README"), []byte("base\n"), 0o600))
	r.git(worker, "add", "README")
	r.git(worker, "commit", "-q", "-m", "base")
	r.git(worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(worker, "fetch", "-q", "origin")
	return remote, worker
}

// cardIn is a card's work on its own branch from a worker's origin/main: its sha (card for
// another repository's worker).
func (r *landRig) cardIn(t *testing.T, worker, id string, files map[string]string) string {
	t.Helper()
	r.git(worker, "switch", "-q", "--no-track", "-c", "sprint/"+id, "refs/remotes/origin/main")
	for f, text := range files {
		p := filepath.Join(worker, f)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(text), 0o600))
		r.git(worker, "add", "--", f)
	}
	r.git(worker, "commit", "-q", "-m", "work of "+id)
	return r.git(worker, "rev-parse", "HEAD")
}

// briefFile writes a card brief naming repo and base, as the fleet's do.
func (r *landRig) briefFile(t *testing.T, dir, id, repo, base string) string {
	t.Helper()
	path := filepath.Join(dir, id+".md")
	require.NoError(t, os.WriteFile(path, []byte(passingBrief("REPO: "+repo+"\nBASE: "+base+"\n\nWrite the files of "+id+".")), 0o600))
	return path
}

// (b) Two streams on one base land in one lane, still in priority order, and the second
// merges onto the first's tip (the order on one base is the design and stays).
func TestLandLaneOnOneBaseKeepsPriorityOrder(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.stubGreenGate()
	twoStreams(t, r,
		map[string]map[string]string{"a1": {"a1.go": "package main\n\nfunc a1() {}\n"}},
		map[string]map[string]string{"b1": {"b1.go": "package main\n\nfunc b1() {}\n"}})
	out := r.ok("land --land-parallel 2")
	assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=main")
	assert.Contains(t, out, "LAND OK stream=s2 cards=1 base=main")
	assert.Less(t, strings.Index(out, "LAND OK stream=s1"), strings.Index(out, "LAND OK stream=s2"), "priority order on one base")
	assert.Equal(t, []string{"land b1 (sprint stream s2)", "land a1 (sprint stream s1)", "the module", "base"}, r.mainLog(), "the second merges onto the first's tip")
	r.clean()
}

// (a) A combined gate of one (clone, base) lane blocks; a batch of another repo's lane
// still pushes and lands. Before the lanes the one serial landing loop held the second
// repo's push behind the first's gate, and this test would time out at its barrier.
func TestALaneLandsWhileAnotherLanesCombinedGateBlocks(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.stubGreenGate()
	remote2, worker2 := r.secondRepo(t)
	// a base shared.go both cards of the first repo change, in different regions, so the
	// re-merge is clean and only the files meet (the combined tree is gated)
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.files("the module", goModule)
	r.files("base shared.go", map[string]string{"shared.go": "package main\n\nfunc main() { work() }\n\nfunc work() {}\n\nfunc tail() {}\n"})
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	briefs := t.TempDir()
	// s0 and s1 are on one repo and base: s0 lands first, moving the tip, so s1 is merged
	// again and its combined tree gated once. s2 is on another repository.
	r.promotionStream("s0")
	r.promotionStream("s1")
	r.promotionStream("s2")
	r.ok("add --stream s0 --one --brief-file " + r.briefFile(t, briefs, "a0", r.remote, "main"))
	r.ok("add --stream s1 --one --brief-file " + r.briefFile(t, briefs, "a1", r.remote, "main"))
	r.ok("add --stream s2 --one --brief-file " + r.briefFile(t, briefs, "b1", remote2, "main"))
	heads := map[string]string{}
	heads["a0"] = r.card("a0", map[string]string{"shared.go": "package main\n\nfunc main() { work() }\n\nfunc work() { zero() }\n\nfunc zero() {}\n\nfunc tail() {}\n"})
	heads["a1"] = r.card("a1", map[string]string{"shared.go": "package main\n\nfunc main() { work() }\n\nfunc work() {}\n\nfunc tail() { one() }\n\nfunc one() {}\n"})
	heads["b1"] = r.cardIn(t, worker2, "b1", map[string]string{"b1.go": "package main\n\nfunc b1() {}\n"})
	r.git(worker2, "push", "-q", "origin", "refs/heads/sprint/*:refs/heads/sprint/*")
	r.queued(heads, "a0", "a1", "b1")
	// The order is made deterministic with the test seams, whatever the lanes' speed, so
	// s1 always cuts on the pre-push base and re-merges onto s0's tip (its combined gate):
	//   s1Cut closes when s1's cut reaches its base gate, after the fetch and cutBranch read
	//     the base before s0's push; s0's push waits for it, so s1's baseSha is the
	//     pre-push base (the reader of attempt 5 found this absent);
	//   s2's phase 1 waits for s0's push, so the first push is s0's, then s2's;
	//   s2's push waits for s1's combined gate to start, so the gate is held while the other
	//     repo's lane pushes, and its push releases it.
	var gates atomic.Int32
	var released atomic.Bool
	barrier := make(chan struct{})
	var once sync.Once
	var pushes atomic.Int32
	s1Cut := make(chan struct{})
	var cutOnce sync.Once
	s0Pushed := make(chan struct{})
	var s0Once sync.Once
	started := make(chan struct{})
	var startOnce sync.Once
	r.a.beforeWait = func(stream, what string) {
		switch {
		case stream == "s1" && what == "gate":
			cutOnce.Do(func() { close(s1Cut) })
		case stream == "s2" && what == "slot":
			<-s0Pushed
		}
	}
	// the second gate of s1's worktree is the combined gate: it blocks until the other
	// repo's push has happened, or two seconds pass
	r.a.gateRan = func(dir string, _ bool) {
		if !strings.HasSuffix(dir, "@s1") {
			return
		}
		if gates.Add(1) != 2 {
			return
		}
		startOnce.Do(func() { close(started) })
		select {
		case <-barrier:
			released.Store(true)
		case <-time.After(2 * time.Second):
		}
	}
	r.a.beforePush = func(int) {
		switch pushes.Add(1) {
		case 1: // s0's push: hold it until s1 has cut on the pre-push base
			<-s1Cut
			s0Once.Do(func() { close(s0Pushed) })
		case 2: // s2's push: hold it until s1's combined gate blocks, then release it
			<-started
			once.Do(func() { close(barrier) })
		}
	}
	out := r.ok("land --land-parallel 2")
	assert.True(t, released.Load(), "another repo's lane pushed while the combined gate blocked")
	assert.Contains(t, out, "LAND OK stream=s0 cards=1 base=main")
	assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=main")
	assert.Contains(t, out, "LAND OK stream=s2 cards=1 base=main")
	r.clean()
}

// (c) Two fetches of one clone never overlap; two fetches of two clones run side by side.
// fetched takes the clone's own lock (landpass.go, fetchLock), so one clone stays serial
// and two clones' go at once.
func TestFetchesOfOneCloneSerializeAndTwoClonesGoSideBySide(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	l := &lander{a: r.a}
	// the pass makes the shared locks once before its lanes start (land.go); do the same
	// here, before the goroutines, so locks() is not raced on l.shared
	l.locks()
	// two clones have two locks: while one fetch holds its clone's lock, the other clone's
	// fetch takes its own and runs beside it
	a, b := l.locks().fetchLock("clone-a"), l.locks().fetchLock("clone-b")
	a.Lock()
	got := make(chan struct{})
	go func() {
		b.Lock()
		close(got)
	}()
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("two clones' fetches did not run side by side")
	}
	b.Unlock()
	a.Unlock()

	// one clone hands out one lock: a second fetch waits for the first and no two run at once
	c1, c2 := l.locks().fetchLock("clone-c"), l.locks().fetchLock("clone-c")
	require.Same(t, c1, c2, "one clone has one fetch lock")
	c1.Lock()
	held := make(chan struct{})
	go func() {
		c2.Lock()
		close(held)
	}()
	select {
	case <-held:
		t.Fatal("a second fetch of one clone entered while the first held the lock")
	case <-time.After(200 * time.Millisecond):
	}
	c1.Unlock()
	select {
	case <-held:
	case <-time.After(time.Second):
		t.Fatal("the second fetch of one clone never got the lock")
	}
	c2.Unlock()
}

// (c) The fetch lock is keyed on the clone, not on the directory a caller passes: a fetch
// run in a stream's worktree takes the same lock as the clone's own worktree writes, so two
// bases of one clone never fetch side by side (landpass.go, fetchKey and fetched). The test
// drives fetched from a real worktree; calling fetchLock with two spellings cannot catch a
// wrong call-site key, which is the regression attempt 4 had.
func TestAWorktreeFetchTakesItsClonesLock(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	l := &lander{a: r.a}
	l.locks()
	ctx := context.Background()
	// a worktree of the rig's clone, as a batch builds in: the lander cuts and fetches there
	wt := filepath.Join(r.dir, "wt-s9")
	r.git(r.clone, "worktree", "add", "--detach", wt, "HEAD")

	// the clone and its worktree resolve to one key: the clone's common git dir
	require.Equal(t, l.fetchKey(ctx, r.clone), l.fetchKey(ctx, wt), "a worktree and its clone share one fetch key")

	// holding that key blocks a fetched run from the worktree: the call site keys on the
	// clone, not on the worktree directory it is handed
	mu := l.locks().fetchLock(l.fetchKey(ctx, wt))
	mu.Lock()
	got := make(chan struct{})
	go func() {
		_, _ = l.fetched(ctx, wt, "rev-parse", "HEAD")
		close(got)
	}()
	select {
	case <-got:
		mu.Unlock()
		t.Fatal("a fetch from the worktree ran while its clone's fetch lock was held")
	case <-time.After(200 * time.Millisecond):
	}
	mu.Unlock()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the worktree's fetch never took its clone's lock")
	}
}

// (4) The clone's fetch lock is keyed on a path stable across the clone's own creation: a
// second lane of one repository (a second base, the card's own case) that calls clone after
// the first lane's `git clone` has made `<dir>/.git` must take the same lock and wait for the
// clone to finish, never prepare a half-made clone. Before the fix fetchKey returned `<dir>`
// before creation and `<dir>/.git` after, so the second lane took a different lock and ran on
// beside the clone in progress (land.go, clone; landpass.go, fetchKey). The test holds the
// pre-creation key exactly as the first lane does, makes `.git` as `git clone` does, and
// drives the second lane's clone call through that window; calling fetchLock with invented
// keys cannot catch a wrong call-site key, as TestAWorktreeFetchTakesItsClonesLock notes.
func TestTwoBasesOfOneRepoShareTheClonesCreationLock(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	l := &lander{a: r.a}
	l.locks()
	ctx := context.Background()
	root, err := r.a.landRoot()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(root, 0o755))
	dir := filepath.Join(root, repoDirName(r.remote))
	// the first lane is inside git clone: it holds the destination directory's lock, the
	// key before .git exists, and git clone has just made .git there
	mu := l.locks().fetchLock(l.fetchKey(ctx, dir))
	require.Equal(t, dir, l.fetchKey(ctx, dir), "a not-yet-made clone keys on its destination directory")
	mu.Lock()
	r.git("", "clone", "-q", r.remote, dir)
	// the second lane of the same repository (another base) calls clone: it must wait for
	// the first lane's clone, so it takes the same lock, not one keyed on the new .git
	done := make(chan struct{})
	go func() { _, _ = l.clone(ctx, r.remote); close(done) }()
	select {
	case <-done:
		mu.Unlock()
		t.Fatal("a second lane's clone ran on the clone in progress: it took a different lock")
	case <-time.After(200 * time.Millisecond):
	}
	mu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the second lane's clone never took its clone's creation lock")
	}
	r.clean()
}

// (d) A batch refused on one base does not stop another base's lane: the main lane lands
// while the alt lane's red tree gate blames its head.
func TestARefusedBatchOnOneBaseDoesNotStopAnotherBasesLane(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.files("the module", goModule)
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/alt")
	r.git(r.worker, "fetch", "-q", "origin")
	briefs := t.TempDir()
	r.promotionStream("s1")
	r.promotionStream("s2")
	r.ok("add --stream s1 --one --brief-file " + r.briefFile(t, briefs, "a", r.remote, "main"))
	r.ok("add --stream s2 --one --brief-file " + r.briefFile(t, briefs, "b", r.remote, "alt"))
	heads := map[string]string{}
	heads["a"] = r.head("a", "main", "a.go", "package main\n\nfunc a() {}\n")
	heads["b"] = r.head("b", "alt", "bad.go", vetRed)
	r.queued(heads, "a", "b")
	code, out, errs := r.do("land --land-parallel 2")
	assert.Equal(t, 1, code, out+errs)
	assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=main", "main's lane lands though alt's batch is refused")
	assert.Contains(t, errs, "ids=b fact=conflict")
	assert.Equal(t, map[string]string{"a": "landed/merged", "b": "ready/returned"}, r.places("a", "b"))
	r.clean()
}
