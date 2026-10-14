package store

import (
	"errors"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The set-defaults owed flag (the design decided 2026-10-13 for #190): a clear's epoch
// advance writes, in the same store write, that the new epoch is owed its step
// (restoreMembers, then the working defaults); the flag is cleared only after both have
// run. A clear run again over a flagged epoch finishes the step and advances nothing; an
// epoch without the flag (an older binary's clear, a fresh init, epoch 0) is never owed
// (tla/ClearDefaults.tla FinishOnlyWhatIsOwed, MembersRestored, CarrySurvives). The first
// four tests are the cold read's findings 1 to 3 and a crash inside restoreMembers: each
// was red at 14caa8b3, which inferred "owed" from a missing defaults_set marker.

// epochOf is the sprint's epoch as the store holds it.
func epochOf(t *testing.T, h *harness) uint64 {
	t.Helper()
	es, err := h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	return es.N
}

// workProp is the current epoch's work-table property, "" when absent.
func workProp(h *harness, name string) string {
	v, _ := h.snap().Work.Prop(name)
	return v
}

// olderBinary is a backend that can take back what only this binary's advance writes,
// so a test's advance is the one an older binary's clear made (its advance writes no
// set-defaults owed flag).
type olderBinary interface{ dropDefaultsOwed() }

// advanceUntilRestore advances the epoch as Clear does and stops where Clear stops before
// restoreMembers (a crash there): the advance, then the settle of the older restore
// marker. It returns the snapshot of the epoch the advance left.
func advanceUntilRestore(t *testing.T, h *harness) (*sprint.Snapshot, *Store) {
	t.Helper()
	old := h.table()
	pp, err := h.st.pin(h.ctx)
	require.NoError(t, err)
	ok, err := pp.root.AdvanceEpoch(h.ctx, pp.epoch, h.st.now())
	require.NoError(t, err)
	require.True(t, ok)
	next, _, err := h.st.repinOnly(h.ctx)
	require.NoError(t, err)
	require.NoError(t, next.root.SettleEpoch(h.ctx, next.epoch))
	return old, next
}

// olderBinaryClear is a clear as an older binary (v1.2.16) made it: the advance, the
// settle and restoreMembers, and no set-defaults step and no owed flag.
func olderBinaryClear(t *testing.T, h *harness) {
	t.Helper()
	old, next := advanceUntilRestore(t, h)
	if o, ok := any(h.m).(olderBinary); ok {
		o.dropDefaultsOwed()
	}
	require.NoError(t, next.restoreMembers(h.ctx, old))
}

// upMembers brings two members up, one of them wider than the default, so the shape a
// clear carries has members with control cards.
func upMembers(h *harness) {
	h.t.Helper()
	h.m.SetMachineWidth("m1", strconv.Itoa(3))
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1"}))
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m2"}))
}

// membersOf is the epoch's members with each control card's status and width, and its
// reader rows.
func membersOf(h *harness) (map[string]string, []string) {
	s := h.snap()
	out := map[string]string{}
	for _, m := range s.Members() {
		if c := s.Fleet.Card(sprint.CtlID(m)); c != nil {
			out[m] = c.F("status") + " width=" + c.F(sprint.FieldWidth)
		} else {
			out[m] = "no control card"
		}
	}
	return out, append([]string(nil), s.Readers.Rows()...)
}

// Finding 1: an older binary's clear between two of this binary's clears. The newest
// epoch holds no flag, so the next clear advances as usual; it never "finishes" a step
// nothing owes and reports success with nothing cleared.
func TestAClearAfterAnOlderBinarysClearAdvances(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.must(SetStep(sprint.SetReq{Who: "tester", AlarmReview: "3"}))
	_, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), epochOf(t, h))
	olderBinaryClear(t, h)
	require.Equal(t, uint64(2), epochOf(t, h), "the older binary's clear advanced")

	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.False(t, res.Owed, "epoch 2 holds no flag: nothing is owed")
	assert.Equal(t, uint64(2), res.From)
	assert.Equal(t, uint64(3), res.To, "the clear advanced")
	assert.Equal(t, uint64(3), epochOf(t, h))
	assert.Equal(t, sprint.AlarmReviewWorking, workProp(h, sprint.PropAlarmReview), "epoch 2 held no threshold: the working one")
	assert.Equal(t, sprint.ReadCardsOnWord, workProp(h, sprint.PropReadCards))
}

// Finding 2: a clear that crashed after its advance and before restoreMembers. The clear
// run again restores the members and the readers, then sets the defaults, and advances
// nothing; the readers never drop to none.
func TestAClearRunAgainAfterACrashBeforeRestoreMembersRestoresThem(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	upMembers(h)
	_, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	members, readers := membersOf(h)
	require.NotEmpty(t, members)
	require.Len(t, readers, 5) // reader-a, reader-b, reader-c and the member readers reader-m1, reader-m2 a clear makes

	advanceUntilRestore(t, h)
	require.Equal(t, uint64(2), epochOf(t, h))

	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.True(t, res.Owed, "epoch 2 was owed its step")
	assert.Equal(t, uint64(2), res.To, "no second advance")
	assert.Equal(t, uint64(2), epochOf(t, h))
	gotMembers, gotReaders := membersOf(h)
	assert.Equal(t, members, gotMembers, "the members and their status are restored")
	assert.ElementsMatch(t, readers, gotReaders, "the readers are restored")
	assert.Equal(t, sprint.ReadCardsOnWord, workProp(h, sprint.PropReadCards), "and the defaults set")
}

// Finding 3: the first clear on a store crashes between its advance and its set-defaults
// step. The clear run again finishes that step over epoch 1, carrying the coordinator's
// settings from epoch 0, and does not advance again from the empty epoch.
func TestAFirstClearCrashedBeforeItsDefaultsIsFinishedNotAdvanced(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.must(SetStep(sprint.SetReq{Who: "tester", AlarmReview: "3", AlarmFleet: "70", ReadCards: sprint.SwitchOff}))
	writeProps(h, h.st, map[string]string{sprint.PropSprintBase: "sprint/v9"}, "carry")
	h.st.beforeDefaults = func(*Store) error { return errors.New("crashed before the set-defaults step") }
	_, err := h.st.Clear(h.ctx)
	require.Error(t, err)
	h.st.beforeDefaults = nil
	require.Equal(t, uint64(1), epochOf(t, h))

	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.True(t, res.Owed, "epoch 1 was owed its step")
	assert.Equal(t, uint64(0), res.From, "carried from epoch 0, the epoch the advance left")
	assert.Equal(t, uint64(1), res.To)
	assert.Equal(t, uint64(1), epochOf(t, h), "no second advance")
	assert.Equal(t, "3", workProp(h, sprint.PropAlarmReview), "the coordinator's threshold is carried")
	assert.Equal(t, "70", workProp(h, sprint.PropAlarmFleet))
	assert.Equal(t, "sprint/v9", workProp(h, sprint.PropSprintBase), "the base is carried")
	assert.Equal(t, sprint.ReadCardsOnWord, workProp(h, sprint.PropReadCards), "a plain setting takes its default")

	// the next clear advances as usual and carries again
	res, err = h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.False(t, res.Owed)
	assert.Equal(t, uint64(2), res.To)
	assert.Equal(t, "3", workProp(h, sprint.PropAlarmReview))
}

// A crash inside restoreMembers: the rows are written, the members' control cards are
// not. The clear run again finishes restoreMembers (the control cards, with the status
// the old epoch held) and the defaults, over the same epoch.
func TestAClearCrashedInsideRestoreMembersIsFinishedByTheRerun(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	upMembers(h)
	members, readers := membersOf(h)
	require.NotEmpty(t, members)
	var advanced, crashed atomic.Bool
	fleet := "apply " + h.st.Names.Table(sprint.Fleet) + " before"
	h.m.Fail = func(p string) error {
		if p == "advance" {
			advanced.Store(true)
		}
		// the store stays down from the members' first write on, as a crashed clear does:
		// every retry fails too
		if advanced.Load() && p == fleet {
			crashed.Store(true)
			return errors.New("crashed inside restoreMembers")
		}
		return nil
	}
	_, err := h.st.Clear(h.ctx)
	require.Error(t, err)
	require.True(t, crashed.Load(), "the crash was inside restoreMembers")
	h.m.Fail = nil
	require.Equal(t, uint64(1), epochOf(t, h))

	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.True(t, res.Owed, "epoch 1 was owed its step")
	assert.Equal(t, uint64(1), epochOf(t, h), "no second advance")
	gotMembers, gotReaders := membersOf(h)
	assert.Equal(t, members, gotMembers, "every member has its control card and its status")
	// the rerun's clear makes every up member's reader row too (reader-m1, reader-m2)
	assert.ElementsMatch(t, append(append([]string(nil), readers...), "reader-m1", "reader-m2"), gotReaders)
	assert.Equal(t, sprint.AlarmReadyWorking, workProp(h, sprint.PropAlarmReady), "the defaults are set")
	assert.Equal(t, sprint.ReadCardsOnWord, workProp(h, sprint.PropReadCards))
}

// The flag's life: epoch 0 and a fresh init owe nothing; the advance writes the flag with
// the epoch (it is set before restoreMembers runs); the step clears it once both parts ran.
func TestTheAdvanceFlagsItsEpochAndTheStepClearsIt(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	es, err := h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	assert.False(t, es.DefaultsOwed, "epoch 0 is never owed")
	seen := false
	h.st.beforeDefaults = func(next *Store) error {
		es, err := next.EpochNow(h.ctx)
		require.NoError(t, err)
		seen = es.DefaultsOwed
		return nil
	}
	_, err = h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.True(t, seen, "inside the step the epoch is flagged owed")
	es, err = h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), es.N)
	assert.False(t, es.DefaultsOwed, "the step cleared the flag")

	// the flag survives a snapshot of the store and its restore
	h.st.beforeDefaults = func(*Store) error { return errors.New("crashed before the set-defaults step") }
	_, err = h.st.Clear(h.ctx)
	require.Error(t, err)
	doc, err := h.m.Snapshot()
	require.NoError(t, err)
	m := NewMem()
	require.NoError(t, m.Restore(doc))
	es, err = m.Epoch(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), es.N)
	assert.True(t, es.DefaultsOwed, "the restored store still owes epoch 2 its step")
}

// A crash after the defaults and before the flag is cleared: the clear run again runs the
// step again (idempotent: a set that landed in between survives) and clears the flag. It
// reports what it finished, and the clear after it advances.
func TestACrashBeforeTheFlagIsClearedIsFinishedAndReported(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	upMembers(h)
	h.m.Fail = func(p string) error {
		if p == "settle-defaults" {
			return errors.New("crashed before the flag was cleared")
		}
		return nil
	}
	_, err := h.st.Clear(h.ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "run: nova-sprint clear again")
	h.m.Fail = nil
	es, err := h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), es.N)
	require.True(t, es.DefaultsOwed, "the flag stays until the step is done")
	require.Equal(t, sprint.ReadCardsOnWord, workProp(h, sprint.PropReadCards), "the defaults were written")
	h.must(SetStep(sprint.SetReq{Who: "tester", ReadCards: sprint.SwitchOff}))

	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.True(t, res.Owed)
	assert.Equal(t, uint64(0), res.From)
	assert.Equal(t, uint64(1), res.To)
	assert.Equal(t, []string{"members:2", "readers:5", "defaults"}, res.OwedFinished, "the clear names what it finished")
	assert.Equal(t, sprint.SwitchOff, workProp(h, sprint.PropReadCards), "the set between survives the step run again")
	es, err = h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), es.N)
	assert.False(t, es.DefaultsOwed)

	res, err = h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.False(t, res.Owed)
	assert.Empty(t, res.OwedFinished)
	assert.Equal(t, uint64(2), res.To)
}
