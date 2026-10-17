package sprint_test

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/require"
)

// The server-default answer to an unanswered bound judgment closes that judgment and
// holds the card by its fields (held, held_by=server-default, held_judgment), so the
// need-blocked judgment retires while the card is still held: "is unheld" is false.
// The decided note must name the standing hold instead. The in-memory store, explicit
// time, no live transport.
func TestANeedBlockedRetireNamesAHoldThatStands(t *testing.T) {
	r := newHoldRig(t, 1, 0)
	r.must(store.AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"s1-2"}, Needs: []string{"s1-1"}}))
	r.must(store.NoteStep("fixture bound", sprint.Note{Kind: sprint.Judgment, Type: sprint.NBound, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, At: r.now, What: "attempt 1's child ended on the member's environment 3 times"}))
	r.must(store.TickPartStep("needs-blocked", sprint.TickNeedBlocked, sprint.TickReq{}, nil, nil, nil))
	r.must(store.DrainStep())
	r.mu.Lock()
	r.now = r.now.Add(2 * sprint.DeadlineJudgment)
	r.mu.Unlock()
	r.must(store.TickPartStep("overdue", sprint.TickOverdue, sprint.TickReq{}, nil, nil, nil))
	r.must(store.DrainStep())
	c := r.snap().Work.Card("s1-1")
	require.Equal(t, "server-default", c.F(sprint.FieldHeldBy), "the server-default answer holds s1-1 by its fields")
	r.must(store.TickPartStep("needs-blocked", sprint.TickNeedBlocked, sprint.TickReq{}, nil, nil, nil))
	r.must(store.DrainStep())
	want := "s1-1 is still held by server-default and no open judgment holds it: nova-sprint release s1-1 deals it again"
	var found bool
	for _, n := range r.allNotes() {
		require.NotEqual(t, "s1-1 is unheld", n.What, "no note says the held card is unheld")
		if n.Kind == sprint.Decided && n.Type == sprint.NNeedBlocked && n.What == want {
			found = true
		}
	}
	require.True(t, found, "the retired need-blocked judgment names the standing hold")

	r.must(store.ReleaseStep(sprint.ReleaseReq{IDs: []string{"s1-1"}, Reason: "reviewed; resume", Who: "coordinator", Coordinator: "coordinator"}))
	r.must(store.DrainStep())
	require.Empty(t, r.snap().Work.Card("s1-1").F(sprint.FieldHeld), "release clears the hold")
	r.must(store.TickPartStep("needs-blocked", sprint.TickNeedBlocked, sprint.TickReq{}, nil, nil, nil))
	r.must(store.DrainStep())
	require.Nil(t, r.openNote(sprint.NNeedBlocked, "s1-1"), "released, no blocker judgment is raised")
}
