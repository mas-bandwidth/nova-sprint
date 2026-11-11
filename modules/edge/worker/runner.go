package worker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"io"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
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
		// Fetch brings the work clone of js's repo (one repo) up to date
		// from its origin with what js check out, shallow (making it on
		// first use). A pass fetches each repo it stages from once, for all
		// its jobs of that repo, before any of them stage (defect 72).
		Fetch(ctx context.Context, js ...Job) error
		// StageWorktree makes j's worktree from the work clone as the last
		// Fetch left it; it fetches nothing.
		StageWorktree(ctx context.Context, j Job) (headSHA string, err error)
		// BranchTip is j's branch on origin as the last Fetch left it in the
		// work clone: its sha, or "" when the branch does not exist. A work
		// job's first push leases from it (defect 78).
		BranchTip(ctx context.Context, j Job) (string, error)
		// Collect takes a sealed job's commits into the worker's own clone
		// of it, outside the job directory: the seal step's bundle
		// (sealArgv_internal), verified and fetched into s.Clone, a fresh
		// shared clone of the work clone. Its head must descend from s.Base
		// and its commits add no gitlink. The head, checked out in s.Clone;
		// s.Base when the seal found no commits.
		Collect(ctx context.Context, s Sealed) (head string, err error)
		// CheckOut checks sha out, detached, in clone, the worker's own
		// clone of a job: after a Collect whose seal found no commits
		// nothing is checked out there, and the stream merges into the head
		// the job started from (rebaseAndTestStep_internal).
		CheckOut(ctx context.Context, clone, sha string) error
		// Export writes clone's commits since base to out, a bundle the wall
		// applies to the job's tree before its done-when tests.
		Export(ctx context.Context, clone, base, out string) error
		// Push moves branch on remote to headSHA only while the branch is at
		// expect ("" : the branch does not exist yet), by lease
		// (--force-with-lease=refs/heads/<branch>:<expect>); never a plain
		// force (defect 78: a stale job's force push rewound the new holder's
		// branch).
		//
		// Push, RebaseOntoStream and OriginHasHead run in clone, the worker's
		// own clone of the job (Collect), never in the job directory, and
		// reach remote, the clone URL the job's take receipt named.
		Push(ctx context.Context, clone, remote, branch, headSHA, expect string) error
		RebaseOntoStream(ctx context.Context, clone, remote, streamRef string) (newSHA string, err error)
		// StreamTip is the stream tip RebaseOntoStream last fetched and
		// merged in clone (its FETCH_HEAD): the base a stream gate red at
		// the merged head is judged against (baseGateRed_internal).
		StreamTip(ctx context.Context, clone string) (string, error)
		OriginHasHead(ctx context.Context, clone, remote, branch, sha string) (bool, error)
		RemoveWorktree(ctx context.Context, worktree string) error
	}
	// Sealed is a sealed job for Collect: its directory, the worker's
	// clone of it, its clone URL, the head it started from, and the
	// sprint's commit identity for the clone's rebase.
	Sealed        struct{ JobDir, Clone, Remote, Base, Name, Email string }
	WallRunResult struct {
		ExitCode, PID  int
		Leftovers      int    // processes of the command's group still alive after it, killed (OSReserveWall.KillGroup)
		LeftoverErr    error  // KillGroup's failure to count or kill them, said on the step's log line
		Output         []byte // stdout then stderr, as the classifier reads it
		Stdout, Stderr []byte
		Argv           []string // the wall's command line as run
		Nonce          string
		Elapsed        time.Duration
	}
	ReserveWall interface {
		RunHarness(ctx context.Context, j Job, argv, env []string, timeout time.Duration) (WallRunResult, error)
		RunTest(ctx context.Context, j Job, cmd string, timeout time.Duration) (WallRunResult, error)
		// RunStep runs one of the worker's own commands in the wall: the
		// seal step, the apply of a rebased head.
		RunStep(ctx context.Context, j Job, argv []string, timeout time.Duration) (WallRunResult, error)
	}
	ProcessTable interface {
		Alive(pid int, nonce string) bool
		Kill(pid int, nonce string, grace time.Duration) error
	}
	OSProcessTable struct{}
)

func (e *ConflictError) Error() string { return "merge conflict: " + strings.Join(e.Files, ", ") }
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
	RemoveDir func(dir string) error
	// KillJob ends every process of the job at a job directory
	// (endJobTree_internal; OSKillJob in the binary) before the worker lets
	// the job go: the clean, a retire, a quarantine. nil: none.
	KillJob                   func(jobDir string) (int, error)
	mu                        *sync.Mutex
	gone                      map[string]int  // per consumer: its jobs of a lower gen hold no branch (holds_internal)
	jobs                      *sync.WaitGroup // every detached job, waited for when the wake loop ends
	fatal                     chan error      // a refused receipt from a detached job ends the wake loop
	probed, withdrawn, served map[string]bool
	tiers                     map[string]string // per row: the tiers its probe proved, a comma list (advertise_internal)
	running                   map[string]context.CancelFunc
	backoffSeq                uint64
	lastBeatErr               string
	advertised                string          // the tiers the heartbeat says this harness serves (advertise_internal)
	baseGates                 map[string]bool // per repository, base sha and stream gate: green there (baseGateRed_internal)
}

// NewRunner is a runner on any machine nova-config gives the bench role;
// the worker holds no list of machines of its own (directive 41: the
// coordinator's machine may be a bench too).
func NewRunner(cfg Runner) (*Runner, error) {
	if strings.TrimSpace(cfg.RootDir) == "" {
		return nil, errors.New("worker refused: root directory is empty: pass --root <dir>")
	}
	if cfg.Now == nil || cfg.Sleep == nil {
		return nil, errors.New("worker refused: no clock: the caller passes Now and Sleep (the real clock in nova-sprint worker, a fake one in a unit test)")
	}
	cfg.Timeouts = cfg.Timeouts.WithDefaults()
	cfg.Procs = cmp.Or[ProcessTable](cfg.Procs, OSProcessTable{})
	if cfg.Wall == nil {
		grace := cfg.Timeouts.TerminateGrace
		cfg.Wall = &OSReserveWall{UseSandbox: true, Exec: cfg.Exec, KillGroup: OSKillGroup, KillJob: func(d string) (int, error) { return OSKillJob(cfg.RootDir, d, grace) },
			Config: WallConfig{ReserveWait: cfg.Timeouts.ReserveWait, NetDeny: true}}
		if cfg.KillJob == nil {
			cfg.KillJob = cfg.Wall.(*OSReserveWall).KillJob
		}
	}
	if cfg.Logger == nil && cfg.StateDir != "" {
		cfg.Logger = &StepLogger{StateDir: cfg.StateDir, Now: cfg.Now}
	}
	cfg.mu, cfg.probed, cfg.withdrawn, cfg.served, cfg.tiers = &sync.Mutex{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]string{}
	cfg.jobs, cfg.fatal = &sync.WaitGroup{}, make(chan error, 1)
	cfg.running, cfg.gone, cfg.baseGates = map[string]context.CancelFunc{}, map[string]int{}, map[string]bool{}
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

// killTracked_internal kills the tracked harness pid carrying nonce, and
// says so when no live process matched: Kill finds nothing then (a worker
// restart on macOS), and a harness left running must not go unseen.
func (r *Runner) killTracked_internal(what string, pid int, nonce string) {
	if r.Procs == nil {
		return
	}
	if !r.Procs.Alive(pid, nonce) {
		r.report(what, fmt.Errorf("no live process matched pid %d with nonce %s", pid, nonce))
		return
	}
	r.report(what, r.Procs.Kill(pid, nonce, r.Timeouts.TerminateGrace))
}

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
		dir := filepath.Join(r.RootDir, "jobs", e.Name())
		rec, err := ReadJobRecord(dir)
		if err == nil {
			if _, rerr := readKept_internal(dir, ReceiptName); errors.Is(rerr, ErrJobDirChanged) {
				err = rerr
			}
		}
		if err != nil && !errors.Is(err, ErrJobDirChanged) && !r.isRunningInFlight_internal(e.Name()) { // a job in flight is still writing its record
			r.report("job record "+dir+" (unreadable: left as it is, nothing pushed or reported)", err)
		}
		if !e.IsDir() && errors.Is(err, ErrJobDirChanged) {
			r.report("job "+e.Name()+" (a file, not a job directory: left as it is, nothing pushed or reported)", err)
		}
		if e.IsDir() && errors.Is(err, ErrJobDirChanged) && !r.isRunningInFlight_internal(e.Name()) { // a job in flight writes its own record
			r.quarantineChanged_internal(dir, e.Name(), err)
			continue
		}
		if e.IsDir() && err == nil && rec.Row == row {
			out[rec.Consumer] = rec
		}
	}
	return out, nil
}
// withdrawRunningJob_internal stops the harness of a job whose consumer is
// final (withdrawn): it pushes nothing, since the branch is the next
// holder's (defect 78); the caller retires the directory.
func (r *Runner) withdrawRunningJob_internal(rec JobRecord) {
	r.markGone_internal(rec.Epoch, rec.Consumer, math.MaxInt)
	r.cancelRunning_internal(rec.Consumer)
	if rec.PID > 0 {
		r.killTracked_internal("stopping the withdrawn harness of "+rec.Consumer, rec.PID, rec.Nonce)
	}
	r.logStep_internal(rec.Row, rec.Consumer, "withdraw", "stopped; nothing pushed", 0)
}

// harvestPartialPush_internal pushes the commits a work job's tree holds,
// once, while its job still holds the consumer (a release with no blame);
// the head pushed, or "" when nothing was (a failed push is said).
func (r *Runner) harvestPartialPush_internal(ctx context.Context, jobDir string, rec JobRecord) string {
	if rec.Kind != "work" || r.Git == nil {
		return ""
	}
	job := Job{Consumer: rec.Consumer, Row: rec.Row, Repo: rec.Repo, Forge: rec.Forge, JobDir: jobDir, WorktreeDir: filepath.Join(jobDir, "repo"), ProxyAddr: r.ProxyAddr}
	sha, err := r.sealAndCollect_internal(ctx, job, rec)
	if err != nil {
		r.report("sealing the tree of "+rec.Consumer, err)
		return ""
	}
	if sha == "" || sha == rec.BaseSHA || sha == rec.PriorHead || sha == rec.PushedHead {
		return ""
	}
	if err := r.pushBranch_internal(ctx, &rec, sha); err != nil {
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

// busHook_internal is the start's first half: the bus hook, which claims
// the row's name, then the self-unhook of a dead worker's sprint hook. The
// unhook gives the row's whole hand back to the queue in its op (released,
// no blame, branches and pushed heads kept), so a restart resumes nothing:
// the dealer deals those cards again (tla/Runner.tla Restart).
func (r *Runner) busHook_internal(ctx context.Context, row string) (WakeHook, error) {
	if r.BusHook == nil {
		return nil, errors.New("worker refused: no bus hook configured: start the worker through nova-sprint worker")
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

// ServeRow is the binary's order: the bus hook and the self-unhook, then
// the probe of the harness with every model the row would run (ProbeRow:
// next --json names a row's models on the bus before its sprint hook),
// then the sprint hook, then the wake loop. Probing
// before the sprint hook makes a hooked row a probed one: its harness
// lists the policy's models and its seat's provider keys reached it, which
// is what up's workers step (9) proves by the row being hooked and up (admission,
// intent rule 34). A failed probe leaves the row unhooked and held, and the
// probe is retried (probeHeld_internal).
func (r *Runner) ServeRow(ctx context.Context, row, harness string) error {
	hook, err := r.busHook_internal(ctx, row)
	if err != nil {
		return err
	}
	defer hook.Stop()
	if !r.probeHeld_internal(ctx, row, harness) {
		return nil // stopped while held: a clean stop
	}
	if err := r.sprintHook_internal(ctx, row); err != nil {
		return err
	}
	r.advertise_internal(ctx, row)
	if r.Meter != nil && r.Beater != nil {
		beatCtx, stop := context.WithCancel(ctx)
		defer stop()
		if m, ok := r.Meter.(OSMeter); ok && m.CPU != nil && r.Now != nil && r.Sleep != nil {
			go m.CPU.Run(beatCtx, r.Now, r.Sleep, r.report)
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
	return "", fmt.Errorf("this bench's %s does not serve %s, the model the deal named for tier %s (its probe proved %s): give the tier a bench model this bench serves (nova-sprint policy set <file> --reason <why>), or this bench a route for it (nova-sprint worker --providers)",
		h.Name(), info.Model, info.Tier, strings.Join(proved, ", "))
}

// refuseReceipt_internal reports the card taken under a receipt the worker
// cannot use as a fault naming the fix, through the durable path (a job
// record at state reporting), and returns the refusal, which ends the worker.
func (r *Runner) refuseReceipt_internal(ctx context.Context, jobDir string, rowInfo NextResponse, c ConsumerSummary, cause error) error {
	rec := JobRecord{
		Epoch: rowInfo.Epoch, Consumer: c.ID, Producer: c.Producer, Kind: c.Kind, Row: rowInfo.Row, Stream: c.Stream,
		Gen: c.Gen, Attempt: c.Attempt, ResultID: resultID_internal(rowInfo.Epoch, c.ID, c.Gen),
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
// receipt (for a later pass to go on with it) and its record at taken.
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
		DoneWhen: info.DoneWhen, StreamGates: info.StreamGates, Gen: info.Gen, Attempt: info.Attempt,
		Review: info.Review, JobDir: jobDir, WorktreeDir: wt,
		ProxyAddr: proxyAddr, CommitName: info.CommitName,
		CommitEmail: info.CommitEmail, Forge: info.Forge, Deadline: deadline,
	}
	rec := JobRecord{
		Epoch: epoch, Consumer: info.Consumer, Producer: info.Producer, Kind: info.Kind,
		Row: rowInfo.Row, Stream: info.Stream, Repo: info.Repo, Base: info.Base,
		StreamRef: info.StreamRef, Branch: info.Branch, BaseSHA: info.BaseSHA,
		PriorHead: info.PriorHead, TargetHead: info.Head, DoneWhen: cloneStrings_internal(info.DoneWhen), StreamGates: cloneStrings_internal(info.StreamGates),
		Gen: info.Gen, Attempt: info.Attempt, ResultID: resultID_internal(epoch, info.Consumer, info.Gen),
		State: JobTaken, StartedAt: now.Unix(), DeadlineAt: now.Add(deadline).Unix(), Forge: info.Forge,
		PriorFinished: info.PriorFinished,
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
	job.Needs, job.Routes = h.Needs(), r.Proxy.RouteNames()
	r.report("harness config for "+job.Consumer, wallRefusal_internal(h, job))
	argv, env := h.Command(job)
	rec.State = JobRunning
	if rec.PID <= 0 || rec.PID == os.Getpid() {
		rec.PID = nextWallPID_internal()
		rec.Nonce = fmt.Sprintf("n-%d-%d", rec.PID, r.Now().UnixNano())
	}
	r.report("job record "+filepath.Base(job.JobDir), WriteJobRecord(job.JobDir, *rec))
	var last WallRunResult
	var runErr error
	r.Proxy.ConsumeStatus(job.Consumer) // a provider status from before this run is not this run's
	for attempt := 1; attempt <= r.Timeouts.LaunchTries; attempt++ {
		// a verdict is this run's: an earlier run's is never read as it
		if err := os.Remove(filepath.Join(job.JobDir, "result.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return WallRunResult{}, fmt.Errorf("worker refused: an earlier run's result.json could not be removed: %w", err)
		}
		last, runErr = r.Wall.RunHarness(ctx, job, argv, env, job.Deadline)
		if errors.Is(runErr, ErrNoSlot) {
			rec.State, rec.PID, rec.Nonce = JobStaged, 0, ""
			r.report("job record "+filepath.Base(job.JobDir), WriteJobRecord(job.JobDir, *rec))
			r.logStep_internal(job.Row, job.Consumer, "run", "no nova-reserve slot on "+r.Host+"; staged, waiting for one", 0)
			return last, runErr
		}
		if last.PID > 0 && last.PID != os.Getpid() {
			rec.PID, rec.Nonce = last.PID, cmp.Or(last.Nonce, rec.Nonce)
			r.report("job record "+filepath.Base(job.JobDir), WriteJobRecord(job.JobDir, *rec))
		}
		r.report("harness logs", keepLogs_internal(job.JobDir, last))
		job.ProxyStatus = cmp.Or(job.ProxyStatus, r.Proxy.ConsumeStatus(job.Consumer))
		out := ClassifyJob(h, job, last.ExitCode, last.Elapsed, r.Timeouts.LaunchGrace, last.Output)
		if !out.Retryable5xx || attempt == r.Timeouts.LaunchTries {
			// the harvest classifies this launch from the same status: read
			// here it was forgotten, and the harvest saw none
			r.Proxy.RecordStatus(job.Consumer, job.ProxyStatus)
			break
		}
		job.ProxyStatus = 0
		r.Sleep(r.launchBackoff_internal(attempt))
	}
	r.logStep_internal(job.Row, job.Consumer, "run", fmt.Sprintf("exit=%d %s", last.ExitCode, leftoversNote_internal(last)), last.Elapsed)
	r.report("killing what the harness of "+job.Consumer+" left running", last.LeftoverErr)
	return last, runErr
}
func isInfraFault_internal(res WallRunResult, runErr error) (bool, string) {
	if runErr != nil {
		return true, "wall execution fault: " + runErr.Error()
	}
	if res.ExitCode == 0 {
		return false, ""
	}
	tail := TailBytes(res.Output, 512)
	if strings.Contains(tail, "SANDBOX REFUSED") || strings.HasPrefix(strings.TrimSpace(string(res.Output)), "SANDBOX REFUSED") || res.ExitCode == 125 || res.ExitCode == 126 || res.ExitCode == 127 {
		return true, fmt.Sprintf("wall/binary fault (exit %d): %s", res.ExitCode, tail)
	}
	return false, ""
}
func isMissingTestToolchain_internal(res WallRunResult) (bool, string) {
	if res.ExitCode == 0 {
		return false, ""
	}
	out := string(res.Output)
	for _, needle := range []string{"command not found", ": not found", "No such file or directory: 'elixir'", "No such file or directory: 'luau'", "No such file or directory: 'sbcl'", "No such file or directory: 'dart'", "No such file or directory: 'dotnet'"} {
		if strings.Contains(out, needle) && (strings.Contains(out, "make:") || strings.Contains(out, "/bin/sh:")) {
			return true, fmt.Sprintf("test toolchain missing in wall (exit %d): %s", res.ExitCode, TailBytes(res.Output, 512))
		}
	}
	return false, ""
}
func (r *Runner) harvestStep_internal(ctx context.Context, job Job, rec *JobRecord, res WallRunResult, runErr error) error {
	h, err := r.harnessFor_internal(job.HarnessName)
	if err != nil {
		return err
	}
	job.ProxyStatus = cmp.Or(job.ProxyStatus, r.Proxy.ConsumeStatus(job.Consumer))
	out := ClassifyJob(h, job, res.ExitCode, res.Elapsed, r.Timeouts.LaunchGrace, res.Output)
	if fault, why := isInfraFault_internal(res, runErr); fault && out.Result != "fault" && (runErr != nil || (out.Result != "ok" && out.Result != "yes" && out.Result != "fix")) {
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

// harvestWorkHead_internal pushes the head a work job left, from the
// worker's own clone (sealAndCollect_internal). Its tree, committed by the
// seal step when dirty (the harness note of 2026-11-07), is the head; ok
// with nothing committed and nothing to commit is a fail naming it.
func (r *Runner) harvestWorkHead_internal(ctx context.Context, job Job, rec *JobRecord, out Outcome) Outcome {
	if r.Git == nil {
		return Outcome{Result: "fault", Reason: "missing git edge for a work consumer: start the worker through nova-sprint worker, which gives it one"}
	}
	sha, herr := r.sealAndCollect_internal(ctx, job, *rec)
	// The verdict is the head's (v1's path, tla/Runner.tla Harvest): a new
	// head is ok here and its done-when tests decide it
	// (rebaseAndTestStep_internal); the harness's own word, or none, never
	// does. Only its fault and its refutation stand.
	byHead := out.Result != "fault" && !card.Refuted(out.Reason)
	switch {
	case herr != nil && byHead:
		return Outcome{Result: "fault", Reason: "seal fault: " + herr.Error()}
	case herr != nil:
		r.report("sealing the tree of "+job.Consumer, herr)
		return out
	case sha != "" && sha != job.BaseSHA && sha != job.PriorHead:
		if perr := r.pushBranch_internal(ctx, rec, sha); perr != nil {
			if byHead {
				return Outcome{Result: "fault", Head: rec.PushedHead, Reason: "push fault: " + perr.Error()}
			}
			return out
		}
		if byHead {
			return Outcome{Result: "ok", Head: sha}
		}
		out.Head = sha
	case out.Result == "ok" && sha != "" && sha != job.BaseSHA && sha == job.PriorHead:
		rec.PushedHead = cmp.Or(rec.PushedHead, sha)
		out.Head = sha
	case out.Result == "ok":
		return Outcome{Result: "fail", Reason: "harness reported ok without advancing HEAD past base_sha and prior_head, and left nothing inside PATHS to commit"}
	}
	return out
}

// Files the worker's steps leave in a job directory: the seal step's
// bundle of the job's commits, or its mark that there were none; the
// rebased head the wall applies before the done-when tests.
const (
	SealBundle    = "out.bundle"
	SealNone      = "out.none"
	RebasedBundle = "rebased.bundle"
)

// SealedDir holds, outside every job directory, the worker's own clone of
// each job, the one its pushes, rebase and checks run in:
// <root>/sealed/<consumer>. (<root>/clones is the work clones' default.)
const SealedDir = "sealed"

func (r *Runner) clonePath_internal(consumer string) string {
	return filepath.Join(r.RootDir, SealedDir, consumer)
}

// sealScript_internal is the seal step, run in the job's tree inside the
// wall (net-deny, no token, the harness's writable set): the tree's changes
// inside the card's PATHS ($7...) added and, when any, committed; then the
// commits since $1 bundled to $2, or $3 marked when there are none. What
// the harness left outside PATHS (build output under a nested target/ on
// rel-rs-ffi-aliasing: 259 files) is never committed; it is printed for
// the step log. Whatever the harness left in .git runs here, inside the
// wall, and nowhere else.
const sealScript_internal = `set -e
rm -f "$2" "$3" "$3.add"
git reset -q
if [ $# -gt 6 ]; then
  (shift 6; git ls-files -z -m -o -d --exclude-standard -- "$@") > "$3.add"
  if [ -s "$3.add" ]; then git --literal-pathspecs add -A --pathspec-from-file="$3.add" --pathspec-file-nul; fi
  rm -f "$3.add"
fi
if ! git diff --cached --quiet; then
  if [ -n "$5" ]; then git -c user.name="$5" -c user.email="$6" commit -q --no-verify -m "$4"; else git commit -q --no-verify -m "$4"; fi
fi
git status --porcelain | sed 's/^/outside PATHS, not committed: /'
if [ "$(git rev-parse HEAD)" = "$1" ]; then : > "$3"; else git bundle create "$2" "$1..HEAD"; fi`

// sealArgv_internal is the seal step's argv; each PATHS entry a git
// pathspec, a glob (* ? [) matched per segment as the card contract's
// admission does (a file it matches, or all under a directory it matches),
// any other entry literal (a file, or a directory and all under it).
func sealArgv_internal(jobDir, base, name, email, msg string, paths []string) []string {
	argv := []string{"sh", "-c", sealScript_internal, "seal", base, filepath.Join(jobDir, SealBundle), filepath.Join(jobDir, SealNone), msg, name, email}
	for _, p := range paths {
		if strings.ContainsAny(p, "*?[") {
			p = strings.TrimSuffix(p, "/")
			argv = append(argv, ":(glob)"+p, ":(glob)"+p+"/**")
		} else {
			argv = append(argv, ":(literal)"+p)
		}
	}
	return argv
}

// applyArgv_internal checks the bundle's head out in the job's tree, in
// the wall, so the done-when tests run on the head that is pushed, and on
// nothing else: the seal committed only what is inside PATHS, so what the
// harness changed outside them is still in the tree (an edit to a test or a
// Makefile, which the forced checkout undoes; untracked files, which the
// clean removes; ignored files, build output, stay).
func applyArgv_internal(bundle string) []string {
	return []string{"sh", "-c", `set -e; git fetch -q --no-tags "$1" HEAD; git checkout -q -f --detach FETCH_HEAD; git clean -q -f -d`, "apply", bundle}
}

// resetArgv_internal puts the job's tree at head, a commit it already
// holds (the head the job started from, verified with no commit of its
// own), as applyArgv_internal does for a new head.
func resetArgv_internal(head string) []string {
	return []string{"sh", "-c", `set -e; git checkout -q -f --detach "$1"; git clean -q -f -d`, "reset", head}
}

// sealNote_internal is the seal's output for its step log: the first 20
// lines (what it left outside PATHS), the rest counted, never cut silently.
func sealNote_internal(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 20 {
		lines = append(lines[:20], fmt.Sprintf("and %d more", len(lines)-20))
	}
	return strings.Join(lines, "\n")
}

// sealAndCollect_internal ends a work job's tree: the seal step in the
// wall, the harness's process group killed after it (the wall's
// KillGroup), then Collect into the worker's own clone. After it no worker
// git runs in the job directory; the job's head is the clone's.
func (r *Runner) sealAndCollect_internal(ctx context.Context, job Job, rec JobRecord) (string, error) {
	if r.Wall == nil {
		return "", errors.New("no wall to run the seal step in")
	}
	remote, err := r.jobRemote_internal(rec)
	if err != nil {
		return "", err
	}
	base := cmp.Or(rec.PriorHead, rec.BaseSHA)
	msg := rec.Consumer + ": the harness's work, committed by nova-worker\n\nThe harness left its changes uncommitted; the worker's seal step committed those inside the card's PATHS.\n"
	res, err := r.Wall.RunStep(ctx, job, sealArgv_internal(job.JobDir, base, job.CommitName, job.CommitEmail, msg, job.Paths), r.Timeouts.RebaseAndTest)
	r.logStep_internal(rec.Row, rec.Consumer, "seal", strings.TrimSpace(fmt.Sprintf("exit=%d %s %s", res.ExitCode, leftoversNote_internal(res), sealNote_internal(res.Output))), res.Elapsed)
	r.report("killing what the seal step of "+rec.Consumer+" left running", res.LeftoverErr)
	if err == nil && res.ExitCode != 0 {
		err = fmt.Errorf("exit %d: %s", res.ExitCode, TailBytes(res.Output, 512))
	}
	if err != nil {
		return "", fmt.Errorf("the seal step: %w", err)
	}
	return r.Git.Collect(ctx, Sealed{JobDir: job.JobDir, Clone: r.clonePath_internal(rec.Consumer), Remote: remote, Base: base, Name: job.CommitName, Email: job.CommitEmail})
}
func (r *Runner) rebaseAndTestStep_internal(ctx context.Context, jobDir string, rec *JobRecord) error {
	if rec.Kind != "work" || rec.Outcome.Result != "ok" {
		return nil
	}
	// The harness said ok and committed nothing over the prior head
	// (tla/Runner.tla atPrior). The card machine refuses that head as a
	// finish when an attempt already finished at it (card.FinishedAt,
	// tla/CardMachine.tla FinishedHeads), which the take receipt says
	// (prior_finished): that head was read and sent back, and neither it nor
	// a merge of the stream into it answers the finding, so the job fails
	// here (NothingNew), with no report to be refused. An unfinished prior
	// head is work an attempt pushed before it faulted, failed or was
	// withdrawn, and this attempt found nothing left to do: it is verified
	// like any head, the stream merged into it and the done-whens run there
	// (Rebase), where failing it left every later attempt nothing but a
	// fail (2026-11-11). A finding on the deal does not change that: a
	// finding is written against a finished head, so an unfinished prior
	// head was pushed after it, by an attempt on that finding, and the read
	// that follows judges it against the finding.
	wt, clone := filepath.Join(jobDir, "repo"), r.clonePath_internal(rec.Consumer)
	atPrior := rec.PriorHead != "" && rec.Outcome.Head == rec.PriorHead
	if atPrior && rec.PriorFinished {
		rec.Outcome = nothingNew_internal()
		r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
		return nil
	} else if atPrior && r.Git != nil { // the seal found no commits: nothing is checked out in the worker's clone
		if err := r.Git.CheckOut(ctx, clone, rec.PriorHead); err != nil {
			rec.Outcome = Outcome{Result: "fault", Reason: "the prior head could not be checked out to verify it: " + err.Error()}
			r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
			return nil
		}
	}
	if r.Git != nil && rec.StreamRef != "" {
		remote, err := r.jobRemote_internal(*rec)
		newSHA := ""
		if err == nil {
			newSHA, err = r.Git.RebaseOntoStream(ctx, clone, remote, rec.StreamRef)
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
			default: // not a conflict: the worker's git, never the card's work
				rec.Outcome = Outcome{Result: "fault", Head: rec.PushedHead, Reason: "stream merge fault: " + err.Error()}
			}
			r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
			return nil
		}
		rec.Outcome.Head = cmp.Or(newSHA, rec.Outcome.Head)
	}
	tJob := Job{Consumer: rec.Consumer, Row: rec.Row, Repo: rec.Repo, Forge: rec.Forge, JobDir: jobDir, WorktreeDir: wt, ProxyAddr: r.ProxyAddr}
	if fault, err := r.applyRebased_internal(ctx, tJob, *rec); err != nil {
		rec.Outcome = Outcome{Result: "fail", Head: rec.PushedHead, Reason: "the rebased head could not be checked out for the done-when tests: " + err.Error()}
		if fault {
			rec.Outcome.Result = "fault"
		}
		r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
		return nil
	}
	for i, cmd := range rec.DoneWhen {
		if strings.TrimSpace(cmd) == "" || r.Wall == nil {
			continue
		}
		if out, red, _ := r.doneWhen_internal(ctx, tJob, *rec, doneWhenGate, cmd); red {
			// The gates after it are not run: said, so none is taken for green.
			if rest := slices.DeleteFunc(slices.Clone(rec.DoneWhen[i+1:]), func(c string) bool { return strings.TrimSpace(c) == "" }); len(rest) > 0 {
				out.Reason += "\ndone-whens not run after it: " + strings.Join(rest, "; ")
			}
			if rest := slices.DeleteFunc(slices.Clone(rec.StreamGates), func(c string) bool { return strings.TrimSpace(c) == "" }); len(rest) > 0 {
				out.Reason += "\nstream gates not run after it: " + strings.Join(rest, "; ")
			}
			rec.Outcome = out
			r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
			return nil
		}
	}
	if atPrior {
		// For the read of this head: the consumer that reports it did not
		// write it. (Which attempt and row did is not in the receipt.) Set
		// before the stream gates, whose note on a gate red at the base is
		// added to it, and whose red replaces it.
		rec.Outcome.Reason = "this attempt added nothing: the head was pushed by an earlier attempt that did not finish, and is verified here (its gates green)"
		if rec.Outcome.Head != rec.PriorHead {
			rec.Outcome.Reason = "this attempt added nothing but the merge of the stream: the work under it was pushed by an earlier attempt that did not finish, and the merge is verified here (its gates green)"
		}
	}
	if out, red, note := r.streamGates_internal(ctx, tJob, *rec); red {
		rec.Outcome = out
		r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
		return nil
	} else if note != "" {
		rec.Outcome.Reason = TailBytes([]byte(strings.TrimPrefix(rec.Outcome.Reason+"; "+note, "; ")), MaxReasonBytes)
	}
	if r.Git != nil && rec.Outcome.Head != "" && rec.Outcome.Head != rec.PushedHead {
		if err := r.pushBranch_internal(ctx, rec, rec.Outcome.Head); err != nil {
			// The head that did land is reported, so the next attempt's
			// prior head (and its lease) is the branch as it stands.
			rec.Outcome = Outcome{Result: "fault", Head: rec.PushedHead, Reason: "post-merge push fault: " + err.Error()}
			r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
			return nil
		}
	}
	rec.State = JobRebased
	r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, *rec))
	return nil
}

// nothingNew_internal is the fail of a job whose harness said ok and
// committed nothing over a prior head an attempt already finished at. No
// head: nothing new was pushed.
func nothingNew_internal() Outcome {
	return Outcome{Result: "fail", Reason: "harness reported ok but committed nothing over the prior head, which an earlier attempt already finished at"}
}

// doneWhenBound_internal is how long a done-when gate of rec may run from
// now, and left, what is left of the card's deadline (rec.DeadlineAt: the
// take plus the receipt's deadline, policy's timeouts().consumer[tier]),
// the one bound the deal carries. The card contract states no other, and
// the worker's own step bound (Timeouts.RebaseAndTest, two minutes) killed
// every longer gate on every bench. The bound is left less the push's
// bound (Timeouts.Push), kept for the push and the report after the gates:
// the sprint withdraws a card that has not reported by its deadline, and a
// gate that ran right up to it lost the attempt. One push bound for both
// is a guess, not measured. Zero or less: no gate starts. A record with no
// deadline (written before they were recorded) keeps the step bound, so no
// gate runs unbounded.
func (r *Runner) doneWhenBound_internal(rec JobRecord) (bound, left time.Duration, what string) {
	if rec.DeadlineAt <= 0 {
		return r.Timeouts.RebaseAndTest, r.Timeouts.RebaseAndTest, "the worker's step bound (the job record carries no deadline)"
	}
	left = time.Unix(rec.DeadlineAt, 0).Sub(r.Now())
	return left - r.Timeouts.Push, left, fmt.Sprintf("what was left of the card's deadline less the %s kept for the push and the report", r.Timeouts.Push)
}

// doneWhen_internal runs one done-when gate of rec in the wall, at the
// head the job's tree holds; red: it did not pass, and out is the job's
// outcome. Red tests are a fail whose reason keeps the lines that say what
// failed (GateTail). A gate the wall's own timeout killed
// (WallTimeoutError) is a fail too, saying after how long: it had the rest
// of the card's deadline, dealt again it would run out of it again, and a
// fail's bounce reaches the bounce limit where a fault is dealt again for
// ever. A gate not started because the deadline has passed, a wall that
// could not run it (a setup step that failed or timed out, the SDK
// environment's among them) and a missing toolchain are faults: that gate
// did not run. A gate killed by a signal (exit above 128, an OOM kill's
// 137 among them) is a fail that says so. testsRed: it ran and exited
// non-zero by itself (1 to 124, or 128), the one red a stream gate's run at the
// base can excuse (streamGates_internal).
func (r *Runner) doneWhen_internal(ctx context.Context, j Job, rec JobRecord, kind gateKind, cmd string) (out Outcome, red, testsRed bool) {
	notRun := func(why string) (Outcome, bool, bool) {
		return Outcome{Result: "fault", Head: rec.PushedHead, Reason: TailBytes([]byte(fmt.Sprintf("%s (%s) did not run: %s", kind.name, cmd, why)), MaxReasonBytes)}, true, false
	}
	bound, left, what := r.doneWhenBound_internal(rec)
	if bound <= 0 {
		// No limit counts a work fault, so the card is dealt again; its next
		// attempt starts from the head this one pushed, with a whole deadline.
		why := fmt.Sprintf("the card's deadline passed %s ago", (-left).Round(time.Second))
		if left > 0 {
			why = fmt.Sprintf("only %s of the card's deadline was left, less than the %s kept for the push and the report", left.Round(time.Second), r.Timeouts.Push)
		}
		return notRun(why + ", so the gate was not started; the card is dealt again (a fault spends no bounce) and continues from the head this attempt pushed")
	}
	tres, err := r.Wall.RunTest(ctx, j, cmd, bound)
	if err == nil && tres.ExitCode == 0 {
		return Outcome{}, false, false
	}
	var own *WallTimeoutError
	if errors.As(err, &own) {
		return Outcome{Result: "fail", Head: rec.PushedHead, Reason: fmt.Sprintf("%s (%s) timed out after %s, %s: %s",
			kind.name, cmd, bound.Round(time.Second), what, cmp.Or(GateTail(tres.Output, MaxReasonBytes), "no output"))}, true, false
	}
	if fault, why := isInfraFault_internal(tres, err); fault {
		return notRun(why)
	}
	if fault, why := isMissingTestToolchain_internal(tres); fault {
		return notRun(why)
	}
	if tres.ExitCode < 0 || tres.ExitCode > 128 {
		return Outcome{Result: "fail", Head: rec.PushedHead, Reason: fmt.Sprintf("%s (%s): killed (exit %d): %s", kind.red, cmd, tres.ExitCode, GateTail(tres.Output, MaxReasonBytes))}, true, false
	}
	return Outcome{Result: "fail", Head: rec.PushedHead, Reason: fmt.Sprintf("%s (%s): %s", kind.red, cmd, GateTail(tres.Output, MaxReasonBytes))}, true, true
}

// gateKind names a gate in its reasons: a card's DONE-WHEN, or a gate of
// its stream (store.Stream.Gates).
type gateKind struct{ name, red string }

var (
	doneWhenGate = gateKind{name: "done-when test", red: "tests red"}
	streamGate   = gateKind{name: "stream gate", red: "stream gate red"}
)

// streamGates_internal runs the stream's gates of rec after its DONE-WHENs,
// at the same head, each as doneWhen_internal runs a DONE-WHEN (the same
// bound and reason rules). A gate that is red at the head (it ran and
// exited non-zero by itself) and red at its base (baseGateRed_internal:
// the stream tip the head was merged with) is the base's fault, not the
// card's: it does not fail the attempt, and note says "stream gate <cmd>
// is red at base <sha>" for the report's reason. A gate red at the head
// and green at the base fails the attempt as a red DONE-WHEN does; so does
// a stream gate that timed out, was killed or did not run, at the head,
// whatever the base. One red at the head that the worker could not judge
// at the base is a fault: the worker's failing, not the card's.
func (r *Runner) streamGates_internal(ctx context.Context, j Job, rec JobRecord) (out Outcome, red bool, note string) {
	if r.Wall == nil {
		return Outcome{}, false, ""
	}
	var reds []string
	outs := map[string]Outcome{}
	for _, cmd := range rec.StreamGates {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		o, isRed, testsRed := r.doneWhen_internal(ctx, j, rec, streamGate, cmd)
		if isRed && !testsRed {
			return o, true, ""
		}
		if isRed {
			reds, outs[cmd] = append(reds, cmd), o
		}
	}
	if len(reds) == 0 {
		return Outcome{}, false, ""
	}
	base, atBase, why := r.baseGateRed_internal(ctx, j, rec, reds)
	var notes, unjudged []string
	for _, cmd := range reds {
		switch isRed, judged := atBase[cmd]; {
		case !judged:
			unjudged = append(unjudged, cmd)
		case !isRed:
			return outs[cmd], true, ""
		default:
			notes = append(notes, fmt.Sprintf("stream gate %s is red at base %s", cmd, base))
		}
	}
	if len(unjudged) > 0 {
		return Outcome{Result: "fault", Head: rec.PushedHead, Reason: TailBytes([]byte(fmt.Sprintf("stream gate (%s) is red at the head and was not judged at the base %s: %s",
			strings.Join(unjudged, "; "), cmp.Or(base, "(unknown)"), why)), MaxReasonBytes)}, true, ""
	}
	note = strings.Join(notes, "; ")
	r.logStep_internal(rec.Row, rec.Consumer, "stream gates at base", note+": the card is not charged", 0)
	return Outcome{}, false, note
}

// baseGateRed_internal is whether each of cmds is red at base, the base a
// stream gate red at the merged head is judged against: the stream tip
// the merge used (GitOps.StreamTip), or rec.BaseSHA when nothing was
// merged. A gate with no answer was not judged, and why says why. Only a
// green answer is kept, per repository, base sha and gate, for the life
// of the worker: a red one is asked again each time, as a red there may be
// the bench's (a kill, a full disk) and gone the next time. The gates not
// yet answered run in a fresh worktree of the job's tree at the base
// (baseArgv_internal), removed on every path after them
// (dropBaseArgv_internal): the job's tree stays at the head. A red is an answer only when the gate ran and exited
// non-zero by itself (doneWhen_internal's testsRed): one that timed out,
// was killed or did not run there is not judged.
func (r *Runner) baseGateRed_internal(ctx context.Context, j Job, rec JobRecord, cmds []string) (base string, out map[string]bool, why string) {
	out, base = map[string]bool{}, rec.BaseSHA
	if r.Git != nil && rec.StreamRef != "" {
		tip, err := r.Git.StreamTip(ctx, r.clonePath_internal(rec.Consumer))
		if err == nil && tip == "" {
			err = errors.New("git named none")
		}
		if err != nil {
			return "", out, "the stream tip the merge used could not be read: " + err.Error()
		}
		base = tip
	}
	if base == "" {
		return "", out, "the job record names no base"
	}
	key := func(cmd string) string { return rec.Repo + "\x00" + base + "\x00" + cmd }
	var ask []string
	r.mu.Lock()
	for _, cmd := range cmds {
		if r.baseGates[key(cmd)] {
			out[cmd] = false
		} else {
			ask = append(ask, cmd)
		}
	}
	r.mu.Unlock()
	if len(ask) == 0 {
		return base, out, ""
	}
	bJob := j
	bJob.WorktreeDir = filepath.Join(j.JobDir, BaseTreeDir)
	res, err := r.Wall.RunStep(ctx, j, baseArgv_internal(base, bJob.WorktreeDir), r.Timeouts.RebaseAndTest)
	defer func() { // the base's tree goes on every path: the job's tree stays at the head
		res, err := r.Wall.RunStep(context.WithoutCancel(ctx), j, dropBaseArgv_internal(bJob.WorktreeDir), r.Timeouts.RebaseAndTest)
		if fault, w := isInfraFault_internal(res, err); fault || res.ExitCode != 0 {
			r.logStep_internal(rec.Row, rec.Consumer, "stream gates at base", strings.TrimSpace(fmt.Sprintf("base tree %s not removed: exit=%d %s %s", bJob.WorktreeDir, res.ExitCode, w, TailBytes(res.Output, 512))), res.Elapsed)
		}
	}()
	if fault, w := isInfraFault_internal(res, err); fault || res.ExitCode != 0 {
		why = strings.TrimSpace(fmt.Sprintf("base %s not checked out: exit=%d %s %s", base, res.ExitCode, w, TailBytes(res.Output, 512)))
		r.logStep_internal(rec.Row, rec.Consumer, "stream gates at base", why, res.Elapsed)
		return base, out, why
	}
	var whys []string
	for _, cmd := range ask {
		o, isRed, testsRed := r.doneWhen_internal(ctx, bJob, rec, streamGate, cmd)
		if isRed && !testsRed {
			whys = append(whys, o.Reason) // not judged at the base: nothing kept
			continue
		}
		out[cmd] = isRed
		if !isRed {
			r.mu.Lock()
			r.baseGates[key(cmd)] = true
			r.mu.Unlock()
		}
	}
	return base, out, strings.Join(whys, "; ")
}

// BaseTreeDir is the job directory's tree a stream gate runs in at the
// base (baseArgv_internal): a worktree of the job's tree, removed after.
const BaseTreeDir = "base"

// baseArgv_internal checks the base sha out in dir, a fresh worktree of the
// job's tree, in the wall, for a stream gate's run at the base: the job's
// tree stays at the head, and nothing the head's gates built there (an
// ignored build cache among it) is in dir to decide the base's answer.
// Run in the job's tree; a dir left by an earlier run is replaced.
func baseArgv_internal(sha, dir string) []string {
	return []string{"sh", "-c", `set -e; rm -rf "$2"; git worktree prune; git worktree add -q -f --detach "$2" "$1"`, "base", sha, dir}
}

// dropBaseArgv_internal removes the base's worktree dir and its record in
// the job's tree, in the wall, run there.
func dropBaseArgv_internal(dir string) []string {
	return []string{"sh", "-c", `rm -rf "$1" && git worktree prune`, "drop", dir}
}

// applyRebased_internal checks the rebased head out in the job's tree,
// inside the wall, before the done-when tests: Export from the worker's
// clone, applyArgv_internal in the wall. Nothing to do with no rebase or
// no tests. fault: the failure is the worker's or the wall's (the export,
// or isInfraFault_internal), not the card's.
func (r *Runner) applyRebased_internal(ctx context.Context, j Job, rec JobRecord) (bool, error) {
	if r.Wall == nil || !slices.ContainsFunc(slices.Concat(rec.DoneWhen, rec.StreamGates), func(c string) bool { return strings.TrimSpace(c) != "" }) {
		return false, nil
	}
	start := cmp.Or(rec.PriorHead, rec.BaseSHA)
	var argv []string
	switch {
	case rec.Outcome.Head == start:
		// Verified at the head it started from, the stream not moved: no
		// commit to export, but the tree is not that head. The harness may
		// have changed anything outside PATHS, a test or a Makefile among
		// them, and the gates must not pass on that.
		argv = resetArgv_internal(start)
	case r.Git == nil || rec.StreamRef == "":
		return false, nil
	default:
		bundle := filepath.Join(j.JobDir, RebasedBundle)
		if err := r.Git.Export(ctx, r.clonePath_internal(rec.Consumer), start, bundle); err != nil {
			return true, err
		}
		argv = applyArgv_internal(bundle)
	}
	res, err := r.Wall.RunStep(ctx, j, argv, r.Timeouts.RebaseAndTest)
	if fault, why := isInfraFault_internal(res, err); fault {
		return true, errors.New(why)
	}
	if res.ExitCode != 0 {
		return false, fmt.Errorf("exit %d: %s", res.ExitCode, TailBytes(res.Output, 512))
	}
	return false, nil
}
func (r *Runner) sendReportAndClean_internal(ctx context.Context, jobDir string, rec JobRecord) error {
	rec.State = JobReporting
	rec.ResultID = cmp.Or(rec.ResultID, resultID_internal(rec.Epoch, rec.Consumer, rec.Gen))
	if err := WriteJobRecord(jobDir, rec); err != nil {
		return err
	}
	// A refused report is never rewritten and sent again as something else:
	// a refusal is also a misuse, a version skew or a verb that was killed,
	// and the record on disk is what a restart resends.
	if err := r.sendReport_internal(ctx, rec); err != nil {
		return err
	}
	r.logStep_internal(rec.Row, rec.Consumer, "report", "accepted=true", 0)
	rec.State = JobRecorded
	r.report("job record "+filepath.Base(jobDir), WriteJobRecord(jobDir, rec))
	return r.cleanJobDir_internal(ctx, jobDir, rec)
}

// sendReport_internal sends rec's report under its result id, waiting out
// a down store three times; a report the sprint did not accept is ErrRefused.
// A fail or a fault never names the prior head: nothing new was pushed, and
// the card machine refuses one that does.
func (r *Runner) sendReport_internal(ctx context.Context, rec JobRecord) error {
	out := reportOutcome_internal(rec) // a read's is ok, fix or fault, a stale record's too
	if out.Result != "ok" && rec.Kind == "work" && rec.PriorHead != "" && out.Head == rec.PriorHead {
		out.Head = ""
	}
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
	if !receipt.Accepted || !store.IsConsumerOp(receipt.Op, req.ResultID) {
		return fmt.Errorf("%w: report %s was not accepted (accepted=%v op=%q reason=%q): keeping job directory",
			ErrRefused, req.ResultID, receipt.Accepted, receipt.Op, receipt.Reason)
	}
	return nil
}
func (r *Runner) cleanJobDir_internal(ctx context.Context, jobDir string, rec JobRecord) error {
	wt, clone := filepath.Join(jobDir, "repo"), r.clonePath_internal(rec.Consumer)
	if rec.Kind == "work" && rec.PushedHead != "" && r.Git != nil {
		remote, err := r.jobRemote_internal(rec)
		ok := false
		if err == nil {
			ok, err = r.Git.OriginHasHead(ctx, clone, remote, rec.Branch, rec.PushedHead)
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
	r.endJobTree_internal(jobDir, rec, "clean")
	if r.Git != nil {
		r.report("removing worktree "+filepath.Base(wt), r.Git.RemoveWorktree(ctx, wt))
	}
	if err := r.removeDir_internal(jobDir); err != nil { // a read-only module cache too, as the retire path
		return err
	}
	if err := os.Remove(filepath.Join(r.RootDir, RemotesDir, rec.Consumer)); err != nil && !errors.Is(err, os.ErrNotExist) {
		r.report("removing the kept clone URL of "+rec.Consumer, err)
	}
	r.removeJobState_internal(rec.Consumer)
	r.logStep_internal(rec.Row, rec.Consumer, "clean", "removed", 0)
	return nil
}

// OSGitOps stages from one work clone per stream repo (directive 42): the
// repo is the deal receipt's; its bare mirror is <MirrorRoot>/<name>.git,
// the bench's object cache, made empty on first use; its work clone is
// <WorkRoot>/<name>, made empty on first use borrowing the mirror's objects.
// Fetch fills both shallow with what a pass's jobs check out (stage.go). A
// job's worktree is a shared clone of the work clone whose origin is the
// repo's clone URL, so its fetches and pushes go to the repo.
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
	url, err := git.CloneURL(j.Repo, j.Forge)
	if err != nil {
		return "", "", err
	}
	name, err := repoName_internal(url)
	return url, name, err
}

// workClone_internal is url's work clone, made (with its mirror) on first
// use, empty: staging's fetches fill both, shallow (fetchShallow_internal);
// refused when the clone there is another repo's. The caller holds url's
// write lock.
func (g *OSGitOps) workClone_internal(ctx context.Context, url, name string) (string, error) {
	to := g.to_internal(g.Timeouts.WithDefaults().Stage)
	mirror, work := filepath.Join(g.MirrorRoot, name+".git"), filepath.Join(g.WorkRoot, name)
	if g.MirrorRoot != "" && !fileExists_internal(mirror) {
		if _, err := g.runGit_internal(ctx, to, "", "init", "--quiet", "--bare", mirror); err != nil {
			return "", fmt.Errorf("stage: the mirror of %s: %w", url, err)
		}
	}
	if !fileExists_internal(work) {
		if err := g.newWorkClone_internal(ctx, to, url, mirror, work); err != nil {
			return "", fmt.Errorf("stage: the work clone of %s: %w", url, err)
		}
	} else if got, err := g.runGit_internal(ctx, to, work, "remote", "get-url", "origin"); err != nil || strings.TrimSpace(got) != url {
		return "", fmt.Errorf("stage refused: the work clone %s is of %q, not %q (%v): move it aside; the worker makes a new one on first use", work, strings.TrimSpace(got), url, err)
	}
	return work, nil
}

// newWorkClone_internal makes an empty work clone of url at work, borrowing
// the mirror's objects when the worker has one.
func (g *OSGitOps) newWorkClone_internal(ctx context.Context, to time.Duration, url, mirror, work string) error {
	if _, err := g.runGit_internal(ctx, to, "", "init", "--quiet", work); err != nil {
		return err
	}
	if _, err := g.runGit_internal(ctx, to, work, "remote", "add", "origin", url); err != nil {
		return err
	}
	if g.MirrorRoot == "" {
		return nil
	}
	absMirror, err := filepath.Abs(mirror)
	if err != nil {
		return err
	}
	return writeGitFile_internal(filepath.Join(work, ".git", "objects", "info", "alternates"), []byte(filepath.Join(absMirror, "objects")+"\n"))
}

// Fetch brings js's repo's mirror and work clone up to date with what js
// check out, shallow (fetchShallow_internal), under the clone's write lock
// and the mirror's bench-wide lock. Every job is of js[0]'s repo.
func (g *OSGitOps) Fetch(ctx context.Context, js ...Job) error {
	if len(js) == 0 {
		return nil
	}
	url, name, err := cloneURL_internal(js[0])
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
	unlock, err := g.lockMirror_internal(ctx, name)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", url, err)
	}
	err = g.fetchShallow_internal(ctx, url, name, work, js)
	if uerr := unlock(); err == nil {
		err = uerr
	}
	if err != nil {
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
	err = g.sharedClone_internal(ctx, to, work, j.WorktreeDir)
	lock.RUnlock()
	if err != nil {
		return "", fmt.Errorf("stage: the work tree: %w", err)
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
	return g.headSHA_internal(ctx, j.WorktreeDir, "HEAD")
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

// headSHA_internal is the worktree's HEAD: staging's own read, before the
// harness has run, and the worker's clone's.
func (g *OSGitOps) headSHA_internal(ctx context.Context, worktree, rev string) (string, error) {
	out, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Stage), worktree, "rev-parse", rev)
	return strings.TrimSpace(out), err
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
	url, _ := git.CloneURL(info.Repo, info.Forge) // "" on a refusal: staging refuses it with its reason
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
// (git.CloneURL of its repo and forge; "" when they give none, which every git
// call that reaches a remote refuses).
func recordRemote_internal(rec JobRecord) string {
	if url, err := git.CloneURL(rec.Repo, rec.Forge); err == nil {
		return url
	}
	return ""
}

// Collect: see GitOps. Every git call here runs in the worker's own clone
// with the worker's config; the job directory is only read, by Go, for the
// seal step's bundle (copySealed_internal).
func (g *OSGitOps) Collect(ctx context.Context, s Sealed) (string, error) {
	to := g.to_internal(g.Timeouts.WithDefaults().Stage)
	name, err := repoName_internal(s.Remote)
	if err != nil {
		return "", fmt.Errorf("collect: %w", err)
	}
	if err := os.RemoveAll(s.Clone); err != nil {
		return "", fmt.Errorf("collect: clear the worker's clone: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.Clone), 0o700); err != nil {
		return "", fmt.Errorf("collect: %w", err)
	}
	lock := g.cloneLock_internal(s.Remote)
	lock.RLock()
	err = g.sharedClone_internal(ctx, to, filepath.Join(g.WorkRoot, name), s.Clone)
	lock.RUnlock()
	if err != nil {
		return "", fmt.Errorf("collect: the worker's clone: %w", err)
	}
	for _, kv := range [][2]string{{"user.name", s.Name}, {"user.email", s.Email}} {
		if kv[1] == "" {
			continue
		}
		if _, err := g.runGit_internal(ctx, to, s.Clone, "config", kv[0], kv[1]); err != nil {
			return "", err
		}
	}
	bundle, err := copySealed_internal(s.JobDir, s.Clone)
	if err != nil || bundle == "" {
		return s.Base, err
	}
	for _, args := range [][]string{
		{"bundle", "verify", "-q", bundle},
		{"-c", "protocol.file.allow=always", "fetch", "--quiet", "--no-tags", "--no-recurse-submodules", bundle, "+HEAD:refs/worker/sealed"},
	} {
		if _, err := g.runGit_internal(ctx, to, s.Clone, args...); err != nil {
			return "", fmt.Errorf("collect: the sealed bundle: %w", err)
		}
	}
	head, err := g.headSHA_internal(ctx, s.Clone, "refs/worker/sealed")
	if err != nil {
		return "", err
	}
	if _, err := g.runGit_internal(ctx, to, s.Clone, "merge-base", "--is-ancestor", s.Base, head); err != nil {
		return "", fmt.Errorf("collect refused: the sealed head %s does not descend from %s, the head the job started from: %w", head, s.Base, err)
	}
	if err := g.newGitlinks_internal(ctx, to, s.Clone, s.Base, head); err != nil {
		return "", err
	}
	if _, err := g.runGit_internal(ctx, to, s.Clone, "checkout", "--quiet", "--detach", head); err != nil {
		return "", err
	}
	return head, nil
}

// CheckOut: see GitOps. The commit is the work clone's, which the worker's
// clone borrows its objects from (sharedClone_internal).
func (g *OSGitOps) CheckOut(ctx context.Context, clone, sha string) error {
	if _, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Stage), clone, "checkout", "--quiet", "--detach", sha); err != nil {
		return fmt.Errorf("check out %s in the worker's clone: %w", sha, err)
	}
	return nil
}

// copySealed_internal copies the seal step's bundle from jobDir into
// clone's .git, opened with no symbolic link followed and refused unless a
// regular file; "" when the seal step marked no commits.
func copySealed_internal(jobDir, clone string) (string, error) {
	f, err := os.OpenFile(filepath.Join(jobDir, SealBundle), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		if fi, lerr := os.Lstat(filepath.Join(jobDir, SealNone)); lerr == nil && fi.Mode().IsRegular() {
			return "", nil
		}
		return "", fmt.Errorf("collect: the seal step left neither %s nor %s in the job directory", SealBundle, SealNone)
	}
	if err != nil {
		return "", fmt.Errorf("collect: open the sealed bundle: %w", err)
	}
	defer func() { _ = f.Close() }()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("collect refused: %s is not a regular file (%v)", SealBundle, err)
	}
	dst := filepath.Join(clone, ".git", "sealed.bundle")
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("collect: %w", err)
	}
	_, err = io.Copy(out, f)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("collect: copy the sealed bundle: %w", err)
	}
	return dst, nil
}

// newGitlinks_internal refuses a commit in base..head whose tree holds a
// gitlink (mode 160000, a submodule) that base's tree does not.
func (g *OSGitOps) newGitlinks_internal(ctx context.Context, to time.Duration, clone, base, head string) error {
	links := func(commit string) ([]string, error) {
		out, err := g.runGit_internal(ctx, to, clone, "ls-tree", "-r", "-z", commit)
		var found []string
		for _, entry := range strings.Split(out, "\x00") {
			if strings.HasPrefix(entry, "160000 ") {
				found = append(found, entry)
			}
		}
		return found, err
	}
	known, err := links(base)
	if err != nil {
		return err
	}
	revs, err := g.runGit_internal(ctx, to, clone, "rev-list", base+".."+head)
	if err != nil {
		return err
	}
	for _, rev := range strings.Fields(revs) {
		found, err := links(rev)
		if err != nil {
			return err
		}
		for _, l := range found {
			if !slices.Contains(known, l) {
				_, path, _ := strings.Cut(l, "\t")
				return fmt.Errorf("collect refused: commit %s adds a gitlink (a submodule) at %s: the worker never pushes one", rev, path)
			}
		}
	}
	return nil
}

// Export: see GitOps. The bundle is made in clone, then written into out's
// directory under a fresh name and renamed over out.
func (g *OSGitOps) Export(ctx context.Context, clone, base, out string) error {
	tmp := filepath.Join(clone, ".git", "export.bundle")
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := g.runGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Stage), clone, "bundle", "create", tmp, base+"..HEAD"); err != nil {
		return fmt.Errorf("export the rebased head: %w", err)
	}
	in, err := os.Open(tmp)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	f, err := os.CreateTemp(filepath.Dir(out), ".rebased-*")
	if err != nil {
		return fmt.Errorf("export the rebased head: %w", err)
	}
	_, err = io.Copy(f, in)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), out)
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return fmt.Errorf("export the rebased head: %w", err)
	}
	return nil
}

// remoteNamed_internal refuses a call that would reach no clone URL.
func remoteNamed_internal(remote string) error {
	if strings.TrimSpace(remote) == "" {
		return errors.New("git refused: the job names no clone URL to push or fetch: its record's repo and forge give none (CloneURL)")
	}
	return nil
}
func (g *OSGitOps) Push(ctx context.Context, clone, remote, branch, headSHA, expect string) error {
	ref := "refs/heads/" + branch
	err := remoteNamed_internal(remote)
	if err == nil {
		_, err = g.runRemoteGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Push), clone, "push", "--no-recurse-submodules", "--force-with-lease="+ref+":"+expect, remote, headSHA+":"+ref)
	}
	if err != nil {
		at := cmp.Or(expect, "no branch (a new one)")
		return fmt.Errorf("push of %s to %s refused or failed: it moves the branch only from %s, the head this job last pushed or started from, and a branch another holder moved is theirs: %w", headSHA, branch, at, err)
	}
	return nil
}
func (g *OSGitOps) RebaseOntoStream(ctx context.Context, clone, remote, streamRef string) (string, error) {
	to := g.to_internal(g.Timeouts.WithDefaults().RebaseAndTest)
	if err := remoteNamed_internal(remote); err != nil {
		return "", err
	}
	worktree := clone // the worker's own clone, never the job directory
	if _, err := g.runRemoteGit_internal(ctx, to, worktree, "fetch", "--quiet", "--no-recurse-submodules", remote, streamRef); err != nil {
		return "", &FetchError{StreamRef: streamRef, Cause: err}
	}
	if err := g.deepenForMerge_internal(ctx, to, worktree, remote, streamRef); err != nil {
		return "", &FetchError{StreamRef: streamRef, Cause: err}
	}
	// Merged in, never rebased: a rebase drops a merge commit that resolved
	// a conflict and replays the card's commits into it again, and its head
	// no longer descends from the job's.
	if _, err := g.runGit_internal(ctx, to, worktree, "merge", "-m", "Merge "+streamRef+" into the card", "FETCH_HEAD"); err != nil {
		out, diffErr := g.runGit_internal(ctx, to, worktree, "diff", "--name-only", "--diff-filter=U")
		// Only a merge in progress is aborted: one refused before it began
		// (unrelated histories, a dirty tree) left nothing to abort.
		if _, inMerge := g.runGit_internal(ctx, to, worktree, "rev-parse", "-q", "--verify", "MERGE_HEAD"); inMerge == nil {
			if _, abortErr := g.runGit_internal(ctx, to, worktree, "merge", "--abort"); abortErr != nil {
				err = fmt.Errorf("%w; cleanup: %w", err, abortErr)
			}
		}
		if files := strings.Fields(out); diffErr == nil && len(files) > 0 {
			return "", &ConflictError{Files: files}
		}
		if diffErr != nil {
			err = fmt.Errorf("%w; listing conflicts: %w", err, diffErr)
		}
		return "", err
	}
	return g.headSHA_internal(ctx, worktree, "HEAD")
}

func (g *OSGitOps) StreamTip(ctx context.Context, clone string) (string, error) {
	return g.headSHA_internal(ctx, clone, "FETCH_HEAD^{commit}")
}

// deepenForMerge_internal deepens the worker's clone while it is shallow
// and HEAD shares no commit with FETCH_HEAD: by each of shallowDeepens, then
// --unshallow, as staging does. The clone carries the work clone's shallow
// boundary, where a stream tip a later staging fetched at depth 1 is a root
// with no parents, so the merge would find no base ("refusing to merge
// unrelated histories"). A deep clone, or one that shares a commit, is left
// as it is; one that shares none once unshallowed is the merge's to name.
func (g *OSGitOps) deepenForMerge_internal(ctx context.Context, to time.Duration, clone, remote, streamRef string) error {
	steps := make([]string, 0, len(shallowDeepens)+1)
	for _, n := range shallowDeepens {
		steps = append(steps, "--deepen="+strconv.Itoa(n))
	}
	for _, step := range append(steps, "--unshallow") {
		if !fileExists_internal(filepath.Join(clone, ".git", "shallow")) {
			return nil
		}
		if _, err := g.runGit_internal(ctx, to, clone, "merge-base", "HEAD", "FETCH_HEAD"); err == nil {
			return nil
		}
		if _, err := g.runRemoteGit_internal(ctx, to, clone, "fetch", "--quiet", "--no-tags", "--no-recurse-submodules", step, remote, streamRef); err != nil {
			return fmt.Errorf("deepen for a merge base with %s (%s): %w", streamRef, step, err)
		}
	}
	return nil
}

// OriginHasHead: the branch at remote, the job's clone URL, is at sha;
// asked from the worker's clone.
func (g *OSGitOps) OriginHasHead(ctx context.Context, clone, remote, branch, sha string) (bool, error) {
	if err := remoteNamed_internal(remote); err != nil {
		return false, err
	}
	return g.hasHead_internal(ctx, clone, remote, branch, sha)
}

func (g *OSGitOps) hasHead_internal(ctx context.Context, dir, remote, branch, sha string) (bool, error) {
	out, err := g.runRemoteGit_internal(ctx, g.to_internal(g.Timeouts.WithDefaults().Push), dir, "ls-remote", remote, "refs/heads/"+branch)
	return err == nil && strings.HasPrefix(strings.TrimSpace(out), sha), err
}
func (g *OSGitOps) RemoveWorktree(_ context.Context, worktree string) error {
	return os.RemoveAll(worktree)
}

// gitPrefix_internal begins every worker git call: no hooks, no
// fsmonitor, and no submodule recursion, whatever any config says.
var gitPrefix_internal = []string{"git", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "submodule.recurse=false", "-c", "diff.ignoreSubmodules=all"}

// gitEnv_internal is a worker git call's whole environment: PATH, HOME,
// no prompt, no global or system config (no host filter, LFS or helper
// runs on what a harness left; the identity is the clone's own config, set
// by Collect), and for a call that reaches a remote the one credential
// (git.CredentialEnv); a local call never carries it.
func (g *OSGitOps) gitEnv_internal(remote bool) []string {
	env := []string{"PATH=" + cmp.Or(os.Getenv("PATH"), "/usr/local/bin:/usr/bin:/bin"), "HOME=" + cmp.Or(os.Getenv("HOME"), "/tmp"), "GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	if !remote {
		return env
	}
	return append(env, git.CredentialEnv(g.Token, cmp.Or(g.GitHubCredential, "gh"))...)
}

// runGit_internal is a local worker git call: no credential.
func (g *OSGitOps) runGit_internal(ctx context.Context, timeout time.Duration, dir string, args ...string) (string, error) {
	return g.runGitEnv_internal(ctx, timeout, dir, g.gitEnv_internal(false), args)
}

// runRemoteGit_internal is a worker git call that reaches a remote (clone,
// fetch, ls-remote, push of a clone URL): it alone carries the credential.
func (g *OSGitOps) runRemoteGit_internal(ctx context.Context, timeout time.Duration, dir string, args ...string) (string, error) {
	return g.runGitEnv_internal(ctx, timeout, dir, g.gitEnv_internal(true), args)
}
func (g *OSGitOps) runGitEnv_internal(ctx context.Context, timeout time.Duration, dir string, env, args []string) (string, error) {
	cmd := edge.Cmd{
		Argv:     append(slices.Clone(gitPrefix_internal), args...),
		Dir:      dir,
		Env:      env,
		CleanEnv: true, Timeout: timeout, MaxBytes: outputMaxBytes_internal,
	}
	res, err := cmp.Or[edge.Exec](g.Exec, edge.NewOSExec()).Run(ctx, cmd)
	if err != nil || res.ExitCode != 0 {
		return res.Stdout + res.Stderr, edge.Failure(cmd.Argv, nil, res.ExitCode, res.Stdout, res.Stderr, err)
	}
	if res.Truncated { // a listing cut short is not the listing (collect reads a whole ls-tree -r)
		return res.Stdout + res.Stderr, edge.Failure(cmd.Argv, nil, res.ExitCode, res.Stdout, res.Stderr, fmt.Errorf("output truncated at %d bytes", outputMaxBytes_internal))
	}
	return res.Stdout, nil
}

type OSReserveWall struct {
	Config     WallConfig
	UseSandbox bool
	Exec       edge.Exec
	LookPath   func(string) (string, error) // exec.LookPath when nil
	// KillGroup kills what is left of a finished wall command's process
	// group and is how many it found (OSKillGroup in the binary); nil kills
	// nothing. No guarantee rests on it: no worker git runs in the job
	// directory after the harness, whatever outlives it.
	KillGroup func(pgid int) (int, error)
	// KillJob ends every process of the job at a job directory (OSKillJob
	// in the binary), after every wall command: the command's own group is
	// nova-reserve's child's, not the one KillGroup reaches. nil: none.
	KillJob func(jobDir string) (int, error)
}

// OSKillGroup kills process group pgid (a wall command's, whose leader has
// exited) and is how many of its processes pgrep found; a pgrep that
// fails (other than finding none) or a kill that fails is its error.
func OSKillGroup(pgid int) (int, error) {
	if pgid <= 1 {
		return 0, nil
	}
	out, err := exec.Command("pgrep", "-g", strconv.Itoa(pgid)).Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 { // none left
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("pgrep -g %d: %w", pgid, err)
	}
	n := len(strings.Fields(string(out)))
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return n, fmt.Errorf("kill -KILL -%d: %w", pgid, err)
	}
	return n, nil
}

// leftoversNote_internal is a step's log note on its wall command's
// leftovers: the count killed, and KillGroup's error when it had one.
func leftoversNote_internal(res WallRunResult) string {
	if res.LeftoverErr != nil {
		return fmt.Sprintf("leftovers-killed=%d leftovers-error=%q", res.Leftovers, res.LeftoverErr.Error())
	}
	return fmt.Sprintf("leftovers-killed=%d", res.Leftovers)
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
	env, release, err := gateJobserver_internal(SanitizeWallEnv(os.Environ(), j.JobDir), j.JobDir)
	if err != nil {
		return WallRunResult{}, err
	}
	defer release()
	lookPath := w.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if kv := cargoLinkerEnv_internal(runtime.GOOS, runtime.GOARCH, lookPath); kv != "" {
		env = append(env, kv)
	}
	return w.runWallCmd_internal(ctx, j, w.effectiveConfig_internal(j), fields, env, timeout)
}

// cargoLinkerEnv_internal is CARGO_TARGET_<host>_LINKER=<absolute cc> on
// Linux, "" elsewhere or without a cc: rustc runs its linker by name with
// its own PATH, and std spawns a name under a changed PATH by fork, never
// posix_spawn, which the wall refuses as for the pipe jobserver; an
// absolute path keeps posix_spawn.
func cargoLinkerEnv_internal(goos, goarch string, lookPath func(string) (string, error)) string {
	triple := map[string]string{"amd64": "X86_64", "arm64": "AARCH64"}[goarch]
	cc, err := lookPath("cc")
	if goos != "linux" || triple == "" || err != nil || !filepath.IsAbs(cc) {
		return ""
	}
	return "CARGO_TARGET_" + triple + "_UNKNOWN_LINUX_GNU_LINKER=" + cc
}

// gateJobs_internal is a gate's build parallelism: one nova-reserve slot,
// which at nova-reserve's default capacity (CPU/4 slots) is 4 cores, or
// the bench's cores when it has fewer.
func gateJobs_internal() int { return max(1, min(4, runtime.NumCPU())) }

// gateJobserver_internal is env with CARGO_MAKEFLAGS naming a FIFO
// jobserver in jobDir (a --write path of the wall), made and held open
// read-write here with gateJobs-1 tokens until release; it replaces any
// CARGO_MAKEFLAGS env had. cargo's own pipe jobserver adds a pre_exec, so
// std forks through socketpair(AF_UNIX, SOCK_SEQPACKET), which the wall's
// seccomp refuses (EPERM, rustc never runs); with a fifo: jobserver
// jobserver-rs adds none and std uses posix_spawn. Harmless to a gate that
// is not cargo, so every gate gets it. env as it is with no jobDir.
func gateJobserver_internal(env []string, jobDir string) ([]string, func(), error) {
	if jobDir == "" {
		return env, func() {}, nil
	}
	// The job owns its tree between runs, so whatever is at the name goes
	// (RemoveAll unlinks a link, never its target), and the FIFO is opened
	// without following a link, checked, and seeded without blocking: a
	// leftover process that swaps or fills it fails the gate, never hangs it.
	fifo := filepath.Join(jobDir, "jobserver.fifo")
	if err := os.RemoveAll(fifo); err != nil {
		return env, nil, fmt.Errorf("worker: clearing the gate's jobserver FIFO %s: %w", fifo, err)
	}
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		return env, nil, fmt.Errorf("worker: making the gate's jobserver FIFO %s: %w", fifo, err)
	}
	fd, err := syscall.Open(fifo, syscall.O_RDWR|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		fd = -1
	}
	release := func() {
		if fd >= 0 {
			_ = syscall.Close(fd)
		}
		_ = os.Remove(fifo)
	}
	var st syscall.Stat_t
	if err == nil {
		err = syscall.Fstat(fd, &st)
	}
	if err == nil && st.Mode&syscall.S_IFMT != syscall.S_IFIFO {
		err = errors.New("not a FIFO")
	}
	tokens := []byte(strings.Repeat("+", gateJobs_internal()-1))
	if n := 0; err == nil && len(tokens) > 0 {
		if n, err = syscall.Write(fd, tokens); err == nil && n != len(tokens) {
			err = fmt.Errorf("wrote %d of %d tokens", n, len(tokens))
		}
	}
	if err != nil {
		release()
		return env, nil, fmt.Errorf("worker: seeding the gate's jobserver FIFO %s: %w", fifo, err)
	}
	env = slices.DeleteFunc(env, func(kv string) bool { return strings.HasPrefix(strings.ToUpper(kv), "CARGO_MAKEFLAGS=") })
	return append(env, fmt.Sprintf("CARGO_MAKEFLAGS=-j%d --jobserver-auth=fifo:%s", gateJobs_internal(), fifo)), release, nil
}

func (w *OSReserveWall) RunStep(ctx context.Context, j Job, argv []string, timeout time.Duration) (WallRunResult, error) {
	return w.runWallCmd_internal(ctx, j, w.effectiveConfig_internal(j), argv, SanitizeWallEnv(os.Environ(), j.JobDir), timeout)
}
func (w *OSReserveWall) effectiveConfig_internal(j Job) WallConfig {
	cfg := w.Config
	cfg.ReserveBin = resolveToolBin_internal(cmp.Or(cfg.ReserveBin, "nova-reserve"))
	cfg.SandboxBin = resolveToolBin_internal(cmp.Or(cfg.SandboxBin, "nova-sandbox"))
	if _, name, err := cloneURL_internal(j); err == nil {
		if cfg.WorkRoot != "" {
			cfg.ReadPaths = append(slices.Clone(cfg.ReadPaths), filepath.Join(cfg.WorkRoot, name, ".git"))
		}
		if cfg.MirrorRoot != "" {
			cfg.ReadPaths = append(slices.Clone(cfg.ReadPaths), filepath.Join(cfg.MirrorRoot, name+".git"))
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

// noSlot_internal: res is nova-reserve's own refusal for want of a slot
// within --wait (nova-reserve help: exit 1, refused): exit 1 with its first
// stderr line "nova-reserve: no slot within <d>" or "nova-reserve: no
// exclusive hold within <d>", printed before any HELD, so before the
// command ran.
func noSlot_internal(res edge.Result) bool {
	first, _, _ := strings.Cut(strings.TrimLeft(res.Stderr, "\n"), "\n")
	return res.ExitCode == 1 && (strings.HasPrefix(first, "nova-reserve: no slot within ") || strings.HasPrefix(first, "nova-reserve: no exclusive hold within "))
}

// sdkWallKeeps_internal are the names the wall sets itself, which the
// bench's SDK environment never changes, and the shell's own.
var sdkWallKeeps_internal = []string{"HOME", "TMPDIR", "XDG_DATA_HOME", "DOTNET_CLI_HOME", "PWD", "OLDPWD", "SHLVL", "_"}

// sdkWallEnv_internal is env with what the bench's SDK environment
// (w.Config.SDKEnv, the sdk-toolchain role's env.sh) exports when sourced
// outside the wall in env alone, with the job's HOME: its PATH, the make
// variables and the toolchains' homes, as a make run on the bench gets them.
// The wall's own names keep the wall's values. env as it is on a bench
// without one; a failing env.sh refuses the command, naming the file.
func (w *OSReserveWall) sdkWallEnv_internal(ctx context.Context, env []string, timeout time.Duration) ([]string, error) {
	if w.Config.SDKEnv == "" {
		return env, nil
	}
	argv := []string{"/bin/sh", "-c", `. "$1" >&2 && exec /usr/bin/env -0`, "sh", w.Config.SDKEnv}
	res, err := cmp.Or[edge.Exec](w.Exec, edge.NewOSExec()).Run(ctx, edge.Cmd{Argv: argv, Env: env, CleanEnv: true, Timeout: min(timeout, 30*time.Second)})
	if err != nil || res.ExitCode != 0 {
		return nil, fmt.Errorf("worker: the bench's SDK environment %s failed: %w", w.Config.SDKEnv, edge.Failure(argv, nil, res.ExitCode, res.Stdout, res.Stderr, err))
	}
	for _, kv := range strings.Split(res.Stdout, "\x00") {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || name == "" || slices.Contains(sdkWallKeeps_internal, strings.ToUpper(name)) || strings.HasPrefix(name, "NOVA_WORKER_") || slices.Contains(env, kv) {
			continue
		}
		env = append(slices.DeleteFunc(env, func(e string) bool { return strings.HasPrefix(e, name+"=") }), kv)
	}
	return env, nil
}

func (w *OSReserveWall) runWallCmd_internal(ctx context.Context, j Job, cfg WallConfig, cmdArgv, env []string, timeout time.Duration) (WallRunResult, error) {
	fallbackPID := nextWallPID_internal()
	nonce := fmt.Sprintf("n-%d", fallbackPID)
	env, err := w.sdkWallEnv_internal(ctx, slices.Clone(env), timeout)
	if err != nil {
		return WallRunResult{}, err
	}
	env, err = CargoWallEnv(GoWallEnv(env, w.Config), w.Config, j.JobDir)
	if err != nil {
		return WallRunResult{}, err
	}
	env = append(env, "NOVA_WORKER_NONCE="+nonce)
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
	leftovers, leftoverErr := 0, error(nil)
	if w.KillGroup != nil && res.PID > 0 && res.PID != os.Getpid() { // edge's exec makes each command its group's leader
		leftovers, leftoverErr = w.KillGroup(res.PID)
	}
	if w.KillJob != nil && j.JobDir != "" {
		n, err := w.KillJob(j.JobDir)
		leftovers, leftoverErr = leftovers+n, errors.Join(leftoverErr, err)
	}
	out := WallRunResult{Leftovers: leftovers, LeftoverErr: leftoverErr, ExitCode: res.ExitCode, Output: []byte(strings.TrimPrefix(combined, "\n")), Stdout: []byte(res.Stdout), Stderr: []byte(res.Stderr),
		Argv: argv, PID: pid, Nonce: nonce, Elapsed: res.Duration}
	if noSlot_internal(res) {
		return out, fmt.Errorf("%w: %w", ErrNoSlot, edge.Failure(argv, nil, res.ExitCode, res.Stdout, res.Stderr, nil))
	}
	if err != nil {
		// The run never completed (a timeout, a missing binary): the
		// command, its exit code and its output so far go with it.
		fail := error(edge.Failure(argv, nil, res.ExitCode, res.Stdout, res.Stderr, err))
		var xe *edge.ExecError
		if errors.As(err, &xe) && xe.Kind == edge.ErrTimeout {
			fail = &WallTimeoutError{Err: fail}
		}
		return out, fail
	}
	return out, nil
}

// WallTimeoutError is a wall command that ran for its whole timeout and
// was killed: the command's own, marked here where it is known, never a
// setup step's before it (the SDK environment script has a bound of its
// own, and its timeout is the same edge.ErrTimeout underneath). Its words
// are the failure's.
type WallTimeoutError struct{ Err error }

func (e *WallTimeoutError) Error() string { return e.Err.Error() }
func (e *WallTimeoutError) Unwrap() error { return e.Err }
