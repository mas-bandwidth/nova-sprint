package store

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/stretchr/testify/require"
)

// Exercise AskStep's real load/plan/apply boundary: the normal snapshot omits
// retired fleet cards, though their member records still occupy their IDs.
// tla/AskReadIDs.tla checks this allocator visibility rule in a bounded model.
func TestAskAfterEndedReadsMintsNewReadIDs(t *testing.T) {
	t.Parallel()
	for _, epoch := range []int{0, 1} {
		t.Run(fmt.Sprintf("epoch-%d", epoch), func(t *testing.T) {
			h := newHarness(t)
			if epoch != 0 {
				_, err := h.st.Clear(h.ctx)
				require.NoError(t, err)
			}
			h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m2", Width: 2}))
			pinned, err := h.st.pin(h.ctx)
			require.NoError(t, err)
			require.NoError(t, pinned.B.RowsAdd(h.ctx, pinned.Names.Table(sprint.Readers), []string{"reader-m2"}))
			require.NoError(t, h.st.BeatReaders(h.ctx))
			h.must(AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"p"}, Brief: "p: read a pushed head\n"}))
			h.startMachine()
		// Materialize the admitted work from the running machine's work queue,
		// then stop before placing it at the review boundary used by this fixture.
		h.machine()
		h.stopMachine()
			// The fixture starts at the observed lifecycle boundary: a primary in
			// review and a reader working its first read of that exact head.
			h.must(Step{Verb: "seed reask", Load: All, Plan: func(s *sprint.Snapshot) sprint.Plan {
				return sprint.Plan{Units: []sprint.Unit{{Key: "p", Changes: []sprint.Change{
					{Table: sprint.Work, Entry: ntable.BatchMemberEntry{ID: "p", Move: &ntable.MemberMoveOp{Row: "s1", Col: sprint.Review}, Set: map[string]string{"attempt": "1", "head": "abc123", "result": "ok"}}},
					{Table: sprint.Fleet, Entry: ntable.BatchMemberEntry{ID: "p.r1.m2", Expect: &ntable.MemberExpect{Absent: true}, Create: &ntable.MemberCreateOp{Row: "m2", Col: sprint.Working, Score: 1}, Set: map[string]string{"kind": "read", sprint.FieldReadCard: "1", "primary": "p", "stream": "s1", "reader": "m2", "attempt": "1", "head": "abc123", "gen": "1"}}},
				}}}}
			}})
			oldIDs := []string{}
			for generation := 0; generation < 3; generation++ {
				id := "p.r1.m2"
				if generation > 0 {
					id += fmt.Sprintf(".g%d", generation)
				}
				h.must(ReadStep(sprint.ReadReq{As: "m2", Sel: sprint.Sel{IDs: []string{id}}, Verdict: "broken", Finding: "origin has no branch", Missing: map[string]sprint.MissingBranch{id: {Named: "missing", Holds: "pushed"}}}))
				oldIDs = append(oldIDs, id)
				// Model the coordinator's pending cannot-ask judgment. The answer must
				// actually deal a read before closing it, through AskStep, not a fake plan.
				h.must(Step{Verb: "seed cannot ask", Load: All, Plan: func(s *sprint.Snapshot) sprint.Plan {
					return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Judgment, Type: sprint.NCannotAsk, Stream: "s1", Primaries: []string{"p"}, Attempt: 1, At: s.Now, What: "read must be asked again"}}}
				}})
				open := h.openOf(sprint.NCannotAsk)
				require.Len(t, open, 1)
				h.must(AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{"p"}}, Answers: []string{open[0].Note.ID}, Who: "tester"}))
				nextID := fmt.Sprintf("p.r1.m2.g%d", generation+1)
				s := h.snap()
				require.NotNil(t, s.Fleet.Placed(nextID))
				require.Equal(t, 1, s.Work.Card("p").Int("attempt"))
				require.Empty(t, h.openOf(sprint.NCannotAsk))
				// A late receipt for any ended read cannot close its replacement.
				for _, old := range oldIDs {
					res := h.run(ReadStep(sprint.ReadReq{As: "m2", Sel: sprint.Sel{IDs: []string{old}}, Gens: map[string]int{old: 1}, Verdict: "ok"}))
					require.NotEmpty(t, res.Refused)
				}
				s, err = h.st.Load(h.ctx, All, sprint.NamedExtras(sprint.Fleet, oldIDs))
				require.NoError(t, err)
				for _, old := range oldIDs {
					record := s.Fleet.Card(old)
					require.NotNil(t, record)
					require.False(t, record.Placed())
					require.Equal(t, sprint.RetiredByMissingBranch, record.F("retired_by"))
					require.Equal(t, "origin has no branch", record.F("finding"))
					require.Empty(t, record.F("verdict"))
				}
				require.NotNil(t, s.Fleet.Placed(nextID))
			}
		})
	}
}
