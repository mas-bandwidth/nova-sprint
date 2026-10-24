package sprint_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// tickConfigDrift applies the config-drift judgment on the facts given (nil: none
// read this tick), as the binding's read would.
func (r *alarmRig) tickConfigDrift(facts *sprint.ConfigDriftFacts) {
	r.t.Helper()
	res, err := r.st.Run(r.ctx, store.Step{Verb: "tick config drift", Actor: sprint.MachineActor, Load: store.All, Plan: func(s *sprint.Snapshot) sprint.Plan {
		p, _ := sprint.TickConfigDrift(s, sprint.TickReq{ConfigDrift: facts})
		return p
	}})
	require.NoError(r.t, err)
	require.Empty(r.t, res.Refused)
}

// configDriftOpen is the one open config-drift judgment, with its note.
func (r *alarmRig) configDriftOpen() (note sprint.Note, open bool) {
	r.t.Helper()
	for _, o := range r.snap().Open {
		if o.Note.Type != sprint.NConfigDrift {
			continue
		}
		require.False(r.t, open, "more than one open config-drift judgment")
		require.Equal(r.t, sprint.ConfigDriftSubject, o.Subject())
		note, open = o.Note, true
	}
	return note, open
}

// configDriftWritten is how many config-drift judgments the sprint wrote.
func (r *alarmRig) configDriftWritten() int {
	r.t.Helper()
	notes, _, err := r.m.NotesSince(r.ctx, "", 100000)
	require.NoError(r.t, err)
	n := 0
	for _, x := range notes {
		if x.Type == sprint.NConfigDrift && x.Kind == sprint.Judgment {
			n++
		}
	}
	return n
}

// The fleet table against nova-config is one judgment per drift set: it names each
// member and field and the one command that applies them, is written once while the
// set stands, kept by a tick that read nothing, replaced when the set changes, and
// closed by a read that finds no drift. It applies nothing.
func TestConfigDriftIsOneJudgmentPerDriftSet(t *testing.T) {
	t.Parallel()
	r := newAlarmRig(t)
	widths := &sprint.ConfigDriftFacts{Drift: []sprint.Drift{
		{Member: "amd-1", Kind: sprint.DriftWidth, From: 8, To: 4},
		{Member: "hetzner1", Kind: sprint.DriftWidth, From: 8, To: 4},
	}}

	// a read that finds none raises nothing
	r.tickConfigDrift(&sprint.ConfigDriftFacts{})
	_, open := r.configDriftOpen()
	require.False(t, open, "no drift raised a judgment")

	// two widths differ: one judgment naming both members, their fields, and the fix
	r.tickConfigDrift(widths)
	note, open := r.configDriftOpen()
	require.True(t, open, "the drift raised no judgment")
	require.Equal(t, sprint.Judgment, note.Kind)
	for _, want := range []string{"amd-1 has width 8 and the inventory says 4", "hetzner1 has width 8 and the inventory says 4", sprint.DriftWidth + ":", "run: nova-sprint fleet sync"} {
		require.Contains(t, note.What, want, "the judgment lacks %q: %s", want, note.What)
	}
	require.Equal(t, 1, r.configDriftWritten())

	// the same set again, and a tick that read nothing: the same judgment, not rewritten
	r.tickConfigDrift(widths)
	r.tickConfigDrift(nil)
	again, open := r.configDriftOpen()
	require.True(t, open, "a tick with no read closed the judgment")
	require.Equal(t, note.ID, again.ID, "the standing judgment was replaced")
	require.Equal(t, 1, r.configDriftWritten(), "the same set wrote a second judgment")

	// one member applied, the other still differs: a new set is a new judgment
	r.tickConfigDrift(&sprint.ConfigDriftFacts{Drift: widths.Drift[1:]})
	changed, open := r.configDriftOpen()
	require.True(t, open)
	require.NotEqual(t, note.ID, changed.ID, "a changed set kept the old judgment")
	require.NotContains(t, changed.What, "amd-1", "the new set names a member that no longer differs")
	require.Contains(t, changed.What, "hetzner1")
	require.Equal(t, 2, r.configDriftWritten())

	// a read with no drift: closed
	r.tickConfigDrift(&sprint.ConfigDriftFacts{})
	_, open = r.configDriftOpen()
	require.False(t, open, "a read with no drift left the judgment open")
	require.Equal(t, 2, r.configDriftWritten(), "the clear wrote a judgment")
}
