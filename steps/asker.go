package steps

import (
	"slices"
	"time"
)

// Asker is layer 5 as the steps consume it: one call per tick with every
// question the tick has subjects for (docs/LAYER-5-POLICY.md, "The question
// set"; names and types as that doc has them). An answer the engine cannot
// use (absent, failed, out of its domain, naming a row or card not offered)
// is the default for this tick, with one policy judgement per question and
// version; no answer ever stalls a step or makes it plan an illegal move
// (guarantee 10, PolicyFailsToDefault).
type Asker interface {
	Ask(Questions) Answers
}

// AskBudget is the policy time a tick allows (policy round 3; the measured
// cold tick was 217 ms).
const AskBudget = 500 * time.Millisecond

// Questions is every question of one tick with its subjects. Limits and
// timeouts take no subject and are always asked.
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
	ID, Stream, Tier, Prose, Priority, PriorHead string
	Score, Attempt, Bounces                      int
	WriterRank                                   string
}

// RowFacts is a row as policy sees it: Rank is the highest tier it lists.
type RowFacts struct {
	Name, Kind, Model, Harness, Rank string
	Width, Working, Ready, Flaps     int
	Tiers                            []string
}

// RowsQuestion asks rows(card, kind, candidates): Kind is work or read;
// Candidates are the rows up, not held, not rested, serving the card's tier
// with room under the deal-ahead, the author excluded for a read.
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
	Models   map[string]string // tier -> the model a bench runs (the catalog's bench key); none: no bench deal
	NoDeal   string            // layer 6's gate: why nothing is dealt this tick ("" deals; model.go)
}

// WidthAnswer is width(row): how many the row works at once, at most its
// width. There is no queue ahead of it: a dealt card is in the row's hand
// (defect 52), so the deal fills a row to Work and no further; policy's
// "ahead" is read by no step since.
type WidthAnswer struct{ Work int }

// LandingAnswer is landing(stream): backed up (rung 2) at BackedUp merging
// heads waiting, flowing again at Flowing; CIWait the seconds a gated sha
// may have no CI at all before it is a judgement (0: the engine's).
type LandingAnswer struct{ BackedUp, Flowing, CIWait int }

// Limits is limits(): the counts. Budget (cards touched per tick) and Pack
// (groups per op) are layer 3's keys in the same dict (LAYER-3, "Policy
// questions and their defaults").
type Limits struct {
	Bounces, Faults, SameFinding, ReportsPerTick, RouteRestWindow, RouteRestAfter int
	Budget, Pack                                                                  int
}

// Timeouts is timeouts(): seconds of running time, each 1..86400. ReadyNag
// is the seconds after a deal from which a row is nagged each tick for a
// card in hand it has not taken; ReadyTake the seconds after which such a
// card is withdrawn and dealt to another row with room.
type Timeouts struct {
	RowSilentGone, RowSilentWithdraw, LandingStall, Judgement, RouteRestFor, ReadyNag, ReadyTake int
	BaseRegate                                                                                   []int
}

// Default answers nothing: the engine's defaults answer the tick.
type Default struct{}

// Ask answers nothing.
func (Default) Ask(Questions) Answers { return Answers{} }

// Defaults are the engine's own answers (LAYER-5's default policy; every
// number marked there). EscalateAfter and FrontierAfter are the bounces at
// which a work consumer is dealt one rank up and to the top rank (guesses,
// LAYER-3 "Escalation"); FlapLimit is the flaps that open a flap judgement
// (LAYER-3 "Retire and fail limits", 3, unmeasured). The seconds after a
// deal at which an unacknowledged card is nudged each tick and withdrawn are
// policy's timeouts ready_nag and ready_take (LAYER-3 "In hand"; the human's
// numbers, 2026-11-07), default.star's 1 and 180; Engine's are those a
// tick with no usable timeouts() answer runs on, as for every timeout.
// UndealtAfter is the seconds a
// producer may stay ready with a serving row with room before it is judged
// (more than one one-second tick; LAYER-3 "Undealt").
type Defaults struct {
	EscalateAfter, FrontierAfter int
	UndealtAfter                 int
	Reads                        int
	FlapLimit                    int
	IdleFloor, IdleCeiling       float64
	LandTarget                   float64
	LandSample                   int
	Limits                       Limits
	Timeouts                     Timeouts
	Landing                      LandingAnswer
}

// Engine is the defaults as the designs give them. Budget 250 and Pack 50
// are measured (the load test, vision 2026-11-07: a tick of 250 cards at
// 5,000 producers is about 265 ms p50 as 5 ops of 50; see the Built
// section of docs/LAYER-3-STEPS.md); the rest are layer 5's defaults or
// guesses, marked there.
var Engine = Defaults{
	EscalateAfter: 2, FrontierAfter: 4, UndealtAfter: 2, Reads: 1, FlapLimit: 3,
	IdleFloor: 0.5, IdleCeiling: 0.75, LandTarget: 0.9, LandSample: 20,
	Limits:   Limits{Bounces: 6, Faults: 3, SameFinding: 2, ReportsPerTick: 1000, RouteRestWindow: 10, RouteRestAfter: 3, Budget: 250, Pack: 50},
	Timeouts: Timeouts{RowSilentGone: 600, RowSilentWithdraw: 1800, LandingStall: 60, Judgement: 3600, RouteRestFor: 1800, ReadyNag: 1, ReadyTake: 180, BaseRegate: []int{120, 300}},
	Landing:  LandingAnswer{BackedUp: 2, Flowing: 1, CIWait: 600},
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

// rows is rows(card, kind, candidates): the set of candidates the card may go
// to (the deal picks among them by the engine's ranking in deal.go, so a burst
// spreads; preemption still takes the first), or nil for the default.
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
// said so. Default: any fix read means fix.
func (o *oracle) verdict(q VerdictQuestion) string {
	has := func(res string) bool {
		return slices.ContainsFunc(q.Reads, func(r ReadFacts) bool { return r.Result == res })
	}
	v, ok := o.a.Verdict[q.Card.ID]
	if o.usable("verdict", ok) {
		if (v == "yes" && has("ok")) || (v == "fix" && has("fix")) || (v == "retry" && !has("ok") && !has("fix")) {
			return v
		}
		o.refuse("verdict")
	}
	// Default (dogfood 2026-11-07, defect 58): yes needs an ok read, fix a fix
	// read; every read faulted means one more read, never a yes.
	if has("fix") {
		return "fix"
	}
	if has("ok") {
		return "yes"
	}
	return "retry"
}

// width is width(row): work at most the width. Default: work = width.
func (o *oracle) width(r RowFacts) WidthAnswer {
	w, ok := o.a.Width[r.Name]
	if o.usable("width", ok) {
		if w.Work >= 0 && w.Work <= r.Width {
			return w
		}
		o.refuse("width")
	}
	return WidthAnswer{Work: r.Width}
}

// landing is landing(stream): 0 <= flowing < backed_up <= 1000, a CI wait
// of at most a day.
func (o *oracle) landing(s StreamFacts) LandingAnswer {
	l, ok := o.a.Landing[s.Name]
	if o.usable("landing", ok) {
		if l.Flowing >= 0 && l.Flowing < l.BackedUp && l.BackedUp <= 1000 && l.CIWait >= 0 && l.CIWait <= 86400 {
			return l
		}
		o.refuse("landing")
	}
	return o.d.Landing
}

// limits is limits(): every count in its range, else the defaults whole.
func (o *oracle) limits() Limits {
	l := o.a.Limits
	if o.usable("limits", l != nil) {
		if limitsOK(*l) {
			return *l
		}
		o.refuse("limits")
	}
	return o.d.Limits
}

func limitsOK(l Limits) bool {
	in := func(n, lo, hi int) bool { return n >= lo && n <= hi }
	return in(l.Bounces, 1, 100) && in(l.Faults, 1, 100) && in(l.SameFinding, 1, 10) && in(l.ReportsPerTick, 1, 10000) &&
		in(l.RouteRestWindow, 1, 100) && in(l.RouteRestAfter, 1, l.RouteRestWindow) && in(l.Budget, 1, 10000) && in(l.Pack, 1, 10000)
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
	ok := day(t.RowSilentGone) && day(t.RowSilentWithdraw) && day(t.LandingStall) && day(t.Judgement) && day(t.RouteRestFor) && day(t.ReadyNag) && day(t.ReadyTake) &&
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
