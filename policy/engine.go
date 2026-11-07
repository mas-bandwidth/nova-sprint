package policy

import (
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
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
	// a thousand times what the default's rows takes.
	CallSteps = 1_000_000
	// TickTime is a tick's one deadline, from the first byte of its input
	// to the last answer returned (ruling 4, 2026-11-06): measured, the
	// built-in's cold tick at 5,000 producers was 217 ms on amd-3, and 500 ms
	// is that with a margin of 2x (ruling 3). It bounds time only: Starlark
	// has no memory limit and none exists here, so a version may allocate
	// what one builtin step allocates before the deadline stops it (a
	// separate evaluator process is the 2.0.x item that would bound memory).
	// A -race build runs several times slower and scales it (race.go).
	TickTime = 500 * time.Millisecond * raceSlowdown
	// fallbackAfter is when a tick starts the versions after the one it is
	// waiting on, if that one has not answered: a guess, half of TickTime,
	// so the built-in (217 ms cold, measured) still has time to answer.
	fallbackAfter = TickTime / 2
	// maxTickBytes caps one tick's answers, and so the answer cache, which
	// holds one tick's (ruling 4): 4 MiB.
	maxTickBytes = 4 << 20
)

// predeclared is the Starlark universe plus TIERS and LEVELS in ladder
// order and prefer (prefer.go); nothing else. Starlark has no clock, random,
// file or network, and none is added; load is refused (Thread.Load is nil).
var predeclared = starlark.StringDict{"TIERS": tupleOf(store.Tiers), "LEVELS": tupleOf(store.Priorities), "prefer": starlark.NewBuiltin("prefer", prefer)}

func tupleOf(ss []string) starlark.Tuple {
	t := make(starlark.Tuple, len(ss))
	for i, s := range ss {
		t[i] = starlark.String(s)
	}
	return t
}

// source is a body a tick or a verb runs: a stored version, the built-in
// (version 0) or an unstored file (named by its file name, never cached).
type source struct {
	version     int
	label, body string
	stored      bool
}

func storedSource(v int, body string) source { return source{v, Label(v), body, true} }

// program is one compiled body: its frozen globals' question functions, its
// last line, and for a stored version or the built-in the answers of the
// last tick it answered (replaced whole, never changed in place).
type program struct {
	version int
	label   string
	last    int
	fns     map[string]*starlark.Function
	stored  bool
	answers map[[32]byte]cached
}

// Engine compiles each stored version and the built-in once (by label and
// body, so a test's edited built-in is a new program) and caches the
// answers of the last tick: versions are immutable and answers
// deterministic, so the cache is exact. It is safe for concurrent use.
type Engine struct {
	mu       sync.Mutex
	progs    map[string]*program
	builtin  string // the built-in's body: defaultBody, or a test's
	compiles int
}

// NewEngine is an engine with empty caches.
func NewEngine() *Engine { return &Engine{progs: map[string]*program{}, builtin: defaultBody} }

// Compiles counts the compiles this engine ran (a test reads it).
func (e *Engine) Compiles() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.compiles
}

// program is src compiled on th, from the engine's programs when src is
// stored and compiled before. Two ticks may compile one version at once;
// the programs are equal, and the last kept.
func (e *Engine) program(src source, th *starlark.Thread) (*program, *Failure) {
	key := src.label + "\x00" + src.body
	e.mu.Lock()
	p, ok := e.progs[key]
	e.compiles += b2i(!ok)
	e.mu.Unlock()
	if ok {
		return p, nil
	}
	p, f := compile(src.version, src.label, src.body, th)
	if f == nil && src.stored {
		p.stored = true
		e.mu.Lock()
		e.progs[key] = p
		e.mu.Unlock()
	}
	return p, f
}

// cacheOf is p's cache, nil for an unstored file.
func (e *Engine) cacheOf(p *program) map[[32]byte]cached {
	e.mu.Lock()
	defer e.mu.Unlock()
	return p.answers
}

// commit makes a tick's answers p's cache, once the whole tick has passed,
// and empties every other program's: the cache holds one tick, at most
// maxTickBytes.
func (e *Engine) commit(p *program, pending map[[32]byte]cached) {
	if !p.stored {
		return // an unstored file: never cached
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, q := range e.progs {
		q.answers = nil
	}
	p.answers = pending
}

// Label names version v in refusals: v7, or default for 0.
func Label(v int) string {
	if v == 0 {
		return "default"
	}
	return fmt.Sprintf("v%d", v)
}

// compile runs a body's top level on th under the step budget, freezes its
// globals, and checks the closed set of names and each question's arity.
func compile(version int, label, body string, th *starlark.Thread) (*program, *Failure) {
	p := &program{version: version, label: label, last: lastLine(body), fns: map[string]*starlark.Function{}}
	fail := func(line int, format string, args ...any) *Failure {
		return &Failure{Version: version, Label: label, Line: line, Msg: cut(fmt.Sprintf(format, args...), maxQuestion)}
	}
	if len(body) > MaxBody {
		f := fail(p.last, "the file is %d bytes, over %d", len(body), MaxBody)
		f.eof = true
		return nil, f
	}
	th.SetMaxExecutionSteps(th.ExecutionSteps() + CallSteps)
	file, prog, err := starlark.SourceProgramOptions(&syntax.FileOptions{}, label+".star", body, predeclared.Has)
	var globals starlark.StringDict
	if err == nil {
		globals, err = prog.Init(th, predeclared)
	}
	if err != nil {
		return nil, fail(errLine(err), "%s", errMsg(th, 0, err))
	}
	globals.Freeze()
	lines := topLines(file)
	for _, name := range globals.Keys() {
		if f := p.bind(name, globals[name], lines[name], fail); f != nil {
			return nil, f
		}
	}
	for _, q := range questionNames() {
		if p.fns[q] == nil && q != "catalog" { // catalog is optional: a version without it uses the built-in's
			f := fail(p.last, "question %s is missing: define def %s(%s)", q, q, params[q])
			f.eof = true
			return nil, f
		}
	}
	return p, nil
}

// lastLine is a body's last line, which a refusal about something absent names.
func lastLine(body string) int { return strings.Count(strings.TrimRight(body, "\n"), "\n") + 1 }

// topLines are the lines where the file's top level binds each name, so a
// refusal of a name that is not a function names its line too.
func topLines(f *syntax.File) map[string]int {
	out := map[string]int{}
	for _, s := range f.Stmts {
		switch x := s.(type) {
		case *syntax.DefStmt:
			out[x.Name.Name] = int(x.Name.NamePos.Line)
		case *syntax.AssignStmt:
			if id, ok := x.LHS.(*syntax.Ident); ok {
				out[id.Name] = int(id.NamePos.Line)
			}
		}
	}
	return out
}

// bind checks one top-level name, bound at line: a helper, or a question
// defined as a function of its arity.
func (p *program) bind(name string, v starlark.Value, line int, fail func(int, string, ...any) *Failure) *Failure {
	if strings.HasPrefix(name, "_") {
		return nil
	}
	n, ok := Arity[name]
	fn, isFn := v.(*starlark.Function)
	switch {
	case !ok:
		return fail(line, "unknown top-level name %s: a question is one of %s, a helper starts with _", name, strings.Join(questionNames(), " "))
	case !isFn:
		return fail(line, "%s is a %s, not a function: define def %s(%s)", name, v.Type(), name, params[name])
	case fn.NumParams() != n || fn.HasVarargs() || fn.HasKwargs():
		return fail(line, "%s takes %d parameter(s): def %s(%s)", name, n, name, params[name])
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

// fail is p's refusal of ask k: at line, or at the question's def when line
// is 0, or at the file's last line when it has no def (catalog).
func (p *program) fail(k *ask, line int, msg string) *Failure {
	f := &Failure{Version: p.version, Label: p.label, Question: k.q, Subject: k.subject, Line: line, Msg: cut(msg, maxQuestion)}
	if fn := p.fns[k.q]; line == 0 && fn != nil {
		f.Line = int(fn.Position().Line)
	} else if line == 0 {
		f.Line, f.eof = p.last, true
	}
	return f
}

// newThread is one version's thread for one tick: output discarded, no load.
func newThread(name string) *starlark.Thread {
	return &starlark.Thread{Name: name, Print: func(*starlark.Thread, string) {}}
}

// call answers one ask on th under the per-call step budget.
func call(th *starlark.Thread, p *program, k *ask) (starlark.Value, *Failure) {
	start := th.ExecutionSteps()
	th.SetMaxExecutionSteps(start + CallSteps)
	v, err := starlark.Call(th, p.fns[k.q], k.args, nil)
	if err != nil {
		return nil, p.fail(k, errLine(err), errMsg(th, start, err))
	}
	return v, nil
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

// errMsg is an error's message without its position, which the refusal
// names, or the step budget when the call (begun at step start) spent it.
func errMsg(th *starlark.Thread, start uint64, err error) string {
	var se syntax.Error
	var re resolve.ErrorList
	var ee *starlark.EvalError
	switch {
	case th.ExecutionSteps() >= start+CallSteps:
		return fmt.Sprintf("over the step budget of %d", CallSteps)
	case errors.As(err, &se):
		return se.Msg
	case errors.As(err, &re):
		return re[0].Msg
	case errors.As(err, &ee):
		return strings.TrimPrefix(ee.Msg, "Starlark computation cancelled: ")
	}
	return err.Error()
}
