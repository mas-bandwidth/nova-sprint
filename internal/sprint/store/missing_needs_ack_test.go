package store

import (
	"fmt"
	"slices"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// Inbox recovery can acknowledge several judgments about the same primary
// at once. Their waivers must form one guarded card change, not duplicate
// member entries or a last-write-wins waiver.
func TestAckCombinesDependencyJudgmentsForOnePrimary(t *testing.T) {
	t.Parallel()
	for _, kinds := range []string{"missing", "dropped", "mixed"} {
		for _, sentinel := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/sentinel=%v", kinds, sentinel), func(t *testing.T) {
				h := newHarness(t)
				h.setup(2)
				h.must(AddStep(sprint.AddReq{Stream: "s2", IDs: []string{"waiter"}, Needs: []string{"s1-1", "s1-2"}, Sentinel: sentinel}))
				var wants []string
				switch kinds {
				case "missing":
					seedMissingNeeds(h, "waiter", "first.bad")
					h.must(ResolveStep(sprint.ResolveReq{}))
					seedMissingNeeds(h, "waiter", "first.bad,second.bad")
					h.must(ResolveStep(sprint.ResolveReq{}))
					wants = []string{"first.bad", "second.bad"}
				case "dropped":
					seedDroppedNeed(h, "s1-1")
					h.must(ResolveStep(sprint.ResolveReq{}))
					seedDroppedNeed(h, "s1-2")
					h.must(ResolveStep(sprint.ResolveReq{}))
					wants = []string{"s1-1", "s1-2"}
				case "mixed":
					seedMissingNeeds(h, "waiter", "first.bad,s1-2")
					h.must(ResolveStep(sprint.ResolveReq{}))
					seedDroppedNeed(h, "s1-2")
					h.must(ResolveStep(sprint.ResolveReq{}))
					wants = []string{"first.bad", "s1-2"}
				}
				notes := append(h.nOpenOf(sprint.NMissingNeed, "waiter"), h.nOpenOf(sprint.NBlocked, "waiter")...)
				require.Len(t, notes, 2, "need two distinct judgments: %+v", notes)
				ids := []string{notes[0].Note.ID, notes[1].Note.ID}
				h.must(AckStep(sprint.AckReq{Notes: ids, Reason: "both unnecessary", Who: "tester"}))
				c := h.snap().Work.Card("waiter")
				for _, n := range wants {
					require.Contains(t, ","+c.F("waived")+",", ","+n+",", "lost waiver %s: %+v", n, c)
				}
				if sentinel {
					require.Equal(t, sprint.Waiting, c.Col, "sentinel: %+v", c)
					require.NotEmpty(t, c.F("reached"), "sentinel: %+v", c)
					require.Empty(t, h.nOpenOf(sprint.NSentinelReached, "waiter"), "progress is not an ackable gate")
					notes, _, err := h.m.NotesSince(h.ctx, "", 100000)
					require.NoError(t, err)
					progress := 0
					for _, n := range notes {
						if n.Type == sprint.NSentinelReached && slices.Contains(n.Primaries, "waiter") {
							require.Equal(t, sprint.Happened, n.Kind)
							require.Empty(t, n.Decisions)
							progress++
						}
					}
					require.Equal(t, 1, progress)
				} else {
					require.Equal(t, sprint.Ready, c.Col, "primary: %+v", c)
				}
				require.Equal(t, 0, len(h.nOpenOf(sprint.NMissingNeed, "waiter"))+len(h.nOpenOf(sprint.NBlocked, "waiter")), "dependency judgment remains")
				h.clean("combined waiver")
			})
		}
	}
}
