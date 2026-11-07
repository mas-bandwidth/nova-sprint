package steps

import (
	"maps"
	"slices"
	"time"
)

// Asker is layer 5 as the steps consume it: layer 5's question set, names
// and types as docs/LAYER-5-POLICY.md has them. A tick asks twice, each
// question once: first the argument-free questions (limits, timeouts) and
// reads, whose answers shape the subjects of the rest (the rest bound, the
// rows a silence will take down, the reads owed); then order, rows,
// verdict, width and landing with every subject. An answer the engine
// cannot use (absent, failed, out of its domain, naming a row or card not
// offered) is the default for this tick, with one policy judgement per
// question and version; no answer ever stalls a step or makes it plan an
// illegal move (guarantee 10, PolicyFailsToDefault).
type Asker interface {
	Ask(Questions) Answers
}

// AskBudget is the policy time a tick allows (policy round 3; the measured
// cold tick was 217 ms).
const AskBudget = 500 * time.Millisecond

// Questions is the questions of one call with their subjects. Limits and
// timeouts take no subject and are answered on every call.
type Questions struct {
	Budget  time.Duration
	Order   []CardFacts
	Rows    []RowsQuestion
	Reads   []CardFacts
	Verdict []VerdictQuestion
	Width   []RowFacts
	Landing []StreamFacts
}

// CardFacts is a producer as policy sees it. WriterRank is set for a read:
// the rank of the row that wrote the head.
type CardFacts struct {
	ID, Stream, Tier, Priority, PriorHead string
	Score, Attempt, Bounces               int
	WriterRank                            string
}

// RowFacts is a row as policy sees it: Rank is the highest tier it lists,
// Model the roster's label (a worker's harness, a kin's model).
type RowFacts struct {
	Name, Kind, Model, Rank      string
	Width, Working, Ready, Flaps int
	Tiers                        []string
}

// RowsQuestion asks rows(card, kind, candidates): Kind is work or read;
// Candidates are the rows up, not held, hooked, not resting, not about to
// go down, serving the card's tier, the author excluded for a read.
type RowsQuestion struct {
	Card       CardFacts
	Kind       string
	Candidates []RowFacts
}

// VerdictQuestion asks verdict(card, reads) over the done reads of the
// card's current review and head.
type VerdictQuestion struct {
	Card  CardFacts
	Reads []ReadFacts
}

// ReadFacts is one done read: the row, its rank, ok or fix, and the finding.
type ReadFacts struct{ Row, Rank, Result, Finding string }

// StreamFacts is a stream as landing(stream) sees it.
type StreamFacts struct {
	Name            string
	Merging, Review int
}

// Answers is one version's answers, by subject. A question the version failed
// as a whole is named in Failed with why; a subject absent from its map is a
// failure for that subject. Version 0 is no policy: every default, no
// judgement.
type Answers struct {
	Version  int
	Order    map[string][]any // a tuple of up to 4 ints or strings
	Rows     map[string][]string
	Reads    map[string]int
	Verdict  map[string]string
	Width    map[string]WidthAnswer
	Landing  map[string]LandingAnswer
	Limits   *Limits
	Timeouts *Timeouts
	Failed   map[string]string // question -> why; "*" when the budget was spent
}

// WidthAnswer is width(row): how many the row works (at most its width) and
// how many it queues ahead (at most 4 x width). Ahead is the deal's room
// beyond the width: policy's number, the width by default (the ruling of
// round 2; limits.ahead did not land in policy round 5, so it stays here).
type WidthAnswer struct{ Work, Ahead int }

// LandingAnswer is landing(stream): backed up at BackedUp merging, flowing
// again at Flowing.
type LandingAnswer struct{ BackedUp, Flowing int }

// Limits is limits() as policy round 5 has it: the counts and layer 3's
// steering numbers. ReportsPerTick is the tick's budget of cards touched.
// BaseRedMeetings and IdleCeiling are asked too (the intent audit's row 8)
// and are optional until policy carries them: 0 is the default (3; half
// again the floor).
type Limits struct {
	Bounces, Faults, SameFinding, ReportsPerTick, RouteRestWindow, RouteRestAfter int
	EscalateAfter, FrontierAfter, FlapLimit                                       int
	IdleFloor, LandTarget                                                         float64
	BaseRedMeetings                                                               int
	IdleCeiling                                                                   float64
}

// Timeouts is timeouts(): seconds of running time, each 1..86400.
type Timeouts struct {
	RowSilentGone, RowSilentWithdraw, LandingStall, Judgement, RouteRestFor int
	BaseRegate                                                              []int
}

// Default answers nothing: the engine's defaults answer the tick.
type Default struct{}

// Ask answers nothing.
func (Default) Ask(Questions) Answers { return Answers{} }

// Defaults are the engine's own answers where policy gives none. Pack is
// the groups per op (measured, the load test); LandSample the landings a
// land rate is judged over (LESSONS 3.21: 20); Limits, Timeouts and Landing
// are layer 5's default policy's numbers, each marked there.
type Defaults struct {
	Reads      int
	Pack       int
	LandSample int
	Limits     Limits
	Timeouts   Timeouts
	Landing    LandingAnswer
}

// Engine is the defaults as the designs give them.
var Engine = Defaults{
	Reads: 1, Pack: 50, LandSample: 20,
	Limits: Limits{Bounces: 6, Faults: 3, SameFinding: 2, ReportsPerTick: 250, RouteRestWindow: 10, RouteRestAfter: 3,
		EscalateAfter: 2, FrontierAfter: 4, FlapLimit: 3, IdleFloor: 0.5, LandTarget: 0.9, BaseRedMeetings: 3, IdleCeiling: 0.75},
	Timeouts: Timeouts{RowSilentGone: 600, RowSilentWithdraw: 1800, LandingStall: 60, Judgement: 3600, RouteRestFor: 1800, BaseRegate: []int{120, 300}},
	Landing:  LandingAnswer{BackedUp: 4, Flowing: 1},
}

// oracle is one tick's answers: the version's where usable, the defaults
// where not, and the questions missed (one policy judgement each).
type oracle struct {
	a    Answers
	d    Defaults
	miss set
}

func newOracle(a Answers, d Defaults) *oracle {
	o := &oracle{a: a, d: d, miss: set{}}
	if a.Failed["*"] != "" {
		o.miss["*"] = struct{}{}
	}
	return o
}

// merge adds the second call's answers to the first's (the version is the
// first call's; a second call under another version is the budget spent).
func (o *oracle) merge(b Answers) {
	if b.Version != o.a.Version {
		o.miss["*"] = struct{}{}
		return
	}
	if o.a.Order == nil {
		o.a.Order = map[string][]any{}
	}
	maps.Copy(o.a.Order, b.Order)
	if o.a.Rows == nil {
		o.a.Rows = map[string][]string{}
	}
	maps.Copy(o.a.Rows, b.Rows)
	if o.a.Verdict == nil {
		o.a.Verdict = map[string]string{}
	}
	maps.Copy(o.a.Verdict, b.Verdict)
	if o.a.Width == nil {
		o.a.Width = map[string]WidthAnswer{}
	}
	maps.Copy(o.a.Width, b.Width)
	if o.a.Landing == nil {
		o.a.Landing = map[string]LandingAnswer{}
	}
	maps.Copy(o.a.Landing, b.Landing)
	if o.a.Failed == nil {
		o.a.Failed = map[string]string{}
	}
	maps.Copy(o.a.Failed, b.Failed)
	if b.Failed["*"] != "" {
		o.miss["*"] = struct{}{}
	}
}

// usable says whether question q's answer for one subject may be read:
// present, and the question not failed. A miss is recorded only under a
// policy version (version 0 is no policy, every default by design).
func (o *oracle) usable(q string, present bool) bool {
	if o.a.Version == 0 {
		return false
	}
	if _, spent := o.miss["*"]; spent {
		return false
	}
	if o.a.Failed[q] != "" || !present {
		o.miss[q] = struct{}{}
		return false
	}
	return true
}

func (o *oracle) refuse(q string) { o.miss[q] = struct{}{} }

// order is order(card): the tuple, sorted by (rung, tuple, id) by the deal.
// Default: fix attempts first (a prior head), then score.
func (o *oracle) order(c CardFacts) []any {
	t, ok := o.a.Order[c.ID]
	if o.usable("order", ok) && orderOK(t) {
		return t
	} else if ok {
		o.refuse("order")
	}
	fix := 1
	if c.PriorHead != "" {
		fix = 0
	}
	return []any{fix, c.Score}
}

func orderOK(t []any) bool {
	if len(t) > 4 {
		return false
	}
	for _, x := range t {
		switch x.(type) {
		case int, string:
		default:
			return false
		}
	}
	return true
}

// rows is rows(card, kind, candidates): the answered candidates in order,
// or nil for the default (the engine's ranking in deal.go).
func (o *oracle) rows(q RowsQuestion) []string {
	ans, ok := o.a.Rows[q.Card.ID]
	if !o.usable("rows", ok) {
		return nil
	}
	if len(ans) == 0 && len(q.Candidates) > 0 {
		o.refuse("rows")
		return nil
	}
	for _, r := range ans {
		if !slices.ContainsFunc(q.Candidates, func(c RowFacts) bool { return c.Name == r }) {
			o.refuse("rows")
			return nil
		}
	}
	return ans
}

// reads is reads(card): 1..5.
func (o *oracle) reads(c CardFacts) int {
	n, ok := o.a.Reads[c.ID]
	if o.usable("reads", ok) {
		if n >= 1 && n <= 5 {
			return n
		}
		o.refuse("reads")
	}
	return o.d.Reads
}

// verdict is verdict(card, reads): yes or fix, each needing a read that
// said so, or retry (one more read is asked). Default: any fix read means
// fix; an ok read yes; none of either (every read faulted) retry.
func (o *oracle) verdict(q VerdictQuestion) string {
	has := func(res string) bool {
		return slices.ContainsFunc(q.Reads, func(r ReadFacts) bool { return r.Result == res })
	}
	v, ok := o.a.Verdict[q.Card.ID]
	if o.usable("verdict", ok) {
		if (v == "yes" && has("ok")) || (v == "fix" && has("fix")) || v == "retry" {
			return v
		}
		o.refuse("verdict")
	}
	switch {
	case has("fix"):
		return "fix"
	case has("ok"):
		return "yes"
	}
	return "retry"
}

// width is width(row): work at most the width, ahead at most 4 x width.
// Default: work = width, ahead = width.
func (o *oracle) width(r RowFacts) WidthAnswer {
	w, ok := o.a.Width[r.Name]
	if o.usable("width", ok) {
		if w.Work >= 0 && w.Work <= r.Width && w.Ahead >= 0 && w.Ahead <= 4*r.Width {
			return w
		}
		o.refuse("width")
	}
	return WidthAnswer{Work: r.Width, Ahead: r.Width}
}

// landing is landing(stream): 0 <= flowing < backed_up <= 1000.
func (o *oracle) landing(s StreamFacts) LandingAnswer {
	l, ok := o.a.Landing[s.Name]
	if o.usable("landing", ok) {
		if l.Flowing >= 0 && l.Flowing < l.BackedUp && l.BackedUp <= 1000 {
			return l
		}
		o.refuse("landing")
	}
	return o.d.Landing
}

// limits is limits(): every number in its range, else the defaults whole;
// an optional key left 0 is its default.
func (o *oracle) limits() Limits {
	l := o.a.Limits
	if o.usable("limits", l != nil) {
		if limitsOK(*l) {
			got := *l
			if got.BaseRedMeetings == 0 {
				got.BaseRedMeetings = o.d.Limits.BaseRedMeetings
			}
			return got
		}
		o.refuse("limits")
	}
	return o.d.Limits
}

func limitsOK(l Limits) bool {
	in := func(n, lo, hi int) bool { return n >= lo && n <= hi }
	frac := func(f float64) bool { return f > 0 && f <= 1 }
	return in(l.Bounces, 1, 100) && in(l.Faults, 1, 100) && in(l.SameFinding, 1, 10) && in(l.ReportsPerTick, 1, 10000) &&
		in(l.RouteRestWindow, 1, 100) && in(l.RouteRestAfter, 1, l.RouteRestWindow) &&
		in(l.EscalateAfter, 1, 100) && in(l.FrontierAfter, l.EscalateAfter, 100) && in(l.FlapLimit, 1, 100) &&
		frac(l.IdleFloor) && frac(l.LandTarget) && in(l.BaseRedMeetings, 0, 100) && (l.IdleCeiling == 0 || frac(l.IdleCeiling) && l.IdleCeiling >= l.IdleFloor)
}

// timeouts is timeouts(): every bound 1..86400 s, withdraw at or above gone,
// regate a rising list; else the defaults whole. No wait is ever unbounded.
func (o *oracle) timeouts() Timeouts {
	t := o.a.Timeouts
	if o.usable("timeouts", t != nil) {
		if timeoutsOK(*t) {
			return *t
		}
		o.refuse("timeouts")
	}
	return o.d.Timeouts
}

func timeoutsOK(t Timeouts) bool {
	day := func(n int) bool { return n >= 1 && n <= 86400 }
	ok := day(t.RowSilentGone) && day(t.RowSilentWithdraw) && day(t.LandingStall) && day(t.Judgement) && day(t.RouteRestFor) &&
		t.RowSilentWithdraw >= t.RowSilentGone && len(t.BaseRegate) > 0
	for i, n := range t.BaseRegate {
		ok = ok && day(n) && (i == 0 || n > t.BaseRegate[i-1])
	}
	return ok
}

// missed is the questions whose answer failed, in name order: one policy
// judgement each ("*" is the budget, spent for every question).
func (o *oracle) missed() []string { return sorted(o.miss) }

func seconds(n int) time.Duration { return time.Duration(n) * time.Second }
