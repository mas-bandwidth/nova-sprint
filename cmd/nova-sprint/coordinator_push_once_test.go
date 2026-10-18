package main

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
)

// A pass over a sprint that has not changed since the last full pass began is skipped: one
// short look on the line (the epoch, the seat, the inbox's end, the open notes) and nothing
// else. Anything the pass depends on changing, a failed send, or coordPushLook since the last
// full pass brings the full pass back. Before (2026-10-17), every log line ran a full pass.
func TestAPassOverAnUnchangedSprintIsSkipped(t *testing.T) {
	t.Parallel()
	const name = "push-skip"
	ta, rig, _ := coordSprint(t, name)
	rig.Hook(t, name, hook.Coordinator)
	ctx := context.Background()
	full := func() bool {
		if ta.a.coordPassSkip(ctx) {
			return false
		}
		require.True(t, ta.a.coordinatorPass(ctx))
		return true
	}
	require.True(t, full(), "the first pass is full")
	ta.step(coordPushFloor)
	before := maps.Clone(ta.m.Calls)
	require.True(t, ta.a.coordPassSkip(ctx), "nothing changed: skipped")
	assert.Equal(t, 1, ta.m.Calls["open"]-before["open"], "the skip reads the open notes once, and runs no step")
	assert.Equal(t, before["acquire"], ta.m.Calls["acquire"])

	ta.step(coordPushLook - coordPushFloor - time.Second)
	require.False(t, full(), "still within coordPushLook of the last full pass")
	ta.step(time.Second)
	require.True(t, full(), "coordPushLook since the last full pass: the backstop runs it")
	ta.step(coordPushFloor)
	require.False(t, full())

	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
	_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: "a-new-judgment", What: "new", At: ta.a.now()}))
	require.NoError(t, err)
	ta.step(coordPushFloor)
	require.True(t, full(), "a new note: full")

	ta.a.coord.mu.Lock()
	ta.a.coord.failed = true
	ta.a.coord.mu.Unlock()
	ta.step(coordPushFloor)
	assert.False(t, ta.a.coordPassSkip(ctx), "a failed send to the holder is never skipped")
}

// Passes start at least coordPushFloor apart, however busy the log; a pass a while after the
// last starts at once.
func TestPassesStartAtLeastASecondApart(t *testing.T) {
	t.Parallel()
	const name = "push-floor"
	ta, rig, _ := coordSprint(t, name)
	rig.Hook(t, name, hook.Coordinator)
	ctx := context.Background()
	start := ta.a.now()
	began := ta.a.coordPushOnce(ctx, start)
	assert.Equal(t, start.Add(coordPushFloor), began, "a pass right after the last waits for the floor")
	later := ta.a.now()
	began = ta.a.coordPushOnce(ctx, later.Add(-10*time.Second))
	assert.Equal(t, later, began, "a pass long after the last starts at once")
}

// An overdue judgment is the deadline lane's; its keys stay told, so when its group comes
// back from overdue (its oldest note closed, a later note's deadline now the group's) the notes
// already told are not told again. Before (2026-10-17), the keys were forgotten while the group
// was overdue, and the whole group was told again each time it came back: one group 36 times.
func TestAnOverdueGroupIsNotToldAgainWhenItComesBack(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0).UTC()
	g := sprint.Group{Kind: sprint.Judgment, ID: "g1", Type: "judgment-x", What: "x", Notes: []string{"n1", "n2"}, Due: now.Add(time.Minute)}
	l := coordLook{holder: "seat", groups: []sprint.Group{g}}
	told := map[string]bool{}
	sends := coordFresh(told, l, now)
	require.Len(t, sends, 1)
	for _, k := range sends[0].keys {
		told[k] = true
	}

	l.groups[0].Due = now.Add(-time.Second) // overdue
	require.Empty(t, coordFresh(told, l, now))
	assert.Len(t, told, 2, "an overdue group's keys stay told")

	l.groups[0].Due = now.Add(time.Hour) // back from overdue, the same notes
	assert.Empty(t, coordFresh(told, l, now), "not told again")

	l.groups[0].Notes = []string{"n1", "n2", "n3"}
	sends = coordFresh(told, l, now)
	require.Len(t, sends, 1, "a new note in it is told")
	assert.Equal(t, []string{noteKey("n3")}, sends[0].keys)
}

// overdueRig is a sprint with a holder and one judgment past its deadline, the owner glenn.
func overdueRig(t *testing.T, name string) (*testApp, *bytes.Buffer) {
	t.Helper()
	ta, _, _ := coordSprint(t, name)
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
	_, _, _, err := st.SetMachine(ctx, true)
	require.NoError(t, err)
	_, err = st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: "owed-decision", What: "owed", At: ta.a.now()}))
	require.NoError(t, err)
	ta.step(sprint.DeadlineJudgment)
	var log bytes.Buffer
	ta.a.serveLog = &log
	return ta, &log
}

// A decision delivery refused by the bus ("glenn is no known name", 2026-10-17) is sent again
// after a minute, then two, and so on to thirty, and said once until its text changes. Both
// lanes go to the coordinator seat's holder (owner-pushes-route-to-coordinator), and the owner
// lane's same set is withheld while the coordinator notice's receipt is unknown
// (v17-decisions-owed-folds-into-one-notice), so the refused send is the coordinator lane's.
// Before, the refused send and its line went every pass: two thirds of the server's log.
func TestARefusedDecisionDeliveryBacksOffAndIsSaidOnce(t *testing.T) {
	t.Parallel()
	const name = "owed-seat"
	ta, log := overdueRig(t, name)
	ctx := context.Background()
	refused := 0
	b := &tokenBus{}
	ta.a.bus = func(ctx context.Context, m bus.Message, say func(string)) error {
		refused++
		return &bus.Refusal{Problems: []string{"glenn is no known name"}}
	}
	ta.a.decisionPass(ctx)
	require.Equal(t, 1, refused, "one send: the owner lane waits on the coordinator's receipt: %s", log.String())
	for range 11 {
		ta.step(5 * time.Second)
		ta.a.decisionPass(ctx)
	}
	assert.Equal(t, 1, refused, "no send within the minute")
	ta.step(busRetryFirst - 55*time.Second)
	ta.a.decisionPass(ctx)
	assert.Equal(t, 2, refused, "sent again at a minute")
	ta.step(2*busRetryFirst - time.Second)
	ta.a.decisionPass(ctx)
	assert.Equal(t, 2, refused, "then two minutes")
	ta.step(time.Second)
	ta.a.decisionPass(ctx)
	assert.Equal(t, 3, refused)
	assert.Equal(t, 1, strings.Count(log.String(), "DECISIONS OWED: coordinator delivery pending"), "said once: %s", log.String())
	assert.Contains(t, log.String(), "refused: tried again in 1m0s")

	// a bus that takes it again delivers it at its next due look, the owner lane folded into
	// it, and the wait is gone
	ta.a.bus = b.send
	ta.step(busRetryCap)
	ta.a.decisionPass(ctx)
	require.Len(t, b.delivered, 1, "one notice: %s", log.String())
	assert.Equal(t, []string{name}, b.delivered[0].To)
	assert.Empty(t, ta.a.coord.decisions)
}

// A send that went under its token before is a delivery, as the coordinator push counts it
// (busMessageWent): the receipt is recorded and the lane sends it no more.
func TestADecisionDeliveryThatWentBeforeIsDelivered(t *testing.T) {
	t.Parallel()
	const name = "went-seat"
	ta, log := overdueRig(t, name)
	ctx := context.Background()
	sends := 0
	ta.a.bus = func(context.Context, bus.Message, func(string)) error {
		sends++
		return &bus.Refusal{Problems: []string{"the token \"decisions:x\" sent 01M before: the message went, and is not sent again"}}
	}
	ta.a.decisionPass(ctx)
	require.Equal(t, 1, sends, "the coordinator's notice; the owner lane's same set folds into it")
	ta.step(5 * time.Second)
	ta.a.decisionPass(ctx)
	assert.Equal(t, 1, sends, "delivered: not sent again")
	assert.NotContains(t, log.String(), "delivery pending")
}

// With no holder of the seat the coordinator's lane still makes its delivery (it is made,
// transferred and closed as before), and its "no coordinator channel" fault waits as a
// refusal does and is said once. The owner lane goes to the seat's holder too
// (owner-pushes-route-to-coordinator), so with no holder nothing is sent. A pass over that
// sprint, unchanged, is skipped: no renewal is owed with no holder.
func TestNoHolderIsAWaitingFaultForBothLanes(t *testing.T) {
	t.Parallel()
	const name = "no-holder"
	ta, log := overdueRig(t, name)
	ctx := context.Background()
	require.NoError(t, ta.m.SetCoordinator(ctx, ""))
	b := &tokenBus{}
	ta.a.bus = b.send
	require.False(t, ta.a.coordPassSkip(ctx))
	require.True(t, ta.a.coordinatorPass(ctx))
	assert.Empty(t, b.delivered, "no holder: no recipient for either lane")
	for range 10 {
		ta.step(5 * time.Second)
		if !ta.a.coordPassSkip(ctx) {
			ta.a.coordinatorPass(ctx)
		}
	}
	assert.Empty(t, b.delivered)
	assert.Equal(t, 1, strings.Count(log.String(), "no coordinator channel is configured"), "said once: %s", log.String())
	ta.step(2 * time.Second)
	assert.True(t, ta.a.coordPassSkip(ctx), "nothing changed and no renewal is owed: skipped")
	ta.step(coordPushLook)
	assert.False(t, ta.a.coordPassSkip(ctx), "the backstop still runs")
}

// A sprint that names no owner is no fault of the owner lane's: it goes to the coordinator
// seat's holder (owner-pushes-route-to-coordinator), so "no owner channel" is never said, and
// nothing waits.
func TestNoOwnerNamedIsNoOwnerChannelFault(t *testing.T) {
	t.Parallel()
	const name = "no-owner"
	ta, log := overdueRig(t, name)
	ctx := context.Background()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
	require.NoError(t, st.SetOwner(ctx, "")) // the sprint names no owner
	b := &tokenBus{}
	ta.a.bus = b.send
	require.False(t, ta.a.coordPassSkip(ctx))
	ta.a.coordinatorPass(ctx)
	for range 10 {
		ta.step(5 * time.Second)
		if !ta.a.coordPassSkip(ctx) {
			ta.a.coordinatorPass(ctx)
		}
	}
	assert.NotContains(t, log.String(), "no owner channel is configured")
	require.NotEmpty(t, b.delivered, "told: %s", log.String())
	for _, m := range b.delivered {
		assert.Equal(t, []string{name}, m.To)
	}
	ta.a.coord.mu.Lock()
	_, waiting := ta.a.coord.decisions["owner"]
	ta.a.coord.mu.Unlock()
	assert.False(t, waiting, "the owner lane waits on nothing")
}

// The sprint push's renewal is never more than a floor late: a log event a second before it
// is due is skipped, and the loop's wait then ends when it is due (coordPushWait), not a whole
// coordPushLook later; the pass renews it first, off the line, so a tick holding the line
// cannot hold it back. Before, an event at heldAt+29 s left the lease near 60 s unrenewed,
// plus however long the line was held, of a 90 s life.
func TestTheSprintPushIsRenewedOnTimeAfterASkippedPass(t *testing.T) {
	t.Parallel()
	const name = "push-renew"
	ta, rig, _ := coordSprint(t, name)
	rig.Hook(t, name, hook.Coordinator)
	ctx := context.Background()
	var waits []time.Duration
	ta.m.LogWait = func(d time.Duration) { waits = append(waits, d); ta.a.sleep(d) }
	t0 := ta.a.now()
	require.False(t, ta.a.coordPassSkip(ctx))
	require.True(t, ta.a.coordinatorPass(ctx))
	require.Equal(t, t0, ta.a.coord.heldAt)

	ta.step(hook.RenewEvery - time.Second) // an event a second before the renewal is due
	began := ta.a.coordPushOnce(ctx, t0)
	require.Len(t, waits, 1)
	assert.Equal(t, time.Second, waits[0], "the wait ends when the renewal is due")
	assert.Equal(t, t0, ta.a.coord.heldAt, "the skipped pass renewed nothing")
	ta.a.coordPushOnce(ctx, began)
	held := ta.a.coord.heldAt
	assert.False(t, held.Before(t0.Add(hook.RenewEvery)))
	assert.False(t, held.After(t0.Add(hook.RenewEvery+coordPushFloor)), "renewed within a floor of its due time: %s", held.Sub(t0))

	// the early renewal takes no line: it renews while a tick holds it
	ta.step(hook.RenewEvery)
	ta.a.serial.Lock()
	ta.a.renewEarly(ctx)
	ta.a.serial.Unlock()
	assert.Equal(t, ta.a.now(), ta.a.coord.heldAt, "renewed with the line held elsewhere")
}

// failingOwner is the memory store whose owner cannot be read.
type failingOwner struct{ *store.Mem }

func (f failingOwner) GetKey(ctx context.Context, name string) (string, bool, error) {
	if name == "owner" {
		return "", false, errors.New("the owner could not be read")
	}
	return f.Mem.GetKey(ctx, name)
}

// The owner lane no longer reads the sprint's owner (owner-pushes-route-to-coordinator): an
// owner that cannot be read holds back neither lane. (A seat that cannot be read fails both,
// then as now: every step reads the seat.)
func TestAnUnreadableOwnerHoldsBackNoLane(t *testing.T) {
	t.Parallel()
	const name = "lane-apart"
	ta, log := overdueRig(t, name)
	ctx := context.Background()
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) { return failingOwner{ta.m}, nil }
	b := &tokenBus{}
	ta.a.bus = b.send
	ta.a.decisionPass(ctx)
	require.Len(t, b.delivered, 1, "the coordinator is told, the owner lane folded in: %s", log.String())
	assert.Equal(t, []string{name}, b.delivered[0].To)
	assert.NotContains(t, log.String(), "DECISIONS OWED: prepare owner failed")
}

// failingInbox is the memory store whose inbox stream cannot be read.
type failingInbox struct{ *store.Mem }

func (failingInbox) NotesSince(context.Context, string, int) ([]sprint.Note, []string, error) {
	return nil, nil, errors.New("the inbox could not be read")
}

// failingWait is the memory store whose log wait fails.
type failingWait struct{ *store.Mem }

func (failingWait) WaitLog(context.Context, string, time.Duration) (string, bool, error) {
	return "", false, errors.New("the log wait failed")
}

// hookedPush is a hooked coordinator whose first full pass held its sprint push at t0, and
// whose loop waits on the test's clock.
func hookedPush(t *testing.T, name string) (*testApp, time.Time) {
	t.Helper()
	ta, rig, _ := coordSprint(t, name)
	rig.Hook(t, name, hook.Coordinator)
	ta.m.LogWait = func(d time.Duration) { ta.a.sleep(d) }
	t0 := ta.a.now()
	ctx := context.Background()
	require.False(t, ta.a.coordPassSkip(ctx))
	require.True(t, ta.a.coordinatorPass(ctx))
	require.Equal(t, t0, ta.a.coord.heldAt)
	require.True(t, ta.a.coord.heldOK)
	return ta, t0
}

// A pass that cannot read its look delivers nothing, so it renews nothing, early or late:
// the lease lapses within hook.LeaseTTL and the gate refuses, as before the early renewal.
// The read is tried again every coordPushRetry, not every second.
func TestAPassThatCannotReadRenewsNothing(t *testing.T) {
	t.Parallel()
	ta, t0 := hookedPush(t, "push-noread")
	ctx := context.Background()
	notes := 0
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) {
		return countingInbox{failingInbox{ta.m}, &notes}, nil
	}
	ta.step(hook.RenewEvery - time.Second)
	var last time.Time
	for ta.a.now().Before(t0.Add(hook.LeaseTTL + coordPushLook)) {
		last = ta.a.coordPushOnce(ctx, last)
	}
	// the one renewal the last good read earned (at t0+RenewEvery, as the first failing pass
	// began) and none after: the lease ends within hook.LeaseTTL of the first failed read
	held := ta.a.coord.heldAt
	assert.False(t, held.After(t0.Add(hook.RenewEvery)), "nothing renewed after the look failed: %s", held.Sub(t0))
	assert.False(t, ta.a.coord.readOK)
	assert.LessOrEqual(t, notes, int((hook.LeaseTTL+coordPushLook)/coordPushRetry)+2, "read again every coordPushRetry, not every second: %d reads", notes)
}

// countingInbox counts the reads of a failing inbox.
type countingInbox struct {
	failingInbox
	n *int
}

func (c countingInbox) NotesSince(ctx context.Context, after string, max int) ([]sprint.Note, []string, error) {
	*c.n++
	return c.failingInbox.NotesSince(ctx, after, max)
}

// A turn of the loop renews the push on time while another goroutine holds the server's line
// for 20 s: the renewal is the turn's first act, off the line.
func TestTheLoopRenewsWhileTheLineIsHeld(t *testing.T) {
	t.Parallel()
	ta, t0 := hookedPush(t, "push-lineheld")
	ctx := context.Background()
	ta.step(hook.RenewEvery)
	hs := ta.a.hookStoreFn
	renewed := make(chan struct{}, 1)
	ta.a.hookStoreFn = func(ctx context.Context) (hook.Store, error) {
		select {
		case renewed <- struct{}{}:
		default:
		}
		return hs(ctx)
	}
	ta.a.serial.Lock()
	done := make(chan struct{})
	go func() { defer close(done); ta.a.coordPushOnce(ctx, t0) }()
	<-renewed // the renewal reached the hook store while the line is held
	ta.step(20 * time.Second)
	ta.a.serial.Unlock()
	<-done
	assert.Equal(t, t0.Add(hook.RenewEvery), ta.a.coord.heldAt, "renewed when due, not when the line came free")
}

// A failed log wait a tenth of a second before the renewal is due sleeps only to the renewal:
// the push is renewed by a second after it was due. Before, it slept a whole look.
func TestAFailedWaitBeforeTheRenewalStillRenewsOnTime(t *testing.T) {
	t.Parallel()
	ta, t0 := hookedPush(t, "push-waitfail")
	ctx := context.Background()
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) { return failingWait{ta.m}, nil }
	ta.step(hook.RenewEvery - 100*time.Millisecond)
	last := ta.a.coordPushOnce(ctx, t0)
	ta.a.coordPushOnce(ctx, last)
	held := ta.a.coord.heldAt
	assert.True(t, held.After(t0), "renewed")
	assert.False(t, held.After(t0.Add(hook.RenewEvery+coordPushFloor)), "by a second after it was due: %s", held.Sub(t0))
}

// A renewal that fails is tried again in coordPushRetry, doubling to holdRetryCap, and said
// once: not every pass, a second apart, with two tries and two lines each.
func TestAFailedRenewalBacksOffAndIsSaidOnce(t *testing.T) {
	t.Parallel()
	ta, t0 := hookedPush(t, "push-holdfail")
	ctx := context.Background()
	var log bytes.Buffer
	ta.a.serveLog = &log
	tries := 0
	ta.a.hookStoreFn = func(context.Context) (hook.Store, error) {
		tries++
		return hook.Store{}, errors.New("the hook store is down")
	}
	ta.step(hook.RenewEvery)
	var last time.Time
	var at []time.Duration
	for ta.a.now().Before(t0.Add(hook.RenewEvery + 2*time.Minute)) {
		before := tries
		began := ta.a.coordPushOnce(ctx, last)
		if tries > before {
			at = append(at, began.Sub(t0)-hook.RenewEvery)
		}
		last = began
	}
	require.GreaterOrEqual(t, len(at), 4, "%v", at)
	for i := 1; i < len(at); i++ {
		gap := at[i] - at[i-1]
		assert.GreaterOrEqual(t, gap, min(coordPushRetry<<(i-1), holdRetryCap), "try %d after %s: %v", i, gap, at)
	}
	assert.LessOrEqual(t, len(at), 8, "tries in two minutes: %v", at)
	assert.Equal(t, 1, strings.Count(log.String(), "could not be held"), log.String())
}

// A bus blip of two seconds unproves the push at once (the header's promise) and re-proves
// it within coordPushRetry: the failed send is retried within it, not a look later.
func TestABusBlipIsReprovedWithinTheRetry(t *testing.T) {
	t.Parallel()
	const name = "push-blip"
	ta, _ := hookedPush(t, name)
	ctx := context.Background()
	send := ta.a.bus
	t1 := ta.a.now()
	ta.a.bus = func(context.Context, bus.Message, func(string)) error { return errors.New("bus unavailable") }
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
	_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, SprintLevel: true, Type: "blip-judgment", What: "new", At: ta.a.now()}))
	require.NoError(t, err)
	last := ta.a.coordPushOnce(ctx, time.Time{})
	require.True(t, ta.a.coord.failed)
	assert.False(t, ta.a.coord.heldOK, "unproved at once")
	ta.a.bus = send // the blip clears within two seconds; the loop waited coordPushRetry
	for !ta.a.coord.heldOK && ta.a.now().Before(t1.Add(coordPushLook)) {
		last = ta.a.coordPushOnce(ctx, last)
	}
	assert.True(t, ta.a.coord.heldOK, "proven again")
	assert.False(t, ta.a.coord.heldAt.After(t1.Add(coordPushRetry+coordPushFloor)), "within the retry: %s", ta.a.coord.heldAt.Sub(t1))
}

// A decisions-owed send whose token went with other arguments is sent at once under a token
// of its own, and its receipt is recorded: not refused under the same token every pass.
func TestADecisionDeliveryWhoseTokenWentWithOtherArgumentsGetsANewToken(t *testing.T) {
	t.Parallel()
	const name = "owed-retoken"
	ta, _ := overdueRig(t, name)
	ctx := context.Background()
	b := &tokenBus{}
	var tokens []string
	ta.a.bus = func(ctx context.Context, m bus.Message, say func(string)) error {
		tokens = append(tokens, m.Token)
		if len(tokens) == 1 {
			return &bus.Refusal{Problems: []string{"the token \"" + m.Token + "\" already sent 01M at 2026-10-17T00:00:00Z with other arguments; a retry repeats them exactly, and a new message wants a new token"}}
		}
		return b.send(ctx, m, say)
	}
	ta.a.decisionPass(ctx)
	require.Len(t, tokens, 2, "sent again at once; the owner lane's same set folds into it")
	assert.True(t, strings.HasPrefix(tokens[1], tokens[0]+":"), tokens[1])
	ta.step(5 * time.Second)
	ta.a.decisionPass(ctx)
	assert.Len(t, tokens, 2, "delivered: not sent again")
	assert.Empty(t, ta.a.coord.decisions)
}

// A server whose environment names no bus has a standing fault: it waits as a refusal does.
func TestAnUnsetBusIsAStandingFault(t *testing.T) {
	t.Parallel()
	r := busRetry{}
	r.failed(time.Unix(1000, 0), errors.New(busRedisEnv+" is not set: no bus to send on"))
	assert.Equal(t, busRetryFirst, r.wait)
}
