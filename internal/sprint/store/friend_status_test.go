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

// The server writes each friend's status word and FriendRows shows exactly that word: the
// precedence is down beats held beats up, and a reader that has no hook record to read, or a
// hook record that now says otherwise, still shows what the server wrote (the clock is an
// argument, no real time). tla/SeatHealth.tla OneWriter.
func TestFriendRowsShowTheWordTheServerWroteAndNeverDeriveOne(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 1}, {Name: "bob", Width: 1}, {Name: "cat", Width: 1}, {Name: "dee", Width: 1}})
	require.NoError(t, err)
	hooks := map[string]hook.Status{
		"amy": {Name: "amy", Role: hook.Friend, State: hook.StateProven},
		"bob": {Name: "bob", Role: hook.Friend, State: hook.StateProven},
		"dee": {Name: "dee", Role: hook.Friend, State: hook.StateUnhooked, Reason: "missed 2 challenges"},
	}
	reads := 0
	h.st.ReadFriendHooks = func(_ context.Context, names ...string) ([]hook.Status, time.Time, error) {
		reads++
		var out []hook.Status
		for _, n := range names {
			if s, ok := hooks[n]; ok {
				out = append(out, s)
			}
		}
		return out, h.now, nil
	}
	require.NoError(t, h.st.SetFriendHeld(h.ctx, "bob", true, "c", "rest", time.Time{}, 0))
	require.NoError(t, h.st.SetFriendHeld(h.ctx, "dee", true, "c", "away", time.Time{}, 0))

	words := func() map[string]string {
		rows, err := h.st.FriendRows(h.ctx, h.now)
		require.NoError(t, err)
		out := map[string]string{}
		for _, r := range rows {
			out[r.Name] = r.Status
		}
		return out
	}

	// before the server's first pass there is no word: down, never a guess from the hook
	assert.Equal(t, map[string]string{"amy": sprint.Down, "bob": sprint.Down, "cat": sprint.Down, "dee": sprint.Down}, words(), "no word written is down")
	assert.Zero(t, reads, "a display reads no hook record")

	require.NoError(t, h.st.PublishFriendStatus(h.ctx, h.now))
	assert.Equal(t, 1, reads, "the server's pass reads the hook record once for all")
	want := map[string]string{"amy": sprint.Up, "bob": sprint.Held, "cat": sprint.Down, "dee": sprint.Down}
	assert.Equal(t, want, words(), "up is up; up and held is held; down is down, held or not")

	// the hook records change and the hold is released, and no display notices: only the
	// server's next pass writes the word
	hooks["amy"] = hook.Status{Name: "amy", Role: hook.Friend, State: hook.StateUnhooked}
	require.NoError(t, h.st.SetFriendHeld(h.ctx, "bob", false, "c", "", time.Time{}, 0))
	h.st.ReadFriendHooks = nil
	assert.Equal(t, want, words(), "a reader shows the server's word until the server writes another")

	h.now = h.now.Add(time.Second)
	hooks["amy"] = hook.Status{Name: "amy", Role: hook.Friend, State: hook.StateProven}
	h.st.ReadFriendHooks = func(_ context.Context, names ...string) ([]hook.Status, time.Time, error) {
		var out []hook.Status
		for _, n := range names {
			if s, ok := hooks[n]; ok {
				out = append(out, s)
			}
		}
		return out, h.now, nil
	}
	require.NoError(t, h.st.PublishFriendStatus(h.ctx, h.now))
	assert.Equal(t, map[string]string{"amy": sprint.Up, "bob": sprint.Up, "cat": sprint.Down, "dee": sprint.Down}, words())

	rows, err := h.st.FriendRows(h.ctx, h.now)
	require.NoError(t, err)
	for _, r := range rows {
		if r.Name == "dee" {
			assert.Equal(t, "missed 2 challenges", r.Reason, "a down friend's reason is the hook's, held or not")
		}
	}
}

// A friend taken off the roster leaves the server's record with the next pass, and a friend
// added shows down until it has written her word.
func TestThePublishedStatusFollowsTheRoster(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 1}})
	require.NoError(t, err)
	h.up("amy")
	require.NoError(t, h.st.PublishFriendStatus(h.ctx, h.now))
	_, _, _, err = h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 1}, {Name: "bob", Width: 1}})
	require.NoError(t, err)
	rows, err := h.st.FriendRows(h.ctx, h.now)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, sprint.Up, rows[0].Status)
	assert.Equal(t, sprint.Down, rows[1].Status, "bob has no word yet")
	assert.Equal(t, NoFriendStatus, rows[1].Evidence)
	_, _, _, err = h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "bob", Width: 1}})
	require.NoError(t, err)
	require.NoError(t, h.st.PublishFriendStatus(h.ctx, h.now))
	stored, err := h.st.storedFriendStatus(h.ctx, mustRootKV(t, h.st))
	require.NoError(t, err)
	assert.Contains(t, stored, "bob")
	assert.NotContains(t, stored, "amy")
}

func mustRootKV(t *testing.T, st *Store) KV {
	t.Helper()
	kv, err := st.rootKV()
	require.NoError(t, err)
	return kv
}
