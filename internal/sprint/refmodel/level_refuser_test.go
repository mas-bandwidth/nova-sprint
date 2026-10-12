package refmodel_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
