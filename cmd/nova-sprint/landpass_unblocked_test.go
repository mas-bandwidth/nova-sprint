package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A base gate waiting on one stream must not prevent a different base's already green
// stream from landing: the built batch lands once its landGrace is spent, while the
// higher-priority stream's gate still runs (fault item 4, 2026-10-10: before, the landings
// went in priority order and the green batch waited for the whole of that gate, up to its
// LandDeadline); released, the first stream lands in the same pass.
func TestLandAnotherBaseProgressesWhileFirstBaseGateWaits(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.files("the module", goModule)
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main", "HEAD:refs/heads/other")
	r.git(r.worker, "fetch", "-q", "origin")
	r.moveBase("other", "other-base.txt")
	r.git(r.worker, "fetch", "-q", "origin")
	r.promotionStream("s1")
	r.promotionStream("s2")
	heads := map[string]string{}
	for _, row := range []struct{ stream, base string }{{"s1", "main"}, {"s2", "other"}} {
		id := row.stream + "-1"
		brief := filepath.Join(t.TempDir(), id+".md")
		require.NoError(t, os.WriteFile(brief, []byte(passingBrief("REPO: "+r.remote+"\nBASE: "+row.base+"\n\nWrite "+id+".")), 0o600))
		r.ok("add --stream " + row.stream + " --one --brief-file " + brief)
		heads[id] = r.head(id, row.base, row.stream+".go", "package main\n\nfunc "+row.stream+"() {}\n")
	}
	r.queued(heads, "s1-1", "s2-1")
	blocked, release, pushed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	r.a.landDeadline = func(string) <-chan time.Time { return nil } // no bound fires: the gate is slow, not stuck
	spent := make(chan time.Time)
	close(spent)
	r.a.landGrace = func(string) <-chan time.Time { return spent }
	var once sync.Once
	r.a.gateRan = func(dir string, tests bool) {
		if strings.HasSuffix(dir, "@s1") {
			once.Do(func() { close(blocked) })
		}
	}
	b := r.a.landState()
	b.mu.Lock()
	b.gateBench = func(ctx context.Context, _, dir string, _ [][]string, _ bool) (string, int, error) {
		if strings.HasSuffix(dir, "@s1") {
			select {
			case <-ctx.Done():
				return "", 1, ctx.Err()
			case <-release:
				return "", 0, nil
			}
		}
		return "", 0, nil
	}
	b.mu.Unlock()
	var pushOnce sync.Once
	r.a.beforePush = func(int) { pushOnce.Do(func() { close(pushed) }) }
	done := make(chan struct{})
	var code int
	var out, errs string
	go func() { code, out, errs = r.do("land --land-parallel 2"); close(done) }()
	select {
	case <-blocked:
	case <-t.Context().Done():
		t.Fatal("first base gate never began")
	}
	select {
	case <-pushed:
		// the first push of the pass, made while s1's gate still waits: only s2 can make it
	case <-t.Context().Done():
		t.Fatal("green second base did not land while first base gate waited")
	}
	close(release)
	select {
	case <-done:
	case <-t.Context().Done():
		t.Fatal("land did not finish after releasing first gate")
	}
	assert.Equal(t, 0, code, "%s%s", out, errs)
	assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s2-1": "landed/merged"}, r.places("s1-1", "s2-1"))
	r.clean()
}

// Once a red batch gate falls back to its bisection, cancellation of a bisection gate is
// still a pass-level timeout, never a finding against a card.
func TestLandFallbackGateDeadlineDoesNotBlameCard(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.files("the module", goModule)
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	mainBase := r.git(r.remote, "rev-parse", "main")
	r.promotionStream("s1")
	var add []string
	heads := map[string]string{}
	for _, id := range []string{"s1-1", "s1-2"} {
		brief := filepath.Join(t.TempDir(), id+".md")
		require.NoError(t, os.WriteFile(brief, []byte(passingBrief("REPO: "+r.remote+"\nBASE: main\n\nWrite "+id+".")), 0o600))
		add = append(add, "--brief-file "+brief)
		heads[id] = r.head(id, "main", strings.ReplaceAll(id, "-", "")+".go", "package main\n\nfunc "+strings.ReplaceAll(id, "-", "")+"() {}\n")
	}
	r.ok("add --stream s1 " + strings.Join(add, " "))
	r.queued(heads, "s1-1", "s1-2")
	deadline := make(chan time.Time, 1)
	r.a.landDeadline = func(string) <-chan time.Time { return deadline }
	var gates atomic.Int32
	fallback := make(chan struct{})
	b := r.a.landState()
	b.mu.Lock()
	b.gateBench = func(ctx context.Context, _, dir string, _ [][]string, _ bool) (string, int, error) {
		if !strings.HasSuffix(dir, "@s1") {
			return "", 0, nil
		}
		call := gates.Add(1)
		if call == 1 {
			return "", 0, nil // the base gate is green
		}
		if call == 2 {
			return "batch gate red", 1, nil
		}
		if call == 3 {
			close(fallback) // the bisection's gate of the tip after s1-1
		}
		<-ctx.Done()
		return "", 1, ctx.Err()
	}
	b.mu.Unlock()
	done := make(chan struct{})
	var code int
	var out, errs string
	go func() { code, out, errs = r.do("land"); close(done) }()
	select {
	case <-fallback:
		deadline <- time.Now()
	case <-t.Context().Done():
		t.Fatal("bisection gate never began")
	}
	select {
	case <-done:
	case <-t.Context().Done():
		t.Fatal("land did not finish after the bisection gate's deadline")
	}
	assert.Equal(t, 1, code, "%s%s", out, errs)
	assert.Contains(t, out+errs, "gate of this batch exceeded", "timeout is a batch refusal")
	assert.NotContains(t, out+errs, "fact=conflict", "no card is blamed")
	assert.Equal(t, map[string]string{"s1-1": "merging/queued", "s1-2": "merging/queued"}, r.places("s1-1", "s1-2"))
	assert.Nil(t, r.a.baseGateFails[mainBase], "canceled fallback gate is not counted as red")
	r.clean()
}

// stuckBehind is the shape of the hang the cold read of PR 5475 found (2026-10-09): two
// streams on different bases, the lower-priority one holding what the higher-priority one
// waits for while its own gate never answers. s1's goroutine is held at the beforeWait
// seam until s2's gate has begun, so s2 is the holder whatever order the goroutines start
// in. Since 2026-10-10 a job waiting for a slot runs no clock; one waiting for another
// stream's gate of its base commit (sameCommit) already holds its slot, so its own clock
// runs (here s1's never fires). Either way the holder's own bound (its LandDeadline, from
// when it took its slot) cancels its gate, which frees the wait, and s1 lands in the same
// pass; s2's batch stays queued, nothing blamed, no base counted red. parallel is the pass's width; sameCommit puts the two bases at one commit
// (the per-commit gate is the wait, width 2), else the bases differ and the width slot is
// the wait (width 1).
func stuckBehind(t *testing.T, parallel int, sameCommit bool) {
	r := newLandRig(t)
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.files("the module", goModule)
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main", "HEAD:refs/heads/other")
	r.git(r.worker, "fetch", "-q", "origin")
	if !sameCommit {
		r.moveBase("other", "other-base.txt")
		r.git(r.worker, "fetch", "-q", "origin")
	}
	mainBase, otherBase := r.git(r.remote, "rev-parse", "main"), r.git(r.remote, "rev-parse", "other")
	r.promotionStream("s1")
	r.promotionStream("s2")
	heads := map[string]string{}
	for _, row := range []struct{ stream, base string }{{"s1", "main"}, {"s2", "other"}} {
		id := row.stream + "-1"
		brief := filepath.Join(t.TempDir(), id+".md")
		require.NoError(t, os.WriteFile(brief, []byte(passingBrief("REPO: "+r.remote+"\nBASE: "+row.base+"\n\nWrite "+id+".")), 0o600))
		r.ok("add --stream " + row.stream + " --one --brief-file " + brief)
		heads[id] = r.head(id, row.base, row.stream+".go", "package main\n\nfunc "+row.stream+"() {}\n")
	}
	r.queued(heads, "s1-1", "s2-1")
	holding, waiting := make(chan struct{}), make(chan struct{})
	deadline := make(chan time.Time, 1)
	r.a.landDeadline = func(stream string) <-chan time.Time {
		if stream == "s2" {
			return deadline
		}
		return nil // s1's bound never fires: it must not need to
	}
	wait := "slot"
	if sameCommit {
		wait = "gate"
	}
	var waitOnce sync.Once
	r.a.beforeWait = func(stream, what string) {
		if stream == "s1" && what == wait {
			<-holding // s2 holds the slot (or the commit's gate) and is stuck before s1 asks
			waitOnce.Do(func() { close(waiting) })
		}
	}
	b := r.a.landState()
	b.mu.Lock()
	var holdOnce sync.Once
	b.gateBench = func(ctx context.Context, _, dir string, _ [][]string, _ bool) (string, int, error) {
		if strings.HasSuffix(dir, "@s2") {
			holdOnce.Do(func() { close(holding) })
			<-ctx.Done() // the bench never answers: only the holder's own bound ends this gate
			return "", 1, ctx.Err()
		}
		return "", 0, nil
	}
	b.mu.Unlock()
	done := make(chan struct{})
	var code int
	var out, errs string
	go func() { code, out, errs = r.do("land --land-parallel " + strconv.Itoa(parallel)); close(done) }()
	select {
	case <-holding:
	case <-t.Context().Done():
		t.Fatal("the holder's gate never began")
	}
	select {
	case <-waiting:
	case <-t.Context().Done():
		t.Fatal("s1 never reached its wait behind s2")
	}
	deadline <- time.Now() // s2's own bound: its gate is cancelled and the wait it held is freed
	select {
	case <-done:
	case <-t.Context().Done():
		t.Fatal("the pass did not end after the holder's bound fired")
	}
	assert.Equal(t, 1, code, "s2's batch is refused for this pass: %s%s", out, errs)
	assert.Equal(t, 1, strings.Count(out+errs, "gate of this batch exceeded"), "the holder's abandonment is the deadline refusal, no card blamed: %s%s", out, errs)
	assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s2-1": "merging/queued"}, r.places("s1-1", "s2-1"))
	assert.Nil(t, r.a.baseGateFails[mainBase], "a wait is not a red base")
	assert.Nil(t, r.a.baseGateFails[otherBase], "an abandoned gate is not a red base")
	r.clean()
}

// A higher-priority stream waiting for the pass's only width slot, held by a stream stuck on
// its bench, lands once the holder's own bound fires.
func TestLandCancelledSlotWaitLeavesThePass(t *testing.T) {
	t.Parallel()
	stuckBehind(t, 1, false)
}

// Two bases at one commit share one gate: a stream waiting for another stream's gate of that
// commit lands once the holder's own bound fires.
func TestLandCancelledGateWaitLeavesThePass(t *testing.T) {
	t.Parallel()
	stuckBehind(t, 2, true)
}

// A red batch of eight heads blames the one that turned the tree red in three gates, not
// eight: the heads before it land, it is reworked at the tip, and the heads after it land in
// the same pass (fault item 4, 2026-10-10: a 99-card batch walked head by head ran past
// LandDeadline and stalled its stream for hours).
func TestLandBisectsARedBatchToTheBreakingHead(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	cards := map[string]map[string]string{}
	for i := 1; i <= 8; i++ {
		id := "a" + strconv.Itoa(i)
		cards[id] = map[string]string{id + ".go": "package main\n\nfunc " + id + "() {}\n"}
	}
	twoStreams(t, r, cards, map[string]map[string]string{})
	var gates atomic.Int32
	r.a.gateRan = func(string, bool) { gates.Add(1) }
	b := r.a.landState()
	b.mu.Lock()
	b.gateBench = func(_ context.Context, _, dir string, _ [][]string, _ bool) (string, int, error) {
		if _, err := os.Stat(filepath.Join(dir, "a6.go")); err == nil {
			return "a6 breaks the build", 1, nil
		}
		return "", 0, nil
	}
	b.mu.Unlock()
	code, out, errs := r.do("land")
	assert.Equal(t, 1, code, out+errs)
	assert.Contains(t, errs, "ids=a6 fact=conflict reason=the head "+r.git(r.worker, "rev-parse", "sprint/a6")+" of a6 fails the tree gate")
	places := r.places("a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8")
	for _, id := range []string{"a1", "a2", "a3", "a4", "a5", "a7", "a8"} {
		assert.Equal(t, "landed/merged", places[id], id)
	}
	assert.Equal(t, "ready/returned", places["a6"])
	// the base, the batch's tree, three bisection gates (after a4 green, after a6 red, after
	// a5 green); then a7 and a8's batch on the gated tip a1..a5: its tree alone
	assert.EqualValues(t, 6, gates.Load())
	r.clean()
}

// A bench that could not run the gate is not a red tree: no go on the PATH (exit 127) or a
// full disk is a fault of the bench, which blames no head.
func TestBenchFaultIsNotARedTree(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "sh: 1: go: not found", benchFault(127, "GATE RUN: go build ./...\nsh: 1: go: not found\n"))
	assert.Contains(t, benchFault(1, "GATE RUN: go build ./...\nwrite /tmp/go-build1/importcfg: disk quota exceeded"), "disk quota exceeded")
	assert.Contains(t, benchFault(2, "mkdir /tmp/go-build2/: no space left on device"), "no space left")
	assert.Empty(t, benchFault(1, "GATE RUN: go build ./...\n./bad.go:3:14: syntax error: unexpected newline"))
	assert.Empty(t, benchFault(1, "--- FAIL: TestX\n    x_test.go:9: file not found in the fixture"), "a test's own words are its finding")
	// a build that ran out of disk in go test is still the bench's: its package never ran
	assert.Contains(t, benchFault(1, "GATE RUN: go test ./...\n# example.com/m/p\ncompile: writing output: write $WORK/b001/_pkg_.a: no space left on device\nFAIL\texample.com/m/p [build failed]\nFAIL"), "no space left")
	// the tree gate tests two packages: go prints a failed build's errors at once, and
	// another package's result may come before the build's own [build failed] line (the
	// order go printed on space, 2026-10-10; #48 reader A): the block stays the bench's
	for name, out := range map[string]string{
		"ok before the build's line": "GATE RUN: go test ...\n# example.com/m/internal/ci [example.com/m/internal/ci.test]\ncompile: writing output: write $WORK/b002/_pkg_.a: no space left on device\nok  \texample.com/m/internal/docs\t2.005s\nFAIL\texample.com/m/internal/ci [build failed]\nFAIL",
		"a package run after it":     "GATE RUN: go test ./...\n# example.com/m/b\nwrite $WORK/b003/_pkg_.a: disk quota exceeded\nok  \texample.com/m/a\t0.010s\nFAIL\texample.com/m/b [build failed]\nok  \texample.com/m/c\t0.020s\nFAIL",
		"a red test after it":        "GATE RUN: go test ./...\n# example.com/m/b\nwrite $WORK/b003/_pkg_.a: no space left on device\n--- FAIL: TestA (0.00s)\n    a_test.go:3: boom\nFAIL\nFAIL\texample.com/m/a\t0.010s\nFAIL\texample.com/m/b [build failed]\nFAIL",
	} {
		assert.NotEmpty(t, benchFault(1, out), name)
	}
	// a test that printed a header-shaped line, then the words, then failed (#48 reader B's
	// probes at 1fd202d): with -v the header is inside the test's output; without it the
	// block is followed by the test's --- FAIL and its package's result, and never answered
	assert.Empty(t, benchFault(1, "GATE RUN: go test ./...\n=== RUN   TestDisk\n# setup\nwrite /tmp/x: no space left on device\n--- FAIL: TestDisk (0.00s)\nFAIL\nFAIL\texample.com/m/disk\t0.012s\nFAIL"))
	assert.Empty(t, benchFault(1, "GATE RUN: go test ./...\n# setup\nwrite /tmp/x: no space left on device\n--- FAIL: TestDisk (0.00s)\nFAIL\nFAIL\texample.com/m/disk\t0.012s\nFAIL"))
	// a test binary's link and an external test package are headed by other names than the
	// package their result names (#48 reader B's probes at 3690f52): still the bench's
	assert.Contains(t, benchFault(1, "GATE RUN: go test ./...\n# example.com/m/p.test\nlink: write $WORK/b001/p.test: no space left on device\nok  \texample.com/m/a\t0.010s\nFAIL\texample.com/m/p [build failed]\nFAIL"), "no space left")
	assert.Contains(t, benchFault(1, "GATE RUN: go test ./...\n# example.com/m/p_test [example.com/m/p.test]\ncompile: writing output: write $WORK/b002/_pkg_.a: no space left on device\nFAIL\texample.com/m/p [build failed]\nFAIL"), "no space left")
	// a build that fails and go stops: no result follows its block, the bench's
	assert.NotEmpty(t, benchFault(1, "GATE RUN: go build ./...\n# example.com/m/p\nwrite $WORK/b001/_pkg_.a: no space left on device"))
	// a test that printed a header-shaped line of its own package, then the words, then
	// failed with no --- FAIL (os.Exit): its package's result says the binary ran
	assert.Empty(t, benchFault(1, "GATE RUN: go test ./...\n# example.com/m/disk\nwrite /tmp/x: no space left on device\nFAIL\texample.com/m/disk\t0.004s\nFAIL"))
	// v1.2.5 candidate 4: a test binary's own words, raw or logged, before or after its
	// --- FAIL line, are never the bench's, whatever they say
	for name, out := range map[string]string{
		"raw print before --- FAIL": testPrintsDiskFull,
		"logged":                    "GATE RUN: go test ./...\n--- FAIL: TestDisk (0.00s)\n    disk_test.go:12: write /tmp/x: no space left on device\nFAIL\nFAIL\texample.com/m/disk\t0.012s\nFAIL",
		"panic":                     "GATE RUN: go test ./...\npanic: write /tmp/x: disk quota exceeded\n\ngoroutine 1 [running]:\nFAIL\texample.com/m/disk\t0.020s\nFAIL",
		"no --- FAIL, os.Exit":      "GATE RUN: go test ./...\nwrite /tmp/x: no space left on device\nFAIL\texample.com/m/disk\t0.004s\nFAIL",
		"an earlier green run":      "GATE RUN: go vet ./...\nwarning: no space left on device for the cache, it is skipped\nGATE RUN: go build ./...\n./bad.go:3:14: syntax error: unexpected newline",
	} {
		assert.Empty(t, benchFault(1, out), name)
	}
}

// testPrintsDiskFull is a red gate whose only failure is a test's: it printed the words a
// full disk prints, and then failed (v1.2.5 candidate 4).
const testPrintsDiskFull = "GATE RUN: go test ./...\nok  \texample.com/m/a\t0.010s\nwrite /tmp/x: no space left on device\n--- FAIL: TestDisk (0.00s)\nFAIL\nFAIL\texample.com/m/disk\t0.012s\nFAIL"

// A real test failure that prints "no space left on device" is a red tree: the head that
// brought it is blamed and the head before it lands, where before (v1.2.5 candidate 4) every
// slot of the ring refused it as a bench fault, the batch blamed no head, and it was refused
// again every pass.
func TestLandBlamesATestThatPrintsADiskFullLine(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	twoStreams(t, r, map[string]map[string]string{
		"a1": {"a1.go": "package main\n\nfunc a1() {}\n"},
		"a2": {"a2.go": "package main\n\nfunc a2() {}\n"},
	}, map[string]map[string]string{})
	b := r.a.landState()
	b.mu.Lock()
	b.gateBench = func(_ context.Context, _, dir string, _ [][]string, _ bool) (string, int, error) {
		if _, err := os.Stat(filepath.Join(dir, "a2.go")); err == nil {
			return testPrintsDiskFull, 1, nil
		}
		return "", 0, nil
	}
	b.mu.Unlock()
	code, out, errs := r.do("land")
	assert.Equal(t, 1, code, out+errs)
	assert.Contains(t, errs, "ids=a2 fact=conflict reason=the head "+r.git(r.worker, "rev-parse", "sprint/a2")+" of a2 fails the tree gate")
	assert.NotContains(t, out+errs, "no card is blamed", "a test's words are not a bench fault")
	assert.Equal(t, map[string]string{"a1": "landed/merged", "a2": "ready/returned"}, r.places("a1", "a2"))
	r.clean()
}
