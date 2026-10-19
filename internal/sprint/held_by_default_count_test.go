package sprint_test

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/require"
)

// where counts the cards the server's safe default holds apart from the judgment
// that parked them: DecisionsOwed's HeldByDefault is one per live server-default
// hold (DefaultHeldDecisions), so the coordinator can see how many cards wait on
// a default rather than on a fresh judgment. A coordinator's own hold is not one.
func TestWhereCountsCardsHeldByServerDefault(t *testing.T) {
	t.Parallel()
	r := newHoldRig(t, 1, 0)
	r.must(store.NoteStep("test", sprint.Note{Kind: sprint.Judgment, Type: sprint.NBound, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, What: "attempt budget exhausted", At: r.now}))
	judgment := r.openNote(sprint.NBound, "s1-1")
	require.NotNil(t, judgment)
	overdue := func() {
		r.must(store.TickPartStep("overdue", sprint.TickOverdue, sprint.TickReq{}, nil, nil, nil))
		r.must(store.DrainStep())
	}
	reports := func(judgment string) int {
		n := 0
		for _, x := range r.allNotes() {
			if x.Type == sprint.NServerDefault && x.Answers == judgment {
				n++
			}
		}
		return n
	}
	r.mu.Lock()
	r.now = r.now.Add(2 * sprint.DeadlineJudgment)
	r.mu.Unlock()
	overdue()
	c := r.snap().Work.Card("s1-1")
	require.Equal(t, sprint.Waiting, c.Col, "the bound primary is parked by the default")
	require.Equal(t, sprint.ServerDefaultActor, c.F("held_by"))
	s := r.snap()
	held := sprint.DefaultHeldDecisions(s)
	require.Len(t, held, 1)
	sum := sprint.DecisionsOwed(s.Now, nil, held, nil)
	require.Equal(t, 1, sum.HeldByDefault)
	require.Contains(t, sum.Line(), "held by server-default 1")
	require.Equal(t, 1, reports(judgment.ID), "the default wrote one report for the judgment")

	// A second overdue pass re-pushes nothing and writes no second report.
	overdue()
	s = r.snap()
	held = sprint.DefaultHeldDecisions(s)
	require.Len(t, held, 1)
	sum = sprint.DecisionsOwed(s.Now, nil, held, nil)
	require.Equal(t, 1, sum.HeldByDefault)
	require.Equal(t, 1, reports(judgment.ID), "an unchanged set is not pushed again")

	// Reversed witness: the coordinator's own held card is not a server-default hold.
	r.must(store.AddStep(sprint.AddReq{Stream: "s2", Count: 1, Held: true, Who: "coordinator"}))
	r.must(store.DrainStep())
	s = r.snap()
	other := s.Work.Card("s2-1")
	require.True(t, sprint.IsHeld(other), "the coordinator's card is held: %+v", other)
	require.Empty(t, other.F("held_by"))
	require.Equal(t, sprint.ServerDefaultActor, s.Work.Card("s1-1").F("held_by"), "both cards are held")
	held = sprint.DefaultHeldDecisions(s)
	require.Len(t, held, 1)
	sum = sprint.DecisionsOwed(s.Now, nil, held, nil)
	require.Equal(t, 1, sum.HeldByDefault, "only the server-default hold counts")
	require.Contains(t, sum.Line(), "held by server-default 1")

	// With only the coordinator's hold left, the count is zero.
	r.must(store.ReleaseStep(sprint.ReleaseReq{IDs: []string{"s1-1"}, Reason: "reviewed; resume", Who: "coordinator", Coordinator: "coordinator"}))
	r.must(store.DrainStep())
	s = r.snap()
	require.True(t, sprint.IsHeld(s.Work.Card("s2-1")))
	require.Empty(t, s.Work.Card("s1-1").F(sprint.FieldHeld))
	held = sprint.DefaultHeldDecisions(s)
	require.Empty(t, held)
	sum = sprint.DecisionsOwed(s.Now, nil, held, nil)
	require.Equal(t, 0, sum.HeldByDefault)
	require.Contains(t, sum.Line(), "held by server-default 0")
}
