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
	"sync"
	"syscall"
	"time"
)

const (
	DefaultTimeout   = 30 * time.Second
	DefaultMaxBytes  = 64 * 1024
	FakeClockStart   = int64(1000)
	ErrInvalidArgv   = "invalid_argv"
	ErrMissingBinary = "missing_binary"
	ErrTimeout       = "timeout"
	ErrStart         = "start_failed"
)

type Cmd struct {
	Argv     []string
	Dir      string
	Env      []string
	CleanEnv bool
	Timeout  time.Duration
	MaxBytes int
}
type Result struct {
	ExitCode       int
	Stdout, Stderr string
	Truncated      bool
	Duration       time.Duration
}

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
		return "install GitHub CLI (gh) on PATH and authenticate with GH_TOKEN or GH_CONFIG_DIR"
	case prog == "git":
		return "install git on PATH or run: nova-fleet check"
	default:
		return fmt.Sprintf("install %s on PATH or check PATH configuration", prog)
	}
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
	c.Stdin, c.Dir, c.WaitDelay = nil, cmd.Dir, 200*time.Millisecond
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process != nil {
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
			return c.Process.Kill()
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
	err := c.Run()
	res := Result{Stdout: outBuf.String(), Stderr: errBuf.String(), Truncated: outBuf.truncated || errBuf.truncated, Duration: time.Since(start)}
	if err == nil || (runCtx.Err() == nil && errors.Is(err, exec.ErrWaitDelay)) {
		return res, nil
	}
	return classifyExecErr_internal(cmd.Argv[0], timeout, runCtx.Err(), err, res)
}
func classifyExecErr_internal(prog string, timeout time.Duration, ctxErr, runErr error, res Result) (Result, error) {
	base := filepath.Base(prog)
	if errors.Is(ctxErr, context.DeadlineExceeded) || errors.Is(ctxErr, context.Canceled) {
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

type FakeOutcome struct {
	ExitCode         int
	Stdout, Stderr   string
	Missing, Timeout bool
	Duration         time.Duration
	Err              error
}
type FakeCall struct {
	PID int
	At  int64
	Cmd Cmd
}
type FakeHandler func(call FakeCall) FakeOutcome
type FakeExec struct {
	mu       sync.Mutex
	clock    int64
	nextPID  int
	calls    []FakeCall
	missing  map[string]bool
	queue    map[string][]FakeOutcome
	handlers map[string]FakeHandler
	fallback FakeHandler
}

func NewFakeExec() *FakeExec {
	return &FakeExec{clock: FakeClockStart, nextPID: 100, missing: make(map[string]bool), queue: make(map[string][]FakeOutcome), handlers: make(map[string]FakeHandler)}
}
func (f *FakeExec) Now() int64 { f.mu.Lock(); defer f.mu.Unlock(); return f.clock }
func (f *FakeExec) Advance(ticks int64) {
	f.mu.Lock()
	if ticks > 0 {
		f.clock += ticks
	}
	f.mu.Unlock()
}
func (f *FakeExec) SetMissing(prog string, missing bool) {
	f.mu.Lock()
	f.missing[filepath.Base(prog)] = missing
	f.mu.Unlock()
}
func (f *FakeExec) Enqueue(prog string, outcomes ...FakeOutcome) {
	f.mu.Lock()
	key := filepath.Base(prog)
	f.queue[key] = append(f.queue[key], outcomes...)
	f.mu.Unlock()
}
func (f *FakeExec) On(prog string, h FakeHandler) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if prog == "*" {
		f.fallback = h
	} else {
		f.handlers[filepath.Base(prog)] = h
	}
}
func (f *FakeExec) Calls() []FakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FakeCall, len(f.calls))
	for i, c := range f.calls {
		c.Cmd.Argv, c.Cmd.Env = slices.Clone(c.Cmd.Argv), slices.Clone(c.Cmd.Env)
		out[i] = c
	}
	return out
}
func (f *FakeExec) Run(ctx context.Context, cmd Cmd) (Result, error) {
	if err := ValidateCmd(cmd); err != nil {
		return Result{}, err
	}
	f.mu.Lock()
	f.nextPID++
	f.clock++
	call := FakeCall{
		PID: f.nextPID, At: f.clock,
		Cmd: Cmd{Argv: slices.Clone(cmd.Argv), Dir: cmd.Dir, Env: slices.Clone(cmd.Env), CleanEnv: cmd.CleanEnv, Timeout: cmd.Timeout, MaxBytes: cmd.MaxBytes},
	}
	f.calls = append(f.calls, call)
	base := filepath.Base(cmd.Argv[0])
	out, ok := f.pickOutcome_internal(base, call)
	f.mu.Unlock()
	if !ok {
		return Result{ExitCode: -1}, &ExecError{Program: base, Kind: ErrMissingBinary, Reason: fmt.Sprintf("binary %q not found in fake process table", cmd.Argv[0]), Fix: binaryFix_internal(base)}
	}
	return f.finishOutcome_internal(ctx, cmd, base, out)
}
func (f *FakeExec) pickOutcome_internal(base string, call FakeCall) (FakeOutcome, bool) {
	if f.missing[base] {
		return FakeOutcome{}, false
	}
	if q := f.queue[base]; len(q) > 0 {
		f.queue[base] = q[1:]
		return q[0], true
	}
	if h, ok := f.handlers[base]; ok {
		return h(call), true
	}
	if f.fallback != nil {
		return f.fallback(call), true
	}
	return FakeOutcome{}, false
}
func (f *FakeExec) finishOutcome_internal(ctx context.Context, cmd Cmd, base string, out FakeOutcome) (Result, error) {
	maxBytes, timeout := cmp.Or(cmd.MaxBytes, DefaultMaxBytes), cmp.Or(cmd.Timeout, DefaultTimeout)
	stdoutBuf, stderrBuf := newCappedBuffer_internal(maxBytes), newCappedBuffer_internal(maxBytes)
	_, _ = stdoutBuf.Write([]byte(out.Stdout))
	_, _ = stderrBuf.Write([]byte(out.Stderr))
	res := Result{ExitCode: out.ExitCode, Stdout: stdoutBuf.String(), Stderr: stderrBuf.String(), Truncated: stdoutBuf.truncated || stderrBuf.truncated, Duration: out.Duration}
	if out.Missing {
		res.ExitCode = -1
		return res, &ExecError{Program: base, Kind: ErrMissingBinary, Reason: fmt.Sprintf("binary %q not found", cmd.Argv[0]), Fix: binaryFix_internal(base)}
	}
	if out.Timeout || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.ExitCode = -1
		return res, &ExecError{Program: base, Kind: ErrTimeout, Reason: fmt.Sprintf("timed out after %s", timeout), Fix: fmt.Sprintf("check %s responsiveness or raise the policy timeout", base)}
	}
	if out.Err != nil {
		res.ExitCode = -1
		return res, out.Err
	}
	return res, nil
}
