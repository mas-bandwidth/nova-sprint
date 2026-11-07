package worker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

const (
	testBaseSHA  = "1111111111111111111111111111111111111111"
	testWorkSHA  = "2222222222222222222222222222222222222222"
	testRebSHA   = "3333333333333333333333333333333333333333"
	testPriorSHA = "4444444444444444444444444444444444444444"
)

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
}

func newFakeGit_internal() *fakeGit {
	return &fakeGit{
		headByWT:    map[string]string{},
		pushed:      map[string]string{},
		originHeads: map[string]string{},
		dirtyByWT:   map[string]bool{},
	}
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
		g.headByWT[j.WorktreeDir] = cmpOr_internal(j.PriorHead, j.BaseSHA)
	}
	return g.headByWT[j.WorktreeDir], nil
}

func (g *fakeGit) HeadSHA(_ context.Context, wt string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.headByWT[wt], nil
}

func (g *fakeGit) Push(_ context.Context, _, branch, sha string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pushErr != nil {
		return g.pushErr
	}
	g.pushed[branch] = sha
	g.originHeads[branch] = sha
	return nil
}

func (g *fakeGit) RebaseOntoStream(_ context.Context, wt, _ string) (string, error) {
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

func (g *fakeGit) OriginHasHead(_ context.Context, _, branch, sha string) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.originHeads[branch] == sha, nil
}

func (g *fakeGit) HasHead(ctx context.Context, repo, branch, sha string) (bool, error) {
	return g.OriginHasHead(ctx, repo, branch, sha)
}

func (g *fakeGit) IsDirty(_ context.Context, wt string) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.dirtyByWT[wt], nil
}

func (g *fakeGit) RemoveWorktree(_ context.Context, wt string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.removedWTs = append(g.removedWTs, wt)
	return os.RemoveAll(wt)
}

type fakeForge struct {
	mu      sync.Mutex
	prs     map[string]int
	green   map[string]bool
	openErr error
}

func newFakeForge_internal() *fakeForge {
	return &fakeForge{prs: map[string]int{}, green: map[string]bool{}}
}

func (f *fakeForge) EnsurePR(_ context.Context, _, branch, _, sha, _ string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.openErr != nil {
		return 0, f.openErr
	}
	f.prs[branch+"@"+sha] = 42
	return 42, nil
}

func (f *fakeForge) HasPR(_ context.Context, _, branch, _, sha string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.prs[branch+"@"+sha]
	return ok, nil
}

func (f *fakeForge) ChecksGreen(_ context.Context, _, sha string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.green[sha], nil
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
	if fn != nil {
		return fn(call, j, argv, env)
	}
	_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
	_ = os.WriteFile(filepath.Join(j.JobDir, "usage.json"), []byte(`{"micro_usd":950,"priced":true}`), 0o600)
	return WallRunResult{ExitCode: 0, PID: 100 + call, Nonce: "n1", Elapsed: 2 * time.Second}, nil
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
			store.PutStream{Name: "s1", Repo: "mas-bandwidth/nova-sprint", Base: "main", Pos: 1},
			store.PutRow{Name: row, Kind: "worker", Width: 1, Tiers: []string{"flash", "pro"}, Model: "deepseek-r1", Harness: "opencode"},
			store.SetRowState{Row: row, State: "up"},
			store.PutCard{
				ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "normal",
				Brief: "brief text", Prose: "implement feature", Paths: []string{"edge/worker"},
				DoneWhen: []string{"go test ./edge/worker/..."},
			},
			store.SetCardState{Card: "p1", State: "ready"},
		},
	})
	if err != nil || !rec.Accepted {
		t.Fatalf("seed setup-1 failed: err=%v reason=%s", err, rec.Reason)
	}
	snap, _ := st.Snapshot(ctx)
	p := card.FromStore(snap.Cards["p1"])
	r := card.RowFromStore(snap.Rows[row])
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

func TestCoordinatorHostRefusal(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"studio", "studio.local", "STUDIO.LOCAL.", "coord-box.local"} {
		err := CheckHost(host, []string{"studio", "coord-box"})
		if err == nil || !strings.Contains(err.Error(), "is a coordinator host (no card runs on the coordinator's machine): run nova-worker on a worker bench") {
			t.Fatalf("host %q: expected coordinator refusal, got %v", host, err)
		}
	}
	if err := CheckHost("", nil); err == nil || !strings.Contains(err.Error(), "host is empty") {
		t.Fatalf("expected empty host refusal, got %v", err)
	}
	if err := CheckHost("b1", nil); err != nil {
		t.Fatalf("unexpected refusal on worker bench b1: %v", err)
	}
}

func TestHarnessAdaptersAndWallWrapping(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	j := Job{
		Consumer: "p1.w1", Kind: "work", Row: "amd-1.ds", Stream: "s1",
		Branch: "sprint/s1/p1", BaseSHA: testBaseSHA, Tier: "pro",
		Model: "deepseek/deepseek-r1", JobDir: dir, WorktreeDir: filepath.Join(dir, "repo"),
		ProxyAddr: "127.0.0.1:19999", Prose: "fix bug",
	}
	ch, _ := NewHarness("claude")
	cArgv, _ := ch.Command(j)
	if !strings.Contains(strings.Join(cArgv, " "), "--disallowedTools Task") {
		t.Fatalf("claude adapter must refuse subagent spawns: %v", cArgv)
	}
	oh, _ := NewHarness("opencode")
	_, oEnv := oh.Command(j)
	envStr := strings.Join(oEnv, "\n")
	if !strings.Contains(envStr, "OPENAI_BASE_URL=http://127.0.0.1:19999/v1") || !strings.Contains(envStr, "OPENAI_API_KEY="+ProxyPlaceholderKey) {
		t.Fatalf("opencode env missing proxy wiring: %v", oEnv)
	}
	dirtyEnv := append(oEnv, "NOVA_PG_DSN=postgres://secret", "GH_TOKEN=ghp_secret", "DEEPSEEK_API_KEY=sk-secret")
	clean := SanitizeWallEnv(dirtyEnv, dir)
	for _, kv := range clean {
		if strings.Contains(kv, "postgres://") || strings.Contains(kv, "ghp_secret") || strings.Contains(kv, "sk-secret") {
			t.Fatalf("SanitizeWallEnv leaked secret: %q", kv)
		}
	}
	wallArgv := BuildWallArgv(WallConfig{
		ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox",
		ReadPaths: []string{"/usr", "/home/nova/sdk"}, NetDeny: true,
	}, j, cArgv)
	joined := strings.Join(wallArgv, " ")
	if !strings.Contains(joined, "nova-reserve run --slots 1") || !strings.Contains(joined, "nova-sandbox --write "+dir) || !strings.Contains(joined, "--net-deny --net-allow 127.0.0.1:19999") {
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
	if out := ClassifyJob(h, j, 1, 5*time.Second, 15*time.Second, []byte("HTTP 503 Service Unavailable")); out.Result != "fault" || !out.Retryable5xx {
		t.Fatalf("503 within 15s grace must be retryable fault, got %+v", out)
	}
	if out := ClassifyJob(h, j, 1, 25*time.Second, 15*time.Second, []byte("HTTP 503 Service Unavailable")); out.Result != "fault" || out.Retryable5xx {
		t.Fatalf("503 past 15s grace must be non-retryable fault, got %+v", out)
	}
	if out := ClassifyJob(h, j, 1, 4*time.Second, 15*time.Second, []byte("HTTP 429 rate_limit_exceeded")); out.Result != "fault" {
		t.Fatalf("429 rate limit must be fault, got %+v", out)
	}
	if out := ClassifyJob(h, j, 0, 4*time.Second, 15*time.Second, []byte("done")); out.Result != "fail" {
		t.Fatalf("missing result.json must be fail, got %+v", out)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestKeyProxyAllowlistAndKeyInjection(t *testing.T) {
	t.Parallel()
	var seenAuth, seenHost string
	p := &KeyProxy{
		DefaultRoute: "deepseek",
		Routes: map[string]ProxyRoute{
			"deepseek": {
				Name:         "deepseek",
				UpstreamURL:  "https://api.deepseek.com",
				AllowedHosts: []string{"api.deepseek.com"},
				Key:          func() string { return "sk-live-secret-999" },
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
	forge := newFakeForge_internal()
	sc := &StoreSprintClient{
		Store: st, Origin: git, Forge: forge,
		RequirePR: map[string]bool{"s1": true}, RequireGreenAtReport: true, Now: clock,
	}
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
	git.originHeads["sprint/s1/p1"] = testWorkSHA
	_, err = sc.Report(ctx, ReportRequest{Row: row, Consumer: "p1.w1", Gen: taken.Gen, ResultID: "r1", Result: "ok", Head: testWorkSHA})
	if err == nil || !strings.Contains(err.Error(), "requires a pull request") {
		t.Fatalf("expected PR refusal, got %v", err)
	}
	forge.prs["sprint/s1/p1@"+testWorkSHA] = 7
	_, err = sc.Report(ctx, ReportRequest{Row: row, Consumer: "p1.w1", Gen: taken.Gen, ResultID: "r1", Result: "ok", Head: testWorkSHA})
	if err == nil || !strings.Contains(err.Error(), "checks are not green") {
		t.Fatalf("expected checks green refusal, got %v", err)
	}
	forge.green[testWorkSHA] = true
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
	forge := newFakeForge_internal()
	wall := &fakeWall{}
	sc := &StoreSprintClient{Store: st, Origin: git, Forge: forge, RequirePR: map[string]bool{"s1": true}, Now: clock}
	root := t.TempDir()
	r, err := NewRunner(Runner{
		Host: "b1", RootDir: root, StateDir: root, Sprint: sc,
		Git: git, Forge: forge, Wall: wall, Now: clock,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	ctx := context.Background()
	wall.runFn = func(call int, j Job, _, _ []string) (WallRunResult, error) {
		return WallRunResult{ExitCode: 1, Output: []byte("keychain locked"), Elapsed: time.Second}, nil
	}
	if err := r.ProbeRow(ctx, row, "opencode", "deepseek-r1"); err == nil {
		t.Fatalf("expected probe failure")
	}
	snap, _ := st.Snapshot(ctx)
	if snap.Judgements["j-probe-"+row].State != "open" {
		t.Fatalf("expected open probe judgement, got %+v", snap.Judgements)
	}
	wall.calls = 0
	wall.runFn = func(call int, j Job, _, _ []string) (WallRunResult, error) {
		if call <= 2 && !strings.HasSuffix(j.Consumer, ".probe") {
			return WallRunResult{ExitCode: 1, Output: []byte("HTTP 502 Bad Gateway"), Elapsed: 3 * time.Second}, nil
		}
		git.mu.Lock()
		git.headByWT[j.WorktreeDir] = testWorkSHA
		git.mu.Unlock()
		_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
		return WallRunResult{ExitCode: 0, PID: 500, Nonce: "n5", Elapsed: 4 * time.Second}, nil
	}
	if err := r.ProbeRow(ctx, row, "opencode", "deepseek-r1"); err != nil {
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

func TestRunnerRebaseConflictStageFaultAndDirtyKeep(t *testing.T) {
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
	r, _ := NewRunner(Runner{Host: "b1", RootDir: root, StateDir: root, Sprint: sc, Git: git, Wall: wall, Now: clock})
	if err := r.StepRow(context.Background(), row); err != nil {
		t.Fatalf("StepRow: %v", err)
	}
	snap, _ := st.Snapshot(context.Background())
	p := snap.Cards["p1"]
	if p.State != "ready" || p.Bounces != 1 || p.Head != testWorkSHA {
		t.Fatalf("expected rebase conflict to bounce p1 to ready with pushed head kept, got %+v", p)
	}
	if _, err := os.Stat(filepath.Join(root, "jobs", "p1.w1")); err != nil {
		t.Fatalf("expected dirty worktree kept on disk, got %v", err)
	}
	next, _ := sc.Next(context.Background(), row)
	err := r.RunConsumer(context.Background(), next, ConsumerSummary{ID: "p1.w1", Gen: 3})
	if err == nil || !strings.Contains(err.Error(), "already exists: a job directory is never reused") {
		t.Fatalf("expected job directory reuse refusal, got %v", err)
	}
}

func TestRunnerRestartRecoveryAndQueryEdgeIdempotency(t *testing.T) {
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
	_ = WriteJobRecord(jobDir, JobRecord{
		Consumer: "p1.w1", Producer: "p1", Kind: "work", Row: row, Stream: "s1",
		Branch: taken.Branch, BaseSHA: taken.BaseSHA, Gen: taken.Gen, Attempt: 1,
		ResultID: "res-p1.w1-g3", State: JobRunning, PID: 777, Nonce: "nonce-live",
	})
	r1, _ := NewRunner(Runner{Host: "b1", RootDir: root, Sprint: sc, Git: git, Procs: procs, Now: clock})
	if err := r1.RecoverRow(ctx, row); err != nil {
		t.Fatalf("RecoverRow live: %v", err)
	}
	if _, ok := r1.adopted["p1.w1"]; !ok {
		t.Fatalf("expected live pid 777 adopted on restart")
	}

	// Simulate harness death while runner was down, then restart with a brand-new Runner struct.
	delete(procs.alive, "777:nonce-live")
	r2, _ := NewRunner(Runner{Host: "b1", RootDir: root, Sprint: sc, Git: git, Procs: procs, Now: clock})
	req := EdgeRequest{Epoch: 1, Kind: "runner", Card: "p1.w1", SHA: testPriorSHA, Row: row, After: []string{"start-p1"}}
	res1, err := r2.QueryEdge(ctx, req)
	if err != nil {
		t.Fatalf("QueryEdge 1: %v", err)
	}
	// Create a third brand-new Runner struct and query the exact same (epoch, kind, card, sha).
	r3, _ := NewRunner(Runner{Host: "b1", RootDir: root, Sprint: sc, Git: git, Procs: procs, Now: clock})
	res2, err := r3.QueryEdge(ctx, req)
	if err != nil {
		t.Fatalf("QueryEdge 2: %v", err)
	}
	if res1.ID != res2.ID || res1.Epoch != 1 || res1.Outcome != res2.Outcome || res1.SHA != res2.SHA || res1.Outcome != "done" || res1.SHA != testPriorSHA {
		t.Fatalf("QueryEdge not idempotent across Runner restart: res1=%+v res2=%+v", res1, res2)
	}
	assertEpochSeparatedQueryEdge_internal(t, ctx, r3, req, res1)
	snap, _ := st.Snapshot(ctx)
	if snap.Cards["p1"].State != "ready" || snap.Cards["p1"].Bounces != 0 || snap.Cards["p1"].Head != testPriorSHA {
		t.Fatalf("dead harness on restart must report fault (0 bounces) and keep partial head %s: %+v", testPriorSHA, snap.Cards["p1"])
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
		Forge:  newFakeForge_internal(),
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
		map[string]card.Row{row: card.RowFromStore(snap.Rows[row])},
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

func assertEpochSeparatedQueryEdge_internal(t *testing.T, ctx context.Context, r *Runner, req EdgeRequest, res1 EdgeResult) {
	t.Helper()
	reqEpoch2 := req
	reqEpoch2.Epoch = 2
	resEpoch2, err := r.QueryEdge(ctx, reqEpoch2)
	if err != nil {
		t.Fatalf("QueryEdge epoch 2: %v", err)
	}
	if resEpoch2.ID == res1.ID || resEpoch2.Epoch != 2 || resEpoch2.ID != RequestID(2, "runner", "p1.w1", testPriorSHA) {
		t.Fatalf("QueryEdge epoch 2 must have distinct RequestID keyed on epoch: res1=%+v resEpoch2=%+v", res1, resEpoch2)
	}
}
