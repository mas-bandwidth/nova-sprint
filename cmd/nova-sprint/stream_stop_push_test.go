package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/bus/bustest"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/stretchr/testify/require"
)

// CoordinatorPush.tla: two durable transition notes, never folded into their
// judgment; no time advance is needed, and a failed send keeps its obligation.
func TestStreamStopAndResumePushABlockerToTheSeat(t *testing.T) {
	t.Parallel()
	for _, drop := range []bool{false, true} {
		name := "stream-push"
		if drop {
			name += "-drop"
		}
		t.Run(name, func(t *testing.T) {
			ta, fake := streamNoticeRig(t, name)
			ctx := context.Background()
			before := ta.a.now()
			ta.ok("merge --stream s1 --batch 1 --conflict s1-2 --note 'landing refusal'")
			// Both events may commit before a single transport step. Closing the stop
			// judgment must not discard the first event.
			ta.ok("resume --stream s1 --did 'resolved landing refusal'")
			if drop {
				fake.Fail = errors.New("dropped send")
			}
			ta.a.coordinatorPass(ctx)
			require.Equal(t, before, ta.a.now(), "event pass cannot depend on a poll cadence")
			if drop {
				require.Zero(t, fake.Len(bus.StreamOf(name)))
				fake.Fail = nil
				ta.step(coordPushLook)
				fake.Advance(coordPushLook)
				ta.a.coordinatorPass(ctx)
			}
			for _, to := range []string{name, "amy", "bea"} {
				es, err := fake.Range(ctx, bus.StreamOf(to), "-", "+", 0)
				require.NoError(t, err)
				var notices []bus.Message
				for _, e := range es {
					m := e.Message()
					if strings.HasPrefix(m.Subject, "STREAM ") {
						notices = append(notices, m)
					}
				}
				require.Len(t, notices, 2, "recipient=%s entries=%+v", to, es)
				require.Equal(t, "STREAM STOPPED s1 cause=conflict waiting=2 at="+before.UTC().Format(time.RFC3339), notices[0].Subject)
				require.Equal(t, "STREAM RESUMED s1 -> merging at="+before.UTC().Format(time.RFC3339), notices[1].Subject)
				kind := bus.KindStatus
				if to == name {
					kind = bus.KindBlocker
				}
				require.Equal(t, kind, notices[0].Kind)
				require.Equal(t, kind, notices[1].Kind)
				require.Contains(t, notices[1].Body, "resolved landing refusal")
			}
			counts := []int{fake.Len(bus.StreamOf(name)), fake.Len(bus.StreamOf("amy")), fake.Len(bus.StreamOf("bea"))}
			ta.a.coordinatorPass(ctx)
			require.Equal(t, counts, []int{fake.Len(bus.StreamOf(name)), fake.Len(bus.StreamOf("amy")), fake.Len(bus.StreamOf("bea"))})
		})
	}
}

// Exercise the actual WaitLog driver, not a manual pass. The memory store's
// fake wait commits the event while the loop is waiting; its next pass delivers
// after the loop's floor (coordPushFloor: a pass at most a second) and no more.
// Dropped sends retry at the quiet-log backstop (coordPushLook).
func TestStreamPushLoopWakesOnCommitAndBackstop(t *testing.T) {
	t.Parallel()
	for _, drop := range []bool{false, true} {
		name := "stream-loop"
		if drop {
			name += "-drop"
		}
		t.Run(name, func(t *testing.T) {
			ta, rig, _ := coordSprint(t, name)
			rig.Hook(t, name, hook.Coordinator)
			ta.ok("add --stream s1 --count 1 --one")
			fake := bustest.NewFake(ta.a.now(), name)
			b := &bus.Bus{Store: fake}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ta.a.bus = func(ctx context.Context, m bus.Message, _ func(string)) error {
				_, err := b.Send(ctx, m)
				if err == nil && strings.HasPrefix(m.Subject, "STREAM RESUMED ") {
					cancel()
				}
				return err
			}
			before := ta.a.now()
			waits := 0
			ta.m.LogWait = func(d time.Duration) {
				require.Equal(t, coordPushLook, d)
				waits++
				switch waits {
				case 1:
					ta.lined(func() { ta.ok("merge --stream s1 --base-red 'base refuses'") })
					ta.lined(func() { ta.ok("resume --stream s1 --did 'fixed base'") })
					if drop {
						fake.Fail = errors.New("dropped event send")
					}
				case 2:
					require.True(t, drop, "healthy event must arrive before the next quiet-log wait")
					require.Equal(t, before.Add(coordPushFloor), ta.a.now())
					fake.Fail = nil
					ta.step(d)
					fake.Advance(d)
				default:
					cancel()
					t.Fatal("transition not delivered at event or backstop")
				}
			}
			ta.a.coordPushLoop(ctx)
			wantWaits := 1
			wantTime := before.Add(coordPushFloor)
			if drop {
				wantWaits = 2
				wantTime = before.Add(coordPushFloor + coordPushLook)
			}
			require.Equal(t, wantWaits, waits)
			require.Equal(t, wantTime, ta.a.now())
			entries, err := fake.Range(context.Background(), bus.StreamOf(name), "-", "+", 0)
			require.NoError(t, err)
			var notices []bus.Message
			for _, e := range entries {
				m := e.Message()
				if strings.HasPrefix(m.Subject, "STREAM ") {
					notices = append(notices, m)
				}
			}
			require.Len(t, notices, 2)
			require.Equal(t, bus.KindBlocker, notices[0].Kind)
			require.Equal(t, bus.KindBlocker, notices[1].Kind)
			require.Contains(t, notices[0].Subject, "STREAM STOPPED s1 cause=base")
			require.Contains(t, notices[1].Subject, "STREAM RESUMED s1 -> waiting")
		})
	}
}

// streamNoticeRig has two queued cards whose completed work records are
// unplaced, each with a different friend owner. It exercises store wrappers.
func streamNoticeRig(t *testing.T, name string) (*testApp, *bustest.Fake) {
	t.Helper()
	ta, rig, _ := coordSprint(t, name)
	rig.Hook(t, name, hook.Coordinator)
	ta.ok("fleet down m2")
	ta.toMergingAdded("s1")
	ta.ok("merge --stream s1 --batch 1")
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
	res, err := st.Run(ctx, store.Step{Verb: "fixture owners", Load: []string{sprint.Fleet}, Extras: func(*sprint.Snapshot) map[string][]string {
		return map[string][]string{sprint.Fleet: {"s1-2.w1", "s1-3.w1"}}
	}, Plan: func(s *sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Units: []sprint.Unit{{Key: "owners", Changes: []sprint.Change{
			{Table: sprint.Fleet, Entry: ntable.BatchMemberEntry{ID: "s1-2.w1", Expect: &ntable.MemberExpect{Revision: strconv.FormatUint(s.Fleet.Card("s1-2.w1").Rev, 10)}, Set: map[string]string{"member": "friend.amy"}}},
			{Table: sprint.Fleet, Entry: ntable.BatchMemberEntry{ID: "s1-3.w1", Expect: &ntable.MemberExpect{Revision: strconv.FormatUint(s.Fleet.Card("s1-3.w1").Rev, 10)}, Set: map[string]string{"member": "friend.bea"}}},
		}}}}
	}})
	require.NoError(t, err)
	require.Empty(t, res.Refused)
	fake := bustest.NewFake(ta.a.now(), name, "amy", "bea")
	b := &bus.Bus{Store: fake}
	ta.a.bus = func(ctx context.Context, m bus.Message, _ func(string)) error { _, err := b.Send(ctx, m); return err }
	return ta, fake
}

// Redo writes its resume directly; the two automatic resume parts run through
// the real tick store loader, which must also fetch unplaced owner records.
func TestStreamResumeNoticesFromRedoAndTick(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"redo", "tick-cross"} {
		t.Run(mode, func(t *testing.T) {
			name := "stream-" + mode
			ta, fake := streamNoticeRig(t, name)
			if mode == "redo" {
				ta.ok("merge --stream s1 --batch 1 --conflict s1-2 --note 'landing refusal'")
				ta.ok("redo s1-2")
			} else {
				ta.toMergingAdded("s2")
				ta.ok("merge --stream s1 --batch 1 --cross s1-2=s2-1")
				ta.ok("merge --stream s2 --batch 1")
				ta.ok("start")
				ta.ok("tick")
			}
			ta.a.coordinatorPass(context.Background())
			for _, to := range []string{name, "amy", "bea"} {
				entries, err := fake.Range(context.Background(), bus.StreamOf(to), "-", "+", 0)
				require.NoError(t, err)
				var notices []bus.Message
				for _, e := range entries {
					m := e.Message()
					if strings.HasPrefix(m.Subject, "STREAM ") && strings.Contains(m.Subject, " s1 ") {
						notices = append(notices, m)
					}
				}
				require.Len(t, notices, 2, "%s: %+v", to, entries)
				require.Contains(t, notices[0].Subject, "STREAM STOPPED s1 ")
				require.Contains(t, notices[1].Subject, "STREAM RESUMED s1 -> merging")
				kind := bus.KindStatus
				if to == name {
					kind = bus.KindBlocker
				}
				require.Equal(t, kind, notices[0].Kind)
				require.Equal(t, kind, notices[1].Kind)
			}
		})
	}
}

func TestStreamNoticeOwnerWhoHoldsSeatGetsBothRoles(t *testing.T) {
	t.Parallel()
	n := sprint.Note{ID: "stop", Type: sprint.NStreamStopped, Stream: "s1", At: t0, StreamTransition: &sprint.StreamTransition{State: sprint.StreamStopped, Cause: "base", Waiting: 2, Owners: []string{"seat", "seat"}}}
	l := coordLook{holder: "seat", streamEvents: []sprint.Note{n}}
	told := map[string]bool{}
	sends := coordFresh(told, l, t0)
	require.Len(t, sends, 2)
	require.Equal(t, bus.KindBlocker, sends[0].kind)
	require.Equal(t, bus.KindStatus, sends[1].kind)
	require.NotEqual(t, sends[0].token, sends[1].token)
	for _, d := range sends {
		for _, key := range d.keys {
			told[key] = true
		}
	}
	require.Empty(t, coordFresh(told, l, t0))
}

func TestStreamNoticeAmbiguousRetryPreservesOrder(t *testing.T) {
	t.Parallel()
	const name = "stream-ambiguous"
	ta, fake := streamNoticeRig(t, name)
	ta.ok("merge --stream s1 --batch 1 --conflict s1-2 --note 'landing refusal'")
	ta.ok("resume --stream s1 --did 'resolved landing refusal'")
	fake.Lose = errors.New("committed stop response lost")
	ta.a.coordinatorPass(context.Background())
	entries, err := fake.Range(context.Background(), bus.StreamOf(name), "-", "+", 0)
	require.NoError(t, err)
	for _, e := range entries {
		require.NotContains(t, e.Message().Subject, "STREAM RESUMED", "resume cannot overtake ambiguous stop for its recipient")
	}
	ta.a.coordinatorPass(context.Background())
	for _, to := range []string{name, "amy", "bea"} {
		entries, err := fake.Range(context.Background(), bus.StreamOf(to), "-", "+", 0)
		require.NoError(t, err)
		var subjects []string
		for _, e := range entries {
			if subject := e.Message().Subject; strings.HasPrefix(subject, "STREAM ") {
				subjects = append(subjects, subject)
			}
		}
		require.Len(t, subjects, 2, to)
		require.Contains(t, subjects[0], "STREAM STOPPED")
		require.Contains(t, subjects[1], "STREAM RESUMED")
	}
}
