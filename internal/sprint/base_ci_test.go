package sprint_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// tickBaseCI applies the base's CI judgment on the facts given (nil: none read
// this tick), as the binding's read would.
func (r *alarmRig) tickBaseCI(facts []sprint.BaseCIFacts) {
	r.t.Helper()
	res, err := r.st.Run(r.ctx, store.Step{Verb: "tick base ci", Actor: sprint.MachineActor, Load: store.All, Plan: func(s *sprint.Snapshot) sprint.Plan {
		p, _ := sprint.TickBaseCI(s, sprint.TickReq{BaseCI: facts})
		return p
	}})
	require.NoError(r.t, err)
	require.Empty(r.t, res.Refused)
}

// baseCIOpen is the one open judgment of the base's CI, by subject, with its note.
func (r *alarmRig) baseCIOpen() (subject string, note sprint.Note, id string) {
	r.t.Helper()
	for _, o := range r.snap().Open {
		if o.Note.Type != sprint.NBaseCIRed {
			continue
		}
		require.Empty(r.t, id, "more than one open red-base judgment")
		subject, note, id = o.Subject(), o.Note, o.Note.ID
	}
	return subject, note, id
}

// baseCIWritten is how many red-base judgments the sprint wrote.
func (r *alarmRig) baseCIWritten() int {
	r.t.Helper()
	notes, _, err := r.m.NotesSince(r.ctx, "", 100000)
	require.NoError(r.t, err)
	n := 0
	for _, x := range notes {
		if x.Type == sprint.NBaseCIRed && x.Kind == sprint.Judgment {
			n++
		}
	}
	return n
}

// The base red after a landing is one judgment per red stretch (docs/SPEC-SPRINT.md
// section 8, "Drift alarms"): written once when the red starts, never rewritten or
// pushed again for a new tip in the same stretch, kept open while the facts say
// nothing of its (Repo, Base), closed when a fact for that (Repo, Base) is green,
// and a new stretch (green, then red again) is a new judgment.
func TestARedBaseIsOneJudgmentPerRedStretch(t *testing.T) {
	t.Parallel()
	r := newAlarmRig(t)
	const repo, base = "mas-bandwidth/nova-tools", "main"
	sub := repo + "@" + base
	tipA, tipB, tipC, tipD := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)
	red := func(tip, first string) []sprint.BaseCIFacts {
		return []sprint.BaseCIFacts{{Repo: repo, Base: base, Tip: tip, Red: true, FirstRed: first, Failed: []string{"internal/ci TestHarnessWatchIsWired"}}}
	}

	// red at tip A, first red A: one judgment naming A and the failing check
	r.tickBaseCI(red(tipA, tipA))
	gotSub, note, first := r.baseCIOpen()
	require.Equal(t, sub, gotSub, "the subject names the repo and the base")
	require.Contains(t, note.What, tipA[:12], "the judgment names the first red commit")
	require.Contains(t, note.What, "TestHarnessWatchIsWired", "and the failing check")
	require.Equal(t, 1, r.baseCIWritten())

	// red again at tip B, first red A: still one judgment, not rewritten, nothing pushed
	r.tickBaseCI(red(tipB, tipA))
	_, again, id := r.baseCIOpen()
	require.Equal(t, 1, r.baseCIWritten(), "a new tip in the same stretch wrote a second judgment")
	require.Equal(t, first, id, "the standing judgment was replaced")
	require.Contains(t, again.What, tipA[:12], "the standing judgment was rewritten")
	require.NotContains(t, again.What, tipB[:12], "the standing judgment was rewritten to the new tip")

	// a tick that read no facts: it stays open
	r.tickBaseCI(nil)
	_, _, still := r.baseCIOpen()
	require.Equal(t, first, still, "a tick with no facts closed the judgment")

	// green at C: closed
	r.tickBaseCI([]sprint.BaseCIFacts{{Repo: repo, Base: base, Tip: tipC}})
	_, _, gone := r.baseCIOpen()
	require.Empty(t, gone, "green did not close the judgment")

	// red again at D: a second stretch is a second judgment naming D
	r.tickBaseCI(red(tipD, tipD))
	gotSub, second, _ := r.baseCIOpen()
	require.Equal(t, sub, gotSub)
	require.Contains(t, second.What, tipD[:12], "the second judgment names its own first red commit")
	require.Equal(t, 2, r.baseCIWritten(), "a new stretch wrote a new judgment")
}

// Two repositories red at once are two judgments, and a fact for another
// (Repo, Base) never closes this one: the judgment is keyed by its (Repo, Base).
func TestTwoRedRepositoriesAreTwoJudgments(t *testing.T) {
	t.Parallel()
	r := newAlarmRig(t)
	x, y := strings.Repeat("1", 40), strings.Repeat("2", 40)
	facts := []sprint.BaseCIFacts{
		{Repo: "mas-bandwidth/nova-tools", Base: "main", Tip: x, Red: true, FirstRed: x, Failed: []string{"TestTools"}},
		{Repo: "mas-bandwidth/nova-sprint", Base: "sprint/base", Tip: y, Red: true, FirstRed: y, Failed: []string{"TestSprint"}},
	}
	subjects := func() map[string]sprint.Note {
		r.t.Helper()
		out := map[string]sprint.Note{}
		for _, o := range r.snap().Open {
			if o.Note.Type == sprint.NBaseCIRed {
				out[o.Subject()] = o.Note
			}
		}
		return out
	}
	r.tickBaseCI(facts)
	got := subjects()
	require.Len(t, got, 2, "two red repositories: %+v", got)
	require.Contains(t, got["mas-bandwidth/nova-tools@main"].What, "TestTools")
	require.Contains(t, got["mas-bandwidth/nova-sprint@sprint/base"].What, "TestSprint")

	// a green fact for the first alone closes it and leaves the second standing
	r.tickBaseCI([]sprint.BaseCIFacts{{Repo: "mas-bandwidth/nova-tools", Base: "main", Tip: x}})
	got = subjects()
	require.Len(t, got, 1, "a fact for another repository closed the second: %+v", got)
	require.Contains(t, got, "mas-bandwidth/nova-sprint@sprint/base")
	require.Equal(t, 2, r.baseCIWritten(), "the green fact wrote a judgment")
}
