package policy

import (
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.starlark.net/resolve"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/mas-bandwidth/nova-sprint/store"
)

//go:embed default.star
var defaultBody string

// DefaultBody is the built-in default policy, embedded in the binary.
func DefaultBody() string { return defaultBody }

const (
	// MaxBody is the largest policy file: 64 KiB.
	MaxBody = 64 << 10
	// CallSteps bounds one call (and the file's top level): a guess, about
	// a thousand times what the default's rows over 100 candidates takes.
	CallSteps = 1_000_000
	// TickSteps bounds one version's whole tick: a guess, 1.5 x the
	// default's 6,550,740 steps measured at 5,000 producers over 50 rows.
	TickSteps = 10_000_000
	// TickTime bounds one version's whole tick in wall-clock time, by
	// cancelling its thread: measured 2026-11-06, the cold tick at 5,000
	// producers was 129 ms on amd-3 (140 ms after this change). Starlark has
	// no memory limit, so this is also the bound on what a policy allocates.
	// A -race build runs several times slower and scales it (race.go).
	TickTime = 250 * time.Millisecond * raceSlowdown
	// cacheMax bounds the answer cache; past it the cache starts again (a guess).
	cacheMax = 200_000
)

// predeclared is the Starlark universe plus TIERS and LEVELS in ladder order;
// nothing else. Starlark has no clock, random, file or network, and none is
// added; load is refused (Thread.Load is nil).
var predeclared = starlark.StringDict{"TIERS": tupleOf(store.Tiers), "LEVELS": tupleOf(store.Priorities)}

func tupleOf(ss []string) starlark.Tuple {
	t := make(starlark.Tuple, len(ss))
	for i, s := range ss {
		t[i] = starlark.String(s)
	}
	return t
}

// program is one compiled version: its frozen globals' question functions.
type program struct {
	version int
	label   string
	fns     map[string]*starlark.Function
	cached  bool // a stored version or the built-in; an unstored file never is
}

// Engine compiles each stored version once and caches answers by version
// and input; versions are immutable, so neither cache is ever invalid. It is
// safe for concurrent use.
type Engine struct {
	mu       sync.Mutex
	progs    map[int]*program
	answers  map[string]starlark.Value
	compiles int
}

// NewEngine is an engine with empty caches.
func NewEngine() *Engine {
	return &Engine{progs: map[int]*program{}, answers: map[string]starlark.Value{}}
}

// Compiles counts the compiles this engine ran (a test reads it).
func (e *Engine) Compiles() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.compiles
}

// program is version v compiled, from the cache when it has been before.
// Version 0 is the built-in default.
func (e *Engine) program(v store.PolicyVersion) (*program, *Failure) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if p, ok := e.progs[v.Version]; ok {
		return p, nil
	}
	e.compiles++
	p, f := compile(v.Version, Label(v.Version), v.Body)
	if f == nil {
		p.cached = true
		e.progs[v.Version] = p
	}
	return p, f
}

// Label names version v in refusals: v7, or default for 0.
func Label(v int) string {
	if v == 0 {
		return "default"
	}
	return fmt.Sprintf("v%d", v)
}

// compile runs a body's top level under the step budget, freezes its
// globals, and checks the closed set of names and each question's arity.
func compile(version int, label, body string) (*program, *Failure) {
	fail := func(line int, format string, args ...any) *Failure {
		return &Failure{Version: version, Label: label, Line: line, Msg: fmt.Sprintf(format, args...)}
	}
	if len(body) > MaxBody {
		return nil, fail(0, "the file is %d bytes, over %d", len(body), MaxBody)
	}
	th := newThread(label)
	globals, err := starlark.ExecFileOptions(&syntax.FileOptions{}, th, label+".star", body, predeclared)
	if err != nil {
		return nil, fail(errLine(err), "%s", errMsg(th, err))
	}
	globals.Freeze()
	p := &program{version: version, label: label, fns: map[string]*starlark.Function{}}
	for _, name := range globals.Keys() {
		if f := p.bind(name, globals[name], fail); f != nil {
			return nil, f
		}
	}
	for _, q := range questionNames() {
		if p.fns[q] == nil && q != "catalog" { // catalog is optional: a version without it uses the built-in's
			return nil, fail(0, "question %s is missing: define def %s(%s)", q, q, params(q))
		}
	}
	return p, nil
}

// bind checks one top-level name: a helper, or a question defined
// as a function of its arity.
func (p *program) bind(name string, v starlark.Value, fail func(int, string, ...any) *Failure) *Failure {
	if strings.HasPrefix(name, "_") {
		return nil
	}
	n, ok := Arity[name]
	fn, isFn := v.(*starlark.Function)
	line := 0
	if isFn {
		line = int(fn.Position().Line)
	}
	switch {
	case !ok:
		return fail(line, "unknown top-level name %s: a question is one of %s, a helper starts with _", name, strings.Join(questionNames(), " "))
	case !isFn:
		return fail(line, "%s is a %s, not a function: define def %s(%s)", name, v.Type(), name, params(name))
	case fn.NumParams() != n || fn.HasVarargs() || fn.HasKwargs():
		return fail(line, "%s takes %d parameter(s): def %s(%s)", name, n, name, params(name))
	}
	p.fns[name] = fn
	return nil
}

func questionNames() []string {
	names := make([]string, 0, len(Arity))
	for q := range Arity {
		names = append(names, q)
	}
	slices.Sort(names)
	return names
}

// params is a question's parameter list as the design writes it.
func params(q string) string {
	return map[string]string{"order": "card", "rows": "card, kind, candidates", "reads": "card", "verdict": "card, reads",
		"width": "row", "landing": "stream", "stitch": "stream"}[q]
}

func newThread(name string) *starlark.Thread {
	th := &starlark.Thread{Name: name, Print: func(*starlark.Thread, string) {}}
	th.SetMaxExecutionSteps(CallSteps)
	return th
}

// budget is one version's run in one tick: one thread, the per-call and
// per-tick step budgets, and the wall clock that cancels it.
type budget struct {
	th    *starlark.Thread
	start uint64 // the thread's steps when the current call began
	late  atomic.Bool
	timer *time.Timer
}

func newBudget(label string) *budget {
	b := &budget{th: &starlark.Thread{Name: label, Print: func(*starlark.Thread, string) {}}}
	b.timer = time.AfterFunc(TickTime, func() { b.late.Store(true); b.th.Cancel("over the tick's time budget") })
	return b
}

func (b *budget) stop() { b.timer.Stop() }

// call answers one ask on budget b, from the cache when the version has
// answered the same input before (answers are deterministic, so the cache is
// exact). Only a stored version or the built-in is cached, keyed by its label.
func (e *Engine) call(p *program, k ask, b *budget) (starlark.Value, *Failure) {
	key := p.label + " " + k.q + " " + k.key
	if p.cached {
		e.mu.Lock()
		v, ok := e.answers[key]
		e.mu.Unlock()
		if ok {
			return v, nil
		}
	}
	fn := p.fns[k.q]
	fail := func(msg string, line int) (starlark.Value, *Failure) {
		return nil, &Failure{Version: p.version, Label: p.label, Question: k.q, Subject: k.subject, Line: cmpLine(line, fn), Msg: msg}
	}
	if b.late.Load() || b.th.ExecutionSteps() >= TickSteps {
		return fail(b.over(), 0)
	}
	b.th.Uncancel()
	b.start = b.th.ExecutionSteps()
	b.th.SetMaxExecutionSteps(min(b.start+CallSteps, TickSteps))
	v, err := starlark.Call(b.th, fn, k.args, nil)
	switch {
	case err != nil:
		return fail(b.errMsg(err), errLine(err))
	case b.late.Load():
		return fail(b.over(), 0)
	}
	if p.cached {
		e.mu.Lock()
		if len(e.answers) >= cacheMax {
			e.answers = map[string]starlark.Value{}
		}
		e.answers[key] = v
		e.mu.Unlock()
	}
	return v, nil
}

// over says which tick budget is spent.
func (b *budget) over() string {
	if b.late.Load() {
		return fmt.Sprintf("over the tick's time budget of %s", TickTime)
	}
	return fmt.Sprintf("over the tick's step budget of %d", TickSteps)
}

// errMsg is a call's error message, naming a spent budget.
func (b *budget) errMsg(err error) string {
	steps := b.th.ExecutionSteps()
	switch {
	case b.late.Load() || steps >= TickSteps:
		return b.over()
	case steps >= b.start+CallSteps:
		return fmt.Sprintf("over the step budget of %d", CallSteps)
	}
	return errText(err)
}

func cmpLine(line int, fn *starlark.Function) int {
	if line > 0 {
		return line
	}
	return int(fn.Position().Line)
}

// errLine is the line an error names: a syntax or resolve error's own, a
// runtime error's innermost frame in the file.
func errLine(err error) int {
	var se syntax.Error
	var re resolve.ErrorList
	var ee *starlark.EvalError
	switch {
	case errors.As(err, &se):
		return int(se.Pos.Line)
	case errors.As(err, &re):
		return int(re[0].Pos.Line)
	case errors.As(err, &ee):
		for i := len(ee.CallStack) - 1; i >= 0; i-- {
			if l := ee.CallStack[i].Pos.Line; l > 0 {
				return int(l)
			}
		}
	}
	return 0
}

// errMsg is an error's message without its position, which the refusal names.
func errMsg(th *starlark.Thread, err error) string {
	if th.ExecutionSteps() >= CallSteps {
		return fmt.Sprintf("over the step budget of %d", CallSteps)
	}
	return errText(err)
}

// errText is an error's message without its position.
func errText(err error) string {
	var se syntax.Error
	var re resolve.ErrorList
	var ee *starlark.EvalError
	switch {
	case errors.As(err, &se):
		return se.Msg
	case errors.As(err, &re):
		return re[0].Msg
	case errors.As(err, &ee):
		return ee.Msg
	}
	return err.Error()
}
