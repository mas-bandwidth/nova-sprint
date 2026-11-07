package worker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/store"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var ErrNoSlot = errors.New("worker: no nova-reserve slot available within wait")

type ConflictError struct{ Files []string }

func (e *ConflictError) Error() string { return "rebase conflict: " + strings.Join(e.Files, ", ") }

type FetchError struct {
	StreamRef string
	Cause     error
}

func (e *FetchError) Error() string { return fmt.Sprintf("fetch %s failed: %v", e.StreamRef, e.Cause) }
func (e *FetchError) Unwrap() error { return e.Cause }

type GitOps interface {
	StageWorktree(ctx context.Context, j Job) (headSHA string, err error)
	HeadSHA(ctx context.Context, worktree string) (string, error)
	Push(ctx context.Context, worktree, branch, headSHA string) error
	RebaseOntoStream(ctx context.Context, worktree, streamRef string) (newSHA string, err error)
	OriginHasHead(ctx context.Context, repo, branch, sha string) (bool, error)
	IsDirty(ctx context.Context, worktree string) (bool, error)
	RemoveWorktree(ctx context.Context, worktree string) error
}
type ForgeOps interface {
	EnsurePR(ctx context.Context, repo, branch, base, headSHA, title string) (int, error)
}
type ForgeAdapter struct{ Client forge.Forge }

func (a *ForgeAdapter) EnsurePR(ctx context.Context, repo, branch, base, headSHA, title string) (int, error) {
	if a == nil || a.Client == nil {
		return 0, errors.New("forge client not configured")
	}
	info, err := a.Client.OpenPR(ctx, repo, branch, base, title, headSHA)
	return info.Number, err
}

type WallRunResult struct {
	ExitCode, PID int
	Output        []byte
	Nonce         string
	Elapsed       time.Duration
}
type ReserveWall interface {
	RunHarness(ctx context.Context, j Job, argv, env []string, timeout time.Duration) (WallRunResult, error)
	RunTest(ctx context.Context, j Job, cmd string, timeout time.Duration) (WallRunResult, error)
}
type ProcessTable interface {
	Alive(pid int, nonce string) bool
	Kill(pid int, nonce string, grace time.Duration) error
}
type OSProcessTable struct{}

func (OSProcessTable) Alive(pid int, _ string) bool {
	return pid > 0 && pid != os.Getpid() && syscall.Kill(pid, 0) == nil
}
func (p OSProcessTable) Kill(pid int, nonce string, grace time.Duration) error {
	if pid <= 0 || pid == os.Getpid() {
		return nil
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	for deadline := time.Now().Add(grace); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		if !p.Alive(pid, nonce) {
			return nil
		}
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}

type Runner struct {
	Host, RootDir, StateDir, ProxyAddr string
	CoordinatorHosts                   []string
	Sprint                             SprintClient
	Git                                GitOps
	Forge                              ForgeOps
	Wall                               ReserveWall
	Procs                              ProcessTable
	Proxy                              *KeyProxy
	Exec                               edge.Exec
	Harnesses                          map[string]Harness
	DefaultHarness                     Harness
	Timeouts                           Timeouts
	Logger                             *StepLogger
	Now                                func() time.Time
	Sleep                              func(time.Duration)
	mu                                 *sync.Mutex
	probed, withdrawn                  map[string]bool
	adopted                            map[string]JobRecord
	running                            map[string]context.CancelFunc
	dedup                              *edge.Deduplicator
}

func NewRunner(cfg Runner) (*Runner, error) {
	if err := CheckHost(cfg.Host, cfg.CoordinatorHosts); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.RootDir) == "" {
		return nil, errors.New("worker refused: root directory is empty: pass --root <dir>")
	}
	cfg.Timeouts = cfg.Timeouts.WithDefaults()
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Unix(1000, 0).UTC() }
	}
	if cfg.Sleep == nil {
		cfg.Sleep = func(time.Duration) {}
	}
	cfg.Procs = cmp.Or[ProcessTable](cfg.Procs, OSProcessTable{})
	cfg.Wall = cmp.Or[ReserveWall](cfg.Wall, &OSReserveWall{UseSandbox: true, Exec: cfg.Exec, Config: WallConfig{ReserveWait: cfg.Timeouts.ReserveWait, NetDeny: true}})
	if cfg.Logger == nil && cfg.StateDir != "" {
		cfg.Logger = &StepLogger{StateDir: cfg.StateDir, Now: cfg.Now}
	}
	cfg.mu, cfg.probed, cfg.withdrawn = &sync.Mutex{}, map[string]bool{}, map[string]bool{}
	cfg.adopted, cfg.running, cfg.dedup = map[string]JobRecord{}, map[string]context.CancelFunc{}, edge.NewDeduplicator()
	return &cfg, nil
}
func (r *Runner) harnessFor_internal(name string) (Harness, error) {
	if name != "" && r.Harnesses != nil {
		if h, ok := r.Harnesses[name]; ok {
			return h, nil
		}
	}
	if r.DefaultHarness != nil {
		return r.DefaultHarness, nil
	}
	return NewHarness(cmp.Or(name, "claude"))
}
func (r *Runner) logStep_internal(row, consumer, step, res string, d time.Duration) {
	if r.Logger != nil {
		_ = r.Logger.Log(row, consumer, step, res, d)
	}
}
func (r *Runner) ProbeRow(ctx context.Context, row, harnessName, model string) error {
	h, err := r.harnessFor_internal(harnessName)
	if err != nil {
		return err
	}
	probeDir := filepath.Join(r.RootDir, "probes", row)
	_ = os.RemoveAll(probeDir)
	if err := os.MkdirAll(filepath.Join(probeDir, "repo"), 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(probeDir)
	pj := h.Probe(Job{
		Consumer: row + ".probe", Row: row, Kind: "work", Tier: "flash",
		Model: model, HarnessName: h.Name(), JobDir: probeDir,
		WorktreeDir: filepath.Join(probeDir, "repo"), ProxyAddr: r.ProxyAddr,
	})
	pj.JobDir, pj.WorktreeDir = probeDir, filepath.Join(probeDir, "repo")
	argv, env := h.Command(pj)
	res, runErr := r.Wall.RunHarness(ctx, pj, argv, env, r.Timeouts.LaunchGrace)
	out := ClassifyJob(h, pj, res.ExitCode, res.Elapsed, r.Timeouts.LaunchGrace, res.Output)
	if runErr != nil || out.Result != "ok" {
		r.mu.Lock()
		r.probed[row] = false
		r.mu.Unlock()
		lastLine := lastNonEmptyLine_internal(string(res.Output), out.Reason)
		if runErr != nil && lastLine == "" {
			lastLine = runErr.Error()
		}
		_ = r.Sprint.OpenJudgement(ctx, store.Judgement{
			ID: "j-probe-" + row, Kind: "probe", About: row, Group: "probe:" + row,
			Question: fmt.Sprintf("harness probe failed on %s: %s", row, lastLine),
			Command:  fmt.Sprintf("nova-sprint judge j-probe-%s retry --reason fixed", row),
		})
		return fmt.Errorf("worker refused: probe failed for row %s: %s: fix the harness or credentials and run nova-worker again", row, lastLine)
	}
	r.mu.Lock()
	r.probed[row] = true
	r.mu.Unlock()
	r.logStep_internal(row, pj.Consumer, "probe", "ok", res.Elapsed)
	return nil
}
func lastNonEmptyLine_internal(s, fallback string) string {
	for _, line := range slices.Backward(strings.Split(strings.TrimSpace(s), "\n")) {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return fallback
}
func jobMatchesSummary_internal(next NextResponse, w ConsumerSummary, rec JobRecord) bool {
	return rec.Consumer == w.ID && (rec.Epoch == 0 || next.Epoch == 0 || rec.Epoch == next.Epoch) &&
		(rec.Attempt == 0 || w.Attempt == 0 || rec.Attempt == w.Attempt) && rec.Gen == w.Gen
}
func (r *Runner) RecoverRow(ctx context.Context, row string) error {
	next, err := r.Sprint.Next(ctx, row)
	if err != nil {
		return err
	}
	disk, err := r.loadRowJobs_internal(row)
	if err != nil {
		return err
	}
	workingByID, finalByID := map[string]ConsumerSummary{}, map[string]ConsumerSummary{}
	for _, w := range next.Working {
		workingByID[w.ID] = w
	}
	for _, f := range next.Final {
		finalByID[f.ID] = f
		if f.State == card.Fail {
			r.cancelRunning_internal(f.ID)
		}
	}
	for id, rec := range disk {
		if f, isFinal := finalByID[id]; isFinal {
			if f.State == card.Fail && rec.State == JobRunning {
				r.withdrawRunningJob_internal(ctx, rec)
			}
			_ = r.cleanJobDir_internal(ctx, filepath.Join(r.RootDir, "jobs", id), rec)
			continue
		}
		if w, isWorking := workingByID[id]; isWorking && !r.isRunningInFlight_internal(id) {
			if !jobMatchesSummary_internal(next, w, rec) {
				r.logStep_internal(row, id, "recover", "identity-mismatch-kept", 0)
				continue
			}
			if err := r.recoverWorkingJob_internal(ctx, w, rec); err != nil && !errors.Is(err, ErrStoreDown) {
				return err
			}
		}
	}
	for _, w := range next.Working {
		if _, onDisk := disk[w.ID]; !onDisk && !r.isRunningInFlight_internal(w.ID) {
			if err := r.reportLostJob_internal(ctx, next.Epoch, row, w); err != nil && !errors.Is(err, ErrStoreDown) {
				return err
			}
		}
	}
	return nil
}
func (r *Runner) loadRowJobs_internal(row string) (map[string]JobRecord, error) {
	jobsRoot := filepath.Join(r.RootDir, "jobs")
	entries, err := os.ReadDir(jobsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]JobRecord{}, nil
		}
		return nil, err
	}
	out := map[string]JobRecord{}
	for _, e := range entries {
		if e.IsDir() {
			if rec, err := ReadJobRecord(filepath.Join(jobsRoot, e.Name())); err == nil && rec.Row == row {
				out[rec.Consumer] = rec
			}
		}
	}
	return out, nil
}
func (r *Runner) recoverWorkingJob_internal(ctx context.Context, w ConsumerSummary, rec JobRecord) error {
	jobDir := filepath.Join(r.RootDir, "jobs", w.ID)
	if rec.State == JobHarvested && rec.Outcome.Result == "ok" && rec.Kind == "work" {
		job := Job{
			Epoch: rec.Epoch, Consumer: rec.Consumer, Producer: rec.Producer, Kind: rec.Kind,
			Row: rec.Row, Stream: rec.Stream, Repo: rec.Repo, Base: rec.Base,
			StreamRef: rec.StreamRef, Branch: rec.Branch, BaseSHA: rec.BaseSHA,
			PriorHead: rec.PriorHead, Head: rec.TargetHead, DoneWhen: rec.DoneWhen,
			Gen: rec.Gen, Attempt: rec.Attempt, JobDir: jobDir,
			WorktreeDir: filepath.Join(jobDir, "repo"), RequirePR: rec.RequirePR,
		}
		if err := r.rebaseAndPRSteps_internal(ctx, job, &rec); err != nil {
			return err
		}
		return r.sendReportAndClean_internal(ctx, jobDir, rec)
	}
	if rec.State == JobReporting || rec.State == JobRebased || (rec.ResultID != "" && rec.Outcome.Result != "") {
		return r.sendReportAndClean_internal(ctx, jobDir, rec)
	}
	if rec.State == JobRunning && r.Procs != nil && r.Procs.Alive(rec.PID, rec.Nonce) {
		r.mu.Lock()
		r.adopted[w.ID] = rec
		r.mu.Unlock()
		r.logStep_internal(rec.Row, w.ID, "adopt", fmt.Sprintf("pid=%d", rec.PID), 0)
		return nil
	}
	rec.PushedHead = cmp.Or(r.harvestPartialPush_internal(ctx, jobDir, rec), rec.PushedHead)
	rec.ResultID = cmp.Or(rec.ResultID, FormatResultID(rec.Epoch, w.ID, rec.Attempt, rec.Gen))
	rec.Outcome = Outcome{Result: "fault", Head: rec.PushedHead, Reason: "restart: harness exited while runner was down"}
	rec.State = JobReporting
	if err := WriteJobRecord(jobDir, rec); err != nil {
		return err
	}
	return r.sendReportAndClean_internal(ctx, jobDir, rec)
}
func (r *Runner) reportLostJob_internal(ctx context.Context, epoch int, row string, w ConsumerSummary) error {
	_, err := r.Sprint.Report(ctx, ReportRequest{
		Epoch: epoch, Row: row, Consumer: w.ID, Gen: w.Gen,
		ResultID: FormatResultID(epoch, w.ID, w.Attempt, w.Gen) + "-lost",
		Result:   "fault", Reason: "runner lost the job",
	})
	r.logStep_internal(row, w.ID, "lost-job", "fault", 0)
	return err
}
func (r *Runner) withdrawRunningJob_internal(ctx context.Context, rec JobRecord) {
	r.cancelRunning_internal(rec.Consumer)
	if r.Procs != nil && rec.PID > 0 {
		_ = r.Procs.Kill(rec.PID, rec.Nonce, r.Timeouts.TerminateGrace)
	}
	jobDir := filepath.Join(r.RootDir, "jobs", rec.Consumer)
	if pushed := r.harvestPartialPush_internal(ctx, jobDir, rec); pushed != "" {
		rec.PushedHead = pushed
		_ = WriteJobRecord(jobDir, rec)
	}
	r.mu.Lock()
	delete(r.adopted, rec.Consumer)
	r.mu.Unlock()
	r.logStep_internal(rec.Row, rec.Consumer, "withdraw", "stopped", 0)
}
func (r *Runner) harvestPartialPush_internal(ctx context.Context, jobDir string, rec JobRecord) string {
	if rec.Kind != "work" || r.Git == nil {
		return ""
	}
	wt := filepath.Join(jobDir, "repo")
	sha, err := r.Git.HeadSHA(ctx, wt)
	if err != nil || sha == "" || sha == rec.BaseSHA || sha == rec.PriorHead || r.Git.Push(ctx, wt, rec.Branch, sha) != nil {
		return ""
	}
	return sha
}
func (r *Runner) tryBeginConsumer_internal(id string, cancel context.CancelFunc) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, busy := r.running[id]; busy {
		return false
	}
	delete(r.withdrawn, id)
	r.running[id] = cancel
	return true
}
func (r *Runner) endConsumer_internal(id string) { r.mu.Lock(); delete(r.running, id); r.mu.Unlock() }
func (r *Runner) isRunningInFlight_internal(id string) bool {
	r.mu.Lock()
	_, b := r.running[id]
	r.mu.Unlock()
	return b
}
func (r *Runner) wasWithdrawn_internal(id string) bool {
	r.mu.Lock()
	w := r.withdrawn[id]
	r.mu.Unlock()
	return w
}
func (r *Runner) cancelRunning_internal(id string) {
	r.mu.Lock()
	cancel, ok := r.running[id]
	if ok {
		r.withdrawn[id] = true
	}
	r.mu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
}
func (r *Runner) StepRow(ctx context.Context, row string) error {
	if err := r.RecoverRow(ctx, row); err != nil {
		if errors.Is(err, ErrStoreDown) {
			return nil
		}
		return err
	}
	next, err := r.Sprint.Next(ctx, row)
	if err != nil || next.State != card.Up || next.Held {
		if errors.Is(err, ErrStoreDown) {
			return nil
		}
		return err
	}
	workingCount := len(next.Working)
	var toLaunch []ConsumerSummary
	for _, c := range next.Ready {
		if workingCount >= next.Width {
			break
		}
		if !r.isRunningInFlight_internal(c.ID) {
			workingCount++
			toLaunch = append(toLaunch, c)
		}
	}
	if len(toLaunch) == 0 {
		return nil
	}
	if len(toLaunch) == 1 {
		if err := r.RunConsumer(ctx, next, toLaunch[0]); !errors.Is(err, ErrNoSlot) && !errors.Is(err, ErrStoreDown) {
			return err
		}
		return nil
	}
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error
	for _, c := range toLaunch {
		c := c
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.RunConsumer(ctx, next, c); err != nil && !errors.Is(err, ErrNoSlot) && !errors.Is(err, ErrStoreDown) {
				errMu.Lock()
				firstErr = cmp.Or(firstErr, err)
				errMu.Unlock()
			}
		}()
	}
	wg.Wait()
	return firstErr
}
func (r *Runner) RunConsumer(ctx context.Context, rowInfo NextResponse, c ConsumerSummary) error {
	runCtx, cancel := context.WithCancel(ctx)
	if !r.tryBeginConsumer_internal(c.ID, cancel) {
		cancel()
		return nil
	}
	defer func() { r.endConsumer_internal(c.ID); cancel() }()
	jobDir := filepath.Join(r.RootDir, "jobs", c.ID)
	if _, err := os.Stat(jobDir); err == nil {
		return fmt.Errorf("worker refused: job directory %s already exists: a job directory is never reused", jobDir)
	}
	info, err := r.Sprint.Take(runCtx, rowInfo.Row, c.ID, c.Gen)
	if err != nil {
		return err
	}
	job, rec, err := r.initJob_internal(rowInfo, info, jobDir)
	if err != nil {
		return err
	}
	if err := r.stageStep_internal(runCtx, &job, &rec); err != nil {
		return r.sendReportAndClean_internal(runCtx, jobDir, rec)
	}
	res, err := r.runStep_internal(runCtx, job, &rec)
	if r.wasWithdrawn_internal(c.ID) {
		r.withdrawRunningJob_internal(ctx, rec)
		_ = r.cleanJobDir_internal(ctx, jobDir, rec)
		return nil
	}
	if errors.Is(err, ErrNoSlot) {
		return ErrNoSlot
	}
	if err := r.harvestStep_internal(runCtx, job, &rec, res, err); err != nil {
		return err
	}
	if err := r.rebaseAndPRSteps_internal(runCtx, job, &rec); err != nil {
		return err
	}
	return r.sendReportAndClean_internal(runCtx, jobDir, rec)
}
func (r *Runner) initJob_internal(rowInfo NextResponse, info TakeInfo, jobDir string) (Job, JobRecord, error) {
	for _, sub := range []string{"home", "tmp", "data"} {
		if err := os.MkdirAll(filepath.Join(jobDir, sub), 0o755); err != nil && sub == "home" {
			return Job{}, JobRecord{}, err
		}
	}
	wt := filepath.Join(jobDir, "repo")
	now := r.Now().UTC()
	dl := now.Add(r.Timeouts.DeadlineFor(info.Tier) + r.Timeouts.CIWaitExtra)
	epoch := cmp.Or(info.Epoch, rowInfo.Epoch)
	job := Job{
		Epoch: epoch, Consumer: info.Consumer, Producer: info.Producer, Kind: info.Kind,
		Row: rowInfo.Row, Stream: info.Stream, Repo: info.Repo, Base: info.Base,
		StreamRef: info.StreamRef, Branch: info.Branch, BaseSHA: info.BaseSHA,
		PriorHead: info.PriorHead, Head: info.Head, Tier: info.Tier,
		Model: rowInfo.Model, HarnessName: rowInfo.Harness, Brief: info.Brief,
		Prose: info.Prose, Finding: info.Finding, Paths: info.Paths,
		DoneWhen: info.DoneWhen, Gen: info.Gen, Attempt: info.Attempt,
		Review: info.Review, JobDir: jobDir, WorktreeDir: wt,
		ProxyAddr: r.ProxyAddr, CommitName: rowInfo.Row,
		CommitEmail: rowInfo.Row + "@nova-sprint.local", RequirePR: info.RequirePR,
	}
	rec := JobRecord{
		Epoch: epoch, Consumer: info.Consumer, Producer: info.Producer, Kind: info.Kind,
		Row: rowInfo.Row, Stream: info.Stream, Repo: info.Repo, Base: info.Base,
		StreamRef: info.StreamRef, Branch: info.Branch, BaseSHA: info.BaseSHA,
		PriorHead: info.PriorHead, TargetHead: info.Head, DoneWhen: cloneStrings_internal(info.DoneWhen),
		Gen: info.Gen, Attempt: info.Attempt, ResultID: FormatResultID(epoch, info.Consumer, info.Attempt, info.Gen),
		State: JobTaken, StartedAt: now.Unix(), DeadlineAt: dl.Unix(), RequirePR: info.RequirePR,
	}
	if err := WriteJobRecord(jobDir, rec); err != nil {
		return Job{}, JobRecord{}, err
	}
	r.logStep_internal(rowInfo.Row, info.Consumer, "take", "ok", 0)
	return job, rec, nil
}
func (r *Runner) stageStep_internal(ctx context.Context, job *Job, rec *JobRecord) error {
	start := r.Now()
	if r.Git != nil {
		stageCtx, cancel := context.WithTimeout(ctx, r.Timeouts.Stage)
		_, err := r.Git.StageWorktree(stageCtx, *job)
		cancel()
		if err != nil {
			rec.State, rec.Outcome = JobReporting, Outcome{Result: "fault", Reason: "staging fault: " + err.Error()}
			_ = WriteJobRecord(job.JobDir, *rec)
			r.logStep_internal(job.Row, job.Consumer, "stage", "fault", r.Now().Sub(start))
			return err
		}
	}
	rec.State = JobStaged
	_ = WriteJobRecord(job.JobDir, *rec)
	r.logStep_internal(job.Row, job.Consumer, "stage", "ok", r.Now().Sub(start))
	return nil
}
func (r *Runner) launchBackoff_internal(attempt int) time.Duration {
	lo, hi := r.Timeouts.LaunchBackoff1Min, r.Timeouts.LaunchBackoff1Max
	if attempt >= 2 {
		lo, hi = r.Timeouts.LaunchBackoff2Min, r.Timeouts.LaunchBackoff2Max
	}
	if hi <= lo {
		return lo
	}
	jitter := time.Duration(r.Now().UnixNano() % int64(hi-lo))
	if jitter < 0 {
		jitter = -jitter
	}
	return lo + jitter
}
func (r *Runner) runStep_internal(ctx context.Context, job Job, rec *JobRecord) (WallRunResult, error) {
	h, err := r.harnessFor_internal(job.HarnessName)
	if err != nil {
		return WallRunResult{}, err
	}
	argv, env := h.Command(job)
	rec.State = JobRunning
	if rec.PID <= 0 {
		rec.PID = os.Getpid()
		rec.Nonce = fmt.Sprintf("n-%d-%d", rec.PID, r.Now().UnixNano())
	}
	_ = WriteJobRecord(job.JobDir, *rec)
	tries := r.Timeouts.LaunchTries
	var last WallRunResult
	var runErr error
	for attempt := 1; attempt <= tries; attempt++ {
		last, runErr = r.Wall.RunHarness(ctx, job, argv, env, r.Timeouts.DeadlineFor(job.Tier))
		if errors.Is(runErr, ErrNoSlot) {
			return last, ErrNoSlot
		}
		if last.PID > 0 {
			rec.PID, rec.Nonce = last.PID, cmp.Or(last.Nonce, rec.Nonce)
			_ = WriteJobRecord(job.JobDir, *rec)
		}
		_ = os.WriteFile(filepath.Join(job.JobDir, "harness.log"), last.Output, 0o600)
		out := ClassifyJob(h, job, last.ExitCode, last.Elapsed, r.Timeouts.LaunchGrace, last.Output)
		if !out.Retryable5xx || attempt == tries {
			break
		}
		r.Sleep(r.launchBackoff_internal(attempt))
	}
	r.logStep_internal(job.Row, job.Consumer, "run", fmt.Sprintf("exit=%d", last.ExitCode), last.Elapsed)
	return last, runErr
}
func isInfraFault_internal(res WallRunResult, runErr error) (bool, string) {
	if strings.Contains(string(res.Output), "SANDBOX REFUSED") || res.ExitCode == 125 || res.ExitCode == 126 || res.ExitCode == 127 {
		return true, fmt.Sprintf("wall/binary fault (exit %d): %s", res.ExitCode, TailBytes(res.Output, 512))
	}
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return true, "wall execution fault: " + runErr.Error()
	}
	return false, ""
}
func (r *Runner) harvestStep_internal(ctx context.Context, job Job, rec *JobRecord, res WallRunResult, runErr error) error {
	h, err := r.harnessFor_internal(job.HarnessName)
	if err != nil {
		return err
	}
	out := ClassifyJob(h, job, res.ExitCode, res.Elapsed, r.Timeouts.LaunchGrace, res.Output)
	if fault, why := isInfraFault_internal(res, runErr); fault && out.Result != "fault" {
		out = Outcome{Result: "fault", Reason: why}
	}
	if cost, ok := h.Usage(job); ok {
		rec.Cost = cost
	}
	if job.Kind == "work" && r.Git != nil {
		sha, herr := r.Git.HeadSHA(ctx, job.WorktreeDir)
		if herr == nil && sha != "" && sha != job.BaseSHA && sha != job.PriorHead {
			if perr := r.Git.Push(ctx, job.WorktreeDir, job.Branch, sha); perr != nil {
				if out.Result == "ok" {
					out = Outcome{Result: "fault", Reason: "push fault: " + perr.Error()}
				}
			} else {
				rec.PushedHead, out.Head = sha, sha
			}
		} else if out.Result == "ok" {
			out = Outcome{Result: "fail", Reason: "harness reported ok without advancing HEAD past base_sha and prior_head"}
		}
	} else if job.Kind == "read" {
		out.Head = job.Head
	}
	rec.Outcome, rec.State = out, JobHarvested
	return WriteJobRecord(job.JobDir, *rec)
}
func (r *Runner) rebaseAndPRSteps_internal(ctx context.Context, job Job, rec *JobRecord) error {
	if job.Kind != "work" || rec.Outcome.Result != "ok" {
		return nil
	}
	if r.Git != nil && job.StreamRef != "" {
		newSHA, err := r.Git.RebaseOntoStream(ctx, job.WorktreeDir, job.StreamRef)
		if err != nil {
			var ce *ConflictError
			var fe *FetchError
			switch {
			case errors.As(err, &ce):
				rec.Outcome = Outcome{Result: "fail", Head: rec.PushedHead, Reason: ce.Error()}
			case errors.As(err, &fe):
				rec.Outcome = Outcome{Result: "fault", Head: rec.PushedHead, Reason: fe.Error()}
				_ = WriteJobRecord(job.JobDir, *rec)
				return fe
			default:
				rec.Outcome = Outcome{Result: "fail", Head: rec.PushedHead, Reason: "rebase failed: " + err.Error()}
			}
			_ = WriteJobRecord(job.JobDir, *rec)
			return nil
		}
		rec.Outcome.Head = cmp.Or(newSHA, rec.Outcome.Head)
	}
	for _, cmd := range job.DoneWhen {
		if strings.TrimSpace(cmd) != "" && r.Wall != nil {
			if tres, err := r.Wall.RunTest(ctx, job, cmd, r.Timeouts.RebaseAndTest); err != nil || tres.ExitCode != 0 {
				rec.Outcome = Outcome{Result: "fail", Head: rec.PushedHead, Reason: fmt.Sprintf("tests red (%s): %s", cmd, TailBytes(tres.Output, MaxReasonBytes))}
				_ = WriteJobRecord(job.JobDir, *rec)
				return nil
			}
		}
	}
	if r.Git != nil && rec.Outcome.Head != "" && rec.Outcome.Head != rec.PushedHead {
		if err := r.Git.Push(ctx, job.WorktreeDir, job.Branch, rec.Outcome.Head); err != nil {
			rec.Outcome = Outcome{Result: "fault", Reason: "post-rebase push fault: " + err.Error()}
			_ = WriteJobRecord(job.JobDir, *rec)
			return nil
		}
		rec.PushedHead = rec.Outcome.Head
	}
	rec.State = JobRebased
	if job.RequirePR && r.Forge != nil {
		ghCtx, cancel := context.WithTimeout(ctx, r.Timeouts.GH)
		pr, err := r.Forge.EnsurePR(ghCtx, job.Repo, job.Branch, job.Base, rec.PushedHead, job.Consumer)
		cancel()
		if err != nil {
			rec.Outcome = Outcome{Result: "fault", Head: rec.PushedHead, Reason: "open PR fault: " + err.Error()}
			_ = WriteJobRecord(job.JobDir, *rec)
			return nil
		}
		rec.PRNumber = pr
	}
	_ = WriteJobRecord(job.JobDir, *rec)
	return nil
}
func (r *Runner) sendReportAndClean_internal(ctx context.Context, jobDir string, rec JobRecord) error {
	rec.State = JobReporting
	rec.ResultID = cmp.Or(rec.ResultID, FormatResultID(rec.Epoch, rec.Consumer, rec.Attempt, rec.Gen))
	if err := WriteJobRecord(jobDir, rec); err != nil {
		return err
	}
	req := ReportRequest{
		Epoch: rec.Epoch, Row: rec.Row, Consumer: rec.Consumer, Gen: rec.Gen,
		ResultID: rec.ResultID, Result: rec.Outcome.Result, Head: rec.Outcome.Head,
		Reason: rec.Outcome.Reason, Finding: rec.Outcome.Finding, Cost: rec.Cost.MicroUSD, Priced: rec.Cost.Priced,
	}
	var receipt store.Receipt
	var err error
	for try := 1; try <= 3; try++ {
		receipt, err = r.Sprint.Report(ctx, req)
		if err == nil || !errors.Is(err, ErrStoreDown) || ctx.Err() != nil {
			break
		}
		r.Sleep(time.Duration(try) * 100 * time.Millisecond)
	}
	if err != nil {
		return err
	}
	if !receipt.Accepted || receipt.Op != req.ResultID {
		return fmt.Errorf("%w: report %s was not accepted (accepted=%v op=%q reason=%q): keeping job directory",
			ErrRefused, req.ResultID, receipt.Accepted, receipt.Op, receipt.Reason)
	}
	r.logStep_internal(rec.Row, rec.Consumer, "report", fmt.Sprintf("accepted=%v", receipt.Accepted), 0)
	rec.State = JobRecorded
	_ = WriteJobRecord(jobDir, rec)
	return r.cleanJobDir_internal(ctx, jobDir, rec)
}
func (r *Runner) cleanJobDir_internal(ctx context.Context, jobDir string, rec JobRecord) error {
	wt := filepath.Join(jobDir, "repo")
	if rec.Kind == "work" && rec.PushedHead != "" && r.Git != nil {
		if ok, err := r.Git.OriginHasHead(ctx, rec.Repo, rec.Branch, rec.PushedHead); err != nil || !ok {
			r.logStep_internal(rec.Row, rec.Consumer, "clean", "kept-unconfirmed-head", 0)
			return nil
		}
	}
	if r.Git != nil {
		if dirty, err := r.Git.IsDirty(ctx, wt); err == nil && dirty {
			r.logStep_internal(rec.Row, rec.Consumer, "clean", "DIRTY WORKTREE KEPT", 0)
			return nil
		}
		_ = r.Git.RemoveWorktree(ctx, wt)
	}
	if err := os.RemoveAll(jobDir); err != nil {
		return err
	}
	r.logStep_internal(rec.Row, rec.Consumer, "clean", "removed", 0)
	return nil
}
func (r *Runner) QueryEdge(ctx context.Context, req EdgeRequest) (EdgeResult, error) {
	return r.dedup.Do(ctx, req, func(ctx context.Context, q EdgeRequest) (EdgeResult, error) {
		return r.evalEdgeQuery_internal(ctx, q.Key(), q)
	})
}
func (r *Runner) evalEdgeQuery_internal(ctx context.Context, id string, req EdgeRequest) (EdgeResult, error) {
	row := req.Row
	jobDir := filepath.Join(r.RootDir, "jobs", req.Card)
	if rec, err := ReadJobRecord(jobDir); err == nil && row == "" {
		row = rec.Row
	}
	if row != "" {
		next, err := r.Sprint.Next(ctx, row)
		if err != nil {
			return EdgeResult{}, err
		}
		for _, f := range next.Final {
			if f.ID == req.Card {
				return EdgeResult{Epoch: req.Epoch, ID: id, Kind: req.Kind, Card: req.Card, SHA: cmp.Or(f.Head, req.SHA), Outcome: f.State}, nil
			}
		}
		for _, w := range next.Working {
			if w.ID == req.Card {
				return EdgeResult{Epoch: req.Epoch, ID: id, Kind: req.Kind, Card: req.Card, SHA: cmp.Or(w.Head, req.SHA), Outcome: card.Working}, nil
			}
		}
		for _, rd := range next.Ready {
			if rd.ID == req.Card {
				return EdgeResult{Epoch: req.Epoch, ID: id, Kind: req.Kind, Card: req.Card, SHA: req.SHA, Outcome: card.Ready}, nil
			}
		}
	}
	if rec, err := ReadJobRecord(jobDir); err == nil {
		return EdgeResult{Epoch: req.Epoch, ID: id, Kind: req.Kind, Card: req.Card, SHA: cmp.Or(rec.PushedHead, req.SHA), Outcome: rec.State, Failure: rec.Outcome.Reason}, nil
	}
	return EdgeResult{Epoch: req.Epoch, ID: id, Kind: req.Kind, Card: req.Card, SHA: req.SHA, Outcome: JobNone}, nil
}

type OSGitOps struct {
	WorkCloneDir, MirrorDir, OriginURL string
	Exec                               edge.Exec
	Timeouts                           Timeouts
	Timeout                            time.Duration
}

func (g *OSGitOps) timeouts_internal() Timeouts                      { return g.Timeouts.WithDefaults() }
func (g *OSGitOps) timeoutOr_internal(d time.Duration) time.Duration { return cmp.Or(g.Timeout, d) }
func (g *OSGitOps) StageWorktree(ctx context.Context, j Job) (string, error) {
	if g.MirrorDir != "" && filepath.Clean(g.WorkCloneDir) == filepath.Clean(g.MirrorDir) {
		return "", errors.New("stage refused: work clone path equals bare mirror path (worktrees must be staged from the work clone, never the mirror)")
	}
	to := g.timeoutOr_internal(g.timeouts_internal().Stage)
	if _, err := g.runGit_internal(ctx, to, g.WorkCloneDir, "fetch", "--quiet", "origin"); err != nil {
		return "", err
	}
	baseRef := cmp.Or(j.PriorHead, j.BaseSHA, "HEAD")
	if j.Kind == "read" {
		if _, err := g.runGit_internal(ctx, to, g.WorkCloneDir, "worktree", "add", "--detach", j.WorktreeDir, cmp.Or(j.Head, baseRef)); err != nil {
			return "", err
		}
		_, _ = g.runGit_internal(ctx, to, j.WorktreeDir, "remote", "set-url", "--push", "origin", "no-push-on-read")
	} else if _, err := g.runGit_internal(ctx, to, g.WorkCloneDir, "worktree", "add", "-B", j.Branch, j.WorktreeDir, baseRef); err != nil {
		return "", err
	}
	if j.CommitName != "" {
		_, _ = g.runGit_internal(ctx, to, j.WorktreeDir, "config", "user.name", j.CommitName)
	}
	if j.CommitEmail != "" {
		_, _ = g.runGit_internal(ctx, to, j.WorktreeDir, "config", "user.email", j.CommitEmail)
	}
	return g.HeadSHA(ctx, j.WorktreeDir)
}
func (g *OSGitOps) HeadSHA(ctx context.Context, worktree string) (string, error) {
	out, err := g.runGit_internal(ctx, g.timeoutOr_internal(g.timeouts_internal().Stage), worktree, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}
func (g *OSGitOps) Push(ctx context.Context, worktree, branch, headSHA string) error {
	_, err := g.runGit_internal(ctx, g.timeoutOr_internal(g.timeouts_internal().Push), worktree, "push", "--force", cmp.Or(g.OriginURL, "origin"), headSHA+":refs/heads/"+branch)
	return err
}
func (g *OSGitOps) RebaseOntoStream(ctx context.Context, worktree, streamRef string) (string, error) {
	to := g.timeoutOr_internal(g.timeouts_internal().RebaseAndTest)
	if _, err := g.runGit_internal(ctx, to, worktree, "fetch", "--quiet", cmp.Or(g.OriginURL, "origin"), streamRef); err != nil {
		return "", &FetchError{StreamRef: streamRef, Cause: err}
	}
	if _, err := g.runGit_internal(ctx, to, worktree, "rebase", "FETCH_HEAD"); err != nil {
		out, _ := g.runGit_internal(ctx, to, worktree, "diff", "--name-only", "--diff-filter=U")
		_, _ = g.runGit_internal(ctx, to, worktree, "rebase", "--abort")
		var files []string
		for _, f := range strings.Split(strings.TrimSpace(out), "\n") {
			if f = strings.TrimSpace(f); f != "" {
				files = append(files, f)
			}
		}
		if len(files) > 0 {
			return "", &ConflictError{Files: files}
		}
		return "", err
	}
	return g.HeadSHA(ctx, worktree)
}
func (g *OSGitOps) OriginHasHead(ctx context.Context, repo, branch, sha string) (bool, error) {
	out, err := g.runGit_internal(ctx, g.timeoutOr_internal(g.timeouts_internal().Push), g.WorkCloneDir, "ls-remote", cmp.Or(g.OriginURL, "origin"), "refs/heads/"+branch)
	return err == nil && strings.HasPrefix(strings.TrimSpace(out), sha), err
}
func (g *OSGitOps) HasHead(ctx context.Context, repo, branch, sha string) (bool, error) {
	return g.OriginHasHead(ctx, repo, branch, sha)
}
func (g *OSGitOps) IsDirty(ctx context.Context, worktree string) (bool, error) {
	out, err := g.runGit_internal(ctx, g.timeoutOr_internal(g.timeouts_internal().Stage), worktree, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != "", err
}
func (g *OSGitOps) RemoveWorktree(ctx context.Context, worktree string) error {
	_, err := g.runGit_internal(ctx, g.timeoutOr_internal(g.timeouts_internal().Stage), g.WorkCloneDir, "worktree", "remove", "--force", worktree)
	return err
}
func (g *OSGitOps) runGit_internal(ctx context.Context, timeout time.Duration, dir string, args ...string) (string, error) {
	res, err := cmp.Or[edge.Exec](g.Exec, edge.NewOSExec()).Run(ctx, edge.Cmd{
		Argv: append([]string{"git"}, args...), Dir: dir, Env: []string{"GIT_TERMINAL_PROMPT=0"}, Timeout: timeout,
	})
	combined := res.Stdout + res.Stderr
	if err != nil {
		return combined, fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, TailBytes([]byte(combined), 512))
	}
	if res.ExitCode != 0 {
		return combined, fmt.Errorf("git %s: exit %d (%s)", strings.Join(args, " "), res.ExitCode, TailBytes([]byte(combined), 512))
	}
	return res.Stdout, nil
}

type OSReserveWall struct {
	Config     WallConfig
	UseSandbox bool
	Exec       edge.Exec
}

func (w *OSReserveWall) RunHarness(ctx context.Context, j Job, argv, env []string, timeout time.Duration) (WallRunResult, error) {
	return w.runWallCmd_internal(ctx, j, BuildWallArgv(w.effectiveConfig_internal(j), j, argv), SanitizeWallEnv(env, j.JobDir), timeout)
}
func (w *OSReserveWall) RunTest(ctx context.Context, j Job, cmdStr string, timeout time.Duration) (WallRunResult, error) {
	fields := strings.Fields(cmdStr)
	if len(fields) == 0 {
		return WallRunResult{}, errors.New("empty test command")
	}
	return w.runWallCmd_internal(ctx, j, BuildWallArgv(w.effectiveConfig_internal(j), j, fields), SanitizeWallEnv(os.Environ(), j.JobDir), timeout)
}
func (w *OSReserveWall) effectiveConfig_internal(j Job) WallConfig {
	cfg := w.Config
	cfg.ReserveBin = resolveToolBin_internal(cmp.Or(cfg.ReserveBin, "nova-reserve"))
	cfg.SandboxBin = resolveToolBin_internal(cmp.Or(cfg.SandboxBin, "nova-sandbox"))
	if j.WorktreeDir != "" {
		if data, err := os.ReadFile(filepath.Join(j.WorktreeDir, ".git")); err == nil {
			if gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:"); ok {
				commonGit := filepath.Dir(filepath.Dir(strings.TrimSpace(gitDir)))
				cfg.WritePaths = append(slices.Clone(cfg.WritePaths), commonGit)
				if alt, err := os.ReadFile(filepath.Join(commonGit, "objects", "info", "alternates")); err == nil && strings.TrimSpace(string(alt)) != "" {
					cfg.ReadPaths = append(slices.Clone(cfg.ReadPaths), filepath.Dir(strings.TrimSpace(string(alt))))
				}
			}
		}
	}
	return cfg
}
func resolveToolBin_internal(name string) string {
	if strings.Contains(name, "/") {
		return name
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		if cand := filepath.Join(home, ".local", "bin", name); fileExists_internal(cand) {
			return cand
		}
	}
	return name
}
func fileExists_internal(p string) bool { _, err := os.Stat(p); return err == nil }
func (w *OSReserveWall) runWallCmd_internal(ctx context.Context, j Job, argv, env []string, timeout time.Duration) (WallRunResult, error) {
	res, err := cmp.Or[edge.Exec](w.Exec, edge.NewOSExec()).Run(ctx, edge.Cmd{
		Argv: argv, Dir: cmp.Or(j.WorktreeDir, j.JobDir), Env: env, CleanEnv: len(env) > 0, Timeout: timeout,
	})
	combined := res.Stdout
	if res.Stderr != "" {
		if combined != "" && !strings.HasSuffix(combined, "\n") {
			combined += "\n"
		}
		combined += res.Stderr
	}
	pid := os.Getpid()
	out := WallRunResult{ExitCode: res.ExitCode, Output: []byte(combined), PID: pid, Nonce: strconv.Itoa(pid), Elapsed: res.Duration}
	if res.ExitCode == 1 && strings.Contains(combined, "nova-reserve:") && !strings.Contains(combined, "nova-reserve: HELD") {
		return out, ErrNoSlot
	}
	return out, err
}
