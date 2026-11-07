package policy

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"

	"go.starlark.net/starlark"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Versions is what a tick is answered from: the active version and the one
// active before it (from the last accepted activation), each nil when none,
// and Seq, the snapshot seq they were read at (LoadVersions); 0 is a
// Versions built by hand (the tests), which no snapshot is checked against.
type Versions struct {
	Active, Prev *store.PolicyVersion
	Seq          int64
}

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
func (e *Engine) Answer(vs Versions, snap store.Snapshot, ro roster.Roster, q Questions) (Answers, []Failure) {
	return e.AnswerContext(context.Background(), vs, snap, ro, q)
}

// AnswerContext is the tick's answer (docs/LAYER-5-POLICY.md, "The tick's
// answer"; tla/Policy.tla Tick): one version answers the whole tick. The
// tick has one deadline, TickTime from the first byte of its input, or
// ctx's when sooner. Any error, refusal or the deadline in a version means
// none of its answers is used and the next is tried; the failures are
// returned, one per failing version, for the policy judgements
// (Judgements). It never stalls: when even the built-in fails,
// Answers.Version is -1 and the tick applies every move that needs no
// answer. A subject the snapshot does not hold is not asked. Every row fact
// a question reads (kind, width, tiers, model, harness) is ro's, by the
// row's name; a row ro does not name is in no question (Judgements reports
// it).
func (e *Engine) AnswerContext(ctx context.Context, vs Versions, snap store.Snapshot, ro roster.Roster, q Questions) (Answers, []Failure) {
	start := e.clock.Now() // the tick's first byte: the deadline and the fallback count from here
	if vs.Seq != 0 && vs.Seq != snap.Seq {
		return Answers{Version: -1}, []Failure{{Version: -1, Label: "tick", Msg: fmt.Sprintf("versions read at seq %d, snapshot at %d: reload", vs.Seq, snap.Seq)}}
	}
	tried := vs.tried(e.builtin)
	if e.proc != nil {
		return e.remote(ctx, start, request{Kind: "tick", Versions: vs, Snap: snap, Roster: ro, Q: q}, tried)
	}
	ctx, cancel := e.deadline(ctx, start)
	defer cancel()
	asks := newIndex(snap, ro).asks(ctx, q)
	if ctx.Err() != nil { // the inputs alone spent the deadline: every version tried fails
		fs := make([]Failure, len(tried))
		for i, src := range tried {
			fs[i] = Failure{Version: src.version, Label: src.label, Line: 1, Msg: overTime() + " building the tick's inputs"}
		}
		return Answers{Version: -1}, fs
	}
	return e.race(ctx, start, tried, asks)
}

// Judgements are the policy judgements a tick opens: one per failing version
// with none open (its group key), named by the snapshot's seq so an id is
// never reused after an answer. A row nova-config does not name is the
// steps' roster judgement (steps.RosterGroup), never one here as well: the
// e2e found two for one coordinator.
func Judgements(snap store.Snapshot, _ roster.Roster, failed []Failure) []store.PutJudgement {
	var out []store.PutJudgement
	for _, f := range failed {
		if f.Version >= 0 && len(OpenJudgements(snap, f.Version)) == 0 { // -1: a refused tick (stale versions), no version's fault
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

// askOf is question q about subject, its answer checked by check and,
// when in domain, set into the tick's Answers by set.
func askOf[T any](q, subject string, args starlark.Tuple, check func(starlark.Value) (T, string), set func(*Answers, T)) ask {
	return ask{q: q, subject: subject, args: args, digest: digestOf(q, args.String()), accept: func(v starlark.Value) (func(*Answers), string) {
		x, msg := check(v)
		return func(a *Answers) { set(a, x) }, msg
	}}
}

func catalogAsk(ctx context.Context) ask {
	return askOf("catalog", "", nil, func(v starlark.Value) (Catalog, string) { return checkCatalog(ctx, v) },
		func(a *Answers, c Catalog) { a.Catalog = c.clone(); a.Catalog.Version = a.Version })
}

// asks are the tick's questions in the order they are answered: the
// argument-free ones, width for every row, then the per-subject ones. The
// validators that loop over what a policy returns check ctx.
func (ix *index) asks(ctx context.Context, q Questions) []ask {
	out := []ask{
		askOf("limits", "", nil, checkLimits, func(a *Answers, l Limits) { a.Limits = l }),
		askOf("timeouts", "", nil, checkTimeouts, func(a *Answers, t Timeouts) { a.Timeouts = t; a.Timeouts.BaseRegate = slices.Clone(t.BaseRegate) }),
		askOf("prices", "", nil, func(v starlark.Value) (map[string]Price, string) { return checkPrices(ctx, v) },
			func(a *Answers, p map[string]Price) { a.Prices = maps.Clone(p) }),
		catalogAsk(ctx),
	}
	for _, r := range ix.rows {
		out = append(out, askOf("width", r.Name, starlark.Tuple{row(r)}, func(v starlark.Value) (Width, string) { return checkWidth(v, r.width) },
			func(a *Answers, w Width) { a.Width[r.Name] = w }))
	}
	for _, id := range q.Order {
		if c, ok := ix.producer(id); ok {
			out = append(out, askOf("order", id, starlark.Tuple{orderCard(c)}, checkOrder, func(a *Answers, o []any) { a.Order[id] = slices.Clone(o) }))
		}
	}
	for _, rq := range q.Rows {
		if c, ok := ix.producer(rq.Card); ok && (rq.Kind == KindWork || rq.Kind == KindRead) {
			out = append(out, ix.rowsAsk(c, rq))
		}
	}
	return append(out, ix.subjectAsks(q)...)
}

// rowsAsk is one rows question, with its joint check: the rows named must
// leave the card as many rows able to take it (a work answer above 0 in
// width) as it needs, one for work and the reads answer for a read (layer 3
// deals a read to that many distinct rows; the names are distinct
// candidates, so never the author), when the candidates could (a configured
// width above 0).
func (ix *index) rowsAsk(c store.Card, rq RowsQuestion) ask {
	set := ix.candidateSet(c, rq.Kind)
	card := ix.card(c)
	return ask{q: "rows", subject: rq.Card + " " + rq.Kind, args: starlark.Tuple{card, starlark.String(rq.Kind), set.list},
		digest: digestOf("rows", card.String()+" "+rq.Kind+" "+set.digest), rq: &rq,
		acceptRows: func(v starlark.Value) ([]string, string) { return checkRows(v, c, set, ix.facts) },
		joint:      func(a *Answers) string { return jointRows(a, rq, set) }}
}

// jointRows is rowsAsk's joint check on the tick's answers a.
func jointRows(a *Answers, rq RowsQuestion, set *candidateSet) string {
	need, have, could, first := 1, 0, 0, ""
	if n, ok := a.Reads[rq.Card]; ok && rq.Kind == KindRead {
		need = n
	}
	for _, n := range a.Rows[rq] {
		have += b2i(a.Width[n].Work > 0)
	}
	for _, r := range set.rows {
		if r.width > 0 {
			could++
			first = cmp.Or(first, r.Name)
		}
	}
	switch need = min(need, could); {
	case have >= need:
		return ""
	case need == 1:
		return refuse("every row named has work 0 in width, while row %s could take it", first)
	}
	return refuse("reads is %d, and %d row(s) named have work above 0 in width, while %d candidate(s) could take a read", a.Reads[rq.Card], have, could)
}

// subjectAsks are reads, verdict, landing and stitch.
func (ix *index) subjectAsks(q Questions) []ask {
	var out []ask
	for _, id := range q.Reads {
		if c, ok := ix.producer(id); ok {
			out = append(out, askOf("reads", id, starlark.Tuple{ix.card(c)}, checkReads, func(a *Answers, n int) { a.Reads[id] = n }))
		}
	}
	for _, id := range q.Verdict {
		if c, ok := ix.producer(id); ok {
			out = append(out, ix.verdictAsk(c))
		}
	}
	for _, lq := range q.Landing {
		if _, ok := ix.streams[lq.Stream]; ok {
			subject := lq.Stream + map[bool]string{true: " " + lq.Rung}[lq.Rung != RungFlowing] // check asks both rungs
			out = append(out, askOf("landing", subject, starlark.Tuple{ix.landingStream(lq)}, checkLanding, func(a *Answers, l Landing) { a.Landing[lq.Stream] = l }))
		}
	}
	for _, s := range q.Stitch {
		if _, ok := ix.streams[s]; ok {
			out = append(out, askOf("stitch", s, starlark.Tuple{ix.stitchStream(s)}, checkStitch, func(a *Answers, b bool) { a.Stitch[s] = b }))
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
	return askOf("verdict", c.ID, starlark.Tuple{ix.card(c), reads}, func(v starlark.Value) (string, string) { return checkVerdict(v, results) },
		func(a *Answers, s string) { a.Verdict[c.ID] = s })
}

func (ix *index) producer(id string) (store.Card, bool) {
	c, ok := ix.snap.Cards[id]
	return c, ok && c.Kind == "producer"
}
