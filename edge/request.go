package edge

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"
)

// RequestID names an edge fact by (epoch, kind, card, sha). The kinds are
// layer 3's (steps.EdgeRequest.Kind: "land", "stop"); edge adds none.
func RequestID(epoch int64, kind, card, sha string) string {
	sum := sha256.Sum256([]byte(strconv.FormatInt(epoch, 10) + "\x00" + kind + "\x00" + card + "\x00" + sha))
	return "req-" + hex.EncodeToString(sum[:12])
}

type (
	EdgeRequest struct {
		Epoch                                                       int64
		ID, Kind, Card, Row, Stream, Repo, Base, Mode, SHA, BaseSHA string
		Forge                                                       string
		Ledgers, Heads, After                                       []string
	}
	EdgeResult struct {
		Epoch                                                                     int64
		ID, Kind, Card, SHA, Outcome, Failure, BaseTip, StreamTip, Blamed, Reason string
		Landed, Files                                                             []string
		PRNumber                                                                  int
	}
	inflightCall_internal struct {
		done chan struct{}
		res  EdgeResult
		err  error
	}
	Deduplicator struct {
		mu       sync.Mutex
		inflight map[string]*inflightCall_internal
	}
	FakeOutcome struct {
		ExitCode         int
		Stdout, Stderr   string
		Missing, Timeout bool
		Duration         time.Duration
		Err              error
	}
	FakeCall struct {
		PID int
		At  int64
		Cmd Cmd
	}
	FakeHandler func(call FakeCall) FakeOutcome
	FakeExec    struct {
		mu       sync.Mutex
		clock    int64
		nextPID  int
		calls    []FakeCall
		missing  map[string]bool
		queue    map[string][]FakeOutcome
		handlers map[string]FakeHandler
		fallback FakeHandler
	}
)

func (r EdgeRequest) Key() string { return cmp.Or(r.ID, RequestID(r.Epoch, r.Kind, r.Card, r.SHA)) }
func NewDeduplicator() *Deduplicator {
	return &Deduplicator{inflight: make(map[string]*inflightCall_internal)}
}
func (d *Deduplicator) Do(ctx context.Context, req EdgeRequest, fn func(context.Context, EdgeRequest) (EdgeResult, error)) (EdgeResult, error) {
	key := req.Key()
	req.ID = key
	d.mu.Lock()
	if call, ok := d.inflight[key]; ok {
		d.mu.Unlock()
		select {
		case <-call.done:
			return call.res, call.err
		case <-ctx.Done():
			return EdgeResult{Epoch: req.Epoch, ID: key, Kind: req.Kind, Card: req.Card, SHA: req.SHA}, ctx.Err()
		}
	}
	call := &inflightCall_internal{done: make(chan struct{})}
	d.inflight[key] = call
	d.mu.Unlock()
	res, err := fn(ctx, req)
	res.Epoch, res.ID, res.Kind = cmp.Or(res.Epoch, req.Epoch), cmp.Or(res.ID, key), cmp.Or(res.Kind, req.Kind)
	res.Card, res.SHA = cmp.Or(res.Card, req.Card), cmp.Or(res.SHA, req.SHA)
	d.mu.Lock()
	call.res, call.err = res, err
	delete(d.inflight, key)
	close(call.done)
	d.mu.Unlock()
	return res, err
}
func NewFakeExec() *FakeExec {
	return &FakeExec{clock: FakeClockStart, nextPID: 4000, missing: map[string]bool{}, queue: map[string][]FakeOutcome{}, handlers: map[string]FakeHandler{}}
}
func (f *FakeExec) Now() int64 { f.mu.Lock(); defer f.mu.Unlock(); return f.clock }
func (f *FakeExec) Advance(ticks int64) {
	f.mu.Lock()
	f.clock += max(ticks, 0)
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
	out := slices.Clone(f.calls)
	for i := range out {
		out[i].Cmd.Argv, out[i].Cmd.Env = slices.Clone(out[i].Cmd.Argv), slices.Clone(out[i].Cmd.Env)
	}
	return out
}
func (f *FakeExec) Run(ctx context.Context, cmd Cmd) (Result, error) {
	if err := ValidateCmd(cmd); err != nil {
		return Result{}, err
	}
	f.mu.Lock()
	if f.nextPID++; f.nextPID == os.Getpid() {
		f.nextPID++
	}
	f.clock++
	call := FakeCall{PID: f.nextPID, At: f.clock, Cmd: Cmd{Argv: slices.Clone(cmd.Argv), Dir: cmd.Dir, Env: slices.Clone(cmd.Env), CleanEnv: cmd.CleanEnv, Timeout: cmd.Timeout, MaxBytes: cmd.MaxBytes}}
	f.calls = append(f.calls, call)
	base := filepath.Base(cmd.Argv[0])
	out, ok := f.pickOutcome_internal(base, call)
	f.mu.Unlock()
	if !ok {
		return Result{ExitCode: -1}, &ExecError{Program: base, Kind: ErrMissingBinary, Reason: fmt.Sprintf("binary %q not found in fake process table", cmd.Argv[0]), Fix: binaryFix_internal(base)}
	}
	if cmd.OnStart != nil {
		cmd.OnStart(call.PID)
	}
	return f.finishOutcome_internal(ctx, cmd, base, call.PID, out)
}
func (f *FakeExec) pickOutcome_internal(base string, call FakeCall) (FakeOutcome, bool) {
	switch {
	case f.missing[base]:
		return FakeOutcome{}, false
	case len(f.queue[base]) > 0:
		out := f.queue[base][0]
		f.queue[base] = f.queue[base][1:]
		return out, true
	case f.handlers[base] != nil:
		return f.handlers[base](call), true
	case f.fallback != nil:
		return f.fallback(call), true
	default:
		return FakeOutcome{}, false
	}
}
func (f *FakeExec) finishOutcome_internal(ctx context.Context, cmd Cmd, base string, pid int, out FakeOutcome) (Result, error) {
	maxBytes, timeout := cmp.Or(cmd.MaxBytes, DefaultMaxBytes), cmp.Or(cmd.Timeout, DefaultTimeout)
	stdoutBuf, stderrBuf := newCappedBuffer_internal(maxBytes), newCappedBuffer_internal(maxBytes)
	_, _ = stdoutBuf.Write([]byte(out.Stdout))
	_, _ = stderrBuf.Write([]byte(out.Stderr))
	res := Result{PID: pid, ExitCode: out.ExitCode, Stdout: stdoutBuf.String(), Stderr: stderrBuf.String(), Truncated: stdoutBuf.truncated || stderrBuf.truncated, Duration: out.Duration}
	switch {
	case out.Missing:
		res.ExitCode = -1
		return res, &ExecError{Program: base, Kind: ErrMissingBinary, Reason: fmt.Sprintf("binary %q not found", cmd.Argv[0]), Fix: binaryFix_internal(base)}
	case out.Timeout || errors.Is(ctx.Err(), context.DeadlineExceeded):
		res.ExitCode = -1
		return res, &ExecError{Program: base, Kind: ErrTimeout, Reason: fmt.Sprintf("timed out after %s", timeout), Fix: fmt.Sprintf("check %s responsiveness or raise the policy timeout", base)}
	case out.Err != nil:
		res.ExitCode = -1
		return res, out.Err
	default:
		return res, nil
	}
}
