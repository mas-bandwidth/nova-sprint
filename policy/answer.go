package policy

import (
	"go.starlark.net/starlark"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Versions is what a tick is answered from: the active version and the one
// active before it (from the last accepted activation), each nil when none.
type Versions struct{ Active, Prev *store.PolicyVersion }

// tried is the order a tick tries: active, else the previous active, else
// the built-in default (version 0).
func (vs Versions) tried() []store.PolicyVersion {
	var out []store.PolicyVersion
	if vs.Active != nil {
		out = append(out, *vs.Active)
	}
	if vs.Prev != nil && (vs.Active == nil || vs.Prev.Version != vs.Active.Version) {
		out = append(out, *vs.Prev)
	}
	return append(out, store.PolicyVersion{Version: 0, Kind: "script", Name: "default.star", Body: defaultBody})
}

// Answer is the tick's answer (docs/LAYER-5-POLICY.md, "The tick's answer";
// tla/Policy.tla Tick): one version answers the whole tick. Any error or
// refusal in a version means none of its answers is used and the next is
// tried; the failures are returned, one per failing version, for the
// policy judgements (Judgements). It never stalls: when even the built-in
// fails, Answers.Version is -1 and the tick applies every move that needs
// no answer. A subject the snapshot does not hold is not asked.
func (e *Engine) Answer(vs Versions, snap store.Snapshot, q Questions) (Answers, []Failure) {
	asks := newIndex(snap).asks(q)
	var failed []Failure
	for _, v := range vs.tried() {
		p, f := e.program(v)
		if f == nil {
			a, fs := e.run(p, asks, false)
			if len(fs) == 0 {
				return a, failed
			}
			f = &fs[0]
		}
		failed = append(failed, *f)
	}
	return Answers{Version: -1}, failed
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

// ask is one question about one subject, with the check that stores its
// answer, and for rows the joint check run once every answer is in.
type ask struct {
	q, subject string
	args       starlark.Tuple
	key        string // the input, for the answer cache: args printed, or for rows a digest of its candidates
	accept     func(a *Answers, v starlark.Value) string
	joint      func(a *Answers) string
}

// run answers every ask with one program and checks each answer. With all
// it collects every failure (check); without, it stops at the first (a tick).
func (e *Engine) run(p *program, asks []ask, all bool) (Answers, []Failure) {
	a := Answers{Version: p.version, Order: map[string][]any{}, Rows: map[RowsQuestion][]string{}, Reads: map[string]int{},
		Verdict: map[string]string{}, Width: map[string]Width{}, Landing: map[string]Landing{}, Stitch: map[string]bool{}, shown: map[string]string{}}
	var failed []Failure
	b := newBudget(p.label)
	defer b.stop()
	refused := func(k ask, msg string) bool {
		failed = append(failed, Failure{Version: p.version, Label: p.label, Question: k.q, Subject: k.subject, Line: int(p.fns[k.q].Position().Line), Msg: msg})
		return !all
	}
	for _, k := range asks {
		if p.fns[k.q] == nil { // catalog, which a version may leave out
			a.Catalog = builtinCatalog()
			continue
		}
		v, f := e.call(p, k, b)
		if f != nil {
			if failed = append(failed, *f); !all {
				return a, failed
			}
			continue
		}
		if msg := k.accept(&a, v); msg != "" {
			if refused(k, msg) {
				return a, failed
			}
			continue
		}
		a.shown[k.q+"("+k.subject+")"] = v.String()
	}
	for _, k := range asks {
		if _, ok := a.shown[k.q+"("+k.subject+")"]; ok && k.joint != nil {
			if msg := k.joint(&a); msg != "" && refused(k, msg) {
				return a, failed
			}
		}
	}
	return a, failed
}

// asks are the tick's questions in the order they are answered: the
// argument-free ones, width for every row, then the per-subject ones.
func (ix *index) asks(q Questions) []ask {
	out := []ask{
		{q: "limits", accept: func(a *Answers, v starlark.Value) (msg string) { a.Limits, msg = checkLimits(v); return }},
		{q: "timeouts", accept: func(a *Answers, v starlark.Value) (msg string) { a.Timeouts, msg = checkTimeouts(v); return }},
		{q: "prices", accept: func(a *Answers, v starlark.Value) (msg string) { a.Prices, msg = checkPrices(v); return }},
		{q: "catalog", accept: func(a *Answers, v starlark.Value) (msg string) { a.Catalog, msg = checkCatalog(v); return }},
	}
	for _, r := range ix.rows {
		out = append(out, ask{q: "width", subject: r.Name, args: starlark.Tuple{row(r)}, accept: func(a *Answers, v starlark.Value) string {
			w, msg := checkWidth(v, r.Width)
			a.Width[r.Name] = w
			return msg
		}})
	}
	for _, id := range q.Order {
		if c, ok := ix.producer(id); ok {
			out = append(out, ask{q: "order", subject: id, args: starlark.Tuple{orderCard(c)}, accept: func(a *Answers, v starlark.Value) string {
				o, msg := checkOrder(v)
				a.Order[id] = o
				return msg
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
		if out[i].key == "" {
			out[i].key = out[i].args.String()
		}
	}
	return out
}

// rowsAsk is one rows question, with its joint check: the rows named must
// leave the card a row able to work it (a work answer above 0 in width) when
// a candidate could (a configured width above 0).
func (ix *index) rowsAsk(c store.Card, rq RowsQuestion) ask {
	set := ix.candidateSet(c, rq.Kind)
	cands, card := set.rows, ix.card(c)
	return ask{q: "rows", subject: rq.Card + " " + rq.Kind, args: starlark.Tuple{card, starlark.String(rq.Kind), set.list},
		key: card.String() + " " + rq.Kind + " " + set.digest,
		accept: func(a *Answers, v starlark.Value) string {
			names, msg := checkRows(v, c, cands, ix.snap.Rows)
			a.Rows[rq] = names
			return msg
		},
		joint: func(a *Answers) string {
			for _, n := range a.Rows[rq] {
				if a.Width[n].Work > 0 {
					return ""
				}
			}
			for _, r := range cands {
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
			out = append(out, ask{q: "reads", subject: id, args: starlark.Tuple{ix.card(c)}, accept: func(a *Answers, v starlark.Value) string {
				n, msg := checkReads(v)
				a.Reads[id] = n
				return msg
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
			out = append(out, ask{q: "landing", subject: lq.Stream, args: starlark.Tuple{ix.landingStream(lq)}, accept: func(a *Answers, v starlark.Value) string {
				l, msg := checkLanding(v)
				a.Landing[lq.Stream] = l
				return msg
			}})
		}
	}
	for _, s := range q.Stitch {
		if _, ok := ix.streams[s]; ok {
			out = append(out, ask{q: "stitch", subject: s, args: starlark.Tuple{ix.stitchStream(s)}, accept: func(a *Answers, v starlark.Value) string {
				b, msg := checkStitch(v)
				a.Stitch[s] = b
				return msg
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
	return ask{q: "verdict", subject: c.ID, args: starlark.Tuple{ix.card(c), reads}, accept: func(a *Answers, v starlark.Value) string {
		s, msg := checkVerdict(v, results)
		a.Verdict[c.ID] = s
		return msg
	}}
}

func (ix *index) producer(id string) (store.Card, bool) {
	c, ok := ix.snap.Cards[id]
	return c, ok && c.Kind == "producer"
}
