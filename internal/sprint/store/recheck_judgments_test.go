package store

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuccessfulStartRechecksAndRecordsEachClosureOnce(t *testing.T) {
	t.Parallel()
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "stopped", true: "already running"}[running], func(t *testing.T) {
			t.Parallel()
			h := routeHarness(t, route("pro-a", "pro"))
			h.addReady("s1", 1, briefOf("pro", ""))
			h.must(DealStep(sprint.DealReq{}))
			h.failTake("s1-1.w1", providerLine)
			if running {
				h.startMachine()
			}
			h.must(Step{Verb: "fixture judgment", Load: All, Plan: func(s *sprint.Snapshot) sprint.Plan {
				return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Judgment, Type: sprint.NWorkLate, Card: "s1-1.w1", Stream: "s1", Primaries: []string{"s1-1"}, What: "not finished", At: s.Now}}}
			}})
			require.Len(t, h.openOf(sprint.NWorkLate), 1)
			id := h.openOf(sprint.NWorkLate)[0].Note.ID
			_, _, result, err := h.st.SetMachine(h.ctx, true)
			require.NoError(t, err)
			require.Len(t, result.Said, 1)
			assert.Contains(t, result.Said[0], id)
			assert.Empty(t, h.openOf(sprint.NWorkLate))
			assert.Equal(t, sprint.Withdrawn, h.snap().Fleet.Card("s1-1.w1").Col, "recheck does not redeal")
			_, _, repeated, err := h.st.SetMachine(h.ctx, true)
			require.NoError(t, err)
			assert.Empty(t, repeated.Said)
			notes, _, err := h.m.NotesSince(h.ctx, "", 1000)
			require.NoError(t, err)
			decided := 0
			for _, n := range notes {
				if n.Kind == sprint.Decided && n.Answers == id {
					decided++
				}
			}
			assert.Equal(t, 1, decided)
		})
	}
}

func TestRecheckCannotCrossItsPinnedEpoch(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	pinned, err := h.st.Pinned(h.ctx)
	require.NoError(t, err)
	_, err = h.st.Clear(h.ctx)
	require.NoError(t, err)
	result, err := pinned.RecheckJudgments(h.ctx, "adopt")
	require.ErrorContains(t, err, "epoch")
	assert.NotEmpty(t, result.Refused)
	assert.Empty(t, result.Said)
}

func TestARefusedStartLeavesObsoleteJudgmentsForASuccessfulRecheck(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	pinned, err := h.st.Pinned(h.ctx)
	require.NoError(t, err)
	_, err = h.st.Clear(h.ctx)
	require.NoError(t, err)
	h.must(Step{Verb: "fixture judgment", Load: All, Plan: func(s *sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Judgment, Type: sprint.NWorkLate, Primaries: []string{"absent"}, At: s.Now}}}
	}})
	require.Len(t, h.openOf(sprint.NWorkLate), 1)
	// The captured old epoch is a real store START refusal; funding is a CLI
	// gate on this base, so a provider rest alone cannot exercise this guard.
	_, _, result, err := pinned.SetMachine(h.ctx, true)
	require.ErrorContains(t, err, "epoch")
	assert.Empty(t, result.Said)
	assert.Len(t, h.openOf(sprint.NWorkLate), 1, "a refused start must not run the closure pass")
	_, _, result, err = h.st.SetMachine(h.ctx, true)
	require.NoError(t, err)
	assert.Len(t, result.Said, 1)
	assert.Empty(t, h.openOf(sprint.NWorkLate), "a fresh successful start closes the obsolete judgment")
}
