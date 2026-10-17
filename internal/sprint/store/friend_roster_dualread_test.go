package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFriendRosterRowsDualRead pins the reader-first dual read of the friend
// roster and presence rows (S4, C13): every record reads alike from its table
// wire image, the padded standard base64 of that image, and today's legacy
// form, while writers still write the legacy form; a malformed value of each
// form is refused by name, never read as absent; reads are counted per record
// and per form. It runs on the in-memory backend only (store.NewMem).
func TestFriendRosterRowsDualRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "friend-dual-read-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	pinned, err := st.pin(ctx)
	require.NoError(t, err)
	kv, err := pinned.kv()
	require.NoError(t, err)

	// The roster entry (friendEntry).
	entry := friendEntry{Held: true, At: now, By: "coord", Width: 2, Class: "a", Mode: "batch", TokenCap: 100, TokenCapSet: true, Roles: "reader", Reason: "hold", Return: true}
	entryWire, err := friendEntryWire(entry)
	require.NoError(t, err)
	entryB64 := base64.StdEncoding.EncodeToString(entryWire)
	entryLegacy, err := json.Marshal(entry)
	require.NoError(t, err)

	got, err := decodeFriendEntry(entryWire)
	require.NoError(t, err)
	assert.Equal(t, entry, got)
	got, err = decodeFriendEntry([]byte(entryB64))
	require.NoError(t, err)
	assert.Equal(t, entry, got)
	got, err = decodeFriendEntry(entryLegacy)
	require.NoError(t, err)
	assert.Equal(t, entry, got)

	_, err = decodeFriendEntry(entryWire[:len(entryWire)-3])
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FRIENDENTRY REFUSED")

	// The friend status record (friendStatus).
	fs := friendStatus{Status: sprint.Up, Evidence: "nova-hook proven", WorkerLive: true, At: now}
	fsWire, err := friendStatusWire(fs)
	require.NoError(t, err)
	fsB64 := base64.StdEncoding.EncodeToString(fsWire)
	fsLegacy, err := json.Marshal(fs)
	require.NoError(t, err)

	gotFS, err := decodeFriendStatus(fsWire)
	require.NoError(t, err)
	assert.Equal(t, fs, gotFS)
	gotFS, err = decodeFriendStatus([]byte(fsB64))
	require.NoError(t, err)
	assert.Equal(t, fs, gotFS)
	gotFS, err = decodeFriendStatus(fsLegacy)
	require.NoError(t, err)
	assert.Equal(t, fs, gotFS)

	_, err = decodeFriendStatus(fsWire[:len(fsWire)-2])
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FRIENDSTATUS REFUSED")

	// The beat (sprint.Beat).
	beat := sprint.Beat{At: now, Load: 55.5, How: sprint.HowGiven, Friend: &sprint.FriendReport{Running: []string{"c1"}}}
	beatWire, err := beatWire(beat)
	require.NoError(t, err)
	beatB64 := base64.StdEncoding.EncodeToString(beatWire)
	beatLegacy, err := json.Marshal(beat)
	require.NoError(t, err)

	gotB, err := decodeBeat(beatWire)
	require.NoError(t, err)
	assert.Equal(t, beat, gotB)
	gotB, err = decodeBeat([]byte(beatB64))
	require.NoError(t, err)
	assert.Equal(t, beat, gotB)
	gotB, err = decodeBeat(beatLegacy)
	require.NoError(t, err)
	assert.Equal(t, beat, gotB)

	_, err = decodeBeat(beatWire[:len(beatWire)-1])
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BEAT REFUSED")

	// Reads count per form (S4).
	report, _, err := st.Check(ctx, 1)
	require.NoError(t, err)
	require.Contains(t, report.RowForms, "FRIENDENTRY")
	require.Contains(t, report.RowForms, "FRIENDSTATUS")
	require.Contains(t, report.RowForms, "BEAT")
	assert.EqualValues(t, 2, report.RowForms["FRIENDENTRY"]["binary"])
	assert.EqualValues(t, 1, report.RowForms["FRIENDENTRY"]["legacy"])
	assert.EqualValues(t, 2, report.RowForms["FRIENDSTATUS"]["binary"])
	assert.EqualValues(t, 1, report.RowForms["FRIENDSTATUS"]["legacy"])
	assert.EqualValues(t, 2, report.RowForms["BEAT"]["binary"])
	assert.EqualValues(t, 1, report.RowForms["BEAT"]["legacy"])
}
