package sprint_test

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHoldReturnLeavesTakenCardsWithTheirWorker(t *testing.T) {
	t.Parallel()
	r := newHoldRig(t, 36, 0)
	r.st.ShareTwin(store.NewTwin())
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 40, Who: "coordinator"}))
	r.hold(sprint.HoldReq{Names: []string{"m2"}, Reason: "keep a lane free"})
	r.tick()
	var taken []string
	for range 14 {
		taken = append(taken, r.takeOne("m1"))
	}
	before := r.snap()
	generation := map[string]int{}
	redeals := map[string]int{}
	for _, id := range taken {
		generation[id] = before.Fleet.Card(id).Int("gen")
		redeals[id] = before.Fleet.Card(id).Int("redeals")
	}
	require.Len(t, onRow(before, "m1", "s1", sprint.Ready), 22, "22 dealt, untaken cards are ready on the row")

	r.hold(sprint.HoldReq{Names: []string{"m1"}, Reason: "return its dealt work", Return: true})
	s := r.snap()
	assert.ElementsMatch(t, taken, onRow(s, "m1", "s1", sprint.Working), "taken cards stay working on their worker")
	assert.Empty(t, onRow(s, "m1", "s1", sprint.Ready), "dealt but untaken cards leave the row")
	for id, gen := range generation {
		assert.Equal(t, gen, s.Fleet.Card(id).Int("gen"), "%s keeps its generation", id)
		assert.Equal(t, redeals[id], s.Fleet.Card(id).Int("redeals"), "%s gets no redeal", id)
		assert.Empty(t, s.Fleet.Card(id).F("retired_by"), "%s is not retired", id)
	}
	lines := r.holdLines()
	line := lines[len(lines)-1]
	assert.Contains(t, line, fmt.Sprintf("--return: %d taken cards stay with their worker and finish:", len(taken)))
	for _, id := range taken {
		assert.Contains(t, line, id)
	}

	r.hold(sprint.HoldReq{Names: []string{"m1"}, Release: true, Reason: "resume"})
	for _, id := range taken {
		finished := r.must(store.FinishStep(sprint.FinishReq{As: "m1", Sel: sprint.Sel{IDs: []string{id}}, Gens: map[string]int{id: generation[id]}, Who: "m1"}))
		require.Len(t, finished.Moved, 1, "taken card finishes once")
	}
}

func TestHoldReturnOnADownMemberStillRedealsItsWorkingCards(t *testing.T) {
	t.Parallel()
	r := newHoldRig(t, 8, 0)
	r.tick()
	taken := r.takeOne("m1")
	gen := r.snap().Fleet.Card(taken).Int("gen")
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m3", Width: 2, Who: "coordinator"}))
	// Preserve the working card while marking the member down, as a dead run
	// awaiting the coordinator's --return.
	r.must(store.FleetStep(sprint.FleetReq{Op: "down", Member: "m1", Finish: true, Who: "coordinator"}))
	s := r.snap()
	require.Equal(t, sprint.Down, s.MemberCtl("m1").F("status"))
	require.Equal(t, sprint.Working, s.Fleet.Card(taken).Col)

	r.hold(sprint.HoldReq{Names: []string{"m1"}, Reason: "return its dead run", Return: true})
	s = r.snap()
	assert.NotEqual(t, "m1", s.Fleet.Card(taken).Row)
	assert.Equal(t, sprint.Ready, s.Fleet.Card(taken).Col)
	assert.Equal(t, gen+1, s.Fleet.Card(taken).Int("gen"))
	assert.Equal(t, 1, s.Fleet.Card(taken).Int("redeals"))
}
