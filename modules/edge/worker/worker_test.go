package worker

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

const (
	testBaseSHA   = "1111111111111111111111111111111111111111"
	testWorkSHA   = "2222222222222222222222222222222222222222"
	testRebSHA    = "3333333333333333333333333333333333333333"
	testPriorSHA  = "4444444444444444444444444444444444444444"
	testCommitSHA = "5555555555555555555555555555555555555555"
)

func noSleep_internal(time.Duration) {}
func testClock_internal() func() time.Time {
	sec := int64(1000)
	var mu sync.Mutex
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		t := time.Unix(sec, 0).UTC()
		sec++
		return t
	}
}

type fakeGit struct {
	mu          sync.Mutex
	stageErr    error
	headByWT    map[string]string
	rebasedSHA  string
	rebaseErr   error
	pushed      map[string]string
	originHeads map[string]string
	dirtyByWT   map[string]bool
	pushErr     error
	removedWTs  []string
	stagedJobs  []Job
	commits     []string
	fetches     map[string]int // per repo
	fetchedJobs map[string]int // per repo: the jobs its fetches were for
	fetchErr    error
	pushLog     []fakePush
	streamTip   string // StreamTip's answer: the stream tip the last merge used
	tipErr      error
}

type fakePush struct{ branch, sha, expect string }

func newFakeGit_internal() *fakeGit {
	return &fakeGit{
		headByWT:    map[string]string{},
		pushed:      map[string]string{},
		originHeads: map[string]string{},
		dirtyByWT:   map[string]bool{},
	}
}

func (g *fakeGit) Fetch(_ context.Context, js ...Job) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.fetches == nil {
		g.fetches = map[string]int{}
	}
	g.fetches[js[0].Repo]++
	if g.fetchedJobs == nil {
		g.fetchedJobs = map[string]int{}
	}
	g.fetchedJobs[js[0].Repo] += len(js)
	return g.fetchErr
}

func (g *fakeGit) StageWorktree(_ context.Context, j Job) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stageErr != nil {
		return "", g.stageErr
	}
	g.stagedJobs = append(g.stagedJobs, j)
	_ = os.MkdirAll(j.WorktreeDir, 0o755)
	if g.headByWT[j.WorktreeDir] == "" {
		g.headByWT[j.WorktreeDir] = cmp.Or(j.PriorHead, j.BaseSHA)
	}
	return g.headByWT[j.WorktreeDir], nil
}

func (g *fakeGit) BranchTip(_ context.Context, j Job) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.originHeads[j.Branch], nil
}

// Collect is the seal step and the collect together: a dirty tree is
// committed (testCommitSHA), and the job tree's head is the worker clone's.
func (g *fakeGit) Collect(_ context.Context, sl Sealed) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	wt := filepath.Join(sl.JobDir, "repo")
	if g.dirtyByWT[wt] {
		g.dirtyByWT[wt] = false
		g.headByWT[wt] = testCommitSHA
		g.commits = append(g.commits, wt)
	}
	g.headByWT[sl.Clone] = g.headByWT[wt]
	return cmp.Or(g.headByWT[wt], sl.Base), nil
}

func (g *fakeGit) Export(context.Context, string, string, string) error { return nil }

// Push records every push (branch, sha, lease) and keeps git's lease: the
// branch on origin must be at expect ("" : absent).
func (g *fakeGit) Push(_ context.Context, _, _, branch, sha, expect string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pushLog = append(g.pushLog, fakePush{branch: branch, sha: sha, expect: expect})
	if g.pushErr != nil {
		return g.pushErr
	}
	if g.originHeads[branch] != expect {
		return fmt.Errorf("! [rejected] %s -> %s (stale info): origin is at %q", sha, branch, g.originHeads[branch])
	}
	g.pushed[branch] = sha
	g.originHeads[branch] = sha
	return nil
}

func (g *fakeGit) pushes() []fakePush {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]fakePush(nil), g.pushLog...)
}

func (g *fakeGit) RebaseOntoStream(_ context.Context, wt, _, _ string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.rebaseErr != nil {
		return "", g.rebaseErr
	}
	if g.rebasedSHA != "" {
		g.headByWT[wt] = g.rebasedSHA
		return g.rebasedSHA, nil
	}
	return g.headByWT[wt], nil
}

func (g *fakeGit) StreamTip(context.Context, string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.streamTip, g.tipErr
}

func (g *fakeGit) OriginHasHead(_ context.Context, _, _, branch, sha string) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.originHeads[branch] == sha, nil
}

func (g *fakeGit) HasHead(ctx context.Context, repo, branch, sha string) (bool, error) {
	return g.OriginHasHead(ctx, "", repo, branch, sha)
}

func (g *fakeGit) RemoveWorktree(_ context.Context, wt string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.removedWTs = append(g.removedWTs, wt)
	return os.RemoveAll(wt)
}

type fakeWall struct {
	mu        sync.Mutex
	runFn     func(call int, j Job, argv, env []string) (WallRunResult, error)
	testFn    func(j Job, cmd string) (WallRunResult, error)
	calls     int
	lastArgv  []string
	lastEnv   []string
	testCalls []string
}

func (w *fakeWall) RunHarness(_ context.Context, j Job, argv, env []string, _ time.Duration) (WallRunResult, error) {
	w.mu.Lock()
	w.calls++
	call := w.calls
	w.lastArgv = argv
	w.lastEnv = env
	fn := w.runFn
	w.mu.Unlock()
	if rec, err := ReadJobRecord(j.JobDir); err == nil && rec.State == JobRunning {
		rec.PID = 4242
		rec.Nonce = "n4242"
		_ = WriteJobRecord(j.JobDir, rec)
	}
	if fn != nil {
		return fn(call, j, argv, env)
	}
	_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
	_ = os.WriteFile(filepath.Join(j.JobDir, "usage.json"), []byte(`{"micro_usd":950,"priced":true}`), 0o600)
	return WallRunResult{ExitCode: 0, PID: 100 + call, Nonce: "n1", Elapsed: 2 * time.Second}, nil
}

func (w *fakeWall) RunStep(context.Context, Job, []string, time.Duration) (WallRunResult, error) {
	return WallRunResult{}, nil
}

func (w *fakeWall) RunTest(_ context.Context, j Job, cmd string, _ time.Duration) (WallRunResult, error) {
	w.mu.Lock()
	w.testCalls = append(w.testCalls, cmd)
	fn := w.testFn
	w.mu.Unlock()
	if fn != nil {
		return fn(j, cmd)
	}
	return WallRunResult{ExitCode: 0, Output: []byte("ok")}, nil
}

type fakeProcTable struct {
	mu     sync.Mutex
	alive  map[string]bool
	killed []int
}

func (p *fakeProcTable) Alive(pid int, nonce string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.alive[fmt.Sprintf("%d:%s", pid, nonce)]
}

func (p *fakeProcTable) Kill(pid int, nonce string, _ time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.alive, fmt.Sprintf("%d:%s", pid, nonce))
	p.killed = append(p.killed, pid)
	return nil
}

func seedFakeStore_internal(t *testing.T, clock func() time.Time) (*store.Fake, string) {
	t.Helper()
	st := store.NewFake()
	ctx := context.Background()
	row := "amd-1.ds"
	rec, err := st.Apply(ctx, store.Op{
		ID: "setup-1", Epoch: 1, Actor: "rowan", Kind: "setup", At: clock(),
		Writes: []store.Write{
			store.PutStream{Name: "s1", Repo: "mas-bandwidth/nova-sprint", Base: "main", Pos: 1, Forge: "github"},
			store.PutRow{Name: row},
			store.SetRowState{Row: row, State: "up"},
			store.PutCard{
				ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "normal",
				Brief: "brief text", Prose: "implement feature", Paths: []string{"edge/worker"},
				DoneWhen: []string{"go test ./modules/edge/worker/..."},
			},
			store.SetCardState{Card: "p1", State: "ready"},
		},
	})
	if err != nil || !rec.Accepted {
		t.Fatalf("seed setup-1 failed: err=%v reason=%s", err, rec.Reason)
	}
	snap, _ := st.Snapshot(ctx)
	p := card.FromStore(snap.Cards["p1"])
	r := card.RowFromStore(snap.Rows[row], 1, []string{"flash", "pro"})
	s := card.StreamFromStore(snap.Streams["s1"])
	plan, ref := card.Move(p, nil, map[string]card.Row{row: r}, s, nil, card.Start{
		Row: row, Branch: "sprint/s1/p1", Base: testBaseSHA,
	})
	if ref != nil {
		t.Fatalf("Start refused: %s", ref.Reason)
	}
	rec, err = st.Apply(ctx, store.Op{
		ID: "start-p1", Epoch: 1, Actor: "sprint", Kind: "start", At: clock(),
		Expect: plan.Expect, Writes: plan.Writes,
	})
	if err != nil || !rec.Accepted {
		t.Fatalf("apply Start failed: err=%v reason=%s", err, rec.Reason)
	}
	return st, row
}

// Directive 41: nova-config's bench role is the only word on where a worker
// runs; the worker refuses no host, the coordinator's included.
func TestRunnerRunsOnAnyHostTheCoordinatorsToo(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"coord", "coord.local", "b1", ""} {
		if _, err := NewRunner(Runner{Host: host, RootDir: t.TempDir(), Now: testClock_internal(), Sleep: noSleep_internal}); err != nil {
			t.Fatalf("host %q: the worker refused: %v", host, err)
		}
	}
	if err := (Config{Row: "coord", Host: "coord", Harness: "opencode", LookupEnv: func(string) (string, bool) { return gitTokenFixture, true }}).Check(); err != nil {
		t.Fatalf("a worker config on the coordinator's host refused: %v", err)
	}
}

func TestHarnessAdaptersAndWallWrapping(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	j := Job{
		Consumer: "p1.w1", Kind: "work", Row: "amd-1.ds", Stream: "s1",
		Branch: "sprint/s1/p1", BaseSHA: testBaseSHA, Tier: "pro", SlotWeight: 1,
		Model: "deepseek/deepseek-r1", JobDir: dir, WorktreeDir: filepath.Join(dir, "repo"),
		ProxyAddr: "127.0.0.1:19999", Routes: []string{"deepseek"}, Prose: "fix bug",
	}
	ch, _ := NewHarness("claude")
	cArgv, _ := ch.Command(j)
	if !strings.Contains(strings.Join(cArgv, " "), "--disallowedTools Task") {
		t.Fatalf("claude adapter must refuse subagent spawns: %v", cArgv)
	}
	oh, _ := NewHarness("opencode")
	_, oEnv := oh.Command(j)
	envStr := strings.Join(oEnv, "\n")
	if !strings.Contains(envStr, `"baseURL":"http://127.0.0.1:19999/deepseek/v1"`) || !strings.Contains(envStr, `"apiKey":"`+ProxyKey("p1.w1")+`"`) {
		t.Fatalf("opencode env missing proxy wiring: %v", oEnv)
	}
	dirtyEnv := append(oEnv, "NOVA_PG_DSN=postgres://secret", "GH_TOKEN=ghp_secret", "DEEPSEEK_API_KEY=sk-secret")
	clean := SanitizeWallEnv(dirtyEnv, dir)
	for _, kv := range clean {
		if strings.Contains(kv, "postgres://") || strings.Contains(kv, "ghp_secret") || strings.Contains(kv, "sk-secret") {
			t.Fatalf("SanitizeWallEnv leaked secret: %q", kv)
		}
	}
	j.Needs = oh.Needs()
	wallArgv := BuildWallArgv(WallConfig{
		ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox",
		ReadPaths: []string{"/usr", "/home/nova/sdk"}, NetDeny: true,
	}, j, oEnv, cArgv)
	if runtime.GOOS != "windows" && (len(wallArgv) < 3 || wallArgv[0] != "nice" || wallArgv[1] != "-n" || wallArgv[2] != "15") {
		t.Fatalf("wall argv must run the work at nice 15, below CI: %v", wallArgv)
	}
	joined := strings.Join(wallArgv, " ")
	if !strings.Contains(joined, "nova-reserve run --slots 1") || !strings.Contains(joined, "nova-sandbox --write "+dir) || !strings.Contains(joined, "--net-deny --net-allow 127.0.0.1:19999") || strings.Contains(joined, "--unix") {
		t.Fatalf("unexpected wall argv: %s", joined)
	}
}

func TestClassifyOutcomesAndFaultRules(t *testing.T) {
	t.Parallel()
	h := &ClaudeHarness{}
	j := Job{Consumer: "p1.w1", JobDir: t.TempDir()}

	if out := ClassifyJob(h, j, 137, 5*time.Second, 15*time.Second, []byte("killed")); out.Result != "fault" {
		t.Fatalf("exit 137 must be fault, got %+v", out)
	}
	j503 := j
	j503.ProxyStatus = 503
	if out := ClassifyJob(h, j503, 1, 5*time.Second, 15*time.Second, []byte("upstream error")); out.Result != "fault" || !out.Retryable5xx {
		t.Fatalf("503 within 15s grace must be retryable fault, got %+v", out)
	}
	if out := ClassifyJob(h, j503, 1, 25*time.Second, 15*time.Second, []byte("upstream error")); out.Result != "fault" || out.Retryable5xx {
		t.Fatalf("503 past 15s grace must be non-retryable fault, got %+v", out)
	}
	j429 := j
	j429.ProxyStatus = 429
	if out := ClassifyJob(h, j429, 1, 4*time.Second, 15*time.Second, []byte("rate limit")); out.Result != "fault" {
		t.Fatalf("429 rate limit must be fault, got %+v", out)
	}
	if out := ClassifyJob(h, j, 0, 4*time.Second, 15*time.Second, []byte("done")); out.Result != "fail" {
		t.Fatalf("missing result.json must be fail, got %+v", out)
	}
}

// A harness's fail that refutes its brief stays a fail, so the card waits
// held for the coordinator: one that names a path outside the card's paths
// is a brief defect, never the fault that is dealt again with no bounce.
func TestRefutedFailStaysFail(t *testing.T) {
	t.Parallel()
	for _, why := range []string{"brief-defect: the fix needs x.go, outside card paths", "refuted: outside allowed paths nothing fails at the base"} {
		if out := normalizeFileOutcome_internal("fail", why, "", 0, ""); out.Result != "fail" || out.Reason != why {
			t.Fatalf("%q must stay a fail, got %+v", why, out)
		}
	}
	if out := normalizeFileOutcome_internal("fail", "edit outside card paths refused", "", 0, ""); out.Result != "fault" {
		t.Fatalf("a blocked edit with no refutation stays a fault, got %+v", out)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestKeyProxyAllowlistAndKeyInjection(t *testing.T) {
	t.Parallel()
	var seenAuth, seenHost string
	p := &KeyProxy{
		Routes: map[string]ProxyRoute{
			"deepseek": {
				Name:         "deepseek",
				UpstreamURL:  "https://api.deepseek.com",
				AllowedHosts: []string{"api.deepseek.com"},
				Key:          func() (string, error) { return "sk-live-secret-999", nil },
			},
		},
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			seenAuth = r.Header.Get("Authorization")
			seenHost = r.URL.Host
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl-1"}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:19999/v1/chat/completions", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+ProxyPlaceholderKey)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || seenAuth != "Bearer sk-live-secret-999" || seenHost != "api.deepseek.com" {
		t.Fatalf("proxy forward failed: code=%d auth=%q host=%q", rec.Code, seenAuth, seenHost)
	}

	badReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:19999/v1/chat/completions", nil)
	badReq.Header.Set("X-Target-Host", "evil.example.com")
	badRec := httptest.NewRecorder()
	p.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusForbidden || !strings.Contains(badRec.Body.String(), "host evil.example.com is not on policy's egress allowlist") {
		t.Fatalf("expected 403 allowlist refusal, got code=%d body=%q", badRec.Code, badRec.Body.String())
	}
	if err := CheckLoopbackAddr("0.0.0.0:19999"); err == nil {
		t.Fatalf("expected non-loopback address refusal")
	}
}

func TestStoreSprintClientReportRefusalsAndIdempotency(t *testing.T) {
	t.Parallel()
	clock := testClock_internal()
	st, row := seedFakeStore_internal(t, clock)
	git := newFakeGit_internal()
	sc := &StoreSprintClient{Store: st, Origin: git, Now: clock}
	ctx := context.Background()
	taken, err := sc.Take(ctx, row, "p1.w1", 0)
	if err != nil {
		t.Fatalf("Take failed: %v", err)
	}
	_, err = sc.Report(ctx, ReportRequest{Row: "wrong", Consumer: "p1.w1", Gen: taken.Gen, ResultID: "r1", Result: "ok", Head: testWorkSHA})
	if err == nil || !strings.Contains(err.Error(), "held by amd-1.ds, not wrong") {
		t.Fatalf("expected holder refusal, got %v", err)
	}
	_, err = sc.Report(ctx, ReportRequest{Row: row, Consumer: "p1.w1", Gen: taken.Gen, ResultID: "r1", Result: "ok", Head: testWorkSHA})
	if err == nil || !strings.Contains(err.Error(), "not on branch sprint/s1/p1 at origin") {
		t.Fatalf("expected origin refusal, got %v", err)
	}
	// Finish needs only the head on origin (intent rule 32): no PR, no
	// checks; the landing opens the PR and reads its checks.
	git.originHeads["sprint/s1/p1"] = testWorkSHA
	rec1, err := sc.Report(ctx, ReportRequest{Row: row, Consumer: "p1.w1", Gen: taken.Gen, ResultID: "r1", Result: "ok", Head: testWorkSHA, Cost: 500, Priced: true})
	if err != nil || !rec1.Accepted {
		t.Fatalf("expected accepted report, got rec=%+v err=%v", rec1, err)
	}
	rec2, err := sc.Report(ctx, ReportRequest{Row: row, Consumer: "p1.w1", Gen: taken.Gen, ResultID: "r1", Result: "ok", Head: testWorkSHA})
	if err != nil || rec2.Seq != rec1.Seq {
		t.Fatalf("expected idempotent replay at seq %d, got %+v err=%v", rec1.Seq, rec2, err)
	}
	snap, _ := st.Snapshot(ctx)
	if snap.Cards["p1"].State != "review" || snap.Cards["p1"].Head != testWorkSHA || snap.Cards["p1"].Review != 1 {
		t.Fatalf("producer p1 did not advance to review: %+v", snap.Cards["p1"])
	}
}

func TestRunnerProbeAndFullLifecycleWithLaunchRetry(t *testing.T) {
	t.Parallel()
	clock := testClock_internal()
	st, row := seedFakeStore_internal(t, clock)
	git := newFakeGit_internal()
	git.rebasedSHA = testRebSHA
	wall := &fakeWall{}
	sc := &StoreSprintClient{Store: st, Origin: git, Now: clock}
	root := t.TempDir()
	r, err := NewRunner(Runner{
		Host: "b1", RootDir: root, StateDir: root, Sprint: sc, DefaultHarness: &ScriptHarness{},
		Git: git, Wall: wall, Now: clock, Sleep: func(time.Duration) {},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	ctx := context.Background()
	wall.runFn = func(call int, j Job, _, _ []string) (WallRunResult, error) {
		return WallRunResult{ExitCode: 1, Output: []byte("keychain locked"), Elapsed: time.Second}, nil
	}
	if err := r.ProbeRow(ctx, row, "script"); err == nil {
		t.Fatalf("expected probe failure")
	}
	snap, _ := st.Snapshot(ctx)
	if len(snap.Judgements) != 0 {
		t.Fatalf("a failed probe wrote a judgement: %+v", snap.Judgements)
	}
	wall.calls = 0
	wall.runFn = func(call int, j Job, _, _ []string) (WallRunResult, error) {
		if call <= 2 && !strings.HasSuffix(j.Consumer, ".probe") {
			return WallRunResult{ExitCode: 1, Output: []byte(`{"http_status":502}`), Elapsed: 3 * time.Second}, nil
		}
		git.mu.Lock()
		git.headByWT[j.WorktreeDir] = testWorkSHA
		git.mu.Unlock()
		_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
		return WallRunResult{ExitCode: 0, PID: 500, Nonce: "n5", Elapsed: 4 * time.Second}, nil
	}
	if err := r.ProbeRow(ctx, row, "script"); err != nil {
		t.Fatalf("expected probe pass, got %v", err)
	}
	if err := r.StepRow(ctx, row); err != nil {
		t.Fatalf("StepRow failed: %v", err)
	}
	snap, _ = st.Snapshot(ctx)
	if snap.Cards["p1"].State != "review" || snap.Cards["p1"].Head != testRebSHA {
		t.Fatalf("expected p1 in review at rebased head %s, got %+v", testRebSHA, snap.Cards["p1"])
	}
	if _, err := os.Stat(filepath.Join(root, "jobs", "p1.w1")); !os.IsNotExist(err) {
		t.Fatalf("expected job directory cleaned after report and push, got err=%v", err)
	}
}

func TestRunnerRebaseConflictStageFaultAndLeftTree(t *testing.T) {
	t.Parallel()
	clock := testClock_internal()
	st, row := seedFakeStore_internal(t, clock)
	git := newFakeGit_internal()
	git.rebaseErr = &ConflictError{Files: []string{"edge/worker/runner.go"}}
	wall := &fakeWall{
		runFn: func(_ int, j Job, _, _ []string) (WallRunResult, error) {
			git.mu.Lock()
			git.headByWT[j.WorktreeDir] = testWorkSHA
			git.dirtyByWT[j.WorktreeDir] = true
			git.mu.Unlock()
			_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
			return WallRunResult{ExitCode: 0, Elapsed: time.Second}, nil
		},
	}
	sc := &StoreSprintClient{Store: st, Origin: git, Now: clock}
	root := t.TempDir()
	r, _ := NewRunner(Runner{Sleep: noSleep_internal, Host: "b1", RootDir: root, StateDir: root, Sprint: sc, Git: git, Wall: wall, Now: clock})
	if err := r.StepRow(context.Background(), row); err != nil {
		t.Fatalf("StepRow: %v", err)
	}
	snap, _ := st.Snapshot(context.Background())
	p := snap.Cards["p1"]
	if p.State != "ready" || p.Bounces != 1 || p.Head != testCommitSHA || len(git.commits) != 1 {
		t.Fatalf("expected rebase conflict to bounce p1 to ready with pushed head (the left tree, committed by the seal step) kept, got %+v commits %v", p, git.commits)
	}
	if _, err := os.Stat(filepath.Join(root, "jobs", "p1.w1")); !os.IsNotExist(err) {
		t.Fatalf("expected the job directory cleaned once its head is on origin, got %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "jobs", "p1.w1"), 0o755); err != nil { // one left behind
		t.Fatal(err)
	}
	next, _ := sc.Next(context.Background(), row)
	err := r.RunConsumer(context.Background(), next, ConsumerSummary{ID: "p1.w1", Gen: 3})
	if err == nil || !strings.Contains(err.Error(), "already exists: a job directory is never reused") {
		t.Fatalf("expected job directory reuse refusal, got %v", err)
	}
}

func TestRunnerRestartRecovery(t *testing.T) {
	t.Parallel()
	clock := testClock_internal()
	st, row := seedFakeStore_internal(t, clock)
	git := newFakeGit_internal()
	sc := &StoreSprintClient{Store: st, Origin: git, Now: clock}
	ctx := context.Background()
	taken, err := sc.Take(ctx, row, "p1.w1", 0)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	root := t.TempDir()
	jobDir := filepath.Join(root, "jobs", "p1.w1")
	_ = os.MkdirAll(filepath.Join(jobDir, "repo"), 0o755)
	git.headByWT[filepath.Join(jobDir, "repo")] = testPriorSHA

	procs := &fakeProcTable{alive: map[string]bool{"777:nonce-live": true}}
	writeStagedRecord_internal(t, jobDir, JobRecord{
		Epoch: 1, Consumer: "p1.w1", Producer: "p1", Kind: "work", Row: row, Stream: "s1",
		Branch: taken.Branch, BaseSHA: taken.BaseSHA, Gen: taken.Gen, Attempt: 1,
		ResultID: "res-p1.w1-g3", State: JobRunning, PID: 777, Nonce: "nonce-live",
	})
	r1, _ := NewRunner(Runner{Sleep: noSleep_internal, Host: "b1", RootDir: root, Sprint: sc, Git: git, Wall: &fakeWall{}, Procs: procs, Now: clock})
	if err := r1.RecoverRow(ctx, row); err != nil {
		t.Fatalf("RecoverRow live: %v", err)
	}
	if snap, _ := st.Snapshot(ctx); snap.Cards["p1.w1"].State != card.Working {
		t.Fatalf("a live harness (pid 777) must be adopted and left working on restart: %+v", snap.Cards["p1.w1"])
	}

	// Simulate harness death while runner was down, then restart and recover on r2.
	delete(procs.alive, "777:nonce-live")
	r2, _ := NewRunner(Runner{Sleep: noSleep_internal, Host: "b1", RootDir: root, Sprint: sc, Git: git, Wall: &fakeWall{}, Procs: procs, Now: clock})
	if err := r2.RecoverRow(ctx, row); err != nil {
		t.Fatalf("RecoverRow dead: %v", err)
	}
	snap, _ := st.Snapshot(ctx)
	// Defect 73: one the restart cannot resume (no receipt) is released with no blame.
	if snap.Cards["p1"].State != "ready" || snap.Cards["p1"].Bounces != 0 || snap.Cards["p1"].Head != testPriorSHA {
		t.Fatalf("dead harness on restart must release with no blame (0 bounces) and keep partial head %s: %+v", testPriorSHA, snap.Cards["p1"])
	}
	if w := snap.Cards["p1.w1"]; w.Result == "fault" || !strings.HasPrefix(w.Reason, "released: runner restarted") || strings.Contains(w.Reason, "lost") {
		t.Fatalf("restart must release p1.w1 with no fault and no blame: %+v", w)
	}
}

func TestTakeRefusesReservedGroupBranch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := testClock_internal()
	st, row := seedFakeStore_internal(t, clock)
	sprint := &StoreSprintClient{
		Store:  st,
		Origin: newFakeGit_internal(),
		Now:    clock,
	}
	info, err := sprint.Take(ctx, row, "p1.w1", 0)
	if err != nil {
		t.Fatalf("Take(p1.w1): %v", err)
	}
	if info.StreamRef != "sprint/s1/group" || info.Branch != "sprint/s1/p1" {
		t.Fatalf("unexpected StreamRef=%q Branch=%q", info.StreamRef, info.Branch)
	}
	_, err = st.Apply(ctx, store.Op{
		ID: "seed-bad-group", Epoch: 1, Actor: "test", Kind: "seed", At: clock(),
		Writes: []store.Write{
			store.PutCard{ID: "p2", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "normal", Brief: "b", Prose: "p", Paths: []string{"g.go"}, DoneWhen: []string{"true"}},
			store.SetCardState{Card: "p2", State: "ready"},
		},
	})
	if err != nil {
		t.Fatalf("Apply p2 seed: %v", err)
	}
	snap, _ := st.Snapshot(ctx)
	plan, ref := card.Move(
		card.FromStore(snap.Cards["p2"]), nil,
		map[string]card.Row{row: card.RowFromStore(snap.Rows[row], 1, []string{"flash", "pro"})},
		card.StreamFromStore(snap.Streams["s1"]), nil,
		card.Start{Row: row, Branch: "sprint/s1/group", Base: testBaseSHA},
	)
	if ref != nil {
		t.Fatalf("Start p2 refused: %s", ref.Reason)
	}
	if rec, err := st.Apply(ctx, store.Op{ID: "start-p2", Epoch: 1, Actor: "sprint", Kind: "start", At: clock(), Expect: plan.Expect, Writes: plan.Writes}); err != nil || !rec.Accepted {
		t.Fatalf("Apply start-p2 failed: err=%v reason=%s", err, rec.Reason)
	}
	if _, err := sprint.Take(ctx, row, "p2.w1", 0); err == nil || !strings.Contains(err.Error(), "sprint/s1/group") {
		t.Fatalf("expected Take(p2.w1) to be refused for reserved group branch, got %v", err)
	}
}

func TestWallEnvExactAllowlistPerName(t *testing.T) {
	t.Parallel()
	forbidden := []string{
		"DATABASE_URL",
		"SSH_AUTH_SOCK",
		"NOVA_SPRINT_DSN",
		"NOVA_PG_DSN",
		"PGPASSWORD",
		"GITHUB_TOKEN",
		"GH_TOKEN",
		"ANTHROPIC_API_KEY",
		"OPENAI_API_KEY",
		"DEEPSEEK_API_KEY",
		"AWS_SECRET_ACCESS_KEY",
		"CUSTOM_KEY",
		"CUSTOM_TOKEN",
		"UNLISTED_VAR",
	}
	for _, name := range forbidden {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := SanitizeWallEnv([]string{name + "=secret-value", "PATH=/usr/bin", "LANG=C.UTF-8"}, t.TempDir())
			for _, kv := range env {
				if strings.HasPrefix(kv, name+"=") {
					t.Fatalf("SanitizeWallEnv failed to strip %s: got %q", name, kv)
				}
			}
		})
	}
}

// The wall closes AF_UNIX, so MSBuild's out-of-process nodes (named pipes)
// die and a multi-target build fails with no error: under the bench's dotnet
// the wall env builds in one node, and the caller cannot turn it back on.
func TestWallEnvDotnetBuildsInOneNode(t *testing.T) {
	t.Parallel()
	env := SanitizeWallEnv([]string{"PATH=/usr/bin", "DOTNET_CLI_TELEMETRY_OPTOUT=1", "BuildInParallel=true"}, t.TempDir())
	if !slices.Contains(env, "BuildInParallel=false") || slices.Contains(env, "BuildInParallel=true") {
		t.Fatalf("SanitizeWallEnv with the bench's dotnet = %q, want BuildInParallel=false and no other", env)
	}
	if env := SanitizeWallEnv([]string{"PATH=/usr/bin"}, t.TempDir()); slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "BuildInParallel=") }) {
		t.Fatalf("SanitizeWallEnv without the bench's dotnet = %q, want no BuildInParallel", env)
	}
}

type rejectReceiptSprint struct {
	crSprint
	receipt store.Receipt
}

func (s *rejectReceiptSprint) Report(_ context.Context, req ReportRequest) (store.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports = append(s.reports, req)
	return s.receipt, nil
}

func TestReportRequiresAcceptedMatchingReceiptBeforeClean(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sp := &rejectReceiptSprint{
		crSprint: crSprint{next: NextResponse{Epoch: 1, Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 2}}}},
		receipt:  store.Receipt{Accepted: false, Op: "res-e1-p1.r1-a0-g2", Reason: "gen mismatch"},
	}
	r := crRunner_internal(t, sp, &crWall{})
	jobDir := filepath.Join(r.RootDir, "jobs", "p1.r1")
	if err := r.RunConsumer(ctx, sp.next, sp.next.Working[0]); !errors.Is(err, ErrRefused) {
		t.Fatalf("unaccepted receipt must refuse and not clean: got %v", err)
	}
	rec, err := ReadJobRecord(jobDir)
	if err != nil || rec.State != JobReporting {
		t.Fatalf("job directory must remain in %s on unaccepted receipt: rec=%+v err=%v", JobReporting, rec, err)
	}

	// Accepted receipt with wrong Op ID must also refuse to clean.
	sp.receipt = store.Receipt{Accepted: true, Op: "wrong-op-id"}
	if err := r.sendReportAndClean_internal(ctx, jobDir, rec); !errors.Is(err, ErrRefused) {
		t.Fatalf("mismatched receipt Op must refuse and not clean: got %v", err)
	}
	if _, err := os.Stat(jobDir); err != nil {
		t.Fatalf("job directory must remain when receipt.Op != ResultID: %v", err)
	}

	// Nor is an id that only shares the result ID's text as a prefix.
	sp.receipt = store.Receipt{Accepted: true, Op: rec.ResultID + "x"}
	if err := r.sendReportAndClean_internal(ctx, jobDir, rec); !errors.Is(err, ErrRefused) {
		t.Fatalf("a receipt of another op must refuse and not clean: got %v", err)
	}

	// An accepted receipt of the report retried after a refusal (the next
	// link of the result ID's chain) records and cleans.
	sp.receipt = store.Receipt{Accepted: true, Op: store.ConsumerRetryID(rec.ResultID, 7)}
	if err := r.sendReportAndClean_internal(ctx, jobDir, rec); err != nil {
		t.Fatalf("expected clean on accepted matching receipt: %v", err)
	}
	if _, err := os.Stat(jobDir); !os.IsNotExist(err) {
		t.Fatalf("expected job directory removed after matching accepted receipt, got %v", err)
	}
}

func TestRecoveryMatchesFullJobIdentityEpochConsumerAttemptGen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sp := &crSprint{next: NextResponse{
		Epoch: 2, Row: "vision.ds", State: card.Up, Width: 1,
		Working: []ConsumerSummary{{ID: "p1.w1", Kind: "work", Attempt: 2, Gen: 5}},
	}}
	procs := &fakeProcTable{alive: map[string]bool{"888:nonce-stale": true}}
	root := t.TempDir()
	jobDir := filepath.Join(root, "jobs", "p1.w1")
	_ = WriteJobRecord(jobDir, JobRecord{
		Epoch: 1, Consumer: "p1.w1", Producer: "p1", Kind: "work", Row: "vision.ds",
		Attempt: 1, Gen: 3, ResultID: "res-e1-p1.w1-a1-g3", State: JobRunning, PID: 888, Nonce: "nonce-stale",
	})
	r, err := NewRunner(Runner{Now: testClock_internal(), Sleep: noSleep_internal, Host: "vision", RootDir: root, Sprint: sp, Procs: procs, Wall: &crWall{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RecoverRow(ctx, "vision.ds"); err != nil {
		t.Fatalf("RecoverRow: %v", err)
	}
	if len(sp.reported()) > 0 {
		t.Fatalf("mismatched job must be kept for requery, never reported under new gen: %+v", sp.reported())
	}
	diskRec, err := ReadJobRecord(jobDir)
	if err != nil || diskRec.Epoch != 1 || diskRec.Attempt != 1 || diskRec.Gen != 3 {
		t.Fatalf("mismatched job record on disk must preserve its original (epoch=1, attempt=1, gen=3), got %+v err=%v", diskRec, err)
	}
}

func TestRebaseOntoStreamFetchFailurePropagates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fx := edge.NewFakeExec()
	fx.On("git", func(c edge.FakeCall) edge.FakeOutcome {
		switch args := c.Cmd.Argv[gitArg0_internal:]; {
		case args[0] == "config" && args[1] == "--local":
			return edge.FakeOutcome{Stdout: "remote.origin.url\n" + testCloneURL + "\x00"}
		case args[0] == "remote":
			return edge.FakeOutcome{Stdout: testCloneURL + "\n"}
		case args[0] == "fetch":
			return edge.FakeOutcome{ExitCode: 128, Stderr: "fatal: couldn't find remote ref sprint/s1/group"}
		}
		return edge.FakeOutcome{}
	})
	gitOps := &OSGitOps{WorkRoot: t.TempDir(), Exec: fx}
	wt := filepath.Join(t.TempDir(), "repo")
	_, err := gitOps.RebaseOntoStream(ctx, wt, testCloneURL, "sprint/s1/group")
	var fe *FetchError
	if !errors.As(err, &fe) || fe.StreamRef != "sprint/s1/group" {
		t.Fatalf("expected *FetchError from RebaseOntoStream when fetch fails, got %T (%v)", err, err)
	}

	// Verify RunConsumer propagates *FetchError and reports nothing.
	sp := &crSprint{next: NextResponse{Epoch: 1, Row: "vision.ds", State: card.Up, Width: 1, Working: []ConsumerSummary{{ID: "p1.w1", Kind: "work", Gen: 1}}}}
	fGit := newFakeGit_internal()
	fGit.rebaseErr = &FetchError{StreamRef: "sprint/s1/group", Cause: errors.New("network down")}
	wall := &fakeWall{
		runFn: func(_ int, j Job, _, _ []string) (WallRunResult, error) {
			fGit.mu.Lock()
			fGit.headByWT[j.WorktreeDir] = testWorkSHA
			fGit.mu.Unlock()
			_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
			return WallRunResult{ExitCode: 0, PID: 321, Nonce: "n321"}, nil
		},
	}
	r, err := NewRunner(Runner{
		Host: "vision", RootDir: t.TempDir(),
		Sprint: &workTakeSprint{crSprint: sp},
		Git:    fGit, Wall: wall, DefaultHarness: &ScriptHarness{}, Now: testClock_internal(), Sleep: noSleep_internal,
	})
	if err != nil {
		t.Fatal(err)
	}
	runErr := r.RunConsumer(ctx, sp.next, sp.next.Working[0])
	if !errors.As(runErr, &fe) {
		t.Fatalf("expected RunConsumer to propagate *FetchError, got %v", runErr)
	}
	if got := sp.reported(); len(got) > 0 {
		t.Fatalf("fetch failure in RebaseOntoStream must report nothing, got %+v", got)
	}
}

type workTakeSprint struct {
	*crSprint
}

func (s *workTakeSprint) Take(_ context.Context, _, id string, gen int) (TakeInfo, error) {
	return TakeInfo{
		Epoch: 1, Consumer: id, Producer: "p1", Kind: "work", Stream: "s1",
		StreamRef: "sprint/s1/group", Branch: "sprint/s1/p1", BaseSHA: testBaseSHA,
		Tier: "pro", Model: "m1", Deadline: 900, Gen: gen, Attempt: 1,
		CommitName: testCommitName, CommitEmail: testCommitEmail,
	}, nil
}

func TestConcurrentWidthWithdrawStopsRunningHarness(t *testing.T) {
	t.Parallel()
	sp := &crSprint{next: NextResponse{Epoch: 1, Row: "vision.ds", State: card.Up, Width: 2, Working: []ConsumerSummary{{ID: "p1.r1", Kind: "read", Gen: 1}}}}
	started := make(chan struct{})
	canceled := make(chan error, 1)
	wall := &cancelWaitWall{started: started, canceled: canceled}
	r := crRunner_internal(t, sp, wall)

	done := make(chan error, 1)
	go func() {
		done <- r.StepRow(context.Background(), "vision.ds")
	}()
	<-started
	sp.mu.Lock()
	sp.next.Ready = nil
	sp.next.Final = []ConsumerSummary{{ID: "p1.r1", Kind: "read", State: card.Fail, Gen: 2}}
	sp.mu.Unlock()
	if err := r.RecoverRow(context.Background(), "vision.ds"); err != nil {
		t.Fatalf("RecoverRow withdraw: %v", err)
	}
	select {
	case err := <-canceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected running harness ctx canceled on withdraw, got %v", err)
		}
	case <-time.After(2 * time.Second): // unitlint: a hang guard, not a measurement
		t.Fatal("timed out waiting for withdraw to cancel running harness")
	}
	if err := <-done; err != nil {
		t.Fatalf("StepRow after withdraw: %v", err)
	}
}

type cancelWaitWall struct {
	started  chan struct{}
	canceled chan error
}

func (w *cancelWaitWall) RunHarness(ctx context.Context, _ Job, _, _ []string, _ time.Duration) (WallRunResult, error) {
	close(w.started)
	<-ctx.Done()
	w.canceled <- ctx.Err()
	return WallRunResult{ExitCode: -1}, ctx.Err()
}
func (w *cancelWaitWall) RunStep(context.Context, Job, []string, time.Duration) (WallRunResult, error) {
	return WallRunResult{}, nil
}
func (w *cancelWaitWall) RunTest(context.Context, Job, string, time.Duration) (WallRunResult, error) {
	return WallRunResult{}, nil
}

func TestRunnerHooksInAsWorkerBeforeFirstTake(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := testClock_internal()
	st, row := seedFakeStore_internal(t, clock)
	git := newFakeGit_internal()
	sprint := &StoreSprintClient{
		Store:        st,
		Origin:       git,
		RequireHook:  true,
		BusPresent:   map[string]bool{row: false},
		BusRedisAddr: "127.0.0.1:6381",
		Now:          clock,
	}
	if _, err := sprint.Take(ctx, row, "p1.w1", 0); err == nil || !strings.Contains(err.Error(), "hook in first: nova-sprint hook --as "+row+" --role worker") {
		t.Fatalf("expected unhooked take refusal, got %v", err)
	}
	if err := sprint.Hook(ctx, row); err == nil || !strings.Contains(err.Error(), "hook in to the bus first: run NOVA_BUS_REDIS=127.0.0.1:6381 nova-bus hook --as "+row+" as the Monitor tool command, then hook in to the sprint again") {
		t.Fatalf("expected bus-first refusal when not present on bus, got %v", err)
	}
	sprint.HookCalls = 0
	wall := &fakeWall{
		runFn: func(_ int, j Job, _, _ []string) (WallRunResult, error) {
			git.mu.Lock()
			git.headByWT[j.WorktreeDir] = testWorkSHA
			git.mu.Unlock()
			_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
			return WallRunResult{ExitCode: 0, PID: 500, Nonce: "n500", Elapsed: time.Second}, nil
		},
	}
	hook := newFakeWakeHook()
	claim := true
	runner, err := NewRunner(Runner{
		Host: "b1", RootDir: t.TempDir(), Sprint: sprint, Git: git, Wall: wall,
		Now: clock, Sleep: func(time.Duration) {},
		BusHook: func(_ context.Context, row string) (WakeHook, error) {
			sprint.BusPresent[row] = claim // the bus hook has claimed the name
			return hook, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	claim = false
	if err := runner.RunOnce(ctx, row); err == nil || !strings.Contains(err.Error(), "hook in to the bus first") {
		t.Fatalf("a bus hook that did not claim the name must stop the start, got %v", err)
	}
	// The card is in hand from the deal (defect 52); before the hook the
	// row neither acknowledges it nor reports it.
	if snap, _ := st.Snapshot(ctx); snap.Cards["p1.w1"].State != card.Working || snap.Cursors[steps.AckCursor(1, "p1.w1")] != 0 {
		t.Fatalf("the row acted on a card before it was hooked in: %+v %v", snap.Cards["p1.w1"], snap.Cursors)
	}
	claim = true
	if err := runner.RunOnce(ctx, row); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if sprint.HookCalls != 1 || sprint.Hooked[row] != "worker" {
		t.Fatalf("HookCalls = %d, Hooked = %v, want 1 worker hook", sprint.HookCalls, sprint.Hooked)
	}
	if hook.stops() != 2 {
		t.Fatalf("the bus hook was stopped %d times, want once per start", hook.stops())
	}
	snap, _ := st.Snapshot(ctx)
	if snap.Cards["p1.w1"].State != card.Done || snap.Rows[row].Session != "worker:1" || snap.Cards["p1.w1"].BaseSHA != testBaseSHA {
		t.Fatalf("unexpected state after hooked run: card=%+v row=%+v", snap.Cards["p1.w1"], snap.Rows[row])
	}
}

// The seal's step-log note keeps the start of the out-of-PATHS list and
// counts the rest, so a long list is never cut silently.
func TestSealNoteCountsWhatItDrops(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	for i := range 25 {
		fmt.Fprintf(&out, "outside PATHS, not committed: ?? f%02d/\n", i)
	}
	note := sealNote_internal([]byte(out.String()))
	if !strings.HasPrefix(note, "outside PATHS, not committed: ?? f00/") || !strings.HasSuffix(note, "\nand 5 more") || strings.Contains(note, "f20/") {
		t.Fatalf("want the first 20 lines and the rest counted, got:\n%s", note)
	}
	if note := sealNote_internal([]byte("a\nb\n")); note != "a\nb" {
		t.Fatalf("a short list is kept whole, got %q", note)
	}
}
