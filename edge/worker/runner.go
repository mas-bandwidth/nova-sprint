package worker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/store"
)

var (
	ErrNoSlot               = errors.New("worker: no nova-reserve slot available within wait")
	wallPIDSeq_internal     atomic.Int64
	liveProcNonces_internal sync.Map
)

func nextWallPID_internal() int {
	if pid := int(40000 + wallPIDSeq_internal.Add(1)); pid != os.Getpid() {
		return pid
	}
	return int(40000 + wallPIDSeq_internal.Add(1))
}

type (
	ConflictError struct{ Files []string }
	FetchError    struct {
		StreamRef string
		Cause     error
	}
	GitOps interface {
		StageWorktree(ctx context.Context, j Job) (headSHA string, err error)
		HeadSHA(ctx context.Context, worktree string) (string, error)
		Push(ctx context.Context, worktree, branch, headSHA string) error
		RebaseOntoStream(ctx context.Context, worktree, streamRef string) (newSHA string, err error)
		OriginHasHead(ctx context.Context, repo, branch, sha string) (bool, error)
		IsDirty(ctx context.Context, worktree string) (bool, error)
		RemoveWorktree(ctx context.Context, worktree string) error
	}
	ForgeOps interface {
		EnsurePR(ctx context.Context, repo, branch, base, headSHA, title string) (int, error)
	}
	ForgeAdapter  struct{ Client forge.Forge }
	WallRunResult struct {
		ExitCode, PID int
		Output        []byte
		Nonce         string
		Elapsed       time.Duration
	}
	ReserveWall interface {
		RunHarness(ctx context.Context, j Job, argv, env []string, timeout time.Duration) (WallRunResult, error)
		RunTest(ctx context.Context, j Job, cmd string, timeout time.Duration) (WallRunResult, error)
	}
	ProcessTable interface {
		Alive(pid int, nonce string) bool
		Kill(pid int, nonce string, grace time.Duration) error
	}
	OSProcessTable struct{}
)

func (e *ConflictError) Error() string { return "rebase conflict: " + strings.Join(e.Files, ", ") }
func (e *FetchError) Error() string    { return fmt.Sprintf("fetch %s failed: %v", e.StreamRef, e.Cause) }
func (e *FetchError) Unwrap() error    { return e.Cause }
func (a *ForgeAdapter) EnsurePR(ctx context.Context, repo, branch, base, headSHA, title string) (int, error) {
	if a == nil || a.Client == nil {
		return 0, errors.New("forge client not configured")
	}
	info, err := a.Client.OpenPR(ctx, repo, branch, base, title, headSHA)
	return info.Number, err
}
func (OSProcessTable) Alive(pid int, nonce string) bool {
	if pid <= 0 || pid == os.Getpid() || strings.TrimSpace(nonce) == "" || syscall.Kill(pid, 0) != nil {
		return false
	}
	if v, ok := liveProcNonces_internal.Load(pid); ok && v == nonce {
		return true
	}
	if envBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid)); err == nil {
		return slices.Contains(strings.Split(string(envBytes), "\x00"), "NOVA_WORKER_NONCE="+nonce)
	}
	return false
}
func (p OSProcessTable) Kill(pid int, nonce string, grace time.Duration) error {
	if !p.Alive(pid, nonce) {
		return nil
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	for deadline := time.Now().Add(grace); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		if !p.Alive(pid, nonce) {
			liveProcNonces_internal.Delete(pid)
			return nil
		}
	}
	err := syscall.Kill(pid, syscall.SIGKILL)
	liveProcNonces_internal.Delete(pid)
	return err
}

type Runner struct {
	Host, RootDir, StateDir, ProxyAddr string
	CoordinatorHosts                   []string
	Sprint                             SprintClient
	Git                                GitOps
	Forges                             map[string]ForgeOps
	BusHook                            BusHookStarter
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
	SkipPR                             bool            // the worker never opens a PR: the landing does (71); serve sets it
	fatal                              chan error      // a refused receipt from a detached job ends the wake loop (66)
	jobs                               *sync.WaitGroup // every job a pass launched, waited for when the loop ends
	probed, withdrawn                  map[string]bool
	adopted                            map[string]JobRecord
	running                            map[string]context.CancelFunc
	dedup                              *edge.Deduplicator
	backoffSeq                         uint64
}

func NewRunner(cfg Runner) (*Runner, error) {
	if err := CheckHost(cfg.Host, cfg.CoordinatorHosts); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.RootDir) == "" {
		return nil, errors.New("worker refused: root directory is empty: pass --root <dir>")
	}
	if cfg.Now == nil || cfg.Sleep == nil {
		return nil, errors.New("worker refused: no clock: the caller passes Now and Sleep (the real clock in cmd/nova-worker, a fake one in a unit test)")
	}
	cfg.Timeouts = cfg.Timeouts.WithDefaults()
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
	if h, ok := r.Harnesses[name]; name != "" && ok {
		return h, nil
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
		Consumer: row + ".probe", Row: row, Kind: "work",
		Model: model, HarnessName: h.Name(), JobDir: probeDir,
		WorktreeDir: filepath.Join(probeDir, "repo"), ProxyAddr: r.ProxyAddr,
	})
	pj.JobDir, pj.WorktreeDir = probeDir, filepath.Join(probeDir, "repo")
	argv, env := h.Command(pj)
	res, runErr := r.Wall.RunHarness(ctx, pj, argv, env, r.Timeouts.LaunchGrace)
	pj.ProxyStatus = cmp.Or(pj.ProxyStatus, r.Proxy.ConsumeStatus())
	out := ClassifyJob(h, pj, res.ExitCode, res.Elapsed, r.Timeouts.LaunchGrace, res.Output)
	if runErr != nil || out.Result != "ok" {
		r.mu.Lock()
		r.probed[row] = false
		r.mu.Unlock()
		lastLine := lastNonEmptyLine_internal(string(res.Output), out.Reason)
		if runErr != nil && lastLine == "" {
			lastLine = runErr.Error()
		}
		// LOCAL DOGFOOD STOPGAP: the whole output, ANSI stripped, bounded, and kept in the probe dir.
		whole := ansiRe_local.ReplaceAllString(string(res.Output), "")
		_ = os.WriteFile(filepath.Join(os.TempDir(), "nova-sprint-probe-"+row+".txt"), []byte(whole+"\nreason="+out.Reason+"\nexit="+fmt.Sprint(res.ExitCode)+"\n"), 0o644)
		whole = strings.Join(strings.Fields(whole), " ")
		if len(whole) > 1500 {
			whole = whole[:1500]
		}
		lastLine = whole + " | reason=" + out.Reason + " exit=" + fmt.Sprint(res.ExitCode) + " proxy=" + fmt.Sprint(pj.ProxyStatus) + " argv=" + strings.Join(argv, " ")
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
		} else if w, isWorking := workingByID[id]; isWorking && !r.isRunningInFlight_internal(id) {
			if !jobMatchesSummary_internal(next, w, rec) {
				r.logStep_internal(row, id, "recover", "identity-mismatch-kept", 0)
			} else if err := r.recoverWorkingJob_internal(ctx, w, rec); err != nil && !errors.Is(err, ErrStoreDown) {
				// Defect 69: one record the sprint keeps refusing must not end the
				// pass before it has taken anything (it wedged a row for an hour).
				// The record stays on disk and is tried again next pass; the pass
				// goes on to the other records and to the ready cards.
				r.logStep_internal(row, id, "recover", "refused: "+err.Error(), 0)
			}
		}
	}
	for _, w := range next.Working {
		if _, onDisk := disk[w.ID]; !onDisk && !r.isRunningInFlight_internal(w.ID) {
			if err := r.reportLostJob_internal(ctx, next.Epoch, row, w); err != nil && !errors.Is(err, ErrStoreDown) {
				r.logStep_internal(row, w.ID, "recover", "lost-job report refused: "+err.Error(), 0)
			}
		}
	}
	return nil
}
func (r *Runner) loadRowJobs_internal(row string) (map[string]JobRecord, error) {
	entries, err := os.ReadDir(filepath.Join(r.RootDir, "jobs"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	out := map[string]JobRecord{}
	for _, e := range entries {
		if rec, err := ReadJobRecord(filepath.Join(r.RootDir, "jobs", e.Name())); e.IsDir() && err == nil && rec.Row == row {
			out[rec.Consumer] = rec
		}
	}
	return out, nil
}
func (r *Runner) recoverWorkingJob_internal(ctx context.Context, w ConsumerSummary, rec JobRecord) error {
	jobDir := filepath.Join(r.RootDir, "jobs", w.ID)
	if rec.State == JobCleaned || rec.State == JobRecorded {
		return nil
	}
	if rec.State == JobTaken || rec.State == JobStaged {
		// Defect 70: a job taken or staged but never run (the runner went down
		// in between) was skipped here and sat "working" forever with no
		// process. "No slot within the wait is no start yet, never a failure"
		// holds for a fresh one; one still not running after the grace is the
		// runner's fault: report it so the card deals again.
		if rec.StartedAt > 0 && r.Now().Unix()-rec.StartedAt > int64(2*time.Minute/time.Second) {
			return r.faultAndReportJob_internal(ctx, jobDir, w.ID, rec, "restart: job "+rec.State+" but never run")
		}
		return nil
	}
	if rec.State == JobHarvested && rec.Outcome.Result == "ok" && rec.Kind == "work" {
		if err := r.rebaseAndPRSteps_internal(ctx, jobDir, &rec); err != nil {
			return err
		}
		return r.sendReportAndClean_internal(ctx, jobDir, rec)
	}
	if rec.State == JobCIWait || rec.State == JobReporting || rec.State == JobRebased || (rec.ResultID != "" && rec.Outcome.Result != "") {
		return r.sendReportAndClean_internal(ctx, jobDir, rec)
	}
	if rec.State == JobRunning && r.Procs != nil && r.Procs.Alive(rec.PID, rec.Nonce) {
		if rec.DeadlineAt > 0 && r.Now().Unix() >= rec.DeadlineAt {
			_ = r.Procs.Kill(rec.PID, rec.Nonce, r.Timeouts.TerminateGrace)
			r.mu.Lock()
			delete(r.adopted, w.ID)
			r.mu.Unlock()
			return r.faultAndReportJob_internal(ctx, jobDir, w.ID, rec, "adopted harness timed out past deadline")
		}
		r.mu.Lock()
		r.adopted[w.ID] = rec
		r.mu.Unlock()
		r.logStep_internal(rec.Row, w.ID, "adopt", fmt.Sprintf("pid=%d", rec.PID), 0)
		return nil
	}
	return r.faultAndReportJob_internal(ctx, jobDir, w.ID, rec, "restart: harness exited while runner was down")
}
func (r *Runner) faultAndReportJob_internal(ctx context.Context, jobDir, id string, rec JobRecord, reason string) error {
	rec.PushedHead = cmp.Or(r.harvestPartialPush_internal(ctx, jobDir, rec), rec.PushedHead)
	rec.ResultID = cmp.Or(rec.ResultID, FormatResultID(rec.Epoch, id, rec.Attempt, rec.Gen))
	rec.Outcome, rec.State = Outcome{Result: "fault", Head: rec.PushedHead, Reason: reason}, JobReporting
	if err := WriteJobRecord(jobDir, rec); err != nil {
		return err
	}
	return r.sendReportAndClean_internal(ctx, jobDir, rec)
}
func (r *Runner) reportLostJob_internal(ctx context.Context, epoch int, row string, w ConsumerSummary) error {
	_, err := r.Sprint.Report(ctx, ReportRequest{
		Epoch: epoch, Row: row, Consumer: w.ID, Gen: w.Gen,
		ResultID: FormatResultID(epoch, w.ID, w.Attempt, w.Gen), Result: "fault", Reason: "runner lost the job",
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
func (r *Runner) beginConsumer_internal(ctx context.Context, id string) (context.Context, func(), bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, busy := r.running[id]; busy {
		return nil, nil, false
	}
	delete(r.withdrawn, id)
	runCtx, cancel := context.WithCancel(ctx)
	r.running[id] = cancel
	return runCtx, func() { r.mu.Lock(); delete(r.running, id); r.mu.Unlock(); cancel() }, true
}
func (r *Runner) isRunningInFlight_internal(id string) bool {
	r.mu.Lock()
	_, b := r.running[id]
	r.mu.Unlock()
	return b
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

// Start is the startup order (drift row 20): the bus hook, which claims the
// row's name; then a self-unhook of any sprint hook a dead worker left (a
// refusal means there was none); then `nova-sprint hook --as <row> --role
// worker`. Nothing acts before it returns, and the caller owns the hook.
func (r *Runner) Start(ctx context.Context, row string) (WakeHook, error) {
	if r.BusHook == nil {
		return nil, errors.New("worker refused: no bus hook configured: start nova-worker through cmd/nova-worker")
	}
	hook, err := r.BusHook(ctx, row)
	if err != nil {
		return nil, err
	}
	if err := r.Sprint.Unhook(ctx, row); err != nil && !errors.Is(err, ErrRefused) {
		hook.Stop()
		return nil, err
	}
	if err := r.Sprint.Hook(ctx, row); err != nil {
		hook.Stop()
		return nil, err
	}
	r.logStep_internal(row, "-", "hook", "ok", 0)
	return hook, nil
}

// RunOnce is one pass under the same startup order.
func (r *Runner) RunOnce(ctx context.Context, row string) error {
	hook, err := r.Start(ctx, row)
	if err != nil {
		return err
	}
	defer hook.Stop()
	return r.StepRow(ctx, row)
}

// RunWakeLoop hooks in (Start), takes one pass, then one pass per burst of
// wakes: a pass runs on a goroutine of its own, wakes that arrive while it
// runs coalesce into one more pass, and unacknowledged reports are sent again
// by each pass (RecoverRow). It returns nil when ctx ends, and an error when
// the bus hook dies, which the caller turns into a non-zero exit, or when a
// receipt is refused (ErrReceipt).
func (r *Runner) RunWakeLoop(ctx context.Context, row string) error {
	hook, err := r.Start(ctx, row)
	if err != nil {
		return err
	}
	defer hook.Stop()
	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	passDone, running, pending := make(chan error, 1), false, true
	// Defect 66: a pass every 10 s as well as on each wake, so a ready card
	// never waits on a missed push (Glenn: a ready card never sits).
	poll := time.NewTicker(10 * time.Second)
	defer poll.Stop()
	r.mu.Lock()
	if r.fatal == nil {
		r.fatal = make(chan error, 1)
	}
	if r.jobs == nil {
		r.jobs = &sync.WaitGroup{}
	}
	r.mu.Unlock()
	wait := func() {
		cancel()
		if running {
			<-passDone
		}
		r.jobsGroup_internal().Wait() // detached jobs end with the loop, as the pass's jobs always did
	}
	for {
		if pending && !running {
			pending, running = false, true
			go func() { passDone <- r.stepRow_internal(loopCtx, row, true) }()
		}
		select {
		case <-ctx.Done():
			wait()
			return nil
		case <-hook.Wake():
			pending = true
		case <-poll.C:
			pending = true
		case err := <-r.fatal: // a refused receipt from a detached job (66)
			wait()
			return err
		case err := <-passDone:
			running = false
			if err != nil {
				r.logStep_internal(row, "-", "pass", "error: "+err.Error(), 0)
				if errors.Is(err, ErrReceipt) {
					return err
				}
			}
		case <-hook.Done():
			wait()
			return hook.Err()
		}
	}
}
func ignoreStoreDown_internal(err error) error {
	if errors.Is(err, ErrStoreDown) {
		return nil
	}
	return err
}

// StepRow is one synchronous pass: it waits for the jobs it launches (the
// --once form and the tests). The wake loop uses stepRow_internal detached.
func (r *Runner) StepRow(ctx context.Context, row string) error {
	return r.stepRow_internal(ctx, row, false)
}

// stageLocks serializes fetches into one work clone (defect 72).
var stageLocks sync.Map

func (r *Runner) jobsGroup_internal() *sync.WaitGroup {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.jobs == nil {
		r.jobs = &sync.WaitGroup{}
	}
	return r.jobs
}

func (r *Runner) stepRow_internal(ctx context.Context, row string, detach bool) error {
	if err := r.RecoverRow(ctx, row); err != nil {
		return ignoreStoreDown_internal(err)
	}
	next, err := r.Sprint.Next(ctx, row)
	if err != nil || next.State != card.Up || next.Held {
		return ignoreStoreDown_internal(err)
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
	// Defect 66 (dogfood 2026-11-07): a pass never waits on the jobs it
	// launches. With wg.Wait() here one long job held the whole row: no
	// take until it ended, every bench frozen at what its first pass
	// launched. Jobs run detached (beginConsumer tracks them; a job's
	// error is logged on its own line) and the pass returns at once.
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error
	for _, c := range toLaunch {
		c := c
		wg.Add(1)
		jobs := r.jobsGroup_internal()
		jobs.Add(1)
		go func() {
			defer wg.Done()
			defer jobs.Done()
			err := r.RunConsumer(ctx, next, c)
			if err == nil || errors.Is(err, ErrNoSlot) || errors.Is(err, ErrStoreDown) {
				return
			}
			if !detach {
				errMu.Lock()
				firstErr = cmp.Or(firstErr, err)
				errMu.Unlock()
				return
			}
			r.logStep_internal(row, c.ID, "job", "error: "+err.Error(), 0)
			if errors.Is(err, ErrReceipt) {
				select {
				case r.fatal <- err:
				default:
				}
			}
		}()
	}
	if detach {
		return nil
	}
	wg.Wait()
	return firstErr
}
func (r *Runner) RunConsumer(ctx context.Context, rowInfo NextResponse, c ConsumerSummary) error {
	runCtx, done, ok := r.beginConsumer_internal(ctx, c.ID)
	if !ok {
		return nil
	}
	defer done()
	jobDir := filepath.Join(r.RootDir, "jobs", c.ID)
	if _, err := os.Stat(jobDir); err == nil {
		return fmt.Errorf("worker refused: job directory %s already exists: a job directory is never reused", jobDir)
	}
	info, err := r.Sprint.Take(runCtx, rowInfo.Row, c.ID, c.Gen)
	if err == nil {
		err = checkReceipt_internal(info)
	}
	if errors.Is(err, ErrReceipt) {
		return r.refuseReceipt_internal(runCtx, jobDir, rowInfo, c, err)
	}
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
	r.mu.Lock()
	withdrawn := r.withdrawn[c.ID]
	r.mu.Unlock()
	if withdrawn {
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
	if err := r.rebaseAndPRSteps_internal(runCtx, jobDir, &rec); err != nil {
		return err
	}
	return r.sendReportAndClean_internal(runCtx, jobDir, rec)
}

// refuseReceipt_internal reports the card taken under a receipt the worker
// cannot use as a fault naming the fix, through the durable path (a job
// record at state reporting), and returns the refusal, which ends the worker.
func (r *Runner) refuseReceipt_internal(ctx context.Context, jobDir string, rowInfo NextResponse, c ConsumerSummary, cause error) error {
	rec := JobRecord{
		Epoch: rowInfo.Epoch, Consumer: c.ID, Producer: c.Producer, Kind: c.Kind, Row: rowInfo.Row, Stream: c.Stream,
		Gen: c.Gen, Attempt: c.Attempt, ResultID: FormatResultID(rowInfo.Epoch, c.ID, c.Attempt, c.Gen),
		State: JobReporting, Outcome: Outcome{Result: "fault", Reason: TailBytes([]byte(cause.Error()), MaxReasonBytes)},
	}
	if err := WriteJobRecord(jobDir, rec); err != nil {
		return err
	}
	if err := r.sendReportAndClean_internal(ctx, jobDir, rec); err != nil {
		r.logStep_internal(rowInfo.Row, c.ID, "receipt", "refused; report kept: "+err.Error(), 0)
	}
	return cause
}
func (r *Runner) initJob_internal(rowInfo NextResponse, info TakeInfo, jobDir string) (Job, JobRecord, error) {
	for _, sub := range []string{"home", "tmp", "data"} {
		if err := os.MkdirAll(filepath.Join(jobDir, sub), 0o755); err != nil && sub == "home" {
			return Job{}, JobRecord{}, err
		}
	}
	wt, now := filepath.Join(jobDir, "repo"), r.Now().UTC()
	deadline := time.Duration(info.Deadline) * time.Second
	epoch := cmp.Or(info.Epoch, rowInfo.Epoch)
	job := Job{
		Epoch: epoch, Consumer: info.Consumer, Producer: info.Producer, Kind: info.Kind,
		Row: rowInfo.Row, Stream: info.Stream, Repo: info.Repo, Base: info.Base,
		StreamRef: info.StreamRef, Branch: info.Branch, BaseSHA: info.BaseSHA,
		PriorHead: info.PriorHead, Head: info.Head, Tier: info.Tier,
		Model: info.Model, HarnessName: rowInfo.Harness, Brief: info.Brief,
		Prose: info.Prose, Finding: info.Finding, Paths: info.Paths,
		DoneWhen: info.DoneWhen, Gen: info.Gen, Attempt: info.Attempt,
		Review: info.Review, JobDir: jobDir, WorktreeDir: wt,
		ProxyAddr: r.ProxyAddr, CommitName: "Rowan Claude", CommitEmail: "305107455+rowan-claude@users.noreply.github.com" /* Glenn 2026-10-22: the CAA keys off the GitHub id; every agent commits as the rowan-claude login */, Forge: info.Forge, Deadline: deadline,
	}
	rec := JobRecord{
		Epoch: epoch, Consumer: info.Consumer, Producer: info.Producer, Kind: info.Kind,
		Row: rowInfo.Row, Stream: info.Stream, Repo: info.Repo, Base: info.Base,
		StreamRef: info.StreamRef, Branch: info.Branch, BaseSHA: info.BaseSHA,
		PriorHead: info.PriorHead, TargetHead: info.Head, DoneWhen: cloneStrings_internal(info.DoneWhen),
		Gen: info.Gen, Attempt: info.Attempt, ResultID: FormatResultID(epoch, info.Consumer, info.Attempt, info.Gen),
		State: JobTaken, StartedAt: now.Unix(), DeadlineAt: now.Add(deadline).Unix(), Forge: info.Forge,
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
	writeJobMD_internal(*job) // defect 68: the prompt says "read $JOB/JOB.md first"; nothing wrote it
	r.logStep_internal(job.Row, job.Consumer, "stage", "ok", r.Now().Sub(start))
	return nil
}

// writeJobMD_internal writes JOB.md in the job directory: what the card is,
// where the staged checkout is, and how the card finishes (the verdict file;
// the worker pushes and reports, the harness never does).
func writeJobMD_internal(j Job) {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nkind: %s (%s)\nrepository: %s\nbase: %s\n", j.Consumer, j.Kind, j.Tier, j.Repo, j.Base)
	fmt.Fprintf(&b, "checkout: %s (a full clone at the base", j.WorktreeDir)
	if j.Kind == "work" {
		fmt.Fprintf(&b, ", on branch %s; commit there, never push)\n", j.Branch)
	} else {
		fmt.Fprintf(&b, "; detached at the head under review %s; edit no tracked file)\n", j.Head)
	}
	if len(j.Paths) > 0 {
		fmt.Fprintf(&b, "paths: %s\n", strings.Join(j.Paths, ", "))
	}
	for _, d := range j.DoneWhen {
		fmt.Fprintf(&b, "done-when: %s\n", d)
	}
	fmt.Fprintf(&b, "\n## How this card finishes\n\nWrite %s/result.json before you exit, then exit 0.\n", j.JobDir)
	if j.Kind == "work" {
		b.WriteString("- {\"result\":\"ok\"}: the work is committed on the branch and every done-when command is green.\n")
		b.WriteString("- {\"result\":\"fail\",\"reason\":\"<what was not done and why>\"}: not done; say what you found.\n")
		b.WriteString("The worker pushes the branch and reports; you run no git push and no gh.\n")
	} else {
		b.WriteString("- {\"result\":\"ok\"}: the head meets the card as written.\n")
		b.WriteString("- {\"result\":\"fix\",\"finding\":\"<path>:<line>: <defect and repair>\"}: it does not; name every defect.\n")
		b.WriteString("- {\"result\":\"fault\",\"reason\":\"<why>\"}: you could not judge it (a tool, a build, a missing input).\n")
	}
	_ = os.WriteFile(filepath.Join(j.JobDir, "JOB.md"), []byte(b.String()), 0o644)
}
func (r *Runner) launchBackoff_internal(attempt int) time.Duration {
	lo, hi := r.Timeouts.LaunchBackoff1Min, r.Timeouts.LaunchBackoff1Max
	if attempt >= 2 {
		lo, hi = r.Timeouts.LaunchBackoff2Min, r.Timeouts.LaunchBackoff2Max
	}
	if hi <= lo {
		return lo
	}
	r.mu.Lock()
	r.backoffSeq++
	seq := r.backoffSeq
	r.mu.Unlock()
	mix := uint64(r.Now().UnixNano()) + seq*0x9e3779b97f4a7c15
	mix = (mix ^ (mix >> 30)) * 0xbf58476d1ce4e5b9
	mix = (mix ^ (mix >> 27)) * 0x94d049bb133111eb
	return lo + time.Duration((mix^(mix>>31))%uint64(hi-lo))
}
func (r *Runner) runStep_internal(ctx context.Context, job Job, rec *JobRecord) (WallRunResult, error) {
	h, err := r.harnessFor_internal(job.HarnessName)
	if err != nil {
		return WallRunResult{}, err
	}
	argv, env := h.Command(job)
	rec.State = JobRunning
	if rec.PID <= 0 || rec.PID == os.Getpid() {
		rec.PID = nextWallPID_internal()
		rec.Nonce = fmt.Sprintf("n-%d-%d", rec.PID, r.Now().UnixNano())
	}
	_ = WriteJobRecord(job.JobDir, *rec)
	var last WallRunResult
	var runErr error
	for attempt := 1; attempt <= r.Timeouts.LaunchTries; attempt++ {
		last, runErr = r.Wall.RunHarness(ctx, job, argv, env, job.Deadline)
		if errors.Is(runErr, ErrNoSlot) {
			rec.State, rec.PID, rec.Nonce = JobStaged, 0, ""
			_ = WriteJobRecord(job.JobDir, *rec)
			return last, ErrNoSlot
		}
		if last.PID > 0 && last.PID != os.Getpid() {
			rec.PID, rec.Nonce = last.PID, cmp.Or(last.Nonce, rec.Nonce)
			_ = WriteJobRecord(job.JobDir, *rec)
		}
		_ = os.WriteFile(filepath.Join(job.JobDir, "harness.log"), last.Output, 0o600)
		job.ProxyStatus = cmp.Or(job.ProxyStatus, r.Proxy.ConsumeStatus())
		out := ClassifyJob(h, job, last.ExitCode, last.Elapsed, r.Timeouts.LaunchGrace, last.Output)
		if !out.Retryable5xx || attempt == r.Timeouts.LaunchTries {
			break
		}
		job.ProxyStatus = 0
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
	job.ProxyStatus = cmp.Or(job.ProxyStatus, r.Proxy.ConsumeStatus())
	out := ClassifyJob(h, job, res.ExitCode, res.Elapsed, r.Timeouts.LaunchGrace, res.Output)
	if fault, why := isInfraFault_internal(res, runErr); fault && out.Result != "fault" {
		out = Outcome{Result: "fault", Reason: why}
	}
	if cost, ok := h.Usage(job); ok {
		rec.Cost = cost
	}
	if job.Kind == "work" {
		if r.Git == nil {
			out = Outcome{Result: "fault", Reason: "missing git edge (--work-clone) for work consumer"}
		} else if sha, herr := r.Git.HeadSHA(ctx, job.WorktreeDir); herr == nil && sha != "" && sha != job.BaseSHA && sha != job.PriorHead {
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
		// Defect 67 (dogfood 2026-11-07): a read reports ok, fix or fault; the
		// store refuses "fail" for a read and the job then sat "working" with
		// no process, re-reported and refused every pass. No verdict file is a
		// fault of the harness; a verdict of fail/broken is a fix with its reason.
		if out.Result == "fail" {
			if strings.Contains(out.Reason, "result.json") {
				out.Result = "fault"
			} else {
				out.Result, out.Finding = "fix", cmp.Or(out.Finding, out.Reason)
			}
		}
	}
	rec.Outcome, rec.State = out, JobHarvested
	return WriteJobRecord(job.JobDir, *rec)
}
func (r *Runner) rebaseAndPRSteps_internal(ctx context.Context, jobDir string, rec *JobRecord) error {
	if rec.Kind != "work" || rec.Outcome.Result != "ok" {
		return nil
	}
	wt := filepath.Join(jobDir, "repo")
	if r.Git != nil && rec.StreamRef != "" {
		newSHA, err := r.Git.RebaseOntoStream(ctx, wt, rec.StreamRef)
		if err != nil {
			var ce *ConflictError
			var fe *FetchError
			switch {
			case errors.As(err, &ce):
				rec.Outcome = Outcome{Result: "fail", Head: rec.PushedHead, Reason: ce.Error()}
			case errors.As(err, &fe):
				rec.Outcome = Outcome{Result: "fault", Head: rec.PushedHead, Reason: fe.Error()}
				_ = WriteJobRecord(jobDir, *rec)
				return fe
			default:
				rec.Outcome = Outcome{Result: "fail", Head: rec.PushedHead, Reason: "rebase failed: " + err.Error()}
			}
			_ = WriteJobRecord(jobDir, *rec)
			return nil
		}
		rec.Outcome.Head = cmp.Or(newSHA, rec.Outcome.Head)
	}
	tJob := Job{Consumer: rec.Consumer, Row: rec.Row, JobDir: jobDir, WorktreeDir: wt, ProxyAddr: r.ProxyAddr}
	for _, cmd := range rec.DoneWhen {
		if strings.TrimSpace(cmd) != "" && r.Wall != nil {
			if tres, err := r.Wall.RunTest(ctx, tJob, cmd, r.Timeouts.RebaseAndTest); err != nil || tres.ExitCode != 0 {
				rec.Outcome = Outcome{Result: "fail", Head: rec.PushedHead, Reason: fmt.Sprintf("tests red (%s): %s", cmd, TailBytes(tres.Output, MaxReasonBytes))}
				_ = WriteJobRecord(jobDir, *rec)
				return nil
			}
		}
	}
	if r.Git != nil && rec.Outcome.Head != "" && rec.Outcome.Head != rec.PushedHead {
		if err := r.Git.Push(ctx, wt, rec.Branch, rec.Outcome.Head); err != nil {
			rec.Outcome = Outcome{Result: "fault", Reason: "post-rebase push fault: " + err.Error()}
			_ = WriteJobRecord(jobDir, *rec)
			return nil
		}
		rec.PushedHead = rec.Outcome.Head
	}
	rec.State = JobRebased
	// Defect 71 (dogfood 2026-11-07, INTENT rule 16): the worker's contract
	// ends at a pushed head; the run's landing opens the PR with the host's
	// identity. A bench has no forge identity, so this step faulted every
	// finished work card ("open PR fault: neither GH_TOKEN nor GH_CONFIG_DIR").
	if rec.Forge != "" && !r.SkipPR {
		fo := r.Forges[rec.Forge]
		if fo == nil {
			rec.Outcome = Outcome{Result: "fault", Head: rec.PushedHead, Reason: fmt.Sprintf("the stream's forge %q has no adapter on this worker: install a nova-worker that serves it, or set the stream's forge to one this worker serves", rec.Forge)}
			_ = WriteJobRecord(jobDir, *rec)
			return nil
		}
		ghCtx, cancel := context.WithTimeout(ctx, r.Timeouts.GH)
		pr, err := fo.EnsurePR(ghCtx, rec.Repo, rec.Branch, rec.Base, rec.PushedHead, rec.Consumer)
		cancel()
		if err != nil {
			rec.Outcome = Outcome{Result: "fault", Head: rec.PushedHead, Reason: "open PR fault: " + err.Error()}
			_ = WriteJobRecord(jobDir, *rec)
			return nil
		}
		rec.PRNumber = pr
	}
	_ = WriteJobRecord(jobDir, *rec)
	return nil
}
func (r *Runner) sendReportAndClean_internal(ctx context.Context, jobDir string, rec JobRecord) error {
	// Defect 67, also for records written before the fix: a read reports ok,
	// fix or fault; a stored "fail" on a read is a fault (no verdict) or a fix.
	if rec.Kind == "read" && rec.Outcome.Result == "fail" {
		if strings.Contains(rec.Outcome.Reason, "result.json") {
			rec.Outcome.Result = "fault"
		} else {
			rec.Outcome.Result, rec.Outcome.Finding = "fix", cmp.Or(rec.Outcome.Finding, rec.Outcome.Reason)
		}
	}
	rec.State = JobReporting
	rec.ResultID = cmp.Or(rec.ResultID, FormatResultID(rec.Epoch, rec.Consumer, rec.Attempt, rec.Gen))
	if err := WriteJobRecord(jobDir, rec); err != nil {
		return err
	}
	req := ReportRequest{
		Epoch: rec.Epoch, Row: rec.Row, Consumer: rec.Consumer, Gen: rec.Gen,
		ResultID: rec.ResultID, Result: rec.Outcome.Result, Head: rec.Outcome.Head,
		Reason: rec.Outcome.Reason, Finding: rec.Outcome.Finding, Cost: rec.Cost.MicroUSD, Priced: rec.Cost.Priced, Usage: rec.Cost.Usage,
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
	res := EdgeResult{Epoch: req.Epoch, ID: id, Kind: req.Kind, Card: req.Card, SHA: req.SHA, Outcome: JobNone}
	rec, recErr := ReadJobRecord(filepath.Join(r.RootDir, "jobs", req.Card))
	if row := cmp.Or(req.Row, rec.Row); row != "" {
		next, err := r.Sprint.Next(ctx, row)
		if err != nil {
			return EdgeResult{}, err
		}
		for _, g := range []struct {
			items []ConsumerSummary
			state string
		}{{next.Final, ""}, {next.Working, card.Working}, {next.Ready, card.Ready}} {
			for _, c := range g.items {
				if c.ID == req.Card {
					res.SHA, res.Outcome = cmp.Or(c.Head, req.SHA), cmp.Or(g.state, c.State)
					return res, nil
				}
			}
		}
	}
	if recErr == nil {
		res.SHA, res.Outcome, res.Failure = cmp.Or(rec.PushedHead, req.SHA), rec.State, rec.Outcome.Reason
	}
	return res, nil
}

type OSGitOps struct {
	WorkCloneDir, MirrorDir, OriginURL string
	Exec                               edge.Exec
	Timeouts                           Timeouts
	Timeout                            time.Duration
}

func (g *OSGitOps) to_internal(d time.Duration) time.Duration { return cmp.Or(g.Timeout, d) }
func (g *OSGitOps) StageWorktree(ctx context.Context, j Job) (string, error) {
	if g.MirrorDir != "" && filepath.Clean(g.WorkCloneDir) == filepath.Clean(g.MirrorDir) {
		return "", errors.New("stage refused: work clone path equals bare mirror path (worktrees must be staged from the work clone, never the mirror)")
	}
	to := g.to_internal(g.Timeouts.WithDefaults().Stage)
	work, origin := g.WorkCloneDir, g.OriginURL
	if os.Getenv("NOVA_WORKER_PER_REPO") == "1" && j.Repo != "" { // LOCAL DOGFOOD STOPGAP (defect 42): one work clone per stream repo, beside the configured one
		work, origin = g.perRepo_internal(j.Repo)
		if _, err := os.Stat(filepath.Join(work, ".git")); err != nil {
			args := []string{"clone", "--quiet"}
			if mirror := filepath.Join(filepath.Dir(g.MirrorDir), filepath.Base(work)+".git"); g.MirrorDir != "" {
				if _, err := os.Stat(mirror); err == nil {
					args = append(args, "--reference-if-able", mirror)
				}
			}
			if _, err := g.runGit_internal(ctx, to, "", append(args, origin, work)...); err != nil {
				return "", err
			}
		}
	}
	// Defect 72 (dogfood 2026-11-07): two jobs staging from the same work
	// clone at once raced on its refs ("fetching ref ... failed: incorrect
	// old value provided") and one staged nothing. One fetch at a time per
	// work clone; the shared clone of it follows under the same lock.
	mu, _ := stageLocks.LoadOrStore(filepath.Clean(work), &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	_, ferr := g.runGit_internal(ctx, to, work, "fetch", "--quiet", "origin", "+refs/heads/*:refs/remotes/origin/*")
	if ferr == nil {
		_ = os.RemoveAll(j.WorktreeDir)
		_, ferr = g.runGit_internal(ctx, to, work, "clone", "--shared", "--no-checkout", work, j.WorktreeDir)
	}
	mu.(*sync.Mutex).Unlock()
	if ferr != nil {
		return "", ferr
	}
	if alt, err := os.ReadFile(filepath.Join(work, ".git", "objects", "info", "alternates")); err == nil && len(alt) > 0 {
		wtAlt := filepath.Join(j.WorktreeDir, ".git", "objects", "info", "alternates")
		cur, _ := os.ReadFile(wtAlt)
		_ = os.WriteFile(wtAlt, append(cur, alt...), 0o644)
	}
	if origin != "" {
		_, _ = g.runGit_internal(ctx, to, j.WorktreeDir, "remote", "set-url", "origin", origin)
	}
	baseRef := cmp.Or(j.PriorHead, j.BaseSHA, "HEAD")
	if j.Kind == "read" {
		if _, err := g.runGit_internal(ctx, to, j.WorktreeDir, "checkout", "--detach", cmp.Or(j.Head, baseRef)); err != nil {
			return "", err
		}
		_, _ = g.runGit_internal(ctx, to, j.WorktreeDir, "remote", "set-url", "--push", "origin", "no-push-on-read")
	} else if _, err := g.runGit_internal(ctx, to, j.WorktreeDir, "checkout", "-B", j.Branch, baseRef); err != nil {
		return "", err
	}
	for _, kv := range [][2]string{{"user.name", j.CommitName}, {"user.email", j.CommitEmail}} {
		if kv[1] != "" {
			_, _ = g.runGit_internal(ctx, to, j.WorktreeDir, "config", kv[0], kv[1])
		}
	}
	return g.HeadSHA(ctx, j.WorktreeDir)
}
func (g *OSGitOps) HeadSHA(ctx context.Context, worktree string) (string, error) {
	out, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Stage), worktree, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}
func (g *OSGitOps) Push(ctx context.Context, worktree, branch, headSHA string) error {
	_, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Push), worktree, "push", "--force", g.originFor_internal(), headSHA+":refs/heads/"+branch)
	return err
}
func (g *OSGitOps) RebaseOntoStream(ctx context.Context, worktree, streamRef string) (string, error) {
	to := g.to_internal(g.Timeouts.WithDefaults().RebaseAndTest)
	if _, err := g.runGit_internal(ctx, to, worktree, "fetch", "--quiet", g.originFor_internal(), streamRef); err != nil {
		return "", &FetchError{StreamRef: streamRef, Cause: err}
	}
	if _, err := g.runGit_internal(ctx, to, worktree, "rebase", "FETCH_HEAD"); err != nil {
		out, _ := g.runGit_internal(ctx, to, worktree, "diff", "--name-only", "--diff-filter=U")
		_, _ = g.runGit_internal(ctx, to, worktree, "rebase", "--abort")
		if files := strings.Fields(out); len(files) > 0 {
			return "", &ConflictError{Files: files}
		}
		return "", err
	}
	return g.HeadSHA(ctx, worktree)
}
func (g *OSGitOps) OriginHasHead(ctx context.Context, repo, branch, sha string) (bool, error) {
	return g.HasHead(ctx, repo, branch, sha)
}
func (g *OSGitOps) HasHead(ctx context.Context, repo, branch, sha string) (bool, error) {
	lsDir, lsOrigin := g.WorkCloneDir, cmp.Or(g.OriginURL, "origin")
	if os.Getenv("NOVA_WORKER_PER_REPO") == "1" && repo != "" { // LOCAL DOGFOOD STOPGAP (defect 42)
		lsDir, lsOrigin = g.perRepo_internal(repo)
	}
	out, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Push), lsDir, "ls-remote", lsOrigin, "refs/heads/"+branch)
	return err == nil && strings.HasPrefix(strings.TrimSpace(out), sha), err
}
func (g *OSGitOps) IsDirty(ctx context.Context, worktree string) (bool, error) {
	out, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Stage), worktree, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != "", err
}
func (g *OSGitOps) RemoveWorktree(_ context.Context, worktree string) error {
	return os.RemoveAll(worktree)
}
func (g *OSGitOps) runGit_internal(ctx context.Context, timeout time.Duration, dir string, args ...string) (string, error) {
	res, err := cmp.Or[edge.Exec](g.Exec, edge.NewOSExec()).Run(ctx, edge.Cmd{
		Argv:     append([]string{"git", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false"}, args...),
		Dir:      dir,
		Env:      []string{"PATH=" + cmp.Or(os.Getenv("PATH"), "/usr/local/bin:/usr/bin:/bin"), "HOME=" + cmp.Or(os.Getenv("HOME"), "/tmp"), "GIT_TERMINAL_PROMPT=0"},
		CleanEnv: true, Timeout: timeout,
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
	fields := splitArgv_internal(cmdStr)
	if len(fields) == 0 {
		return WallRunResult{}, errors.New("empty test command")
	}
	return w.runWallCmd_internal(ctx, j, BuildWallArgv(w.effectiveConfig_internal(j), j, fields), SanitizeWallEnv(os.Environ(), j.JobDir), timeout)
}
func splitArgv_internal(s string) (args []string) {
	var cur strings.Builder
	var quote rune
	inTok := false
	for _, r := range s + " " {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote, inTok = r, true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inTok {
				args, inTok = append(args, cur.String()), false
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
			inTok = true
		}
	}
	return args
}
func (w *OSReserveWall) effectiveConfig_internal(j Job) WallConfig {
	cfg := w.Config
	cfg.ReserveBin = resolveToolBin_internal(cmp.Or(cfg.ReserveBin, "nova-reserve"))
	cfg.SandboxBin = resolveToolBin_internal(cmp.Or(cfg.SandboxBin, "nova-sandbox"))
	if data, err := os.ReadFile(filepath.Join(j.WorktreeDir, ".git")); j.WorktreeDir != "" && err == nil {
		if gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:"); ok {
			commonGit := filepath.Dir(filepath.Dir(strings.TrimSpace(gitDir)))
			cfg.ReadPaths = append(slices.Clone(cfg.ReadPaths), commonGit)
			j.WorktreeDir = filepath.Dir(commonGit)
		}
	}
	if alt, err := os.ReadFile(filepath.Join(j.WorktreeDir, ".git", "objects", "info", "alternates")); j.WorktreeDir != "" && err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(alt)), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				cfg.ReadPaths = append(slices.Clone(cfg.ReadPaths), filepath.Dir(line))
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
	fallbackPID := nextWallPID_internal()
	nonce := fmt.Sprintf("n-%d", fallbackPID)
	res, err := cmp.Or[edge.Exec](w.Exec, edge.NewOSExec()).Run(ctx, edge.Cmd{
		Argv: argv, Dir: cmp.Or(j.WorktreeDir, j.JobDir), Env: append(slices.Clone(env), "NOVA_WORKER_NONCE="+nonce), CleanEnv: len(env) > 0, Timeout: timeout,
		OnStart: func(pid int) {
			if pid <= 0 || pid == os.Getpid() {
				pid = fallbackPID
			}
			liveProcNonces_internal.Store(pid, nonce)
			if rec, rerr := ReadJobRecord(j.JobDir); j.JobDir != "" && rerr == nil && rec.State == JobRunning {
				rec.PID, rec.Nonce = pid, nonce
				_ = WriteJobRecord(j.JobDir, rec)
			}
		},
	})
	combined := res.Stdout
	if res.Stderr != "" {
		combined = strings.TrimSuffix(combined, "\n") + "\n" + res.Stderr
	}
	pid := res.PID
	if pid <= 0 || pid == os.Getpid() {
		pid = fallbackPID
	}
	liveProcNonces_internal.Delete(pid)
	out := WallRunResult{ExitCode: res.ExitCode, Output: []byte(strings.TrimPrefix(combined, "\n")), PID: pid, Nonce: nonce, Elapsed: res.Duration}
	if res.ExitCode == 1 && strings.Contains(combined, "nova-reserve:") && !strings.Contains(combined, "nova-reserve: HELD") {
		return out, ErrNoSlot
	}
	return out, err
}

var ansiRe_local = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

// perRepo_internal is the work clone and origin URL for a stream repo, beside
// the configured work clone (LOCAL DOGFOOD STOPGAP, defect 42).
func (g *OSGitOps) perRepo_internal(repo string) (string, string) {
	name := strings.TrimSuffix(repo[strings.LastIndex(repo, "/")+1:], ".git")
	url := repo
	if !strings.Contains(url, "://") && !strings.Contains(url, "@") {
		url = "https://github.com/" + strings.TrimSuffix(repo, ".git")
	}
	return filepath.Join(filepath.Dir(g.WorkCloneDir), name), url
}

// originFor_internal is "origin" under the per-repo stopgap (the worktree's
// origin was set at staging), else the configured origin URL.
func (g *OSGitOps) originFor_internal() string {
	if os.Getenv("NOVA_WORKER_PER_REPO") == "1" {
		return "origin"
	}
	return cmp.Or(g.OriginURL, "origin")
}
