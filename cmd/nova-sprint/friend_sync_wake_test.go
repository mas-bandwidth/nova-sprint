package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// TestDealIsToldOnCommitNotOnFriendSync pins tla/FriendSyncWake.tla
// (PushedWithinOneEvent, EveryDealIsPushed): a tick that deals a card to friend
// ada and a card to the coordinator seat's own row wakes friend sync --every 15s
// on the sprint log (Store.WaitLog), delivering both inbox BRIEF.md files and
// sending both bus messages (the seat's own row with kind=status) after one log
// wake with the fake clock never advanced. When the log wake is dropped on the
// fake transport, advancing the fake clock past the 15s backstop interval
// catches the deal once.
func TestDealIsToldOnCommitNotOnFriendSync(t *testing.T) {
	t.Parallel()

	t.Run("log wake delivers and tells without advancing the clock", func(t *testing.T) {
		t.Parallel()
		ta, cfg := friendApp(t, "ada", "coordinator")
		_, _, err := cfg.Update(context.Background(), config.KindFriend, "coordinator", map[string]string{"mode": config.FriendModeOneShot}, "t")
		require.NoError(t, err)
		ta.a.tip = tipIs(t, landHead)
		root := t.TempDir()

		briefs := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(briefs, "s1-1.md"), []byte(passingBrief("s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: only friend ada")), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(briefs, "s1-2.md"), []byte(passingBrief("s1-2: seat's card\nREPO: mas-bandwidth/nova-tools\nWHO: only friend coordinator")), 0o644))

		beganAt := ta.a.now()
		var sent []bus.Message
		ta.a.bus = func(_ context.Context, m bus.Message, _ func(string)) error {
			sent = append(sent, m)
			return nil
		}

		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		ta.a.notify = func(context.Context) (context.Context, context.CancelFunc) {
			return ctx, func() {}
		}

		fire := func() <-chan time.Time {
			ch := make(chan time.Time, 1)
			ch <- ta.a.now()
			return ch
		}
		pass := 0
		ta.a.after = func(d time.Duration) <-chan time.Time {
			pass++
			if pass == 1 {
				ta.beatUp("ada")
				ta.beatUp("coordinator")
				ta.ok("add --stream s1 --brief-dir " + briefs)
				ta.ok("start")
				ta.ok("tick")
				// The fake clock does not advance: only a loop parked in
				// Store.WaitLog wakes on the deal commit's log entry.
				if ta.m.Calls["waitlog"] >= 1 {
					return fire()
				}
			}
			stop()
			return fire()
		}

		var out, errs bytes.Buffer
		code := runInto(ta.a, []string{"friend", "sync", "--every", "15s", "--root", root}, &out, &errs)
		require.Equal(t, 0, code, errs.String())

		assert.Equal(t, beganAt, ta.a.now(), "the fake clock never advanced")
		assert.GreaterOrEqual(t, ta.m.Calls["waitlog"], 1, "friend sync --every must wait on Store.WaitLog")

		_, err = os.Stat(filepath.Join(root, "ada-working", "inbox", "s1-1.w1", "BRIEF.md"))
		assert.NoError(t, err, "ada's inbox BRIEF.md is written on the deal commit's log wake")
		_, err = os.Stat(filepath.Join(root, "coordinator-working", "inbox", "s1-2.w1", "BRIEF.md"))
		assert.NoError(t, err, "the seat's inbox BRIEF.md is written on the deal commit's log wake")

		require.Len(t, sent, 2, "both ada and the seat's own row are told on the bus within one log wake")
		byTo := map[string]bus.Message{}
		for _, m := range sent {
			require.Len(t, m.To, 1)
			byTo[m.To[0]] = m
		}
		require.Contains(t, byTo, "ada")
		require.Contains(t, byTo, "coordinator")
		assert.Equal(t, bus.KindStatus, byTo["ada"].Kind)
		assert.Equal(t, bus.KindStatus, byTo["coordinator"].Kind, "the seat's own row is told with kind=status")
	})

	t.Run("dropped log wake is caught once by the backstop sweep", func(t *testing.T) {
		t.Parallel()
		ta, cfg := friendApp(t, "ada", "coordinator")
		_, _, err := cfg.Update(context.Background(), config.KindFriend, "coordinator", map[string]string{"mode": config.FriendModeOneShot}, "t")
		require.NoError(t, err)
		ta.a.tip = tipIs(t, landHead)
		root := t.TempDir()

		briefs := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(briefs, "s1-1.md"), []byte(passingBrief("s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: only friend ada")), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(briefs, "s1-2.md"), []byte(passingBrief("s1-2: seat's card\nREPO: mas-bandwidth/nova-tools\nWHO: only friend coordinator")), 0o644))

		var sent []bus.Message
		ta.a.bus = func(_ context.Context, m bus.Message, _ func(string)) error {
			sent = append(sent, m)
			return nil
		}

		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		ta.a.notify = func(context.Context) (context.Context, context.CancelFunc) {
			return ctx, func() {}
		}

		fire := func() <-chan time.Time {
			ch := make(chan time.Time, 1)
			ch <- ta.a.now()
			return ch
		}
		var waits []time.Duration
		pass := 0
		ta.a.after = func(d time.Duration) <-chan time.Time {
			waits = append(waits, d)
			pass++
			switch pass {
			case 1:
				ta.beatUp("ada")
				ta.beatUp("coordinator")
				ta.ok("add --stream s1 --brief-dir " + briefs)
				ta.ok("start")
				ta.ok("tick")
				// With the log wake dropped on the fake transport and the fake
				// clock not yet advanced, nothing has been told.
				assert.Empty(t, sent, "dropped log wake has not told anyone before the backstop fires")
				if ta.m.Calls["waitlog"] == 0 {
					stop()
					return fire()
				}
				// Advance the fake clock past the 15s backstop interval.
				ta.a.sleep(d)
				return fire()
			case 2:
				assert.Len(t, sent, 2, "the --every backstop sweep delivers and tells both cards")
				// Advance another 15s backstop interval with no new deal.
				ta.a.sleep(d)
				return fire()
			default:
				stop()
				return fire()
			}
		}

		var out, errs bytes.Buffer
		code := runInto(ta.a, []string{"friend", "sync", "--every", "15s", "--root", root}, &out, &errs)
		require.Equal(t, 0, code, errs.String())
		assert.Equal(t, []time.Duration{15 * time.Second, 15 * time.Second, 15 * time.Second}, waits)
		assert.Len(t, sent, 2, "caught once: the next sweep does not re-tell")
		byTo := map[string]bus.Message{}
		for _, m := range sent {
			require.Len(t, m.To, 1)
			byTo[m.To[0]] = m
		}
		assert.Equal(t, bus.KindStatus, byTo["ada"].Kind)
		assert.Equal(t, bus.KindStatus, byTo["coordinator"].Kind, "the seat's own row is told with kind=status")
	})
}
