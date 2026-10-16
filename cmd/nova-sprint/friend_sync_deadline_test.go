package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-tools/hook"
)

// The owner's law, 2026-10-14: "never wait forever, mechanically" (tla/FriendSyncPass.tla,
// PassEnds). A pass of friend sync whose stage never answers ends at the pass's deadline
// in a named refusal, exit 1, and never waits on: here the config store's read never
// answers, and the pass is refused at friendSyncPassBound naming where it stood. The
// clock is synctest's, so no real time passes. The reversed witness: at v1.4.5 the pass
// runs on context.Background() and waits on the read for ever, and the test's own bound,
// past the pass's, fails it.
func TestFriendSyncPassNeverWaitsForeverOnAStage(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ta, _ := friendApp(t, "amy")
		release := make(chan struct{})
		defer close(release)
		ta.a.friends = func(ctx context.Context, _ string) ([]config.Row, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
				return nil, errors.New("released by the test: the pass was still waiting on the config store")
			}
		}
		type ended struct {
			code      int
			out, errs string
		}
		done := make(chan ended, 1)
		go func() {
			var out, errs bytes.Buffer
			code := runInto(ta.a, []string{"friend", "sync"}, &out, &errs)
			done <- ended{code, out.String(), errs.String()}
		}()
		begun := time.Now()
		select {
		case e := <-done:
			assert.Equal(t, 1, e.code, "a pass past its deadline is refused: %s%s", e.out, e.errs)
			assert.Contains(t, e.errs, "friend sync REFUSED: the pass did not end within its deadline, "+friendSyncPassBound.String()+" on the sprint's clock")
			assert.Contains(t, e.errs, "stood at reading the config's friend rows")
			assert.Contains(t, e.errs, "the next pass retries")
			assert.Equal(t, 1, strings.Count(e.errs, "\n"), "one line: %q", e.errs)
			assert.LessOrEqual(t, time.Since(begun), friendSyncPassBound, "it ended by its deadline")
		case <-time.After(friendSyncPassBound + time.Minute):
			t.Fatalf("friend sync waited on a stage that never answers for %s, past its deadline of %s: it waits for ever", friendSyncPassBound+time.Minute, friendSyncPassBound)
		}
	})
}

// The same deadline on the sprint's clock, time passed as an argument: a pass whose
// stages answer, but whose clock reads past friendSyncPassBound when one returns, is
// refused at the next stage, exit 1, and does nothing more (the friends table is not
// synced). The reversed witness: at v1.4.5 the pass has no deadline and syncs.
func TestFriendSyncPassIsRefusedPastItsDeadlineOnTheSprintClock(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t, "amy")
	ta.a.friends = func(ctx context.Context, _ string) ([]config.Row, error) {
		ta.step(friendSyncPassBound) // the read took the whole bound on the sprint's clock
		return cfg.List(ctx, config.KindFriend)
	}
	code, out, errs := ta.do("friend sync")
	assert.Equal(t, 1, code, "%s%s", out, errs)
	assert.Contains(t, errs, "friend sync REFUSED: the pass did not end within its deadline")
	assert.Contains(t, errs, "stood at after reading the config's friend rows")
	assert.NotContains(t, out, "FRIEND-SYNC OK", "nothing more is done once the deadline is passed")
	assert.NotContains(t, ta.ok("where"), "amy", "the friends table was not synced")

	ta.a.friends = func(ctx context.Context, _ string) ([]config.Row, error) { return cfg.List(ctx, config.KindFriend) }
	assert.Contains(t, ta.ok("friend sync"), "FRIEND-SYNC OK added=amy", "the next pass, inside its deadline, syncs")
}

// The studio, 2026-10-16: the friend sync loop is installed with no actor (friend sync
// install refuses one), so it acts as the coordinator seat each pass. While the seat's
// nova-hook record is proven, the loop syncs as before; once it lapses (its leases run
// out unrenewed), every pass is refused at once by name, "the coordinator's hook is not
// proven", exit 1, and never waits (no config read, no card staged, no report collected:
// the pass did nothing); the loop goes on, and the first pass after the record is proven
// again syncs. The reversed witness: at v1.4.5 the refusal is the store open's usage
// refusal, exit 2, with no word of the coordinator's hook.
func TestFriendSyncLoopRefusesALapsedCoordinatorHookByNameAndRetries(t *testing.T) {
	t.Parallel()
	const name = "sync-seat"
	ta, rig, env := coordSprint(t, name)
	ctx := context.Background()
	env["HOME"] = t.TempDir()
	env["NOVA_SPRINT_ACTOR"] = "" // the loop row names no actor
	cfg := config.NewMem()
	addFriendRow(t, cfg, "amy")
	reads := 0
	ta.a.friends = func(ctx context.Context, _ string) ([]config.Row, error) {
		reads++
		return cfg.List(ctx, config.KindFriend)
	}
	rig.Hook(t, name, hook.Coordinator)
	_, err := ta.a.holdSprintPush(ctx, name, true)
	require.NoError(t, err)
	require.Equal(t, hook.StateProven, rig.State(t, name).State)

	stopCtx, stop := context.WithCancel(context.Background())
	defer stop()
	ta.a.notify = func(context.Context) (context.Context, context.CancelFunc) { return stopCtx, stop }
	passes, readsWhileLapsed := 0, 0
	ta.a.after = func(d time.Duration) <-chan time.Time {
		passes++
		switch passes {
		case 1:
			// the session's hook lapses: nothing renews its leases
			rig.Pass(hook.LeaseTTL + time.Second)
			require.NotEqual(t, hook.StateProven, rig.State(t, name).State)
			readsWhileLapsed = reads
		case 3:
			assert.Equal(t, readsWhileLapsed, reads, "a refused pass reads nothing of the config")
			rig.Hook(t, name, hook.Coordinator) // hooked in again and proven
			_, err := ta.a.holdSprintPush(ctx, name, true)
			require.NoError(t, err)
		case 4:
			stop()
		}
		ta.step(d)
		fired := make(chan time.Time, 1)
		fired <- ta.a.now()
		return fired
	}
	var out, errs bytes.Buffer
	code := runInto(ta.a, []string{"friend", "sync", "--every", "15s"}, &out, &errs)
	assert.Equal(t, 0, code, "an interrupt ends the loop: %s", errs.String())
	assert.Equal(t, 4, passes)
	assert.Contains(t, out.String(), "FRIEND-SYNC OK added=amy", "proven: the pass syncs as before")
	assert.Contains(t, out.String(), "FRIEND-SYNC OK again after 2 failing passes", "proven again: the next pass syncs")
	assert.Contains(t, errs.String(), "FRIEND-SYNC FAILING exit=1")
	assert.Contains(t, errs.String(), "friend sync REFUSED: the coordinator's hook is not proven: You must hook in first: "+name+" is not hooked in")
	assert.Contains(t, errs.String(), "the next pass retries")
	assert.Equal(t, 1, strings.Count(errs.String(), "the coordinator's hook is not proven"), "a failing pass is said once, not each pass: %s", errs.String())
}
