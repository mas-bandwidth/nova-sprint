package sprint_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/hostload"
)

// readyRig is a sprint on the in-memory store with a fake clock (no real time): a fleet
// member beating, so cards added are moves due.
type readyRig struct {
	t   *testing.T
	ctx context.Context
	m   *store.Mem
	st  *store.Store
	mu  sync.Mutex
	now time.Time
}

func newReadyRig(t *testing.T) *readyRig {
	t.Helper()
	r := &readyRig{t: t, ctx: context.Background(), m: store.NewMem(), now: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	n := 0
	r.st = &store.Store{B: r.m, Names: sprint.Names{Prefix: "r-"}, Actor: "coordinator",
		Now:   func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now },
		NewID: func() string { r.mu.Lock(); defer r.mu.Unlock(); n++; return fmt.Sprint(n) },
		Sleep: func(time.Duration) {}}
	require.NoError(t, r.st.Init(r.ctx))
	require.NoError(t, r.m.RowsAdd(r.ctx, "r-fleet", []string{"m1"}))
	require.NoError(t, r.m.SetCoordinator(r.ctx, "coordinator"))
	r.beat()
	return r
}

func (r *readyRig) step(d time.Duration) {
	r.mu.Lock()
	r.now = r.now.Add(d)
	r.mu.Unlock()
}

func (r *readyRig) clock() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now }

func (r *readyRig) beat() {
	r.t.Helper()
	zero := 0.0
	_, err := r.st.Beat(r.ctx, "m1", &zero, hostload.Source{})
	require.NoError(r.t, err)
}

func (r *readyRig) add(stream string, n int) {
	r.t.Helper()
	res, err := r.st.Run(r.ctx, store.AddStep(sprint.AddReq{Stream: stream, Count: n}))
	require.NoError(r.t, err)
	require.Empty(r.t, res.Refused)
}

func (r *readyRig) machine() store.Machine {
	r.t.Helper()
	m, _, err := r.st.Machine(r.ctx)
	require.NoError(r.t, err)
	return m
}

// judgments is the open judgment groups of the inbox as read now.
func (r *readyRig) judgments() []sprint.Group {
	r.t.Helper()
	v, err := r.st.Inbox(r.ctx, time.Hour, time.Hour, 1000)
	require.NoError(r.t, err)
	var out []sprint.Group
	for _, g := range v.Groups {
		if g.Kind == sprint.Judgment {
			out = append(out, g)
		}
	}
	return out
}

func groupIDs(gs []sprint.Group) []string {
	ids := make([]string, len(gs))
	for i, g := range gs {
		ids[i] = g.ID
	}
	return ids
}

// A clear leaves the machine READY (docs/SPEC-SPRINT.md section 14, "READY";
// tla/MachineReady.tla): READY from init and after a clear from RUNNING and from STOPPED;
// READY says READY (the machine line, the view's state) and is STOPPED to a worker; with
// moves due it raises no machine:stopped judgment and no waiting-judgment warning over
// ten simulated minutes; stop on it is refused with the reason; start moves it to
// RUNNING, stop to STOPPED, which keeps its judgment; and a record a v1.2.16 clear left
// (no start epoch recorded) reads READY with nothing rewritten.
func TestAClearLeavesTheMachineReadyAndRaisesNoJudgment(t *testing.T) {
	t.Parallel()
	r := newReadyRig(t)

	// init: READY, said READY, STOPPED to a worker
	m := r.machine()
	require.True(t, m.Ready(), "a sprint never started is READY: %+v", m)
	assert.Equal(t, store.Stopped, m.StateWord(), "a worker reads READY as STOPPED: nothing is taken before the first start")
	assert.Equal(t, "machine: READY", r.st.MachineLine(r.ctx))
	assert.Equal(t, store.Ready, store.ViewState(m))

	// READY with moves due over ten simulated minutes: no judgment, no warning push
	r.add("s1", 3)
	readyQuiet := func(label string) {
		t.Helper()
		firstSeen := map[string]time.Time{}
		var warned map[string]bool
		var warnedAt time.Time
		judged, pushes := 0, 0
		for range 600 {
			r.step(time.Second)
			if r.clock().Second()%10 == 0 {
				r.beat()
				_, err := r.st.Tick(r.ctx)
				require.NoError(t, err)
			}
			gs := r.judgments()
			judged += len(gs)
			if ws := sprint.JudgmentWarnings(gs, r.clock(), firstSeen); len(ws) > 0 && sprint.JudgmentWarnDue(ws, warned, warnedAt, r.clock()) {
				pushes++
				warnedAt = r.clock()
			}
		}
		assert.Zero(t, judged, "%s: READY raises no judgment", label)
		assert.Zero(t, pushes, "%s: READY pushes no warning", label)
		assert.True(t, r.machine().Ready(), "%s: ticks leave READY", label)
	}
	readyQuiet("init")

	// stop on READY is refused with the reason, and changes nothing
	_, after, _, err := r.st.StopUntil(r.ctx, "a test", r.clock().Add(time.Hour))
	require.ErrorIs(t, err, store.ErrNothingToStop)
	assert.Contains(t, err.Error(), "the machine is READY: it has not started since the clear; nothing to stop")
	assert.True(t, after.Ready())
	assert.True(t, r.machine().Ready())

	// start: RUNNING; stop: STOPPED (not READY), and STOPPED with moves due judges as before
	_, after, _, err = r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	require.True(t, after.Running())
	assert.Equal(t, "machine: running", r.st.MachineLine(r.ctx))
	_, _, _, err = r.st.StopUntil(r.ctx, "a test", r.clock().Add(time.Hour))
	require.NoError(t, err, "a stop of a RUNNING machine is a stop, at the same clock reading as its start too")
	m = r.machine()
	require.False(t, m.Ready(), "a machine stopped after its start is STOPPED, not READY: %+v", m)
	assert.Equal(t, store.Stopped, store.ViewState(m))
	r.add("s1", 2) // moves due on the STOPPED machine
	assert.Contains(t, groupIDs(r.judgments()), "machine:stopped", "STOPPED with moves due keeps its judgment")

	// a clear from STOPPED: READY, and quiet
	r.step(time.Second)
	_, err = r.st.Clear(r.ctx)
	require.NoError(t, err)
	require.True(t, r.machine().Ready(), "a clear from STOPPED leaves READY")
	assert.Equal(t, "machine: READY", r.st.MachineLine(r.ctx))
	r.add("s1", 3)
	readyQuiet("after a clear from STOPPED")

	// a clear from RUNNING: READY, and quiet
	_, _, _, err = r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	r.step(time.Second)
	_, err = r.st.Clear(r.ctx)
	require.NoError(t, err)
	require.True(t, r.machine().Ready(), "a clear from RUNNING leaves READY")
	r.add("s1", 3)
	readyQuiet("after a clear from RUNNING")

	// migration: the record a v1.2.16 clear left (no start epoch recorded) reads READY,
	// with nothing rewritten
	_, _, _, err = r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	r.step(time.Second)
	_, err = r.st.Clear(r.ctx)
	require.NoError(t, err)
	raw, ok, err := r.m.GetKey(r.ctx, "machine")
	require.NoError(t, err)
	require.True(t, ok)
	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &rec))
	require.Contains(t, rec, "started_in")
	delete(rec, "started_in")
	old, err := json.Marshal(rec)
	require.NoError(t, err)
	require.NoError(t, r.m.SetKey(r.ctx, "machine", string(old)))
	require.True(t, r.machine().Ready(), "a v1.2.16 record cleared and never started since reads READY")
	r.step(time.Second)
	assert.Empty(t, r.judgments(), "and raises nothing")
}

// The warning's rate (sprint.JudgmentWarnDue): the first warning of a set at once, the
// same set no more often than once a JudgmentWarnEvery, a judgment joining the set at
// once, and a set that only shrank waits its minute.
func TestTheWarningIsDueAtOnceForANewJudgmentElseOnceAMinute(t *testing.T) {
	t.Parallel()
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	a, b := sprint.JudgmentWarning{ID: "a"}, sprint.JudgmentWarning{ID: "b"}
	one, two := []sprint.JudgmentWarning{a}, []sprint.JudgmentWarning{a, b}
	assert.True(t, sprint.JudgmentWarnDue(one, nil, time.Time{}, at), "the first warning of a set")
	seen := map[string]bool{"a": true}
	assert.False(t, sprint.JudgmentWarnDue(one, seen, at, at.Add(time.Second)), "the same set a second later")
	assert.False(t, sprint.JudgmentWarnDue(one, seen, at, at.Add(sprint.JudgmentWarnEvery-time.Second)), "the same set within the minute")
	assert.True(t, sprint.JudgmentWarnDue(one, seen, at, at.Add(sprint.JudgmentWarnEvery)), "the same set a minute later")
	assert.True(t, sprint.JudgmentWarnDue(two, seen, at, at.Add(time.Second)), "a judgment joining the set: at once")
	assert.False(t, sprint.JudgmentWarnDue(one, map[string]bool{"a": true, "b": true}, at, at.Add(time.Second)), "a set that only shrank waits its minute")
}
