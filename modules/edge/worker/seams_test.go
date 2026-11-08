package worker

import (
	"cmp"
	"context"
	"errors"
)

// Test seams: one-call drivers of the runner's internals, which no
// product code calls (the binary runs ServeRow and StepRow).

// RunConsumer runs one dealt card as a pass would, and waits for it.
func (r *Runner) RunConsumer(ctx context.Context, rowInfo NextResponse, c ConsumerSummary) error {
	l, ok := r.claim_internal(ctx, c, nil)
	if !ok {
		return nil
	}
	return r.runJobs_internal(ctx, rowInfo, []launch_internal{l})[0].err
}

// RecoverRow is one recovery of the row's jobs, run to the end: what is
// owed is sent, finished cards' directories are cleaned, a live harness is
// adopted, and every job a stopped runner left is resumed (and waited for).
func (r *Runner) RecoverRow(ctx context.Context, row string) error {
	resumes, unsent, err := r.recoverRow_internal(ctx, row)
	if err != nil || len(resumes) == 0 {
		return cmp.Or(err, unsent)
	}
	next, err := r.Sprint.Next(ctx, row)
	if err != nil {
		return err
	}
	var launches []launch_internal
	for _, l := range resumes {
		if l, ok := r.claim_internal(ctx, l.c, l.resume); ok {
			launches = append(launches, l)
		}
	}
	return errors.Join(unsent, r.runBatch_internal(ctx, next, launches))
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

// HasHead: the branch of the repository at remote (a clone URL or path) is
// at sha.
func (g *OSGitOps) HasHead(ctx context.Context, remote, branch, sha string) (bool, error) {
	return g.hasHead_internal(ctx, "", remote, branch, sha)
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
