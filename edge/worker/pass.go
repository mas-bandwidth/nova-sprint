package worker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
)

// Passes and jobs are separate (defect 66, tla/Runner.tla): a pass reads the
// row, sends what is owed, and starts what it can; the jobs it starts run on
// their own, and the next pass never waits for them. A job is one consumer
// from take to report (or to a resume of one the runner stopped).

// WakeEvery is how often the wake loop runs a pass without a wake, beside
// the bus hook's wakes, so a missed push never leaves a dealt card sitting
// (defect 66). A guess (the stopgap's 10 s), not measured.
const WakeEvery = 10 * time.Second

type (
	// launch_internal is one job a pass starts: a dealt card to take, or a
	// working card of this row's to resume from its job record. done ends
	// the consumer's in-flight mark; ctx is the job's, cancelled on a
	// withdraw or when the worker stops.
	launch_internal struct {
		ctx    context.Context
		c      ConsumerSummary
		resume *JobRecord
		done   func()
	}
	// prepared_internal is a launch with its job built: stage says it needs
	// a staged worktree (one fetch of its repo first), harvestOnly that its
	// harness already ran and wrote its verdict, end that it is finished.
	prepared_internal struct {
		launch_internal
		job                     Job
		rec                     JobRecord
		stage, harvestOnly, end bool
		err                     error
	}
)

// wakeLoop_internal is RunWakeLoop's loop on a hook already started: a pass
// now, then one per burst of wakes and one every WakeEvery. A pass runs on
// a goroutine of its own and starts its jobs detached; wakes while it runs
// coalesce into one more pass. It returns nil when ctx ends, the hook's
// error when the bus hook dies, and a refused receipt (ErrReceipt) from a
// pass or a job; each way it stops the jobs and waits for them, and each
// stopped job keeps its record to be resumed (defect 73).
func (r *Runner) wakeLoop_internal(ctx context.Context, row string, hook WakeHook) error {
	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	passDone, running, pending := make(chan error, 1), false, true
	stop := func() {
		cancel()
		if running {
			<-passDone
		}
		r.jobs.Wait()
	}
	tick := r.periodic_internal()
	for {
		if pending && !running {
			pending, running = false, true
			go func() { passDone <- r.stepRow_internal(loopCtx, row, true) }()
		}
		select {
		case <-ctx.Done():
			stop()
			return nil
		case <-hook.Wake():
			pending = true
		case <-tick:
			pending, tick = true, r.periodic_internal()
		case err := <-r.fatal:
			stop()
			return err
		case err := <-passDone:
			running = false
			if err != nil {
				r.logStep_internal(row, "-", "pass", "error: "+err.Error(), 0)
				if errors.Is(err, ErrReceipt) {
					stop()
					return err
				}
			}
		case <-hook.Done():
			stop()
			return hook.Err()
		}
	}
}

// periodic_internal is the next periodic wake, or nil (never) with no timer.
func (r *Runner) periodic_internal() <-chan time.Time {
	if r.After == nil {
		return nil
	}
	return r.After(cmp.Or(r.WakeEvery, WakeEvery))
}

// wait_internal waits d on the runner's timer, or until ctx ends; with no
// timer it is the runner's Sleep.
func (r *Runner) wait_internal(ctx context.Context, d time.Duration) {
	if r.After == nil {
		r.Sleep(d)
		return
	}
	select {
	case <-ctx.Done():
	case <-r.After(d):
	}
}

// ProbeBackoff is the first wait before a failed probe is run again, and
// ProbeBackoffMax the longest; the wait doubles between them. Both are
// guesses (not measured): long enough not to spin a provider that is down,
// short enough that a fixed key is picked up within minutes.
const (
	ProbeBackoff    = 30 * time.Second
	ProbeBackoffMax = 10 * time.Minute
)

// probeHeld_internal probes the row until its harness passes (defect 61): a
// failed probe never exits the worker. The row stays unhooked (dealt
// nothing) and is held with the probe's reason, raised on stderr; the probe
// runs again after a backoff. True when it passed, false when ctx ended.
func (r *Runner) probeHeld_internal(ctx context.Context, row, harness string) bool {
	wait := ProbeBackoff
	for {
		err := r.ProbeRow(ctx, row, harness)
		if err == nil {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		r.report(fmt.Sprintf("row %s held (unhooked, probed again in %s): its probe failed", row, wait), err)
		r.logStep_internal(row, "-", "probe", "held: "+err.Error(), 0)
		r.wait_internal(ctx, wait)
		wait = min(2*wait, ProbeBackoffMax)
	}
}

func ignoreStoreDown_internal(err error) error {
	if errors.Is(err, ErrStoreDown) {
		return nil
	}
	return err
}

// StepRow is one pass that waits for the jobs it starts (--once and the
// tests); the wake loop's passes start theirs detached.
func (r *Runner) StepRow(ctx context.Context, row string) error {
	return r.stepRow_internal(ctx, row, false)
}

// stepRow_internal is one pass. Detached, its jobs run under the runner's
// WaitGroup and a refused receipt among them reaches the loop on fatal.
func (r *Runner) stepRow_internal(ctx context.Context, row string, detach bool) error {
	next, launches, err := r.planPass_internal(ctx, row)
	if err != nil || len(launches) == 0 {
		return ignoreStoreDown_internal(err)
	}
	if !detach {
		return r.runBatch_internal(ctx, next, launches)
	}
	r.jobs.Add(1)
	go func() {
		defer r.jobs.Done()
		if err := r.runBatch_internal(ctx, next, launches); err != nil {
			r.logStep_internal(row, "-", "jobs", "error: "+err.Error(), 0)
			if errors.Is(err, ErrReceipt) {
				select {
				case r.fatal <- err:
				default:
				}
			}
		}
	}()
	return nil
}

// planPass_internal recovers the row's jobs (what is owed is sent, what was
// stopped is resumed), then reads the row and claims its dealt cards up to
// width. Each launch is claimed (in flight) before the pass returns, so the
// next pass never starts it twice.
func (r *Runner) planPass_internal(ctx context.Context, row string) (NextResponse, []launch_internal, error) {
	resumes, err := r.recoverRow_internal(ctx, row)
	if err != nil {
		return NextResponse{}, nil, err
	}
	next, err := r.Sprint.Next(ctx, row)
	if err != nil {
		return next, nil, err
	}
	var launches []launch_internal
	for _, l := range resumes {
		if l, ok := r.claim_internal(ctx, l.c, l.resume); ok {
			launches = append(launches, l)
		}
	}
	if next.State != card.Up || next.Held {
		return next, launches, nil
	}
	// The deal is the take (defect 52): every card in hand is working from
	// its deal, within the row's width, so the pass starts a job for each
	// one with none yet (on disk, in flight, or set aside); its take is the
	// acknowledgement. One with a job on disk is recovery's.
	for _, c := range next.Working {
		if fileExists_internal(filepath.Join(r.RootDir, "jobs", c.ID)) || r.quarantined_internal(c) {
			continue
		}
		if l, ok := r.claim_internal(ctx, c, nil); ok {
			launches = append(launches, l)
		}
	}
	return next, launches, nil
}

// claim_internal marks c in flight with a job context of its own, or is
// false when it already is.
func (r *Runner) claim_internal(ctx context.Context, c ConsumerSummary, resume *JobRecord) (launch_internal, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, busy := r.running[c.ID]; busy {
		return launch_internal{}, false
	}
	delete(r.withdrawn, c.ID)
	runCtx, cancel := context.WithCancel(ctx)
	r.running[c.ID] = cancel
	var once sync.Once
	done := func() {
		once.Do(func() { r.mu.Lock(); delete(r.running, c.ID); r.mu.Unlock(); cancel() })
	}
	return launch_internal{ctx: runCtx, c: c, resume: resume, done: done}, true
}

// RunConsumer runs one dealt card as a pass would, and waits for it.
func (r *Runner) RunConsumer(ctx context.Context, rowInfo NextResponse, c ConsumerSummary) error {
	l, ok := r.claim_internal(ctx, c, nil)
	if !ok {
		return nil
	}
	return r.runJobs_internal(ctx, rowInfo, []launch_internal{l})[0].err
}

// runBatch_internal runs one pass's launches: each is prepared (taken, or
// its record read back), every repo they stage is fetched once (defect 72:
// no two fetches into one work clone at once, and one per repo per pass),
// then each job runs on its own. The first error that is not a missing
// slot or a down store is returned; each launch's claim ends with its job.
func (r *Runner) runBatch_internal(ctx context.Context, next NextResponse, launches []launch_internal) error {
	var first error
	for _, p := range r.runJobs_internal(ctx, next, launches) {
		if p.err != nil && !errors.Is(p.err, ErrNoSlot) && !errors.Is(p.err, ErrStoreDown) {
			first = cmp.Or(first, p.err)
		}
	}
	return first
}

// runJobs_internal runs launches as runBatch_internal says and returns each
// one's prepared job with its error.
func (r *Runner) runJobs_internal(ctx context.Context, next NextResponse, launches []launch_internal) []prepared_internal {
	ps := make([]prepared_internal, len(launches))
	var wg sync.WaitGroup
	for i, l := range launches {
		wg.Go(func() { ps[i] = r.prepare_internal(next, l) })
	}
	wg.Wait()
	fetched := r.fetchRepos_internal(ctx, ps)
	for i := range ps {
		if ps[i].end {
			ps[i].done()
			continue
		}
		wg.Go(func() {
			defer ps[i].done()
			ps[i].err = r.finishJob_internal(&ps[i], fetched[repoKey_internal(ps[i].job)])
		})
	}
	wg.Wait()
	return ps
}

// repoKey_internal groups jobs by the work clone they stage from.
func repoKey_internal(j Job) string { return j.Forge + "|" + j.Repo }

// fetchRepos_internal fetches each repo the prepared jobs stage from, once,
// the repos at once; the error per repo (nil: fetched).
func (r *Runner) fetchRepos_internal(ctx context.Context, ps []prepared_internal) map[string]error {
	first := map[string]Job{}
	for _, p := range ps {
		if !p.end && p.stage {
			if _, ok := first[repoKey_internal(p.job)]; !ok {
				first[repoKey_internal(p.job)] = p.job
			}
		}
	}
	out := make(map[string]error, len(first))
	if r.Git == nil {
		return out
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for key, j := range first {
		wg.Go(func() {
			fetchCtx, cancel := context.WithTimeout(ctx, r.Timeouts.Stage)
			err := r.Git.Fetch(fetchCtx, j)
			cancel()
			mu.Lock()
			out[key] = err
			mu.Unlock()
		})
	}
	wg.Wait()
	return out
}

// prepare_internal takes a dealt card (or reads back a resumed one's
// receipt) and builds its job; a receipt the worker cannot use is reported
// a fault and ends the worker (ErrReceipt).
func (r *Runner) prepare_internal(next NextResponse, l launch_internal) prepared_internal {
	p := prepared_internal{launch_internal: l}
	if l.resume != nil {
		r.prepareResume_internal(next, &p)
		return p
	}
	jobDir := filepath.Join(r.RootDir, "jobs", l.c.ID)
	if fileExists_internal(jobDir) {
		p.end, p.err = true, fmt.Errorf("worker refused: job directory %s already exists: a job directory is never reused", jobDir)
		return p
	}
	info, err := r.Sprint.Take(l.ctx, next.Row, l.c.ID, l.c.Gen)
	if err == nil {
		err = checkReceipt_internal(info)
	}
	if errors.Is(err, ErrReceipt) {
		p.end, p.err = true, r.refuseReceipt_internal(l.ctx, jobDir, next, l.c, err)
		return p
	}
	if err == nil {
		p.job, p.rec, err = r.initJob_internal(next, info, jobDir)
	}
	if err != nil {
		p.end, p.err = true, err
		return p
	}
	p.stage = true
	r.modelFor_internal(&p, info)
	return p
}

// modelFor_internal sets the job's model (chooseModel_internal); none is a
// fault reported now, and the job ends.
func (r *Runner) modelFor_internal(p *prepared_internal, info TakeInfo) {
	m, err := r.chooseModel_internal(p.job.HarnessName, info)
	if err == nil {
		p.job.Model = m
		return
	}
	p.rec.State, p.rec.Outcome = JobReporting, Outcome{Result: "fault", Reason: err.Error()}
	p.end, p.err = true, r.sendReportAndClean_internal(p.ctx, p.job.JobDir, p.rec)
}

// finishJob_internal is a prepared job from where it stands to its report:
// stage (from the fetched clone), run, harvest, rebase and test, report. A
// job the worker's stop cut short keeps its record to be resumed.
func (r *Runner) finishJob_internal(p *prepared_internal, fetchErr error) error {
	ctx, jobDir := p.ctx, p.job.JobDir
	if p.stage {
		if fetchErr != nil {
			if ctx.Err() != nil {
				return r.stopped_internal(p)
			}
			p.rec.State, p.rec.Outcome = JobReporting, Outcome{Result: "fault", Reason: "staging fault: " + fetchErr.Error()}
			return r.send_internal(ctx, jobDir, p.rec)
		}
		if err := r.stageStep_internal(ctx, &p.job, &p.rec); err != nil {
			if ctx.Err() != nil {
				return r.stopped_internal(p)
			}
			return r.send_internal(ctx, jobDir, p.rec)
		}
	}
	if p.rec.State != JobHarvested {
		if done, err := r.runAndHarvest_internal(p); done {
			return err
		}
	}
	if err := r.rebaseAndTestStep_internal(ctx, jobDir, &p.rec); err != nil {
		return err
	}
	return r.send_internal(ctx, jobDir, p.rec)
}

// send_internal sends a finished job's report; a refusal is counted on its
// record as every later resend's is (resendErr_internal), so the first
// refusal is one of the RefusalLimit.
func (r *Runner) send_internal(ctx context.Context, jobDir string, rec JobRecord) error {
	err := r.sendReportAndClean_internal(ctx, jobDir, rec)
	r.resendErr_internal(ctx, jobDir, rec, err)
	return err
}

// runAndHarvest_internal runs the harness (unless it already ran and wrote
// its verdict) and harvests it. done: the job ends here (withdrawn, stopped,
// no slot yet, or a harvest that could not be written).
func (r *Runner) runAndHarvest_internal(p *prepared_internal) (bool, error) {
	ctx := p.ctx
	var res WallRunResult
	var err error
	if !p.harvestOnly {
		res, err = r.runStep_internal(ctx, p.job, &p.rec)
		r.mu.Lock()
		withdrawn := r.withdrawn[p.c.ID]
		r.mu.Unlock()
		switch {
		case withdrawn:
			r.withdrawRunningJob_internal(p.rec)
			r.retireJob_internal(p.job.JobDir, p.rec, "consumer withdrawn while its harness ran")
			return true, nil
		case ctx.Err() != nil:
			return true, r.stopped_internal(p)
		case errors.Is(err, ErrNoSlot):
			return true, err
		}
	}
	if herr := r.harvestStep_internal(ctx, p.job, &p.rec, res, err); herr != nil {
		return true, herr
	}
	return false, nil
}

// stopped_internal keeps a job the worker's stop cut short where it stood,
// with no harness, to be resumed by the next start (defect 73): never a
// report, never a fault, never the wall's timeout.
func (r *Runner) stopped_internal(p *prepared_internal) error {
	if p.rec.State == JobReporting && p.rec.Outcome.Result != "" {
		p.rec.State, p.rec.Outcome = JobTaken, Outcome{}
	}
	p.rec.PID, p.rec.Nonce = 0, ""
	r.logStep_internal(p.job.Row, p.job.Consumer, "stop", "runner stopped; kept at "+p.rec.State+" to resume", 0)
	return WriteJobRecord(p.job.JobDir, p.rec)
}
