package sprint_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// The owner, 2026-10-11: "Defaults should always be the sprint machine works", "I am sick
// of these invisible settings being off", and "There must be an alert, every time cards are
// added to "review" set, if read cards is off, and you get it via push as a warning". These
// pin the design settings' working defaults on a real (in-memory) store and what the hook's
// two warnings say (settings_warn.go; docs/SPEC-SPRINT.md section 8, "Design settings off").

// requireWorkingDefaults says every design setting of the snapshot is at its working value,
// with no property written for any of them, or default.
func requireWorkingDefaults(t *testing.T, s *sprint.Snapshot, when string) {
	t.Helper()
	assert.True(t, s.ReadCardsOn(), "%s: read cards are on", when)
	for _, prop := range []string{sprint.PropReadCards, sprint.PropAlarmReview, sprint.PropAlarmReady, sprint.PropAlarmMerging, sprint.PropAlarmFleet} {
		v, ok := s.Work.Prop(prop)
		assert.True(t, !ok || v == sprint.ReadTierDefault, "%s: %s is %q: none or default, the working value", when, prop, v)
	}
	want := map[string]string{"--read-cards": "on", "--alarm-review": "40", "--alarm-ready": "on", "--alarm-merging": "15", "--alarm-fleet": "50"}
	got := map[string]string{}
	for _, d := range sprint.DesignSettings(s) {
		got[d.Flag] = d.Value
		assert.True(t, d.At, "%s: %s is %s, at its working value %s", when, d.Flag, d.Value, d.Working)
	}
	assert.Equal(t, want, got, when)
	assert.Empty(t, sprint.SettingsOffWhat(sprint.DesignSettings(s)), when)
}

// openJ is the open judgments of the type on the store.
func (r *alarmRig) openJ(typ string) []sprint.Open {
	r.t.Helper()
	var out []sprint.Open
	for _, o := range r.snap().Open {
		if o.Note.Type == typ {
			out = append(out, o)
		}
	}
	return out
}

func (r *alarmRig) set(req sprint.SetReq) {
	r.t.Helper()
	req.Who = "coordinator"
	r.must(store.SetStep(req))
}

// A fresh store, and a store after a clear whose settings were all moved, has every
// design setting at its working value with nothing written: read cards on, the alarms at
// 40, on, 15 and 50 percent. set --read-cards off turns read cards off; default turns them on
// again, as on does.
func TestAFreshAndAClearedStoreHaveEveryDesignSettingAtItsWorkingDefault(t *testing.T) {
	t.Parallel()
	r := newAlarmRig(t)
	requireWorkingDefaults(t, r.snap(), "a fresh store")

	r.set(sprint.SetReq{ReadCards: "off"})
	assert.False(t, r.snap().ReadCardsOn(), "set --read-cards off turns them off")
	r.set(sprint.SetReq{ReadCards: "default"})
	assert.True(t, r.snap().ReadCardsOn(), "set --read-cards default is on")
	r.set(sprint.SetReq{ReadCards: "off"})
	r.set(sprint.SetReq{ReadCards: "on"})
	assert.True(t, r.snap().ReadCardsOn(), "set --read-cards on")

	// every design setting moved off its working value (the ready alarm has none to move: on
	// is its one value, an alarm is never off)
	r.set(sprint.SetReq{ReadCards: "off", AlarmReview: "7", AlarmMerging: "9", AlarmFleet: "11"})
	for _, d := range sprint.DesignSettings(r.snap()) {
		assert.Equal(t, d.Flag == "--alarm-ready", d.At, "%s is %s, working value %s", d.Flag, d.Value, d.Working)
	}
	_, err := r.st.Clear(r.ctx)
	require.NoError(t, err)
	requireWorkingDefaults(t, r.snap(), "after a clear")

	// default restores an alarm's working threshold, as a number does
	r.set(sprint.SetReq{AlarmReview: "7"})
	assert.Contains(t, sprint.SettingsOffWhat(sprint.DesignSettings(r.snap())), "--alarm-review is 7, working value 40 (restore: nova-sprint set --alarm-review 40)")
	r.set(sprint.SetReq{AlarmReview: "default"})
	requireWorkingDefaults(t, r.snap(), "after set --alarm-review default")
}

// The backlog alarms are on with no set at all: a dealt and untaken, b waiting on it, nothing
// ready, raises the ready alarm and the fleet's (m1 works 0 of its width 4, below 50
// percent) by themselves; both were off by default before 2026-10-11.
func TestTheBacklogAlarmsAreOnWithNoSet(t *testing.T) {
	t.Parallel()
	r := newAlarmRig(t)
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 4}))
	r.must(store.AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"a"}}))
	r.must(store.AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"b"}, Needs: []string{"a"}}))
	_, _, _, err := r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	r.ticks(3)
	require.Empty(t, r.snap().Work.Column(sprint.Ready), "the fixture: a dealt")
	require.Len(t, r.openJ(sprint.NAlarmReady), 1, "nothing ready while b waits: the ready alarm is on by default")
	require.Len(t, r.openJ(sprint.NAlarmFleet), 1, "m1 works 0 of 4 with b waiting: the fleet alarm is on by default")
	assert.Empty(t, sprint.SettingsOffWhat(sprint.DesignSettings(r.snap())), "no setting is off")
}

// readRig is the alarm rig with three members up, each with its reader row (reader-m1,
// reader-m2, reader-m3), every one beaten before each tick.
func readRig(t *testing.T) *alarmRig {
	t.Helper()
	r := newAlarmRig(t)
	require.NoError(t, r.m.RowsAdd(r.ctx, "a-readers", []string{"reader-m1", "reader-m2", "reader-m3"}))
	for _, m := range []string{"m1", "m2", "m3"} {
		r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: m, Width: 2}))
	}
	r.extra = []string{"m2", "m3"}
	r.beat()
	return r
}

// With read cards at their default (nothing set), a primary that enters review with two
// free readers that may read it has every read it needs dealt as read cards by the next
// tick, with no seat verb (no ask): the bug of 2026-10-11, 75 cards in review and 81 of 140
// fleet slots idle while the setting sat off after a clear.
func TestReadsAreDealtByTheTickWithNoAskAtTheDefault(t *testing.T) {
	t.Parallel()
	r := readRig(t)
	r.must(store.AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"a"}}))
	_, _, _, err := r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	r.ticks(2)
	s := r.snap()
	wc := s.Fleet.Card(s.Work.Card("a").F("work"))
	require.NotNil(t, wc, "a is dealt")
	row := wc.Row
	r.must(store.TakeStep(sprint.TakeReq{As: row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: max(wc.Int("gen"), 1)}, Who: row}))
	wc = r.snap().Fleet.Card(wc.ID)
	r.must(store.FinishStep(sprint.FinishReq{As: row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}, Who: row}))

	// the tick that takes a into review deals its reads, no ask run
	r.ticks(1)
	s = r.snap()
	require.Equal(t, sprint.Review, s.Work.Card("a").Col, "the fixture: a in review")
	pr := s.Work.Card("a")
	need := sprint.ReadsNeededIn(s, pr)
	require.GreaterOrEqual(t, need, 1)
	var reads []*sprint.Card
	for _, c := range s.Fleet.Column(sprint.Ready, sprint.Working) {
		if c.F("kind") == "read" && c.F("primary") == "a" {
			reads = append(reads, c)
		}
	}
	require.Len(t, reads, need, "every read a needs is dealt by the tick, no ask run")
	for _, c := range reads {
		assert.Equal(t, "1", c.F(sprint.FieldReadCard), "%s is a read card", c.ID)
		assert.NotEqual(t, row, c.Row, "never read by its own worker")
	}
	for _, c := range s.Readers.Column(sprint.Asked) {
		assert.NotEqual(t, "a", c.F("primary"), "the readers table is asked nothing")
	}
}

// warnings is every warning note of the type the sprint wrote in its epoch, in order:
// happened notes addressed to the coordinator (the hook pushes each).
func (r *alarmRig) warnings(typ string) []sprint.Note {
	r.t.Helper()
	notes, _, err := r.m.NotesSince(r.ctx, "", 100000)
	require.NoError(r.t, err)
	var out []sprint.Note
	for _, n := range notes {
		if n.Type == typ {
			require.Equal(r.t, sprint.Happened, n.Kind, "a warning is a note, never a judgment that holds a card")
			require.Equal(r.t, "coordinator", n.To, "a warning is the coordinator's: the hook pushes it")
			require.True(r.t, strings.HasPrefix(n.What, sprint.WarningWord), n.What)
			out = append(out, n)
		}
	}
	return out
}

// running starts the rig's machine: the deadlines part, which writes the warnings, runs
// in a running tick.
func (r *alarmRig) running() {
	r.t.Helper()
	_, _, _, err := r.st.SetMachine(r.ctx, true)
	require.NoError(r.t, err)
}

// A set that moves a design setting off its working value is warned of by the set itself,
// once, as a note to the coordinator naming it and the exact set that restores it (an alarm
// cannot be off: a threshold away from its working one is the move here); a second setting off is a
// new warning naming both; back at the working values nothing is said, and the next off is
// warned of again (off, on, off is two warnings), between two ticks too.
func TestEveryChangeOfTheDesignSettingsToOffIsWarnedOf(t *testing.T) {
	t.Parallel()
	r := newAlarmRig(t)
	r.set(sprint.SetReq{AlarmReview: "7"})
	w := r.warnings(sprint.NSettingsOff)
	require.Len(t, w, 1, "set --alarm-review 7 is warned of by the set itself, the machine stopped")
	assert.Contains(t, w[0].What, "--alarm-review is 7, working value 40 (restore: nova-sprint set --alarm-review 40)")
	r.set(sprint.SetReq{AlarmReview: "default"})
	r.running()
	r.ticks(2)
	require.Len(t, r.warnings(sprint.NSettingsOff), 1, "back at the working values, the tick says nothing")

	r.set(sprint.SetReq{AlarmReview: "7"})
	require.Len(t, r.warnings(sprint.NSettingsOff), 2, "set --alarm-review 7 is warned of at once")
	r.ticks(3)
	require.Len(t, r.warnings(sprint.NSettingsOff), 2, "and once: the ticks after say nothing again")

	r.set(sprint.SetReq{ReadCards: "off"})
	r.ticks(2)
	w = r.warnings(sprint.NSettingsOff)
	require.Len(t, w, 3, "a second setting off is a new warning")
	assert.Contains(t, w[2].What, "--read-cards is off, working value on (restore: nova-sprint set --read-cards on)")
	assert.Contains(t, w[2].What, "run: nova-sprint set --read-cards on --alarm-review 40")

	r.set(sprint.SetReq{ReadCards: "on", AlarmReview: "default"})
	r.ticks(2)
	require.Len(t, r.warnings(sprint.NSettingsOff), 3, "back at the working values, nothing is said")
	r.set(sprint.SetReq{AlarmReview: "7"})
	r.ticks(2)
	require.Len(t, r.warnings(sprint.NSettingsOff), 4, "off, on, off: the second off is warned of again")

	// off, on, off with no tick between: each set that leaves a setting off says so
	r.set(sprint.SetReq{AlarmReview: "default"})
	r.set(sprint.SetReq{AlarmReview: "7"})
	r.ticks(1)
	require.Len(t, r.warnings(sprint.NSettingsOff), 5, "off, on, off between two ticks is warned of again")
	r.set(sprint.SetReq{AlarmMerging: "20"})
	require.Len(t, r.warnings(sprint.NSettingsOff), 6, "any set of a design setting that leaves one off says so again")

	// a clear, and the same off in the next epoch: TestTheHookIsPushedEveryWarning
	// (cmd/nova-sprint) drives it through the verbs
}

// With read cards off, every card that enters review is warned of by the tick, the ones new
// at a tick in one note naming only them; a card still in review is never warned of twice; a
// card that leaves review and comes back is warned of again. With read cards on, none.
func TestEveryCardEnteringReviewWithReadCardsOffIsWarnedOf(t *testing.T) {
	t.Parallel()
	for _, on := range []bool{false, true} {
		r := newAlarmRig(t)
		if !on {
			r.set(sprint.SetReq{ReadCards: "off"})
		}
		r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 4}))
		r.must(store.AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"a", "b"}}))
		r.running()
		r.ticks(2)
		r.must(store.TakeStep(sprint.TakeReq{As: "m1", Sel: sprint.Sel{Limit: 2}, Who: "m1"}))
		r.finish("a")
		r.ticks(2)
		if on {
			assert.Empty(t, r.warnings(sprint.NReviewOffReadCards), "read cards on: no warning")
			continue
		}
		w := r.warnings(sprint.NReviewOffReadCards)
		require.Len(t, w, 1, "a entered review with read cards off")
		assert.Equal(t, []string{"a"}, w[0].Primaries)
		assert.Contains(t, w[0].What, "a@1 entered review")
		assert.Contains(t, w[0].What, "run: nova-sprint set --read-cards on")

		r.finish("b")
		r.ticks(2)
		w = r.warnings(sprint.NReviewOffReadCards)
		require.Len(t, w, 2, "b entering review is warned of too, a not again")
		assert.Equal(t, []string{"b"}, w[1].Primaries)
		assert.Contains(t, w[1].What, "b@1 entered review (1 more already in review, warned of before)")

		// a leaves review (dropped back to ready by a rework) and comes back: warned again
		r.must(store.ReworkStep(sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{"a"}}, Fix: "again", Who: "coordinator"}))
		r.ticks(3)
		r.must(store.TakeStep(sprint.TakeReq{As: "m1", Sel: sprint.Sel{Limit: 1}, Who: "m1"}))
		r.finish("a")
		r.ticks(2)
		w = r.warnings(sprint.NReviewOffReadCards)
		require.Len(t, w, 3, "a back in review at its next attempt is warned of again")
		assert.Contains(t, w[2].What, "a@2 entered review")

		// a clear, and the same card id in review in the next epoch:
		// TestTheHookIsPushedEveryWarning (cmd/nova-sprint) drives it
	}
}
