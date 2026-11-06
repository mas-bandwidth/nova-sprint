package policy

import (
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

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
		if p.fns[q] == nil {
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

// call answers one ask, from the cache when the version has answered the
// same input before (answers are deterministic, so the cache is exact). An
// unstored file (version 0 but not the built-in) is never cached.
func (e *Engine) call(p *program, k ask) (starlark.Value, *Failure) {
	key := fmt.Sprintf("%d %s %s", p.version, k.q, k.key)
	cache := p.version > 0 || p.label == "default"
	if cache {
		e.mu.Lock()
		v, ok := e.answers[key]
		e.mu.Unlock()
		if ok {
			return v, nil
		}
	}
	fn := p.fns[k.q]
	th := newThread(p.label)
	v, err := starlark.Call(th, fn, k.args, nil)
	if err != nil {
		return nil, &Failure{Version: p.version, Label: p.label, Question: k.q, Subject: k.subject, Line: cmpLine(errLine(err), fn), Msg: errMsg(th, err)}
	}
	if cache {
		e.mu.Lock()
		if len(e.answers) >= cacheMax {
			e.answers = map[string]starlark.Value{}
		}
		e.answers[key] = v
		e.mu.Unlock()
	}
	return v, nil
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
	var se syntax.Error
	var re resolve.ErrorList
	var ee *starlark.EvalError
	switch {
	case th.ExecutionSteps() >= CallSteps:
		return fmt.Sprintf("over the step budget of %d", CallSteps)
	case errors.As(err, &se):
		return se.Msg
	case errors.As(err, &re):
		return re[0].Msg
	case errors.As(err, &ee):
		return ee.Msg
	}
	return err.Error()
}
