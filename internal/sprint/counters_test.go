package sprint_test

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stamp is RFC3339 time format.
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// TestCountersGiveIPCAndStallReasonsThatSumToWallTime verifies on a twin-store fixture
// (store.NewMem, store.Store) of known cards that:
// 1. Each card's stall parts sum to its wall time.
// 2. Each card's top stall reason matches the hand count.
// 3. Overall IPC and stream IPC equal the exact hand count (landed cards per slot-hour).
// 4. Overall top stall reason and stream top stall reason equal the hand count.
func TestCountersGiveIPCAndStallReasonsThatSumToWallTime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := store.NewMem()
	t0 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	now := t0.Add(40 * time.Second)

	st := &store.Store{
		B:     m,
		Names: sprint.Names{Prefix: "t-"},
		Actor: "coordinator",
		Now:   func() time.Time { return now },
		NewID: func() string { return "1" },
		Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	require.NoError(t, m.RowsAdd(ctx, "t-readers", []string{"reader-a", "reader-b"}))
	require.NoError(t, m.SetCoordinator(ctx, "coordinator"))

	// Fleet: 2 members of width 2 each = 4 slots total
	require.NoError(t, m.RowsAdd(ctx, "t-fleet", []string{"m1", "m2"}))
	require.NoError(t, m.RowsAdd(ctx, "t-work", []string{"s1", "s2"}))

	snap, err := st.Load(ctx, store.All, nil)
	require.NoError(t, err)

	// Set up fleet members as up with width 2
	m1Ctl := &sprint.Card{
		ID:     sprint.CtlID("m1"),
		Row:    "m1",
		Col:    sprint.Up,
		Fields: map[string]string{sprint.Status: sprint.Up, sprint.FieldWidth: "2"},
	}
	snap.Fleet.Put(m1Ctl)

	m2Ctl := &sprint.Card{
		ID:     sprint.CtlID("m2"),
		Row:    "m2",
		Col:    sprint.Up,
		Fields: map[string]string{sprint.Status: sprint.Up, sprint.FieldWidth: "2"},
	}
	snap.Fleet.Put(m2Ctl)

	require.Equal(t, 4, sprint.TotalFleetSlots(snap), "fleet slots hand count: 2 + 2 = 4")

	type cardExpectation struct {
		card     *sprint.Card
		wallS    float64
		stalls   map[string]float64
		topStall string
	}

	stmap := func(m map[string]float64) map[string]float64 {
		out := map[string]float64{
			sprint.StallNeed:     0,
			sprint.StallRelease:  0,
			sprint.StallExternal: 0,
			sprint.StallSlot:     0,
			sprint.StallRead:     0,
			sprint.StallMerge:    0,
			sprint.StallRework:   0,
		}
		for k, v := range m {
			out[k] = v
		}
		return out
	}

	var cases []cardExpectation

	// Helper to add card to snapshot's Work table in Landed column
	addLanded := func(c *sprint.Card) {
		c.Col = sprint.Landed
		snap.Work.Put(c)
	}

	// 1. s1: c-need (waits on need for 10s)
	// admitted: t0, ready: t0+10s, taken: t0+10s, finished: t0+10s, accepted: t0+10s, queued: t0+10s, landed: t0+10s
	cNeed := &sprint.Card{
		ID:  "c-need",
		Row: "s1",
		Fields: map[string]string{
			"admitted":              stamp(t0),
			sprint.FieldReadyAt:     stamp(t0.Add(10 * time.Second)),
			sprint.FieldTakenAt:     stamp(t0.Add(10 * time.Second)),
			sprint.FieldFinishedAt:  stamp(t0.Add(10 * time.Second)),
			"accepted":              stamp(t0.Add(10 * time.Second)),
			sprint.FieldReadAskedAt: stamp(t0.Add(10 * time.Second)),
			sprint.FieldReadDoneAt:  stamp(t0.Add(10 * time.Second)),
			"landed":                stamp(t0.Add(10 * time.Second)),
			"needs":                 "dep-0",
		},
	}
	addLanded(cNeed)
	cases = append(cases, cardExpectation{
		card:     cNeed,
		wallS:    10,
		stalls:   stmap(map[string]float64{sprint.StallNeed: 10}),
		topStall: sprint.StallNeed,
	})

	// 2. s1: c-release (waits for release for 20s)
	// admitted: t0, ready: t0+20s, taken: t0+20s, finished: t0+20s, accepted: t0+20s, queued: t0+20s, landed: t0+20s
	cRelease := &sprint.Card{
		ID:  "c-release",
		Row: "s1",
		Fields: map[string]string{
			"admitted":              stamp(t0),
			sprint.FieldReadyAt:     stamp(t0.Add(20 * time.Second)),
			sprint.FieldTakenAt:     stamp(t0.Add(20 * time.Second)),
			sprint.FieldFinishedAt:  stamp(t0.Add(20 * time.Second)),
			"accepted":              stamp(t0.Add(20 * time.Second)),
			sprint.FieldReadAskedAt: stamp(t0.Add(20 * time.Second)),
			sprint.FieldReadDoneAt:  stamp(t0.Add(20 * time.Second)),
			"landed":                stamp(t0.Add(20 * time.Second)),
			"wait_for":              "release",
		},
	}
	addLanded(cRelease)
	cases = append(cases, cardExpectation{
		card:     cRelease,
		wallS:    20,
		stalls:   stmap(map[string]float64{sprint.StallRelease: 20}),
		topStall: sprint.StallRelease,
	})

	// 3. s1: c-external (waits for external operand for 30s)
	// admitted: t0, ready: t0+30s, taken: t0+30s, finished: t0+30s, accepted: t0+30s, queued: t0+30s, landed: t0+30s
	cExternal := &sprint.Card{
		ID:  "c-external",
		Row: "s1",
		Fields: map[string]string{
			"admitted":              stamp(t0),
			sprint.FieldReadyAt:     stamp(t0.Add(30 * time.Second)),
			sprint.FieldTakenAt:     stamp(t0.Add(30 * time.Second)),
			sprint.FieldFinishedAt:  stamp(t0.Add(30 * time.Second)),
			"accepted":              stamp(t0.Add(30 * time.Second)),
			sprint.FieldReadAskedAt: stamp(t0.Add(30 * time.Second)),
			sprint.FieldReadDoneAt:  stamp(t0.Add(30 * time.Second)),
			"landed":                stamp(t0.Add(30 * time.Second)),
			"wait_for":              "external",
		},
	}
	addLanded(cExternal)
	cases = append(cases, cardExpectation{
		card:     cExternal,
		wallS:    30,
		stalls:   stmap(map[string]float64{sprint.StallExternal: 30}),
		topStall: sprint.StallExternal,
	})

	// 4. s1: c-slot (waits for slot for 15s)
	// admitted: t0, ready: t0, taken: t0+15s, finished: t0+15s, accepted: t0+15s, queued: t0+15s, landed: t0+15s
	cSlot := &sprint.Card{
		ID:  "c-slot",
		Row: "s1",
		Fields: map[string]string{
			"admitted":              stamp(t0),
			sprint.FieldReadyAt:     stamp(t0),
			sprint.FieldTakenAt:     stamp(t0.Add(15 * time.Second)),
			sprint.FieldFinishedAt:  stamp(t0.Add(15 * time.Second)),
			"accepted":              stamp(t0.Add(15 * time.Second)),
			sprint.FieldReadAskedAt: stamp(t0.Add(15 * time.Second)),
			sprint.FieldReadDoneAt:  stamp(t0.Add(15 * time.Second)),
			"landed":                stamp(t0.Add(15 * time.Second)),
		},
	}
	addLanded(cSlot)
	cases = append(cases, cardExpectation{
		card:     cSlot,
		wallS:    15,
		stalls:   stmap(map[string]float64{sprint.StallSlot: 15}),
		topStall: sprint.StallSlot,
	})

	// 5. s1: c-multi (waits on need 5s, slot 10s, read 7s, merge 3s = wall 25s)
	cMulti := &sprint.Card{
		ID:  "c-multi",
		Row: "s1",
		Fields: map[string]string{
			"admitted":              stamp(t0),
			sprint.FieldReadyAt:     stamp(t0.Add(5 * time.Second)),
			sprint.FieldTakenAt:     stamp(t0.Add(15 * time.Second)),
			sprint.FieldFinishedAt:  stamp(t0.Add(15 * time.Second)),
			"accepted":              stamp(t0.Add(22 * time.Second)),
			sprint.FieldReadAskedAt: stamp(t0.Add(15 * time.Second)),
			sprint.FieldReadDoneAt:  stamp(t0.Add(22 * time.Second)),
			"landed":                stamp(t0.Add(25 * time.Second)),
			"needs":                 "dep-multi",
		},
	}
	addLanded(cMulti)
	cases = append(cases, cardExpectation{
		card:     cMulti,
		wallS:    25,
		stalls:   stmap(map[string]float64{sprint.StallNeed: 5, sprint.StallSlot: 10, sprint.StallRead: 7, sprint.StallMerge: 3}),
		topStall: sprint.StallSlot,
	})

	// 6. s2: c-read (waits in read for 25s)
	// admitted: t0, ready: t0, taken: t0, finished: t0, accepted: t0+25s, queued: t0+25s, landed: t0+25s
	cRead := &sprint.Card{
		ID:  "c-read",
		Row: "s2",
		Fields: map[string]string{
			"admitted":              stamp(t0),
			sprint.FieldReadyAt:     stamp(t0),
			sprint.FieldTakenAt:     stamp(t0),
			sprint.FieldFinishedAt:  stamp(t0),
			"accepted":              stamp(t0.Add(25 * time.Second)),
			sprint.FieldReadAskedAt: stamp(t0),
			sprint.FieldReadDoneAt:  stamp(t0.Add(25 * time.Second)),
			"landed":                stamp(t0.Add(25 * time.Second)),
		},
	}
	addLanded(cRead)
	cases = append(cases, cardExpectation{
		card:     cRead,
		wallS:    25,
		stalls:   stmap(map[string]float64{sprint.StallRead: 25}),
		topStall: sprint.StallRead,
	})

	// 7. s2: c-merge (waits in merge for 12s)
	// admitted: t0, ready: t0, taken: t0, finished: t0, accepted: t0, queued: t0, landed: t0+12s
	cMerge := &sprint.Card{
		ID:  "c-merge",
		Row: "s2",
		Fields: map[string]string{
			"admitted":              stamp(t0),
			sprint.FieldReadyAt:     stamp(t0),
			sprint.FieldTakenAt:     stamp(t0),
			sprint.FieldFinishedAt:  stamp(t0),
			"accepted":              stamp(t0),
			sprint.FieldReadAskedAt: stamp(t0),
			sprint.FieldReadDoneAt:  stamp(t0),
			"landed":                stamp(t0.Add(12 * time.Second)),
		},
	}
	addLanded(cMerge)
	cases = append(cases, cardExpectation{
		card:     cMerge,
		wallS:    12,
		stalls:   stmap(map[string]float64{sprint.StallMerge: 12}),
		topStall: sprint.StallMerge,
	})

	// 8. s2: c-rework (waits in rework for 18s)
	// admitted: t0, ready: t0, rework_s: 18, taken: t0+18s, finished: t0+18s, accepted: t0+18s, landed: t0+18s
	cRework := &sprint.Card{
		ID:  "c-rework",
		Row: "s2",
		Fields: map[string]string{
			"admitted":              stamp(t0),
			sprint.FieldReadyAt:     stamp(t0),
			sprint.FieldReworkS:     "18",
			sprint.FieldTakenAt:     stamp(t0.Add(18 * time.Second)),
			sprint.FieldFinishedAt:  stamp(t0.Add(18 * time.Second)),
			"accepted":              stamp(t0.Add(18 * time.Second)),
			sprint.FieldReadAskedAt: stamp(t0.Add(18 * time.Second)),
			sprint.FieldReadDoneAt:  stamp(t0.Add(18 * time.Second)),
			"landed":                stamp(t0.Add(18 * time.Second)),
		},
	}
	addLanded(cRework)
	cases = append(cases, cardExpectation{
		card:     cRework,
		wallS:    18,
		stalls:   stmap(map[string]float64{sprint.StallRework: 18}),
		topStall: sprint.StallRework,
	})

	// Check each individual card: stall parts sum to its wall time!
	for _, tc := range cases {
		cc := sprint.CardStallBreakdown(snap, tc.card)
		assert.Equal(t, tc.wallS, cc.WallS, "%s: wall_s", tc.card.ID)
		assert.Equal(t, tc.stalls, cc.Stalls, "%s: stalls breakdown", tc.card.ID)
		assert.Equal(t, tc.topStall, cc.TopStall, "%s: top stall reason", tc.card.ID)

		sumParts := 0.0
		for _, sec := range cc.Stalls {
			sumParts += sec
		}
		assert.Equal(t, cc.WallS, sumParts, "%s: stall parts must sum exactly to wall time", tc.card.ID)
	}

	// Hand counts over the 1-hour window:
	// Total landed cards = 8 (5 in s1, 3 in s2)
	// Total fleet slots = 4
	// Window = 1 hour
	// Overall slot-hours = 4 * 1 = 4.0
	// Overall IPC = 8 / 4 = 2.0 cards / slot-hour
	// s1 IPC = 5 / 4 = 1.25 cards / slot-hour
	// s2 IPC = 3 / 4 = 0.75 cards / slot-hour
	// Sum of stalls overall:
	//   waiting on a need:             10 (cNeed) + 5 (cMulti) = 15s
	//   waiting on a release:          20 (cRelease)           = 20s
	//   waiting on an external operand: 30 (cExternal)          = 30s
	//   waiting for a slot:            15 (cSlot) + 10 (cMulti) = 25s
	//   in read:                       7 (cMulti) + 25 (cRead) = 32s
	//   in merge:                      3 (cMulti) + 12 (cMerge) = 15s
	//   in rework:                     18 (cRework)            = 18s
	// Overall top stall = "in read" (32s)
	// s1 top stall = "waiting on an external operand" (30s)
	// s2 top stall = "in read" (25s)

	counters := sprint.Counters(snap, now, 1*time.Hour)

	assert.Equal(t, 2.0, counters.IPC, "overall IPC: 8 landed / 4 slot-hours = 2.0")
	assert.Equal(t, sprint.StallRead, counters.TopStall, "overall top stall")
	assert.Len(t, counters.Cards, 8, "8 landed cards in window")

	wantOverallStalls := map[string]float64{
		sprint.StallNeed:     15,
		sprint.StallRelease:  20,
		sprint.StallExternal: 30,
		sprint.StallSlot:     25,
		sprint.StallRead:     32,
		sprint.StallMerge:    15,
		sprint.StallRework:   18,
	}
	assert.Equal(t, wantOverallStalls, counters.Stalls, "overall stalls sum")

	s1 := counters.Streams["s1"]
	assert.Equal(t, 1.25, s1.IPC, "s1 IPC: 5 / 4 = 1.25")
	assert.Equal(t, sprint.StallExternal, s1.TopStall, "s1 top stall")
	assert.Len(t, s1.Cards, 5)

	s2 := counters.Streams["s2"]
	assert.Equal(t, 0.75, s2.IPC, "s2 IPC: 3 / 4 = 0.75")
	assert.Equal(t, sprint.StallRead, s2.TopStall, "s2 top stall")
	assert.Len(t, s2.Cards, 3)
}
