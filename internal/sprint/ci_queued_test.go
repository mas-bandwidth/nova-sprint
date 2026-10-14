package sprint_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// tickCIQueued applies the queued-CI judgment on the facts given (nil: none read
// this tick), as the binding's read would.
func (r *alarmRig) tickCIQueued(facts []sprint.CIQueuedFacts) {
	r.t.Helper()
	res, err := r.st.Run(r.ctx, store.Step{Verb: "tick ci queued", Actor: sprint.MachineActor, Load: store.All, Plan: func(s *sprint.Snapshot) sprint.Plan {
		p, _ := sprint.TickCIQueued(s, sprint.TickReq{CIQueued: facts})
		return p
	}})
	require.NoError(r.t, err)
	require.Empty(r.t, res.Refused)
}

// ciQueuedOpen is the one open queued-CI judgment, by subject, with its note.
func (r *alarmRig) ciQueuedOpen() (subject string, note sprint.Note, id string) {
	r.t.Helper()
	for _, o := range r.snap().Open {
		if o.Note.Type != sprint.NCIQueuedNoRunner {
			continue
		}
		require.Empty(r.t, id, "more than one open queued-CI judgment")
		subject, note, id = o.Subject(), o.Note, o.Note.ID
	}
	return subject, note, id
}

// ciQueuedWritten is how many queued-CI judgments the sprint wrote.
func (r *alarmRig) ciQueuedWritten() int {
	r.t.Helper()
	notes, _, err := r.m.NotesSince(r.ctx, "", 100000)
	require.NoError(r.t, err)
	n := 0
	for _, x := range notes {
		if x.Type == sprint.NCIQueuedNoRunner && x.Kind == sprint.Judgment {
			n++
		}
	}
	return n
}

// A CI job queued past the bound with no runner online is one judgment (docs/SPEC-SPRINT.md,
// "Drift alarms", beside the base's CI after a landing and the landing-stall bound): written
// once, never rewritten while the same job sits queued, closed when the job is no longer in
// the facts, and never raised while it is under the bound or the setting raises the bound.
func TestACIJobQueuedPastTheBoundIsAJudgment(t *testing.T) {
	t.Parallel()
	r := newAlarmRig(t)
	const repo, job = "mas-bandwidth/rocketnet", "build-x64"
	labels := []string{"self-hosted", "Linux", "X64", "rocketnet"}
	fact := func(since time.Time, online string) []sprint.CIQueuedFacts {
		return []sprint.CIQueuedFacts{{Repo: repo, RunID: "42", Run: "CI", Job: job, Labels: labels,
			QueuedSince: since, RunnerOnline: online}}
	}

	// 1. a job queued 11 minutes before now, runner "no": one judgment naming the repo,
	// the job and the labels
	r.tickCIQueued(fact(r.now.Add(-11*time.Minute), "no"))
	sub, note, first := r.ciQueuedOpen()
	require.Equal(t, repo+"@"+job, sub, "the subject names the repo and the job")
	require.Contains(t, note.What, repo, "the judgment names the repo")
	require.Contains(t, note.What, job, "and the job")
	require.Contains(t, note.What, "self-hosted", "and the runs-on labels")
	require.Contains(t, note.What, "rocketnet")
	require.Equal(t, 1, r.ciQueuedWritten())
	require.NotEmpty(t, first)

	// 4. a second tick with the same fact: no second judgment, the same one stands
	r.tickCIQueued(fact(r.now.Add(-11*time.Minute), "no"))
	_, again, second := r.ciQueuedOpen()
	require.Equal(t, 1, r.ciQueuedWritten(), "a second tick wrote a second judgment")
	require.Equal(t, first, second, "the standing judgment was replaced")
	require.Equal(t, note.What, again.What, "the standing judgment was rewritten")

	// 5. a tick with no facts: closed
	r.tickCIQueued(nil)
	_, _, gone := r.ciQueuedOpen()
	require.Empty(t, gone, "no facts did not close the judgment")

	// 2. the same fact at 9 minutes: under the bound, nothing raised
	r.tickCIQueued(fact(r.now.Add(-9*time.Minute), "no"))
	_, _, young := r.ciQueuedOpen()
	require.Empty(t, young, "a job under the bound raised a judgment")
	require.Equal(t, 1, r.ciQueuedWritten())

	// 3. with the setting at 30m, 11 minutes is under the bound: nothing
	r.must(store.SetStep(sprint.SetReq{CIQueuedBound: "30m", Who: "coordinator"}))
	r.tickCIQueued(fact(r.now.Add(-11*time.Minute), "no"))
	_, _, set := r.ciQueuedOpen()
	require.Empty(t, set, "the setting at 30m did not raise the bound")
	require.Equal(t, 1, r.ciQueuedWritten())
}
