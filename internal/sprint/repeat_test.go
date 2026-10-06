package sprint_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// repeatRig is the alarm rig's sprint with a repeat count on its clock, flushed as the
// run loop flushes it: one step of RepeatPlan.
type repeatRig struct {
	*alarmRig
	reps *sprint.Repeats
}

func newRepeatRig(t *testing.T) *repeatRig {
	r := &repeatRig{alarmRig: newAlarmRig(t)}
	r.reps = sprint.NewRepeats(r.at())
	return r
}

func (r *repeatRig) at() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now }

// to sets the clock to d after the count began.
func (r *repeatRig) to(began time.Time, d time.Duration) {
	r.mu.Lock()
	r.now = began.Add(d)
	r.mu.Unlock()
}

func (r *repeatRig) observe(c sprint.RepeatCause, holds ...string) { r.reps.Observe(c, holds, r.at()) }

func (r *repeatRig) flush() {
	r.t.Helper()
	_, err := r.st.Run(r.ctx, store.Step{Verb: sprint.RepeatVerb, Actor: sprint.MachineActor,
		Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.RepeatPlan(s, r.reps, sprint.MachineActor) }})
	require.NoError(r.t, err)
	r.reps.Done(r.at())
}

// repeats is the open repeat judgments.
func (r *repeatRig) repeats() []sprint.Note {
	r.t.Helper()
	open, err := r.m.OpenNotes(r.ctx)
	require.NoError(r.t, err)
	var out []sprint.Note
	for _, o := range open {
		if o.Note.Type == sprint.NRepeated && o.Note.Kind == sprint.Judgment {
			out = append(out, o.Note)
		}
	}
	return out
}

// The owner, 2026-10-05: a refusal that repeats is the machine saying something is
// stuck, and it arrives as a pushed judgment the first time it repeats, with its count,
// never as a log line alone (docs/SPEC-SPRINT.md section 8, "A repeated refusal is an
// alarm"). Three same refusals in five minutes raise one judgment; a fourth raises its
// count in place; a different cause raises its own; the cause stopping closes it.
func TestTheThirdSameRefusalInFiveMinutesRaisesOnePushedJudgment(t *testing.T) {
	t.Parallel()
	r := newRepeatRig(t)
	t0 := r.at()
	brief := sprint.RepeatCause{Verb: "tick drain", Reason: "card s1-7: field brief is 65552 bytes, over the bound of 65536 bytes"}

	r.observe(brief, "s1-7")
	r.to(t0, time.Minute)
	r.observe(brief, "s1-7")
	r.flush()
	require.Empty(t, r.repeats(), "two are not yet a repeat")

	r.to(t0, 2*time.Minute)
	r.observe(brief, "s1-7")
	r.flush()
	got := r.repeats()
	require.Len(t, got, 1, "the third in five minutes raises one judgment")
	j := got[0]
	assert.Contains(t, j.What, brief.Text(), "it names the cause")
	assert.Contains(t, j.What, "3 times", "it names the count")
	assert.Contains(t, j.What, "first "+t0.UTC().Format(time.RFC3339), "it names the first time")
	assert.Contains(t, j.What, "last "+t0.Add(2*time.Minute).UTC().Format(time.RFC3339), "it names the last time")
	assert.Contains(t, j.What, "holds up s1-7", "it names what it holds up")
	assert.Equal(t, []string{"s1-7"}, j.Primaries)
	assert.True(t, j.Marked)

	// pushed: it is the coordinator's, in the inbox, as a judgment
	in, err := r.st.Inbox(r.ctx, time.Hour, time.Hour, 100)
	require.NoError(t, err)
	var shown bool
	for _, g := range in.Groups {
		shown = shown || g.Kind == sprint.Judgment && g.Type == sprint.NRepeated
	}
	assert.True(t, shown, "the inbox shows it: %+v", in.Groups)

	assert.False(t, r.reps.Due(r.at()), "nothing new: the loop reads nothing")
	r.flush()
	require.Len(t, r.repeats(), 1, "a flush with nothing new writes nothing")

	r.to(t0, 3*time.Minute)
	r.observe(brief, "s1-7")
	assert.True(t, r.reps.Due(r.at()), "the count rose: due")
	r.flush()
	got = r.repeats()
	require.Len(t, got, 1, "a fourth raises no second judgment")
	assert.Equal(t, j.ID, got[0].ID, "the same judgment, in place")
	assert.Contains(t, got[0].What, "4 times", "its count rose")
	assert.Equal(t, 3, got[0].Before)

	land := sprint.RepeatCause{Verb: "land", Subject: "s2", Reason: "LAND REFUSED the batch's history is not the base's"}
	for i := range 3 {
		r.to(t0, 4*time.Minute+time.Duration(i)*time.Second)
		r.observe(land, "s2-1", "s2-2")
	}
	r.flush()
	got = r.repeats()
	require.Len(t, got, 2, "a different cause raises its own")

	// the brief's cause stops (its last at 3m); the land's goes on
	r.to(t0, 7*time.Minute)
	r.observe(land, "s2-1", "s2-2")
	r.flush()
	require.Len(t, r.repeats(), 2, "under five minutes quiet is not stopped")
	r.to(t0, 8*time.Minute)
	r.observe(land, "s2-1", "s2-2")
	r.flush()
	got = r.repeats()
	require.Len(t, got, 1, "five minutes quiet closes the brief's")
	assert.True(t, strings.HasPrefix(got[0].What, land.Text()), got[0].What)
	assert.Contains(t, got[0].What, "5 times")

	// the stop is said to the coordinator, once
	lines, err := r.st.Inbox(r.ctx, time.Hour, time.Hour, 100)
	require.NoError(t, err)
	stopped := 0
	for _, g := range lines.Groups {
		if g.Type == sprint.NRepeatStopped {
			stopped += g.Count
			assert.Contains(t, g.What, brief.Text())
		}
	}
	assert.Equal(t, 1, stopped, "one stopped note: %+v", lines.Groups)

	r.to(t0, 14*time.Minute)
	r.flush()
	assert.Empty(t, r.repeats(), "the land's stops too")
}

// A judgment the coordinator acknowledged stays acknowledged while its cause repeats: no
// second judgment; the cause stopping closes the acknowledgement.
func TestARepeatJudgmentAcknowledgedIsNotRaisedAgainUntilItStops(t *testing.T) {
	t.Parallel()
	r := newRepeatRig(t)
	t0 := r.at()
	c := sprint.RepeatCause{Verb: "friend reconcile", Subject: "terra", Reason: "her directory is not reachable"}
	for i := range 3 {
		r.to(t0, time.Duration(i)*time.Second)
		r.observe(c)
	}
	r.flush()
	got := r.repeats()
	require.Len(t, got, 1)
	assert.Contains(t, got[0].What, "holds up nothing it names")
	r.must(store.Step{Verb: "ack", Named: true, Plan: func(s *sprint.Snapshot) sprint.Plan {
		return sprint.Ack(s, sprint.AckReq{Notes: []string{got[0].ID}, Reason: "seen", Who: "coordinator"})
	}})
	require.Empty(t, r.repeats())
	r.to(t0, time.Minute)
	r.observe(c)
	r.flush()
	require.Empty(t, r.repeats(), "acknowledged: not raised again while it repeats")
	r.to(t0, 7*time.Minute)
	r.flush()
	open, err := r.m.OpenNotes(r.ctx)
	require.NoError(t, err)
	for _, o := range open {
		assert.NotEqual(t, sprint.NRepeated, o.Note.Type, "the cause stopped: closed")
	}
}

// A count begun after a restart closes an open judgment of a cause it never saw only
// once it has run five minutes: the cause may still be repeating.
func TestARepeatJudgmentLeftByAnEarlierServerClosesOnlyAfterAQuietWindow(t *testing.T) {
	t.Parallel()
	r := newRepeatRig(t)
	t0 := r.at()
	c := sprint.RepeatCause{Verb: "land", Subject: "s1", Reason: "LAND FAILED the check"}
	for range 3 {
		r.observe(c)
	}
	r.flush()
	require.Len(t, r.repeats(), 1)
	r.reps = sprint.NewRepeats(t0.Add(time.Minute)) // the server restarted
	r.to(t0, 2*time.Minute)
	r.flush()
	require.Len(t, r.repeats(), 1, "not yet: the new count has not seen five minutes")
	r.to(t0, 6*time.Minute)
	r.flush()
	assert.Empty(t, r.repeats(), "five minutes with no sight of it: stopped")
}

// A cause seen again after five quiet minutes the loop did not flush in is a new episode:
// the next flush closes the old judgment (tla RepeatAlarm, found by the model) and the
// new episode raises its own by its own third occurrence.
func TestARepeatSeenAgainAfterAQuietWindowIsANewEpisode(t *testing.T) {
	t.Parallel()
	r := newRepeatRig(t)
	t0 := r.at()
	c := sprint.RepeatCause{Verb: "land refused", Subject: "stream s1", Reason: "the base moved"}
	for range 3 {
		r.observe(c)
	}
	r.flush()
	first := r.repeats()
	require.Len(t, first, 1)
	r.to(t0, 6*time.Minute)
	r.observe(c) // no flush between: the loop had not seen it stop
	assert.True(t, r.reps.Due(r.at()), "a renewed episode is due")
	r.flush()
	assert.Empty(t, r.repeats(), "the old episode's judgment closed")
	r.to(t0, 7*time.Minute)
	r.observe(c)
	r.observe(c)
	r.flush()
	got := r.repeats()
	require.Len(t, got, 1)
	assert.NotEqual(t, first[0].ID, got[0].ID, "a new judgment for the new episode")
	assert.Contains(t, got[0].What, "3 times, first "+t0.Add(6*time.Minute).UTC().Format(time.RFC3339))
}
