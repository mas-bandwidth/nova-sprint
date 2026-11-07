package policy

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"go.starlark.net/starlark"
)

// A tick's one deadline: each version runs on its own
// goroutine and thread under the tick's context; the deadline cancels the
// thread, every phase (compile, each answer, each validation loop, the joint
// check) checks it, and the caller stops waiting at it, so a single builtin
// step that outlasts it (one sort of 50M ints) is abandoned, not waited for:
// its goroutine ends at its next step. A version's answers are used and
// cached only when its whole run passed before the deadline.

// run is one version's run in one tick: what the caller reads when the
// deadline passes first (the program and the ask in hand).
type run struct {
	e     *Engine
	ctx   context.Context
	src   source
	all   bool // check: every failure, and the answers' text; never cached
	th    *starlark.Thread
	prog  atomic.Pointer[program]
	at    atomic.Pointer[ask]
	asks  []ask
	ready chan outcome
}

// outcome is a run's result: its answers, its failures (none when it
// passed), and the answers to cache when it is the one used.
type outcome struct {
	a       Answers
	fs      []Failure
	p       *program
	pending map[[32]byte]cached
}

// cached is one validated answer: what it sets in Answers and the bytes it
// holds; for rows, the answer's names and their key (joined by NUL), which
// each tick interns.
type cached struct {
	set    func(*Answers)
	weight int
	key    string
	names  []string
}

// overTime is a run's failure when the deadline passed first.
func overTime() string { return fmt.Sprintf("over the tick's time budget of %s", TickTime) }

// start runs src's asks on a goroutine of its own; its outcome arrives on ready.
func (e *Engine) start(ctx context.Context, src source, asks []ask, all bool) *run {
	r := &run{e: e, ctx: ctx, src: src, all: all, th: newThread(src.label), asks: asks, ready: make(chan outcome, 1)}
	e.live.Add(1)
	go func() {
		defer e.live.Add(-1)
		r.ready <- r.exec()
	}()
	return r
}

// race answers a tick: tried in order, each started when the one before it
// fails, or at fallbackAfter from start (the tick's first byte, before its
// inputs were built) when it has not answered; the first in
// order that passes before the deadline answers. Each failing version
// before it is one Failure; when none passes, Version is -1.
func (e *Engine) race(ctx context.Context, start time.Time, tried []source, asks []ask) (Answers, []Failure) {
	runs := make([]*run, len(tried))
	launch := func(i int) {
		if runs[i] == nil {
			runs[i] = e.start(ctx, tried[i], asks, false)
		}
	}
	spec := e.clock.After(fallbackAfter - e.clock.Now().Sub(start))
	var failed []Failure
	for i := range tried {
		launch(i)
		o := runs[i].await(spec, func() {
			for j := i + 1; j < len(tried); j++ {
				launch(j)
			}
		})
		if len(o.fs) == 0 {
			e.commit(o.p, o.pending)
			return o.a, failed
		}
		failed = append(failed, o.fs[0])
	}
	return Answers{Version: -1}, failed
}

// await is r's outcome, or at the deadline the failure naming where it was;
// early starts the fallbacks when spec fires.
func (r *run) await(spec <-chan time.Time, early func()) outcome {
	for {
		select {
		case o := <-r.ready:
			return o
		case <-spec:
			spec = nil
			early()
		case <-r.ctx.Done():
			select {
			case o := <-r.ready:
				return o
			default:
				return outcome{fs: []Failure{r.late()}}
			}
		}
	}
}

// late is the failure of a run the deadline overtook: at the def of the
// question in hand, or across the top level while it compiled.
func (r *run) late() Failure {
	p, k := r.prog.Load(), r.at.Load()
	if p == nil || k == nil {
		return Failure{Version: r.src.version, Label: r.src.label, Line: 1,
			Msg: fmt.Sprintf("%s running the top level, lines 1 to %d", overTime(), lastLine(r.src.body))}
	}
	return *p.fail(k, 0, overTime())
}

// exec compiles and answers, with the deadline cancelling the thread.
func (r *run) exec() outcome {
	stop := context.AfterFunc(r.ctx, func() { r.th.Cancel(overTime()) })
	defer stop()
	p, f := r.e.program(r.src, r.th)
	if f != nil {
		return outcome{fs: []Failure{*f}}
	}
	r.prog.Store(p)
	o := outcome{a: newAnswers(p.version), p: p, pending: map[[32]byte]cached{}}
	if r.all {
		o.a.shown = map[string]string{}
	}
	var cache map[[32]byte]cached
	if !r.all {
		cache = r.e.cacheOf(p)
	}
	t := &tally{rows: map[string][]string{}}
	for i := range r.asks {
		if f := r.one(p, &r.asks[i], cache, t, &o); f != nil {
			if o.refuse(&r.asks[i], *f); !r.all || r.ctx.Err() != nil {
				return o
			}
			r.th.Uncancel() // a spent step budget cancels the thread; the deadline is checked next
		}
	}
	r.joint(p, &o)
	return o
}

// one answers ask k into o: from the cache, or called and validated.
func (r *run) one(p *program, k *ask, cache map[[32]byte]cached, t *tally, o *outcome) *Failure {
	if r.ctx.Err() != nil {
		return p.fail(k, 0, overTime())
	}
	r.at.Store(k)
	c, ok := cache[k.digest]
	if !ok {
		var f *Failure
		if c, f = r.fresh(p, k, o); f != nil {
			return f
		}
	}
	if k.rq != nil {
		names := t.intern(c.key, c.names)
		c.set = func(a *Answers) { a.Rows[*k.rq] = names }
	}
	if t.bytes += c.weight + entryBytes; t.bytes > maxTickBytes {
		return p.fail(k, 0, fmt.Sprintf("the tick's answers are over %d bytes", maxTickBytes))
	}
	c.set(&o.a)
	o.pending[k.digest] = c
	return nil
}

// fresh is ask k called on the program and validated; for check, its text.
func (r *run) fresh(p *program, k *ask, o *outcome) (cached, *Failure) {
	if p.fns[k.q] == nil { // catalog, which a version may leave out
		return r.inherit(p, k)
	}
	v, f := call(r.th, p, k)
	if f != nil {
		if r.ctx.Err() != nil {
			f.Msg = overTime()
		}
		return cached{}, f
	}
	var c cached
	var msg string
	if k.rq != nil {
		if c.names, msg = k.acceptRows(v); msg == "" {
			c.key = strings.Join(c.names, "\x00")
		}
	} else if c.set, msg = k.accept(v); msg == "" {
		c.weight = weight(v)
	}
	if msg != "" {
		return cached{}, p.fail(k, 0, msg)
	}
	if r.all {
		o.a.shown[k.q+"("+k.subject+")"] = v.String()
	}
	return c, nil
}

// refuse records f, and for check marks k's answer refused, so a refused
// answer is never shown as a change of value.
func (o *outcome) refuse(k *ask, f Failure) {
	o.fs = append(o.fs, f)
	if o.a.shown != nil {
		o.a.shown[k.q+"("+k.subject+")"] = "(refused)"
	}
}

// joint runs each rows answer's joint check once every answer is in. When
// a width was refused (check goes on past it) the joint check has nothing
// to stand on and is not run: the width's refusal is the report's, not a
// rows refusal per card.
func (r *run) joint(p *program, o *outcome) {
	widths := 0
	for i := range r.asks {
		widths += b2i(r.asks[i].q == "width")
	}
	for i := range r.asks {
		k := &r.asks[i]
		if _, ok := o.pending[k.digest]; k.joint == nil || !ok || len(o.a.Width) < widths {
			continue
		}
		msg := k.joint(&o.a)
		if r.ctx.Err() != nil {
			msg = overTime()
		}
		if msg != "" {
			if o.refuse(k, *p.fail(k, 0, msg)); !r.all || r.ctx.Err() != nil {
				return
			}
		}
	}
}

// inherit is the catalog of a version without catalog(): the built-in's,
// whose version it names (the catalog that answered), in a tick
// and in ReadCatalog alike. The built-in failing is p's failure at its last
// line, never a panic.
func (r *run) inherit(p *program, k *ask) (cached, *Failure) {
	fail := func(msg string) (cached, *Failure) {
		return cached{}, p.fail(k, 0, "catalog() is not defined, and the built-in's "+msg)
	}
	b, f := r.e.program(storedSource(0, r.e.builtin), r.th)
	if f != nil {
		return fail("failed: " + f.Error())
	}
	if b.fns["catalog"] == nil {
		return fail("is missing")
	}
	v, f := call(r.th, b, k)
	if f != nil {
		return fail("failed: " + f.Error())
	}
	c, msg := checkCatalog(r.ctx, v)
	if msg != "" {
		return fail("is refused: " + msg)
	}
	c.Version = 0
	return cached{set: func(a *Answers) { a.Catalog = c.clone() }, weight: weight(v)}, nil
}

// entryBytes is what one cached answer costs beyond its weight: its key,
// its closure and the map's slot (a guess).
const entryBytes = 96

// tally is what one run's answers hold: their bytes, and the rows answers
// by content, so equal answers share one slice and count once.
type tally struct {
	bytes int
	rows  map[string][]string
}

// intern is the tick's one slice for the rows answer names, whose key is
// the names joined by NUL.
func (t *tally) intern(key string, names []string) []string {
	if s, ok := t.rows[key]; ok {
		return s
	}
	t.rows[key] = names
	t.bytes += len(key) + 16*len(names) + entryBytes
	return names
}

// weight is the bytes a validated answer holds, about: its strings and
// numbers and the structure around them. Validated answers are at most a
// few levels deep (a catalog), so the recursion is shallow.
func weight(v starlark.Value) int {
	switch x := v.(type) {
	case starlark.String:
		return 16 + len(x)
	case starlark.Indexable: // a tuple or a list
		n := 24
		for i := range x.Len() {
			n += weight(x.Index(i))
		}
		return n
	case *starlark.Dict:
		n := 48
		for _, kv := range x.Items() {
			n += weight(kv[0]) + weight(kv[1])
		}
		return n
	}
	return 16
}

// digestOf keys the answer cache: the question and its input.
func digestOf(q, key string) [32]byte { return sha256.Sum256([]byte(q + " " + key)) }
