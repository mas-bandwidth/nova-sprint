package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestDecisionsOwedDeadlineFoldOwnerRetryAndRestart(t *testing.T) {
	ta, _, _ := coordSprint(t, "decision-seat") // no hook proof
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: "decision-seat"}
	_, _, _, startErr := st.SetMachine(ctx, true)
	require.NoError(t, startErr)
	for _, typ := range []string{"decision-a", "decision-b", "decision-c"} {
		_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: typ, What: typ, At: ta.a.now()}))
		require.NoError(t, err)
	}
	b := &tokenBus{}
	ta.a.bus = b.send
	ta.step(sprint.DeadlineJudgment - time.Second)
	ta.a.coordinatorPass(ctx)
	count := func(to string) int {
		n := 0
		for _, m := range b.delivered {
			if len(m.To) == 1 && m.To[0] == to && m.Subject == "nova-sprint: decisions owed" {
				n++
			}
		}
		return n
	}
	require.Zero(t, count("glenn"))
	ta.step(time.Second)
	ta.a.coordinatorPass(ctx)
	require.Equal(t, 1, count("glenn"), "at deadline owner receives one folded message despite unproven hook")
	require.Equal(t, 1, count("decision-seat"))
	for _, m := range b.delivered {
		if m.Subject == "nova-sprint: decisions owed" {
			for _, word := range []string{"decision-a", "decision-b", "decision-c"} {
				require.Contains(t, m.Body, word)
			}
		}
	}
	ta.step(29 * time.Minute)
	ta.a.coordinatorPass(ctx)
	require.Equal(t, 1, count("glenn"))
	require.Equal(t, 1, count("decision-seat"))
	// A server restart loses process memory; durable escalation receipts remain.
	ta.a.coord = coordPush{}
	ta.step(time.Minute)
	ta.a.coordinatorPass(ctx)
	require.Equal(t, 2, count("glenn"))
	require.Equal(t, 1, count("decision-seat"), "unchanged overdue set is never repeated to coordinator")
	// A changed set is prepared durably before a failed send, and its fault is visible.
	_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: "decision-d", At: ta.a.now().Add(-sprint.DeadlineJudgment)}))
	require.NoError(t, err)
	ta.step(30 * time.Minute)
	b.fails = 2
	ta.a.coordinatorPass(ctx)
	var view string
	_, view, err = ta.a.whereOf(ctx, st, 0, false, true)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(view, "DECISIONS OWED 4, oldest "), view)
	require.Contains(t, strings.Split(view, "\n")[0], "delivery faults 2")
	before := len(b.attempts)
	ta.a.coord = coordPush{}
	ta.a.coordinatorPass(ctx)
	require.Greater(t, len(b.attempts), before)
	require.Equal(t, 3, count("glenn"))
	require.Equal(t, 2, count("decision-seat"))
	_, view, err = ta.a.whereOf(ctx, st, 0, false, true)
	require.NoError(t, err)
	require.Contains(t, strings.Split(view, "\n")[0], "delivery faults 0")
}

// A review or merge judgment is pushed on the existing durable coordinator
// lane immediately and then once each minute until the judgment closes. The
// bus receipt is delivery, not an answer; a process restart keeps the receipt.
func TestReviewAndMergeJudgmentsRepeatUntilResolved(t *testing.T) {
	for _, typ := range []string{sprint.NReadBroken, sprint.NMergeLate} {
		t.Run(typ, func(t *testing.T) {
			ta, _, _ := coordSprint(t, "repeat-seat")
			ctx := context.Background()
			st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: "repeat-seat"}
			b := &tokenBus{}
			ta.a.bus = b.send
			add := func(head string) {
				_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, Type: typ, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, At: ta.a.now(), What: "head=" + head + " needs a decision"}))
				require.NoError(t, err)
			}
			count := func() int {
				n := 0
				for _, m := range b.delivered {
					if m.Subject == "nova-sprint: decisions owed" && len(m.To) == 1 && m.To[0] == "repeat-seat" {
						n++
					}
				}
				return n
			}
			add("head-1")
			ta.a.coordinatorPass(ctx)
			require.Equal(t, 1, count(), "first raise is immediate")
			ta.step(sprint.RepeatedDecisionEvery - time.Second)
			ta.a.coordinatorPass(ctx)
			require.Equal(t, 1, count(), "the accepted bus receipt is not a resolution or an early repeat")
			ta.a.coord = coordPush{} // restart loses process memory, not the persisted receipt
			ta.a.coordinatorPass(ctx)
			require.Equal(t, 1, count(), "restart does not duplicate the delivered judgment")
			ta.step(time.Second)
			ta.a.coordinatorPass(ctx)
			require.Equal(t, 2, count(), "same unresolved judgment repeats at exactly 60 seconds")
			require.Contains(t, b.delivered[len(b.delivered)-1].Body, "head=head-1")
			_, err := st.Run(ctx, store.Step{Verb: "fixture resolve", Plan: func(s *sprint.Snapshot) sprint.Plan {
				var p sprint.Plan
				for _, o := range s.Open {
					if o.Note.Type == typ && strings.Contains(o.Note.What, "head=head-1") {
						p.Closes = append(p.Closes, o)
					}
				}
				return p
			}})
			require.NoError(t, err)
			ta.step(2 * sprint.RepeatedDecisionEvery)
			ta.a.coordinatorPass(ctx)
			require.Equal(t, 2, count(), "resolved judgment stops repeating")
			add("head-2")
			ta.a.coordinatorPass(ctx)
			require.Equal(t, 3, count(), "a new head is a new judgment episode")
			require.Contains(t, b.delivered[len(b.delivered)-1].Body, "head-2")
			require.NotContains(t, b.delivered[len(b.delivered)-1].Body, "head-1")
		})
	}
}

func TestWhereDecisionsOwedIsFirstEvenWhenEmpty(t *testing.T) {
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	text := ta.ok("where")
	require.True(t, strings.HasPrefix(text, "DECISIONS OWED 0, oldest 0s; server-defaults applied this hour 0; delivery faults 0; earlier receipts unknown 0\n"), text)
}

func TestDecisionsOwedAmbiguousExpiryRenewsAndKeepsUnknownHistory(t *testing.T) {
	ta, _, _ := coordSprint(t, "ambiguous-seat")
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: "ambiguous-seat"}
	_, _, _, startErr := st.SetMachine(ctx, true)
	require.NoError(t, startErr)
	_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: "manual-decision", At: ta.a.now().Add(-sprint.DeadlineJudgment)}))
	require.NoError(t, err)
	ta.step(sprint.DeadlineJudgment)
	b := &tokenBus{}
	ta.a.bus = func(ctx context.Context, m bus.Message, say func(string)) error {
		if m.Subject != "nova-sprint: decisions owed" {
			return nil
		}
		if err := b.send(ctx, m, say); err != nil {
			return err
		}
		return errors.New("reply lost after bus acceptance")
	}
	ta.a.coordinatorPass(ctx)
	require.Len(t, b.delivered, 2)
	first := append([]bus.Message(nil), b.delivered...)
	ta.step(bus.DefaultTokenLife + time.Second)
	ta.a.bus = func(ctx context.Context, m bus.Message, say func(string)) error {
		if m.Subject != "nova-sprint: decisions owed" {
			return nil
		}
		return b.send(ctx, m, say)
	}
	ta.a.coord = coordPush{}
	ta.a.coordinatorPass(ctx)
	require.Len(t, b.delivered, 4, "expired ambiguous token must not leave the owner silent")
	for i := range 2 {
		require.NotEqual(t, first[i].Token, b.delivered[i+2].Token)
		require.Equal(t, first[i].Body, b.delivered[i+2].Body)
		require.NotEmpty(t, b.delivered[i+2].From)
	}
	var out string
	_, out, err = ta.a.whereOf(ctx, st, 0, false, true)
	require.NoError(t, err)
	require.Contains(t, strings.Split(out, "\n")[0], "delivery faults 0; earlier receipts unknown 2")
	open, err := st.B.OpenNotes(ctx)
	require.NoError(t, err)
	found := 0
	for _, o := range open {
		if strings.Contains(o.Note.What, "earlier receipt") {
			found++
			require.Contains(t, o.Note.What, "judgment IDs=")
		}
	}
	require.Equal(t, 2, found)
	// A later changed set archives the ambiguous history without hiding it.
	_, err = st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: "next-decision"}))
	require.NoError(t, err)
	ta.step(sprint.DeadlineJudgment)
	ta.a.coordinatorPass(ctx)
	_, out, err = ta.a.whereOf(ctx, st, 0, false, true)
	require.NoError(t, err)
	require.Contains(t, strings.Split(out, "\n")[0], "earlier receipts unknown 2")
	require.Contains(t, out, "DECISION RECEIPT HISTORY:")
	require.Contains(t, out, "judgment IDs=")
	for _, o := range open {
		if o.Note.Kind == sprint.Judgment {
			require.Contains(t, out, o.Note.ID, "old ambiguous judgment stays visible after set replacement")
		}
	}
}

func TestDecisionsOwedLargeReasonsFitTransport(t *testing.T) {
	ta, _, _ := coordSprint(t, "large-seat")
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: "large-seat"}
	_, _, _, startErr := st.SetMachine(ctx, true)
	require.NoError(t, startErr)
	var ids []string
	for i := 0; i < 20; i++ {
		typ := fmt.Sprintf("judgment-%03d", i)
		_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: typ, What: strings.Repeat("x", 60*1024), At: ta.a.now()}))
		require.NoError(t, err)
		ids = append(ids, typ)
	}
	b := &tokenBus{}
	ta.a.bus = b.send
	ta.step(sprint.DeadlineJudgment)
	ta.a.coordinatorPass(ctx)
	found := false
	for _, m := range b.delivered {
		if m.Subject != "nova-sprint: decisions owed" {
			continue
		}
		found = true
		require.LessOrEqual(t, len(m.Body), bus.MaxBody)
		for _, id := range ids {
			require.Contains(t, m.Body, id)
		}
		require.Contains(t, m.Body, "full reason in inbox")
	}
	require.True(t, found)
}

func TestDecisionsOwedPendingRecipientChangeKeepsFrozenObligation(t *testing.T) {
	ta, _, env := coordSprint(t, "old-seat")
	env[busUserEnv], env[busPasswordEnvEnv], env["TEST_PASSWORD"] = "server-one", "TEST_PASSWORD", "fixture"
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: "old-seat"}
	_, _, _, err := st.SetMachine(ctx, true)
	require.NoError(t, err)
	_, err = st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: "transfer-decision"}))
	require.NoError(t, err)
	ta.step(sprint.DeadlineJudgment)
	b := &tokenBus{}
	ta.a.bus = func(ctx context.Context, m bus.Message, say func(string)) error {
		if m.Subject != "nova-sprint: decisions owed" {
			return nil
		}
		_ = b.send(ctx, m, say)
		return errors.New("receipt lost")
	}
	ta.a.coordinatorPass(ctx)
	require.Len(t, b.delivered, 2)
	frozen := b.delivered[0]
	require.NoError(t, ta.m.SetCoordinator(ctx, "new-seat"))
	env[busUserEnv] = "server-two"
	ta.a.bus = func(ctx context.Context, m bus.Message, say func(string)) error {
		if m.Subject != "nova-sprint: decisions owed" {
			return nil
		}
		return b.send(ctx, m, say)
	}
	ta.a.coordinatorPass(ctx)
	require.Len(t, b.delivered, 4)
	require.Equal(t, []string{"new-seat"}, b.delivered[2].To)
	require.Equal(t, "server-two", b.delivered[2].From)
	require.Equal(t, "server-two", b.delivered[3].From)
	require.Equal(t, frozen.Body, b.delivered[2].Body)
	require.NotEqual(t, frozen.Token, b.delivered[2].Token)
	_, frame, err := ta.a.whereOf(ctx, st, 0, false, true)
	require.NoError(t, err)
	require.Contains(t, strings.Split(frame, "\n")[0], "delivery faults 0; earlier receipts unknown 2")
}

func TestDecisionsWhereBatchesSeatServerCoordinator(t *testing.T) {
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1 --coordinator batched-seat")
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: "batched-seat"}
	require.NoError(t, st.SetServer(ctx, store.ServerRecord{Actor: "server", Version: "test-version", At: ta.a.now()}))
	seat, _, err := st.Seat(ctx)
	require.NoError(t, err)
	ta.m.Calls = map[string]int{}
	got, moved, server, fresh, coordinator, err := st.SeatServerCoordinator(ctx)
	require.NoError(t, err)
	require.Equal(t, seat, got)
	require.Equal(t, seat.Holder != "", moved)
	require.True(t, fresh)
	require.Equal(t, "test-version", server.Version)
	require.Equal(t, "batched-seat", coordinator)
	require.Equal(t, map[string]int{"kv": 1}, ta.m.Calls, "three actual keys share the backend multi-key read")
}

func TestDecisionsOwnerChangedSetWaitsFullWindow(t *testing.T) {
	ta, _, _ := coordSprint(t, "cadence-seat")
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: "cadence-seat"}
	_, _, _, err := st.SetMachine(ctx, true)
	require.NoError(t, err)
	add := func(typ string) {
		_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: typ, At: ta.a.now().Add(-sprint.DeadlineJudgment)}))
		require.NoError(t, err)
	}
	b := &tokenBus{}
	ta.a.bus = b.send
	add("first")
	ta.step(sprint.DeadlineJudgment)
	ta.a.decisionPass(ctx)
	count := func() int {
		n := 0
		for _, m := range b.delivered {
			if len(m.To) == 1 && m.To[0] == "glenn" {
				n++
			}
		}
		return n
	}
	require.Equal(t, 1, count())
	ta.step(time.Minute)
	add("second")
	ta.a.decisionPass(ctx)
	require.Equal(t, 1, count(), "changed sets must not bypass the owner's 30-minute bound")
	ta.step(29*time.Minute - time.Second)
	ta.a.decisionPass(ctx)
	require.Equal(t, 1, count())
	ta.step(time.Second)
	ta.a.decisionPass(ctx)
	require.Equal(t, 2, count())
	require.Contains(t, b.delivered[len(b.delivered)-1].Body, "second")
}

func TestDecisionsParkedCardRemainsOwedUntilRelease(t *testing.T) {
	ta, rig, _ := coordSprint(t, "parked-seat")
	rig.Hook(t, "parked-seat", hook.Coordinator)
	ta.lined(func() { ta.ok("add --stream s1 --count 1 --one") })
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: "parked-seat"}
	_, _, _, err := st.SetMachine(ctx, true)
	require.NoError(t, err)
	_, err = st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, Type: sprint.NBound, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, At: ta.a.now(), What: "budget bound"}))
	require.NoError(t, err)
	ta.step(2 * sprint.DeadlineJudgment)
	_, err = st.Run(ctx, store.TickPartStep("overdue", sprint.TickOverdue, sprint.TickReq{}, nil, nil, nil))
	require.NoError(t, err)
	_, err = st.Run(ctx, store.DrainStep())
	require.NoError(t, err)
	// A second actual overdue pass retires the legacy alarm for the now-closed judgment.
	_, err = st.Run(ctx, store.TickPartStep("overdue", sprint.TickOverdue, sprint.TickReq{}, nil, nil, nil))
	require.NoError(t, err)
	_, err = st.Run(ctx, store.DrainStep())
	require.NoError(t, err)
	b := &tokenBus{}
	ta.a.bus = b.send
	ta.a.decisionPass(ctx)
	ta.step(2 * time.Hour)
	v, _, err := ta.a.whereOf(ctx, st, 0, false, true)
	require.NoError(t, err)
	require.Equal(t, 1, v.Decisions.Count, "parked card remains owed after the report hour expires")
	ta.a.decisionPass(ctx)
	require.Contains(t, b.delivered[len(b.delivered)-1].Body, "s1-1")
	require.Contains(t, b.delivered[len(b.delivered)-1].Body, "server-default")
	_, err = st.Run(ctx, store.ReleaseStep(sprint.ReleaseReq{IDs: []string{"s1-1"}, Who: "parked-seat", Coordinator: "parked-seat", Reason: "reviewed"}))
	require.NoError(t, err)
	_, err = st.Run(ctx, store.DrainStep())
	require.NoError(t, err)
	v, _, err = ta.a.whereOf(ctx, st, 0, false, true)
	require.NoError(t, err)
	require.Zero(t, v.Decisions.Count)
}

func TestDecisionsOldestExcludesStoppedTime(t *testing.T) {
	ta, _, _ := coordSprint(t, "clock-seat")
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: "clock-seat"}
	_, _, _, err := st.SetMachine(ctx, true)
	require.NoError(t, err)
	_, err = st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: "clock", At: ta.a.now()}))
	require.NoError(t, err)
	ta.step(time.Minute)
	_, _, _, err = st.SetMachine(ctx, false)
	require.NoError(t, err)
	ta.step(time.Hour)
	v, _, err := ta.a.whereOf(ctx, st, 0, false, true)
	require.NoError(t, err)
	require.Equal(t, time.Minute, v.Decisions.Oldest)
}
