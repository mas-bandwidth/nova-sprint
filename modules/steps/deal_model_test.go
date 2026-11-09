package steps

import (
	"fmt"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What the deal carries and when the tick deals: defects 50, 53, 54, 57 and
// section 64b. Unit: store.Fake, the clock an argument.

// kin is a friend row with its own model in the roster, hooked in.
func (w *world) kin(name, model string, width int, tiers ...string) {
	w.roster.Kin[name] = roster.Kin{Name: name, Model: model, Width: width, Tiers: tiers}
	w.must("row", store.PutRow{Name: name, Session: "hook-" + name, Role: "worker"})
	w.must("seen", store.SetRowState{Row: name, State: "up"})
}

// TestTickDealNamesTheModel: a bench's work consumer is cut with the
// policy's bench model for the card's tier, a friend's with its own model,
// and a read the same way per row (defect 57).
func TestTickDealNamesTheModel(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("b1", 1, "pro")
	s.kin("f1", "m-friend", 1, "pro")
	s.stream("s1")
	s.producer("p1", "s1", "pro", "a/x")
	s.producer("p2", "s1", "pro", "a/y")
	s.run(4)
	got := map[string]string{s.card("p1.w1").Holder: s.card("p1.w1").Model, s.card("p2.w1").Holder: s.card("p2.w1").Model}
	assert.Equal(t, map[string]string{"b1": "test/pro", "f1": "m-friend"}, got)
	writer := s.card("p1.w1").Holder
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.take("p2.w1") // the deal is the take: the other row has room for p1's read once p2 is done
	s.report("p2.w1", "ok", sha(2), "done")
	s.run(4)
	r := s.card("p1.r1.1")
	assert.NotEqual(t, writer, r.Holder, "never the author")
	assert.Equal(t, map[string]string{"b1": "test/pro", "f1": "m-friend"}[r.Holder], r.Model, "a read carries its row's model")
}

// TestTickNoModelNoDeal: a bench serving a tier the policy names no bench
// model for is not dealt to; the card stays ready and one no-model judgement
// per tier says why; a friend with a model still takes it; the judgement
// clears once a model can be named (defect 57).
func TestTickNoModelNoDeal(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	models := benchModels()
	delete(models, "heavy")
	s.ask = scripted{Answers{Models: models}}
	s.row("b1", 2, "heavy")
	s.stream("s1")
	s.producer("p1", "s1", "heavy", "a/x")
	s.run(3)
	assert.Equal(t, "ready", s.state("p1"), "no model, no deal")
	require.Equal(t, []string{"no-model.heavy.1"}, s.openJudgements("no-model"))
	assert.Contains(t, s.snap().Judgements["no-model.heavy.1"].Question, "policy v0 names no bench model for tier heavy (bench rows b1;")
	s.now = s.now.Add(10 * time.Duration(Engine.UndealtAfter) * time.Second)
	s.tick()
	assert.Empty(t, s.openJudgements("undealt"), "no-model says why p1 waits; a row with no model to name has no room for it")
	s.kin("f1", "m-friend", 1, "heavy")
	s.run(3)
	assert.Equal(t, "working", s.state("p1"))
	assert.Equal(t, "m-friend", s.card("p1.w1").Model)
	assert.Empty(t, s.openJudgements("no-model"), "cleared: a row serving heavy can be named a model")
}

// TestTickNoDealWithoutPolicy: while layer 6 says no policy is active (or
// the active one failed), nothing is dealt, read or preempted; one blocking
// no-policy judgement on the coordinator's row says why; resolve still runs;
// once the gate lifts the deal resumes and the judgement clears (defect 53).
func TestTickNoDealWithoutPolicy(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.ask = scripted{Answers{Models: benchModels(), NoDeal: "no policy is active: nova-sprint policy set <file> --reason <why>"}}
	s.row("b1", 1, "pro")
	s.must("coord", store.PutRow{Name: "carol", Session: "hook-carol", Role: "coordinator"})
	s.stream("s1")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(3)
	assert.Equal(t, "ready", s.state("p1"), "resolved, never dealt")
	require.Equal(t, []string{"no-policy.carol.1"}, s.openJudgements("no-policy"))
	j := s.snap().Judgements["no-policy.carol.1"]
	assert.Equal(t, "the run deals nothing: no policy is active: nova-sprint policy set <file> --reason <why>", j.Question)
	assert.True(t, j.Deadline.IsZero(), "blocking: no deadline")
	s.now = s.now.Add(10 * time.Duration(Engine.UndealtAfter) * time.Second)
	s.tick()
	assert.Empty(t, s.openJudgements("undealt"), "the gate says why p1 waits; no undealt beside it")
	s.ask = benchAsk{}
	s.run(3)
	assert.Equal(t, "working", s.state("p1"))
	assert.Empty(t, s.openJudgements("no-policy"), "cleared when the gate lifts")
}

// TestTickResolvesFromState: a waiting producer whose needs were landed by a
// store op (a reconcile's SetCardState, not the land step) is ready on the
// next tick, and so is one in a view built cold from such a snapshot (a
// restore): resolve reads state, never landed events (section 64b).
func TestTickResolvesFromState(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("b1", 4, "pro")
	s.stream("s1")
	s.producer("p1", "s1", "pro", "a/x")
	s.must("add", store.PutCard{ID: "p2", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p2", Paths: []string{"a/y"}, Needs: []string{"p1"}})
	s.must("reconcile", store.SetCardState{Card: "p1", State: "landed"})
	s.tick()
	assert.Equal(t, "ready", s.state("p2"), "a store op's landed resolves its dependant")
	cold := newSim(t)
	cold.row("b1", 4, "pro")
	cold.stream("s1")
	cold.producer("p1", "s1", "pro", "a/x")
	cold.must("add", store.PutCard{ID: "p2", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p2", Paths: []string{"a/y"}, Needs: []string{"p1"}})
	cold.must("reconcile", store.SetCardState{Card: "p1", State: "landed"})
	cold.v = Build(cold.snap(), cold.now)
	cold.cursor = cold.v.Seq
	cold.planTick()
	assert.Equal(t, "ready", cold.state("p2"), "a cold view (restore) resolves it too")
}

// TestTickSameFileCardsBothStart: only a NEEDS orders starts. Two
// producers of one stream that name the same file, with no NEEDS between
// them, are both dealt in the same tick, and no judgement holds either
// back; their landing stitches them.
func TestTickSameFileCardsBothStart(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("b1", 8, "pro")
	s.stream("s1")
	s.producer("e1", "s1", "pro", "internal/x.go")
	s.producer("e2", "s1", "pro", "internal/x.go")
	s.run(3)
	assert.Equal(t, []string{"working", "working"}, []string{s.state("e1"), s.state("e2")}, "a shared file holds nothing")
}

// listRows is a policy whose rows answer names every candidate in name
// order (what the default policy answers for rows at one rank): the set a
// card may go to, never a first-fit order.
type listRows struct{}

func (listRows) Ask(q Questions) Answers {
	a := benchAsk{}.Ask(q)
	a.Version = 1
	a.Rows = map[string][]string{}
	for _, r := range q.Rows {
		for _, c := range r.Candidates {
			a.Rows[r.Card.ID] = append(a.Rows[r.Card.ID], c.Name)
		}
	}
	return a
}

// TestTickBurstSpreadsAcrossAnsweredRows: five cards ready at once over a
// width-2 and a width-4 row policy both allows are dealt in one tick with
// no row taking a second card while another has none; the in-tick tally
// counts, so the burst does not land on the first answered row until it is
// full.
func TestTickBurstSpreadsAcrossAnsweredRows(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.ask = listRows{}
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 4, "pro")
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("p%d", i)
		s.producer(id, "s1", "pro", "a/"+id)
		s.move(s.plan(id, card.Resolve{}))
	}
	s.tick()
	require.Empty(t, s.rejected)
	held := map[string]int{"r1": 0, "r2": 0}
	var order []string
	for _, op := range s.last.Ops {
		for _, w := range op.Writes {
			if p, ok := w.(store.PutCard); ok && p.Kind == "work" {
				for r, n := range held {
					assert.False(t, r != p.Holder && n == 0 && held[p.Holder] > 0, "%s takes a second card while %s has none (deals so far %v)", p.Holder, r, order)
				}
				held[p.Holder]++
				order = append(order, p.Holder)
			}
		}
	}
	assert.Equal(t, map[string]int{"r1": 2, "r2": 3}, held, "five cards in one tick, every one dealt")
}
