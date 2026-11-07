package edge

// OSExec against real processes (functional: POSIX sh); each test names the
// guarantee it checks.

import (
	"context"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestExecTimeoutLeavesGrandchildRunning: Exec of
// `sh -c 'sleep 5 & echo $!; sleep 5'` with a 300 ms timeout returns
// ErrTimeout and the backgrounded sleep it started is dead afterwards. A
// timeout ends the whole process group (SIGTERM, then SIGKILL after the
// grace), not only the direct child.
func TestFunctionalExecTimeoutLeavesGrandchildRunning(t *testing.T) {
	t.Parallel()
	res, err := NewOSExec().Run(context.Background(), Cmd{
		Argv:    []string{"sh", "-c", "sleep 5 & echo $!; sleep 5"},
		Timeout: 300 * time.Millisecond,
	})
	var ee *ExecError
	if err == nil {
		t.Fatalf("expected a timeout, got %+v", res)
	}
	if e, ok := err.(*ExecError); ok {
		ee = e
	}
	if ee == nil || ee.Kind != ErrTimeout {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
	pid, perr := strconv.Atoi(strings.TrimSpace(res.Stdout))
	if perr != nil || pid <= 0 {
		t.Fatalf("no grandchild pid in stdout %q", res.Stdout)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	time.Sleep(100 * time.Millisecond)
	if syscall.Kill(pid, 0) == nil {
		t.Fatalf("grandchild pid %d still alive after Exec timed out: the timeout kills only the direct child, not its process group", pid)
	}
}

// TestExecSuccessWithBackgroundChildIsStartFailure: Exec of
// `sh -c '(sleep 2 &); exit 0'`: the program exits 0 at once but a
// descendant keeps its stdout open. After WaitDelay (200 ms) Go's Wait
// returns exec.ErrWaitDelay; a successful command must not be reported as
// ErrStart. Any git or gh call that leaves a helper behind (credential cache
// daemon, ssh ControlPersist master, fsmonitor) is the same case.
//
// Contract: Exec's ExitCode is the program's exit status and that
// ErrStart means the program could not be started (exec.go
// classifyExecErr_internal).
func TestExecSuccessWithBackgroundChildIsStartFailure(t *testing.T) {
	t.Parallel()
	res, err := NewOSExec().Run(context.Background(), Cmd{
		Argv:    []string{"sh", "-c", "(sleep 2 &); exit 0"},
		Timeout: 10 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("program exited 0, Exec reported exit=%d err=%v", res.ExitCode, err)
	}
}
