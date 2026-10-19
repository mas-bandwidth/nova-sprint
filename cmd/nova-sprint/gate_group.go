package main

// gate_group.go gives a land gate's child its own process group, so a cancelled gate
// (shutdown, a deadline, a caller's cancel) reaches every descendant, not just the
// child it started. subproc.Prepare sets WaitDelay but no process group.

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// gateKillGrace is how long a gate's process group gets after SIGTERM before the
// whole group is forced with SIGKILL.
const gateKillGrace = 5 * time.Second

// gateGroupSignal is the group signal call: a negative pid names the group. Tests
// replace it to see the sequence without a process.
var gateGroupSignal = syscall.Kill

// gateAfter waits d; tests replace it with a fake clock.
var gateAfter = time.After

// gateGroupCmd puts cmd in a process group of its own and makes its cancellation end
// that group: SIGTERM to every member now, then SIGKILL after gateKillGrace when any
// member is left. WaitDelay stays as subproc set it.
func gateGroupCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		_, err := gateGroupEnd(cmd.Process.Pid, gateAfter)
		return err
	}
}

// gateGroupEnd signals the group pgid: SIGTERM at once, then SIGKILL once after
// gateKillGrace says the group is still there. A group already gone (ESRCH) is not an
// error, and one gone before the grace takes no forced signal. done, when not nil,
// closes once the escalation has finished; the caller may ignore it.
func gateGroupEnd(pgid int, after func(time.Duration) <-chan time.Time) (<-chan struct{}, error) {
	if err := gateGroupSignal(-pgid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil, nil
		}
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-after(gateKillGrace)
		if err := gateGroupSignal(-pgid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		_ = gateGroupSignal(-pgid, syscall.SIGKILL)
	}()
	return done, nil
}
