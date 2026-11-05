//go:build functional

package store

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/hostload"
	"github.com/stretchr/testify/require"
)

// Every key family the store writes — the tables and their residue, the
// per-epoch sprint keys, the dynamic beat, hold, stats-archive and done-page
// keys, and every machine-wide key including archive, cardcheck and
// refusedrecord — is removed by Teardown; none of the three is history that
// outlives a torn-down deployment.
func TestTeardownRemovesEveryMachineKey(t *testing.T) {
	t.Parallel()
	h, c := liveHarness(t)
	ctx := context.Background()

	h.setup(1)
	h.landThrough("s1", "s1-1")
	h.startMachine()

	refused, err := h.st.ArchiveStreams(ctx, []string{"s1"})
	require.NoError(t, err)
	require.Empty(t, refused)
	refused, err = h.st.UnarchiveStreams(ctx, []string{"s1"})
	require.NoError(t, err)
	require.Empty(t, refused)

	_, err = h.st.PutCardCheck(ctx, CheckRecord{At: h.now, By: h.st.Actor, Clean: true, Digest: "sha256:clean"})
	require.NoError(t, err)
	require.NoError(t, h.st.RecordRefused(ctx, sprint.RefusedVerb{At: h.now, Verb: "take", Why: "refused"}))

	h.setGoal("friend-a", "goal", "file:"+filepath.Join(t.TempDir(), "goal.txt"))
	h.setTimer(time.Minute, h.st.Actor)
	require.NoError(t, h.st.SetRulesPath(ctx, "rules.sexp"))
	require.NoError(t, h.st.SetOwner(ctx, "owner-a"))
	h.must(SeatStep(sprint.SeatReq{To: "friend-a", Who: h.st.Actor, Reason: "handover"}))
	require.NoError(t, h.st.B.SetCoordinator(ctx, h.st.Actor))
	require.NoError(t, h.st.B.SetCursor(ctx, "1-0"))

	zero := 0.0
	_, err = h.st.Beat(ctx, "stranger-1", &zero, hostload.Source{})
	require.NoError(t, err)
	require.NoError(t, h.st.SetReaderAway(ctx, "reader-a", true, h.st.Actor))
	_, _, _, err = h.st.SyncFriends(ctx, []FriendSpec{{Name: "friend-a", Width: 1}})
	require.NoError(t, err)
	_, err = h.st.FriendBeatReport(ctx, "friend-a", sprint.FriendReport{}, nil)
	require.NoError(t, err)
	require.NoError(t, h.st.PublishFriendStatus(ctx, h.now))

	h.machine()
	_, err = h.st.StopsNow(ctx)
	require.NoError(t, err)

	require.NoError(t, h.st.putDropDebt(ctx, []string{"m-dropped"}))
	require.NoError(t, h.st.putJSON(ctx, keyStuck, Stuck{Op: "op-1", Verb: "take", At: h.now, Seen: h.now, Why: "stuck"}))
	require.NoError(t, h.st.putJSON(ctx, keyRetiredPushers, []string{"old-pusher"}))
	kv := h.st.B.(KV)
	require.NoError(t, kv.SetKey(ctx, retiredPushKey("old-pusher"), "1"))
	require.NoError(t, kv.SetKey(ctx, keyRetiredSeatHook, "1"))
	require.NoError(t, h.st.putJSON(ctx, keyStats, StatsRecord{Archives: []string{"stats-archive:1"}}))
	require.NoError(t, kv.SetKey(ctx, "stats-archive:1", "{}"))

	during := liveKeys(t, c)
	for _, k := range []string{
		keyMachine, keyHeartbeat, keyStuck, keyCoordinator, keyGoals, keyTimers,
		keyStrangers, keyTickEnd, keyRules, keyFriends, keyFriendStatus, keyDropDebt,
		keySeat, keyOwner, keyWhere, keyRetiredPushers, keyRetiredSeatHook,
		keyStats, keyStops, keyDoneRecord,
		keyArchive, keyCheckRecord, keyRefusedRecord,
	} {
		want := h.st.Names.Key(k)
		require.True(t, slices.Contains(during, want), "expected machine key %s to be written before teardown; got %v", want, during)
	}

	h.stopMachine()
	_, err = h.st.Teardown(ctx)
	require.NoError(t, err)
	require.Empty(t, liveKeys(t, c), "every sprint and machine key is removed by teardown")
}
