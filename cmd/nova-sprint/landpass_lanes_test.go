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
	// the second gate of s1's worktree is the combined gate: it blocks until the other
	// repo's push (the second push of the pass) has happened, or two seconds pass
	var gates atomic.Int32
	var released atomic.Bool
	barrier := make(chan struct{})
	var once sync.Once
	var pushes atomic.Int32
	r.a.gateRan = func(dir string, _ bool) {
		if !strings.HasSuffix(dir, "@s1") {
			return
		}
		if gates.Add(1) != 2 {
			return
		}
		select {
		case <-barrier:
			released.Store(true)
		case <-time.After(2 * time.Second):
		}
	}
	r.a.beforePush = func(int) {
		if pushes.Add(1) == 2 {
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
	// two clones: both enter the fetch lock at once
	var mu sync.Mutex
	in, most := 0, 0
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	r.a.fetchHold = func(string) {
		mu.Lock()
		in++
		most = max(most, in)
		mu.Unlock()
		entered <- struct{}{}
		<-release
		mu.Lock()
		in--
		mu.Unlock()
	}
	var wg sync.WaitGroup
	for _, dir := range []string{"clone-a", "clone-b"} {
		wg.Add(1)
		go func(dir string) {
			defer wg.Done()
			_, _ = l.fetched(context.Background(), dir, "fetch", "--no-tags", "origin")
		}(dir)
	}
	<-entered
	<-entered
	mu.Lock()
	assert.Equal(t, 2, most, "two clones' fetches run side by side")
	mu.Unlock()
	close(release)
	wg.Wait()

	// one clone: the second fetch waits for the first
	mu.Lock()
	in, most = 0, 0
	mu.Unlock()
	entered = make(chan struct{}, 2)
	release = make(chan struct{})
	r.a.fetchHold = func(string) {
		mu.Lock()
		in++
		most = max(most, in)
		mu.Unlock()
		entered <- struct{}{}
		<-release
		mu.Lock()
		in--
		mu.Unlock()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = l.fetched(context.Background(), "clone-c", "fetch", "--no-tags", "origin")
	}()
	<-entered
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = l.fetched(context.Background(), "clone-c", "fetch", "--no-tags", "origin")
	}()
	select {
	case <-entered:
		t.Fatal("a second fetch of one clone entered while the first held the lock")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	<-entered
	wg.Wait()
	mu.Lock()
	assert.Equal(t, 1, most, "one clone's fetches stay serial")
	mu.Unlock()
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
