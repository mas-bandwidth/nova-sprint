package policy

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// The evaluator (cold read 3's ruling 1): every version a process engine
// runs (a tick's, check's, the catalog's) runs in a child process, this
// binary as `<binary> policy-eval` (EvalArg), one per engine and reused
// across ticks, fed one request at a time on its stdin and answering on its
// stdout (gob). The child runs the in-process engine with the budget the
// parent leaves it, so a version over the deadline fails there with the
// question in hand named, as before. A run the child abandoned at its
// deadline (one builtin step that never yields) is still burning a core, so
// the child says so and the parent kills it; a child that has not answered
// by the deadline plus evalGrace is killed too, and every version it was
// running fails. The next request starts a new child. No goroutine or core
// is left running past a tick. The in-process path (newLocal) is the unit
// tests' and the child's own.

const (
	// EvalArg is the child's verb: layer 6's main calls ServeMain when its
	// first argument is EvalArg, as the tests' TestMain does.
	EvalArg = "policy-eval"
	// evalGrace is how long past a tick's deadline the parent waits for its
	// child's answer before it kills it: the gob round trip of a 5,000-card
	// snapshot and its answers (a guess, about ten times a measured 9 ms).
	evalGrace = 100 * time.Millisecond * raceSlowdown
)

// request is one call into the child: a tick (Versions), a check (Src) or a
// catalog (Src), on the snapshot and roster, with the parent's built-in and
// the budget left of the tick.
type request struct {
	Kind     string // tick, check or catalog
	Versions Versions
	Src      wireSource
	Snap     store.Snapshot
	Roster   roster.Roster
	Q        Questions
	Builtin  string
	Budget   time.Duration
}

type wireSource struct {
	Version     int
	Label, Body string
	Stored      bool
}

// reply is the child's answer. Live is true when a run it abandoned at the
// deadline is still running: the parent kills the child.
type reply struct {
	Answers  Answers
	Shown    map[string]string
	Failures []wireFailure
	Compiles int
	Live     bool
}

type wireFailure struct {
	Version                       int
	Label, Question, Subject, Msg string
	Line                          int
	EOF                           bool
}

// evaluator is a process engine's child, started on first use.
type evaluator struct {
	mu    sync.Mutex
	cmd   *exec.Cmd
	in    io.WriteCloser
	enc   *gob.Encoder
	dec   *gob.Decoder
	error *capped
}

// NewEngine is an engine that runs every version in its own child process
// (the evaluator), timed by clock: layer 6's, on RealClock. The child is
// stopped once the engine is unreachable.
func NewEngine(clock Clock) *Engine {
	e := newLocal(clock)
	e.proc = &evaluator{}
	runtime.AddCleanup(e, (*evaluator).stop, e.proc)
	return e
}

// remote is req answered by e's child, the deadline counted from start on
// e's clock; failing lists the sources failed when the child is killed.
func (e *Engine) remote(ctx context.Context, start time.Time, req request, failing []source) (Answers, []Failure) {
	req.Builtin, req.Budget, req.Snap = e.builtin, e.budget-e.clock.Now().Sub(start), unworded(req.Snap)
	if ctx.Err() != nil { // the caller's deadline has passed: every version tried fails
		return killed(failing, overTime()+" building the tick's inputs")
	}
	ev := e.proc
	ev.mu.Lock()
	defer ev.mu.Unlock()
	got := make(chan reply, 1)
	errc := make(chan error, 1)
	if err := ev.send(req, got, errc); err != nil {
		return killed(failing, err.Error())
	}
	select {
	case rep := <-got:
		e.mu.Lock()
		e.compiles += rep.Compiles
		e.mu.Unlock()
		if rep.Live {
			ev.stop()
		}
		return rep.answers()
	case err := <-errc:
		ev.stop()
		return killed(failing, "the policy evaluator failed: "+err.Error()+ev.error.first())
	case <-e.clock.After(req.Budget + evalGrace):
	case <-ctx.Done():
	}
	ev.stop()
	return killed(failing, overTime()+": the policy evaluator was stopped")
}

// sibling is an engine like e, with caches of its own: in its own child
// when e has one, else in this process.
func (e *Engine) sibling() *Engine {
	s := newLocal(e.clock)
	if e.proc != nil {
		s = NewEngine(e.clock)
	}
	s.builtin = e.builtin
	return s
}

func (s source) wire() wireSource { return wireSource{s.version, s.label, s.body, s.stored} }

// unworded is snap with every card's brief and prose dropped: no question
// reads them (rowan2's ruling 4 and 5), so what crosses to the child grows
// with the card count, not with the briefs.
func unworded(snap store.Snapshot) store.Snapshot {
	cards := make(map[string]store.Card, len(snap.Cards))
	for id, c := range snap.Cards {
		c.Brief, c.Prose = "", ""
		cards[id] = c
	}
	snap.Cards = cards
	return snap
}

// killed is the tick's answer when the child is gone: every source failed.
func killed(failing []source, msg string) (Answers, []Failure) {
	fs := make([]Failure, len(failing))
	for i, s := range failing {
		fs[i] = Failure{Version: s.version, Label: s.label, Line: 1, Msg: msg}
	}
	return Answers{Version: -1}, fs
}

// send starts the child if none runs, writes req and reads its reply on a
// goroutine of its own into got, or the error into errc.
func (ev *evaluator) send(req request, got chan<- reply, errc chan<- error) error {
	if ev.cmd == nil {
		if err := ev.startChild(); err != nil {
			return err
		}
	}
	dec := ev.dec
	go func() {
		var rep reply
		if err := dec.Decode(&rep); err != nil {
			errc <- err
			return
		}
		got <- rep
	}()
	return ev.enc.Encode(req)
}

func (ev *evaluator) startChild() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find the policy evaluator: %w", err)
	}
	cmd := exec.Command(exe, EvalArg)
	cmd.Env = append(os.Environ(), evalMemoryEnv+"="+strconv.Itoa(EvalMemory))
	ev.error = &capped{}
	cmd.Stderr = ev.error
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot start the policy evaluator: %w", err)
	}
	ev.cmd, ev.in, ev.enc, ev.dec = cmd, in, gob.NewEncoder(in), gob.NewDecoder(out)
	return nil
}

// stop kills the child, if any; the next request starts another.
func (ev *evaluator) stop() {
	if ev.cmd == nil {
		return
	}
	_ = ev.in.Close()         // ignored: the kill below ends it either way
	_ = ev.cmd.Process.Kill() // ignored: it may have exited already
	_ = ev.cmd.Wait()         // ignored: a killed child's status is the kill
	ev.cmd = nil
}

// capped keeps the first maxQuestion bytes a child writes to stderr.
type capped struct {
	mu sync.Mutex
	b  []byte
}

func (c *capped) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.b = append(c.b, p[:min(len(p), max(maxQuestion-len(c.b), 0))]...)
	return len(p), nil
}

// first is the first line the child wrote, as " (line)", or "".
func (c *capped) first() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	line, _, _ := strings.Cut(strings.TrimSpace(string(c.b)), "\n")
	if line == "" {
		return ""
	}
	return " (" + line + ")"
}

func (r reply) answers() (Answers, []Failure) {
	a := r.Answers
	a.shown = r.Shown
	for k, names := range a.Rows { // gob sends an empty list as nil; an empty answer is an empty list
		if names == nil {
			a.Rows[k] = []string{}
		}
	}
	fs := make([]Failure, len(r.Failures))
	for i, f := range r.Failures {
		fs[i] = Failure{Version: f.Version, Label: f.Label, Question: f.Question, Subject: f.Subject, Line: f.Line, Msg: f.Msg, eof: f.EOF}
	}
	return a, fs
}

// ServeMain is the child's main, for layer 6's binary and the tests'
// TestMain: it serves requests until its input closes, and returns the
// exit status.
func ServeMain() int {
	if err := capMemory(); err != nil {
		fmt.Fprintln(os.Stderr, "policy-eval:", err)
		return 1
	}
	if err := ServeEval(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "policy-eval:", err)
		return 1
	}
	return 0
}

// ServeEval answers each request read from r on w, until r ends, on an
// in-process engine of its own on the wall clock.
func ServeEval(r io.Reader, w io.Writer) error {
	e := newLocal(RealClock{})
	dec, enc := gob.NewDecoder(r), gob.NewEncoder(w)
	for {
		var req request
		if err := dec.Decode(&req); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		if err := enc.Encode(e.serve(req)); err != nil {
			return err
		}
	}
}

// serve is one request answered in this process.
func (e *Engine) serve(req request) reply {
	e.builtin, e.budget = req.Builtin, req.Budget
	before := e.Compiles()
	src := source{req.Src.Version, req.Src.Label, req.Src.Body, req.Src.Stored}
	var a Answers
	var fs []Failure
	switch req.Kind {
	case "tick":
		a, fs = e.AnswerContext(context.Background(), req.Versions, req.Snap, req.Roster, req.Q)
	case "check":
		a, fs = e.checkRun(context.Background(), src, req.Snap, req.Roster)
	default:
		a, fs = e.catalog(context.Background(), src)
	}
	rep := reply{Answers: a, Shown: a.shown, Compiles: e.Compiles() - before, Live: e.live.Load() > 0}
	for _, f := range fs {
		rep.Failures = append(rep.Failures, wireFailure{f.Version, f.Label, f.Question, f.Subject, f.Msg, f.Line, f.eof})
	}
	return rep
}
