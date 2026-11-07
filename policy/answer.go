package policy

import (
	"context"
	"maps"
	"slices"

	"go.starlark.net/starlark"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Versions is what a tick is answered from: the active version and the one
// active before it (from the last accepted activation), each nil when none.
type Versions struct{ Active, Prev *store.PolicyVersion }

// tried is the order a tick tries: active, else the previous active, else
// the built-in default (version 0).
func (vs Versions) tried(builtin string) []source {
	var out []source
	if vs.Active != nil {
		out = append(out, storedSource(vs.Active.Version, vs.Active.Body))
	}
	if vs.Prev != nil && (vs.Active == nil || vs.Prev.Version != vs.Active.Version) {
		out = append(out, storedSource(vs.Prev.Version, vs.Prev.Body))
	}
	return append(out, storedSource(0, builtin))
}

// Answer is AnswerContext with no deadline but the tick's own, TickTime.
func (e *Engine) Answer(vs Versions, snap store.Snapshot, q Questions) (Answers, []Failure) {
	return e.AnswerContext(context.Background(), vs, snap, q)
}

// AnswerContext is the tick's answer (docs/LAYER-5-POLICY.md, "The tick's
// answer"; tla/Policy.tla Tick): one version answers the whole tick. The
// tick has one deadline, TickTime from the first byte of its input, or
// ctx's when sooner. Any error, refusal or the deadline in a version means
// none of its answers is used and the next is tried; the failures are
// returned, one per failing version, for the policy judgements
// (Judgements). It never stalls: when even the built-in fails,
// Answers.Version is -1 and the tick applies every move that needs no
// answer. A subject the snapshot does not hold is not asked.
func (e *Engine) AnswerContext(ctx context.Context, vs Versions, snap store.Snapshot, q Questions) (Answers, []Failure) {
	ctx, cancel := context.WithTimeout(ctx, TickTime)
	defer cancel()
	tried := vs.tried(e.builtin)
	asks := newIndex(snap).asks(ctx, q)
	if ctx.Err() != nil { // the inputs alone spent the deadline: no version can answer
		b := tried[len(tried)-1]
		return Answers{Version: -1}, []Failure{{Version: b.version, Label: b.label, Line: 1, Msg: overTime() + " building the tick's inputs"}}
	}
	return e.race(ctx, tried, asks)
}

// Judgements are the policy judgements the failures open: one per failing
// version with none open (its group key), named by the snapshot's seq so an
// id is never reused after an answer.
func Judgements(snap store.Snapshot, failed []Failure) []store.PutJudgement {
	var out []store.PutJudgement
	for _, f := range failed {
		if len(OpenJudgements(snap, f.Version)) == 0 {
			out = append(out, f.Judgement(snap.Seq))
		}
	}
	return out
}

// ask is one question about one subject, with the check that validates its
// answer into what it sets (for rows, into names the tick interns), and for
// rows the joint check run once every answer is in.
type ask struct {
	q, subject string
	args       starlark.Tuple
	digest     [32]byte // the answer cache's key: the question and its input
	accept     func(v starlark.Value) (func(*Answers), string)
	rq         *RowsQuestion
	acceptRows func(v starlark.Value) ([]string, string)
	joint      func(a *Answers) string
}

func newAnswers(v int) Answers {
	return Answers{Version: v, Order: map[string][]any{}, Rows: map[RowsQuestion][]string{}, Reads: map[string]int{},
		Verdict: map[string]string{}, Width: map[string]Width{}, Landing: map[string]Landing{}, Stitch: map[string]bool{}}
}

func catalogAsk(ctx context.Context) ask {
	return ask{q: "catalog", digest: digestOf("catalog", "()"), accept: func(v starlark.Value) (func(*Answers), string) {
		c, msg := checkCatalog(ctx, v)
		return func(a *Answers) { a.Catalog = c.clone(); a.Catalog.Version = a.Version }, msg
	}}
}

// asks are the tick's questions in the order they are answered: the
// argument-free ones, width for every row, then the per-subject ones. The
// validators that loop over what a policy returns check ctx.
func (ix *index) asks(ctx context.Context, q Questions) []ask {
	out := []ask{
		{q: "limits", accept: func(v starlark.Value) (func(*Answers), string) {
			l, msg := checkLimits(v)
			return func(a *Answers) { a.Limits = l }, msg
		}},
		{q: "timeouts", accept: func(v starlark.Value) (func(*Answers), string) {
			t, msg := checkTimeouts(v)
			return func(a *Answers) { a.Timeouts = t; a.Timeouts.BaseRegate = slices.Clone(t.BaseRegate) }, msg
		}},
		{q: "prices", accept: func(v starlark.Value) (func(*Answers), string) {
			p, msg := checkPrices(ctx, v)
			return func(a *Answers) { a.Prices = maps.Clone(p) }, msg
		}},
		catalogAsk(ctx),
	}
	for _, r := range ix.rows {
		out = append(out, ask{q: "width", subject: r.Name, args: starlark.Tuple{row(r)}, accept: func(v starlark.Value) (func(*Answers), string) {
			w, msg := checkWidth(v, r.Width)
			return func(a *Answers) { a.Width[r.Name] = w }, msg
		}})
	}
	for _, id := range q.Order {
		if c, ok := ix.producer(id); ok {
			out = append(out, ask{q: "order", subject: id, args: starlark.Tuple{orderCard(c)}, accept: func(v starlark.Value) (func(*Answers), string) {
				o, msg := checkOrder(v)
				return func(a *Answers) { a.Order[id] = slices.Clone(o) }, msg
			}})
		}
	}
	for _, rq := range q.Rows {
		if c, ok := ix.producer(rq.Card); ok && (rq.Kind == KindWork || rq.Kind == KindRead) {
			out = append(out, ix.rowsAsk(c, rq))
		}
	}
	out = append(out, ix.subjectAsks(q)...)
	for i := range out {
		if out[i].digest == ([32]byte{}) {
			out[i].digest = digestOf(out[i].q, out[i].args.String())
		}
	}
	return out
}

// rowsAsk is one rows question, with its joint check: the rows named must
// leave the card a row able to work it (a work answer above 0 in width) when
// a candidate could (a configured width above 0).
func (ix *index) rowsAsk(c store.Card, rq RowsQuestion) ask {
	set := ix.candidateSet(c, rq.Kind)
	card := ix.card(c)
	return ask{q: "rows", subject: rq.Card + " " + rq.Kind, args: starlark.Tuple{card, starlark.String(rq.Kind), set.list},
		digest: digestOf("rows", card.String()+" "+rq.Kind+" "+set.digest), rq: &rq,
		acceptRows: func(v starlark.Value) ([]string, string) { return checkRows(v, c, set, ix.snap.Rows) },
		joint: func(a *Answers) string {
			for _, n := range a.Rows[rq] {
				if a.Width[n].Work > 0 {
					return ""
				}
			}
			for _, r := range set.rows {
				if r.Width > 0 {
					return refuse("every row named has work 0 in width, while row %s could take it", r.Name)
				}
			}
			return ""
		}}
}

// subjectAsks are reads, verdict, landing and stitch.
func (ix *index) subjectAsks(q Questions) []ask {
	var out []ask
	for _, id := range q.Reads {
		if c, ok := ix.producer(id); ok {
			out = append(out, ask{q: "reads", subject: id, args: starlark.Tuple{ix.card(c)}, accept: func(v starlark.Value) (func(*Answers), string) {
				n, msg := checkReads(v)
				return func(a *Answers) { a.Reads[id] = n }, msg
			}})
		}
	}
	for _, id := range q.Verdict {
		if c, ok := ix.producer(id); ok {
			out = append(out, ix.verdictAsk(c))
		}
	}
	for _, lq := range q.Landing {
		if _, ok := ix.streams[lq.Stream]; ok {
			out = append(out, ask{q: "landing", subject: lq.Stream, args: starlark.Tuple{ix.landingStream(lq)}, accept: func(v starlark.Value) (func(*Answers), string) {
				l, msg := checkLanding(v)
				return func(a *Answers) { a.Landing[lq.Stream] = l }, msg
			}})
		}
	}
	for _, s := range q.Stitch {
		if _, ok := ix.streams[s]; ok {
			out = append(out, ask{q: "stitch", subject: s, args: starlark.Tuple{ix.stitchStream(s)}, accept: func(v starlark.Value) (func(*Answers), string) {
				b, msg := checkStitch(v)
				return func(a *Answers) { a.Stitch[s] = b }, msg
			}})
		}
	}
	return out
}

func (ix *index) verdictAsk(c store.Card) ask {
	ids := ix.reads[c.ID]
	reads, results := make(starlark.Tuple, len(ids)), make([]string, len(ids))
	for i, id := range ids {
		reads[i], results[i] = ix.read(ix.snap.Cards[id]), ix.snap.Cards[id].Result
	}
	return ask{q: "verdict", subject: c.ID, args: starlark.Tuple{ix.card(c), reads}, accept: func(v starlark.Value) (func(*Answers), string) {
		s, msg := checkVerdict(v, results)
		return func(a *Answers) { a.Verdict[c.ID] = s }, msg
	}}
}

func (ix *index) producer(id string) (store.Card, bool) {
	c, ok := ix.snap.Cards[id]
	return c, ok && c.Kind == "producer"
}
