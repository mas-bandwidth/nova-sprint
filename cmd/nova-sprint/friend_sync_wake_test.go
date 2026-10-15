package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/bus/bustest"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// workerUp is a friend's beat and a proven friend-role hook record: the worker
// evidence the deal requires since "Require proven live workers before dealing
// friend work" (internal/sprint/friend_deal.go friendWorkerLive). The shared
// beatUp pong carries no role, so it proves a presence and no worker.
func (ta *testApp) workerUp(friend string) {
	ta.t.Helper()
	ta.ok("friend beat " + friend)
	prev := ta.a.readFriendHooks
	ta.a.readFriendHooks = func(ctx context.Context, names ...string) ([]hook.Status, time.Time, error) {
		by := map[string]hook.Status{}
		if prev != nil {
			rows, _, err := prev(ctx, names...)
			if err != nil {
				return nil, time.Time{}, err
			}
			for _, r := range rows {
				by[r.Name] = r
			}
		}
		by[friend] = hook.Status{Name: friend, Role: hook.Friend, State: hook.StateProven}
		var out []hook.Status
		for _, n := range names {
			if r, ok := by[n]; ok {
				out = append(out, r)
			}
		}
		return out, ta.now, nil
	}
}

// dealApart runs the dealing steps while the coordinator key is held by another
// name and then gives it back to "coordinator", the actor of every friend sync
// pass. The deal never gives work to the seat holder's own row (friendWorkerLive),
// and friend sync is the holder's alone, so a card reaches the row named like the
// pass's actor, From == To, only by a handover of the seat after the deal: the
// case the one-shot tell must carry kind status for.
func (ta *testApp) dealApart(f func(actor string)) {
	ta.t.Helper()
	ctx := context.Background()
	require.NoError(ta.t, ta.m.SetCoordinator(ctx, "boss"))
	f(" --actor boss")
	require.NoError(ta.t, ta.m.SetCoordinator(ctx, "coordinator"))
}

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
				ta.workerUp("ada")
				ta.workerUp("coordinator")
				ta.dealApart(func(as string) {
					ta.ok("add --stream s1 --brief-dir " + briefs + as)
					ta.ok("start" + as)
					ta.ok("tick" + as)
				})
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
				ta.workerUp("ada")
				ta.workerUp("coordinator")
				ta.dealApart(func(as string) {
					ta.ok("add --stream s1 --brief-dir " + briefs + as)
					ta.ok("start" + as)
					ta.ok("tick" + as)
				})
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

// A commit after the pass snapshot must not be hidden by a later tail read.
func TestFriendSyncDealBetweenPassAndWait(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t, "ada", "coordinator")
	_, _, err := cfg.Update(context.Background(), config.KindFriend, "coordinator", map[string]string{"mode": config.FriendModeOneShot}, "t")
	require.NoError(t, err)
	ta.a.tip = tipIs(t, landHead)
	root := t.TempDir()
	briefs := t.TempDir()
	for _, id := range []string{"s1-1", "s1-2"} {
		require.NoError(t, os.WriteFile(filepath.Join(briefs, id+".md"), []byte(passingBrief(id+": racing deal\nREPO: mas-bandwidth/nova-tools\nWHO: only friend "+map[string]string{"s1-1": "ada", "s1-2": "coordinator"}[id])), 0o644))
	}
	ta.ok("friend sync --root " + root)
	ta.workerUp("ada")
	ta.workerUp("coordinator")
	ta.dealApart(func(as string) {
		ta.ok("add --stream s1 --one --brief-file " + filepath.Join(briefs, "s1-1.md") + as)
		ta.ok("start" + as)
		ta.ok("tick" + as)
	})
	var sent []bus.Message
	ta.a.bus = func(_ context.Context, m bus.Message, _ func(string)) error {
		sent = append(sent, m)
		return nil
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	ta.a.notify = func(context.Context) (context.Context, context.CancelFunc) { return ctx, func() {} }
	began := ta.a.now()
	out := &friendSyncRaceWriter{commit: func() {
		ta.dealApart(func(as string) {
			ta.ok("add --stream s1 --one --brief-file " + filepath.Join(briefs, "s1-2.md") + as)
			ta.ok("tick" + as)
		})
	}}
	ta.a.after = func(d time.Duration) <-chan time.Time {
		assert.Equal(t, 15*time.Second, d)
		assert.Len(t, sent, 2, "the racing deal must be delivered before the backstop fires")
		_, err := os.Stat(filepath.Join(root, "coordinator-working", "inbox", "s1-2.w1", "BRIEF.md"))
		assert.NoError(t, err, "racing deal inbox exists before the quiet-log wait")
		stop()
		ch := make(chan time.Time, 1)
		ch <- ta.a.now()
		return ch
	}
	var errs bytes.Buffer
	require.Equal(t, 0, runInto(ta.a, []string{"friend", "sync", "--every", "15s", "--root", root}, out, &errs), errs.String())
	assert.True(t, out.committed, "commit must occur after the first pass output")
	assert.Equal(t, began, ta.a.now(), "no clock advance may hide a missed wake")
}

type friendSyncRaceWriter struct {
	bytes.Buffer
	commit    func()
	committed bool
}

func (w *friendSyncRaceWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if !w.committed {
		w.committed = true
		w.commit()
	}
	return n, err
}

// Failed sends and accepted sends with lost replies share one frozen token.
func TestFriendSyncRetriesFailedTell(t *testing.T) {
	t.Parallel()
	for _, accepted := range []bool{false, true} {
		t.Run(map[bool]string{false: "before send", true: "lost reply"}[accepted], func(t *testing.T) {
			t.Parallel()
			ta, _ := friendApp(t, "ada")
			root := t.TempDir()
			ta.ok("friend sync --root " + root)
			ta.workerUp("ada")
			brief := filepath.Join(t.TempDir(), "s1-1.md")
			require.NoError(t, os.WriteFile(brief, []byte(passingBrief("s1-1: retry a tell\nREPO: mas-bandwidth/nova-tools\nWHO: only friend ada")), 0644))
			ta.ok("add --stream s1 --one --brief-file " + brief)
			ta.ok("start")
			ta.ok("tick")
			fake := bustest.NewFake(ta.a.now(), "ada", "coordinator")
			b := &bus.Bus{Store: fake}
			calls := 0
			var messages []bus.Message
			ta.a.bus = func(ctx context.Context, m bus.Message, _ func(string)) error {
				calls++
				messages = append(messages, m)
				if calls == 1 && !accepted {
					return errors.New("pre-send failure")
				}
				_, err := b.Send(ctx, m)
				if err != nil {
					return err
				}
				if calls == 1 {
					return errors.New("accepted but reply lost")
				}
				return nil
			}
			ta.ok("friend sync --root " + root)
			require.Equal(t, 1, calls)
			before, err := os.ReadFile(filepath.Join(root, "ada-working", "inbox", "s1-1.w1", "BRIEF.md"))
			require.NoError(t, err)
			ta.step(15 * time.Second)
			fake.Advance(15 * time.Second)
			ta.ok("friend sync --root " + root)
			require.Equal(t, 2, calls, "backstop must retry a failed tell even though BRIEF exists")
			require.NotEmpty(t, messages[0].Token)
			assert.Equal(t, messages[0], messages[1], "same frozen payload and token")
			assert.Equal(t, 1, fake.Len(bus.StreamOf("ada")), "one accepted message, including lost reply")
			ta.ok("friend sync --root " + root)
			assert.Equal(t, 2, calls, "receipt stops later retries")
			after, err := os.ReadFile(filepath.Join(root, "ada-working", "inbox", "s1-1.w1", "BRIEF.md"))
			require.NoError(t, err)
			assert.Equal(t, before, after)
		})
	}
}

func friendDeliveryFixture(t *testing.T, n int) (*testApp, *store.Store, string, []sprint.Packet, []*sprint.Card) {
	t.Helper()
	ta, _ := friendApp(t, "ada")
	root := t.TempDir()
	ta.ok("friend sync --root " + root)
	ta.workerUp("ada")
	briefs := t.TempDir()
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("s1-%d", i)
		require.NoError(t, os.WriteFile(filepath.Join(briefs, id+".md"), []byte(passingBrief(id+": durable delivery\nREPO: mas-bandwidth/nova-tools\nWHO: only friend ada")), 0644))
	}
	ta.ok("add --stream s1 --brief-dir " + briefs)
	ta.ok("start")
	ta.ok("tick")
	st := &store.Store{B: ta.m, Actor: "coordinator", Now: ta.a.now, Sleep: ta.a.sleep, Attempts: 1, Resends: 1}
	cs, err := st.ReadCells(context.Background(), sprint.Fleet, sprint.FriendRow("ada"), sprint.Ready, sprint.Working)
	require.NoError(t, err)
	ps, err := st.Packets(context.Background(), cs)
	require.NoError(t, err)
	require.Len(t, ps, n)
	return ta, st, filepath.Join(root, "ada-working"), ps, cs
}
func friendDeliveryNotes(t *testing.T, st *store.Store) []sprint.Note {
	t.Helper()
	all, err := st.B.OpenNotes(context.Background())
	require.NoError(t, err)
	var out []sprint.Note
	for _, o := range all {
		if o.Note.Type == nFriendDelivery {
			out = append(out, o.Note)
		}
	}
	return out
}
func TestFriendDeliveryIntentBeforeBriefAndReservations(t *testing.T) {
	t.Parallel()
	ta, st, dir, ps, cs := friendDeliveryFixture(t, 3)
	ctx := context.Background()
	calls := 0
	ta.a.bus = func(context.Context, bus.Message, func(string)) error { calls++; return nil }
	_, err := ta.a.friendDeliveryPrepare(ctx, st, "ada", dir, config.FriendModeBatch, ps[:2], cs[:2])
	require.NoError(t, err)
	// Simulates another pass planned before the first committed, overlapping B.
	_, err = ta.a.friendDeliveryPrepare(ctx, st, "ada", dir, config.FriendModeBatch, ps[1:], cs[1:])
	require.NoError(t, err)
	notes := friendDeliveryNotes(t, st)
	require.Len(t, notes, 2)
	jobs := map[string]int{}
	for _, n := range notes {
		require.Equal(t, sprint.Acknowledged, n.Kind)
		require.Empty(t, n.To)
		assert.False(t, forCoordinator(sprint.Group{Kind: n.Kind, To: n.To}), "intent cannot enter global coordinator sender")
		assert.Empty(t, coordFresh(map[string]bool{}, coordLook{holder: "coordinator", groups: []sprint.Group{{Kind: n.Kind, Type: n.Type, To: n.To, Notes: []string{n.ID}}}, open: []sprint.Open{{Note: n}}}, ta.a.now()), "global coordinator sender cannot bypass BRIEF gate")
		for job := range friendDeliveryOf(n).Items {
			jobs[job]++
			_, err := os.Stat(filepath.Join(dir, "inbox", job, "BRIEF.md"))
			assert.True(t, os.IsNotExist(err), "intent commits before any BRIEF")
		}
	}
	require.Len(t, jobs, 3)
	for _, count := range jobs {
		assert.Equal(t, 1, count, "overlapping plans reserve each job once")
	}
	require.NoError(t, ta.a.friendDeliverySend(ctx, st, "ada", dir, func(string) {}))
	assert.Zero(t, calls, "no send before all frozen BRIEFs exist")
	_, _, err = ta.a.friendCardsOf(ctx, st, "ada", dir, func(string) {})
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	// Restart/replan with only part of the original batch keeps both receipts.
	_, err = ta.a.friendDeliveryPrepare(ctx, st, "ada", dir, config.FriendModeOneShot, ps[1:], cs[1:])
	require.NoError(t, err)
	assert.Len(t, friendDeliveryNotes(t, st), 2)
	require.NoError(t, ta.a.friendDeliverySend(ctx, st, "ada", dir, func(string) {}))
	assert.Equal(t, 2, calls)
}
func TestFriendDeliveryWriteFailuresAndLegacyFiles(t *testing.T) {
	t.Parallel()
	t.Run("intent write failure precedes BRIEF", func(t *testing.T) {
		t.Parallel()
		ta, st, dir, ps, _ := friendDeliveryFixture(t, 1)
		ta.m.MaxWrite = 1
		calls := 0
		ta.a.bus = func(context.Context, bus.Message, func(string)) error { calls++; return nil }
		_, _, err := ta.a.friendCardsOf(context.Background(), st, "ada", dir, func(string) {})
		require.Error(t, err)
		assert.Zero(t, calls)
		_, err = os.Stat(filepath.Join(dir, "inbox", friendJobOf(ps[0]), "BRIEF.md"))
		assert.True(t, os.IsNotExist(err))
		ta.m.MaxWrite = 0
	})
	t.Run("receipt write failure replays stable token", func(t *testing.T) {
		t.Parallel()
		ta, st, dir, _, _ := friendDeliveryFixture(t, 1)
		fake := bustest.NewFake(ta.a.now(), "ada", "coordinator")
		b := &bus.Bus{Store: fake}
		calls := 0
		ta.a.bus = func(ctx context.Context, m bus.Message, _ func(string)) error {
			calls++
			_, err := b.Send(ctx, m)
			if calls == 1 {
				ta.m.MaxWrite = 1
			}
			return err
		}
		_, _, err := ta.a.friendCardsOf(context.Background(), st, "ada", dir, func(string) {})
		require.Error(t, err)
		ta.m.MaxWrite = 0
		require.False(t, friendDeliveryNotes(t, st)[0].CoordinatorDelivery.Sent)
		restarted := &app{now: ta.a.now, bus: ta.a.bus, getenv: func(string) string { return "" }}
		reopened := &store.Store{B: ta.m, Actor: "coordinator", Now: ta.a.now, Sleep: ta.a.sleep}
		_, _, err = restarted.friendCardsOf(context.Background(), reopened, "ada", dir, func(string) {})
		require.NoError(t, err)
		assert.Equal(t, 2, calls)
		assert.Equal(t, 1, fake.Len(bus.StreamOf("ada")))
		require.True(t, friendDeliveryNotes(t, st)[0].CoordinatorDelivery.Sent)
	})
	t.Run("legacy BRIEF never mass resends", func(t *testing.T) {
		t.Parallel()
		ta, st, dir, ps, _ := friendDeliveryFixture(t, 1)
		in := filepath.Join(dir, "inbox", friendJobOf(ps[0]))
		require.NoError(t, os.MkdirAll(in, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(in, "BRIEF.md"), []byte(friendBrief("ada", ps[0])), 0644))
		calls := 0
		ta.a.bus = func(context.Context, bus.Message, func(string)) error { calls++; return nil }
		_, _, err := ta.a.friendCardsOf(context.Background(), st, "ada", dir, func(string) {})
		require.NoError(t, err)
		assert.Zero(t, calls)
		assert.Empty(t, friendDeliveryNotes(t, st))
	})
}
func TestFriendDeliveryExpiryIsTerminalWithoutFiles(t *testing.T) {
	t.Parallel()
	ta, st, dir, ps, cs := friendDeliveryFixture(t, 1)
	ctx := context.Background()
	_, err := ta.a.friendDeliveryPrepare(ctx, st, "ada", dir, config.FriendModeBatch, ps, cs)
	require.NoError(t, err)
	original := *friendDeliveryNotes(t, st)[0].CoordinatorDelivery
	calls := 0
	ta.a.bus = func(context.Context, bus.Message, func(string)) error { calls++; return nil }
	ta.step(bus.DefaultTokenLife)
	require.NoError(t, ta.a.friendDeliveryExpire(ctx, st, "ada", dir))
	n := friendDeliveryNotes(t, st)[0]
	require.True(t, friendDeliveryOf(n).ReceiptUnknown)
	require.False(t, n.CoordinatorDelivery.Sent)
	all, err := st.B.OpenNotes(ctx)
	require.NoError(t, err)
	var judgments []sprint.Note
	for _, o := range all {
		if o.Note.Kind == sprint.Judgment && o.Note.Type == sprint.NFriendNotWoken {
			judgments = append(judgments, o.Note)
		}
	}
	require.Len(t, judgments, 1)
	ta.ok("ack " + judgments[0].ID + " --reason inspected")
	ta.step(8 * 24 * time.Hour)
	_, _, err = ta.a.friendCardsOf(ctx, st, "ada", dir, func(string) {})
	require.NoError(t, err)
	assert.Zero(t, calls)
	notes := friendDeliveryNotes(t, st)
	require.Len(t, notes, 1)
	assert.Equal(t, original.Token, notes[0].CoordinatorDelivery.Token)
	assert.Equal(t, original.RetryBefore, notes[0].CoordinatorDelivery.RetryBefore)
	all, err = st.B.OpenNotes(ctx)
	require.NoError(t, err)
	count := 0
	for _, o := range all {
		if o.Note.Kind == sprint.Judgment && o.Note.Type == sprint.NFriendNotWoken {
			count++
		}
	}
	assert.Zero(t, count, "ack never reraises terminal receipt unknown")
	err = friendDeliveryMarkSent(ctx, st, notes[0].ID)
	require.NoError(t, err)
	assert.False(t, friendDeliveryNotes(t, st)[0].CoordinatorDelivery.Sent, "late success cannot overwrite terminal unknown")
}
func TestFriendDeliveryRejectsChangedOrUnsafeBrief(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"changed", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ta, st, dir, ps, _ := friendDeliveryFixture(t, 1)
			calls := 0
			ta.a.bus = func(context.Context, bus.Message, func(string)) error { calls++; return errors.New("offline") }
			_, _, err := ta.a.friendCardsOf(context.Background(), st, "ada", dir, func(string) {})
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			path := filepath.Join(dir, "inbox", friendJobOf(ps[0]), "BRIEF.md")
			if kind == "changed" {
				require.NoError(t, os.WriteFile(path, []byte("changed"), 0644))
			} else {
				saved := filepath.Join(t.TempDir(), "brief")
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(saved, data, 0644))
				require.NoError(t, os.Remove(path))
				require.NoError(t, os.Symlink(saved, path))
			}
			require.NoError(t, ta.a.friendDeliverySend(context.Background(), st, "ada", dir, func(string) {}))
			assert.Equal(t, 1, calls)
		})
	}
}

func TestFriendDeliveryCurrentAssignmentAndEpochFence(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"withdraw", "clear"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			ta, st, dir, _, _ := friendDeliveryFixture(t, 1)
			calls := 0
			ta.a.bus = func(context.Context, bus.Message, func(string)) error { calls++; return errors.New("offline") }
			_, _, err := ta.a.friendCardsOf(context.Background(), st, "ada", dir, func(string) {})
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			if change == "withdraw" {
				ta.ok("friend take ada s1-1 --reason reassigned")
			} else {
				ta.ok("clear --confirm sprint")
			}
			require.NoError(t, ta.a.friendDeliverySend(context.Background(), st, "ada", dir, func(string) {}))
			assert.Equal(t, 1, calls, "stale or old-epoch delivery never sends")
		})
	}
}

func TestFriendDeliveryDeadlineRecheckedAfterEnrollment(t *testing.T) {
	t.Parallel()
	ta, st, dir, _, _ := friendDeliveryFixture(t, 1)
	calls := 0
	ta.a.bus = func(context.Context, bus.Message, func(string)) error { calls++; return errors.New("offline") }
	_, _, err := ta.a.friendCardsOf(context.Background(), st, "ada", dir, func(string) {})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	oldEnv := ta.a.getenv
	advanced := false
	ta.a.getenv = func(key string) string {
		if key == busRedisEnv && !advanced {
			advanced = true
			ta.step(bus.DefaultTokenLife)
		}
		return oldEnv(key)
	}
	require.NoError(t, ta.a.friendDeliverySend(context.Background(), st, "ada", dir, func(string) {}))
	assert.True(t, advanced)
	assert.Equal(t, 1, calls, "enrollment crossed deadline: no retry starts")
	require.NoError(t, ta.a.friendDeliveryExpire(context.Background(), st, "ada", dir))
	assert.True(t, friendDeliveryOf(friendDeliveryNotes(t, st)[0]).ReceiptUnknown)
}

func TestFriendDeliveryPartialBatchKeepsTokenAcrossRootSpellings(t *testing.T) {
	t.Parallel()
	ta, st, dir, ps, cs := friendDeliveryFixture(t, 2)
	ctx := context.Background()
	_, err := ta.a.friendDeliveryPrepare(ctx, st, "ada", dir, config.FriendModeBatch, ps, cs)
	require.NoError(t, err)
	original := friendDeliveryNotes(t, st)[0].CoordinatorDelivery.Token
	in := filepath.Join(dir, "inbox", friendJobOf(ps[0]))
	require.NoError(t, os.MkdirAll(in, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(in, "BRIEF.md"), []byte(friendBrief("ada", ps[0])), 0644))
	cwd, err := os.Getwd()
	require.NoError(t, err)
	relative, err := filepath.Rel(cwd, dir)
	require.NoError(t, err)
	calls := 0
	ta.a.bus = func(_ context.Context, m bus.Message, _ func(string)) error {
		calls++
		assert.Equal(t, original, m.Token)
		return nil
	}
	restarted := &app{now: ta.a.now, bus: ta.a.bus, getenv: func(string) string { return "" }}
	_, _, err = restarted.friendCardsOf(ctx, st, "ada", relative, func(string) {})
	require.NoError(t, err)
	notes := friendDeliveryNotes(t, st)
	require.Len(t, notes, 1)
	assert.Len(t, friendDeliveryOf(notes[0]).Items, 2)
	assert.Equal(t, original, notes[0].CoordinatorDelivery.Token)
	assert.Equal(t, 1, calls)
	_, _, err = ta.a.friendCardsOf(ctx, st, "ada", dir, func(string) {})
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestFriendDeliveryRejectsSymlinkParentBeforeWriting(t *testing.T) {
	t.Parallel()
	ta, st, dir, ps, _ := friendDeliveryFixture(t, 1)
	require.NoError(t, os.MkdirAll(dir, 0755))
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "inbox")))
	_, _, err := ta.a.friendCardsOf(context.Background(), st, "ada", dir, func(string) {})
	require.Error(t, err)
	_, err = os.Stat(filepath.Join(outside, friendJobOf(ps[0]), "BRIEF.md"))
	assert.True(t, os.IsNotExist(err))
	assert.Empty(t, friendDeliveryNotes(t, st))
	_, why, err := friendInbox(dir, sprint.Packet{Card: "s1-1.r1", Kind: "read", ReadJob: "../outside"})
	require.NoError(t, err)
	assert.NotEmpty(t, why)
}

func TestFriendDeliveryLegacyReadKeepsPreparedBriefAcrossRestart(t *testing.T) {
	t.Parallel()
	ta, st, dir, ps, cs := friendDeliveryFixture(t, 1)
	ps[0].Kind, ps[0].ReadJob = "read", "legacy-read-job"
	ctx := context.Background()
	_, err := ta.a.friendDeliveryPrepare(ctx, st, "ada", dir, config.FriendModeBatch, ps, cs)
	require.NoError(t, err)
	first := friendDeliveryNotes(t, st)[0]
	item := friendDeliveryOf(first).Items[friendJobOf(ps[0])]
	ta.step(time.Minute)
	prepared, err := ta.a.friendDeliveryPrepare(ctx, st, "ada", dir, config.FriendModeBatch, ps, cs)
	require.NoError(t, err)
	require.NotEmpty(t, prepared[friendJobOf(ps[0])])
	_, _, err = ta.a.friendReadOf(ctx, st, "ada", dir, ps[0], cs[0], prepared[friendJobOf(ps[0])], func(string) {}, func(sprint.Packet, string, string) error { return nil })
	require.NoError(t, err)
	text, err := os.ReadFile(filepath.Join(dir, "inbox", friendJobOf(ps[0]), "BRIEF.md"))
	require.NoError(t, err)
	assert.Equal(t, item.BriefHash, friendDeliveryHash(string(text)), "restart must write the originally prepared bytes, not renew the read deadline")
	require.Len(t, friendDeliveryNotes(t, st), 1)
}

func TestFriendDeliveryReadTargetChangeRefusesStaleSend(t *testing.T) {
	t.Parallel()
	for field, replacement := range map[string]string{"head": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "branch": "changed-read-target", "base": "changed-base", "report": "changed report"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			ta, root, id := friendReadTwin(t, false)
			st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
			require.NoError(t, err)
			ctx := context.Background()
			before := ta.fleetCard(id)
			calls := 0
			ta.a.bus = func(context.Context, bus.Message, func(string)) error { calls++; return errors.New("offline") }
			_, _, err = ta.a.friendCardsOf(ctx, st, "amy", filepath.Join(root, "amy-working"), func(string) {})
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			snap, err := st.Load(ctx, []string{sprint.Fleet}, nil)
			require.NoError(t, err)
			work := snap.Fleet.Card("s1-1.w1")
			require.NotNil(t, work)
			_, err = ta.m.Apply(ctx, ntable.BatchManifest{Schema: 1, Table: st.Names.Table(sprint.Fleet), Epoch: fmt.Sprint(st.PinnedEpoch()), ExpectedTableRevision: fmt.Sprint(snap.Fleet.Revision), OperationID: "change-read-target-" + field, Members: []ntable.BatchMemberEntry{{ID: work.ID, Expect: &ntable.MemberExpect{Revision: fmt.Sprint(work.Rev)}, Set: map[string]string{field: replacement}}}})
			require.NoError(t, err)
			after := ta.fleetCard(id)
			require.Equal(t, before.Int("gen"), after.Int("gen"))
			require.Equal(t, before.Int("attempt"), after.Int("attempt"))
			require.NoError(t, ta.a.friendDeliverySend(ctx, st, "amy", filepath.Join(root, "amy-working"), func(string) {}))
			assert.Equal(t, 1, calls, "changed read target at same generation cannot send stale brief")
		})
	}
}
