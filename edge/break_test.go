package edge

// Breaker findings (Opus breaker probe, 1794013063891-0) against OSExec (functional: real processes, POSIX sh).
// Each test fails on 6bb85564 and names the guarantee it breaks.

import (
	"context"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestBreakExecTimeoutLeavesGrandchildRunning
//
// Sequence: Exec `sh -c 'sleep 5 & echo $!; sleep 5'` with a 300 ms
// timeout. The timeout fires, Exec returns ErrTimeout, and the backgrounded
// sleep it started is still alive afterwards.
//
// Broken: LAYER-4-EDGES.md Built, "TerminateGrace 3s ... Process-group
// SIGTERM -> SIGKILL grace" and "mandatory policy timeouts": OSExec uses
// exec.CommandContext with no process group (no Setpgid, no group kill), so
// only the direct child is killed and every descendant outlives the edge.
func TestBreakExecTimeoutLeavesGrandchildRunning(t *testing.T) {
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

// TestBreakExecSuccessWithBackgroundChildIsStartFailure
//
// Sequence: Exec `sh -c '(sleep 2 &); exit 0'`: the program exits 0 at once
// but a descendant keeps its stdout open. After WaitDelay (200 ms) Go's Wait
// returns exec.ErrWaitDelay, which classifyExecErr_internal does not
// recognise, so a successful command is reported as ErrStart
// ("start_failed: verify working directory and permissions"), ExitCode -1.
// Any git or gh call that leaves a helper behind (credential cache daemon,
// ssh ControlPersist master, fsmonitor) is misreported the same way.
//
// Broken: Exec's contract that ExitCode is the program's exit status and that
// ErrStart means the program could not be started (exec.go
// classifyExecErr_internal).
func TestBreakExecSuccessWithBackgroundChildIsStartFailure(t *testing.T) {
	res, err := NewOSExec().Run(context.Background(), Cmd{
		Argv:    []string{"sh", "-c", "(sleep 2 &); exit 0"},
		Timeout: 10 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("program exited 0, Exec reported exit=%d err=%v", res.ExitCode, err)
	}
}
