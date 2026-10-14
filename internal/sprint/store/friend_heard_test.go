package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A friend's last-heard time (docs/SPEC-SPRINT.md section 1, a friend's health;
// sprint.FriendPhase; tla/FriendHeard.tla), on the twin store with the harness's clock:
// friend-heard:<f> only moves forward, is never later than the store's clock, is refused for
// a friend the roster lacks, is read into her row, and is set by each valid event the store
// itself sees of her (a finish, an answered wake ping, a beat that proves her session) and
// by no beat alone.

func (h *harness) heardRow(name string) FriendRow {
	h.t.Helper()
	rows, err := h.st.FriendRows(h.ctx, h.now)
	require.NoError(h.t, err)
	for _, r := range rows {
		if r.Name == name {
			return r
		}
	}
	require.Fail(h.t, "no row for "+name)
	return FriendRow{}
}

// FriendHeard is a monotonic max: an older time changes nothing; a time after the store's clock is
// the store's clock; a friend the roster lacks is refused and nothing is written.
func TestFriendHeardIsAMonotonicMaxClampedToTheClockAndRefusesAStranger(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 2}})
	require.NoError(t, err)

	at, err := h.st.FriendHeardAt(h.ctx, "amy")
	require.NoError(t, err)
	assert.True(t, at.IsZero(), "never heard")

	got, err := h.st.FriendHeard(h.ctx, "amy", h.now.Add(-5*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, h.now.Add(-5*time.Minute), got)
	got, err = h.st.FriendHeard(h.ctx, "amy", h.now.Add(-10*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, h.now.Add(-5*time.Minute), got, "an older time moves nothing")
	got, err = h.st.FriendHeard(h.ctx, "amy", h.now.Add(-time.Minute))
	require.NoError(t, err)
	assert.Equal(t, h.now.Add(-time.Minute), got, "a newer one moves it")
	got, err = h.st.FriendHeard(h.ctx, "amy", h.now.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, h.now, got, "never in the future of the store's clock: the clock now")
	got, err = h.st.FriendHeard(h.ctx, "amy", time.Time{})
	require.NoError(t, err)
	assert.Equal(t, h.now, got, "the zero time is none")

	_, err = h.st.FriendHeard(h.ctx, "zed", h.now)
	require.Error(t, err)
	assert.True(t, IsNoFriend(err), "a friend the roster lacks: %v", err)
	_, ok, err := h.m.GetKey(h.ctx, friendHeardKey("zed"))
	require.NoError(t, err)
	assert.False(t, ok, "a refusal writes nothing")

	// to the second, as the other records are
	h.tick(time.Second)
	got, err = h.st.FriendHeard(h.ctx, "amy", h.now.Add(500*time.Millisecond))
	require.NoError(t, err)
	assert.Equal(t, h.now, got)
}

// Her row reads it: up while under FriendHeardUp since, deaf (shown up) for FriendDeafHold more,
// then down, with the phase on the row and the row's JSON saying only up or down.
func TestFriendRowsReadHerLastHeardAndShowOnlyUpOrDown(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 2}})
	require.NoError(t, err)
	row := h.heardRow("amy")
	assert.Equal(t, sprint.Down, row.Status)
	assert.Equal(t, sprint.Down, row.Phase)
	assert.Contains(t, row.Evidence, "never heard")

	_, err = h.st.FriendHeard(h.ctx, "amy", h.now)
	require.NoError(t, err)
	row = h.heardRow("amy")
	assert.Equal(t, sprint.Up, row.Status)
	assert.Equal(t, sprint.Up, row.Phase)
	assert.Equal(t, h.now, row.Heard)
	assert.Equal(t, "heard 0s ago", row.Evidence)

	for _, c := range []struct {
		after         time.Duration
		status, phase string
	}{
		{sprint.FriendHeardUp - time.Second, sprint.Up, sprint.Up},
		{time.Second, sprint.Up, sprint.Deaf},
		{sprint.FriendDeafHold - time.Second, sprint.Up, sprint.Deaf},
		{time.Second, sprint.Down, sprint.Down},
	} {
		h.tick(c.after)
		row = h.heardRow("amy")
		assert.Equal(t, c.status, row.Status)
		assert.Equal(t, c.phase, row.Phase)
		b, err := json.Marshal(row)
		require.NoError(t, err)
		assert.NotContains(t, string(b), `"deaf"`, "the row never says deaf")
		assert.NotContains(t, string(b), `"asleep"`, "nor asleep")
		var decoded map[string]any
		require.NoError(t, json.Unmarshal(b, &decoded))
		assert.Equal(t, c.status, decoded["status"])
		_, hasPhase := decoded["phase"]
		assert.False(t, hasPhase, "the phase is not a column")
	}

	// the next valid event: up at once
	_, err = h.st.FriendHeard(h.ctx, "amy", h.now)
	require.NoError(t, err)
	assert.Equal(t, sprint.Up, h.heardRow("amy").Status)

	// the seats the tick deals from carry the phase and the shown word
	h.tick(sprint.FriendHeardUp + time.Minute)
	seats, err := h.st.FriendSeats(h.ctx, h.now)
	require.NoError(t, err)
	require.Len(t, seats, 1)
	assert.Equal(t, sprint.Up, seats[0].Status, "deaf is dealt to")
	assert.Equal(t, sprint.Deaf, seats[0].Phase)
	assert.Equal(t, h.now.Add(-sprint.FriendHeardUp-time.Minute), seats[0].Heard)
}

// The events the store sees itself: a finish sets the time; a beat, with a proof or without, an
// unproved pong and a daemon's pong do not (a proof is read where it stands, while her beat is
// fresh, and an answered wake ping under the seat: sprint.FriendPhase).
func TestTheStoreRecordsHerValidEventsAndNotABeat(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 2}})
	require.NoError(t, err)
	heard := func() time.Time {
		at, err := h.st.FriendHeardAt(h.ctx, "amy")
		require.NoError(t, err)
		return at
	}

	// a beat alone is her daemon's: no event
	_, err = h.st.FriendBeat(h.ctx, "amy")
	require.NoError(t, err)
	assert.True(t, heard().IsZero(), "a plain beat is no event")
	_, proof, err := h.st.FriendBeatProof(h.ctx, "amy", sprint.FriendReport{}, nil, sprint.BeatWords{Run: "r1", Check: "n1"})
	require.NoError(t, err)
	assert.False(t, proof.Proved)
	assert.True(t, heard().IsZero(), "a check asked is not a check answered")
	h.tick(time.Second)
	_, proof, err = h.st.FriendBeatProof(h.ctx, "amy", sprint.FriendReport{}, nil, sprint.BeatWords{Run: "r1", Pong: "never-asked"})
	require.NoError(t, err)
	assert.False(t, proof.Proved)
	assert.True(t, heard().IsZero(), "an answer to no check is no event")

	// a beat that proves her session is read where it stands, while the beat is fresh
	_, proof, err = h.st.FriendBeatProof(h.ctx, "amy", sprint.FriendReport{}, nil, sprint.BeatWords{Run: "r1", Pong: "n1"})
	require.NoError(t, err)
	require.True(t, proof.Proved, proof.NoProof)
	assert.True(t, heard().IsZero(), "a proof is not copied into the heard time")
	assert.Equal(t, sprint.Up, h.friendStatus("amy"))
	assert.Equal(t, "session proof 0s ago", h.heardRow("amy").Evidence)
	h.tick(sprint.BeatDeadline + time.Second)
	assert.Equal(t, sprint.Down, h.friendStatus("amy"), "her beats stopped, so her proof stopped with them")

	// the coordinator's daemon pong is not hers; an answered wake ping is read from the observation
	// under the seat (FriendPhase), not copied into the heard time
	h.tick(time.Minute)
	_, _, _, err = h.health("amy", "tester", sprint.DaemonPong, h.now, 1)
	require.NoError(t, err)
	assert.True(t, heard().IsZero(), "a daemon pong is no event of hers")
	h.tick(time.Minute)
	_, _, _, err = h.health("amy", "tester", sprint.Up, h.now, 1)
	require.NoError(t, err)
	assert.True(t, heard().IsZero(), "an answered wake ping is the observation's, under the seat")
	assert.Equal(t, "session pong 0s ago", h.heardRow("amy").Evidence)
	h.give("tester", "stella")
	assert.Equal(t, sprint.Down, h.friendStatus("amy"), "an old seat's proof never looks up under a new seat")
	require.NoError(t, h.st.FriendFinished(h.ctx, "amy", h.now))
	assert.Equal(t, sprint.Up, h.friendStatus("amy"), "a finish is hers under any seat")

	// a card finished: an event, even an older finish than the record holds writes nothing back
	h.tick(time.Minute)
	require.NoError(t, h.st.FriendFinished(h.ctx, "amy", h.now))
	assert.Equal(t, h.now, heard())
	require.NoError(t, h.st.FriendFinished(h.ctx, "amy", h.now.Add(-time.Hour)))
	assert.Equal(t, h.now, heard(), "an older finish moves nothing back")
}

// A friend taken off the roster takes her records with her, the heard time too.
func TestARemovedFriendTakesHerHeardTimeWithHer(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "amy", Width: 2}, {Name: "bob", Width: 2}})
	require.NoError(t, err)
	_, err = h.st.FriendHeard(h.ctx, "amy", h.now)
	require.NoError(t, err)
	_, err = h.st.FriendHeard(h.ctx, "bob", h.now)
	require.NoError(t, err)
	_, removed, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: "bob", Width: 2}})
	require.NoError(t, err)
	assert.Equal(t, []string{"amy"}, removed)
	_, ok, err := h.m.GetKey(h.ctx, friendHeardKey("amy"))
	require.NoError(t, err)
	assert.False(t, ok, "amy's heard record is gone with her")
	_, ok, err = h.m.GetKey(h.ctx, friendHeardKey("bob"))
	require.NoError(t, err)
	assert.True(t, ok, "bob's stays")
}
