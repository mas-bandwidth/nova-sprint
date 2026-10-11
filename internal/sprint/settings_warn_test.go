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

// A fresh store, and a store after a clear whose settings were all moved off, has every
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

	r.set(sprint.SetReq{ReadCards: "off", AlarmReview: "off", AlarmReady: "off", AlarmMerging: "off", AlarmFleet: "off"})
	for _, d := range sprint.DesignSettings(r.snap()) {
		assert.Equal(t, "off", d.Value, "%s set off", d.Flag)
		assert.False(t, d.At, "%s set off", d.Flag)
	}
	_, err := r.st.Clear(r.ctx)
	require.NoError(t, err)
	requireWorkingDefaults(t, r.snap(), "after a clear")

	// default restores an alarm's working threshold, as a number does
	r.set(sprint.SetReq{AlarmReview: "7"})
	assert.Contains(t, sprint.SettingsOffWhat(sprint.DesignSettings(r.snap())), "--alarm-review is 7, working default 40 (restore: nova-sprint set --alarm-review 40)")
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

// A set that turns a design setting off is what the hook's warning says (SettingsOffWhat;
// cmd/nova-sprint/hook.go pushes it): each setting off with its working default and the set
// that restores it, and one set that restores them all; restored, there is nothing to say.
func TestASetThatTurnsADesignSettingOffIsWhatTheWarningSays(t *testing.T) {
	t.Parallel()
	r := newAlarmRig(t)
	require.Empty(t, sprint.SettingsOffWhat(sprint.DesignSettings(r.snap())), "nothing off, nothing to warn of")

	r.set(sprint.SetReq{AlarmReview: "off"})
	what := sprint.SettingsOffWhat(sprint.DesignSettings(r.snap()))
	assert.True(t, strings.HasPrefix(what, sprint.WarningWord), what)
	assert.Contains(t, what, "--alarm-review is off, working default 40 (restore: nova-sprint set --alarm-review 40)")

	r.set(sprint.SetReq{ReadCards: "off"})
	what = sprint.SettingsOffWhat(sprint.DesignSettings(r.snap()))
	assert.Contains(t, what, "--read-cards is off, working default on (restore: nova-sprint set --read-cards on)")
	assert.Contains(t, what, "--alarm-review is off")
	assert.Contains(t, what, "run: nova-sprint set --read-cards on --alarm-review 40")

	r.set(sprint.SetReq{ReadCards: "on", AlarmReview: "default"})
	assert.Empty(t, sprint.SettingsOffWhat(sprint.DesignSettings(r.snap())), "restored, nothing to warn of")
}

// With read cards off, every primary in review is one the hook warns of, by its card and
// attempt (ReviewOffReadCards), so a card that enters review is a new warning; with read
// cards on, none.
func TestEveryCardInReviewWithReadCardsOffIsOneToWarnOf(t *testing.T) {
	t.Parallel()
	for _, on := range []bool{false, true} {
		r := newAlarmRig(t)
		if !on {
			r.set(sprint.SetReq{ReadCards: "off"})
		}
		r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 4}))
		r.must(store.AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"a", "b"}}))
		_, _, _, err := r.st.SetMachine(r.ctx, true)
		require.NoError(t, err)
		r.ticks(2)
		r.must(store.TakeStep(sprint.TakeReq{As: "m1", Sel: sprint.Sel{Limit: 2}, Who: "m1"}))
		r.finish("a")
		r.ticks(1)
		if on {
			assert.Empty(t, sprint.ReviewOffReadCards(r.snap()), "read cards on: nothing to warn of")
			continue
		}
		require.Equal(t, []string{"a@1"}, sprint.ReviewOffReadCards(r.snap()), "a entered review with read cards off")
		r.finish("b")
		r.ticks(1)
		cards := sprint.ReviewOffReadCards(r.snap())
		assert.ElementsMatch(t, []string{"a@1", "b@1"}, cards, "b entering review is one more")
		what := sprint.ReviewOffReadCardsWhat([]string{"b@1"})
		assert.True(t, strings.HasPrefix(what, sprint.WarningWord), what)
		assert.Contains(t, what, "b@1 entered review")
		assert.Contains(t, what, "run: nova-sprint set --read-cards on")
	}
}
