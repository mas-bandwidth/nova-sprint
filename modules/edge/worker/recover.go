package worker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// RefusalLimit is how many identical refusals of one record's report the
// worker takes before it sets the record aside for a reader (defect 69). A
// guess, not measured.
const RefusalLimit = 3

// ReceiptName is the take receipt as the worker read it, kept in the job
// directory from the take on, so a job a pass left (staged with no slot, or
// harvested with a fault to send) goes on in a later pass (defects 61, 70)
// without asking the sprint for it again.
const ReceiptName = "receipt.json"

// recoverRow_internal does a pass's recovery and returns the jobs to go on
// with (not yet claimed), and the reports it owed and could not send. A
// restart resumes nothing: the start's self-unhook gave the row's whole hand
// back (2026-11-10: a bench that restarts gives up its cards and the dealer
// deals them again), so the old session's jobs are final here and retired.
// One record the sprint keeps refusing never ends it (defect 69): it is
// logged, kept, and sent again next pass.
func (r *Runner) recoverRow_internal(ctx context.Context, row string) ([]launch_internal, error, error) {
	next, err := r.Sprint.Next(ctx, row)
	if err != nil {
		return nil, nil, err
	}
	disk, err := r.loadRowJobs_internal(row)
	if err != nil {
		return nil, nil, err
	}
	workingByID, finalByID := r.holders_internal(next)
	var out []launch_internal
	var unsent []error
	for id, rec := range disk {
		if f, isFinal := finalByID[id]; isFinal {
			if !r.isRunningInFlight_internal(id) { // a job in flight ends and retires itself
				r.finalJob_internal(f, rec)
			}
		} else if w, isWorking := workingByID[id]; isWorking && !r.isRunningInFlight_internal(id) {
			if sameEpoch_internal(next, rec) && rec.Gen < w.Gen {
				r.retireJob_internal(filepath.Join(r.RootDir, "jobs", id), rec, fmt.Sprintf("generation %d is stale: the row holds %s at %d", rec.Gen, id, w.Gen))
			} else if !jobMatchesSummary_internal(next, w, rec) {
				r.logStep_internal(row, id, "recover", "identity-mismatch-kept", 0)
			} else if rec, resume, err := r.recoverWorkingJob_internal(ctx, w, rec); resume {
				out = append(out, launch_internal{c: w, resume: &rec})
			} else if err != nil {
				unsent = append(unsent, err)
			}
		}
	}
	return out, errors.Join(unsent...), nil
}

// holders_internal is the row's working and final consumers by id, and
// records who holds a branch (holds_internal): a final consumer's jobs
// none, a working one's only at its gen or above; a withdrawn consumer's
// job in flight is cancelled.
func (r *Runner) holders_internal(next NextResponse) (map[string]ConsumerSummary, map[string]ConsumerSummary) {
	workingByID, finalByID := map[string]ConsumerSummary{}, map[string]ConsumerSummary{}
	for _, w := range next.Working {
		workingByID[w.ID] = w
		r.markGone_internal(next.Epoch, w.ID, w.Gen)
	}
	for _, f := range next.Final {
		finalByID[f.ID] = f
		r.markGone_internal(next.Epoch, f.ID, math.MaxInt)
		if f.State == card.Fail {
			r.cancelRunning_internal(f.ID)
		}
	}
	return workingByID, finalByID
}

// finalJob_internal ends a job whose card is final, on the first pass that
// sees it: a harness left running is stopped, nothing is pushed (the branch
// is the card's next holder's), and the directory is retired (defect 78).
func (r *Runner) finalJob_internal(f ConsumerSummary, rec JobRecord) {
	if rec.State == JobRunning {
		r.withdrawRunningJob_internal(rec)
	}
	r.retireJob_internal(filepath.Join(r.RootDir, "jobs", rec.Consumer), rec, "consumer "+f.State)
}

// recoverWorkingJob_internal acts on a working card's record: a report it
// owes is sent; anything else (staged with no slot, harvested with a fault
// to send) goes on, which the second result says. A live harness is never
// this worker's own (its harness ends with its job), so it is a dead
// worker's: it is killed and the card given back, as that worker's restart
// gives up its hand. The error is an owed report not sent.
func (r *Runner) recoverWorkingJob_internal(ctx context.Context, w ConsumerSummary, rec JobRecord) (JobRecord, bool, error) {
	jobDir := filepath.Join(r.RootDir, "jobs", w.ID)
	switch {
	case rec.State == JobCleaned || rec.State == JobRecorded:
		return rec, false, nil
	case rec.State == JobCIWait || rec.State == JobReporting || rec.State == JobRebased:
		return rec, false, r.resend_internal(ctx, jobDir, rec)
	case rec.State == JobRunning && r.Procs != nil && r.Procs.Alive(rec.PID, rec.Nonce):
		r.killTracked_internal("stopping the harness a dead worker left for "+w.ID, rec.PID, rec.Nonce)
		r.releaseNoBlame_internal(ctx, rec, "its worker restarted")
		return rec, false, nil
	}
	return rec, true, nil
}

// prepareResume_internal builds a job a pass left back from its directory:
// the receipt it was taken under, its record, and where it stood. A job
// with no receipt cannot go on; it is released for a redeal with no fault
// and no blame.
func (r *Runner) prepareResume_internal(next NextResponse, p *prepared_internal) {
	rec, jobDir := *p.resume, filepath.Join(r.RootDir, "jobs", p.resume.Consumer)
	info, err := readReceipt_internal(jobDir)
	if err != nil {
		p.end = true
		r.releaseNoBlame_internal(p.ctx, rec, "the job cannot go on: "+err.Error())
		return
	}
	p.job, _ = buildJob_internal(next, info, jobDir, r.ProxyAddr, r.Now())
	p.rec = rec
	r.logStep_internal(rec.Row, rec.Consumer, "resume", "from "+rec.State, 0)
	switch {
	case rec.State == JobHarvested:
		return
	case rec.State == JobTaken || !fileExists_internal(p.job.WorktreeDir):
		p.stage, p.rec.State = true, JobTaken
	default:
		p.rec.State, p.rec.PID, p.rec.Nonce = JobStaged, 0, ""
	}
	// The record's deadline stands: the sprint counts the card's from the
	// deal, however many passes the job waited for a slot, and the done-when
	// gates run until it (doneWhenBound_internal). Only a record without one
	// (from before deadlines were recorded) counts from now.
	if p.rec.DeadlineAt == 0 {
		p.rec.DeadlineAt = r.Now().Unix() + int64(info.Deadline)
	}
	r.modelFor_internal(p, info)
}

// releaseNoBlame_internal gives a card the worker cannot go on with back to
// the sprint for a redeal: its commits pushed first, once, while it still
// holds the card (by lease from the head it last pushed), then `release`
// (no result, no fault, no bounce: neither ok nor fail) with that head. The
// directory is retired once the release is recorded, and nothing is pushed
// after it (defect 78); a release that fails is said and tried again by the
// next pass, whose push of the same head is skipped.
func (r *Runner) releaseNoBlame_internal(ctx context.Context, rec JobRecord, why string) {
	jobDir := filepath.Join(r.RootDir, "jobs", rec.Consumer)
	if fileExists_internal(jobDir) {
		if pushed := r.harvestPartialPush_internal(ctx, jobDir, rec); pushed != "" {
			rec.PushedHead = pushed
			r.report("job record "+rec.Consumer, WriteJobRecord(jobDir, rec))
		}
	}
	err := r.Sprint.Release(ctx, rec.Row, rec.Consumer, rec.Gen, rec.PushedHead, why)
	if err != nil {
		r.report(fmt.Sprintf("release of %s@%d (%s)", rec.Consumer, rec.Gen, why), err)
		return
	}
	r.markGone_internal(rec.Epoch, rec.Consumer, math.MaxInt)
	r.logStep_internal(rec.Row, rec.Consumer, "release", "no blame: "+why, 0)
	if fileExists_internal(jobDir) {
		r.retireJob_internal(jobDir, rec, "released with no blame")
	}
}

// resend_internal sends a report a record owes again; the error is the
// send's.
func (r *Runner) resend_internal(ctx context.Context, jobDir string, rec JobRecord) error {
	err := r.sendReportAndClean_internal(ctx, jobDir, rec)
	r.resendErr_internal(ctx, jobDir, rec, err)
	return err
}

// resendErr_internal handles a send's error: a down store waits for the
// next pass; a refusal is said (a WORKER WARNING and the step log) and
// counted on the record, and the record refused the same way RefusalLimit
// times is set aside (quarantine_internal).
func (r *Runner) resendErr_internal(ctx context.Context, jobDir string, rec JobRecord, err error) {
	if err == nil || errors.Is(err, ErrStoreDown) || ctx.Err() != nil {
		return
	}
	r.logStep_internal(rec.Row, rec.Consumer, "recover", "refused: "+err.Error(), 0)
	r.report(fmt.Sprintf("report of %s@%d refused", rec.Consumer, rec.Gen), err)
	if cur, rerr := ReadJobRecord(jobDir); rerr == nil {
		rec = cur
	}
	why := edge.RedactN(err.Error(), edge.FailureMax)
	if rec.LastRefusal == why {
		rec.Refusals++
	} else {
		rec.Refusals, rec.LastRefusal = 1, why
	}
	if rec.Refusals < cmp.Or(r.RefusalLimit, RefusalLimit) {
		r.report("job record "+rec.Consumer, WriteJobRecord(jobDir, rec))
		return
	}
	r.quarantine_internal(jobDir, rec)
}

// quarantine_internal sets a record the sprint keeps refusing aside, in
// <root>/quarantine/<consumer>@<gen>, and raises it: a WORKER WARNING on
// the worker's output and its step log, with the refusal and where the
// record is. The pass goes on; the card is not resumed or released while
// its record is there, so a reader decides.
func (r *Runner) quarantine_internal(jobDir string, rec JobRecord) {
	r.report("job record "+rec.Consumer, WriteJobRecord(jobDir, rec))
	r.setAside_internal(jobDir, rec, fmt.Sprintf("its report was refused %d times the same way (%s)", rec.Refusals, rec.LastRefusal))
}

// quarantineChanged_internal sets aside a job directory whose record or
// receipt is not the worker's kept copy, or that has none (a job from
// before kept copies, one the harness's leftovers changed, or a crash
// between the two writes): its tracked harness is killed, then it goes the
// way of quarantine_internal, so a reader decides. Its pid, nonce and gen are the kept copy's when there is
// one; else the directory's own, used only to kill (Procs.Kill kills only
// a process carrying the nonce) and to name the quarantine.
func (r *Runner) quarantineChanged_internal(jobDir, consumer string, cause error) {
	var rec JobRecord
	data, err := os.ReadFile(filepath.Join(r.RootDir, RecordsDir, consumer, "job.json"))
	if err != nil {
		data, err = os.ReadFile(filepath.Join(jobDir, "job.json"))
	}
	if err == nil {
		r.report("the record of "+consumer+" set aside", json.Unmarshal(data, &rec))
	} else {
		r.report("harness not stopped for "+jobDir, fmt.Errorf("neither the kept job.json nor the job directory's could be read, so its pid and nonce are unknown: %w", err))
	}
	rec.Consumer = consumer
	if rec.PID > 0 {
		r.killTracked_internal("stopping the harness of "+consumer+", set aside", rec.PID, rec.Nonce)
	}
	r.setAside_internal(jobDir, rec, cause.Error())
}

// setAside_internal moves jobDir to <root>/quarantine/<consumer>@<gen>,
// removes the worker's own state for it (removeJobState_internal) and
// raises why.
func (r *Runner) setAside_internal(jobDir string, rec JobRecord, why string) {
	r.endJobTree_internal(jobDir, rec, "quarantine")
	dst := r.quarantineDir_internal(rec.Consumer, rec.Gen)
	if fileExists_internal(dst) {
		dst += fmt.Sprintf("-%d", r.Now().Unix())
	}
	err := os.MkdirAll(filepath.Dir(dst), 0o755)
	if err == nil {
		err = os.Rename(jobDir, dst)
	}
	if err != nil {
		r.report("quarantine of "+rec.Consumer, fmt.Errorf("setting the job directory aside failed, it stays in %s: %w", jobDir, err))
		return
	}
	r.removeJobState_internal(rec.Consumer)
	r.logStep_internal(rec.Row, rec.Consumer, "quarantine", dst, 0)
	r.report("quarantine", fmt.Errorf("%s@%d: %s; the job directory is set aside in %s for a reader: read it, then nova-sprint show %s",
		rec.Consumer, rec.Gen, why, dst, rec.Consumer))
}

// removeJobState_internal removes what the worker keeps for consumer
// outside its job directory: its kept record (RecordsDir) and its clone
// (SealedDir).
func (r *Runner) removeJobState_internal(consumer string) {
	r.report("removing the kept record of "+consumer, os.RemoveAll(filepath.Join(r.RootDir, RecordsDir, consumer)))
	r.report("removing the worker's clone of "+consumer, os.RemoveAll(r.clonePath_internal(consumer)))
}

func (r *Runner) quarantineDir_internal(consumer string, gen int) string {
	return filepath.Join(r.RootDir, "quarantine", fmt.Sprintf("%s@%d", consumer, gen))
}

// quarantined_internal: w's record at this gen is set aside.
func (r *Runner) quarantined_internal(w ConsumerSummary) bool {
	return fileExists_internal(r.quarantineDir_internal(w.ID, w.Gen))
}

// writeReceipt_internal keeps the take receipt in the job directory and in
// the worker's copy outside it (RecordsDir).
func writeReceipt_internal(jobDir string, info TakeInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("the take receipt: %w", err)
	}
	if err := writeKept_internal(jobDir, ReceiptName, append(data, '\n')); err != nil {
		return fmt.Errorf("keep the take receipt: %w", err)
	}
	return nil
}

// readReceipt_internal is the take receipt the worker kept for a job
// directory, refused when the directory's copy differs.
func readReceipt_internal(jobDir string) (TakeInfo, error) {
	var info TakeInfo
	data, err := readKept_internal(jobDir, ReceiptName)
	if err != nil {
		return info, fmt.Errorf("no take receipt in %s: %w", filepath.Base(jobDir), err)
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return info, fmt.Errorf("the take receipt in %s: %s", filepath.Base(jobDir), edge.ParseFailure(err, data))
	}
	return info, nil
}
