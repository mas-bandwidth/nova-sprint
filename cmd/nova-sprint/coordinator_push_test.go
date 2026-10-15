package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus/bustest"
	"github.com/mas-bandwidth/nova-tools/hook"
)

// toSeat is every message the app sent the seat's holder on the bus, from the sprint.
func (ta *testApp) toSeat(name string) []bus.Message {
	ta.mu.Lock()
	defer ta.mu.Unlock()
	var out []bus.Message
	for _, m := range ta.sent {
		if m.From != prog && len(m.To) == 1 && m.To[0] == name {
			out = append(out, m)
		}
	}
	return out
}

// pass is one look of the server's coordinator push, and what it sent the holder.
func (ta *testApp) pass(name string) []bus.Message {
	ta.t.Helper()
	before := len(ta.toSeat(name))
	ta.a.coordinatorPass(context.Background())
	return ta.toSeat(name)[before:]
}

// timerJudgment raises a judgment for the coordinator: a reminder past its due time,
// raised by the tick.
func timerJudgment(ta *testApp, name string) {
	ta.t.Helper()
	ta.lined(func() { ta.ok("add --stream s1 --count 2") })
	ta.lined(func() { ta.ok("start") }) // the machine runs, so its tick raises the timer
	ta.lined(func() { ta.ok("remind --in 1m --note look-here --for " + name) })
	ta.step(2 * time.Minute) // past its due time: the tick raises the timer's judgment now
	ta.lined(func() { ta.ok("tick") })
}

// A judgment reaches the coordinator's name on the bus (v1.3.6: the seat hook's stream and
// the seat push are retired; nova-hook's bus push puts the message into the session): the
// server's look sends every group for the coordinator not told yet, once, in one message
// that asks (a judgment is in it), with the commands that answer it; the next look sends
// nothing; a note joining the group is new; and a send that fails tells nothing, so the
// next look sends it again.
func TestAJudgmentReachesTheCoordinatorsBusOnce(t *testing.T) {
	t.Parallel()
	const name = "push-once"
	ta, rig, _ := coordSprint(t, name)
	rig.Hook(t, name, hook.Coordinator)
	timerJudgment(ta, name)

	sent := ta.pass(name)
	require.Len(t, sent, 1, "%+v", sent)
	m := sent[0]
	assert.Equal(t, bus.KindRequest, m.Kind)
	assert.Equal(t, "nova-sprint: 1 new for the coordinator", m.Subject)
	assert.True(t, strings.HasPrefix(m.Body, "NOVA SPRINT INBOX: new for the coordinator\n"), m.Body)
	assert.Contains(t, m.Body, "JUDGMENT ")
	assert.Contains(t, m.Body, "look-here")
	assert.Empty(t, ta.pass(name), "told once: the next look sends nothing")

	// a second judgment, and the bus refuses the send: nothing is told
	ta.lined(func() { ta.ok("remind --in 1m --note look-again --for " + name) })
	ta.step(2 * time.Minute)
	ta.lined(func() { ta.ok("tick") })
	send := ta.a.bus
	ta.a.bus = func(context.Context, bus.Message, func(string)) error { return errors.New("the bus store is down") }
	ta.a.coordinatorPass(context.Background())
	ta.a.bus = send
	sent = ta.pass(name)
	require.Len(t, sent, 2, "the failed delivery and failed warning both retry at the next look: %+v", sent)
	assert.Equal(t, "nova-sprint: judgments waiting on the coordinator", sent[1].Subject)
	assert.Contains(t, sent[0].Body, "JUDGMENT ", "the group with the new reminder in it")
	assert.Empty(t, ta.pass(name), "and once it went, it is told")
}

// The server holds the coordinator's sprint push while its sends reach the bus (the gate
// wants it, so a coordinator command runs only while the judgments have a way to the
// seat): hooked in with nova-hook (the bus push), the server's look proves the sprint push
// and the verb runs; a send to the seat that fails unproves it at the next renewal, and
// the verb is refused; the next send that goes proves it again.
func TestTheServerHoldsTheSprintPushWhileItsSendsGo(t *testing.T) {
	t.Parallel()
	const name = "push-hold"
	ta, rig, _ := coordSprint(t, name)
	hookBusOnly(t, rig, name)
	ta.refusedFirst("where", sprint.HookFirst+": "+name+" is not hooked in: partial as coordinator")
	ta.pass(name)
	ta.lined(func() { ta.ok("where") })
	timerJudgment(ta, name)

	send := ta.a.bus
	ta.a.bus = func(context.Context, bus.Message, func(string)) error { return errors.New("the bus store is down") }
	ta.step(hook.RenewEvery)
	ta.a.coordinatorPass(context.Background())
	ta.refusedFirst("where", sprint.HookFirst+": "+name+" is not hooked in: live as coordinator (bus=proven sprint=armed)")

	ta.a.bus = send
	ta.step(hook.RenewEvery)
	require.NotEmpty(t, ta.pass(name))
	ta.lined(func() { ta.ok("where") })
}

// The waiting-judgment warning goes on the bus (the owner, 2026-10-11: "Any card waiting
// for judgement should alert you via warning [...] once it has been in this state for 10
// seconds or longer." "These must be push notifications."): a judgment open 9 s, nothing;
// at 10 s one warning naming it and the command that answers it; a second later nothing
// (the same set waits its minute, sprint.JudgmentWarnEvery); a minute later, again as one
// line; answered with the command it names, none.
func TestAWaitingJudgmentIsWarnedOnTheBus(t *testing.T) {
	t.Parallel()
	const name = "push-warn"
	ta, rig, _ := coordSprint(t, name)
	rig.Hook(t, name, hook.Coordinator)
	timerJudgment(ta, name)
	require.Len(t, ta.pass(name), 1, "the judgment itself first")

	ta.step(9 * time.Second)
	assert.Empty(t, ta.pass(name), "9 s: no warning")
	ta.step(time.Second)
	w := ta.pass(name)
	require.Len(t, w, 1)
	assert.Equal(t, "nova-sprint: judgments waiting on the coordinator", w[0].Subject)
	assert.Equal(t, bus.KindRequest, w[0].Kind)
	assert.Contains(t, w[0].Body, "WARNING 1 judgment(s) waiting on the coordinator 10s or longer")
	assert.Contains(t, w[0].Body, "waited=10s")
	ta.step(time.Second)
	assert.Empty(t, ta.pass(name), "the same set a second later: it waits its minute")

	// the machine ticks through the minute (else it reads silent, a judgment of its own)
	ta.lined(func() { ta.step(sprint.JudgmentWarnEvery - time.Second); ta.ok("tick") })
	w2 := ta.pass(name)
	require.Len(t, w2, 1)
	assert.Equal(t, "WARNING 1 judgment(s) waiting on the coordinator 10s or longer: j1\n", w2[0].Body, "the set has not changed: its one line")

	// answered with the command it names: no more warnings
	ta.lined(func() { ta.ok(strings.Join(warningAnswer(t, w[0].Body), " ")) })
	ta.step(time.Second)
	assert.Empty(t, ta.pass(name), "answered: no warning")
}

// warningAnswer is the first command the warning names to answer its first judgment, as
// the words after "nova-sprint".
func warningAnswer(t *testing.T, text string) []string {
	t.Helper()
	for _, l := range strings.Split(text, "\n") {
		if !strings.HasPrefix(l, "  ") {
			continue
		}
		_, cmdline, ok := strings.Cut(strings.TrimSpace(l), ": ")
		require.True(t, ok, l)
		first, _, _ := strings.Cut(cmdline, " && ")
		words := strings.Fields(first)
		require.Equal(t, "nova-sprint", words[0], l)
		return words[1:]
	}
	t.Fatalf("the warning names no answer: %s", text)
	return nil
}

// coordFresh delivers each key once and forgets what closed: a group for the coordinator
// with a key not told is in the holder's message; a told key is not again; a note added to
// a told group is new; a happened note with no one to tell is never sent; a note addressed
// to someone else (a timer for a friend) goes to that name, after the holder's; keys of
// groups no longer open are forgotten.
func TestCoordFreshTellsEachKeyOnceAndForgetsTheClosed(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	j := sprint.Group{ID: "n-1", Alias: "j1", Kind: sprint.Judgment, Notes: []string{"n-1"}, Oldest: now, What: "look here"}
	quiet := sprint.Group{ID: "n-9", Kind: sprint.Happened, Notes: []string{"n-9"}}
	told := map[string]bool{}
	sends := coordFresh(told, coordLook{holder: "rowan", groups: []sprint.Group{quiet, j}}, now)
	require.Len(t, sends, 1)
	assert.Equal(t, "rowan", sends[0].to)
	assert.Equal(t, bus.KindRequest, sends[0].kind)
	assert.Equal(t, []string{"n-1"}, sends[0].groups, "a happened note for no one is not the coordinator's")
	assert.Contains(t, sends[0].text, "NOVA SPRINT INBOX: new for the coordinator\nJUDGMENT n-1")
	for _, k := range sends[0].keys {
		told[k] = true
	}
	assert.Empty(t, coordFresh(told, coordLook{holder: "rowan", groups: []sprint.Group{j}}, now), "told once")

	j.Notes = []string{"n-1", "n-2"}
	sends = coordFresh(told, coordLook{holder: "rowan", groups: []sprint.Group{j}}, now)
	require.Len(t, sends, 1)
	assert.Equal(t, []string{"n-2"}, sends[0].keys, "a new note in a told group is new")

	amy := sprint.Group{ID: "n-5", Kind: sprint.Judgment, To: "amy", Notes: []string{"n-5"}, Oldest: now}
	done := sprint.Group{ID: "n-6", Kind: sprint.Happened, To: "rowan", Notes: []string{"n-6"}}
	sends = coordFresh(told, coordLook{holder: "rowan", groups: []sprint.Group{amy, done}}, now)
	require.Len(t, sends, 2)
	assert.Equal(t, "rowan", sends[0].to, "the holder's first")
	assert.Equal(t, bus.KindReport, sends[0].kind, "a note addressed to the holder reports")
	assert.Equal(t, "amy", sends[1].to, "a timer for a friend goes to her")
	assert.True(t, strings.HasPrefix(sends[1].text, "NOVA SPRINT INBOX: new for amy\n"), sends[1].text)
	assert.NotContains(t, told, "n-1", "the closed judgment's keys are forgotten")
}

// An auto-hold is told the coordinator once (the owner, 2026-10-14: "if a machine is auto
// held, you should get a warning notify"): a member's push refused for want of a credential
// holds the member with one judgment, and the server's look sends the coordinator exactly
// one bus message for it, kind judgment, its subject naming the machine and the reason; the
// next looks send nothing more.
func TestAnAutoHoldIsOneJudgmentMessageToTheCoordinator(t *testing.T) {
	t.Parallel()
	const name = "push-autohold"
	ta, rig, _ := coordSprint(t, name)
	rig.Hook(t, name, hook.Coordinator)
	const refused = "fatal: could not read Username for the origin: terminal prompts disabled"
	ta.lined(func() { ta.ok("add --stream s1 --count 2") })
	ta.lined(func() { ta.deal(2) })
	ta.lined(func() { ta.failOnce("m1", "s1-1.w1@1", refused) })
	require.NotEmpty(t, ta.group(sprint.NMemberEnvFault, "member:m1").ID, "the member is held with one judgment")

	var judged []bus.Message
	for range 3 {
		for _, m := range ta.pass(name) {
			if m.Kind == bus.KindJudgment {
				judged = append(judged, m)
			}
		}
		ta.step(coordPushLook)
	}
	require.Len(t, judged, 1, "one message for the episode: %+v", judged)
	assert.Equal(t, []string{name}, judged[0].To)
	assert.Contains(t, judged[0].Subject, "m1 "+sprint.NMemberEnvFault+": ")
	assert.Contains(t, judged[0].Subject, "(push)", "the subject names the reason")
	assert.Contains(t, judged[0].Body, "fleet up m1")
}

// A state episode has a durable receipt, and its frozen token payload bridges a
// lost bus response. Waiting, rewriting the cause and restarting do not resend it;
// closing it and raising the fault again makes a new episode.
func TestStateEpisodeSurvivesRetryRestartWaitAndRecurrence(t *testing.T) {
	t.Parallel()
	const name = "push-episode"
	ta, rig, _ := coordSprint(t, name)
	rig.Hook(t, name, hook.Coordinator)
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
	add := func() {
		_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, Type: sprint.NMemberEnvFault, Stream: sprint.MemberSubject("m1"), StreamLevel: true, What: "member m1 environment fault: credential unavailable", At: ta.a.now()}))
		require.NoError(t, err)
	}
	add()
	fake := bustest.NewFake(ta.a.now(), name)
	b := &bus.Bus{Store: fake}
	ta.a.bus = func(ctx context.Context, m bus.Message, _ func(string)) error { _, err := b.Send(ctx, m); return err }
	fake.Fail = errors.New("bus unavailable")
	ta.a.coordinatorPass(ctx)
	assert.Zero(t, fake.Len(bus.StreamOf(name)))
	fake.Fail = nil
	fake.Lose = errors.New("response lost after commit")
	ta.a.coordinatorPass(ctx)
	require.Equal(t, 1, fake.Len(bus.StreamOf(name)))
	// Drop every in-memory delivery cache as on a restart; also change the message
	// that would be generated now. The persisted pending payload must win.
	ta.a.coord = coordPush{}
	open, err := ta.m.OpenNotes(ctx)
	require.NoError(t, err)
	require.Len(t, open, 1)
	n := open[0].Note
	require.NotNil(t, n.CoordinatorDelivery)
	require.False(t, n.CoordinatorDelivery.Sent)
	n.What = "member m1 environment fault: a different cause in the same episode"
	_, err = st.Run(ctx, store.Step{Verb: "fixture update", Plan: func(*sprint.Snapshot) sprint.Plan { return sprint.Plan{Updates: []sprint.Note{n}} }})
	require.NoError(t, err)
	ta.a.coordinatorPass(ctx)
	require.Equal(t, 1, fake.Len(bus.StreamOf(name)), "ambiguous send retries the same token and frozen arguments")
	open, err = ta.m.OpenNotes(ctx)
	require.NoError(t, err)
	require.True(t, open[0].Note.CoordinatorDelivery.Sent)
	require.NoError(t, st.SetReview(ctx, n.ID, ta.a.now().Add(time.Minute)))
	ta.step(8 * 24 * time.Hour)
	fake.Advance(8 * 24 * time.Hour) // past the bus token cleanup: receipt still suppresses send
	ta.a.coord = coordPush{}
	ta.a.coordinatorPass(ctx)
	require.Equal(t, 1, fake.Len(bus.StreamOf(name)), "one per episode, including no repeated generic warning")
	_, err = st.Run(ctx, store.Step{Verb: "fixture clear", Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.Plan{Closes: s.Open} }})
	require.NoError(t, err)
	add()
	ta.a.coordinatorPass(ctx)
	assert.Equal(t, 2, fake.Len(bus.StreamOf(name)), "a cleared fault that recurs is a new episode")
}

func TestFailedWarningRetriesAtTheNextLook(t *testing.T) {
	t.Parallel()
	const name = "push-warning-retry"
	ta, rig, _ := coordSprint(t, name)
	rig.Hook(t, name, hook.Coordinator)
	timerJudgment(ta, name)
	ta.pass(name) // deliver the judgment before the warning is due
	ta.step(sprint.JudgmentWarnAfter)
	send := ta.a.bus
	ta.a.bus = func(context.Context, bus.Message, func(string)) error { return errors.New("bus unavailable") }
	ta.a.coordinatorPass(context.Background())
	ta.a.bus = send
	ta.step(coordPushLook)
	messages := ta.pass(name)
	require.Len(t, messages, 1)
	assert.Equal(t, "nova-sprint: judgments waiting on the coordinator", messages[0].Subject)
	assert.False(t, ta.a.coord.failed)
}

// A status judgment can keep its ID when a later transition changes its episode.
// A send prepared from an older inbox snapshot must not attach to the new one.
func TestStateDeliveryPreparationRejectsAnOldTransition(t *testing.T) {
	t.Parallel()
	const name = "push-transition"
	ta, _, _ := coordSprint(t, name)
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
	_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, Type: sprint.NStatus, Primaries: []string{"friend.worker"}, At: ta.a.now(), What: "friend worker is down (was up)"}))
	require.NoError(t, err)
	look, err := ta.a.readCoordLook(ctx)
	require.NoError(t, err)
	sends := coordFresh(map[string]bool{}, look, ta.a.now())
	require.Len(t, sends, 1)
	d := sends[0]
	n := look.open[0].Note
	n.At = n.At.Add(time.Minute)
	_, err = st.Run(ctx, store.Step{Verb: "fixture transition", Plan: func(*sprint.Snapshot) sprint.Plan { return sprint.Plan{Updates: []sprint.Note{n}} }})
	require.NoError(t, err)
	frozen, err := st.CoordinatorDeliveries(ctx, map[string]sprint.CoordinatorDelivery{d.note: {To: d.to, Token: d.token, Episode: d.episode, Subject: d.subject, Body: d.text}}, false, look.epoch)
	require.NoError(t, err)
	assert.Empty(t, frozen, "the old episode cannot freeze its message on the new transition")
	look, err = ta.a.readCoordLook(ctx)
	require.NoError(t, err)
	assert.Nil(t, look.open[0].Note.CoordinatorDelivery)
	newer := coordFresh(map[string]bool{}, look, ta.a.now())
	require.Len(t, newer, 1)
	assert.NotEqual(t, d.token, newer[0].token)
	look.scope += "another-sprint"
	other := coordFresh(map[string]bool{}, look, ta.a.now())
	require.Len(t, other, 1)
	assert.NotEqual(t, newer[0].token, other[0].token, "stores/namespaces sharing a bus cannot collide")
}

func TestFleetCauseAndItsStatusSendOneMessage(t *testing.T) {
	t.Parallel()
	for _, typ := range []string{sprint.NMemberEnvFault, sprint.NStopMemberDown} {
		t.Run(typ, func(t *testing.T) {
			t.Parallel()
			name := "push-cause-down"
			if typ == sprint.NMemberEnvFault {
				name = "push-cause-env"
			}
			ta, _, _ := coordSprint(t, name)
			ctx := context.Background()
			st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
			_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, Type: typ, Stream: sprint.MemberSubject("m1"), StreamLevel: true, What: "member m1 fault", At: ta.a.now()}))
			require.NoError(t, err)
			word := "down"
			if typ == sprint.NMemberEnvFault {
				word = "held"
			}
			_, err = st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, Type: sprint.NStatus, Primaries: []string{"m1"}, What: "member m1 is " + word + " (was up)", At: ta.a.now()}))
			require.NoError(t, err)
			sent := ta.pass(name)
			require.Len(t, sent, 1, "the causal episode owns the notification")
			assert.Equal(t, bus.KindJudgment, sent[0].Kind)
			ta.a.coord = coordPush{}
			ta.step(time.Minute)
			assert.Empty(t, ta.pass(name), "restart and status warnings add no second notification")
		})
	}
}

func TestResolvedFailedDeliveryUsesOneRecoveryProbe(t *testing.T) {
	t.Parallel()
	const name = "push-recovery"
	ta, rig, _ := coordSprint(t, name)
	hookBusOnly(t, rig, name)
	ta.pass(name)
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
	_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, Type: sprint.NMemberEnvFault, Stream: sprint.MemberSubject("m1"), StreamLevel: true, What: "member m1 fault", At: ta.a.now()}))
	require.NoError(t, err)
	send := ta.a.bus
	ta.a.bus = func(context.Context, bus.Message, func(string)) error { return errors.New("bus unavailable") }
	ta.step(hook.RenewEvery)
	ta.a.coordinatorPass(ctx)
	assert.True(t, ta.a.coord.failed)
	ta.refusedFirst("where", sprint.HookFirst+": "+name+" is not hooked in: live as coordinator (bus=proven sprint=armed)")
	_, err = st.Run(ctx, store.Step{Verb: "fixture resolve", Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.Plan{Closes: s.Open} }})
	require.NoError(t, err)
	ta.a.coordinatorPass(ctx) // recovery probe fails: same recovery token is retained
	token := ta.a.coord.recoveryToken
	require.NotEmpty(t, token)
	ta.a.bus = send
	ta.step(hook.RenewEvery)
	messages := ta.pass(name)
	require.Len(t, messages, 1)
	assert.Equal(t, "nova-sprint: coordinator delivery restored", messages[0].Subject)
	assert.Equal(t, token, messages[0].Token)
	assert.False(t, ta.a.coord.failed)
	ta.lined(func() { ta.ok("where") })
	assert.Empty(t, ta.pass(name), "no periodic healthy probe")
}

func TestStateEpisodeStoreOperationsAreBatched(t *testing.T) {
	t.Parallel()
	for _, count := range []int{1, 100} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			t.Parallel()
			name := "push-batch-" + strconv.Itoa(count)
			ta, _, _ := coordSprint(t, name)
			ctx := context.Background()
			st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
			var notes []sprint.Note
			for i := range count {
				notes = append(notes, sprint.Note{Kind: sprint.Judgment, Type: sprint.NMemberEnvFault, Stream: sprint.MemberSubject("m" + strconv.Itoa(i)), StreamLevel: true, What: "environment fault", At: ta.a.now()})
			}
			_, err := st.Run(ctx, store.Step{Verb: "fixture batch", Plan: func(*sprint.Snapshot) sprint.Plan { return sprint.Plan{Notes: notes} }})
			require.NoError(t, err)
			before := ta.m.Calls["acquire"]
			messages := ta.pass(name)
			require.Len(t, messages, count)
			assert.Equal(t, 2, ta.m.Calls["acquire"]-before, "one prepare and one receipt commit, independent of count")
		})
	}
}

func TestAmbiguousStateDeliveryStopsBeforeTokenExpiry(t *testing.T) {
	t.Parallel()
	const name = "push-expired"
	ta, _, _ := coordSprint(t, name)
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
	_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, Type: sprint.NMemberEnvFault, Stream: sprint.MemberSubject("m1"), StreamLevel: true, What: "member m1 fault", At: ta.a.now()}))
	require.NoError(t, err)
	fake := bustest.NewFake(ta.a.now(), name)
	b := &bus.Bus{Store: fake}
	calls := 0
	ta.a.bus = func(ctx context.Context, m bus.Message, _ func(string)) error {
		calls++
		_, err := b.Send(ctx, m)
		return err
	}
	fake.Lose = errors.New("response lost after commit")
	ta.a.coordinatorPass(ctx)
	require.Equal(t, 1, fake.Len(bus.StreamOf(name)))
	for _, elapsed := range []time.Duration{bus.DefaultTokenLife + time.Hour, 8 * 24 * time.Hour} {
		ta.step(elapsed)
		fake.Advance(elapsed)
		ta.a.coord = coordPush{}
		ta.a.coordinatorPass(ctx)
		require.Equal(t, 1, calls, "an expired pending receipt cannot send automatically")
		require.Equal(t, 1, fake.Len(bus.StreamOf(name)))
		require.True(t, ta.a.coord.failed, "unknown receipt keeps the transport gate unproven")
	}
}

func TestStatusKeepsItsAlertWhenTheCauseCloses(t *testing.T) {
	t.Parallel()
	for _, afterFailedSend := range []bool{false, true} {
		t.Run(strconv.FormatBool(afterFailedSend), func(t *testing.T) {
			t.Parallel()
			name := "push-close-" + strconv.FormatBool(afterFailedSend)
			ta, _, _ := coordSprint(t, name)
			ctx := context.Background()
			st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
			for _, n := range []sprint.Note{
				{Kind: sprint.Judgment, Type: sprint.NMemberEnvFault, Stream: sprint.MemberSubject("m1"), StreamLevel: true, What: "member m1 environment fault", At: ta.a.now()},
				{Kind: sprint.Judgment, Type: sprint.NStatus, Primaries: []string{"m1"}, What: "member m1 is held (was up)", At: ta.a.now()},
			} {
				_, err := st.Run(ctx, store.NoteStep("fixture", n))
				require.NoError(t, err)
			}
			look, err := ta.a.readCoordLook(ctx)
			require.NoError(t, err)
			candidates := map[string]sprint.CoordinatorDelivery{}
			for _, d := range coordFresh(map[string]bool{}, look, ta.a.now()) {
				candidates[d.note] = sprint.CoordinatorDelivery{From: name, To: d.to, Token: d.token, Subject: d.subject, Body: d.text, Episode: d.episode, CoveredBy: d.coveredBy, RetryBefore: ta.a.now().Add(bus.DefaultTokenLife)}
			}
			if afterFailedSend {
				send := ta.a.bus
				ta.a.bus = func(context.Context, bus.Message, func(string)) error { return errors.New("bus unavailable") }
				ta.a.coordinatorPass(ctx)
				ta.a.bus = send
			}
			_, err = st.Run(ctx, store.Step{Verb: "fixture close cause", Plan: func(s *sprint.Snapshot) sprint.Plan {
				var p sprint.Plan
				for _, o := range s.Open {
					if o.Note.Type == sprint.NMemberEnvFault {
						p.Closes = append(p.Closes, o)
					}
				}
				return p
			}})
			require.NoError(t, err)
			if !afterFailedSend {
				// The inbox observed the cause, but the fenced preparation no longer does.
				ds, err := st.CoordinatorDeliveries(ctx, candidates, false, look.epoch)
				require.NoError(t, err)
				require.Len(t, ds, 1)
				for _, d := range ds {
					assert.Empty(t, d.CoveredBy)
				}
			}
			messages := ta.pass(name)
			require.Len(t, messages, 1)
			assert.Equal(t, bus.KindJudgment, messages[0].Kind, "a recovery status is not the missing alert")
			if afterFailedSend {
				assert.Contains(t, messages[0].Body, "environment fault")
			} else {
				assert.Contains(t, messages[0].Body, "is held")
			}
			ta.a.coord = coordPush{}
			assert.Empty(t, ta.pass(name))
		})
	}
}

func TestAmbiguousRecoveryStopsBeforeTokenExpiry(t *testing.T) {
	t.Parallel()
	const name = "push-recovery-expired"
	ta, _, _ := coordSprint(t, name)
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
	_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, Type: sprint.NMemberEnvFault, Stream: sprint.MemberSubject("m1"), StreamLevel: true, What: "member m1 fault", At: ta.a.now()}))
	require.NoError(t, err)
	ta.a.bus = func(context.Context, bus.Message, func(string)) error { return errors.New("bus unavailable") }
	ta.a.coordinatorPass(ctx)
	_, err = st.Run(ctx, store.Step{Verb: "fixture resolve", Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.Plan{Closes: s.Open} }})
	require.NoError(t, err)
	fake := bustest.NewFake(ta.a.now(), name)
	fake.Lose = errors.New("response lost after commit")
	b := &bus.Bus{Store: fake}
	calls := 0
	ta.a.bus = func(ctx context.Context, m bus.Message, _ func(string)) error {
		calls++
		_, err := b.Send(ctx, m)
		return err
	}
	ta.a.coordinatorPass(ctx)
	require.Equal(t, 1, fake.Len(bus.StreamOf(name)))
	for _, elapsed := range []time.Duration{bus.DefaultTokenLife + time.Hour, 8 * 24 * time.Hour} {
		ta.step(elapsed)
		fake.Advance(elapsed)
		ta.a.coordinatorPass(ctx)
		require.Equal(t, 1, calls)
		require.Equal(t, 1, fake.Len(bus.StreamOf(name)))
		require.True(t, ta.a.coord.failed)
	}
}
