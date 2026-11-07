package card

import (
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// taken is p1 working on r1 with p1.w1 in hand and not yet reported.
func taken(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.move("p1", Start{Row: "r1", Branch: "b", Base: base})
	return w
}

func (w *world) reportPlan(id string, r Report) (Plan, *Refusal) {
	w.t.Helper()
	s := w.snap()
	c := fromStore(s.Cards[id])
	p := fromStore(s.Cards[c.Of])
	var cons []Card
	all := map[string]Card{}
	for _, card := range s.Cards {
		all[card.ID] = fromStore(card)
		if card.Of == p.ID {
			cons = append(cons, fromStore(card))
		}
	}
	slices.SortFunc(cons, func(a, b Card) int { return strings.Compare(a.ID, b.ID) })
	return ReportPlan(c, p, cons, w.rows(), StreamFromStore(s.Streams[p.Stream]), all, r)
}

func TestReportPlanThreeResults(t *testing.T) {
	t.Parallel()
	for _, result := range []string{"ok", "fail", "fault"} {
		t.Run(result, func(t *testing.T) {
			w := taken(t)
			c, p := w.card("p1.w1"), w.card("p1")
			plan, r := w.reportPlan("p1.w1", Report{Row: "r1", Result: result, Head: h2, Reason: "because", Cost: 7, Priced: true})
			require.Nil(t, r)
			fenced := map[string]int{}
			for _, e := range plan.Expect {
				if g, ok := e.(store.CardGen); ok {
					fenced[g.Card] = g.Gen
				}
			}
			assert.Equal(t, map[string]int{"p1": p.Gen, "p1.w1": c.Gen}, fenced)
			w.op("report", plan)
			assert.Equal(t, Done, w.card("p1.w1").State)
			assert.Equal(t, result, w.card("p1.w1").Result)
			switch result {
			case "ok":
				assert.Equal(t, Review, w.card("p1").State)
			default:
				assert.Equal(t, Ready, w.card("p1").State)
			}
		})
	}
}

func TestReportPlanEqualsTwoApplies(t *testing.T) {
	t.Parallel()
	for _, result := range []string{"ok", "fail", "fault"} {
		t.Run(result, func(t *testing.T) {
			rep := Report{Row: "r1", Result: result, Head: h2, Reason: "because", Cost: 7, Priced: true}
			one := taken(t)
			plan, r := one.reportPlan("p1.w1", rep)
			require.Nil(t, r)
			one.op("report", plan)
			two := taken(t)
			two.consume("p1.w1", rep)
			follow := map[string]Event{"ok": Finished{Consumer: "p1.w1"}, "fail": Broken{Consumer: "p1.w1"}, "fault": Faulted{Consumer: "p1.w1"}}[result]
			two.move("p1", follow)
			assert.Equal(t, two.snap().Cards, one.snap().Cards)
		})
	}
}

func TestReportPlanStaleHead(t *testing.T) {
	t.Parallel()
	w := taken(t)
	_, r := w.reportPlan("p1.w1", Report{Row: "r1", Result: "ok", Head: base})
	refusal{r, "head " + base + " is the base; nothing was done"}.check(t)
	assert.Equal(t, []string{Working, Working}, []string{w.card("p1").State, w.card("p1.w1").State})
}

func TestReportPlanReadLeavesTheProducer(t *testing.T) {
	t.Parallel()
	w := newWorld(t, producer("p1"))
	w.move("p1", Resolve{})
	w.work("p1", "r1", "ok", h2, "done")
	w.move("p1", Finished{Consumer: "p1.w1"})
	w.move("p1", AskRead{Row: "r2"})
	var read string
	for id, c := range w.snap().Cards {
		if c.Kind == "read" {
			read = id
		}
	}
	require.NotEmpty(t, read)
	before := w.card("p1")
	plan, r := w.reportPlan(read, Report{Row: "r2", Result: "fix", Head: before.Head, Reason: "a hole"})
	require.Nil(t, r)
	for _, wr := range plan.Writes {
		switch v := wr.(type) {
		case store.SetCardState:
			assert.NotEqual(t, "p1", v.Card)
		case store.SetCardResult:
			assert.NotEqual(t, "p1", v.Card)
		}
	}
	w.op("read-report", plan)
	assert.Equal(t, before, w.card("p1"))
	assert.Equal(t, []any{Done, "fix"}, []any{w.card(read).State, w.card(read).Result})
}
