package sprint_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A friend is up only on a valid event of her own session (docs/SPEC-FRIEND.md,
// "Presence is her session's evidence"; the last-heard rule, sprint.FriendPhase): a wake ping
// her session answered, which the coordinator writes as friend health --state up, or a card of
// hers finished, each setting the time she was last heard; she is up for
// sprint.FriendHeardUp after it, deaf (shown up) for sprint.FriendDeafHold more, then down.
// On 2026-10-04 one friend read up for four hours and another read working for an hour on
// beats a loop sent for them while their sessions took no turn; a beat, whoever sends it, is
// recorded and never makes her up.

// presenceRig is the twin store (store.Mem) with an injected clock, a
// coordinator holding the seat at its first generation, and the friend amy.
type presenceRig struct {
	t   *testing.T
	st  *store.Store
	ctx context.Context
	mu  sync.Mutex
	now time.Time
}

var presenceT0 = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

func newPresenceRig(t *testing.T) *presenceRig {
	t.Helper()
	m := store.NewMem()
	r := &presenceRig{t: t, ctx: context.Background(), now: presenceT0}
	r.st = &store.Store{B: m, Names: sprint.Names{Prefix: "t-"}, Actor: "coordinator",
		Now:   func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now },
		NewID: func() string { return "1" }, Sleep: func(time.Duration) {}}
	require.NoError(t, r.st.Init(r.ctx))
	require.NoError(t, m.SetCoordinator(r.ctx, "coordinator"))
	_, _, _, err := r.st.SyncFriends(r.ctx, []store.FriendSpec{{Name: "amy", Width: 2}})
	require.NoError(t, err)
	return r
}

// second moves the clock a second and beats amy, as a loop beside her app did.
func (r *presenceRig) second() {
	r.t.Helper()
	r.mu.Lock()
	r.now = r.now.Add(time.Second)
	r.mu.Unlock()
	_, err := r.st.FriendBeat(r.ctx, "amy")
	require.NoError(r.t, err)
}

// beatFor beats amy every second for d.
func (r *presenceRig) beatFor(d time.Duration) {
	for range int(d / time.Second) {
		r.second()
	}
}

// row is amy's row of the friends table now.
func (r *presenceRig) row() store.FriendRow {
	r.t.Helper()
	rows, err := r.st.FriendRows(r.ctx, r.st.Now())
	require.NoError(r.t, err)
	require.Len(r.t, rows, 1)
	return rows[0]
}

// observe is the coordinator's friend health of amy at the clock now.
func (r *presenceRig) observe(state string) {
	r.t.Helper()
	_, _, _, err := r.st.FriendHealth(r.ctx, "amy", "coordinator", sprint.FriendHealth{State: state, Seen: r.st.Now(), Generation: sprint.FirstSeatGeneration}, "")
	require.NoError(r.t, err)
}

func TestFriendIsUpOnlyOnEvidenceFromHerSession(t *testing.T) {
	t.Parallel()
	r := newPresenceRig(t)

	// beaten every second, no pong, no card: down from the first beat to past the window
	r.second()
	row := r.row()
	assert.Equal(t, sprint.Down, row.Status, "a beat alone is no evidence")
	assert.Equal(t, sprint.Asleep, row.Phase, "the friend's daemon answers and the friend's session does not")
	assert.Contains(t, row.Evidence, "no session evidence")
	assert.Contains(t, row.Evidence, "beat 0s ago is not evidence", "the row names the beat and its age")
	r.beatFor(sprint.FriendHeardUp + sprint.FriendDeafHold + time.Minute)
	row = r.row()
	assert.Equal(t, sprint.Down, row.Status, "beaten every second for twenty-one minutes, still down")
	assert.Contains(t, row.Evidence, "nothing heard from the friend within 20m0s")
	assert.Contains(t, row.Evidence, "never heard")

	// her daemon's own pong is no evidence either
	r.observe(sprint.DaemonPong)
	assert.Equal(t, sprint.Down, r.row().Status, "a pong the daemon answered is not her session")

	// one wake ping her session answered: up, naming it and its age
	r.second()
	r.observe(sprint.Up)
	row = r.row()
	assert.Equal(t, sprint.Up, row.Status)
	assert.Equal(t, "session pong 0s ago", row.Evidence)
	assert.True(t, row.Heard.IsZero(), "the answered wake is read from the observation, under the seat, and not copied into heard")
	r.beatFor(sprint.FriendHeardUp - time.Second)
	row = r.row()
	assert.Equal(t, sprint.Up, row.Status, "within the window")
	assert.Equal(t, sprint.Up, row.Phase)
	assert.Equal(t, "session pong 9m59s ago", row.Evidence)
	r.second()
	row = r.row()
	assert.Equal(t, sprint.Up, row.Status, "the window out with no other pong: deaf, shown up")
	assert.Equal(t, sprint.Deaf, row.Phase)
	assert.Contains(t, row.Evidence, "deaf: last heard 10m0s ago")
	r.beatFor(sprint.FriendDeafHold - time.Second)
	row = r.row()
	assert.Equal(t, sprint.Up, row.Status, "deaf for the whole hold")
	assert.Equal(t, sprint.Deaf, row.Phase)
	r.second()
	row = r.row()
	assert.Equal(t, sprint.Down, row.Status, "the hold out with no other event: down, beats or not")
	assert.Contains(t, row.Evidence, "nothing heard from the friend within 20m0s")
	assert.Contains(t, row.Evidence, "last heard 20m0s ago (session pong)")
	r.beatFor(time.Hour)
	assert.Equal(t, sprint.Down, r.row().Status, "an hour of beats changes nothing")

	// a card of hers finished: up for the up window, deaf for the hold, then down
	require.NoError(t, r.st.FriendFinished(r.ctx, "amy", r.st.Now()))
	row = r.row()
	assert.Equal(t, sprint.Up, row.Status)
	assert.Equal(t, "finish 0s ago", row.Evidence)
	assert.Equal(t, r.st.Now(), row.Finished)
	require.NoError(t, r.st.FriendFinished(r.ctx, "amy", r.st.Now().Add(-time.Hour)), "an older finish")
	assert.Equal(t, r.st.Now(), r.row().Finished, "an older finish moves nothing")
	assert.Equal(t, r.st.Now(), r.row().Heard, "nor does it move her last-heard time back")
	r.beatFor(sprint.FriendHeardUp + sprint.FriendDeafHold - time.Second)
	assert.Equal(t, sprint.Up, r.row().Status)
	r.second()
	row = r.row()
	assert.Equal(t, sprint.Down, row.Status)
	assert.Contains(t, row.Evidence, "last heard 20m0s ago")
}

// The rule itself, with no store: held is held whatever the evidence; a pong
// under an old seat generation, or dated after now, is none; a beat is none.
func TestFriendEvidenceRule(t *testing.T) {
	t.Parallel()
	t0 := presenceT0
	pong := sprint.FriendHealth{State: sprint.Up, Seen: t0, Generation: 2}
	grace := sprint.FriendHeardUp + sprint.FriendDeafHold
	cases := []struct {
		name string
		f    sprint.FriendPresence
		at   time.Time
		want string
	}{
		{"never anything", sprint.FriendPresence{Generation: 2}, t0, sprint.Down},
		{"a fresh beat", sprint.FriendPresence{Beat: sprint.Beat{At: t0}, Generation: 2}, t0, sprint.Down},
		{"held with a fresh pong", sprint.FriendPresence{Held: true, Health: pong, Generation: 2}, t0, sprint.Held},
		{"a fresh pong", sprint.FriendPresence{Health: pong, Generation: 2}, t0.Add(sprint.FriendHeardUp - time.Second), sprint.Up},
		{"a pong the up window old: deaf, shown up", sprint.FriendPresence{Health: pong, Generation: 2}, t0.Add(sprint.FriendHeardUp), sprint.Up},
		{"a pong the grace old", sprint.FriendPresence{Health: pong, Generation: 2}, t0.Add(grace), sprint.Down},
		{"a pong under an old seat", sprint.FriendPresence{Health: pong, Generation: 3}, t0, sprint.Down},
		{"a pong from the future", sprint.FriendPresence{Health: pong, Generation: 2}, t0.Add(-time.Second), sprint.Down},
		{"a daemon pong", sprint.FriendPresence{Health: sprint.FriendHealth{State: sprint.DaemonPong, Seen: t0, Generation: 2}, Generation: 2}, t0, sprint.Down},
		{"a fresh finish", sprint.FriendPresence{Finished: t0, Generation: 2}, t0.Add(sprint.FriendHeardUp - time.Second), sprint.Up},
		{"a finish the up window old: deaf, shown up", sprint.FriendPresence{Finished: t0, Generation: 2}, t0.Add(sprint.FriendHeardUp), sprint.Up},
		{"a finish the grace old", sprint.FriendPresence{Finished: t0, Generation: 2}, t0.Add(grace), sprint.Down},
		{"heard just now", sprint.FriendPresence{Heard: t0, Generation: 2}, t0, sprint.Up},
		{"heard the grace ago", sprint.FriendPresence{Heard: t0, Generation: 2}, t0.Add(grace), sprint.Down},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sprint.FriendStatus(c.f, c.at), c.name)
	}
	assert.Equal(t, 10*time.Minute, sprint.FriendHeardUp, "the owner, 2026-10-13: up for ten minutes")
	assert.Equal(t, 10*time.Minute, sprint.FriendDeafHold, "the owner, 2026-10-13: deaf for the ten minutes before they go down")
}
