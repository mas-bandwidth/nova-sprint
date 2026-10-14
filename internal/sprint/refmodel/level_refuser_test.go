package refmodel_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/refmodel"
)

// The levelrefuser case under the free-lane rule: a member A holds 65 ready
// cards (one over its width of 64), its newest (p65, the highest score)
// refused at staging by S; S holds 63 (one free lane) and T holds 64 (none).
// A card moves only off a member over its width, and only onto a free lane
// that did not refuse it: the newest is skipped (its only free lane refused
// it) and the next-newest moves to S, then A is at its width and the call ends
// (round.levelTo, nextLane; tla/Level.tla, NeverOnARefuser, and NeverOffAFreeLane,
// which lands with #127).
func TestLevelRefuserTheEngineAndTheModelAgree(t *testing.T) {
	t.Parallel()
	s := refmodel.New(nil, []string{"A", "S", "T"}, "coordinator")
	for _, m := range []string{"A", "S", "T"} {
		s.Members[m] = refmodel.Up
	}
	put := func(id, member string, score float64, refusers ...string) {
		s.Primaries[id] = refmodel.Primary{Stream: "s1", State: refmodel.Working, Score: score, Attempt: 1}
		s.Work[id+".w1"] = refmodel.WorkCard{Primary: id, Member: member, Place: refmodel.FReady, Gen: 1, Refusers: refusers}
	}
	for i := 1; i <= 64; i++ {
		put(fmt.Sprintf("p%02d", i), "A", float64(i))
	}
	put("p65", "A", 65, "S") // the newest card: its only free lane (S) refused it at staging
	for i := 1; i <= 63; i++ {
		put(fmt.Sprintf("s%02d", i), "S", float64(100+i))
	}
	for i := 1; i <= 64; i++ {
		put(fmt.Sprintf("t%02d", i), "T", float64(200+i))
	}
	after, err := refmodel.Level(s, nil)
	require.NoError(t, err)
	for id, c := range after.Work {
		assert.NotContains(t, c.Refusers, c.Member, "%s levelled onto a member that refused it", id)
	}
	assert.Equal(t, "S", after.Work["p64.w1"].Member, "the next-newest moves onto the free lane")
	assert.Equal(t, "A", after.Work["p65.w1"].Member, "the refused newest stays")
	moved := 0
	for id, w := range s.Work {
		if after.Work[id].Member != w.Member {
			moved++
		}
	}
	assert.Equal(t, 1, moved, "one card moves: A was one over its width")
}

// The engine and the model agree when the most-over source has no movable
// card: over (width 64) holds 64 working and 3 ready, each refused at staging
// by free; less holds 64 working and 1 ready; free holds none. The engine's
// level skips over and moves less's card to free, and the model, given the
// engine's choice, takes it as its own (Level refuses a choice its round did
// not make). tla/Level.tla ChoiceFree, which lands with #127.
func TestLevelSkipsASourceWithNoMovableCardTheEngineAndTheModelAgree(t *testing.T) {
	t.Parallel()
	w := newWorld("reader-a")
	for _, m := range []string{"over", "less", "free"} {
		w.must(t, sprint.FleetStep(w.s, sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: m, Who: coordinator}))
	}
	score := 1.0
	put := func(id, member, col string, refuser string) {
		fields := map[string]string{"kind": "work", "primary": id, "stream": "s1", "attempt": "1", "gen": "2", "member": member,
			"dealt": "2026-09-30T00:00:00Z", "untaken_since": "2026-09-30T00:00:00Z", "redeals": "0"}
		if refuser != "" {
			fields[sprint.FieldStagingTake+"1"] = sprint.ProviderTake{Member: refuser}.String()
		}
		w.s.Fleet.Put(&sprint.Card{ID: id + ".w1", Row: member, Col: col, Score: score, Rev: 1, Fields: fields})
		w.s.Work.Put(&sprint.Card{ID: id, Row: "s1", Col: sprint.Working, Score: score, Rev: 1,
			Fields: map[string]string{"kind": "primary", "attempt": "1", "stream": "s1", "work": id + ".w1"}})
		score++
	}
	for i := 0; i < refmodel.Width; i++ {
		put(fmt.Sprintf("over-w%02d", i), "over", sprint.Working, "")
		put(fmt.Sprintf("less-w%02d", i), "less", sprint.Working, "")
	}
	for i := 0; i < 3; i++ {
		put(fmt.Sprintf("over-r%d", i), "over", sprint.Ready, "free")
	}
	put("less-r", "less", sprint.Ready, "")
	before := refmodel.Abstract(refmodel.Observed{Snap: w.s, Machine: refmodel.Running})

	p := sprint.FleetStep(w.s, sprint.FleetReq{Op: "level", Who: sprint.MachineActor})
	moves := map[string]string{}
	for _, u := range p.Units {
		for _, ch := range u.Changes {
			if ch.Entry.Move != nil {
				moves[u.Key] = ch.Entry.Move.Row
			}
		}
	}
	require.Equal(t, map[string]string{"less-r.w1": "free"}, moves, "the engine's level")
	after, err := refmodel.Level(before, moves)
	require.NoError(t, err, "the model refuses the engine's choice")
	assert.Equal(t, "free", after.Work["less-r.w1"].Member)
	for i := 0; i < 3; i++ {
		assert.Equal(t, "over", after.Work[fmt.Sprintf("over-r%d.w1", i)].Member, "a refused card stays")
	}
}
