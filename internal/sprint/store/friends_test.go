package store

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FriendRows is the roster with width and status only: a friend's ready,
// working, ok and failed are her sprint cards' counts, filled by where from
// her fleet row (friend.<name>), never read or counted here — the store holds
// no job record.
func TestFriendRowsReturnsNameWidthStatusOnly(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 3}, {Name: "bob", Width: 1}})
	require.NoError(t, err)
	_, err = h.st.FriendBeat(h.ctx, "amy")
	require.NoError(t, err)
	h.up("amy")
	require.NoError(t, err)
	require.NoError(t, h.st.SetFriendHeld(h.ctx, "bob", true, "c", "", time.Time{}, 0))
	rows, err := h.st.FriendRows(h.ctx, h.now)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	// up first, then held (FleetOrder); the counts are all zero, never read; her beat's time
	// is carried (view coordinator reads how stale her report is), and the evidence her
	// status rests on, her session's pong, never her beat
	assert.Equal(t, []FriendRow{
		{Name: "amy", Width: 3, Status: sprint.Up, WorkerLive: true, Evidence: "nova-hook proven", Beat: h.now.UTC().Truncate(time.Second)},
		{Name: "bob", Width: 1, Status: sprint.Down, Evidence: "no hook record", SprintHeld: true},
	}, rows)
}

// up is a friend's beat and a wake ping her session answered, observed by the seat's
// holder at the clock now: a friend up, as the tests that deal to her want her.
func (h *harness) up(friend string) {
	h.t.Helper()
	prev := h.st.ReadFriendHooks
	h.st.ReadFriendHooks = func(ctx context.Context, names ...string) ([]hook.Status, time.Time, error) {
		by := map[string]hook.Status{}
		if prev != nil {
			rows, _, err := prev(ctx, names...)
			if err != nil {
				return nil, time.Time{}, err
			}
			for _, r := range rows {
				by[r.Name] = r
			}
		}
		by[friend] = hook.Status{Name: friend, Role: hook.Friend, State: hook.StateProven}
		var out []hook.Status
		for _, n := range names {
			if r, ok := by[n]; ok {
				out = append(out, r)
			}
		}
		return out, h.now, nil
	}
}

// A friend's delivery mode (batch or one-shot) is respected by the store's tick dealing on
// its twin (docs/SPEC-SPRINT.md section 1, "A friend's card"): in either mode she fills up to
// her width working and ready behind (DealAhead times width); a one-shot friend runs one lane
// per unit of width (the owner, 2026-10-10), and the lane a finish frees takes her next at once.
func TestTwinStoreDealingRespectsFriendDeliveryMode(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{
		{Name: "amy", Width: 2, Mode: "batch", Class: "flash"},
		{Name: "bob", Width: 2, Mode: "one-shot", Class: "flash"},
	})
	require.NoError(t, err)
	h.up("amy")
	h.up("bob")

	brief := func(who string) string {
		return "c: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: " + who + "\n\nThe task."
	}
	h.must(AddStep(sprint.AddReq{Stream: "s1", Cards: []sprint.CardAdd{
		{ID: "s1-1", Brief: brief("only friend amy")},
		{ID: "s1-2", Brief: brief("only friend amy")},
		{ID: "s1-3", Brief: brief("only friend amy")},
		{ID: "s1-4", Brief: brief("only friend amy")},
		{ID: "s1-5", Brief: brief("only friend bob")},
		{ID: "s1-6", Brief: brief("only friend bob")},
		{ID: "s1-7", Brief: brief("only friend bob")},
	}}))

	h.startMachine()
	h.machine()
	h.start("amy", 2) // each starts what her lanes hold: dealt ready, working once started
	h.start("bob", 2)

	snap := h.snap()
	amyRow := sprint.FriendRow("amy")
	bobRow := sprint.FriendRow("bob")
	assert.Equal(t, 2, snap.Fleet.Count(amyRow, sprint.Working))
	assert.Equal(t, 2, snap.Fleet.Count(amyRow, sprint.Ready))
	// Bob (one-shot mode, width 2): his three cards dealt, 2 working and 1 ready behind
	assert.Equal(t, 2, snap.Fleet.Count(bobRow, sprint.Working))
	assert.Equal(t, 1, snap.Fleet.Count(bobRow, sprint.Ready))

	// Bob finishes a card: the lane it frees takes his next at once
	h.must(FinishStep(sprint.FinishReq{
		As: bobRow, Sel: sprint.Sel{IDs: []string{"s1-5.w1"}},
		Gens: map[string]int{"s1-5.w1": 1}, Head: "abc",
	}))
	snap = h.snap()
	assert.Equal(t, 2, snap.Fleet.Count(bobRow, sprint.Working), "the freed lane took his next")
	assert.Equal(t, 0, snap.Fleet.Count(bobRow, sprint.Ready))
}

// A friend's row switching from batch to one-shot mode through config sync keeps her lanes:
// a one-shot friend runs one lane per unit of width (the owner, 2026-10-10), so each finish
// frees its own lane and her oldest ready card is taken into it at once.
func TestTwinStoreConfigSyncToOneShotKeepsEachLaneRefreshing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	// Amy begins in batch mode with width 2
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{
		{Name: "amy", Width: 2, Mode: "batch", Class: "flash"},
	})
	require.NoError(t, err)
	h.up("amy")

	brief := func(who string) string {
		return "c: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: " + who + "\n\nThe task."
	}
	h.must(AddStep(sprint.AddReq{Stream: "s1", Cards: []sprint.CardAdd{
		{ID: "s1-1", Brief: brief("only friend amy")},
		{ID: "s1-2", Brief: brief("only friend amy")},
		{ID: "s1-3", Brief: brief("only friend amy")},
		{ID: "s1-4", Brief: brief("only friend amy")},
	}}))

	h.startMachine()
	h.machine()
	h.start("amy", 2) // she starts what her lanes hold

	snap := h.snap()
	amyRow := sprint.FriendRow("amy")

	// Amy has 2 working, 2 ready behind
	assert.Equal(t, 2, snap.Fleet.Count(amyRow, sprint.Working))
	assert.Equal(t, 2, snap.Fleet.Count(amyRow, sprint.Ready))
	assert.Equal(t, sprint.Working, snap.StateOf("s1-1"))
	assert.Equal(t, sprint.Working, snap.StateOf("s1-2"))

	// Switch amy to one-shot mode through config sync
	_, _, updated, err := h.st.SyncFriends(h.ctx, []FriendSpec{
		{Name: "amy", Width: 2, Mode: "one-shot", Class: "flash"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"amy"}, updated)

	// Finish the first Working card (s1-1.w1)
	h.must(FinishStep(sprint.FinishReq{
		As: amyRow, Sel: sprint.Sel{IDs: []string{"s1-1.w1"}},
		Gens: map[string]int{"s1-1.w1": 1}, Head: "abc",
	}))

	snap = h.snap()
	// s1-2.w1 still runs and the lane s1-1 freed took s1-3.w1 at once
	assert.Equal(t, 2, snap.Fleet.Count(amyRow, sprint.Working))
	assert.Equal(t, 1, snap.Fleet.Count(amyRow, sprint.Ready))
	assert.Equal(t, sprint.Working, snap.Fleet.Card("s1-2.w1").Col)
	assert.Equal(t, sprint.Working, snap.Fleet.Card("s1-3.w1").Col)
	assert.Equal(t, sprint.Ready, snap.Fleet.Card("s1-4.w1").Col)

	h.must(FinishStep(sprint.FinishReq{
		As: amyRow, Sel: sprint.Sel{IDs: []string{"s1-2.w1"}},
		Gens: map[string]int{"s1-2.w1": 1}, Head: "def",
	}))
	snap = h.snap()
	assert.Equal(t, 2, snap.Fleet.Count(amyRow, sprint.Working), "the next freed lane took s1-4")
	assert.Equal(t, 0, snap.Fleet.Count(amyRow, sprint.Ready))
	assert.Equal(t, sprint.Working, snap.Fleet.Card("s1-4.w1").Col)
}

// Status Up is a presence fact shared by friend, coordinator and comms hooks. A name has one
// hook record, so a healthy proven friend, coordinator or comms record is the worker proof of
// its friend row (v1.3.19); an unproven, unhealthy, unhooked or role-less record is not.
func TestFriendSeatsCarryProvenWorkerEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		hook hook.Status
		live bool
	}{
		{"worker", hook.Status{Name: "amy", Role: hook.Friend, State: hook.StateProven}, true},
		{"coordinator", hook.Status{Name: "amy", Role: hook.Coordinator, State: hook.StateProven}, true},
		{"unproven coordinator", hook.Status{Name: "amy", Role: hook.Coordinator, State: hook.StateLive}, false},
		{"unhooked coordinator", hook.Status{Name: "amy", Role: hook.Coordinator, State: hook.StateUnhooked}, false},
		{"unhealthy coordinator", hook.Status{Name: "amy", Role: hook.Coordinator, State: hook.StateProven, Down: hook.NoAnswerReason}, false},
		{"comms", hook.Status{Name: "amy", Role: hook.Comms, State: hook.StateProven}, true},
		{"unknown role", hook.Status{Name: "amy", State: hook.StateProven}, false},
		{"unproven worker", hook.Status{Name: "amy", Role: hook.Friend, State: hook.StateLive}, false},
		{"unhealthy worker", hook.Status{Name: "amy", Role: hook.Friend, State: hook.StateProven, Down: hook.NoAnswerReason}, false},
		{"missing", hook.Status{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 1}})
			require.NoError(t, err)
			h.st.ReadFriendHooks = func(context.Context, ...string) ([]hook.Status, time.Time, error) {
				return []hook.Status{tc.hook}, h.now, nil
			}
			seats, err := h.st.FriendSeats(h.ctx, h.now)
			require.NoError(t, err)
			require.Len(t, seats, 1)
			assert.Equal(t, tc.live, seats[0].WorkerLive)
		})
	}
}

func TestCheckLoadsWorkerEvidenceForDealtFriendCards(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 1, Class: "flash"}})
	require.NoError(t, err)
	h.up("amy")
	h.must(AddStep(sprint.AddReq{Stream: "s1", Cards: []sprint.CardAdd{{ID: "s1-1", Brief: "c: worker evidence\nREPO: mas-bandwidth/nova-tools\nWHO: only friend amy\n\nThe task."}}}))
	h.startMachine()
	h.machine()
	require.NotNil(t, h.snap().Fleet.Placed("s1-1.w1"))
	rep, snap, err := h.st.Check(h.ctx, 1)
	require.NoError(t, err)
	require.Len(t, snap.Friends, 1)
	require.True(t, snap.Friends[0].WorkerLive)
	for _, v := range rep.Violations {
		require.NotContains(t, v.Detail, "no live worker")
	}
	h.st.ReadFriendHooks = func(context.Context, ...string) ([]hook.Status, time.Time, error) {
		return []hook.Status{{Name: "amy", Role: hook.Comms, State: hook.StateLive}}, h.now, nil
	}
	rep, _, err = h.st.Check(h.ctx, 1)
	require.NoError(t, err)
	var details []string
	for _, v := range rep.Violations {
		details = append(details, v.Detail)
	}
	assert.Contains(t, details, "work card s1-1.w1 is dealt to row friend.amy with no live worker")
}

// workerSeats constructs a coherent fake roster and worker hook for legacy
// tick fixtures that previously supplied only an Up seat to the core.
func (h *harness) workerSeats(seats []sprint.FriendSeat) []sprint.FriendSeat {
	h.t.Helper()
	specs, err := h.st.FriendSpecs(h.ctx)
	require.NoError(h.t, err)
	for i := range seats {
		f := &seats[i]
		found := false
		for j := range specs {
			if specs[j].Name == f.Name {
				specs[j].Width, specs[j].Class = f.Width, f.Class
				found = true
				break
			}
		}
		if !found {
			specs = append(specs, FriendSpec{Name: f.Name, Width: f.Width, Class: f.Class})
		}
		f.WorkerLive = f.Status == sprint.Up
	}
	_, _, _, err = h.st.SyncFriends(h.ctx, specs)
	require.NoError(h.t, err)
	for _, f := range seats {
		if f.WorkerLive {
			h.up(f.Name)
		}
	}
	return seats
}

// The coordinator's friend row is dealt work on the strength of the coordinator's own proven hook
// record (v1.3.19), on the in-memory twin with the clock an argument: the friend row is named
// for the sprint's coordinator, its one hook record is hooked as coordinator, and an unhooked
// record is dealt nothing.
func TestAFriendRowHookedAsCoordinatorIsDealtAWorkCardAndAnUnhookedOneIsNot(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		rec   hook.Status
		dealt bool
	}{
		{"coordinator record proven", hook.Status{Role: hook.Coordinator, State: hook.StateProven}, true},
		{"comms record proven", hook.Status{Role: hook.Comms, State: hook.StateProven}, true},
		{"unhooked", hook.Status{Role: hook.Coordinator, State: hook.StateUnhooked}, false},
		{"no record", hook.Status{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			coord := h.st.Actor // the harness acts as the sprint's coordinator
			_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: coord, Width: 32, Class: "flash"}})
			require.NoError(t, err)
			h.st.ReadFriendHooks = func(context.Context, ...string) ([]hook.Status, time.Time, error) {
				if tc.rec.State == "" {
					return nil, h.now, nil
				}
				r := tc.rec
				r.Name = coord
				return []hook.Status{r}, h.now, nil
			}
			h.must(AddStep(sprint.AddReq{Stream: "s1", Cards: []sprint.CardAdd{{ID: "s1-1", Brief: "c: coordinator row\nREPO: mas-bandwidth/nova-tools\nWHO: only friend " + coord + "\n\nThe task."}}}))
			h.startMachine()
			h.machine()
			wc := h.snap().Fleet.Placed("s1-1.w1")
			if !tc.dealt {
				assert.Nil(t, wc)
				return
			}
			require.NotNil(t, wc, "the coordinator's own proven row is dealt")
			assert.Equal(t, sprint.FriendRow(coord), wc.Row)
			rep, _, err := h.st.Check(h.ctx, 1)
			require.NoError(t, err)
			for _, v := range rep.Violations {
				require.NotContains(t, v.Detail, "no live worker")
			}
		})
	}
}
