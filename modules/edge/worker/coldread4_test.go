package worker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
)

// Cold read 3, second batch, 2026-11-08, at 032afd657 (modules/edge,
// modules/dash): one failing probe per confirmed defect.

// cpu.go Run: a Second meter (top on macOS) that failed at once was stepped
// again with no wait, a busy loop, and its error was dropped. A failed step
// is waited on and said once per cause.
func TestColdread4CPUSamplerFailingMeterWaitsAndSaysSo(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls, sleeps := 0, 0
	s := &CPUSampler{Second: func(context.Context) (float64, error) {
		if calls++; calls >= 5 {
			cancel()
		}
		return 0, errors.New("top: no CPU usage: exit 1")
	}}
	var warned []string
	s.Run(ctx, func() time.Time { return time.Unix(1000, 0) }, func(time.Duration) { sleeps++ }, func(what string, err error) {
		warned = append(warned, what+": "+err.Error())
	})
	if sleeps < calls-1 {
		t.Errorf("%d failed steps, %d waits: a failing meter is stepped in a busy loop", calls, sleeps)
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "exit 1") {
		t.Errorf("warnings %q: want the meter's failure said once", warned)
	}
}

// proxy.go: one lastStatus for every job, so job B, ending after job A's
// request got a 503, was classified by A's 503 (runner.go ConsumeStatus).
// A provider status is the job's whose placeholder key the request carried.
func TestColdread4ProxyStatusIsPerJob(t *testing.T) {
	t.Parallel()
	p := &KeyProxy{Routes: map[string]ProxyRoute{"deepseek": {Name: "deepseek", UpstreamURL: "https://api.deepseek.com", AllowedHosts: []string{"api.deepseek.com"}, Key: func() (string, error) { return "sk-x", nil }}},
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
		})}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7000/deepseek/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+ProxyKey("p1.a1"))
	p.ServeHTTP(httptest.NewRecorder(), req)
	if got := p.ConsumeStatus("p2.b1"); got != 0 {
		t.Errorf("job p2.b1 consumed status %d from job p1.a1's request", got)
	}
	if got := p.ConsumeStatus("p1.a1"); got != http.StatusServiceUnavailable {
		t.Errorf("job p1.a1 consumed %d, want its own 503", got)
	}
}

// runner.go CloneURL and git.CloneURL disagreed: admission (git.CloneURL)
// took an scp-like address or a ./ path the worker refused, so every card of
// such a stream faulted "stage refused". The worker stages from the one
// function admission uses.
func TestColdread4WorkerStagesEveryRepoAdmissionAccepts(t *testing.T) {
	t.Parallel()
	for _, rf := range [][2]string{{"deploy@host:repo.git", ""}, {"host:org/repo.git", "github"}, {"./repo", ""}, {"o/r", "github"}} {
		want, err := git.CloneURL(rf[0], rf[1])
		if err != nil {
			t.Fatalf("admission refused %q: %v", rf[0], err)
		}
		if got, _, err := cloneURL_internal(Job{Repo: rf[0], Forge: rf[1]}); err != nil || got != want {
			t.Errorf("repo %q forge %q: admission's clone URL %q, the worker's %q (%v)", rf[0], rf[1], want, got, err)
		}
	}
}

// harness.go BuildWallArgv put --dir before the verb ("nova-reserve --dir D
// run"), which nova-reserve refuses (`unexpected "run"`): a wall with a
// reserve directory of its own (a test's, isolated from the host's slots)
// could never run anything. The verb comes first, as `nova-reserve help`
// writes it.
func TestColdread4WallReserveDirFollowsTheVerb(t *testing.T) {
	t.Parallel()
	argv := BuildWallArgv(WallConfig{ReserveBin: "nova-reserve", ReserveDir: "/tmp/r", SandboxBin: "nova-sandbox"}, Job{Consumer: "c1", JobDir: "/tmp/j"}, nil, []string{"true"})
	line := strings.Join(argv, " ")
	if !strings.Contains(line, "nova-reserve run --dir /tmp/r ") {
		t.Errorf("wall argv %q: nova-reserve wants its verb before --dir", line)
	}
}

// runner.go cleanJobDir_internal removed a reported job's directory with
// os.RemoveAll, which a read-only Go module cache inside it defeats; its
// error then went to resendErr_internal as if the sprint had refused the
// report. It removes the tree as the retire path does (removeTree_internal).
func TestColdread4CleanRemovesAReadOnlyTree(t *testing.T) {
	t.Parallel()
	sp := &cr2Sprint{}
	r := cr2Runner_internal(t, sp, &cr2Wall{}, &cr2Procs{}, time.Unix(1000, 0))
	jobDir := filepath.Join(r.RootDir, "jobs", "p1.r1")
	rec := JobRecord{Epoch: 3, Consumer: "p1.r1", Kind: "read", Row: "vision.ds", Gen: 2, State: JobReporting, Outcome: Outcome{Result: "ok"}}
	if err := WriteJobRecord(jobDir, rec); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(jobDir, "home", "go", "pkg", "mod", "m@v1")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "go.mod"), []byte("module m\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cache, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cache, 0o755) })
	if err := r.sendReportAndClean_internal(context.Background(), jobDir, rec); err != nil {
		t.Errorf("clean of a reported job with a read-only module cache: %v", err)
	}
	if fileExists_internal(jobDir) {
		t.Errorf("the job directory is still there")
	}
}

// fakeWall.testFn was never set: no test ran a done-when command red. A red
// done-when test on the rebased head fails the card, its reason naming the
// command and its output, and the rebased head is never pushed.
func TestColdread4DoneWhenRedFailsTheCard(t *testing.T) {
	t.Parallel()
	clock := testClock_internal()
	st, row := seedFakeStore_internal(t, clock)
	git := newFakeGit_internal()
	git.rebasedSHA = testRebSHA
	wall := &fakeWall{
		runFn: func(_ int, j Job, _, _ []string) (WallRunResult, error) {
			git.mu.Lock()
			git.headByWT[j.WorktreeDir] = testWorkSHA
			git.mu.Unlock()
			_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
			return WallRunResult{ExitCode: 0, Elapsed: time.Second}, nil
		},
		testFn: func(Job, string) (WallRunResult, error) {
			return WallRunResult{ExitCode: 1, Output: []byte("--- FAIL: TestX")}, nil
		},
	}
	sc := &StoreSprintClient{Store: st, Origin: git, Now: clock}
	root := t.TempDir()
	r, _ := NewRunner(Runner{Sleep: noSleep_internal, Host: "b1", RootDir: root, StateDir: root, Sprint: sc, Git: git, Wall: wall, Now: clock})
	if err := r.StepRow(context.Background(), row); err != nil {
		t.Fatalf("StepRow: %v", err)
	}
	if len(wall.testCalls) != 1 || wall.testCalls[0] != "go test ./modules/edge/worker/..." {
		t.Fatalf("the done-when command did not run once: %v", wall.testCalls)
	}
	snap, _ := st.Snapshot(context.Background())
	w := snap.Cards["p1.w1"]
	if w.Result != "fail" || !strings.Contains(w.Reason, "tests red (go test ./modules/edge/worker/...)") || !strings.Contains(w.Reason, "--- FAIL: TestX") {
		t.Fatalf("a red done-when test must fail the card with its command and output: %+v", w)
	}
	if p := snap.Cards["p1"]; p.Head == testRebSHA {
		t.Fatalf("the rebased head was pushed although its tests were red: %+v", p)
	}
}
