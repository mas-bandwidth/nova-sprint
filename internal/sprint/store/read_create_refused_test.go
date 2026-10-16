package store

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestACreateOfAnExistingRecordIsRefusedAtOnce(t *testing.T) {
	t.Parallel()
	for _, epoch := range []uint64{0, 2} {
		t.Run(fmt.Sprintf("epoch %d", epoch), func(t *testing.T) {
			h := newHarness(t)
			for e := uint64(0); e < epoch; e++ {
				_, err := h.st.Clear(h.ctx)
				require.NoError(t, err)
			}
			require.Equal(t, epoch, h.snap().Epoch)
			h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 2}))
			h.must(AddStep(sprint.AddReq{Brief: "job tier: pro\n\nWork.", Stream: "s1", Count: 1}))

			// Attempt a step that tries to create s1-1 which already exists, expecting absent.
			step := Step{
				Verb:  "create-existing",
				Actor: "tester",
				Load:  tables(sprint.Work),
				Plan: func(s *sprint.Snapshot) sprint.Plan {
					return sprint.Plan{
						Units: []sprint.Unit{
							{
								Key:    "s1-1",
								Stream: "s1",
								Changes: []sprint.Change{
									{
										Table: sprint.Work,
										Entry: ntable.BatchMemberEntry{
											ID:     "s1-1",
											Expect: &ntable.MemberExpect{Absent: true},
											Create: &ntable.MemberCreateOp{Row: "s1", Col: sprint.Ready, Score: 1},
										},
									},
								},
							},
						},
					}
				},
			}
			res, err := h.st.Run(h.ctx, step)
			require.NoError(t, err)
			assert.Equal(t, 1, res.Attempts, "creating an existing record must not be retried as curable")
			assert.False(t, res.Lost, "creating an existing record must not be converted to 'the sprint kept changing'")
			require.NotEmpty(t, res.Refused)
			assert.Contains(t, res.Refused[0].Why, "MEMBEREXISTS")
		})
	}
}

func TestARetiredG1NeverBlocksTheDeal(t *testing.T) {
	t.Parallel()
	for _, epoch := range []uint64{0, 1} {
		t.Run(fmt.Sprintf("epoch %d", epoch), func(t *testing.T) {
			h := newHarness(t)
			for e := uint64(0); e < epoch; e++ {
				_, err := h.st.Clear(h.ctx)
				require.NoError(t, err)
			}
			h.startMachine()
			require.Equal(t, epoch, h.snap().Epoch)
			// Bring up m1 and m2 with reader rows
			h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 2}))
			h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m2", Width: 2}))
			// Put reader rows for m1 and m2 so they can read pro cards
			pinnedRds, err := h.st.pin(h.ctx)
			require.NoError(t, err)
			require.NoError(t, pinnedRds.B.RowsAdd(h.ctx, pinnedRds.Names.Table(sprint.Readers), []string{"reader-m1", "reader-m2"}))
			require.NoError(t, pinnedRds.B.RowSet(h.ctx, pinnedRds.Names.Table(sprint.Readers), "reader-m1", map[string]string{sprint.ReaderTiers: "pro,flash"}))
			require.NoError(t, pinnedRds.B.RowSet(h.ctx, pinnedRds.Names.Table(sprint.Readers), "reader-m2", map[string]string{sprint.ReaderTiers: "pro,flash"}))
			h.beat()

			// Add primary s1-1 (which will be put in review) and s1-2 (work card waiting in ready)
			h.must(AddStep(sprint.AddReq{Brief: "job1 tier: pro\n\nWork 1.", Stream: "s1", Count: 1}))
			h.must(AddStep(sprint.AddReq{Brief: "job2 tier: pro\n\nWork 2.", Stream: "s1", Count: 1}))

			// Deal s1-1 to m1 to work it
			h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
			wc := h.snap().Fleet.Card("s1-1.w1")
			require.NotNil(t, wc)
			h.work("m1")
			require.Equal(t, sprint.Review, h.state("s1-1"))

			// Pre-create s1-1.r1.m2.g1 in fleet as a retired record
			readID := "s1-1.r1.m2.g1"
			pinned, err := h.st.pin(h.ctx)
			require.NoError(t, err)
			_, err = pinned.B.Apply(h.ctx, ntable.BatchManifest{
				Schema: 1, Table: pinned.Names.Table(sprint.Fleet), Epoch: fmt.Sprint(pinned.epoch),
				ExpectedTableRevision: fmt.Sprint(h.snap().Fleet.Revision), OperationID: "seed-retired-g1",
				Members: []ntable.BatchMemberEntry{
					{
						ID:     sprint.StoredID(readID, pinned.epoch),
						Expect: &ntable.MemberExpect{Absent: true},
						Create: &ntable.MemberCreateOp{Row: "m2", Col: sprint.DoneOK, Score: 1},
						Set:    map[string]string{"kind": "read", "primary": "s1-1", "attempt": "1", "reader": "m2", "retired_by": "unstarted"},
					},
				},
			})
			require.NoError(t, err)
			pinned, err = h.st.pin(h.ctx)
			require.NoError(t, err)
			_, err = pinned.B.Apply(h.ctx, ntable.BatchManifest{
				Schema: 1, Table: pinned.Names.Table(sprint.Fleet), Epoch: fmt.Sprint(pinned.epoch),
				ExpectedTableRevision: fmt.Sprint(h.snap().Fleet.Revision), OperationID: "remove-retired-g1",
				Members: []ntable.BatchMemberEntry{
					{
						ID:     sprint.StoredID(readID, pinned.epoch),
						Expect: &ntable.MemberExpect{},
						Remove: true,
						Set:    map[string]string{"retired_by": "unstarted"},
					},
				},
			})
			require.NoError(t, err)

			// Also seed s1-1.r1.m2 (generation 0) as retired so the deal chooses .g1
			gen0ID := "s1-1.r1.m2"
			pinned, err = h.st.pin(h.ctx)
			require.NoError(t, err)
			_, err = pinned.B.Apply(h.ctx, ntable.BatchManifest{
				Schema: 1, Table: pinned.Names.Table(sprint.Fleet), Epoch: fmt.Sprint(pinned.epoch),
				ExpectedTableRevision: fmt.Sprint(h.snap().Fleet.Revision), OperationID: "seed-retired-g0",
				Members: []ntable.BatchMemberEntry{
					{
						ID:     sprint.StoredID(gen0ID, pinned.epoch),
						Expect: &ntable.MemberExpect{Absent: true},
						Create: &ntable.MemberCreateOp{Row: "m2", Col: sprint.DoneOK, Score: 1},
						Set:    map[string]string{"kind": "read", "primary": "s1-1", "attempt": "1", "reader": "m2", "retired_by": "unstarted"},
					},
				},
			})
			require.NoError(t, err)
			pinned, err = h.st.pin(h.ctx)
			require.NoError(t, err)
			_, err = pinned.B.Apply(h.ctx, ntable.BatchManifest{
				Schema: 1, Table: pinned.Names.Table(sprint.Fleet), Epoch: fmt.Sprint(pinned.epoch),
				ExpectedTableRevision: fmt.Sprint(h.snap().Fleet.Revision), OperationID: "remove-retired-g0",
				Members: []ntable.BatchMemberEntry{
					{
						ID:     sprint.StoredID(gen0ID, pinned.epoch),
						Expect: &ntable.MemberExpect{},
						Remove: true,
						Set:    map[string]string{"retired_by": "unstarted"},
					},
				},
			})
			require.NoError(t, err)

			// Now run the tick deal step. It must NOT fail or block:
			// s1-2 must be dealt to an up member!
			var due int
			step := TickPartStep("deal", sprint.TickDeal, sprint.TickReq{Who: sprint.MachineActor}, nil, nil, &due)
			res, err := h.st.Run(h.ctx, step)
			require.NoError(t, err)

			// The work card for s1-2 must have been dealt!
			wc2 := h.snap().Fleet.Card("s1-2.w1")
			assert.NotNil(t, wc2, "s1-2.w1 must be dealt even when a read card create was refused")

			// And the read for m2 is either dealt as .g2 or listed with its reason
			rc2 := h.snap().Fleet.Card("s1-1.r1.m2.g2")
			assert.True(t, rc2 != nil || len(res.Refused) > 0, "the read for m2 is either dealt as .g2 or listed with its reason")
		})
	}
}

func TestAReaderPastTheWindowIsSkippedNotRefused(t *testing.T) {
	t.Parallel()
	for _, epoch := range []uint64{0, 1} {
		t.Run(fmt.Sprintf("epoch %d", epoch), func(t *testing.T) {
			h := newHarness(t)
			for e := uint64(0); e < epoch; e++ {
				_, err := h.st.Clear(h.ctx)
				require.NoError(t, err)
			}
			h.startMachine()
			require.Equal(t, epoch, h.snap().Epoch)
			// Bring up m1 as worker, m2 as reader
			h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 2}))
			h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m2", Width: 2}))
			pinnedRds, err := h.st.pin(h.ctx)
			require.NoError(t, err)
			require.NoError(t, pinnedRds.B.RowsAdd(h.ctx, pinnedRds.Names.Table(sprint.Readers), []string{"reader-m2"}))
			require.NoError(t, pinnedRds.B.RowSet(h.ctx, pinnedRds.Names.Table(sprint.Readers), "reader-m2", map[string]string{sprint.ReaderTiers: cardhdr.RoutePro}))
			h.beat()

			// Add primary s1-1 in review
			h.must(AddStep(sprint.AddReq{Brief: "job1 tier: pro\n\nWork 1.", Stream: "s1", Count: 1}))
			h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
			wc := h.snap().Fleet.Card("s1-1.w1")
			require.NotNil(t, wc)
			h.work("m1")
			h.must(DrainStep())
			require.Equal(t, sprint.Review, h.state("s1-1"))

			// Seed m2 with retired records up to generation 5 (0, .g1, .g2, .g3, .g4, .g5)
			// which is past readCardLoadWindow = 4
			base := "s1-1.r1.m2"
			genIDs := []string{base, base + ".g1", base + ".g2", base + ".g3", base + ".g4", base + ".g5"}
			for i, gid := range genIDs {
				pinned, err := h.st.pin(h.ctx)
				require.NoError(t, err)
				_, err = pinned.B.Apply(h.ctx, ntable.BatchManifest{
					Schema: 1, Table: pinned.Names.Table(sprint.Fleet), Epoch: fmt.Sprint(pinned.epoch),
					ExpectedTableRevision: fmt.Sprint(h.snap().Fleet.Revision), OperationID: fmt.Sprintf("seed-gen-%d", i),
					Members: []ntable.BatchMemberEntry{
						{
							ID:     sprint.StoredID(gid, pinned.epoch),
							Expect: &ntable.MemberExpect{Absent: true},
							Create: &ntable.MemberCreateOp{Row: "m2", Col: sprint.DoneOK, Score: 1},
							Set:    map[string]string{"kind": "read", "primary": "s1-1", "attempt": "1", "reader": "m2", "retired_by": "unstarted"},
						},
					},
				})
				require.NoError(t, err)
				pinned, err = h.st.pin(h.ctx)
				require.NoError(t, err)
				_, err = pinned.B.Apply(h.ctx, ntable.BatchManifest{
					Schema: 1, Table: pinned.Names.Table(sprint.Fleet), Epoch: fmt.Sprint(pinned.epoch),
					ExpectedTableRevision: fmt.Sprint(h.snap().Fleet.Revision), OperationID: fmt.Sprintf("remove-gen-%d", i),
					Members: []ntable.BatchMemberEntry{
						{
							ID:     sprint.StoredID(gid, pinned.epoch),
							Expect: &ntable.MemberExpect{},
							Remove: true,
							Set:    map[string]string{"retired_by": "unstarted"},
						},
					},
				})
				require.NoError(t, err)
			}

			// Run the deal step
			var due int
			step := TickPartStep("deal", sprint.TickDeal, sprint.TickReq{Who: sprint.MachineActor}, nil, nil, &due)
			res, err := h.st.Run(h.ctx, step)
			require.NoError(t, err, "never a step refusal when a reader is past the window")

			// The reader m2 must be skipped, not dealt a read card past the window
			for _, m := range res.Moved {
				assert.NotContains(t, m, "s1-1 asked of m2")
			}

			// A plan refusal reason indicates past the load window
			hasWindowRefusal := false
			for _, r := range res.Refused {
				if r.Why == "read generations past the load window" {
					hasWindowRefusal = true
					break
				}
			}
			assert.True(t, hasWindowRefusal, "must have a plan refusal with 'read generations past the load window'")
		})
	}
}

func TestADealDropsRefusedReadCreateWhenTableRefuses(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	// Bring up m1 and m2 so their rows exist in fleet
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 2}))
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m2", Width: 2}))

	// Seed a retired read card in the table at epoch 0:
	readID := "s1-1.r1.m2.g1"
	pinned, err := h.st.pin(h.ctx)
	require.NoError(t, err)
	_, err = pinned.B.Apply(h.ctx, ntable.BatchManifest{
		Schema: 1, Table: pinned.Names.Table(sprint.Fleet), Epoch: fmt.Sprint(pinned.epoch),
		ExpectedTableRevision: fmt.Sprint(h.snap().Fleet.Revision), OperationID: "seed-retired",
		Members: []ntable.BatchMemberEntry{
			{
				ID:     sprint.StoredID(readID, pinned.epoch),
				Expect: &ntable.MemberExpect{Absent: true},
				Create: &ntable.MemberCreateOp{Row: "m2", Col: sprint.DoneOK, Score: 1},
				Set:    map[string]string{"kind": "read", "primary": "s1-1", "attempt": "1", "reader": "m2", "retired_by": "unstarted"},
			},
		},
	})
	require.NoError(t, err)
	pinned, err = h.st.pin(h.ctx)
	require.NoError(t, err)
	_, err = pinned.B.Apply(h.ctx, ntable.BatchManifest{
		Schema: 1, Table: pinned.Names.Table(sprint.Fleet), Epoch: fmt.Sprint(pinned.epoch),
		ExpectedTableRevision: fmt.Sprint(h.snap().Fleet.Revision), OperationID: "remove-retired",
		Members: []ntable.BatchMemberEntry{
			{
				ID:     sprint.StoredID(readID, pinned.epoch),
				Expect: &ntable.MemberExpect{},
				Remove: true,
				Set:    map[string]string{"retired_by": "unstarted"},
			},
		},
	})
	require.NoError(t, err)

	// Now construct a Step with DropRefusedReadCreates = true
	// whose plan attempts to create readID (expecting absent) AND deals work card s1-2.w1
	step := Step{
		Verb:                   "tick deal",
		Actor:                  sprint.MachineActor,
		Load:                   tables(sprint.Fleet, sprint.Work),
		DropRefusedReadCreates: true,
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			return sprint.Plan{
				Units: []sprint.Unit{
					{
						Key:   "s1-1",
						Moved: "s1-1 asked of m2 (read cards)",
						Changes: []sprint.Change{
							{
								Table: sprint.Fleet,
								Entry: ntable.BatchMemberEntry{
									ID:     readID,
									Expect: &ntable.MemberExpect{Absent: true},
									Create: &ntable.MemberCreateOp{Row: "m2", Col: sprint.Ready, Score: 1},
									Set:    map[string]string{"kind": "read", sprint.FieldReadCard: "1", "primary": "s1-1"},
								},
							},
						},
					},
					{
						Key:   "s1-2",
						Moved: "s1-2 dealt to m1",
						Changes: []sprint.Change{
							{
								Table: sprint.Fleet,
								Entry: ntable.BatchMemberEntry{
									ID:     "s1-2.w1",
									Expect: &ntable.MemberExpect{Absent: true},
									Create: &ntable.MemberCreateOp{Row: "m1", Col: sprint.Ready, Score: 1},
									Set:    map[string]string{"kind": "work", "primary": "s1-2"},
								},
							},
						},
					},
				},
			}
		},
	}
	res, err := h.st.Run(h.ctx, step)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Attempts)
	assert.False(t, res.Lost)

	// The work card s1-2.w1 was dealt!
	wc := h.snap().Fleet.Card("s1-2.w1")
	assert.NotNil(t, wc, "work card s1-2.w1 must be dealt")

	// And the refused read card is recorded in Refused
	require.NotEmpty(t, res.Refused)
	assert.Equal(t, "s1-1", res.Refused[0].Key)
	assert.Contains(t, res.Refused[0].Why, "MEMBEREXISTS")

	// And Moved contains only the work card
	assert.Equal(t, []string{"s1-2 dealt to m1"}, res.Moved)
}

func TestADealDropsRefusedReadCreateAtNonzeroEpoch(t *testing.T) {
	t.Parallel()
	for _, epoch := range []uint64{1, 2} {
		t.Run(fmt.Sprintf("epoch %d", epoch), func(t *testing.T) {
			h := newHarness(t)
			// Advance to epoch 1 or later before the deal setup
			for e := uint64(0); e < epoch; e++ {
				_, err := h.st.Clear(h.ctx)
				require.NoError(t, err)
			}
			require.Equal(t, epoch, h.snap().Epoch)

			// Bring up m1 and m2 so their rows exist in fleet at the advanced epoch
			h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 2}))
			h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m2", Width: 2}))

			// Seed a retired read card in the table at the advanced epoch (stored ID has ~<epoch>):
			readID := "s1-1.r1.m2.g1"
			pinned, err := h.st.pin(h.ctx)
			require.NoError(t, err)
			require.Equal(t, epoch, pinned.epoch)
			storedReadID := sprint.StoredID(readID, pinned.epoch)
			require.Equal(t, fmt.Sprintf("%s~%d", readID, epoch), storedReadID)

			_, err = pinned.B.Apply(h.ctx, ntable.BatchManifest{
				Schema: 1, Table: pinned.Names.Table(sprint.Fleet), Epoch: fmt.Sprint(pinned.epoch),
				ExpectedTableRevision: fmt.Sprint(h.snap().Fleet.Revision), OperationID: "seed-retired",
				Members: []ntable.BatchMemberEntry{
					{
						ID:     storedReadID,
						Expect: &ntable.MemberExpect{Absent: true},
						Create: &ntable.MemberCreateOp{Row: "m2", Col: sprint.DoneOK, Score: 1},
						Set:    map[string]string{"kind": "read", "primary": "s1-1", "attempt": "1", "reader": "m2", "retired_by": "unstarted"},
					},
				},
			})
			require.NoError(t, err)
			pinned, err = h.st.pin(h.ctx)
			require.NoError(t, err)
			_, err = pinned.B.Apply(h.ctx, ntable.BatchManifest{
				Schema: 1, Table: pinned.Names.Table(sprint.Fleet), Epoch: fmt.Sprint(pinned.epoch),
				ExpectedTableRevision: fmt.Sprint(h.snap().Fleet.Revision), OperationID: "remove-retired",
				Members: []ntable.BatchMemberEntry{
					{
						ID:     storedReadID,
						Expect: &ntable.MemberExpect{},
						Remove: true,
						Set:    map[string]string{"retired_by": "unstarted"},
					},
				},
			})
			require.NoError(t, err)

			// Now construct a Step with DropRefusedReadCreates = true
			// whose plan attempts to create readID (expecting absent) AND deals work card s1-2.w1.
			// In the plan, IDs are plain (as generated by plan functions before engine adds ~epoch).
			step := Step{
				Verb:                   "tick deal",
				Actor:                  sprint.MachineActor,
				Load:                   tables(sprint.Fleet, sprint.Work),
				DropRefusedReadCreates: true,
				Plan: func(s *sprint.Snapshot) sprint.Plan {
					return sprint.Plan{
						Units: []sprint.Unit{
							{
								Key:   "s1-1",
								Moved: "s1-1 asked of m2 (read cards)",
								Changes: []sprint.Change{
									{
										Table: sprint.Fleet,
										Entry: ntable.BatchMemberEntry{
											ID:     readID,
											Expect: &ntable.MemberExpect{Absent: true},
											Create: &ntable.MemberCreateOp{Row: "m2", Col: sprint.Ready, Score: 1},
											Set:    map[string]string{"kind": "read", sprint.FieldReadCard: "1", "primary": "s1-1"},
										},
									},
								},
							},
							{
								Key:   "s1-2",
								Moved: "s1-2 dealt to m1",
								Changes: []sprint.Change{
									{
										Table: sprint.Fleet,
										Entry: ntable.BatchMemberEntry{
											ID:     "s1-2.w1",
											Expect: &ntable.MemberExpect{Absent: true},
											Create: &ntable.MemberCreateOp{Row: "m1", Col: sprint.Ready, Score: 1},
											Set:    map[string]string{"kind": "work", "primary": "s1-2"},
										},
									},
								},
							},
						},
					}
				},
			}
			res, err := h.st.Run(h.ctx, step)
			require.NoError(t, err)
			assert.Equal(t, 1, res.Attempts)
			assert.False(t, res.Lost)

			// The work card s1-2.w1 was dealt at the advanced epoch!
			wc := h.snap().Fleet.Card("s1-2.w1")
			assert.NotNil(t, wc, fmt.Sprintf("work card s1-2.w1 must be dealt at epoch %d", epoch))

			// And the refused read card is recorded in Refused
			require.NotEmpty(t, res.Refused)
			assert.Equal(t, "s1-1", res.Refused[0].Key)
			assert.Contains(t, res.Refused[0].Why, "MEMBEREXISTS")

			// And Moved contains only the work card
			assert.Equal(t, []string{"s1-2 dealt to m1"}, res.Moved)
		})
	}
}
