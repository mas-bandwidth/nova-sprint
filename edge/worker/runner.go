package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrNoSlot = errors.New("worker: no nova-reserve slot available within wait")

type ConflictError struct {
	Files []string
}

func (e *ConflictError) Error() string { return "rebase conflict: " + strings.Join(e.Files, ", ") }

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
type WallRunResult struct {
	ExitCode int
	Output   []byte
	PID      int
	Nonce    string
	Elapsed  time.Duration
}
type ReserveWall interface {
	RunHarness(ctx context.Context, j Job, argv, env []string, timeout time.Duration) (WallRunResult, error)
	RunTest(ctx context.Context, j Job, cmd string, timeout time.Duration) (WallRunResult, error)
}
type ProcessTable interface {
	Alive(pid int, nonce string) bool
	Kill(pid int, nonce string, grace time.Duration) error
}
type Runner struct {
	Host              string
	CoordinatorHosts  []string
	RootDir, StateDir string
	Sprint            SprintClient
	Git               GitOps
	Forge             ForgeOps
	Wall              ReserveWall
	Procs             ProcessTable
	Harnesses         map[string]Harness
	DefaultHarness    Harness
	ProxyAddr         string
	Timeouts          Timeouts
	Logger            *StepLogger
	Now               func() time.Time
	Sleep             func(time.Duration)
	mu                *sync.Mutex
	probed            map[string]bool
	adopted           map[string]JobRecord
	dedup             *edge.Deduplicator
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
	if cfg.Logger == nil && cfg.StateDir != "" {
		cfg.Logger = &StepLogger{StateDir: cfg.StateDir, Now: cfg.Now}
	}
	cfg.mu = &sync.Mutex{}
	cfg.probed = map[string]bool{}
	cfg.adopted = map[string]JobRecord{}
	cfg.dedup = edge.NewDeduplicator()
	return &cfg, nil
}
func (r *Runner) harnessFor_internal(name string) (Harness, error) {
	if name != "" && r.Harnesses != nil {
		if h, ok := r.Harnesses[name]; ok {
			return h, nil
		}
	}
	switch {
	case r.DefaultHarness != nil:
		return r.DefaultHarness, nil
	case name != "":
		return NewHarness(name)
	}
	return NewHarness("script")
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
	baseJob := Job{
		Consumer: row + ".probe", Row: row, Kind: "work", Tier: "flash",
		Model: model, HarnessName: h.Name(), JobDir: probeDir,
		WorktreeDir: filepath.Join(probeDir, "repo"), ProxyAddr: r.ProxyAddr,
	}
	pj := h.Probe(baseJob)
	pj.JobDir = probeDir
	pj.WorktreeDir = filepath.Join(probeDir, "repo")
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
			ID:       "j-probe-" + row,
			Kind:     "probe",
			About:    row,
			Question: fmt.Sprintf("harness probe failed on %s: %s", row, lastLine),
			Command:  fmt.Sprintf("nova-sprint judge j-probe-%s retry --reason fixed", row),
			Group:    "probe:" + row,
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
func (r *Runner) RecoverRow(ctx context.Context, row string) error {
	next, err := r.Sprint.Next(ctx, row)
	if err != nil {
		return err
	}
	disk, err := r.loadRowJobs_internal(row)
	if err != nil {
		return err
	}
	workingByID := map[string]ConsumerSummary{}
	for _, w := range next.Working {
		workingByID[w.ID] = w
	}
	finalByID := map[string]ConsumerSummary{}
	for _, f := range next.Final {
		finalByID[f.ID] = f
	}
	for id, rec := range disk {
		if f, isFinal := finalByID[id]; isFinal {
			if f.State == card.Fail && rec.State == JobRunning {
				r.withdrawRunningJob_internal(ctx, rec)
			}
			_ = r.cleanJobDir_internal(ctx, filepath.Join(r.RootDir, "jobs", id), rec)
			continue
		}
		if w, isWorking := workingByID[id]; isWorking {
			if err := r.recoverWorkingJob_internal(ctx, w, rec); err != nil && !errors.Is(err, ErrStoreDown) {
				return err
			}
		}
	}
	for _, w := range next.Working {
		if _, onDisk := disk[w.ID]; !onDisk {
			if err := r.reportLostJob_internal(ctx, row, w); err != nil && !errors.Is(err, ErrStoreDown) {
				return err
			}
		}
	}
	return nil
}
func (r *Runner) loadRowJobs_internal(row string) (map[string]JobRecord, error) {
	jobsRoot := filepath.Join(r.RootDir, "jobs")
	entries, err := os.ReadDir(jobsRoot)
	if os.IsNotExist(err) {
		return map[string]JobRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]JobRecord{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rec, err := ReadJobRecord(filepath.Join(jobsRoot, e.Name()))
		if err == nil && rec.Row == row {
			out[rec.Consumer] = rec
		}
	}
	return out, nil
}
func (r *Runner) recoverWorkingJob_internal(ctx context.Context, w ConsumerSummary, rec JobRecord) error {
	jobDir := filepath.Join(r.RootDir, "jobs", w.ID)
	if rec.State == JobReporting || (rec.ResultID != "" && rec.Outcome.Result != "") {
		rec.Gen = w.Gen
		return r.sendReportAndClean_internal(ctx, jobDir, rec)
	}
	if rec.State == JobRunning && r.Procs != nil && r.Procs.Alive(rec.PID, rec.Nonce) {
		r.mu.Lock()
		r.adopted[w.ID] = rec
		r.mu.Unlock()
		r.logStep_internal(rec.Row, w.ID, "adopt", fmt.Sprintf("pid=%d", rec.PID), 0)
		return nil
	}
	pushed := r.harvestPartialPush_internal(ctx, jobDir, rec)
	rec.Gen = w.Gen
	rec.PushedHead = cmpOr_internal(pushed, rec.PushedHead)
	if rec.ResultID == "" {
		rec.ResultID = fmt.Sprintf("res-%s-g%d", w.ID, w.Gen)
	}
	rec.Outcome = Outcome{Result: "fault", Head: rec.PushedHead, Reason: "restart: harness exited while runner was down"}
	rec.State = JobReporting
	if err := WriteJobRecord(jobDir, rec); err != nil {
		return err
	}
	return r.sendReportAndClean_internal(ctx, jobDir, rec)
}
func (r *Runner) reportLostJob_internal(ctx context.Context, row string, w ConsumerSummary) error {
	req := ReportRequest{Row: row, Consumer: w.ID, Gen: w.Gen, ResultID: fmt.Sprintf("res-%s-g%d-lost", w.ID, w.Gen), Result: "fault", Reason: "runner lost the job"}
	_, err := r.Sprint.Report(ctx, req)
	r.logStep_internal(row, w.ID, "lost-job", "fault", 0)
	return err
}
func (r *Runner) withdrawRunningJob_internal(ctx context.Context, rec JobRecord) {
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
	if err != nil || sha == "" || sha == rec.BaseSHA || sha == rec.PriorHead {
		return ""
	}
	if err := r.Git.Push(ctx, wt, rec.Branch, sha); err != nil {
		return ""
	}
	return sha
}
func (r *Runner) StepRow(ctx context.Context, row string) error {
	if err := r.RecoverRow(ctx, row); err != nil {
		if errors.Is(err, ErrStoreDown) {
			return nil
		}
		return err
	}
	next, err := r.Sprint.Next(ctx, row)
	if err != nil {
		if errors.Is(err, ErrStoreDown) {
			return nil
		}
		return err
	}
	if next.State != card.Up {
		return nil
	}
	workingCount := len(next.Working)
	for _, c := range next.Ready {
		if workingCount >= next.Width {
			break
		}
		workingCount++
		if err := r.RunConsumer(ctx, next, c); err != nil && !errors.Is(err, ErrNoSlot) && !errors.Is(err, ErrStoreDown) {
			return err
		}
	}
	return nil
}
func (r *Runner) RunConsumer(ctx context.Context, rowInfo NextResponse, c ConsumerSummary) error {
	jobDir := filepath.Join(r.RootDir, "jobs", c.ID)
	if _, err := os.Stat(jobDir); err == nil {
		return fmt.Errorf("worker refused: job directory %s already exists: a job directory is never reused", jobDir)
	}
	info, err := r.Sprint.Take(ctx, rowInfo.Row, c.ID, c.Gen)
	if err != nil {
		return err
	}
	job, rec, err := r.initJob_internal(rowInfo, info, jobDir)
	if err != nil {
		return err
	}
	if err := r.stageStep_internal(ctx, &job, &rec); err != nil {
		return r.sendReportAndClean_internal(ctx, jobDir, rec)
	}
	res, err := r.runStep_internal(ctx, job, &rec)
	if errors.Is(err, ErrNoSlot) {
		return ErrNoSlot
	}
	if err := r.harvestStep_internal(ctx, job, &rec, res, err); err != nil {
		return err
	}
	r.rebaseAndPRSteps_internal(ctx, job, &rec)
	return r.sendReportAndClean_internal(ctx, jobDir, rec)
}
func (r *Runner) initJob_internal(rowInfo NextResponse, info TakeInfo, jobDir string) (Job, JobRecord, error) {
	if err := os.MkdirAll(filepath.Join(jobDir, "home"), 0o755); err != nil {
		return Job{}, JobRecord{}, err
	}
	_ = os.MkdirAll(filepath.Join(jobDir, "tmp"), 0o755)
	_ = os.MkdirAll(filepath.Join(jobDir, "data"), 0o755)
	wt := filepath.Join(jobDir, "repo")
	now := r.Now().UTC()
	dl := now.Add(r.Timeouts.DeadlineFor(info.Tier))
	job := Job{
		Consumer: info.Consumer, Producer: info.Producer, Kind: info.Kind,
		Row: rowInfo.Row, Stream: info.Stream, Repo: info.Repo, Base: info.Base,
		StreamRef: info.StreamRef, Branch: info.Branch, BaseSHA: info.BaseSHA,
		PriorHead: info.PriorHead, Head: info.Head, Tier: info.Tier,
		Model: rowInfo.Model, HarnessName: rowInfo.Harness, Brief: info.Brief,
		Prose: info.Prose, Finding: info.Finding, Paths: info.Paths,
		DoneWhen: info.DoneWhen, Gen: info.Gen, Attempt: info.Attempt,
		Review: info.Review, SlotWeight: 1, JobDir: jobDir, WorktreeDir: wt,
		ProxyAddr: r.ProxyAddr, CommitName: rowInfo.Row,
		CommitEmail: rowInfo.Row + "@nova-sprint.local", RequirePR: info.RequirePR,
	}
	rec := JobRecord{
		Consumer: info.Consumer, Producer: info.Producer, Kind: info.Kind,
		Row: rowInfo.Row, Stream: info.Stream, Repo: info.Repo, Base: info.Base,
		StreamRef: info.StreamRef, Branch: info.Branch, BaseSHA: info.BaseSHA,
		PriorHead: info.PriorHead, TargetHead: info.Head, Gen: info.Gen,
		Attempt: info.Attempt, ResultID: fmt.Sprintf("res-%s-g%d", info.Consumer, info.Gen),
		State: JobTaken, StartedAt: now.Unix(), DeadlineAt: dl.Unix(),
		RequirePR: info.RequirePR,
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
			rec.State = JobReporting
			rec.Outcome = Outcome{Result: "fault", Reason: "staging fault: " + err.Error()}
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
func (r *Runner) runStep_internal(ctx context.Context, job Job, rec *JobRecord) (WallRunResult, error) {
	h, err := r.harnessFor_internal(job.HarnessName)
	if err != nil {
		return WallRunResult{}, err
	}
	argv, env := h.Command(job)
	rec.State = JobRunning
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
			rec.PID, rec.Nonce = last.PID, last.Nonce
			_ = WriteJobRecord(job.JobDir, *rec)
		}
		_ = os.WriteFile(filepath.Join(job.JobDir, "harness.log"), last.Output, 0o600)
		out := ClassifyJob(h, job, last.ExitCode, last.Elapsed, r.Timeouts.LaunchGrace, last.Output)
		if !out.Retryable5xx || attempt == tries {
			break
		}
		backoff := r.Timeouts.LaunchBackoff1Min
		if attempt >= 2 {
			backoff = r.Timeouts.LaunchBackoff2Min
		}
		r.Sleep(backoff)
	}
	r.logStep_internal(job.Row, job.Consumer, "run", fmt.Sprintf("exit=%d", last.ExitCode), last.Elapsed)
	return last, runErr
}
func (r *Runner) harvestStep_internal(ctx context.Context, job Job, rec *JobRecord, res WallRunResult, runErr error) error {
	h, err := r.harnessFor_internal(job.HarnessName)
	if err != nil {
		return err
	}
	out := ClassifyJob(h, job, res.ExitCode, res.Elapsed, r.Timeouts.LaunchGrace, res.Output)
	if runErr != nil && out.Result != "fault" {
		out = Outcome{Result: "fault", Reason: "wall execution fault: " + runErr.Error()}
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
				rec.PushedHead = sha
				out.Head = sha
			}
		} else if out.Result == "ok" {
			out = Outcome{Result: "fail", Reason: "harness reported ok without advancing HEAD past base_sha and prior_head"}
		}
	} else if job.Kind == "read" {
		out.Head = job.Head
	}
	rec.Outcome = out
	rec.State = JobHarvested
	return WriteJobRecord(job.JobDir, *rec)
}
func (r *Runner) rebaseAndPRSteps_internal(ctx context.Context, job Job, rec *JobRecord) {
	if job.Kind != "work" || rec.Outcome.Result != "ok" {
		return
	}
	if r.Git != nil && job.StreamRef != "" {
		newSHA, err := r.Git.RebaseOntoStream(ctx, job.WorktreeDir, job.StreamRef)
		if err != nil {
			var ce *ConflictError
			if errors.As(err, &ce) {
				rec.Outcome = Outcome{Result: "fail", Head: rec.PushedHead, Reason: ce.Error()}
			} else {
				rec.Outcome = Outcome{Result: "fail", Head: rec.PushedHead, Reason: "rebase failed: " + err.Error()}
			}
			_ = WriteJobRecord(job.JobDir, *rec)
			return
		}
		if newSHA != "" {
			rec.Outcome.Head = newSHA
		}
	}
	for _, cmd := range job.DoneWhen {
		if strings.TrimSpace(cmd) == "" || r.Wall == nil {
			continue
		}
		tres, err := r.Wall.RunTest(ctx, job, cmd, r.Timeouts.RebaseAndTest)
		if err != nil || tres.ExitCode != 0 {
			rec.Outcome = Outcome{Result: "fail", Head: rec.PushedHead, Reason: fmt.Sprintf("tests red (%s): %s", cmd, TailBytes(tres.Output, MaxReasonBytes))}
			_ = WriteJobRecord(job.JobDir, *rec)
			return
		}
	}
	if r.Git != nil && rec.Outcome.Head != "" && rec.Outcome.Head != rec.PushedHead {
		if err := r.Git.Push(ctx, job.WorktreeDir, job.Branch, rec.Outcome.Head); err != nil {
			rec.Outcome = Outcome{Result: "fault", Reason: "post-rebase push fault: " + err.Error()}
			_ = WriteJobRecord(job.JobDir, *rec)
			return
		}
		rec.PushedHead = rec.Outcome.Head
	}
	rec.State = JobRebased
	if job.RequirePR && r.Forge != nil {
		pr, err := r.Forge.EnsurePR(ctx, job.Repo, job.Branch, job.Base, rec.PushedHead, job.Consumer)
		if err != nil {
			rec.Outcome = Outcome{Result: "fault", Head: rec.PushedHead, Reason: "open PR fault: " + err.Error()}
			_ = WriteJobRecord(job.JobDir, *rec)
			return
		}
		rec.PRNumber = pr
	}
	_ = WriteJobRecord(job.JobDir, *rec)
}
func (r *Runner) sendReportAndClean_internal(ctx context.Context, jobDir string, rec JobRecord) error {
	rec.State = JobReporting
	if rec.ResultID == "" {
		rec.ResultID = fmt.Sprintf("res-%s-g%d", rec.Consumer, rec.Gen)
	}
	if err := WriteJobRecord(jobDir, rec); err != nil {
		return err
	}
	req := ReportRequest{
		Row:      rec.Row,
		Consumer: rec.Consumer,
		Gen:      rec.Gen,
		ResultID: rec.ResultID,
		Result:   rec.Outcome.Result,
		Head:     rec.Outcome.Head,
		Reason:   rec.Outcome.Reason,
		Finding:  rec.Outcome.Finding,
		Cost:     rec.Cost.MicroUSD,
		Priced:   rec.Cost.Priced,
	}
	receipt, err := r.Sprint.Report(ctx, req)
	if err != nil {
		return err
	}
	r.logStep_internal(rec.Row, rec.Consumer, "report", fmt.Sprintf("accepted=%v", receipt.Accepted), 0)
	rec.State = JobRecorded
	_ = WriteJobRecord(jobDir, rec)
	return r.cleanJobDir_internal(ctx, jobDir, rec)
}
func (r *Runner) cleanJobDir_internal(ctx context.Context, jobDir string, rec JobRecord) error {
	wt := filepath.Join(jobDir, "repo")
	if rec.Kind == "work" && rec.PushedHead != "" && r.Git != nil {
		ok, err := r.Git.OriginHasHead(ctx, rec.Repo, rec.Branch, rec.PushedHead)
		if err != nil || !ok {
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
		_ = r.RecoverRow(ctx, row)
		next, err := r.Sprint.Next(ctx, row)
		if err != nil {
			return EdgeResult{}, err
		}
		for _, f := range next.Final {
			if f.ID == req.Card {
				return EdgeResult{ID: id, Kind: req.Kind, Card: req.Card, SHA: cmpOr_internal(f.Head, req.SHA), Outcome: f.State}, nil
			}
		}
		for _, w := range next.Working {
			if w.ID == req.Card {
				return EdgeResult{ID: id, Kind: req.Kind, Card: req.Card, SHA: cmpOr_internal(w.Head, req.SHA), Outcome: card.Working}, nil
			}
		}
		for _, rd := range next.Ready {
			if rd.ID == req.Card {
				return EdgeResult{ID: id, Kind: req.Kind, Card: req.Card, SHA: req.SHA, Outcome: card.Ready}, nil
			}
		}
	}
	if rec, err := ReadJobRecord(jobDir); err == nil {
		return EdgeResult{ID: id, Kind: req.Kind, Card: req.Card, SHA: cmpOr_internal(rec.PushedHead, req.SHA), Outcome: rec.State, Failure: rec.Outcome.Reason}, nil
	}
	return EdgeResult{ID: id, Kind: req.Kind, Card: req.Card, SHA: req.SHA, Outcome: JobNone}, nil
}

type OSGitOps struct {
	WorkCloneDir, MirrorDir, OriginURL string
}

func (g *OSGitOps) StageWorktree(ctx context.Context, j Job) (string, error) {
	if g.MirrorDir != "" && filepath.Clean(g.WorkCloneDir) == filepath.Clean(g.MirrorDir) {
		return "", errors.New("stage refused: work clone path equals bare mirror path (worktrees must be staged from the work clone, never the mirror)")
	}
	if _, err := runGit_internal(ctx, g.WorkCloneDir, "fetch", "--quiet", "origin"); err != nil {
		return "", err
	}
	baseRef := cmpOr_internal(j.PriorHead, cmpOr_internal(j.BaseSHA, "HEAD"))
	if j.Kind == "read" {
		baseRef = cmpOr_internal(j.Head, baseRef)
		if _, err := runGit_internal(ctx, g.WorkCloneDir, "worktree", "add", "--detach", j.WorktreeDir, baseRef); err != nil {
			return "", err
		}
		_, _ = runGit_internal(ctx, j.WorktreeDir, "remote", "set-url", "--push", "origin", "no-push-on-read")
	} else {
		if _, err := runGit_internal(ctx, g.WorkCloneDir, "worktree", "add", "-B", j.Branch, j.WorktreeDir, baseRef); err != nil {
			return "", err
		}
	}
	if j.CommitName != "" {
		_, _ = runGit_internal(ctx, j.WorktreeDir, "config", "user.name", j.CommitName)
	}
	if j.CommitEmail != "" {
		_, _ = runGit_internal(ctx, j.WorktreeDir, "config", "user.email", j.CommitEmail)
	}
	return g.HeadSHA(ctx, j.WorktreeDir)
}
func (g *OSGitOps) HeadSHA(ctx context.Context, worktree string) (string, error) {
	out, err := runGit_internal(ctx, worktree, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}
func (g *OSGitOps) Push(ctx context.Context, worktree, branch, headSHA string) error {
	target := cmpOr_internal(g.OriginURL, "origin")
	_, err := runGit_internal(ctx, worktree, "push", "--force", target, headSHA+":refs/heads/"+branch)
	return err
}
func (g *OSGitOps) RebaseOntoStream(ctx context.Context, worktree, streamRef string) (string, error) {
	target := cmpOr_internal(g.OriginURL, "origin")
	if _, err := runGit_internal(ctx, worktree, "fetch", "--quiet", target, streamRef); err != nil {
		return g.HeadSHA(ctx, worktree)
	}
	if _, err := runGit_internal(ctx, worktree, "rebase", "FETCH_HEAD"); err != nil {
		out, _ := runGit_internal(ctx, worktree, "diff", "--name-only", "--diff-filter=U")
		_, _ = runGit_internal(ctx, worktree, "rebase", "--abort")
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
	target := cmpOr_internal(g.OriginURL, "origin")
	out, err := runGit_internal(ctx, g.WorkCloneDir, "ls-remote", target, "refs/heads/"+branch)
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(strings.TrimSpace(out), sha), nil
}
func (g *OSGitOps) HasHead(ctx context.Context, repo, branch, sha string) (bool, error) {
	return g.OriginHasHead(ctx, repo, branch, sha)
}
func (g *OSGitOps) IsDirty(ctx context.Context, worktree string) (bool, error) {
	out, err := runGit_internal(ctx, worktree, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}
func (g *OSGitOps) RemoveWorktree(ctx context.Context, worktree string) error {
	_, err := runGit_internal(ctx, g.WorkCloneDir, "worktree", "remove", "--force", worktree)
	return err
}
func runGit_internal(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, TailBytes(out, 512))
	}
	return string(out), nil
}

type OSReserveWall struct {
	Config     WallConfig
	UseSandbox bool
}

func (w *OSReserveWall) RunHarness(ctx context.Context, j Job, argv, env []string, timeout time.Duration) (WallRunResult, error) {
	var fullArgv []string
	if w.UseSandbox {
		fullArgv = BuildWallArgv(w.Config, j, argv)
	} else if w.Config.ReserveBin != "" {
		fullArgv = buildReserveOnlyArgv_internal(w.Config, j, argv)
	} else {
		fullArgv = argv
	}
	return execBounded_internal(ctx, j.WorktreeDir, fullArgv, env, timeout)
}
func (w *OSReserveWall) RunTest(ctx context.Context, j Job, cmdStr string, timeout time.Duration) (WallRunResult, error) {
	argv := []string{"sh", "-c", cmdStr}
	if w.Config.ReserveBin != "" {
		argv = buildReserveOnlyArgv_internal(w.Config, j, argv)
	}
	return execBounded_internal(ctx, j.WorktreeDir, argv, SanitizeWallEnv(os.Environ(), j.JobDir), timeout)
}
func buildReserveOnlyArgv_internal(cfg WallConfig, j Job, inner []string) []string {
	resBin := cmpOr_internal(cfg.ReserveBin, "nova-reserve")
	argv := []string{resBin}
	if cfg.ReserveDir != "" {
		argv = append(argv, "--dir", cfg.ReserveDir)
	}
	weight := j.SlotWeight
	if weight <= 0 {
		weight = 1
	}
	wait := cfg.ReserveWait
	if wait <= 0 {
		wait = 5 * time.Second
	}
	argv = append(argv, "run", "--slots", strconv.Itoa(weight), "--wait", wait.String(), "--why", cmpOr_internal(j.Consumer, "test"), "--")
	return append(argv, inner...)
}
func execBounded_internal(ctx context.Context, dir string, argv, env []string, timeout time.Duration) (WallRunResult, error) {
	if len(argv) == 0 {
		return WallRunResult{}, errors.New("empty command argv")
	}
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	start := time.Now()
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin = nil
	if len(env) > 0 {
		cmd.Env = env
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	elapsed := time.Since(start)
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	return WallRunResult{ExitCode: code, Output: buf.Bytes(), PID: pid, Nonce: strconv.Itoa(pid), Elapsed: elapsed}, err
}
