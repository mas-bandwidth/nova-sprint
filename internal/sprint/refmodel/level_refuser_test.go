package refmodel_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/refmodel"
)

// The levelrefuser case (Zhi, zhi-d61b4dd67f9b) on the model's own rule: the
// emptiest open member refused the longest queue's newest card at staging.
// The wedge of 2026-10-02 (nova-tools#5122) at the model's one width: four
// members up, A holding 3 ready cards, T 2, S 1 and U 4 working, so the
// backlogs are -61, -62, -63 and -60 and the mean -62; A's newest (A-refused)
// refused by S. The model skips the refused card, not moving it one below (no
// gap: the wedge's rule) nor onto S (no skip: the #5000 probe), and moves A's
// next older card to S; the call ends. The engine's level is a separate card
// (tla-level-moves-only-onto-a-free-lane), so the model alone is pinned here.
func TestLevelRefuserTheModelSkipsTheRefusedNewest(t *testing.T) {
	t.Parallel()
	s := refmodel.New(nil, []string{"A", "T", "S", "U"}, "coordinator")
	for _, m := range []string{"A", "T", "S", "U"} {
		s.Members[m] = refmodel.Up
	}
	put := func(id, member, place string, score float64, refusers ...string) {
		s.Primaries[id] = refmodel.Primary{Stream: "s1", State: refmodel.Working, Score: score, Attempt: 1}
		s.Work[id+".w1"] = refmodel.WorkCard{Primary: id, Member: member, Place: place, Gen: 1, Refusers: refusers}
	}
	put("A-ready-0", "A", refmodel.FReady, 1)
	put("A-ready-1", "A", refmodel.FReady, 2)
	put("A-refused", "A", refmodel.FReady, 3, "S") // A's newest, refused by S
	put("T-ready-0", "T", refmodel.FReady, 4)
	put("T-ready-1", "T", refmodel.FReady, 5)
	put("S-ready-0", "S", refmodel.FReady, 6)
	for i := range 4 {
		put(fmt.Sprintf("U-working-%d", i), "U", refmodel.FWorking, float64(7+i))
	}
	after, err := refmodel.Level(s, nil)
	require.NoError(t, err)
	for id, c := range after.Work {
		assert.NotContains(t, c.Refusers, c.Member, "%s levelled onto a member that refused it", id)
	}
	assert.Equal(t, "A", after.Work["A-refused.w1"].Member, "the refused newest stays on A")
	assert.Equal(t, "S", after.Work["A-ready-1.w1"].Member, "the next older card moves to S, below the mean")
	moved := 0
	for id, w := range s.Work {
		if after.Work[id].Member != w.Member {
			moved++
		}
	}
	assert.Equal(t, 1, moved, "one card moves: the levelrefuser case")
}
