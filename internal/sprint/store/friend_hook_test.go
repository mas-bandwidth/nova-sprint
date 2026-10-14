package store

import (
	"context"
	"errors"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestFriendRowsUseHookAndHoldWithoutReportInference(t *testing.T) {
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 1}})
	require.NoError(t, err)
	require.NoError(t, h.st.SetFriendHeld(h.ctx, "amy", true, "tester", "rest", time.Time{}, 0))
	state := hook.Status{Name: "amy", State: hook.StateProven}
	reads := 0
	h.st.ReadFriendHooks = func(context.Context, ...string) ([]hook.Status, time.Time, error) {
		reads++
		return []hook.Status{state}, h.now, nil
	}
	_, err = h.st.FriendBeatReport(h.ctx, "amy", sprint.FriendReport{Running: []string{"job"}, Active: h.now, Until: h.now.Add(time.Hour), Reason: "limit"}, nil)
	require.NoError(t, err)
	rows, err := h.st.FriendRows(h.ctx, h.now.Add(24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, sprint.Held, rows[0].Status)
	require.Equal(t, "rest", rows[0].Reason)
	require.Equal(t, []string{"job"}, rows[0].Report.Running)
	state.State = hook.StateUnhooked
	state.Reason = "missed 2 challenges"
	rows, err = h.st.FriendRows(h.ctx, h.now)
	require.NoError(t, err)
	require.Equal(t, sprint.Down, rows[0].Status)
	require.Equal(t, state.Reason, rows[0].Reason)
	require.Equal(t, 2, reads, "each display reads the authoritative record")
	require.NoError(t, h.st.SetFriendHeld(h.ctx, "amy", false, "tester", "", time.Time{}, 0))
	h.st.ReadFriendHooks = nil
	rows, err = h.st.FriendRows(h.ctx, h.now)
	require.NoError(t, err)
	require.Equal(t, sprint.Down, rows[0].Status, "a work report never proves presence")
	h.st.ReadFriendHooks = func(context.Context, ...string) ([]hook.Status, time.Time, error) {
		return nil, time.Time{}, errors.New("unavailable")
	}
	_, err = h.st.FriendRows(h.ctx, h.now)
	require.ErrorContains(t, err, "friend hook record: unavailable")
}

func TestStoppedReconcileReadsHookWithoutAnyLiveReport(t *testing.T) {
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 1}})
	require.NoError(t, err)
	calls := 0
	h.st.ReadFriendHooks = func(context.Context, ...string) ([]hook.Status, time.Time, error) {
		calls++
		return []hook.Status{{Name: "amy", State: hook.StateUnhooked}}, h.now, nil
	}
	require.True(t, LiveReturnStep(nil).Friends)
	require.NoError(t, h.st.liveStopped(h.ctx, nil, &TickResult{}))
	require.Positive(t, calls, "STOPPED reads unhook even without a live report")
}

func TestRetiredWorkWithStopReceiptDoesNotKeepDebt(t *testing.T) {
	fleet := sprint.NewTable(sprint.Fleet)
	fleet.SetRows([]string{sprint.FriendRow("amy")})
	fleet.Put(&sprint.Card{ID: "work", Fields: map[string]string{"gen": "3", "stopped_from_gen": "2"}})
	s := &sprint.Snapshot{Fleet: fleet}
	require.False(t, stopOwned(s, StopLease{Table: sprint.Fleet, ID: "work", Gen: 2}))
	require.True(t, stopOwned(s, StopLease{Table: sprint.Fleet, ID: "work", Gen: 3}))
	fleet.Card("work").Fields["gen"] = "2"
	require.True(t, stopOwned(s, StopLease{Table: sprint.Fleet, ID: "work", Gen: 2}), "a receipt without a newer generation is not a return")
}

func TestUnhookedReadRetirementSettlesStoppedDebt(t *testing.T) {
	fleet := sprint.NewTable(sprint.Fleet)
	fleet.SetRows([]string{sprint.FriendRow("amy")})
	c := &sprint.Card{ID: "read.g2", Row: sprint.FriendRow("amy"), Col: sprint.Working, Fields: map[string]string{"kind": "read", "gen": "2"}}
	fleet.Put(c)
	s := &sprint.Snapshot{Now: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC), Fleet: fleet, Friends: []sprint.FriendSeat{{Name: "amy", Status: sprint.Down, Unhooked: true}}}
	debt := []StopLease{{Table: sprint.Fleet, ID: c.ID, Gen: 2}}
	require.Len(t, owedDebt(s, debt), 1)
	p := sprint.LiveReturns(s, nil, true)
	require.Len(t, p.Units, 1)
	for k, v := range p.Units[0].Changes[0].Entry.Set {
		c.Fields[k] = v
	}
	c.Row, c.Col = "", ""
	require.Empty(t, owedDebt(s, debt), "retired unhooked read must no longer block START")
}
