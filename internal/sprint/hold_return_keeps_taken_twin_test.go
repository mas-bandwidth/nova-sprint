package sprint_test

import (
	"strconv"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHoldReturnLeavesTakenCardsWithTheirWorker(t *testing.T) {
	t.Parallel()
	r := newReadCardsRig(t, "m1", "m2", "m3")
	r.st.ShareTwin(store.NewTwin())

	// Work 30 pro primaries on m2/m3 while m1 drains. Once they finish,
	// m1 receives one actual read card for each primary at review.
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Drain: true, Who: "coordinator"}))
	r.m.SetMachineWidth("m2", strconv.Itoa(40))
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m2", Who: "coordinator"}))
	r.m.SetMachineWidth("m3", strconv.Itoa(40))
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m3", Who: "coordinator"}))
	r.must(store.AddStep(sprint.AddReq{Stream: "s1", Count: 30, Brief: proBrief}))
	r.tick()
	r.tick()
	workByMember := map[string][]string{"m2": {}, "m3": {}}
	workGensByMember := map[string]map[string]int{"m2": {}, "m3": {}}
	for _, member := range []string{"m2", "m3"} {
		for _, c := range r.snap().Fleet.Cell(member, sprint.Ready) {
			if c.F("kind") == "work" {
				workByMember[member] = append(workByMember[member], c.ID)
				workGensByMember[member][c.ID] = c.Int("gen")
			}
		}
	}
	require.Len(t, append(workByMember["m2"], workByMember["m3"]...), 30, "all 30 primaries were dealt")
	for _, member := range []string{"m2", "m3"} {
		ids := workByMember[member]
		gens := workGensByMember[member]
		r.must(store.TakeStep(sprint.TakeReq{As: member, Sel: sprint.Sel{IDs: ids}, Gens: gens, Who: member}))
		r.must(store.FinishStep(sprint.FinishReq{As: member, Head: fmt.Sprintf("%040d", 1), Branch: "sprint/reads", Report: "done", Sel: sprint.Sel{IDs: ids}, Gens: gens, Who: member}))
	}
	r.m.SetMachineWidth("m1", strconv.Itoa(40))
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Who: "coordinator"}))
	r.tick()
	// m1 now has one read card for each of the 30 reviewed primaries.
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m2", Drain: true, Who: "coordinator"}))
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m3", Drain: true, Who: "coordinator"}))
	r.tick()
	r.must(store.AddStep(sprint.AddReq{Stream: "s1", Count: 6, Brief: proBrief}))
	r.tick()

	before := r.snap()
	var readReady, workReady []string
	for _, c := range before.Fleet.Cell("m1", sprint.Ready) {
		if c.F("kind") == "read" {
			readReady = append(readReady, c.ID)
		} else if c.F("kind") == "work" {
			workReady = append(workReady, c.ID)
		}
	}
	require.Len(t, workReady, 6, "six work cards are ready on m1")
	require.Len(t, readReady, 30, "the 30 pro primaries generated 30 real read cards on m1")

	takenWork := append([]string(nil), workReady...)
	workGens := map[string]int{}
	for _, id := range takenWork {
		workGens[id] = before.Fleet.Card(id).Int("gen")
	}
	r.must(store.TakeStep(sprint.TakeReq{As: "m1", Sel: sprint.Sel{IDs: takenWork}, Gens: workGens, Who: "m1"}))
	takenRead := append([]string(nil), readReady[:8]...)
	beginGens := map[string]int{}
	for _, id := range takenRead {
		beginGens[id] = before.Fleet.Card(id).Int("gen")
	}
	r.must(store.TakeStep(sprint.TakeReq{As: "m1", Sel: sprint.Sel{IDs: takenRead}, Gens: beginGens, Who: "m1"}))

	before = r.snap()
	taken := append(append([]string(nil), takenWork...), takenRead...)
	require.Len(t, taken, 14, "six work cards and eight started reads")
	require.Len(t, onRow(before, "m1", "s1", sprint.Working), 14, "the 14 taken work and read cards are all on m1")
	require.Len(t, onRow(before, "m1", "s1", sprint.Ready), 22, "22 read cards remain ready on m1")
	generation := map[string]int{}
	redeals := map[string]int{}
	for _, id := range taken {
		generation[id] = before.Fleet.Card(id).Int("gen")
		redeals[id] = before.Fleet.Card(id).Int("redeals")
	}

	res, err := r.st.Hold(r.ctx, sprint.HoldReq{Names: []string{"m1"}, Reason: "return its dealt work", Return: true, Who: "coordinator"})
	require.NoError(t, err)
	require.Empty(t, res.Refused)
	after := r.snap()
	assert.ElementsMatch(t, taken, onRow(after, "m1", "s1", sprint.Working), "taken cards stay working on their worker")
	assert.Empty(t, after.Fleet.Cell("m1", sprint.Ready), "all dealt but untaken cards leave the row")
	for id, gen := range generation {
		c := after.Fleet.Card(id)
		if !assert.NotNil(t, c, "%s remains on the fleet table", id) {
			continue
		}
		assert.Equal(t, "m1", c.Row, "%s stays on m1", id)
		assert.Equal(t, sprint.Working, c.Col, "%s stays working", id)
		assert.Equal(t, gen, c.Int("gen"), "%s keeps its generation", id)
		assert.Equal(t, redeals[id], c.Int("redeals"), "%s gets no redeal", id)
		assert.Empty(t, c.F("retired_by"), "%s is not retired", id)
	}
	for _, id := range readReady {
		c := after.Fleet.Card(id)
		assert.True(t, c == nil || c.Row != "m1" || c.Col != sprint.Ready, "%s was ready and leaves m1", id)
	}
	lines, err := r.st.Log(r.ctx)
	require.NoError(t, err)
	var holdLine string
	for _, line := range lines {
		if line.Note != nil && line.Note.Type == sprint.NHeld && strings.Contains(line.Note.What, "member m1 held:") {
			holdLine = line.Note.What
		}
	}
	require.NotEmpty(t, holdLine)
	assert.Contains(t, holdLine, "--return: 14 taken cards stay with their worker and finish:")
	for _, id := range taken {
		assert.Contains(t, holdLine, id)
	}

	_, err = r.st.Hold(r.ctx, sprint.HoldReq{Names: []string{"m1"}, Release: true, Reason: "resume", Who: "coordinator"})
	require.NoError(t, err)
	for _, id := range takenWork {
		finished := r.must(store.FinishStep(sprint.FinishReq{As: "m1", Sel: sprint.Sel{IDs: []string{id}}, Gens: map[string]int{id: generation[id]}, Who: "m1"}))
		require.NotEmpty(t, finished.Moved, "work card %s finishes", id)
	}
	finishReadGens := map[string]int{}
	for _, id := range takenRead {
		finishReadGens[id] = generation[id]
	}
	readResult := r.must(store.ReadStep(sprint.ReadReq{As: "m1", Verdict: "ok", Sel: sprint.Sel{IDs: takenRead}, Gens: finishReadGens, Who: "m1"}))
	require.NotEmpty(t, readResult.Moved, "the eight reads finish through the member read verb")
}

func TestRepeatedLiveHoldReturnDoesNotReclaimTakenCards(t *testing.T) {
	t.Parallel()
	r := newHoldRig(t, 4, 0)
	r.tick()
	taken := r.takeOne("m1")
	before := r.snap()
	gen := before.Fleet.Card(taken).Int("gen")

	r.hold(sprint.HoldReq{Names: []string{"m1"}, Reason: "first hold", Return: true})
	first := r.snap()
	require.Equal(t, sprint.Working, first.Fleet.Card(taken).Col)
	require.Equal(t, gen, first.Fleet.Card(taken).Int("gen"))

	r.hold(sprint.HoldReq{Names: []string{"m1"}, Reason: "repeat hold", Return: true})
	after := r.snap()
	assert.Equal(t, "m1", after.Fleet.Card(taken).Row)
	assert.Equal(t, sprint.Working, after.Fleet.Card(taken).Col)
	assert.Equal(t, gen, after.Fleet.Card(taken).Int("gen"))
	assert.Equal(t, before.Fleet.Card(taken).Int("redeals"), after.Fleet.Card(taken).Int("redeals"))
}

func TestHoldReturnOnADownMemberStillRedealsItsWorkingCards(t *testing.T) {
	t.Parallel()
	r := newHoldRig(t, 8, 0)
	r.tick()
	taken := r.takeOne("m1")
	gen := r.snap().Fleet.Card(taken).Int("gen")
	r.m.SetMachineWidth("m3", strconv.Itoa(2))
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m3", Who: "coordinator"}))
	// The beat has genuinely lapsed before the coordinator marks the member
	// down; Finish keeps the dead run present for the following hold --return.
	r.mu.Lock()
	r.now = r.now.Add(sprint.MissedBeatsDown*sprint.BeatDeadline + time.Second)
	now := r.now
	r.mu.Unlock()
	beats, err := r.st.Beats(r.ctx, []string{"m1"})
	require.NoError(t, err)
	require.False(t, beats["m1"].Alive(now), "m1's beat has really lapsed")
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
