package steps

import (
	"slices"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// tick is one tick's state: the view, the inputs, the oracle, and what the
// steps have claimed and planned so far.
type tick struct {
	v   *View
	in  Inputs
	o   *oracle
	lim Limits
	tmo Timeouts

	rowsCache map[string]card.Row
	rowNames  []string              // every row, by name, once
	rfacts    map[string]RowFacts   // rowFacts, once per row per tick
	roster    map[string]RosterRow  // the roster's facts, once per row per tick
	planned   int                   // cards touched by the non-blocker groups planned so far
	candCache map[string][]RowFacts // candidates per tier for work, once per tick
	roomCache map[string]int        // room per row (policy's work plus ahead), once per tick
	rankCache map[string]int        // rank index per row, once per tick
	resolvedP map[string]set        // stream + path -> producers resolved this tick (the overlap tally)
	claimed   set                   // cards and rows an earlier step claimed
	resolved  set                   // producers resolved this tick: started, for the overlap tally
	tally     map[string]int        // consumers dealt per row this tick
	rested    set                   // rows resting after faults (LPM R34): a deal filter
	groups    []group
	edges     []EdgeRequest // land requests, waiting on no op
	judges    []judgement
	pend      set               // kind and about of every judgement in judges (addJudge)
	jidx      judgedIndex       // the view's judgements by kind and about (judged)
	expires   map[string]string // judgement id -> why it expires
}

// Tick plans one tick (docs/LAYER-3-STEPS.md "The tick"; tla/Steps.tla Tick).
// The steps run in the model's order, each skipping the cards an earlier one
// claimed: reports and takes from the inbox, preemption, land results, needs
// (resolve, reached, drops), holds, retire and fail limits, fix, the deal,
// land requests, judgements. Then the groups are ordered, budgeted, packed
// into ops and the rest of the inbox is carried.
func Tick(v *View, in Inputs, ask Asker) Plan {
	t := newTick(v, in)
	t.o = newOracle(ask.Ask(t.questions()), Engine)
	t.lim, t.tmo = t.o.limits(), t.o.timeouts()
	t.rest()
	t.unlisted()
	t.reports()
	t.preempt()
	t.landResults()
	t.needs()
	t.holds()
	t.limits()
	t.fix()
	t.deal()
	t.landRequests()
	t.judgements()
	return t.pack()
}

func newTick(v *View, in Inputs) *tick {
	return &tick{v: v, in: in, claimed: set{}, resolved: set{}, tally: map[string]int{}, rested: set{}, expires: map[string]string{},
		rowNames: sortedKeys(v.Rows), rfacts: map[string]RowFacts{}, roster: map[string]RosterRow{},
		candCache: map[string][]RowFacts{}, roomCache: map[string]int{}, rankCache: map[string]int{}, resolvedP: map[string]set{}}
}

// overBudget says whether the non-blocker groups planned so far touch the
// budget's count of cards: the packer would admit no more, so no more are
// planned (the cost bound, guarantee 11). The steps still tally, judge and
// claim past it, as the model's folds do, so the admitted plan is the same.
func (t *tick) overBudget() bool { return t.planned >= t.lim.Budget }

// free says whether producer p and every consumer of it is unclaimed (the
// model's Free).
func (t *tick) free(p string) bool {
	if _, c := t.claimed[p]; c {
		return false
	}
	for id := range t.v.consOf[p] {
		if _, c := t.claimed[id]; c {
			return false
		}
	}
	return true
}

func (t *tick) unclaimed(id string) bool { _, c := t.claimed[id]; return !c }

// add claims a group's cards and row and keeps it.
func (t *tick) add(g group) {
	for _, c := range g.key.Cards {
		t.claimed[c] = struct{}{}
	}
	if g.key.Row != "" {
		t.claimed[g.key.Row] = struct{}{}
	}
	t.groups = append(t.groups, g)
}

// group plans a group of moves and keeps it; a machine refusal is a defect
// of the step's pre-checks, kept as an engine judgement naming the plan and
// the reason, never an op (RefusedOnlyByRace). Past the budget a non-blocker
// group is not planned (nil): the packer would not admit it.
func (t *tick) group(kind, about string, moves ...move) *group {
	if kind != "pre" && t.overBudget() && !(kind == "start" && rung(t.v.Cards[about]) == "blocker") {
		return nil
	}
	g, r := t.newGroup(kind, about, moves...)
	if r != nil {
		t.engine(about, kind+" of "+about+" refused by the card machine on the view: "+r.Reason)
		return nil
	}
	if kind != "pre" && !(kind == "start" && rung(t.v.Cards[about]) == "blocker") {
		t.planned += g.size()
	}
	t.add(g)
	return &t.groups[len(t.groups)-1]
}

// consume keeps an inbox item with no move (the model's none group).
func (t *tick) consume(item string) {
	t.groups = append(t.groups, group{kind: "none", none: true, key: GroupKey{Kind: "none", Item: item}})
}

// questions gathers every question of the tick with its subjects, from the
// view alone, so the asker is called once (LAYER-3: "a dozen calls whatever
// the size"; here one).
func (t *tick) questions() Questions {
	q := Questions{Budget: AskBudget}
	for _, id := range t.v.Producers("ready") {
		c := t.v.Cards[id]
		if c.Kind != "producer" {
			continue
		}
		q.Order = append(q.Order, t.facts(c))
		q.Rows = append(q.Rows, RowsQuestion{Card: t.facts(c), Kind: "work", Candidates: t.candidates(c, "work")})
	}
	for _, id := range t.v.Producers("review") {
		c := t.v.Cards[id]
		q.Order = append(q.Order, t.facts(c))
		q.Reads = append(q.Reads, t.facts(c))
		if t.owed(c) > 0 {
			q.Rows = append(q.Rows, RowsQuestion{Card: t.facts(c), Kind: "read", Candidates: t.candidates(c, "read")})
		}
		if reads := t.settled(c); reads != nil {
			q.Verdict = append(q.Verdict, VerdictQuestion{Card: t.facts(c), Reads: reads})
		}
	}
	for _, n := range t.rowNames {
		q.Width = append(q.Width, t.rowFacts(t.v.Rows[n]))
	}
	for _, s := range sortedKeys(t.v.Streams) {
		if f := t.streamFacts(s); f.Merging > 0 {
			q.Landing = append(q.Landing, f)
		}
	}
	return q
}

func (t *tick) facts(c store.Card) CardFacts {
	f := CardFacts{ID: c.ID, Stream: c.Stream, Tier: c.Tier, Prose: c.Prose, Priority: rung(c), Score: c.Score, Attempt: c.Attempt, Bounces: c.Bounces}
	if c.State == "review" {
		f.WriterRank = t.rank(c.Author)
	}
	if w, ok := t.v.Cards[workID(c)]; ok {
		f.PriorHead = w.Prior
	}
	return f
}

func (t *tick) rowFacts(r store.Row) RowFacts {
	if f, ok := t.rfacts[r.Name]; ok {
		return f
	}
	f := RowFacts{Name: r.Name, Kind: t.kind(r.Name), Rank: t.rank(r.Name), Width: t.width(r.Name), Flaps: r.Flaps, Tiers: t.tiers(r.Name)}
	for id := range t.v.liveOnRow[r.Name] {
		if t.v.Cards[id].State == "working" {
			f.Working++
		} else {
			f.Ready++
		}
	}
	t.rfacts[r.Name] = f
	return f
}

func (t *tick) streamFacts(s string) StreamFacts {
	f := StreamFacts{Name: s}
	for id := range t.v.byState["merging"] {
		if t.v.Cards[id].Stream == s {
			f.Merging++
		}
	}
	for id := range t.v.byState["review"] {
		if t.v.Cards[id].Stream == s {
			f.Review++
		}
	}
	return f
}

// candidates is the rows policy may name for c: open (up, not held), not
// resting, serving c's tier, and for a read not c's author and not holding
// a live read of c's head. Room is the engine's check at the deal. The work
// list is built once per tier per tick (row claims are made before the
// deal); a read's is filtered from it.
func (t *tick) candidates(c store.Card, kind string) []RowFacts {
	work, ok := t.candCache[c.Tier]
	if !ok {
		for _, n := range t.rowNames {
			r := t.v.Rows[n]
			if _, resting := t.rested[n]; resting || !t.v.Open(r) || !slices.Contains(t.tiers(n), c.Tier) || !t.unclaimed(n) {
				continue
			}
			work = append(work, t.rowFacts(r))
		}
		t.candCache[c.Tier] = work
	}
	if kind != "read" {
		return work
	}
	var out []RowFacts
	for _, f := range work {
		if f.Name != c.Author && !t.readingOn(c, f.Name) {
			out = append(out, f)
		}
	}
	return out
}

// readingOn says whether row holds a live read of c's head.
func (t *tick) readingOn(c store.Card, row string) bool {
	for id := range t.v.liveOnRow[row] {
		if x := t.v.Cards[id]; x.Kind == "read" && x.Of == c.ID && x.Head == c.Head {
			return true
		}
	}
	return false
}

func tierIndex(tier string) int { return slices.Index(store.Tiers, tier) }

// workID is p's current work consumer's id.
func workID(p store.Card) string { return p.ID + ".w" + itoa(p.Attempt) }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
