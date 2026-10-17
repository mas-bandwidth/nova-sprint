package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFriendStatusWriteFixed pins the friend status writer's switch to the table form (S4,
// writers second): PublishFriendStatus writes each friend's status word as the padded standard
// base64 of the FriendStatus table wire (the variable form, friendrows.schema emits no fixed
// form), never the legacy JSON of the structs, and what it writes reads back equal through the
// dual reader. The writer switch proves old readers still work: the dual reader reads what the
// switched writer writes, and a legacy fixture of the same record still reads. The FriendStatus
// table declares no bounds (friendrows.schema: status, reason and evidence are open text), so
// no value is refused today; the writer still writes each value through the row codec, so a
// value the table cannot hold would be refused by name (FRIENDSTATUS REFUSED) before a byte is
// written, never written in the legacy form.
func TestFriendStatusWriteFixed(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{
		{Name: "amy", Width: 1}, {Name: "bob", Width: 1}, {Name: "cat", Width: 1},
	})
	require.NoError(t, err)
	hooks := map[string]hook.Status{
		"amy": {Name: "amy", Role: hook.Friend, State: hook.StateProven},
		"bob": {Name: "bob", Role: hook.Friend, State: hook.StateProven},
		"cat": {Name: "cat", Role: hook.Friend, State: hook.StateUnhooked, Reason: "missed 3 challenges"},
	}
	h.st.ReadFriendHooks = func(_ context.Context, names ...string) ([]hook.Status, time.Time, error) {
		var out []hook.Status
		for _, n := range names {
			if s, ok := hooks[n]; ok {
				out = append(out, s)
			}
		}
		return out, h.now, nil
	}
	require.NoError(t, h.st.SetFriendHeld(h.ctx, "bob", true, "c", "rest", time.Time{}, 0))

	kv := mustRootKV(t, h.st)
	// no record before the writer's pass; writing nothing changes nothing
	raw, ok, err := kv.GetKey(h.ctx, keyFriendStatus)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, h.st.PublishFriendStatus(h.ctx, h.now))

	// the stored record is the table form: a JSON object keyed by name, each value a JSON
	// string holding the padded standard base64 of the friend's FriendStatus wire
	raw, ok, err = kv.GetKey(h.ctx, keyFriendStatus)
	require.NoError(t, err)
	require.True(t, ok)
	var obj map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &obj))
	require.Len(t, obj, 3)
	for name, v := range obj {
		b := []byte(v)
		require.NotEmpty(t, b, "%s is stored", name)
		assert.Equal(t, byte('"'), b[0], "%s is a JSON string (table base64), not legacy JSON", name)
	}

	// what the writer wrote reads back equal through the dual reader
	stored, err := h.st.storedFriendStatus(h.ctx, kv)
	require.NoError(t, err)
	want, err := h.st.deriveFriendStatus(h.ctx, h.now, false)
	require.NoError(t, err)
	seen := map[string]friendStatus{}
	for name, fs := range want {
		fs.Why = ""
		seen[name] = fs
	}
	assert.Equal(t, seen, stored)
	assert.Equal(t, sprint.Up, stored["amy"].Status)
	assert.Equal(t, sprint.Held, stored["bob"].Status)
	assert.Equal(t, "rest", stored["bob"].Reason)
	assert.Equal(t, sprint.Down, stored["cat"].Status)
	assert.Equal(t, "missed 3 challenges", stored["cat"].Reason)

	// a legacy fixture of the same record still reads through the kept dual reader
	legacy := map[string]friendStatus{
		"bob": {Status: sprint.Held, Reason: "rest", Evidence: "held", At: h.now.UTC().Truncate(time.Second)},
	}
	lb, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(h.ctx, keyFriendStatus, string(lb)))
	readLegacy, err := h.st.storedFriendStatus(h.ctx, kv)
	require.NoError(t, err)
	assert.Equal(t, legacy, readLegacy)
}
