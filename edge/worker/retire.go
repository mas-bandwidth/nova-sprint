package worker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// A branch belongs to the consumer that holds the card now (defect 78). A
// withdrawn bench job once pushed its dirty head to the card's branch with
// a plain force on every recovery pass, because its directory was never
// removed (a dirty tree was kept, and the Go module cache inside it is
// read-only), and so rewound the new holder's branch again and again. Now:
// every push goes through pushBranch_internal, which refuses a job that no
// longer holds its consumer and moves the branch only by lease from the head
// the job last pushed; and a job whose consumer is final, or whose
// generation is stale, has its directory retired on the first pass that
// sees it, pushing nothing. Model: tla/Runner.tla (Retire, HolderPush,
// NoPushAfterFinal, BranchOnlyByHolder).

// RetiredDir is where a retired job's record is kept, outside its removed
// directory: <root>/retired/e<epoch>/<consumer>@<gen>/job.json. It is the receipt
// that the job was retired, so a removal that failed is never tried again.
const RetiredDir = "retired"

// pushBranch_internal is every push the worker makes: rec's branch moves to
// sha only while rec's job holds its consumer (holds_internal), and only by
// lease from leaseFrom_internal. A push that lands is rec.PushedHead, which
// the caller writes to the job record.
func (r *Runner) pushBranch_internal(ctx context.Context, wt string, rec *JobRecord, sha string) error {
	if !r.holds_internal(*rec) {
		return fmt.Errorf("push of %s to %s refused: %s@%d no longer holds its card (its consumer is final, or the row holds a newer generation), and the branch belongs to the card's holder", sha, rec.Branch, rec.Consumer, rec.Gen)
	}
	remote, err := r.jobRemote_internal(*rec)
	if err != nil {
		return fmt.Errorf("push of %s to %s refused: %w", sha, rec.Branch, err)
	}
	if err := r.Git.Push(ctx, wt, remote, rec.Branch, sha, leaseFrom_internal(*rec)); err != nil {
		return err
	}
	rec.PushedHead = sha
	return nil
}

// leaseFrom_internal is where a job's push expects the branch to be: the
// head this job last pushed; before its first push, the tip it found at
// stage ("" : absent); for a record from before StartTip, its prior head.
func leaseFrom_internal(rec JobRecord) string {
	switch {
	case rec.PushedHead != "":
		return rec.PushedHead
	case rec.StartTipRead:
		return rec.StartTip
	}
	return rec.PriorHead
}

// holds_internal: rec's job still holds its consumer as far as this worker
// has read the row: the consumer is not final, and no newer generation of
// it is in the row's hand.
func (r *Runner) holds_internal(rec JobRecord) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return rec.Gen >= r.gone[goneKey_internal(rec.Epoch, rec.Consumer)]
}

// markGone_internal records that consumer's jobs of the epoch below gen
// hold no branch (math.MaxInt: none of them, the consumer is final). A
// cleared sprint's next epoch reuses consumer ids, so the epoch is the key's.
func (r *Runner) markGone_internal(epoch int, consumer string, gen int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := goneKey_internal(epoch, consumer)
	r.gone[k] = max(r.gone[k], gen)
}

func goneKey_internal(epoch int, consumer string) string {
	return fmt.Sprintf("%d/%s", epoch, consumer)
}

// retiredDir_internal is rec's retired record's directory:
// <root>/retired/e<epoch>/<consumer>@<gen>.
func (r *Runner) retiredDir_internal(rec JobRecord) string {
	return filepath.Join(r.RootDir, RetiredDir, fmt.Sprintf("e%d", rec.Epoch), fmt.Sprintf("%s@%d", rec.Consumer, rec.Gen))
}

// retireJob_internal removes the directory of a job that holds its card no
// more, once, and pushes nothing. Its record goes first to RetiredDir; the
// directory is then made writable and removed. A removal that fails is
// raised (stderr and the step log) and the directory moved to
// <root>/quarantine; with the record in RetiredDir it is not tried again.
func (r *Runner) retireJob_internal(jobDir string, rec JobRecord, why string) {
	keep := r.retiredDir_internal(rec)
	if fileExists_internal(keep) {
		return // retired before: a removal that failed was raised then
	}
	r.markGone_internal(rec.Epoch, rec.Consumer, rec.Gen+1)
	rec.State = JobCleaned
	r.report("the retired record of "+rec.Consumer+" (its removal may be tried again next pass)", WriteJobRecord(keep, rec))
	if err := r.removeDir_internal(jobDir); err != nil {
		r.retireFailed_internal(jobDir, rec, why, err)
		return
	}
	r.logStep_internal(rec.Row, rec.Consumer, "retire", "removed, nothing pushed: "+why, 0)
}

// retireFailed_internal raises a job directory that could not be removed
// and moves it to <root>/quarantine, out of the jobs the passes read.
func (r *Runner) retireFailed_internal(jobDir string, rec JobRecord, why string, cause error) {
	dst := r.quarantineDir_internal(rec.Consumer, rec.Gen)
	if fileExists_internal(dst) {
		dst += fmt.Sprintf("-%d", r.Now().Unix())
	}
	where := dst
	err := os.MkdirAll(filepath.Dir(dst), 0o755)
	if err == nil {
		err = os.Rename(jobDir, dst)
	}
	if err != nil {
		where, cause = jobDir, fmt.Errorf("%w; moving it to quarantine: %w", cause, err)
	}
	msg := fmt.Errorf("%s@%d (%s): its job directory could not be removed (%w); it is left in %s and not tried again: remove it by hand with chmod -R u+w %s, then rm -rf %s",
		rec.Consumer, rec.Gen, why, cause, where, where, where)
	r.report("retire", msg)
	r.logStep_internal(rec.Row, rec.Consumer, "retire", "removal failed: "+msg.Error(), 0)
}

func (r *Runner) removeDir_internal(dir string) error {
	if r.RemoveDir != nil {
		return r.RemoveDir(dir)
	}
	return removeTree_internal(dir)
}

// removeTree_internal removes dir and everything in it, first giving the
// owner write on every directory under it (a Go module cache is read-only,
// and a plain remove fails on it). Symbolic links are never followed.
func removeTree_internal(dir string) error {
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	// Every entry is visited whatever fails (the callback returns nil); the
	// first failure goes with the removal's error.
	var walkErr error
	walkErr = cmp.Or(filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			err = os.Chmod(p, 0o700)
		}
		walkErr = cmp.Or(walkErr, err)
		return nil
	}), walkErr)
	if err := os.RemoveAll(dir); err != nil {
		if walkErr != nil {
			return fmt.Errorf("%w (making it writable: %w)", err, walkErr)
		}
		return err
	}
	return nil
}

// sameEpoch_internal: rec is of the epoch next reads (or either is unknown).
func sameEpoch_internal(next NextResponse, rec JobRecord) bool {
	return rec.Epoch == 0 || next.Epoch == 0 || rec.Epoch == next.Epoch
}
