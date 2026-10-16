package store

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTwoOkReadsAtTheHeadAcceptWithAG1 tests that a primary in review with two ok reads
// at its head (one plain and one .g1 regeneration of another reader) is accepted into
// merging by AcceptStep, even if all read cards are retired.
// Last night's case, epoch 20: p at attempt 4, head H; p.r4.vision retired, verdict ok,
// head H; p.r4.superman retired by environment, no verdict; p.r4.superman.g1 retired,
// verdict ok, head H. AcceptStep moves it to merging.
func TestTwoOkReadsAtTheHeadAcceptWithAG1(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	// Set up members vision and superman on fleet table and readers table
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "vision", Width: 2}))
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "superman", Width: 2}))
	require.NoError(t, h.st.B.RowsAdd(h.ctx, h.st.Names.Table(sprint.Readers), []string{"reader-vision", "reader-superman"}))
	require.NoError(t, h.m.RowSet(h.ctx, h.st.Names.Table(sprint.Readers), "reader-vision", map[string]string{sprint.ReaderTiers: "pro,flash"}))
	require.NoError(t, h.m.RowSet(h.ctx, h.st.Names.Table(sprint.Readers), "reader-superman", map[string]string{sprint.ReaderTiers: "pro,flash"}))
	require.NoError(t, h.st.BeatReaders(h.ctx))

	// Ensure stream s1 row exists on work and merge tables
	require.NoError(t, h.m.RowsAdd(h.ctx, h.st.Names.Table(sprint.Work), []string{"s1"}))
	require.NoError(t, h.m.RowsAdd(h.ctx, h.st.Names.Table(sprint.Merge), []string{"s1"}))
	h.poke(sprint.Merge, ntable.BatchMemberEntry{
		ID: sprint.CtlID("s1"), Expect: &ntable.MemberExpect{Absent: true},
		Create: &ntable.MemberCreateOp{Row: "s1", Col: sprint.Ctl, Score: 0},
		Set:    map[string]string{"kind": "stream", "stream": "s1"},
	})

	head := "fa6d1991"

	// Seed primary card p in review at attempt 4, head H, needing 2 reads (tier pro)
	pinned, err := h.st.pin(h.ctx)
	require.NoError(t, err)
	_, err = pinned.B.Apply(h.ctx, ntable.BatchManifest{
		Schema: 1, Table: pinned.Names.Table(sprint.Work), Epoch: fmt.Sprint(pinned.epoch),
		ExpectedTableRevision: fmt.Sprint(h.snap().Work.Revision), OperationID: "seed-primary-p",
		Members: []ntable.BatchMemberEntry{
			{
				ID:     "p",
				Expect: &ntable.MemberExpect{Absent: true},
				Create: &ntable.MemberCreateOp{Row: "s1", Col: sprint.Review, Score: 1},
				Set: map[string]string{
					"kind":    "primary",
					"stream":  "s1",
					"tier":    "pro",
					"attempt": "4",
					"head":    head,
				},
			},
		},
	})
	require.NoError(t, err)

	// Seed read cards in fleet table:
	// p.r4.vision retired, verdict ok, head H
	// p.r4.superman retired by environment, no verdict
	// p.r4.superman.g1 retired, verdict ok, head H
	pinned, err = h.st.pin(h.ctx)
	require.NoError(t, err)
	_, err = pinned.B.Apply(h.ctx, ntable.BatchManifest{
		Schema: 1, Table: pinned.Names.Table(sprint.Fleet), Epoch: fmt.Sprint(pinned.epoch),
		ExpectedTableRevision: fmt.Sprint(h.snap().Fleet.Revision), OperationID: "seed-read-cards",
		Members: []ntable.BatchMemberEntry{
			{
				ID:     "p.r4.vision",
				Expect: &ntable.MemberExpect{Absent: true},
				Create: &ntable.MemberCreateOp{Row: "vision", Col: sprint.DoneOK, Score: 1},
				Set: map[string]string{
					"kind":       "read",
					"primary":    "p",
					"attempt":    "4",
					"reader":     "vision",
					"head":       head,
					"verdict":    "ok",
					"retired_by": "read",
				},
			},
			{
				ID:     "p.r4.superman",
				Expect: &ntable.MemberExpect{Absent: true},
				Create: &ntable.MemberCreateOp{Row: "superman", Col: sprint.DoneOK, Score: 1},
				Set: map[string]string{
					"kind":       "read",
					"primary":    "p",
					"attempt":    "4",
					"reader":     "superman",
					"head":       head,
					"retired_by": "environment",
				},
			},
			{
				ID:     "p.r4.superman.g1",
				Expect: &ntable.MemberExpect{Absent: true},
				Create: &ntable.MemberCreateOp{Row: "superman", Col: sprint.DoneOK, Score: 1},
				Set: map[string]string{
					"kind":       "read",
					"primary":    "p",
					"attempt":    "4",
					"reader":     "superman",
					"head":       head,
					"verdict":    "ok",
					"retired_by": "read",
				},
			},
		},
	})
	require.NoError(t, err)

	// Retire all three read cards off their rows
	pinned, err = h.st.pin(h.ctx)
	require.NoError(t, err)
	_, err = pinned.B.Apply(h.ctx, ntable.BatchManifest{
		Schema: 1, Table: pinned.Names.Table(sprint.Fleet), Epoch: fmt.Sprint(pinned.epoch),
		ExpectedTableRevision: fmt.Sprint(h.snap().Fleet.Revision), OperationID: "retire-read-cards",
		Members: []ntable.BatchMemberEntry{
			{ID: "p.r4.vision", Expect: &ntable.MemberExpect{Revision: "1"}, Remove: true},
			{ID: "p.r4.superman", Expect: &ntable.MemberExpect{Revision: "1"}, Remove: true},
			{ID: "p.r4.superman.g1", Expect: &ntable.MemberExpect{Revision: "1"}, Remove: true},
		},
	})
	require.NoError(t, err)

	// AcceptStep moves it to merging
	res, err := h.st.Run(h.ctx, AcceptStep(sprint.AcceptReq{Sel: sprint.Sel{IDs: []string{"p"}}}))
	require.NoError(t, err)
	require.Empty(t, res.Refused, "AcceptStep refused: %v", res.Refused)
	assert.Equal(t, sprint.Merging, h.state("p"))
}
