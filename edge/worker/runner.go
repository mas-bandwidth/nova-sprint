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
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
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
		OriginHasHead(ctx context.Context, worktree, branch, sha string) (bool, error)
		IsDirty(ctx context.Context, worktree string) (bool, error)
		RemoveWorktree(ctx context.Context, worktree string) error
	}
	ForgeOps interface {
		EnsurePR(ctx context.Context, repo, branch, base, headSHA, title string) (int, error)
	}
	ForgeAdapter  struct{ Client forge.Forge }
	WallRunResult struct {
		ExitCode, PID  int
		Output         []byte // stdout then stderr, as the classifier reads it
		Stdout, Stderr []byte
		Argv           []string // the wall's command line as run
		Nonce          string
		Elapsed        time.Duration
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
	Warn                               func(string) // a best-effort step's failure; nil: stderr
	// Meter and Beater, both set, make ServeRow push a heartbeat every
	// BeatEvery (the const when zero); a worker without them beats not at all.
	Meter                     Meter
	Beater                    Beater
	BeatEvery                 time.Duration
	Now                       func() time.Time
	Sleep                     func(time.Duration)
	mu                        *sync.Mutex
	probed, withdrawn, served map[string]bool
	adopted                   map[string]JobRecord
	running                   map[string]context.CancelFunc
	dedup                     *edge.Deduplicator
	backoffSeq                uint64
	lastBeatErr               string
}

// NewRunner is a runner on any machine nova-config gives the bench role;
// the worker holds no list of machines of its own (directive 41: the
// coordinator's machine may be a bench too).
func NewRunner(cfg Runner) (*Runner, error) {
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
	cfg.mu, cfg.probed, cfg.withdrawn, cfg.served = &sync.Mutex{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
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

// report writes a best-effort step's failure (a job record, a step log line,
// a cleanup) on the worker's output: the step goes on, never silently.
func (r *Runner) report(what string, err error) { reportTo(r.Warn, what, err) }

// reportTo writes err as a WORKER WARNING line to warn, or stderr when warn
// is nil; nothing for a nil err.
func reportTo(warn func(string), what string, err error) {
	if err == nil {
		return
	}
	line := "WORKER WARNING " + what + ": " + edge.RedactN(err.Error(), edge.CommandMax)
	if warn != nil {
		warn(line)
		return
	}
	fmt.Fprintln(os.Stderr, line)
}

func (r *Runner) logStep_internal(row, consumer, step, res string, d time.Duration) {
	if r.Logger != nil {
		r.report("step log for "+row, r.Logger.Log(row, consumer, step, res, d))
	}
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
			r.report("cleaning job dir "+id, r.cleanJobDir_internal(ctx, filepath.Join(r.RootDir, "jobs", id), rec))
		} else if w, isWorking := workingByID[id]; isWorking && !r.isRunningInFlight_internal(id) {
			if !jobMatchesSummary_internal(next, w, rec) {
				r.logStep_internal(row, id, "recover", "identity-mismatch-kept", 0)
			} else if err := r.recoverWorkingJob_internal(ctx, w, rec); err != nil && !errors.Is(err, ErrStoreDown) {
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
	if rec.State == JobCleaned || rec.State == JobRecorded || rec.State == JobTaken || rec.State == JobStaged {
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
		r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, rec))
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
	hook, err := r.busHook_internal(ctx, row)
	if err != nil {
		return nil, err
	}
	if err := r.sprintHook_internal(ctx, row); err != nil {
		hook.Stop()
		return nil, err
	}
	return hook, nil
}

// busHook_internal is the start's first half: the bus hook, which claims
// the row's name, then the self-unhook of a dead worker's sprint hook.
func (r *Runner) busHook_internal(ctx context.Context, row string) (WakeHook, error) {
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
	return hook, nil
}

// sprintHook_internal is the start's second half: nova-sprint hook.
func (r *Runner) sprintHook_internal(ctx context.Context, row string) error {
	if err := r.Sprint.Hook(ctx, row); err != nil {
		return err
	}
	r.logStep_internal(row, "-", "hook", "ok", 0)
	return nil
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
	return r.wakeLoop_internal(ctx, row, hook)
}

// ServeRow is the binary's order: the bus hook and the self-unhook, then
// the probe of the harness with every model the row would run (ProbeRow:
// next --json names a row's models on the bus before its sprint hook),
// then the sprint hook, then one pass (once) or the wake loop. Probing
// before the sprint hook makes a hooked row a probed one: its harness
// lists the policy's models and its seat's provider keys reached it, which
// is what up's step 7 proves by the row being hooked and up (admission,
// intent rule 34). A failed probe leaves the row unhooked.
func (r *Runner) ServeRow(ctx context.Context, row, harness string, once bool) error {
	hook, err := r.busHook_internal(ctx, row)
	if err != nil {
		return err
	}
	defer hook.Stop()
	if err := r.ProbeRow(ctx, row, harness); err != nil {
		return err
	}
	if err := r.sprintHook_internal(ctx, row); err != nil {
		return err
	}
	if once {
		return r.StepRow(ctx, row)
	}
	if r.Meter != nil && r.Beater != nil {
		beatCtx, stop := context.WithCancel(ctx)
		defer stop()
		go r.beatLoop_internal(beatCtx, row)
	}
	return r.wakeLoop_internal(ctx, row, hook)
}

// wakeLoop_internal is RunWakeLoop's loop on a hook already started.
func (r *Runner) wakeLoop_internal(ctx context.Context, row string, hook WakeHook) error {
	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	passDone, running, pending := make(chan error, 1), false, true
	wait := func() {
		if running {
			cancel()
			<-passDone
		}
	}
	for {
		if pending && !running {
			pending, running = false, true
			go func() { passDone <- r.StepRow(loopCtx, row) }()
		}
		select {
		case <-ctx.Done():
			wait()
			return nil
		case <-hook.Wake():
			pending = true
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
func (r *Runner) StepRow(ctx context.Context, row string) error {
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
	if job.Model, err = r.chooseModel_internal(job.HarnessName, info); err != nil {
		rec.State, rec.Outcome = JobReporting, Outcome{Result: "fault", Reason: err.Error()}
		return r.sendReportAndClean_internal(runCtx, jobDir, rec)
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
		r.report("cleaning job dir", r.cleanJobDir_internal(ctx, jobDir, rec))
		return nil
	}
	if errors.Is(err, ErrNoSlot) {
		return err
	}
	if err := r.harvestStep_internal(runCtx, job, &rec, res, err); err != nil {
		return err
	}
	if err := r.rebaseAndPRSteps_internal(runCtx, jobDir, &rec); err != nil {
		return err
	}
	return r.sendReportAndClean_internal(runCtx, jobDir, rec)
}

// chooseModel_internal is the model a job runs: a friend-shaped receipt's
// own, else the first of a bench's candidates its probe proved this harness
// serves (the policy decides the models per tier; the worker only drops
// those it cannot serve). None is a fault naming the candidates and the fix.
func (r *Runner) chooseModel_internal(harness string, info TakeInfo) (string, error) {
	h, err := r.harnessFor_internal(harness)
	if err != nil || info.Model != "" || !h.Needs().Model {
		return info.Model, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range info.Models {
		if r.served[m] {
			return m, nil
		}
	}
	return "", fmt.Errorf("this bench's %s serves none of the policy's models for tier %s (%s): give the tier a model this bench serves (nova-sprint policy set <file> --reason <why>), or this bench a route for one (nova-worker --providers)",
		h.Name(), info.Tier, strings.Join(info.Models, ", "))
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
		ProxyAddr: r.ProxyAddr, CommitName: rowInfo.Row,
		CommitEmail: rowInfo.Row + "@" + CommitEmailDomain, Forge: info.Forge, Deadline: deadline,
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
			r.report("job record "+filepath.Base(job.JobDir), WriteJobRecord(job.JobDir, *rec))
			r.logStep_internal(job.Row, job.Consumer, "stage", "fault", r.Now().Sub(start))
			return err
		}
	}
	rec.State = JobStaged
	r.report("job record "+filepath.Base(job.JobDir), WriteJobRecord(job.JobDir, *rec))
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
	job.Needs = h.Needs()
	argv, env := h.Command(job)
	rec.State = JobRunning
	if rec.PID <= 0 || rec.PID == os.Getpid() {
		rec.PID = nextWallPID_internal()
		rec.Nonce = fmt.Sprintf("n-%d-%d", rec.PID, r.Now().UnixNano())
	}
	r.report("job record "+filepath.Base(job.JobDir), WriteJobRecord(job.JobDir, *rec))
	var last WallRunResult
	var runErr error
	for attempt := 1; attempt <= r.Timeouts.LaunchTries; attempt++ {
		last, runErr = r.Wall.RunHarness(ctx, job, argv, env, job.Deadline)
		if errors.Is(runErr, ErrNoSlot) {
			rec.State, rec.PID, rec.Nonce = JobStaged, 0, ""
			r.report("job record "+filepath.Base(job.JobDir), WriteJobRecord(job.JobDir, *rec))
			return last, runErr
		}
		if last.PID > 0 && last.PID != os.Getpid() {
			rec.PID, rec.Nonce = last.PID, cmp.Or(last.Nonce, rec.Nonce)
			r.report("job record "+filepath.Base(job.JobDir), WriteJobRecord(job.JobDir, *rec))
		}
		r.report("harness logs", keepLogs_internal(job.JobDir, last))
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
			out = Outcome{Result: "fault", Reason: "missing git edge for a work consumer: start the worker through nova-worker or nova-sprint worker, which give it one"}
		} else if sha, herr := r.Git.HeadSHA(ctx, job.WorktreeDir); herr != nil {
			if out.Result == "ok" {
				out = Outcome{Result: "fault", Reason: "head fault: " + herr.Error()}
			}
		} else if sha != "" && sha != job.BaseSHA && sha != job.PriorHead {
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
				r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
				return fe
			default:
				rec.Outcome = Outcome{Result: "fail", Head: rec.PushedHead, Reason: "rebase failed: " + err.Error()}
			}
			r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
			return nil
		}
		rec.Outcome.Head = cmp.Or(newSHA, rec.Outcome.Head)
	}
	tJob := Job{Consumer: rec.Consumer, Row: rec.Row, JobDir: jobDir, WorktreeDir: wt, ProxyAddr: r.ProxyAddr}
	for _, cmd := range rec.DoneWhen {
		if strings.TrimSpace(cmd) != "" && r.Wall != nil {
			if tres, err := r.Wall.RunTest(ctx, tJob, cmd, r.Timeouts.RebaseAndTest); err != nil || tres.ExitCode != 0 {
				why := TailBytes(tres.Output, MaxReasonBytes)
				if err != nil {
					why = TailBytes([]byte(err.Error()+": "+why), MaxReasonBytes)
				}
				rec.Outcome = Outcome{Result: "fail", Head: rec.PushedHead, Reason: fmt.Sprintf("tests red (%s): %s", cmd, why)}
				r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
				return nil
			}
		}
	}
	if r.Git != nil && rec.Outcome.Head != "" && rec.Outcome.Head != rec.PushedHead {
		if err := r.Git.Push(ctx, wt, rec.Branch, rec.Outcome.Head); err != nil {
			rec.Outcome = Outcome{Result: "fault", Reason: "post-rebase push fault: " + err.Error()}
			r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
			return nil
		}
		rec.PushedHead = rec.Outcome.Head
	}
	rec.State = JobRebased
	if rec.Forge != "" {
		fo := r.Forges[rec.Forge]
		if fo == nil {
			rec.Outcome = Outcome{Result: "fault", Head: rec.PushedHead, Reason: fmt.Sprintf("the stream's forge %q has no adapter on this worker: install a nova-worker that serves it, or set the stream's forge to one this worker serves", rec.Forge)}
			r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
			return nil
		}
		ghCtx, cancel := context.WithTimeout(ctx, r.Timeouts.GH)
		pr, err := fo.EnsurePR(ghCtx, rec.Repo, rec.Branch, rec.Base, rec.PushedHead, rec.Consumer)
		cancel()
		if err != nil {
			rec.Outcome = Outcome{Result: "fault", Head: rec.PushedHead, Reason: "open PR fault: " + err.Error()}
			r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
			return nil
		}
		rec.PRNumber = pr
	}
	r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
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
	r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, rec))
	return r.cleanJobDir_internal(ctx, jobDir, rec)
}
func (r *Runner) cleanJobDir_internal(ctx context.Context, jobDir string, rec JobRecord) error {
	wt := filepath.Join(jobDir, "repo")
	if rec.Kind == "work" && rec.PushedHead != "" && r.Git != nil {
		if ok, err := r.Git.OriginHasHead(ctx, wt, rec.Branch, rec.PushedHead); err != nil || !ok {
			why := "kept-unconfirmed-head"
			if err != nil {
				why += ": " + err.Error()
			}
			r.logStep_internal(rec.Row, rec.Consumer, "clean", why, 0)
			return nil
		}
	}
	if r.Git != nil {
		if dirty, err := r.Git.IsDirty(ctx, wt); err == nil && dirty {
			r.logStep_internal(rec.Row, rec.Consumer, "clean", "DIRTY WORKTREE KEPT", 0)
			return nil
		} else if err != nil {
			r.logStep_internal(rec.Row, rec.Consumer, "clean", "dirty check failed, removing: "+err.Error(), 0)
		}
		r.report("removing worktree "+filepath.Base(wt), r.Git.RemoveWorktree(ctx, wt))
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

// OSGitOps stages from one work clone per stream repo (directive 42): the
// repo is the deal receipt's; its bare mirror is <MirrorRoot>/<name>.git,
// the layout mirror-refresh keeps, cloned on first use when absent; its work
// clone is <WorkRoot>/<name>, made on first use borrowing the mirror's
// objects. A job's worktree is a shared clone of the work clone whose origin
// is the repo's clone URL, so its fetches and pushes go to the repo.
type OSGitOps struct {
	WorkRoot, MirrorRoot string
	Exec                 edge.Exec
	Timeouts             Timeouts
	Timeout              time.Duration
	mu                   sync.Mutex
	clones               map[string]*sync.Mutex // per clone URL: first use is made once
}

var orgRepo_internal = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// CloneURL is the URL a stream's repo is cloned from: a URL or an absolute
// path as it is; owner/name on the github forge, https://github.com/owner/name.git.
func CloneURL(repo, forgeName string) (string, error) {
	repo = strings.TrimSpace(repo)
	switch {
	case repo == "":
		return "", errors.New("stage refused: the receipt names no repo: set the stream's repo (nova-sprint stream set)")
	case strings.Contains(repo, "://") || strings.HasPrefix(repo, "/") || strings.HasPrefix(repo, "git@"):
		return repo, nil
	case forgeName == "github" && orgRepo_internal.MatchString(repo):
		return "https://github.com/" + strings.TrimSuffix(repo, ".git") + ".git", nil
	}
	return "", fmt.Errorf("stage refused: repo %q is not a URL or a path, and forge %q gives no clone URL for it: set the stream's repo to a clone URL, or its forge to github", repo, forgeName)
}

// repoName_internal is the clone's directory name: the URL's last element
// without .git, as mirror-refresh names its mirrors.
func repoName_internal(cloneURL string) (string, error) {
	name := strings.TrimSuffix(filepath.Base(strings.TrimRight(strings.ReplaceAll(cloneURL, ":", "/"), "/")), ".git")
	if name == "" || name == "." || name == ".." || name == "/" {
		return "", fmt.Errorf("stage refused: clone URL %q names no repository: set the stream's repo to <owner>/<name> or a clone URL", cloneURL)
	}
	return name, nil
}

func (g *OSGitOps) to_internal(d time.Duration) time.Duration { return cmp.Or(g.Timeout, d) }

// workClone_internal is j's repo's work clone, made (with its mirror) on
// first use, and refused when the clone there is another repo's.
func (g *OSGitOps) workClone_internal(ctx context.Context, j Job) (string, string, error) {
	url, err := CloneURL(j.Repo, j.Forge)
	if err != nil {
		return "", "", err
	}
	name, err := repoName_internal(url)
	if err != nil {
		return "", "", err
	}
	g.mu.Lock()
	if g.clones == nil {
		g.clones = map[string]*sync.Mutex{}
	}
	lock := g.clones[url]
	if lock == nil {
		lock = &sync.Mutex{}
		g.clones[url] = lock
	}
	g.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	to := g.to_internal(g.Timeouts.WithDefaults().Stage)
	mirror, work := filepath.Join(g.MirrorRoot, name+".git"), filepath.Join(g.WorkRoot, name)
	if g.MirrorRoot != "" && !fileExists_internal(mirror) {
		if _, err := g.runGit_internal(ctx, to, "", "clone", "--quiet", "--mirror", url, mirror); err != nil {
			return "", "", fmt.Errorf("stage: the mirror of %s: %w", url, err)
		}
	}
	if !fileExists_internal(work) {
		args := []string{"clone", "--quiet", "--no-checkout"}
		if g.MirrorRoot != "" {
			args = append(args, "--reference", mirror)
		}
		if _, err := g.runGit_internal(ctx, to, "", append(args, url, work)...); err != nil {
			return "", "", fmt.Errorf("stage: the work clone of %s: %w", url, err)
		}
	} else if got, err := g.runGit_internal(ctx, to, work, "remote", "get-url", "origin"); err != nil || strings.TrimSpace(got) != url {
		return "", "", fmt.Errorf("stage refused: the work clone %s is of %q, not %q (%v): move it aside; the worker makes a new one on first use", work, strings.TrimSpace(got), url, err)
	}
	return work, url, nil
}
func (g *OSGitOps) StageWorktree(ctx context.Context, j Job) (string, error) {
	work, url, err := g.workClone_internal(ctx, j)
	if err != nil {
		return "", err
	}
	to := g.to_internal(g.Timeouts.WithDefaults().Stage)
	if _, err := g.runGit_internal(ctx, to, work, "fetch", "--quiet", "origin", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return "", err
	}
	if err := os.RemoveAll(j.WorktreeDir); err != nil {
		return "", fmt.Errorf("stage: clear the work tree: %w", err)
	}
	if _, err := g.runGit_internal(ctx, to, work, "clone", "--shared", "--no-checkout", work, j.WorktreeDir); err != nil {
		return "", err
	}
	if alt, err := os.ReadFile(filepath.Join(work, ".git", "objects", "info", "alternates")); err == nil && len(alt) > 0 {
		wtAlt := filepath.Join(j.WorktreeDir, ".git", "objects", "info", "alternates")
		cur, err := os.ReadFile(wtAlt)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("stage: read the work tree's alternates: %w", err)
		}
		if err := os.WriteFile(wtAlt, append(cur, alt...), 0o644); err != nil {
			return "", fmt.Errorf("stage: write the work tree's alternates: %w", err)
		}
	}
	if _, err := g.runGit_internal(ctx, to, j.WorktreeDir, "remote", "set-url", "origin", url); err != nil {
		return "", err
	}
	return g.checkoutJob_internal(ctx, to, j)
}

// checkoutJob_internal checks out j's base (a read: detached at its head,
// with no push URL) and sets the row's commit identity.
func (g *OSGitOps) checkoutJob_internal(ctx context.Context, to time.Duration, j Job) (string, error) {
	baseRef := cmp.Or(j.PriorHead, j.BaseSHA, "HEAD")
	if j.Kind == "read" {
		if _, err := g.runGit_internal(ctx, to, j.WorktreeDir, "checkout", "--detach", cmp.Or(j.Head, baseRef)); err != nil {
			return "", err
		}
		if _, err := g.runGit_internal(ctx, to, j.WorktreeDir, "remote", "set-url", "--push", "origin", "no-push-on-read"); err != nil {
			return "", err
		}
	} else if _, err := g.runGit_internal(ctx, to, j.WorktreeDir, "checkout", "-B", j.Branch, baseRef); err != nil {
		return "", err
	}
	for _, kv := range [][2]string{{"user.name", j.CommitName}, {"user.email", j.CommitEmail}} {
		if kv[1] != "" {
			if _, err := g.runGit_internal(ctx, to, j.WorktreeDir, "config", kv[0], kv[1]); err != nil {
				return "", err
			}
		}
	}
	return g.HeadSHA(ctx, j.WorktreeDir)
}
func (g *OSGitOps) HeadSHA(ctx context.Context, worktree string) (string, error) {
	out, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Stage), worktree, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}
func (g *OSGitOps) Push(ctx context.Context, worktree, branch, headSHA string) error {
	_, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Push), worktree, "push", "--force", "origin", headSHA+":refs/heads/"+branch)
	return err
}
func (g *OSGitOps) RebaseOntoStream(ctx context.Context, worktree, streamRef string) (string, error) {
	to := g.to_internal(g.Timeouts.WithDefaults().RebaseAndTest)
	if _, err := g.runGit_internal(ctx, to, worktree, "fetch", "--quiet", "origin", streamRef); err != nil {
		return "", &FetchError{StreamRef: streamRef, Cause: err}
	}
	if _, err := g.runGit_internal(ctx, to, worktree, "rebase", "FETCH_HEAD"); err != nil {
		out, diffErr := g.runGit_internal(ctx, to, worktree, "diff", "--name-only", "--diff-filter=U")
		if _, abortErr := g.runGit_internal(ctx, to, worktree, "rebase", "--abort"); abortErr != nil {
			err = fmt.Errorf("%w; cleanup: %w", err, abortErr)
		}
		if files := strings.Fields(out); diffErr == nil && len(files) > 0 {
			return "", &ConflictError{Files: files}
		}
		if diffErr != nil {
			err = fmt.Errorf("%w; listing conflicts: %w", err, diffErr)
		}
		return "", err
	}
	return g.HeadSHA(ctx, worktree)
}

// OriginHasHead: the branch on the worktree's origin (the repo) is at sha.
func (g *OSGitOps) OriginHasHead(ctx context.Context, worktree, branch, sha string) (bool, error) {
	url, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Stage), worktree, "remote", "get-url", "origin")
	if err != nil {
		return false, err
	}
	return g.HasHead(ctx, strings.TrimSpace(url), branch, sha)
}

// HasHead: the branch of the repository at remote (a clone URL or path) is
// at sha.
func (g *OSGitOps) HasHead(ctx context.Context, remote, branch, sha string) (bool, error) {
	out, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Push), "", "ls-remote", remote, "refs/heads/"+branch)
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
	cmd := edge.Cmd{
		Argv:     append([]string{"git", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false"}, args...),
		Dir:      dir,
		Env:      []string{"PATH=" + cmp.Or(os.Getenv("PATH"), "/usr/local/bin:/usr/bin:/bin"), "HOME=" + cmp.Or(os.Getenv("HOME"), "/tmp"), "GIT_TERMINAL_PROMPT=0"},
		CleanEnv: true, Timeout: timeout,
	}
	res, err := cmp.Or[edge.Exec](g.Exec, edge.NewOSExec()).Run(ctx, cmd)
	if err != nil || res.ExitCode != 0 {
		return res.Stdout + res.Stderr, edge.Failure(cmd.Argv, nil, res.ExitCode, res.Stdout, res.Stderr, err)
	}
	return res.Stdout, nil
}

type OSReserveWall struct {
	Config     WallConfig
	UseSandbox bool
	Exec       edge.Exec
	LookPath   func(string) (string, error) // exec.LookPath when nil
}

// RunHarness runs the harness in the wall, granting --read on the
// directories its binary lives in (HarnessDirs).
func (w *OSReserveWall) RunHarness(ctx context.Context, j Job, argv, env []string, timeout time.Duration) (WallRunResult, error) {
	cfg := w.effectiveConfig_internal(j)
	if len(argv) > 0 {
		lookPath := w.LookPath
		if lookPath == nil {
			lookPath = exec.LookPath
		}
		cfg.ReadPaths = append(slices.Clone(cfg.ReadPaths), HarnessDirs(argv[0], lookPath)...)
	}
	return w.runWallCmd_internal(ctx, j, cfg, argv, SanitizeWallEnv(env, j.JobDir), timeout)
}
func (w *OSReserveWall) RunTest(ctx context.Context, j Job, cmdStr string, timeout time.Duration) (WallRunResult, error) {
	fields, why := edge.ParseGate(cmdStr)
	if why != "" {
		return WallRunResult{}, fmt.Errorf("gate %q is not one command: %s: %s", cmdStr, why, edge.GateFix)
	}
	return w.runWallCmd_internal(ctx, j, w.effectiveConfig_internal(j), fields, SanitizeWallEnv(os.Environ(), j.JobDir), timeout)
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
func (w *OSReserveWall) runWallCmd_internal(ctx context.Context, j Job, cfg WallConfig, cmdArgv, env []string, timeout time.Duration) (WallRunResult, error) {
	fallbackPID := nextWallPID_internal()
	nonce := fmt.Sprintf("n-%d", fallbackPID)
	env = append(slices.Clone(env), "NOVA_WORKER_NONCE="+nonce)
	argv := BuildWallArgv(cfg, j, env, cmdArgv)
	res, err := cmp.Or[edge.Exec](w.Exec, edge.NewOSExec()).Run(ctx, edge.Cmd{
		Argv: argv, Dir: cmp.Or(j.WorktreeDir, j.JobDir), Env: env, CleanEnv: true, Timeout: timeout,
		OnStart: func(pid int) {
			if pid <= 0 || pid == os.Getpid() {
				pid = fallbackPID
			}
			liveProcNonces_internal.Store(pid, nonce)
			if rec, rerr := ReadJobRecord(j.JobDir); j.JobDir != "" && rerr == nil && rec.State == JobRunning {
				rec.PID, rec.Nonce = pid, nonce
				reportTo(nil, "job record "+filepath.Base(j.JobDir), WriteJobRecord(j.JobDir, rec))
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
	out := WallRunResult{ExitCode: res.ExitCode, Output: []byte(strings.TrimPrefix(combined, "\n")), Stdout: []byte(res.Stdout), Stderr: []byte(res.Stderr),
		Argv: argv, PID: pid, Nonce: nonce, Elapsed: res.Duration}
	if res.ExitCode == 1 && strings.Contains(combined, "nova-reserve:") && !strings.Contains(combined, "nova-reserve: HELD") {
		return out, fmt.Errorf("%w: %w", ErrNoSlot, edge.Failure(argv, nil, res.ExitCode, res.Stdout, res.Stderr, nil))
	}
	if err != nil {
		// The run never completed (a timeout, a missing binary): the
		// command, its exit code and its output so far go with it.
		return out, edge.Failure(argv, nil, res.ExitCode, res.Stdout, res.Stderr, err)
	}
	return out, nil
}
