package steps

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
)

// scripted is an Asker that returns fixed answers.
type scripted struct{ a Answers }

func (s scripted) Ask(Questions) Answers { return s.a }

func rowsQ(cands ...string) RowsQuestion {
	q := RowsQuestion{Card: CardFacts{ID: "p1", Tier: "pro"}, Kind: "work"}
	for _, c := range cands {
		q.Candidates = append(q.Candidates, RowFacts{Name: c, Width: 2, Rank: "pro", Tiers: []string{"pro"}})
	}
	return q
}

// TestAskerAbsent: no policy at all is every default and no judgement
// (list item 3: absent).
func TestAskerAbsent(t *testing.T) {
	o := newOracle(Default{}.Ask(Questions{}), Engine)
	assert.Equal(t, []any{1, 7}, o.order(CardFacts{ID: "p1", Score: 7}))
	assert.Equal(t, []any{0, 7}, o.order(CardFacts{ID: "p1", Score: 7, PriorHead: sha(1)}), "a fix attempt first")
	assert.Nil(t, o.rows(rowsQ("r1")))
	assert.Equal(t, 1, o.reads(CardFacts{ID: "p1"}))
	assert.Equal(t, "fix", o.verdict(VerdictQuestion{Reads: []ReadFacts{{Result: "ok"}, {Result: "fix"}}}), "any fix read means fix")
	assert.Equal(t, "yes", o.verdict(VerdictQuestion{Reads: []ReadFacts{{Result: "ok"}}}))
	assert.Equal(t, WidthAnswer{2}, o.width(RowFacts{Name: "r1", Width: 2}))
	assert.Equal(t, Engine.Landing, o.landing(StreamFacts{Name: "s1"}))
	assert.Equal(t, Engine.Limits, o.limits())
	assert.Equal(t, Engine.Timeouts, o.timeouts())
	assert.Empty(t, o.missed(), "no policy is no judgement")
}

// TestAskerErroring: a question the version failed is the default for the
// tick with one judgement; the other questions are still read.
func TestAskerErroring(t *testing.T) {
	a := Answers{Version: 3, Failed: map[string]string{"rows": "division by zero"}, Reads: map[string]int{"p1": 2}}
	o := newOracle(a, Engine)
	assert.Nil(t, o.rows(rowsQ("r1", "r2")))
	assert.Nil(t, o.rows(rowsQ("r1")), "asked twice, judged once")
	assert.Equal(t, 2, o.reads(CardFacts{ID: "p1"}))
	assert.Equal(t, 1, o.reads(CardFacts{ID: "p9"}), "a subject the version did not answer is a default")
	assert.Equal(t, []string{"reads", "rows"}, o.missed())
}

// TestAskerSlow: the budget spent is every default and one judgement; no
// answer of that version is read, so a half-finished batch never mixes.
func TestAskerSlow(t *testing.T) {
	a := Answers{Version: 2, Failed: map[string]string{"*": "the 500ms of policy time was spent"}, Reads: map[string]int{"p1": 3}}
	o := newOracle(a, Engine)
	assert.Equal(t, 1, o.reads(CardFacts{ID: "p1"}))
	assert.Equal(t, Engine.Limits, o.limits())
	assert.Equal(t, []string{"*"}, o.missed())
}

// TestAskerOutOfRange: every answer outside its domain is the default and a
// judgement, and the valid ones stand.
func TestAskerOutOfRange(t *testing.T) {
	bad := Limits{Bounces: 0}
	tmo := Timeouts{RowSilentGone: 600, RowSilentWithdraw: 10, LandingStall: 60, Judgement: 3600, RouteRestFor: 1800, BaseRegate: []int{120}}
	a := Answers{Version: 5,
		Order:    map[string][]any{"p1": {1, 2, 3, 4, 5}, "p2": {1.5}, "p3": {"a", 2}},
		Rows:     map[string][]string{"p1": {"r9"}, "p2": {}, "p3": {"r2", "r1"}},
		Reads:    map[string]int{"p1": 9, "p2": 5},
		Verdict:  map[string]string{"p1": "yes", "p2": "maybe", "p3": "fix"},
		Width:    map[string]WidthAnswer{"r1": {Work: 3}, "r2": {Work: 1}, "r3": {Work: 2}},
		Landing:  map[string]LandingAnswer{"s1": {BackedUp: 2, Flowing: 2}, "s2": {BackedUp: 3, Flowing: 0}},
		Limits:   &bad,
		Timeouts: &tmo,
	}
	o := newOracle(a, Engine)
	assert.Equal(t, []any{1, 0}, o.order(CardFacts{ID: "p1"}), "five is too long")
	assert.Equal(t, []any{1, 0}, o.order(CardFacts{ID: "p2"}), "a float is neither int nor string")
	assert.Equal(t, []any{"a", 2}, o.order(CardFacts{ID: "p3"}))
	assert.Nil(t, o.rows(RowsQuestion{Card: CardFacts{ID: "p1"}, Candidates: rowsQ("r1").Candidates}), "a row not offered")
	assert.Nil(t, o.rows(RowsQuestion{Card: CardFacts{ID: "p2"}, Candidates: rowsQ("r1").Candidates}), "empty while candidates exist")
	assert.Equal(t, []string{"r2", "r1"}, o.rows(RowsQuestion{Card: CardFacts{ID: "p3"}, Candidates: rowsQ("r1", "r2").Candidates}))
	assert.Equal(t, 1, o.reads(CardFacts{ID: "p1"}))
	assert.Equal(t, 5, o.reads(CardFacts{ID: "p2"}))
	fixOnly := []ReadFacts{{Result: "fix", Finding: "x"}}
	assert.Equal(t, "fix", o.verdict(VerdictQuestion{Card: CardFacts{ID: "p1"}, Reads: fixOnly}), "yes with no ok read is refused")
	assert.Equal(t, "fix", o.verdict(VerdictQuestion{Card: CardFacts{ID: "p2"}, Reads: fixOnly}))
	assert.Equal(t, "fix", o.verdict(VerdictQuestion{Card: CardFacts{ID: "p3"}, Reads: fixOnly}))
	assert.Equal(t, WidthAnswer{2}, o.width(RowFacts{Name: "r1", Width: 2}), "work over the width")
	assert.Equal(t, WidthAnswer{1}, o.width(RowFacts{Name: "r2", Width: 2}))
	assert.Equal(t, WidthAnswer{2}, o.width(RowFacts{Name: "r3", Width: 2}))
	assert.Equal(t, Engine.Landing, o.landing(StreamFacts{Name: "s1"}), "flowing must be below backed up")
	assert.Equal(t, LandingAnswer{BackedUp: 3}, o.landing(StreamFacts{Name: "s2"}))
	assert.Equal(t, Engine.Limits, o.limits())
	assert.Equal(t, Engine.Timeouts, o.timeouts(), "withdraw below gone")
	assert.Equal(t, []string{"landing", "limits", "order", "reads", "rows", "timeouts", "verdict", "width"}, o.missed())
}

// TestAskerInDomain: valid answers of a version are used as given.
func TestAskerInDomain(t *testing.T) {
	l := Limits{Bounces: 3, Faults: 2, SameFinding: 1, ReportsPerTick: 100, RouteRestWindow: 5, RouteRestAfter: 2, Budget: 40, Pack: 5}
	tm := Timeouts{RowSilentGone: 60, RowSilentWithdraw: 60, LandingStall: 30, Judgement: 86400, RouteRestFor: 1, BaseRegate: []int{1, 2, 3}}
	o := newOracle(Answers{Version: 1, Limits: &l, Timeouts: &tm}, Engine)
	assert.Equal(t, l, o.limits())
	assert.Equal(t, tm, o.timeouts())
	assert.Empty(t, o.missed())
}

// benchAsk is the engine's defaults with a bench model for every tier, as an
// active policy's catalog names them (defect 57: a bench deal names its
// model, and a version with none deals to no bench).
type benchAsk struct{}

func (benchAsk) Ask(Questions) Answers { return Answers{Models: benchModels()} }

// benchModels is a model for every tier, provider/model, as the catalog's
// bench key has them.
func benchModels() map[string]string {
	out := map[string]string{}
	for _, t := range store.Tiers {
		out[t] = "test/" + t
	}
	return out
}

// TestOracleRefusalsSayWhy: every answer the engine cannot use, under every
// question, is a miss with its text (directive 58): failed with the
// version's text, absent for its subject, or out of its domain, the verdict
// "retry" included. A policy judgement with no text tells nobody anything.
func TestOracleRefusalsSayWhy(t *testing.T) {
	bad := Limits{Bounces: 0}
	tmo := Timeouts{RowSilentGone: 0}
	failed := map[string]string{}
	for _, q := range []string{"order", "rows", "reads", "verdict", "width", "landing", "limits", "timeouts"} {
		failed[q] = "line 9: " + q + " raised"
	}
	reads := []ReadFacts{{Result: "ok"}}
	ask := func(a Answers) *oracle {
		o := newOracle(a, Engine)
		o.order(CardFacts{ID: "p1"})
		o.rows(RowsQuestion{Card: CardFacts{ID: "p1"}, Candidates: rowsQ("r1").Candidates})
		o.reads(CardFacts{ID: "p1"})
		o.verdict(VerdictQuestion{Card: CardFacts{ID: "p1"}, Reads: reads})
		o.width(RowFacts{Name: "r1", Width: 2})
		o.landing(StreamFacts{Name: "s1"})
		o.limits()
		o.timeouts()
		return o
	}
	for name, a := range map[string]Answers{
		"failed": {Version: 4, Failed: failed},
		"absent": {Version: 4, Limits: &bad, Timeouts: &tmo},
		"domain": {Version: 4, Order: map[string][]any{"p1": {1.5}}, Rows: map[string][]string{"p1": {"r9"}}, Reads: map[string]int{"p1": 9},
			Verdict: map[string]string{"p1": "retry"}, Width: map[string]WidthAnswer{"r1": {Work: 3}},
			Landing: map[string]LandingAnswer{"s1": {BackedUp: 1, Flowing: 1}}, Limits: &bad, Timeouts: &tmo},
	} {
		o := ask(a)
		assert.Len(t, o.missed(), 8, "%s: every question missed", name)
		for _, q := range o.missed() {
			assert.NotEmpty(t, o.why(q), "%s: question %s refused with no text", name, q)
		}
	}
	o := ask(Answers{Version: 4, Verdict: map[string]string{"p1": "retry"}})
	assert.Contains(t, o.why("verdict"), `verdict(p1) is "retry": yes needs an ok read and fix a fix read`)
}
