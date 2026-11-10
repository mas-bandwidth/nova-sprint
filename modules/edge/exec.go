// Package edge is the foundation of layer 4 of nova-sprint v2: the one way an edge runs an outside program (Exec).
package edge

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

const (
	DefaultTimeout, DefaultMaxBytes  = 30 * time.Second, 64 * 1024
	FakeClockStart                   = int64(1000)
	ErrInvalidArgv, ErrMissingBinary = "invalid_argv", "missing_binary"
	ErrTimeout, ErrStart             = "timeout", "start_failed"
	// ErrCanceled is a run whose caller's context was cancelled before it
	// ended: the caller stopped it (a worker shutting down), never its own
	// timeout (defect 73: a stopped job was reported "timed out after 1h30m"
	// after 70 s).
	ErrCanceled = "canceled"
)

type (
	Cmd struct {
		Argv     []string
		Dir      string
		Env      []string
		CleanEnv bool
		Timeout  time.Duration
		MaxBytes int
		OnStart  func(pid int)
	}
	Result struct {
		PID, ExitCode  int
		Stdout, Stderr string
		Truncated      bool
		Duration       time.Duration
	}
)

func (r Result) OK() bool { return r.ExitCode == 0 }

type ExecError struct {
	Program, Kind, Reason, Fix string
	Cause                      error
}

func (e *ExecError) Error() string {
	return fmt.Sprintf("exec %s (%s): %s: %s", e.Program, e.Kind, e.Reason, e.Fix)
}
func (e *ExecError) Unwrap() error { return e.Cause }

type Exec interface {
	Run(ctx context.Context, cmd Cmd) (Result, error)
}

func RunArgv(ctx context.Context, ex Exec, timeout time.Duration, dir string, env []string, argv ...string) (Result, error) {
	return ex.Run(ctx, Cmd{Argv: argv, Dir: dir, Env: env, Timeout: timeout})
}
func ValidateCmd(cmd Cmd) error {
	if len(cmd.Argv) == 0 || strings.TrimSpace(cmd.Argv[0]) == "" {
		return &ExecError{Program: "<empty>", Kind: ErrInvalidArgv, Reason: "argv[0] is empty", Fix: "pass a non-empty binary name in Cmd.Argv[0]"}
	}
	prog := filepath.Base(cmd.Argv[0])
	for _, a := range cmd.Argv {
		if strings.IndexByte(a, 0) >= 0 {
			return &ExecError{Program: prog, Kind: ErrInvalidArgv, Reason: "argv contains a NUL byte", Fix: "remove NUL bytes from command arguments"}
		}
	}
	if strings.IndexByte(cmd.Dir, 0) >= 0 {
		return &ExecError{Program: prog, Kind: ErrInvalidArgv, Reason: "working directory contains a NUL byte", Fix: "pass a valid directory path without NUL bytes"}
	}
	for _, kv := range cmd.Env {
		if strings.IndexByte(kv, 0) >= 0 {
			return &ExecError{Program: prog, Kind: ErrInvalidArgv, Reason: "environment entry contains a NUL byte", Fix: "remove NUL bytes from Cmd.Env entries"}
		}
	}
	return nil
}
func binaryFix_internal(prog string) string {
	switch {
	case strings.HasPrefix(prog, "nova-"):
		return fmt.Sprintf("install nova-tools on PATH or run: nova-fleet up (missing %s)", prog)
	case prog == "gh":
		return "install GitHub CLI (gh) on PATH and gh auth login on this host (the landing acts as the host's gh login)"
	case prog == "git":
		return "install git on PATH or run: nova-fleet check"
	default:
		return fmt.Sprintf("install %s on PATH or check PATH configuration", prog)
	}
}

// TerminateGrace is how long a stopped process group has, after SIGTERM,
// before SIGKILL (LAYER-4-EDGES.md). git removes its *.lock files on
// SIGTERM and leaves them on SIGKILL; a shallow.lock left that way in a
// work clone refused every later fetch.
const TerminateGrace, cancelPoll = 3 * time.Second, 50 * time.Millisecond

// stopGroup_internal ends process group pgid: SIGTERM, a probe every poll
// while any member is left, and SIGKILL once grace has passed. An error
// from SIGTERM (ESRCH: the group is gone) or from SIGKILL is its error.
func stopGroup_internal(pgid int, kill func(int, syscall.Signal) error, sleep func(time.Duration), grace, poll time.Duration) error {
	if err := kill(-pgid, syscall.SIGTERM); err != nil {
		return err
	}
	for waited := time.Duration(0); !errors.Is(kill(-pgid, 0), syscall.ESRCH); waited += poll {
		if waited >= grace {
			return kill(-pgid, syscall.SIGKILL)
		}
		sleep(poll)
	}
	return nil
}

type OSExec struct{}

func NewOSExec() *OSExec { return &OSExec{} }
func (o *OSExec) Run(ctx context.Context, cmd Cmd) (Result, error) {
	if err := ValidateCmd(cmd); err != nil {
		return Result{}, err
	}
	timeout, maxBytes := cmp.Or(cmd.Timeout, DefaultTimeout), cmp.Or(cmd.MaxBytes, DefaultMaxBytes)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	c := exec.CommandContext(runCtx, cmd.Argv[0], cmd.Argv[1:]...)
	c.Stdin, c.Dir, c.WaitDelay, c.SysProcAttr = nil, cmd.Dir, 200*time.Millisecond, &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process != nil {
			return errors.Join(stopGroup_internal(c.Process.Pid, syscall.Kill, time.Sleep, TerminateGrace, cancelPoll), c.Process.Kill())
		}
		return nil
	}
	if cmd.CleanEnv {
		c.Env = slices.Clone(cmd.Env)
	} else if len(cmd.Env) > 0 {
		c.Env = append(os.Environ(), cmd.Env...)
	}
	outBuf, errBuf := newCappedBuffer_internal(maxBytes), newCappedBuffer_internal(maxBytes)
	c.Stdout, c.Stderr = outBuf, errBuf
	start := time.Now()
	if err := c.Start(); err != nil {
		return classifyExecErr_internal(cmd.Argv[0], timeout, runCtx.Err(), err, Result{Duration: time.Since(start)})
	}
	pid := c.Process.Pid
	if cmd.OnStart != nil {
		cmd.OnStart(pid)
	}
	err := c.Wait()
	res := Result{PID: pid, Stdout: outBuf.String(), Stderr: errBuf.String(), Truncated: outBuf.truncated || errBuf.truncated, Duration: time.Since(start)}
	if err == nil || (runCtx.Err() == nil && errors.Is(err, exec.ErrWaitDelay)) {
		return res, nil
	}
	return classifyExecErr_internal(cmd.Argv[0], timeout, runCtx.Err(), err, res)
}
func classifyExecErr_internal(prog string, timeout time.Duration, ctxErr, runErr error, res Result) (Result, error) {
	base := filepath.Base(prog)
	if errors.Is(ctxErr, context.Canceled) {
		res.ExitCode = -1
		return res, &ExecError{Program: base, Kind: ErrCanceled, Reason: "stopped by its caller before it ended (not a timeout)", Fix: "nothing to fix in " + base + ": its caller stopped it", Cause: ctxErr}
	}
	if errors.Is(ctxErr, context.DeadlineExceeded) {
		res.ExitCode = -1
		return res, &ExecError{Program: base, Kind: ErrTimeout, Reason: fmt.Sprintf("timed out after %s", timeout), Fix: fmt.Sprintf("check %s responsiveness or raise the policy timeout", base), Cause: ctxErr}
	}
	if errors.Is(runErr, exec.ErrNotFound) || errors.Is(runErr, os.ErrNotExist) {
		res.ExitCode = -1
		return res, &ExecError{Program: base, Kind: ErrMissingBinary, Reason: fmt.Sprintf("binary %q not found", prog), Fix: binaryFix_internal(base), Cause: runErr}
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	res.ExitCode = -1
	return res, &ExecError{Program: base, Kind: ErrStart, Reason: runErr.Error(), Fix: fmt.Sprintf("verify working directory and permissions for %s", base), Cause: runErr}
}

type cappedBuffer_internal struct {
	max       int
	head      bytes.Buffer
	tail      []byte
	truncated bool
}

func newCappedBuffer_internal(max int) *cappedBuffer_internal {
	return &cappedBuffer_internal{max: max}
}
func (b *cappedBuffer_internal) Write(p []byte) (int, error) {
	total := len(p)
	if b.max <= 0 {
		return total, nil
	}
	if b.max < 64 {
		if rem := b.max - b.head.Len(); rem > 0 {
			if len(p) > rem {
				p, b.truncated = p[:rem], true
			}
			b.head.Write(p)
		} else if len(p) > 0 {
			b.truncated = true
		}
		return total, nil
	}
	const marker = "\n...[truncated]...\n"
	headMax := (b.max - len(marker)) / 2
	tailMax := b.max - len(marker) - headMax
	if !b.truncated && b.head.Len()+len(p) <= b.max {
		b.head.Write(p)
		return total, nil
	}
	if !b.truncated {
		b.truncated = true
		cur := append(b.head.Bytes(), p...)
		b.head.Reset()
		b.head.Write(cur[:headMax])
		p = cur[headMax:]
	}
	b.tail = append(b.tail, p...)
	if len(b.tail) > tailMax {
		b.tail = slices.Clone(b.tail[len(b.tail)-tailMax:])
	}
	return total, nil
}
func (b *cappedBuffer_internal) String() string {
	if !b.truncated || b.max < 64 {
		return b.head.String()
	}
	return b.head.String() + "\n...[truncated]...\n" + string(b.tail)
}
