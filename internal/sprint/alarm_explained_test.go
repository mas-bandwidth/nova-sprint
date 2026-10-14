package sprint_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/storetest"
	"github.com/mas-bandwidth/nova-sprint/pkg/hostload"
)

// An alarm whose condition stands but whose cause is already open elsewhere is not raised
// (docs/SPEC-SPRINT.md section 8, "Backlog alarms"; the owner, 2026-10-13): the tick does
// not raise the alarm and does not re-raise a seat's wait on it, one quiet note names the
// cause per episode, an open alarm judgment closes with that note, and the alarm raises as
// today when the cause ends or the condition ends. internal/sprint/alarms.go alarmExplained.

// explainedRig is a sprint on the in-memory twin store, ticked by the machine, with a fake
// clock; each tick beats only the members named.
type explainedRig struct {
	t   *testing.T
	ctx context.Context
	m   *store.Mem
	st  *store.Store
	mu  sync.Mutex
	now time.Time
}

func newExplainedRig(t *testing.T) *explainedRig {
	t.Helper()
	r := &explainedRig{t: t, ctx: context.Background(), m: store.NewMem(), now: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	n := 0
	r.st = &store.Store{B: r.m, Names: sprint.Names{Prefix: "x-"}, Actor: "coordinator",
		Now:   func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now },
		NewID: func() string { r.mu.Lock(); defer r.mu.Unlock(); n++; return fmt.Sprint(n) },
		Sleep: func(time.Duration) {}}
	r.st.CheckTwin = func(twin, fresh *sprint.Snapshot) error {
		if d := storetest.TwinDiff(twin, fresh); d != "" {
			return errors.New(d)
		}
		return nil
	}
	require.NoError(t, r.st.Init(r.ctx))
	require.NoError(t, r.m.RowsAdd(r.ctx, "x-readers", []string{"reader-a", "reader-b"}))
	require.NoError(t, r.m.SetCoordinator(r.ctx, "coordinator"))
	r.must(store.SetStep(sprint.SetReq{ReadCards: sprint.SwitchOff, Who: "coordinator"}))
	return r
}

func (r *explainedRig) must(step store.Step) {
	r.t.Helper()
	res, err := r.st.Run(r.ctx, step)
	require.NoError(r.t, err, "%s", step.Verb)
	require.Empty(r.t, res.Refused, "%s refused", step.Verb)
}

// beat beats the readers and the members named at the clock's time.
func (r *explainedRig) beat(members ...string) {
	r.t.Helper()
	require.NoError(r.t, r.st.BeatReaders(r.ctx))
	zero := 0.0
	for _, m := range members {
		_, err := r.st.Beat(r.ctx, m, &zero, hostload.Source{})
		require.NoError(r.t, err)
	}
}

// clock moves the fake clock on by d.
func (r *explainedRig) clock(d time.Duration) {
	r.mu.Lock()
	r.now = r.now.Add(d)
	r.mu.Unlock()
}

// tick beats the members named and runs one tick.
func (r *explainedRig) tick(members ...string) {
	r.t.Helper()
	r.clock(time.Second)
	r.beat(members...)
	_, err := r.st.Tick(r.ctx)
	require.NoError(r.t, err)
}

func (r *explainedRig) snap() *sprint.Snapshot {
	r.t.Helper()
	s, err := r.st.Load(r.ctx, store.All, nil)
	require.NoError(r.t, err)
	return s
}

// open is the id of the open judgment of the type, "" when none is.
func (r *explainedRig) open(typ string) string {
	for _, o := range r.snap().Open {
		if o.Note.Type == typ {
			return o.Note.ID
		}
	}
	return ""
}

// quietNotes is the quiet notes the sprint wrote whose what says word, with the word "explained by".
func (r *explainedRig) quietNotes(typ string) []sprint.Note {
	r.t.Helper()
	notes, _, err := r.m.NotesSince(r.ctx, "", 100000)
	require.NoError(r.t, err)
	var out []sprint.Note
	for _, n := range notes {
		if n.Kind == sprint.Happened && n.Type == sprint.NAlarmQuiet && strings.HasPrefix(n.What, typ+":") {
			out = append(out, n)
		}
	}
	return out
}

// finish reports the primary's work card, taken by m, finished ok.
func (r *explainedRig) finish(m, id string) {
	r.t.Helper()
	s := r.snap()
	wc := s.Fleet.Card(s.Work.Card(id).F("work"))
	require.NotNil(r.t, wc, "%s has a work card", id)
	r.must(store.FinishStep(sprint.FinishReq{As: m, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}, Who: m}))
}

// reviewCard puts one primary in review with the alarm threshold at 0: the review alarm's
// condition. m1 up width 4, a dealt and taken, finished.
func reviewCard(t *testing.T, r *explainedRig) {
	t.Helper()
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 4}))
	r.must(store.AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"a"}}))
	_, _, _, err := r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	r.must(store.SetStep(sprint.SetReq{AlarmReview: "0", Who: "coordinator"}))
	r.tick("m1") // dealt
	r.must(store.TakeStep(sprint.TakeReq{As: "m1", Sel: sprint.Sel{Limit: 1}, Who: "m1"}))
	r.finish("m1", "a")
	r.tick("m1")
}

func TestAnAlarmExplainedByAnOpenCauseIsNotRaised(t *testing.T) {
	t.Parallel()

	t.Run("review quiet while a merge window is open and raised after it ends", func(t *testing.T) {
		t.Parallel()
		r := newExplainedRig(t)
		r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 4}))
		r.must(store.AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"a"}}))
		_, _, _, err := r.st.SetMachine(r.ctx, true)
		require.NoError(t, err)
		r.must(store.SetStep(sprint.SetReq{AlarmReview: "0", Who: "coordinator"}))
		// the window opens before the primary reaches review: the alarm is not raised
		r.must(store.MergeWindowStep(sprint.MergeWindowReq{For: "1h", Reason: "the release merges by hand", Who: "coordinator"}))
		r.tick("m1") // dealt
		r.must(store.TakeStep(sprint.TakeReq{As: "m1", Sel: sprint.Sel{Limit: 1}, Who: "m1"}))
		r.finish("m1", "a")
		r.tick("m1")
		require.Equal(t, 1, len(r.snap().Work.Column(sprint.Review)), "a is in review")
		assert.Empty(t, r.open(sprint.NAlarmReview), "the review alarm is not raised while the window explains it")
		quiet := r.quietNotes(sprint.NAlarmReview)
		require.Len(t, quiet, 1, "one quiet note names the cause")
		assert.Contains(t, quiet[0].What, "explained by merge window open until")

		// the window ends: the condition stands and the alarm raises as today
		r.clock(2 * time.Hour)
		r.tick("m1")
		assert.NotEmpty(t, r.open(sprint.NAlarmReview), "the review alarm raises once the window ends")
	})

	t.Run("an open alarm closes with the quiet note when its cause appears", func(t *testing.T) {
		t.Parallel()
		r := newExplainedRig(t)
		reviewCard(t, r)
		require.NotEmpty(t, r.open(sprint.NAlarmReview), "the review alarm is open")
		require.Empty(t, r.quietNotes(sprint.NAlarmReview))

		// the cause appears: the open judgment closes, one note names the cause
		r.must(store.MergeWindowStep(sprint.MergeWindowReq{For: "1h", Reason: "the release merges by hand", Who: "coordinator"}))
		r.tick("m1")
		assert.Empty(t, r.open(sprint.NAlarmReview), "the open alarm closes itself when its cause appears")
		quiet := r.quietNotes(sprint.NAlarmReview)
		require.Len(t, quiet, 1, "one quiet note names the cause")
		assert.Contains(t, quiet[0].What, "explained by merge window open until")
	})

	t.Run("a seat wait on an explained alarm is not re-raised", func(t *testing.T) {
		t.Parallel()
		r := newExplainedRig(t)
		reviewCard(t, r)
		id := r.open(sprint.NAlarmReview)
		require.NotEmpty(t, id, "the review alarm is open")
		r.must(store.WaitStep(sprint.WaitReq{Note: id, Until: r.now.Add(20 * time.Minute), Who: "coordinator"}))

		// the cause appears within the wait: the wait is closed, not re-raised
		r.must(store.MergeWindowStep(sprint.MergeWindowReq{For: "1h", Reason: "the release merges by hand", Who: "coordinator"}))
		for range 3 {
			r.tick("m1")
		}
		assert.Empty(t, r.open(sprint.NAlarmReview), "no judgment is re-raised while the window explains the alarm")
		quiet := r.quietNotes(sprint.NAlarmReview)
		require.Len(t, quiet, 1, "one quiet note names the cause")
	})

	t.Run("fleet quiet while every non-working member is held or down judged, and raised when one is not", func(t *testing.T) {
		t.Parallel()
		r := newExplainedRig(t)
		r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 4}))
		r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m2", Width: 4}))
		r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m3", Width: 4}))
		r.must(store.AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"a", "b", "c", "d", "e", "f"}}))
		r.must(store.AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"g"}, Needs: []string{"a"}}))
		_, _, _, err := r.st.SetMachine(r.ctx, true)
		require.NoError(t, err)
		r.must(store.SetStep(sprint.SetReq{AlarmFleet: "50", Who: "coordinator"}))
		r.tick("m1", "m2", "m3") // dealt round the fleet
		// m1 works one card; m2 and m3 do not work
		r.must(store.TakeStep(sprint.TakeReq{As: "m1", Sel: sprint.Sel{Limit: 1}, Who: "m1"}))
		r.tick("m1", "m2", "m3")

		// the server host m2 is held with a reason, and m3 stops beating and is down with an
		// open judgment: every member that does not work is explained, and the alarm is not
		// raised.
		r.must(store.FleetStep(sprint.FleetReq{Op: "hold", Member: "m2", Who: "coordinator", Reason: "the server host", Finish: true}))
		r.clock(time.Minute)
		r.tick("m1") // m3 stops beating: past its missed-beats bound it goes down
		require.Equal(t, sprint.Down, r.snap().MemberCtl("m3").F("status"), "m3 is down")
		r.clock(3 * time.Minute) // past the stop dwell: m3's down is a judgment
		r.tick("m1")
		require.NotEmpty(t, r.open(sprint.NStopMemberDown), "m3's down is an open judgment")
		r.tick("m1") // the next tick's deadlines see the open down judgment
		require.Equal(t, 1, r.snap().Fleet.Count("m1", sprint.Working), "m1 works")
		assert.Empty(t, r.open(sprint.NAlarmFleet), "the fleet alarm is not raised: its members' idleness is explained")
		quiet := r.quietNotes(sprint.NAlarmFleet)
		require.Len(t, quiet, 1, "one quiet note names the cause")
		assert.Contains(t, quiet[0].What, "explained by")

		// the server host is released (unheld): it is up and does not work, and the alarm
		// raises as today.
		r.must(store.FleetStep(sprint.FleetReq{Op: "release", Member: "m2", Who: "coordinator"}))
		r.tick("m1", "m2")
		assert.NotEmpty(t, r.open(sprint.NAlarmFleet), "the fleet alarm raises once a non-working member is unexplained")
	})
}
