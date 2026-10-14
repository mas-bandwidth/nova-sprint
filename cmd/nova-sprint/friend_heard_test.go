package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus/bustest"
)

// A friend's last-heard time through the commands (docs/SPEC-SPRINT.md section 1, a friend's
// health; sprint.FriendPhase; tla/FriendHeard.tla): the take of a friend read down is the event that
// brings her up, deaf shows as up, the seat's friend heard records one by hand, and friend sync
// folds the bus store's heard hashes in.

// Deaf shows up and behaves as up: the friends table, where --json and friend heard's own line
// say up and never deaf, her cards stay with her through the tick, and the fresh event of
// hers (her take) is what brings a friend down back up.
func TestADeafFriendShowsAndBehavesAsUp(t *testing.T) {
	t.Parallel()
	ta, _, _ := friendReadyApp(t)
	ta.a.serveAddr = "mem:0"
	ta.ok("friend heard amy")
	ta.a.sleep(sprint.FriendHeardUp + time.Minute)

	f := whereFriends(ta)["amy"]
	assert.Equal(t, sprint.Up, f.Status, "deaf shows up")
	assert.Contains(t, f.Evidence, "deaf: last heard 11m0s ago")
	frame := tableOf(ta.frame(), sprint.Friends)
	assert.NotContains(t, frame, "deaf")
	assert.NotContains(t, frame, "asleep")
	ta.ok("tick")
	assert.Equal(t, 2, whereFriends(ta)["amy"].Working, "her working cards stay: only down returns them")
	judged := ta.ok("inbox")
	assert.Contains(t, judged, "friend amy is deaf", "one judgment to the seat")
	assert.Contains(t, judged, "nova-hook status --as amy")
	assert.Contains(t, judged, "nova-hook hook --as amy --role friend")
	assert.Contains(t, judged, "nova-bus recv --as amy --forever --exec")

	// any valid event: up, and the judgment closes at the next tick
	ta.a.sleep(time.Second)
	code, out := ta.served("take", "--as", "friend.amy", "s1-3.w1@1", "--epoch", "0")
	require.Equal(t, 0, code, out)
	ta.ok("tick")
	f = whereFriends(ta)["amy"]
	assert.Equal(t, sprint.Up, f.Status)
	assert.True(t, len(f.Evidence) >= 6 && f.Evidence[:6] == "heard ", f.Evidence)
	assert.NotContains(t, ta.ok("inbox"), "friend amy is deaf", "up again: the deaf judgment closed")
	ta.clean()
}

// A friend past the grace is down, and her take is the event that brings her up.
func TestADeafFriendWhoStaysSilentGoesDown(t *testing.T) {
	t.Parallel()
	ta, _, _ := friendReadyApp(t)
	ta.ok("friend heard amy")
	ta.a.sleep(sprint.FriendHeardUp + sprint.FriendDeafHold - time.Second)
	assert.Equal(t, sprint.Up, whereFriends(ta)["amy"].Status, "deaf to the last second")
	ta.a.sleep(time.Second)
	f := whereFriends(ta)["amy"]
	assert.Equal(t, sprint.Down, f.Status)
	assert.Contains(t, f.Evidence, "no session evidence: nothing heard from the friend within 20m0s")
	ta.clean()
}

// friend heard is the seat's: it records the time, only forward, never past the server's clock,
// for a friend on the table; the stranger, the unknown friend and a bad time are refused.
func TestFriendHeardIsTheSeatsHandForAnEventItDidNotSee(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy")
	ta.ok("friend sync --root " + t.TempDir())
	assert.Equal(t, sprint.Down, whereFriends(ta)["amy"].Status)

	assert.Contains(t, ta.dry("friend heard amy --dry-run"), "FRIEND-HEARD DRY-RUN amy at=")
	assert.Equal(t, sprint.Down, whereFriends(ta)["amy"].Status, "a dry run records nothing")

	out := ta.ok("friend heard amy")
	assert.Equal(t, "FRIEND-HEARD OK amy heard="+ta.now.UTC().Truncate(time.Second).Format(time.RFC3339)+"\n", out)
	assert.Equal(t, sprint.Up, whereFriends(ta)["amy"].Status)

	ta.a.sleep(time.Minute)
	older := ta.now.Add(-time.Hour).UTC().Format(time.RFC3339)
	first := ta.now.Add(-time.Minute).UTC().Truncate(time.Second).Format(time.RFC3339)
	assert.Contains(t, ta.ok("friend heard amy --at "+older), "heard="+first, "an older time moves nothing")
	later := ta.now.Add(-30 * time.Second).UTC().Format(time.RFC3339)
	assert.Contains(t, ta.ok("friend heard amy --at "+later), "heard="+later)
	future := ta.now.Add(time.Hour).UTC().Format(time.RFC3339)
	assert.Contains(t, ta.ok("friend heard amy --at "+future), "heard="+ta.now.UTC().Truncate(time.Second).Format(time.RFC3339), "never past the server's clock")

	code, _, errs := ta.do("friend heard amy --actor stella")
	assert.Equal(t, 2, code, errs)
	assert.Contains(t, errs, "friend heard is the coordinator's alone: coordinator, not stella")
	code, _, errs = ta.do("friend heard nobody")
	assert.Equal(t, 1, code, errs)
	assert.Contains(t, errs, "no friend nobody on the friends table")
	code, _, errs = ta.do("friend heard amy --at yesterday")
	assert.Equal(t, 2, code, errs)
	assert.Contains(t, errs, "--at wants an RFC3339 time")
}

// A beat is no event: friend beat records the daemon and never her last-heard time.
func TestAPlainBeatIsNotAnEventOfHers(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy")
	ta.ok("friend sync --root " + t.TempDir())
	for range 5 {
		ta.a.sleep(time.Minute)
		ta.ok("friend beat amy")
	}
	f := whereFriends(ta)["amy"]
	assert.Equal(t, sprint.Down, f.Status)
	assert.True(t, f.Heard.IsZero())
	assert.Contains(t, f.Evidence, "asleep: the friend's daemon answers and the friend's session does not")
	assert.NotContains(t, tableOf(ta.frame(), sprint.Friends), "asleep", "asleep is never shown")
}

// busHeardApp is amy and bob with the bus store behind the app's dial: no socket.
func busHeardApp(t *testing.T) (*testApp, *bustest.Fake, string) {
	t.Helper()
	ta, _ := friendApp(t, "amy", "bob")
	env := ta.a.getenv
	ta.a.getenv = func(k string) string {
		if k == busRedisEnv {
			return "bus.test:6379"
		}
		return env(k)
	}
	fake := bustest.NewFake(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC), "coordinator", "amy", "bob")
	fake.Friends = []string{"amy", "bob"}
	ta.a.busOpen = func(_ context.Context, _, _ string) (*bus.Bus, func(), error) {
		return &bus.Bus{Store: fake}, func() {}, nil
	}
	return ta, fake, t.TempDir()
}

func busStamp(at time.Time) string { return at.UTC().Format("2006-01-02T15:04:05.000Z") }

// friend sync folds the bus store's heard hashes in: the later of a name's last send and last recv, for
// each friend whose name appears; a friend that does not appear is untouched; a store with no
// hash is no error.
func TestFriendSyncFoldsTheBusStoresHeardHashesIn(t *testing.T) {
	t.Parallel()
	ta, fake, root := busHeardApp(t)

	out := ta.ok("friend sync --root " + root)
	assert.NotContains(t, out, "FRIEND-SYNC NOTE", "a bus store with no heard hashes is no error")
	assert.Equal(t, sprint.Down, whereFriends(ta)["amy"].Status)

	require.NoError(t, fake.AddAll(context.Background(), nil, nil,
		bus.Mark{Key: busHeardSend, Field: "amy", Value: busStamp(ta.now.Add(-3 * time.Minute))},
		bus.Mark{Key: busHeardRecv, Field: "amy", Value: busStamp(ta.now.Add(-time.Minute))},
		bus.Mark{Key: busHeardSend, Field: "bob", Value: busStamp(ta.now.Add(-40 * time.Minute))},
		bus.Mark{Key: busHeardRecv, Field: "stranger", Value: busStamp(ta.now)},
	))
	out = ta.ok("friend sync --root " + root)
	assert.NotContains(t, out, "FRIEND-SYNC NOTE", out)
	f := whereFriends(ta)
	assert.Equal(t, sprint.Up, f["amy"].Status, "her last recv a minute ago")
	assert.Equal(t, "heard 1m0s ago", f["amy"].Evidence, "the later of send and recv")
	assert.Equal(t, ta.now.Add(-time.Minute).UTC().Truncate(time.Second), f["amy"].Heard.UTC())
	assert.Equal(t, sprint.Down, f["bob"].Status, "bob last spoke forty minutes ago")

	// a friend that sends again is up, and the older hash value moves nothing back
	ta.a.sleep(30 * time.Minute)
	assert.Equal(t, sprint.Down, whereFriends(ta)["amy"].Status)
	require.NoError(t, fake.AddAll(context.Background(), nil, nil, bus.Mark{Key: busHeardSend, Field: "amy", Value: busStamp(ta.now.Add(-time.Second))}))
	ta.ok("friend sync --root " + root)
	assert.Equal(t, sprint.Up, whereFriends(ta)["amy"].Status, "a message of hers on the bus: up")
	require.NoError(t, fake.AddAll(context.Background(), nil, nil, bus.Mark{Key: busHeardSend, Field: "amy", Value: busStamp(ta.now.Add(-time.Hour))}))
	ta.ok("friend sync --root " + root)
	assert.Equal(t, sprint.Up, whereFriends(ta)["amy"].Status, "never moved back")
}

// A bus store that cannot be read costs the fold, said on sync's line, and nothing else.
func TestFriendSyncSaysABusStoreThatCannotBeReadAndGoesOn(t *testing.T) {
	t.Parallel()
	ta, fake, root := busHeardApp(t)
	fake.Fail = errors.New("dial tcp: connection refused")
	out := ta.ok("friend sync --root " + root)
	assert.Contains(t, out, "FRIEND-SYNC NOTE: the bus store's heard times could not be read (dial tcp: connection refused)")
	assert.Contains(t, out, "FRIEND-SYNC OK")
}
