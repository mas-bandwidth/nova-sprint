package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheStoreStopsAndStartRefusesUntilAWorkedTierIsLive(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, providerRoute("flash-live", "flash", "flash"), providerRoute("pro-credit", "pro", "credit"), providerRoute("pro-auth", "pro", "key"), providerRoute("heavy-seat", "heavy", "seat"))
	h.addReady("pro-stream", 2, "RESULT: pro-work sha=000000000000 tier: pro\n\nThe task.")
	h.addReady("heavy-stream", 1, "RESULT: heavy-work sha=000000000000 tier: heavy\n\nThe task.")
	h.startMachine()
	at := h.now.UTC().Format(time.RFC3339)
	until := h.now.Add(3 * time.Hour).UTC().Format(time.RFC3339)
	h.must(Step{Verb: "fixture rests", Load: All, Plan: func(s *sprint.Snapshot) sprint.Plan {
		var p sprint.Plan
		for provider, value := range map[string]string{
			"credit": at + " " + until + " old.w1 " + sprint.RestCredit + " out of credit",
			"key":    at + " open - " + sprint.RestAuth + " auth refused",
			"seat":   at + " " + h.now.Add(4*time.Hour).UTC().Format(time.RFC3339) + " - " + sprint.RestCoordinator + " rested by the seat",
		} {
			name := sprint.PropProviderRest(provider)
			was, ok := s.Fleet.Prop(name)
			p.Props = append(p.Props, sprint.PropWrite{Table: sprint.Fleet, Name: name, Value: value, Was: was, WasAbsent: !ok})
		}
		return p
	}})
	res := h.machine()
	m := h.machineRecord()
	require.False(t, m.Running(), "ready pro/heavy are blocked while the only live flash route has no work")
	assert.Equal(t, "nothing funded can work", m.Cause)
	assert.Contains(t, m.Reason, "pro:")
	assert.Contains(t, m.Reason, "heavy:")
	assert.Equal(t, "machine: STOPPED (nothing funded can work)", MachineLine(h.now, m, Heartbeat{}))
	assert.Contains(t, res.Halted, "pro-credit")
	require.Len(t, h.openOf("nothing funded can work"), 1)
	_, _, _, err := h.st.SetMachine(h.ctx, true)
	require.Error(t, err, "owner's start refuses until a worked tier has a live route")
	assert.Contains(t, err.Error(), "pro:")
	assert.Contains(t, err.Error(), "heavy:")
	for range 4 {
		h.tick(30 * time.Minute)
		h.machine()
	}
	assert.False(t, h.machineRecord().Running())
	for _, typ := range []string{sprint.NWorkLate, sprint.NStreamStale, sprint.NOverdue} {
		assert.Empty(t, h.tickNotes(typ), "no stopped-time alarm: %s", typ)
	}
	assert.Empty(t, h.tickNotes("a funded route is live again"))
	h.tick(time.Hour)
	h.machine()
	assert.False(t, h.machineRecord().Running(), "a rest ending does not restart the sprint")
	require.Len(t, h.tickNotes("a funded route is live again"), 1)
	h.machine()
	require.Len(t, h.tickNotes("a funded route is live again"), 1, "one recovery note per stop")
	h.startMachine()
	assert.True(t, h.machineRecord().Running())
}

// A new hand STOP or START generation between the recovery look and its
// Acquire must discard the old episode's notification, before any write.
type recoveryInterleave struct {
	*Mem
	before      func()
	ran         bool
	failRelease bool
}

func (b *recoveryInterleave) Acquire(ctx context.Context, gen uint64, op OpRecord) (bool, error) {
	if op.Verb == "tick funded live again" && !b.ran {
		b.ran = true
		b.before()
	}
	return b.Mem.Acquire(ctx, gen, op)
}

func (b *recoveryInterleave) Release(ctx context.Context, op OpRecord, commit bool) error {
	if b.failRelease && op.Verb == "tick funded live again" && !commit {
		return errors.New("release receipt lost")
	}
	return b.Mem.Release(ctx, op, commit)
}

func TestRecoveryCannotNotifyForAnOlderStopEpisode(t *testing.T) {
	t.Parallel()
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprint(restart), func(t *testing.T) {
			t.Parallel()
			h := routeHarness(t, route("pro-live", "pro"))
			h.addReady("s1", 1, "RESULT: pro-work sha=000000000000 tier: pro\n\nThe task.")
			h.startMachine()
			old := h.machineRecord()
			h.must(RouteRestStep(sprint.RouteRestReq{Target: "pro-live", Reason: "blocked episode", Until: h.now.Add(time.Hour)}))
			_, _, _, err := h.st.stopWithCause(h.ctx, "nothing funded can work", old.RunSeq)
			require.NoError(t, err)
			old = h.machineRecord()
			h.must(RouteWakeStep(sprint.RouteWakeReq{Target: "pro-live", Reason: "live again"}))
			b := &recoveryInterleave{Mem: h.m}
			b.before = func() {
				if restart {
					h.startMachine()
					m := h.machineRecord()
					h.must(RouteRestStep(sprint.RouteRestReq{Target: "pro-live", Reason: "new blocked episode", Until: h.now.Add(time.Hour)}))
					_, _, _, err := h.st.stopWithCause(h.ctx, "nothing funded can work", m.RunSeq)
					require.NoError(t, err)
				} else {
					_, _, _, err := h.st.StopUntil(h.ctx, "new owner's stop", time.Time{})
					require.NoError(t, err)
				}
			}
			h.st.B = b
			var res TickResult
			require.NoError(t, h.st.fundedLiveAgain(h.ctx, old, &res))
			assert.Empty(t, h.tickNotes("a funded route is live again"))
			assert.Empty(t, res.Parts)
		})
	}
}

func TestARecoveryGuardReleaseFailureWritesNoNote(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("pro-live", "pro"))
	h.addReady("s1", 1, "RESULT: pro-work sha=000000000000 tier: pro\n\nThe task.")
	h.startMachine()
	m := h.machineRecord()
	h.must(RouteRestStep(sprint.RouteRestReq{Target: "pro-live", Reason: "blocked episode", Until: h.now.Add(time.Hour)}))
	_, _, _, err := h.st.stopWithCause(h.ctx, "nothing funded can work", m.RunSeq)
	require.NoError(t, err)
	m = h.machineRecord()
	h.must(RouteWakeStep(sprint.RouteWakeReq{Target: "pro-live", Reason: "live again"}))
	b := &recoveryInterleave{Mem: h.m, failRelease: true}
	b.before = func() {
		_, _, _, err := h.st.StopUntil(h.ctx, "new owner's stop", time.Time{})
		require.NoError(t, err)
	}
	h.st.B = b
	var res TickResult
	err = h.st.fundedLiveAgain(h.ctx, m, &res)
	require.ErrorContains(t, err, "release receipt lost")
	assert.Empty(t, h.tickNotes("a funded route is live again"))
	assert.Empty(t, res.Parts)
	f, err := h.m.ReadFence(h.ctx)
	require.NoError(t, err)
	require.NotNil(t, f.Pending)
	assert.True(t, f.Pending.Lock, "only an empty guard may survive failed release")
	assert.Empty(t, f.Pending.Notes)
	assert.Empty(t, f.Pending.Manifests)
	b.failRelease = false
	h.mu.Lock()
	h.now = h.now.Add(h.st.grace() + time.Second)
	h.mu.Unlock()
	rr, err := h.st.Repair(h.ctx)
	require.NoError(t, err)
	require.Len(t, rr, 1)
	assert.Equal(t, RepairAbandoned, rr[0].Done)
	assert.Empty(t, h.tickNotes("a funded route is live again"), "repair cannot replay a stale episode's notification")
}

func TestAFirstStartRefusesBlockedWorkUnderItsTransitionFence(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("pro-resting", "pro"))
	h.addReady("s1", 1, "RESULT: pro-work sha=000000000000 tier: pro\n\nThe task.")
	h.must(RouteRestStep(sprint.RouteRestReq{Target: "pro-resting", Reason: "rested by the seat", Until: h.now.Add(time.Hour)}))
	before := h.machineRecord()
	_, _, _, err := h.st.SetMachine(h.ctx, true)
	require.ErrorContains(t, err, "nothing funded can work")
	assert.Equal(t, before, h.machineRecord())
}

func TestTheFundsStartGuardSeesKeptNamedNeedsAndPropagatesReadFailure(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-live", "flash"), route("pro-resting", "pro"))
	h.setup(1)
	h.landThrough("s1", "s1-1")
	h.must(AddStep(sprint.AddReq{Stream: "s2", Count: 1, Needs: []string{"s1-1"}, Brief: "RESULT: pro-work sha=000000000000 tier: pro\n\nThe task."}))
	h.must(RouteRestStep(sprint.RouteRestReq{Target: "pro-resting", Reason: "rested", Until: h.now.Add(time.Hour)}))
	why, err := h.st.NothingFundedCanWork(h.ctx)
	require.NoError(t, err)
	require.Contains(t, why, "pro-resting", "landed named needs remain dealable even outside the placed work set")
	h.m.Fail = func(point string) error {
		if point == "readset t-work" {
			return errors.New("named needs read lost")
		}
		return nil
	}
	_, _, _, err = h.st.SetMachine(h.ctx, true)
	require.ErrorContains(t, err, "named needs read lost")
	h.m.Fail = nil
	assert.False(t, h.machineRecord().Running())
}

func TestRecoveryMarkerBelongsToEachExplicitRunStop(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("pro-live", "pro"))
	h.addReady("s1", 1, "RESULT: pro-work sha=000000000000 tier: pro\n\nThe task.")
	for episode := 1; episode <= 2; episode++ {
		h.startMachine()
		m := h.machineRecord()
		h.must(RouteRestStep(sprint.RouteRestReq{Target: "pro-live", Reason: "blocked episode", Until: h.now.Add(time.Hour)}))
		_, _, _, err := h.st.stopWithCause(h.ctx, sprint.NothingFundedCause, m.RunSeq)
		require.NoError(t, err)
		h.must(RouteWakeStep(sprint.RouteWakeReq{Target: "pro-live", Reason: "live again"}))
		var res TickResult
		require.NoError(t, h.st.fundedLiveAgain(h.ctx, h.machineRecord(), &res))
		require.NoError(t, h.st.fundedLiveAgain(h.ctx, h.machineRecord(), &res))
		assert.Len(t, h.tickNotes(sprint.NFundedLiveAgain), episode)
		assert.False(t, h.machineRecord().Running(), "recovery never starts the machine")
	}
}

func TestAWorkRestWrittenBetweenStartPinAndLockIsSeen(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("pro-live", "pro"))
	h.addReady("s1", 1, "RESULT: pro-work sha=000000000000 tier: pro\n\nThe task.")
	h.st.beforeGuardedWrite = func() {
		h.must(RouteRestStep(sprint.RouteRestReq{Target: "pro-live", Reason: "rested before start lock", Until: h.now.Add(time.Hour)}))
	}
	_, _, _, err := h.st.SetMachine(h.ctx, true)
	require.ErrorContains(t, err, "nothing funded can work")
	assert.False(t, h.machineRecord().Running())
}

// The route wake is a normal fenced write in the same explicit machine run.
// It occurs after the blocked plan was observed and before the cause lock.
type causeWakeInterleave struct {
	*Mem
	before func()
	ran    bool
}

func (b *causeWakeInterleave) Acquire(ctx context.Context, gen uint64, op OpRecord) (bool, error) {
	if op.Verb == "machine cause lock" && !b.ran {
		b.ran = true
		b.before()
	}
	return b.Mem.Acquire(ctx, gen, op)
}

func TestARecoveredSameRunIsNotStoppedByItsOlderBlockedPlan(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("pro-live", "pro"), route("flash-live", "flash"))
	h.addReady("s1", 1, "RESULT: pro-work sha=000000000000 tier: pro\n\nThe task.")
	h.startMachine()
	seq := h.machineRecord().RunSeq
	h.must(RouteRestStep(sprint.RouteRestReq{Target: "pro-live", Reason: "rested", Until: h.now.Add(time.Hour)}))
	why, err := h.st.NothingFundedCanWork(h.ctx)
	require.NoError(t, err)
	require.NotEmpty(t, why)
	// The tick's blocked judgment already committed before its machine stop.
	h.must(NoteStep("old blocked plan", sprint.Note{Kind: sprint.Judgment, Type: sprint.NNothingFunded, SprintLevel: true, Who: sprint.MachineActor, What: why, Decisions: []string{"ack", "wait"}}))
	b := &causeWakeInterleave{Mem: h.m}
	b.before = func() {
		h.must(RouteWakeStep(sprint.RouteWakeReq{Target: "pro-live", Reason: "paid and woke", Who: "coordinator"}))
	}
	h.st.B = b
	var res TickResult
	require.NoError(t, h.st.stopFor(h.ctx, sprint.NothingFundedCause, why, seq, &res))
	assert.True(t, h.machineRecord().Running(), "the cause was cleared in this same run before the stop's lock")
	assert.Empty(t, res.Halted)
	h.machine()
	assert.Empty(t, h.openOf(sprint.NNothingFunded), "the next running tick closes the now-stale judgment")
}

func TestARecoveryRetryWithNoPlanReportsNoUncommittedEffects(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("pro-live", "pro"))
	h.addReady("s1", 1, "RESULT: pro-work sha=000000000000 tier: pro\n\nThe task.")
	h.startMachine()
	m := h.machineRecord()
	h.must(RouteRestStep(sprint.RouteRestReq{Target: "pro-live", Reason: "blocked", Until: h.now.Add(time.Hour)}))
	_, _, _, err := h.st.stopWithCause(h.ctx, sprint.NothingFundedCause, m.RunSeq)
	require.NoError(t, err)
	m = h.machineRecord()
	h.must(RouteWakeStep(sprint.RouteWakeReq{Target: "pro-live", Reason: "live"}))
	b := &recoveryInterleave{Mem: h.m}
	b.before = func() {
		h.must(RouteRestStep(sprint.RouteRestReq{Target: "pro-live", Reason: "blocked again", Until: h.now.Add(time.Hour)}))
	}
	h.st.B = b
	r, err := h.st.Run(h.ctx, Step{Verb: "tick funded live again", Load: All, Routes: true, RequiresStopped: true, StopEpisode: &m,
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			if !s.HasLiveWorkedTier() {
				return sprint.Plan{}
			}
			return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Happened, Type: sprint.NFundedLiveAgain, What: "live again"}}}
		}})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, r.Attempts, 2)
	assert.Zero(t, r.Notes)
	assert.Empty(t, r.Op)
	assert.Nil(t, r.Tables)
	assert.Empty(t, h.tickNotes(sprint.NFundedLiveAgain))
}

func TestAStillBlockedStopReportsTheFreshRestReason(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("pro-live", "pro"), route("flash-live", "flash"))
	h.addReady("s1", 1, "RESULT: pro-work sha=000000000000 tier: pro\n\nThe task.")
	h.startMachine()
	seq := h.machineRecord().RunSeq
	h.must(RouteRestStep(sprint.RouteRestReq{Target: "pro-live", Reason: "old rest", Until: h.now.Add(time.Hour)}))
	why, err := h.st.NothingFundedCanWork(h.ctx)
	require.NoError(t, err)
	b := &causeWakeInterleave{Mem: h.m}
	b.before = func() {
		h.must(RouteWakeStep(sprint.RouteWakeReq{Target: "pro-live", Reason: "old rest ended"}))
		h.must(RouteRestStep(sprint.RouteRestReq{Target: "pro-live", Reason: "new rest reason", Until: h.now.Add(2 * time.Hour)}))
	}
	h.st.B = b
	var res TickResult
	require.NoError(t, h.st.stopFor(h.ctx, sprint.NothingFundedCause, why, seq, &res))
	assert.False(t, h.machineRecord().Running())
	assert.Contains(t, h.machineRecord().Reason, "new rest reason")
	assert.Contains(t, res.Halted, "new rest reason", "the tick reports the same current evidence as the committed stop")
}

// Append to nothing_can_work_test.go in an isolated, frozen merged candidate.
// Uses existing imports and harness; no production callback or scheduler pause.
type retiredCauseDuringClear struct {
	*Mem
	before func(OpRecord)
	ran    bool
}

func (b *retiredCauseDuringClear) Routes(ctx context.Context) (RouteSet, int64, error) {
	set, trips, err := b.Mem.Routes(ctx)
	if err != nil || b.ran {
		return set, trips, err
	}
	f, err := b.Mem.ReadFence(ctx)
	if err != nil {
		return RouteSet{}, trips, err
	}
	if f.Pending != nil && f.Pending.Lock && f.Pending.Verb == "machine cause lock" {
		b.ran = true
		b.before(*f.Pending)
	}
	return set, trips, nil
}

func TestARetiredFundedStopCannotOverwriteClearReady(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("pro-live", "pro"))
	h.addReady("s1", 1, "RESULT: pro-work sha=000000000000 tier: pro\n\nThe task.")
	h.startMachine()
	seq := h.machineRecord().RunSeq
	h.must(RouteRestStep(sprint.RouteRestReq{Target: "pro-live", Reason: "blocked", Until: h.now.Add(time.Hour)}))
	why, err := h.st.NothingFundedCanWork(h.ctx)
	require.NoError(t, err)
	require.NotEmpty(t, why)
	oldEpoch, err := h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	b := &retiredCauseDuringClear{Mem: h.m}
	var cleared Machine
	b.before = func(op OpRecord) {
		// This is the real expired-empty-lock repair path, not a fabricated
		// fence mutation. The old stop remains suspended in its live read.
		h.mu.Lock()
		h.now = h.now.Add(h.st.grace() + time.Second)
		h.mu.Unlock()
		repaired, err := h.st.finishOp(h.ctx, op)
		require.NoError(t, err)
		require.Equal(t, RepairAbandoned, repaired.Done)
		clearer := *h.st
		clearer.beforeRestore = func(*Store) error { return errors.New("witness pauses new epoch restore") }
		_, err = clearer.Clear(h.ctx)
		require.ErrorContains(t, err, "witness pauses new epoch restore")
		es, err := h.st.EpochNow(h.ctx)
		require.NoError(t, err)
		require.Equal(t, oldEpoch.N+1, es.N)
		pending, err := h.st.RestorePending(h.ctx)
		require.NoError(t, err)
		require.True(t, pending)
		cleared = h.machineRecord()
		require.True(t, cleared.Ready(), "clear advanced without a new START")
		require.Empty(t, cleared.Cause)
	}
	h.st.B = b
	_, _, changed, err := h.st.stopWithCauseReason(h.ctx, sprint.NothingFundedCause, why, seq)
	require.True(t, b.ran, "the stop reached its live revalidation while owning the fence")
	assert.False(t, changed, "a retired stop writer cannot claim a commit (err=%v)", err)
	assert.Equal(t, cleared, h.machineRecord(), "an old stop cannot overwrite clear's new-epoch READY record")
	assert.True(t, h.machineRecord().Ready())
	assert.Empty(t, h.machineRecord().Cause)
}
