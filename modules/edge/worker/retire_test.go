package worker

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// Defect 78 (tla/Runner.tla: Retire, HolderPush, NoPushAfterFinal,
// BranchOnlyByHolder). A withdrawn bench job pushed its dirty head to the
// card's branch with a plain force on every recovery pass, rewinding the new
// holder's branch, because its directory (a dirty tree, a read-only Go module
// cache) was never removed.

const (
	testHolderSHA = "6666666666666666666666666666666666666666"
	testStaleSHA  = "7777777777777777777777777777777777777777"
	testBranch    = "sprint/s1/p1"
	testCloneURL  = "https://github.com/o/r.git"
)

// The lease form: OSGitOps pushes with --force-with-lease on the branch's
// full ref and the expected sha (empty: the branch must not exist), never a
// plain --force; BranchTip reads the work clone's remote-tracking ref.
func TestOSGitPushIsALeaseFromTheExpectedHead(t *testing.T) {
	t.Parallel()
	fx := edge.NewFakeExec()
	fx.On("git", func(c edge.FakeCall) edge.FakeOutcome {
		switch c.Cmd.Argv[gitArg0_internal] {
		case "remote":
			return edge.FakeOutcome{Stdout: testCloneURL + "\n"}
		case "config":
			return edge.FakeOutcome{Stdout: "remote.origin.url\n" + testCloneURL + "\x00"}
		case "ls-files":
			return edge.FakeOutcome{}
		}
		return edge.FakeOutcome{Stdout: testPriorSHA + "\n"}
	})
	g := &OSGitOps{WorkRoot: t.TempDir(), Exec: fx}
	ctx := context.Background()
	wt := filepath.Join(t.TempDir(), "repo")
	if err := g.Push(ctx, wt, testCloneURL, testBranch, testWorkSHA, testPriorSHA); err != nil {
		t.Fatal(err)
	}
	if err := g.Push(ctx, wt, testCloneURL, testBranch, testWorkSHA, ""); err != nil {
		t.Fatal(err)
	}
	tip, err := g.BranchTip(ctx, Job{Repo: "o/r", Forge: "github", Branch: testBranch})
	if err != nil || tip != testPriorSHA {
		t.Fatalf("BranchTip = %q, %v", tip, err)
	}
	var got []string
	for _, c := range fx.Calls() {
		if v := c.Cmd.Argv[gitArg0_internal]; v != "remote" && v != "config" && v != "ls-files" { // the guard's reads
			got = append(got, strings.Join(c.Cmd.Argv[gitArg0_internal:], " "))
		}
	}
	want := []string{
		"push --no-recurse-submodules --force-with-lease=refs/heads/" + testBranch + ":" + testPriorSHA + " " + testCloneURL + " " + testWorkSHA + ":refs/heads/" + testBranch,
		"push --no-recurse-submodules --force-with-lease=refs/heads/" + testBranch + ": " + testCloneURL + " " + testWorkSHA + ":refs/heads/" + testBranch,
		"for-each-ref --format=%(objectname) refs/remotes/origin/" + testBranch,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("git argv:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The lint: no plain --force argument anywhere in the worker's sources.
func TestWorkerSourcesHaveNoPlainForce(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	plain := regexp.MustCompile(`--force([^-]|$)`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if plain.MatchString(line) {
				t.Errorf("%s:%d: a plain --force (defect 78: a push moves a branch only by lease): %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// A normal job's pushes: the first leases from the tip it found at stage
// (here absent), the rebased one from the head it pushed.
func TestJobPushesLeaseFromStartTipThenFromItsOwnHead(t *testing.T) {
	t.Parallel()
	clock := testClock_internal()
	st, row := seedFakeStore_internal(t, clock)
	git := newFakeGit_internal()
	git.rebasedSHA = testRebSHA
	wall := &fakeWall{runFn: func(_ int, j Job, _, _ []string) (WallRunResult, error) {
		git.mu.Lock()
		git.headByWT[j.WorktreeDir] = testWorkSHA
		git.mu.Unlock()
		_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
		return WallRunResult{Elapsed: time.Second}, nil
	}}
	r, err := NewRunner(Runner{Host: "b1", RootDir: t.TempDir(), Sprint: &StoreSprintClient{Store: st, Origin: git, Now: clock}, Git: git, Wall: wall, Now: clock, Sleep: noSleep_internal})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.StepRow(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	want := []fakePush{{testBranch, testWorkSHA, ""}, {testBranch, testRebSHA, testWorkSHA}}
	if got := git.pushes(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("pushes %+v, want %+v", got, want)
	}
}

// retireRig_internal is a row whose consumer p1.w1 (gen 1) has a job
// directory on disk as the live defect left it: a work job at running with
// its harness gone, a dirty tree whose head is a stale commit, a read-only
// module cache inside it, and the card's branch already moved by the new
// holder.
type retireRig_internal struct {
	r       *Runner
	sp      *crSprint
	git     *fakeGit
	jobDir  string
	removes int
	warns   []string
	mu      sync.Mutex
}

func newRetireRig_internal(t *testing.T, next NextResponse, rec JobRecord) *retireRig_internal {
	t.Helper()
	root := t.TempDir()
	g := &retireRig_internal{sp: &crSprint{next: next}, git: newFakeGit_internal(), jobDir: filepath.Join(root, "jobs", rec.Consumer)}
	writeStagedRecord_internal(t, g.jobDir, rec)
	wt := filepath.Join(g.jobDir, "repo")
	cache := filepath.Join(wt, ".gomodcache", "mod", "example.com", "m@v1")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "go.mod"), []byte("module m\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{cache, filepath.Dir(cache), filepath.Dir(filepath.Dir(cache))} {
		if err := os.Chmod(d, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = removeTree_internal(root) })
	g.git.headByWT[wt] = testStaleSHA
	g.git.originHeads[testBranch] = testHolderSHA
	r, err := NewRunner(Runner{
		Host: "b1", RootDir: root, Sprint: g.sp, Git: g.git, Wall: &fakeWall{}, Procs: &fakeProcTable{alive: map[string]bool{}},
		DefaultHarness: &ScriptHarness{}, Now: testClock_internal(), Sleep: noSleep_internal,
		Warn: func(s string) { g.mu.Lock(); g.warns = append(g.warns, s); g.mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	r.RemoveDir = func(dir string) error { g.mu.Lock(); g.removes++; g.mu.Unlock(); return removeTree_internal(dir) }
	g.r = r
	return g
}

func staleWorkRecord_internal(state string, gen int) JobRecord {
	return JobRecord{
		Epoch: 1, Consumer: "p1.w1", Producer: "p1", Kind: "work", Row: "vision.ds", Stream: "s1",
		Branch: testBranch, BaseSHA: testBaseSHA, PushedHead: testWorkSHA, Gen: gen, Attempt: 1,
		State: state, PID: 4242, Nonce: "n4242",
	}
}

// The defect's own path: the consumer is final (withdrawn, the card re-dealt);
// three recovery passes push nothing, remove the directory once, and keep
// the retired record outside it.
func TestWithdrawnJobPushesNothingAndIsRemovedOnce(t *testing.T) {
	t.Parallel()
	g := newRetireRig_internal(t, NextResponse{Epoch: 1, Row: "vision.ds", State: card.Up, Width: 1,
		Final: []ConsumerSummary{{ID: "p1.w1", Kind: "work", State: card.Fail, Gen: 2}}}, staleWorkRecord_internal(JobRunning, 1))
	for pass := 0; pass < 3; pass++ {
		if err := g.r.RecoverRow(context.Background(), "vision.ds"); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}
	if got := g.git.pushes(); len(got) != 0 {
		t.Fatalf("a final consumer's job pushed: %+v", got)
	}
	if g.git.originHeads[testBranch] != testHolderSHA {
		t.Fatalf("the holder's branch moved to %s", g.git.originHeads[testBranch])
	}
	if _, err := os.Stat(g.jobDir); !errors.Is(err, os.ErrNotExist) || g.removes != 1 {
		t.Fatalf("job directory: stat %v, removals %d (want gone after exactly one)", err, g.removes)
	}
	if rec, err := ReadJobRecord(g.r.retiredDir_internal(staleWorkRecord_internal(JobRunning, 1))); err != nil || rec.State != JobCleaned {
		t.Fatalf("retired record: %+v, %v", rec, err)
	}
	// The rig's harness is not running: that is said once, nothing else.
	if len(g.warns) != 1 || !strings.Contains(g.warns[0], "no live process matched pid 4242 with nonce n4242") {
		t.Fatalf("warnings: %q", g.warns)
	}
}

// A removal that fails is raised once (stderr), the directory moved to
// quarantine, and never tried again.
func TestRetireFailureIsRaisedOnceAndQuarantined(t *testing.T) {
	t.Parallel()
	g := newRetireRig_internal(t, NextResponse{Epoch: 1, Row: "vision.ds", State: card.Up, Width: 1,
		Final: []ConsumerSummary{{ID: "p1.w1", Kind: "work", State: card.Done, Gen: 2}}}, staleWorkRecord_internal(JobReporting, 1))
	g.r.RemoveDir = func(string) error { g.mu.Lock(); g.removes++; g.mu.Unlock(); return errors.New("permission denied") }
	for pass := 0; pass < 3; pass++ {
		if err := g.r.RecoverRow(context.Background(), "vision.ds"); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}
	quarantined := filepath.Join(g.r.RootDir, "quarantine", "p1.w1@1")
	if _, err := os.Stat(quarantined); err != nil || g.removes != 1 || len(g.git.pushes()) != 0 {
		t.Fatalf("quarantine stat %v, removals %d, pushes %+v", err, g.removes, g.git.pushes())
	}
	if len(g.warns) != 1 || !strings.Contains(g.warns[0], "could not be removed (permission denied)") || !strings.Contains(g.warns[0], "set aside in .../p1.w1@1") {
		t.Fatalf("want one warning naming the fix, got %q", g.warns)
	}
}

// A job of a stale generation (the row holds the consumer at a newer one) is
// retired without a push; the newer generation's card is then free to run.
func TestStaleGenerationJobIsRetiredWithoutAPush(t *testing.T) {
	t.Parallel()
	g := newRetireRig_internal(t, NextResponse{Epoch: 1, Row: "vision.ds", State: card.Up, Width: 1,
		Working: []ConsumerSummary{{ID: "p1.w1", Kind: "work", State: card.Working, Gen: 5, Attempt: 1}}}, staleWorkRecord_internal(JobHarvested, 3))
	if _, _, err := g.r.recoverRow_internal(context.Background(), "vision.ds"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(g.jobDir); !errors.Is(err, os.ErrNotExist) || len(g.git.pushes()) != 0 || len(g.sp.reported()) != 0 {
		t.Fatalf("stat %v, pushes %+v, reports %+v", err, g.git.pushes(), g.sp.reported())
	}
}

// The gate: a job whose consumer this worker has seen final never pushes,
// whatever path asks.
func TestPushGateRefusesAJobThatNoLongerHolds(t *testing.T) {
	t.Parallel()
	g := newRetireRig_internal(t, NextResponse{Epoch: 1, Row: "vision.ds"}, staleWorkRecord_internal(JobHarvested, 1))
	rec := staleWorkRecord_internal(JobHarvested, 1)
	g.git.originHeads[testBranch] = testWorkSHA
	if err := g.r.pushBranch_internal(context.Background(), &rec, testStaleSHA); err != nil {
		t.Fatalf("a holder's push by lease: %v", err)
	}
	g.r.markGone_internal(1, "p1.w1", math.MaxInt)
	err := g.r.pushBranch_internal(context.Background(), &rec, testRebSHA)
	if err == nil || !strings.Contains(err.Error(), "no longer holds its card") || len(g.git.pushes()) != 1 {
		t.Fatalf("after final: %v, pushes %+v", err, g.git.pushes())
	}
	if !g.r.holds_internal(JobRecord{Epoch: 2, Consumer: "p1.w1", Gen: 1}) {
		t.Fatal("a final consumer of epoch 1 made epoch 2's job of the same id a non-holder")
	}
}

// The restart's release: one push, by lease from the tip the job found, then
// the release and the directory retired; a branch another holder moved is
// never overwritten.
func TestReleasePushesOnceByLeaseThenRetires(t *testing.T) {
	t.Parallel()
	for _, moved := range []bool{false, true} {
		rec := staleWorkRecord_internal(JobStaged, 1)
		rec.PushedHead, rec.StartTip = "", testPriorSHA
		g := newRetireRig_internal(t, NextResponse{Epoch: 1, Row: "vision.ds", State: card.Up, Width: 1,
			Working: []ConsumerSummary{{ID: "p1.w1", Kind: "work", State: card.Working, Gen: 1, Attempt: 1}}}, rec)
		g.git.originHeads[testBranch] = testPriorSHA
		if moved {
			g.git.originHeads[testBranch] = testHolderSHA
		}
		if err := g.r.RecoverRow(context.Background(), "vision.ds"); err != nil {
			t.Fatal(err)
		}
		pushes := g.git.pushes()
		if len(pushes) != 1 || pushes[0] != (fakePush{testBranch, testStaleSHA, testPriorSHA}) || len(g.sp.releases) != 1 {
			t.Fatalf("moved=%v: pushes %+v releases %v", moved, pushes, g.sp.releases)
		}
		if want := map[bool]string{false: testStaleSHA, true: testHolderSHA}[moved]; g.git.originHeads[testBranch] != want {
			t.Fatalf("moved=%v: branch at %s, want %s", moved, g.git.originHeads[testBranch], want)
		}
		if _, err := os.Stat(g.jobDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("moved=%v: job directory after the release: %v", moved, err)
		}
	}
}
