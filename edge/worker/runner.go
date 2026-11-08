package worker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/store"
	"maps"
	"math"
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
		// Fetch brings j's repo's work clone up to date from its origin
		// (making it on first use). A pass fetches each repo it stages from
		// once, before any of its jobs stage (defect 72).
		Fetch(ctx context.Context, j Job) error
		// StageWorktree makes j's worktree from the work clone as the last
		// Fetch left it; it fetches nothing.
		StageWorktree(ctx context.Context, j Job) (headSHA string, err error)
		// BranchTip is j's branch on origin as the last Fetch left it in the
		// work clone: its sha, or "" when the branch does not exist. A work
		// job's first push leases from it (defect 78).
		BranchTip(ctx context.Context, j Job) (string, error)
		HeadSHA(ctx context.Context, worktree string) (string, error)
		// Push moves branch on the worktree's origin to headSHA only while
		// the branch is at expect ("" : the branch does not exist yet), by
		// lease (--force-with-lease=refs/heads/<branch>:<expect>); never a
		// plain force (defect 78: a stale job's force push rewound the new
		// holder's branch).
		//
		// Push, RebaseOntoStream and OriginHasHead reach remote, the job's
		// clone URL (CloneURL of its record), and refuse a worktree whose
		// origin or url.*.insteadOf now points anywhere else.
		Push(ctx context.Context, worktree, remote, branch, headSHA, expect string) error
		RebaseOntoStream(ctx context.Context, worktree, remote, streamRef string) (newSHA string, err error)
		OriginHasHead(ctx context.Context, worktree, remote, branch, sha string) (bool, error)
		IsDirty(ctx context.Context, worktree string) (bool, error)
		// CommitAll commits every change in the worktree (git add -A) with
		// message, under the worktree's configured identity; the new head.
		CommitAll(ctx context.Context, worktree, message string) (string, error)
		RemoveWorktree(ctx context.Context, worktree string) error
	}
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
	Meter     Meter
	Beater    Beater
	BeatEvery time.Duration
	Now       func() time.Time
	Sleep     func(time.Duration)
	// After is the clock's timer (time.After in the binary): the wake
	// loop's periodic pass every WakeEvery (WakeEvery const when zero) and
	// the probe's backoff wait on it. nil: no periodic pass, and waits are
	// Sleep (a unit test drives the passes itself).
	After     func(time.Duration) <-chan time.Time
	WakeEvery time.Duration
	// RefusalLimit is identical refusals before a record is set aside
	// (RefusalLimit const when zero).
	RefusalLimit int
	// RemoveDir removes a retired job's directory (retireJob_internal); nil
	// is removeTree_internal. A unit test's fault injection.
	RemoveDir                 func(dir string) error
	mu                        *sync.Mutex
	gone                      map[string]int  // per consumer: its jobs of a lower gen hold no branch (holds_internal)
	jobs                      *sync.WaitGroup // every detached job, waited for when the wake loop ends
	fatal                     chan error      // a refused receipt from a detached job ends the wake loop
	probed, withdrawn, served map[string]bool
	tiers                     map[string]string // per row: the tiers its probe proved, a comma list (advertise_internal)
	adopted                   map[string]JobRecord
	running                   map[string]context.CancelFunc
	dedup                     *edge.Deduplicator
	backoffSeq                uint64
	lastBeatErr               string
	advertised                string // the tiers the heartbeat says this harness serves (advertise_internal)
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
	cfg.mu, cfg.probed, cfg.withdrawn, cfg.served, cfg.tiers = &sync.Mutex{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]string{}
	cfg.jobs, cfg.fatal = &sync.WaitGroup{}, make(chan error, 1)
	cfg.adopted, cfg.running, cfg.dedup, cfg.gone = map[string]JobRecord{}, map[string]context.CancelFunc{}, edge.NewDeduplicator(), map[string]int{}
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
func (r *Runner) faultAndReportJob_internal(ctx context.Context, jobDir, id string, rec JobRecord, reason string) error {
	rec.PushedHead = cmp.Or(r.harvestPartialPush_internal(ctx, jobDir, rec), rec.PushedHead)
	rec.ResultID = cmp.Or(rec.ResultID, FormatResultID(rec.Epoch, id, rec.Attempt, rec.Gen))
	rec.Outcome, rec.State = Outcome{Result: "fault", Head: rec.PushedHead, Reason: reason}, JobReporting
	if err := WriteJobRecord(jobDir, rec); err != nil {
		return err
	}
	return r.sendReportAndClean_internal(ctx, jobDir, rec)
}

// withdrawRunningJob_internal stops the harness of a job whose consumer is
// final (withdrawn): it pushes nothing, since the branch is the next
// holder's (defect 78); the caller retires the directory.
func (r *Runner) withdrawRunningJob_internal(rec JobRecord) {
	r.markGone_internal(rec.Epoch, rec.Consumer, math.MaxInt)
	r.cancelRunning_internal(rec.Consumer)
	if r.Procs != nil && rec.PID > 0 {
		r.report("stopping the withdrawn harness of "+rec.Consumer, r.Procs.Kill(rec.PID, rec.Nonce, r.Timeouts.TerminateGrace))
	}
	r.mu.Lock()
	delete(r.adopted, rec.Consumer)
	r.mu.Unlock()
	r.logStep_internal(rec.Row, rec.Consumer, "withdraw", "stopped; nothing pushed", 0)
}

// harvestPartialPush_internal pushes the commits a work job's tree holds,
// once, while its job still holds the consumer (a fault past the deadline,
// a restart's release); the head pushed, or "" when nothing was (a failed
// push is said).
func (r *Runner) harvestPartialPush_internal(ctx context.Context, jobDir string, rec JobRecord) string {
	if rec.Kind != "work" || r.Git == nil {
		return ""
	}
	wt := filepath.Join(jobDir, "repo")
	sha, err := r.Git.HeadSHA(ctx, wt)
	if err != nil || sha == "" || sha == rec.BaseSHA || sha == rec.PriorHead || sha == rec.PushedHead {
		return ""
	}
	if err := r.pushBranch_internal(ctx, wt, &rec, sha); err != nil {
		r.report("pushing the partial head of "+rec.Consumer, err)
		return ""
	}
	return sha
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
// intent rule 34). A failed probe leaves the row unhooked: once, it is the
// exit; served, the row is held and the probe retried (probeHeld_internal).
func (r *Runner) ServeRow(ctx context.Context, row, harness string, once bool) error {
	hook, err := r.busHook_internal(ctx, row)
	if err != nil {
		return err
	}
	defer hook.Stop()
	if once {
		if err := r.ProbeRow(ctx, row, harness); err != nil {
			return err
		}
	} else if !r.probeHeld_internal(ctx, row, harness) {
		return nil // stopped while held: a clean stop
	}
	if err := r.sprintHook_internal(ctx, row); err != nil {
		return err
	}
	r.advertise_internal(ctx, row)
	if once {
		return r.StepRow(ctx, row)
	}
	if r.Meter != nil && r.Beater != nil {
		beatCtx, stop := context.WithCancel(ctx)
		defer stop()
		if m, ok := r.Meter.(OSMeter); ok && m.CPU != nil && r.Now != nil && r.Sleep != nil {
			go m.CPU.Run(beatCtx, r.Now, r.Sleep)
		}
		go r.beatLoop_internal(beatCtx, row)
	}
	return r.wakeLoop_internal(ctx, row, hook)
}

// chooseModel_internal is the model a job runs: the one the deal named and
// the receipt carries (defect 57: the worker never picks one). A harness
// that takes a model and whose probe proved models, none of them this one,
// is a fault naming the model and the fix.
func (r *Runner) chooseModel_internal(harness string, info TakeInfo) (string, error) {
	h, err := r.harnessFor_internal(harness)
	if err != nil || !h.Needs().Model {
		return info.Model, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	proved := slices.DeleteFunc(slices.Sorted(maps.Keys(r.served)), func(m string) bool { return m == "" || !r.served[m] })
	if len(proved) == 0 || r.served[info.Model] {
		return info.Model, nil
	}
	return "", fmt.Errorf("this bench's %s does not serve %s, the model the deal named for tier %s (its probe proved %s): give the tier a bench model this bench serves (nova-sprint policy set <file> --reason <why>), or this bench a route for it (nova-worker --providers)",
		h.Name(), info.Model, info.Tier, strings.Join(proved, ", "))
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

// initJob_internal makes the job directory of a card just taken, with its
// receipt (to resume it after a stop) and its record at taken.
func (r *Runner) initJob_internal(rowInfo NextResponse, info TakeInfo, jobDir string) (Job, JobRecord, error) {
	for _, sub := range []string{"home", "tmp", "data"} {
		if err := os.MkdirAll(filepath.Join(jobDir, sub), 0o755); err != nil && sub == "home" {
			return Job{}, JobRecord{}, err
		}
	}
	job, rec := buildJob_internal(rowInfo, info, jobDir, r.ProxyAddr, r.Now())
	if err := writeReceipt_internal(jobDir, info); err != nil {
		return Job{}, JobRecord{}, err
	}
	if err := r.keepRemote_internal(info); err != nil {
		return Job{}, JobRecord{}, err
	}
	if err := WriteJobRecord(jobDir, rec); err != nil {
		return Job{}, JobRecord{}, err
	}
	r.logStep_internal(rowInfo.Row, info.Consumer, "take", "ok", 0)
	return job, rec, nil
}

// buildJob_internal is the job and its first record from a take receipt.
func buildJob_internal(rowInfo NextResponse, info TakeInfo, jobDir, proxyAddr string, at time.Time) (Job, JobRecord) {
	wt, now := filepath.Join(jobDir, "repo"), at.UTC()
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
		ProxyAddr: proxyAddr, CommitName: info.CommitName,
		CommitEmail: info.CommitEmail, Forge: info.Forge, Deadline: deadline,
	}
	rec := JobRecord{
		Epoch: epoch, Consumer: info.Consumer, Producer: info.Producer, Kind: info.Kind,
		Row: rowInfo.Row, Stream: info.Stream, Repo: info.Repo, Base: info.Base,
		StreamRef: info.StreamRef, Branch: info.Branch, BaseSHA: info.BaseSHA,
		PriorHead: info.PriorHead, TargetHead: info.Head, DoneWhen: cloneStrings_internal(info.DoneWhen),
		Gen: info.Gen, Attempt: info.Attempt, ResultID: FormatResultID(epoch, info.Consumer, info.Attempt, info.Gen),
		State: JobTaken, StartedAt: now.Unix(), DeadlineAt: now.Add(deadline).Unix(), Forge: info.Forge,
	}
	return job, rec
}
func (r *Runner) stageStep_internal(ctx context.Context, job *Job, rec *JobRecord) error {
	start := r.Now()
	if r.Git != nil {
		stageCtx, cancel := context.WithTimeout(ctx, r.Timeouts.Stage)
		_, err := r.Git.StageWorktree(stageCtx, *job)
		if err == nil && job.Kind == "work" {
			rec.StartTip, err = r.Git.BranchTip(stageCtx, *job)
			rec.StartTipRead = err == nil
		}
		cancel()
		if err != nil {
			rec.State, rec.Outcome = JobReporting, Outcome{Result: "fault", Reason: "staging fault: " + err.Error()}
			r.report("job record "+filepath.Base(job.JobDir), WriteJobRecord(job.JobDir, *rec))
			r.logStep_internal(job.Row, job.Consumer, "stage", "fault", r.Now().Sub(start))
			return err
		}
	}
	if err := writeJobMD_internal(*job); err != nil {
		rec.State, rec.Outcome = JobReporting, Outcome{Result: "fault", Reason: "staging fault: " + err.Error()}
		r.report("job record "+filepath.Base(job.JobDir), WriteJobRecord(job.JobDir, *rec))
		return err
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
		out = r.harvestWorkHead_internal(ctx, job, rec, out)
	} else if job.Kind == "read" {
		_, out = ReadOutcomeOf(out)
		out.Head = job.Head
	}
	rec.Outcome, rec.State = out, JobHarvested
	return WriteJobRecord(job.JobDir, *rec)
}

// harvestWorkHead_internal pushes the head a work job left. A harness that
// said ok with its changes uncommitted and HEAD where it started has its
// tree committed here (the harness note of 2026-11-07); ok with nothing to
// commit is a fail naming it.
func (r *Runner) harvestWorkHead_internal(ctx context.Context, job Job, rec *JobRecord, out Outcome) Outcome {
	if r.Git == nil {
		return Outcome{Result: "fault", Reason: "missing git edge for a work consumer: start the worker through nova-worker or nova-sprint worker, which give it one"}
	}
	sha, herr := r.Git.HeadSHA(ctx, job.WorktreeDir)
	if herr == nil && out.Result == "ok" && (sha == job.BaseSHA || sha == job.PriorHead) {
		sha, herr = r.commitLeftTree_internal(ctx, job, sha)
	}
	switch {
	case herr != nil && out.Result == "ok":
		return Outcome{Result: "fault", Reason: "head fault: " + herr.Error()}
	case herr != nil:
		return out
	case sha != "" && sha != job.BaseSHA && sha != job.PriorHead:
		if perr := r.pushBranch_internal(ctx, job.WorktreeDir, rec, sha); perr != nil {
			if out.Result == "ok" {
				return Outcome{Result: "fault", Head: rec.PushedHead, Reason: "push fault: " + perr.Error()}
			}
			return out
		}
		out.Head = sha
	case out.Result == "ok":
		return Outcome{Result: "fail", Reason: "harness reported ok without advancing HEAD past base_sha and prior_head, and left nothing to commit"}
	}
	return out
}

// commitLeftTree_internal commits a dirty work tree the harness left
// uncommitted, under the row's identity, and is the new head; a clean tree
// is head unchanged.
func (r *Runner) commitLeftTree_internal(ctx context.Context, job Job, head string) (string, error) {
	dirty, err := r.Git.IsDirty(ctx, job.WorktreeDir)
	if err != nil || !dirty {
		return head, err
	}
	msg := job.Consumer + ": the harness's work, committed by nova-worker\n\nThe harness reported ok and left its changes uncommitted; the worker committed the tree as it was.\n"
	sha, err := r.Git.CommitAll(ctx, job.WorktreeDir, msg)
	if err != nil {
		return head, fmt.Errorf("commit the tree the harness left: %w", err)
	}
	r.logStep_internal(job.Row, job.Consumer, "commit-left-tree", sha, 0)
	return sha, nil
}
func (r *Runner) rebaseAndTestStep_internal(ctx context.Context, jobDir string, rec *JobRecord) error {
	if rec.Kind != "work" || rec.Outcome.Result != "ok" {
		return nil
	}
	wt := filepath.Join(jobDir, "repo")
	if r.Git != nil && rec.StreamRef != "" {
		remote, err := r.jobRemote_internal(*rec)
		newSHA := ""
		if err == nil {
			newSHA, err = r.Git.RebaseOntoStream(ctx, wt, remote, rec.StreamRef)
		}
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
		if err := r.pushBranch_internal(ctx, wt, rec, rec.Outcome.Head); err != nil {
			// The head that did land is reported, so the next attempt's
			// prior head (and its lease) is the branch as it stands.
			rec.Outcome = Outcome{Result: "fault", Head: rec.PushedHead, Reason: "post-rebase push fault: " + err.Error()}
			r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
			return nil
		}
	}
	rec.State = JobRebased
	r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
	return nil
}
func (r *Runner) sendReportAndClean_internal(ctx context.Context, jobDir string, rec JobRecord) error {
	rec.State = JobReporting
	rec.ResultID = cmp.Or(rec.ResultID, FormatResultID(rec.Epoch, rec.Consumer, rec.Attempt, rec.Gen))
	if err := WriteJobRecord(jobDir, rec); err != nil {
		return err
	}
	out := reportOutcome_internal(rec) // a read's is ok, fix or fault, a stale record's too
	req := ReportRequest{
		Epoch: rec.Epoch, Row: rec.Row, Consumer: rec.Consumer, Gen: rec.Gen,
		ResultID: rec.ResultID, Result: out.Result, Head: out.Head,
		Reason: out.Reason, Finding: out.Finding, Cost: rec.Cost.MicroUSD, Priced: rec.Cost.Priced, Usage: rec.Cost.Usage,
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
		remote, err := r.jobRemote_internal(rec)
		ok := false
		if err == nil {
			ok, err = r.Git.OriginHasHead(ctx, wt, remote, rec.Branch, rec.PushedHead)
		}
		if err != nil || !ok {
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
	if err := os.Remove(filepath.Join(r.RootDir, RemotesDir, rec.Consumer)); err != nil && !errors.Is(err, os.ErrNotExist) {
		r.report("removing the kept clone URL of "+rec.Consumer, err)
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
	// Token and GitHubCredential are every git call's one credential over
	// https (git.CredentialEnv): the token when set, else GitHubCredential's
	// (default gh) login for https://github.com. The host's own credential
	// helpers (~/.gitconfig's) are reset and never asked; the token is in
	// git's environment only, never its argv or the harness's environment.
	Token, GitHubCredential string
	mu                      sync.Mutex
	clones                  map[string]*sync.RWMutex // per clone URL (cloneLock_internal)
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

// cloneLock_internal is url's lock: its write side makes the work clone and
// fetches into it, its read side clones a job's worktree from it, so no
// fetch races another on the clone's refs and no worktree is cloned from a
// half-made one (defect 72).
func (g *OSGitOps) cloneLock_internal(url string) *sync.RWMutex {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.clones == nil {
		g.clones = map[string]*sync.RWMutex{}
	}
	if g.clones[url] == nil {
		g.clones[url] = &sync.RWMutex{}
	}
	return g.clones[url]
}

// cloneURL_internal is j's repo's clone URL and its directory name.
func cloneURL_internal(j Job) (string, string, error) {
	url, err := CloneURL(j.Repo, j.Forge)
	if err != nil {
		return "", "", err
	}
	name, err := repoName_internal(url)
	return url, name, err
}

// workClone_internal is url's work clone, made (with its mirror) on first
// use, and refused when the clone there is another repo's. The caller holds
// url's write lock.
func (g *OSGitOps) workClone_internal(ctx context.Context, url, name string) (string, error) {
	to := g.to_internal(g.Timeouts.WithDefaults().Stage)
	mirror, work := filepath.Join(g.MirrorRoot, name+".git"), filepath.Join(g.WorkRoot, name)
	if g.MirrorRoot != "" && !fileExists_internal(mirror) {
		if _, err := g.runGit_internal(ctx, to, "", "clone", "--quiet", "--mirror", url, mirror); err != nil {
			return "", fmt.Errorf("stage: the mirror of %s: %w", url, err)
		}
	}
	if !fileExists_internal(work) {
		args := []string{"clone", "--quiet", "--no-checkout"}
		if g.MirrorRoot != "" {
			args = append(args, "--reference", mirror)
		}
		if _, err := g.runGit_internal(ctx, to, "", append(args, url, work)...); err != nil {
			return "", fmt.Errorf("stage: the work clone of %s: %w", url, err)
		}
	} else if got, err := g.runGit_internal(ctx, to, work, "remote", "get-url", "origin"); err != nil || strings.TrimSpace(got) != url {
		return "", fmt.Errorf("stage refused: the work clone %s is of %q, not %q (%v): move it aside; the worker makes a new one on first use", work, strings.TrimSpace(got), url, err)
	}
	return work, nil
}

// Fetch brings j's repo's work clone up to date: one fetch of every branch,
// under the clone's write lock.
func (g *OSGitOps) Fetch(ctx context.Context, j Job) error {
	url, name, err := cloneURL_internal(j)
	if err != nil {
		return err
	}
	lock := g.cloneLock_internal(url)
	lock.Lock()
	defer lock.Unlock()
	work, err := g.workClone_internal(ctx, url, name)
	if err != nil {
		return err
	}
	if _, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Stage), work, "fetch", "--quiet", "origin", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return fmt.Errorf("fetch %s: %w", url, err)
	}
	return nil
}

// StageWorktree makes j's worktree, a shared clone of the work clone as the
// pass's Fetch left it, whose origin is the repo.
func (g *OSGitOps) StageWorktree(ctx context.Context, j Job) (string, error) {
	url, name, err := cloneURL_internal(j)
	if err != nil {
		return "", err
	}
	lock := g.cloneLock_internal(url)
	lock.Lock()
	work, err := g.workClone_internal(ctx, url, name)
	lock.Unlock()
	if err != nil {
		return "", err
	}
	to := g.to_internal(g.Timeouts.WithDefaults().Stage)
	if err := os.RemoveAll(j.WorktreeDir); err != nil {
		return "", fmt.Errorf("stage: clear the work tree: %w", err)
	}
	lock.RLock()
	_, err = g.runGit_internal(ctx, to, work, "clone", "--shared", "--no-checkout", work, j.WorktreeDir)
	lock.RUnlock()
	if err != nil {
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
// with no push URL) and sets the sprint's commit identity from the
// receipt; a job with none is refused, never committed as the bench.
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
	if j.CommitName == "" || j.CommitEmail == "" { // a receipt from before the sprint's identity: never the bench's own
		return "", fmt.Errorf("stage: %s has no commit identity in its receipt (taken from an older nova-sprint): the coordinator sets it with nova-sprint identity, and the next deal carries it", j.Consumer)
	}
	for _, kv := range [][2]string{{"user.name", j.CommitName}, {"user.email", j.CommitEmail}} {
		if _, err := g.runGit_internal(ctx, to, j.WorktreeDir, "config", kv[0], kv[1]); err != nil {
			return "", err
		}
	}
	return g.HeadSHA(ctx, j.WorktreeDir)
}

// BranchTip reads j's branch from the work clone's remote-tracking refs
// (the pass's Fetch), under the clone's read lock; "" when it is absent.
func (g *OSGitOps) BranchTip(ctx context.Context, j Job) (string, error) {
	url, name, err := cloneURL_internal(j)
	if err != nil {
		return "", err
	}
	lock := g.cloneLock_internal(url)
	lock.RLock()
	defer lock.RUnlock()
	out, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Stage), filepath.Join(g.WorkRoot, name), "for-each-ref", "--format=%(objectname)", "refs/remotes/origin/"+j.Branch)
	if err != nil {
		return "", fmt.Errorf("stage: read the tip of %s: %w", j.Branch, err)
	}
	return strings.TrimSpace(out), nil
}
func (g *OSGitOps) HeadSHA(ctx context.Context, worktree string) (string, error) {
	to := g.to_internal(g.Timeouts.WithDefaults().Stage)
	if err := g.guardConfig_internal(ctx, to, worktree); err != nil {
		return "", err
	}
	out, err := g.runGit_internal(ctx, to, worktree, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// stagedConfigKey_internal is true for a local config key git writes at
// clone (core.*, origin, the default branch's upstream, and the format
// extensions a clone carries from its source) or the worker sets at stage
// (origin's push URL on a read, the commit identity, the job branch's
// upstream). Nothing else is ever in a staged worktree's .git/config.
func stagedConfigKey_internal(key, value string) bool {
	switch strings.ToLower(key) {
	case "core.repositoryformatversion", "core.filemode", "core.bare", "core.logallrefupdates", "core.ignorecase", "core.precomposeunicode", "core.symlinks",
		"extensions.objectformat", "extensions.refstorage",
		"remote.origin.url", "remote.origin.fetch", "remote.origin.pushurl", "user.name", "user.email":
		return true
	}
	if name, ok := strings.CutPrefix(key, "branch."); ok {
		lower := strings.ToLower(name)
		return strings.HasSuffix(lower, ".merge") || strings.HasSuffix(lower, ".remote") && value == "origin"
	}
	return false
}

// guardConfig_internal runs before every worker git call after the
// harness (which can write the worktree's .git): the worktree's own config
// (git config --local, read with nothing run) holds only the keys staging
// wrote (stagedConfigKey_internal). Any other key (a filter, a
// core.sshCommand, core.fsmonitor or core.hooksPath, an include, a remote
// other than origin, url.*, http.*, credential.*) is refused, so the
// worker's git never runs a command or reaches a server the harness chose.
// Hooks never run in any case: every worker git call passes
// core.hooksPath=/dev/null (runGit_internal).
func (g *OSGitOps) guardConfig_internal(ctx context.Context, to time.Duration, worktree string) error {
	list, err := g.runGit_internal(ctx, to, worktree, "config", "--local", "--null", "--list")
	if err != nil {
		return err
	}
	for _, entry := range strings.Split(list, "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		if key != "" && !stagedConfigKey_internal(key, value) {
			return fmt.Errorf("git refused: the worktree's config has %s, which neither git's clone nor the worker's staging writes: the harness changed .git/config, and the worker's git runs only with the config it staged", key)
		}
	}
	return nil
}

// RemotesDir holds, outside every job directory (which the harness can
// write), the clone URL each job's take receipt named: <root>/remotes/<consumer>.
const RemotesDir = "remotes"

// keepRemote_internal keeps info's clone URL ("" when its repo and forge
// give none) where the harness cannot write, for jobRemote_internal.
func (r *Runner) keepRemote_internal(info TakeInfo) error {
	dir := filepath.Join(r.RootDir, RemotesDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("keep the job's clone URL: %w", err)
	}
	url, _ := CloneURL(info.Repo, info.Forge) // "" on a refusal: staging refuses it with its reason
	if err := os.WriteFile(filepath.Join(dir, info.Consumer), []byte(url+"\n"), 0o600); err != nil {
		return fmt.Errorf("keep the job's clone URL: %w", err)
	}
	return nil
}

// jobRemote_internal is the clone URL rec's job pushes and fetches from:
// the one its take receipt named, kept outside the job directory
// (keepRemote_internal). A job.json whose repo and forge now give another
// is refused: the harness changed it.
func (r *Runner) jobRemote_internal(rec JobRecord) (string, error) {
	data, err := os.ReadFile(filepath.Join(r.RootDir, RemotesDir, rec.Consumer))
	if err != nil {
		return "", fmt.Errorf("git refused: %s has no clone URL kept from its take receipt outside its job directory: %w", rec.Consumer, err)
	}
	kept := strings.TrimSpace(string(data))
	if got := recordRemote_internal(rec); got != kept {
		return "", fmt.Errorf("git refused: job.json's repo %q (forge %q, clone URL %q) is not the take receipt's clone URL %q: the job directory was changed after the take", rec.Repo, rec.Forge, got, kept)
	}
	return kept, nil
}

// recordRemote_internal is the clone URL rec's job was staged from
// (CloneURL of its repo and forge; "" when they give none, which every git
// call that reaches a remote refuses, guardRemote_internal).
func recordRemote_internal(rec JobRecord) string {
	if url, err := CloneURL(rec.Repo, rec.Forge); err == nil {
		return url
	}
	return ""
}

// guardRemote_internal is the check before every git call that reaches
// remote, the job's clone URL as the worker staged it (CloneURL of its
// record): the worktree, which the harness could write, has origin's fetch
// and push URLs still remote, and no url.<base>.insteadOf or
// pushInsteadOf in any config git reads there rewrites remote. Each such
// call then names remote itself, never origin, so neither another repo nor
// another host is ever pushed to or fetched from with the worker's
// credential (#680 cold read).
func (g *OSGitOps) guardRemote_internal(ctx context.Context, to time.Duration, worktree, remote string) error {
	if strings.TrimSpace(remote) == "" {
		return errors.New("git refused: the job names no clone URL to push or fetch: its record's repo and forge give none (CloneURL)")
	}
	if err := g.guardConfig_internal(ctx, to, worktree); err != nil {
		return err
	}
	for _, args := range [][]string{{"remote", "get-url", "origin"}, {"remote", "get-url", "--push", "origin"}} {
		got, err := g.runGit_internal(ctx, to, worktree, args...)
		if err != nil {
			return err
		}
		if strings.TrimSpace(got) != remote {
			return fmt.Errorf("git refused: the worktree's origin (git %s) is %q, not the job's clone URL %q: the worktree was changed after it was staged, and the worker never pushes or fetches anywhere else", strings.Join(args, " "), strings.TrimSpace(got), remote)
		}
	}
	list, err := g.runGit_internal(ctx, to, worktree, "config", "--null", "--list")
	if err != nil {
		return err
	}
	for _, entry := range strings.Split(list, "\x00") {
		key, prefix, _ := strings.Cut(entry, "\n")
		key = strings.ToLower(key)
		if strings.HasPrefix(key, "url.") && (strings.HasSuffix(key, ".insteadof") || strings.HasSuffix(key, ".pushinsteadof")) && prefix != "" && strings.HasPrefix(remote, prefix) {
			return fmt.Errorf("git refused: config %s=%q rewrites the job's clone URL %q: the worker never pushes or fetches through a rewritten URL", key, prefix, remote)
		}
	}
	return nil
}
func (g *OSGitOps) Push(ctx context.Context, worktree, remote, branch, headSHA, expect string) error {
	ref := "refs/heads/" + branch
	to := g.to_internal(g.Timeouts.WithDefaults().Push)
	err := g.guardRemote_internal(ctx, to, worktree, remote)
	if err == nil {
		_, err = g.runGit_internal(ctx, to, worktree, "push", "--force-with-lease="+ref+":"+expect, remote, headSHA+":"+ref)
	}
	if err != nil {
		at := cmp.Or(expect, "no branch (a new one)")
		return fmt.Errorf("push of %s to %s refused or failed: it moves the branch only from %s, the head this job last pushed or started from, and a branch another holder moved is theirs: %w", headSHA, branch, at, err)
	}
	return nil
}
func (g *OSGitOps) RebaseOntoStream(ctx context.Context, worktree, remote, streamRef string) (string, error) {
	to := g.to_internal(g.Timeouts.WithDefaults().RebaseAndTest)
	if err := g.guardRemote_internal(ctx, to, worktree, remote); err != nil {
		return "", err // the worktree's doing, so the job's failure, never a fault to retry
	}
	if _, err := g.runGit_internal(ctx, to, worktree, "fetch", "--quiet", remote, streamRef); err != nil {
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

// OriginHasHead: the branch at remote, the job's clone URL (which the
// worktree's origin must still be, guardRemote_internal), is at sha.
func (g *OSGitOps) OriginHasHead(ctx context.Context, worktree, remote, branch, sha string) (bool, error) {
	if err := g.guardRemote_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Stage), worktree, remote); err != nil {
		return false, err
	}
	return g.HasHead(ctx, remote, branch, sha)
}

// HasHead: the branch of the repository at remote (a clone URL or path) is
// at sha.
func (g *OSGitOps) HasHead(ctx context.Context, remote, branch, sha string) (bool, error) {
	out, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Push), "", "ls-remote", remote, "refs/heads/"+branch)
	return err == nil && strings.HasPrefix(strings.TrimSpace(out), sha), err
}
func (g *OSGitOps) IsDirty(ctx context.Context, worktree string) (bool, error) {
	to := g.to_internal(g.Timeouts.WithDefaults().Stage)
	if err := g.guardConfig_internal(ctx, to, worktree); err != nil {
		return false, err
	}
	out, err := g.runGit_internal(ctx, to, worktree, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != "", err
}
func (g *OSGitOps) CommitAll(ctx context.Context, worktree, message string) (string, error) {
	to := g.to_internal(g.Timeouts.WithDefaults().Stage)
	if err := g.guardConfig_internal(ctx, to, worktree); err != nil {
		return "", err
	}
	if _, err := g.runGit_internal(ctx, to, worktree, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := g.runGit_internal(ctx, to, worktree, "commit", "--quiet", "--no-verify", "-m", message); err != nil {
		return "", err
	}
	return g.HeadSHA(ctx, worktree)
}
func (g *OSGitOps) RemoveWorktree(_ context.Context, worktree string) error {
	return os.RemoveAll(worktree)
}

// gitEnv_internal is every worker git call's whole environment: PATH,
// HOME, no prompt, and the one credential (git.CredentialEnv).
func (g *OSGitOps) gitEnv_internal() []string {
	env := []string{"PATH=" + cmp.Or(os.Getenv("PATH"), "/usr/local/bin:/usr/bin:/bin"), "HOME=" + cmp.Or(os.Getenv("HOME"), "/tmp"), "GIT_TERMINAL_PROMPT=0"}
	return append(env, git.CredentialEnv(g.Token, cmp.Or(g.GitHubCredential, "gh"))...)
}
func (g *OSGitOps) runGit_internal(ctx context.Context, timeout time.Duration, dir string, args ...string) (string, error) {
	cmd := edge.Cmd{
		Argv:     append([]string{"git", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false"}, args...),
		Dir:      dir,
		Env:      g.gitEnv_internal(),
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
