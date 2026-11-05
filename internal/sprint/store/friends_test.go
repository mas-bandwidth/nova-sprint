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
	_, err = h.st.FriendBeatReport(h.ctx, "amy", sprint.FriendReport{}, nil)
	require.NoError(t, err)
	h.up("amy")
	require.NoError(t, err)
	require.NoError(t, h.st.SetFriendHeld(h.ctx, "bob", true, "c", "", time.Time{}, 0))
	require.NoError(t, h.st.PublishFriendStatus(h.ctx, h.now)) // the server's pass
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

// A roster entry stored before 2026-11-05 may still carry the one-shot fields (mode,
// config_dir, session, token_cap, token_cap_set). They need no migration: the roster is
// read by json.Unmarshal into friendEntry, which has none of them, so the row reads as it
// would without them, and the next write of the roster (friend sync, friend up or down,
// hold) is the struct marshalled, so they are gone from the record from then on.
func TestAStoredRosterWithTheOneShotFieldsReadsAndDropsThem(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	kv, err := h.st.rootKV()
	require.NoError(t, err)
	old := `{"amy":{"width":2,"mode":"one-shot","config_dir":"/x","session":"one-shot","token_cap":500,"token_cap_set":true,"pronouns":"she/her"}}`
	require.NoError(t, kv.SetKey(h.ctx, keyFriends, old))
	rows, err := h.st.FriendRows(h.ctx, h.now)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "amy", rows[0].Name)
	assert.Equal(t, 2, rows[0].Width)
	assert.Equal(t, "she/her", rows[0].Pronouns)
	require.NoError(t, h.st.SetFriendHeld(h.ctx, "amy", true, "c", "", time.Time{}, 0))
	raw, ok, err := kv.GetKey(h.ctx, keyFriends)
	require.NoError(t, err)
	require.True(t, ok)
	for _, gone := range []string{"mode", "config_dir", "session", "token_cap"} {
		assert.NotContains(t, raw, `"`+gone, "the next write drops %s", gone)
	}
	assert.Contains(t, raw, `"pronouns":"she/her"`)
}
