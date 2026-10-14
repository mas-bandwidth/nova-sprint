package store

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The owed step restores the members and the readers at most once (the cold read of #190
// at 02af0680, 2026-10-13): the restore writes its marker in the same store write as the
// members' control cards, and a clear run again after it never restores again, so a
// reader's tiers set or a row removed in the new epoch after a crash stay as the epoch left
// them (tla/ClearDefaults.tla RestoreDone, RerunKeepsEpochWrites). The scenarios run on the
// harness given, Mem here and Redis in the functional tier
// (clear_restore_once_functional_test.go).

// readerTiersOf is the reader's tiers cell in the current epoch, "" when none.
func readerTiersOf(h *harness, reader string) string {
	s := h.snap()
	if s.Readers == nil || s.Readers.Texts == nil {
		return ""
	}
	return s.Readers.Texts[reader][sprint.ReaderTiers]
}

// setTiersRaw writes a reader's tiers cell at the current epoch through the backend, as
// the setup of an epoch before the clear.
func setTiersRaw(h *harness, reader, tiers string) {
	h.t.Helper()
	pinned, err := h.st.pin(h.ctx)
	require.NoError(h.t, err)
	require.NoError(h.t, pinned.EnsureReaderTiers(h.ctx))
	require.NoError(h.t, pinned.B.RowSet(h.ctx, pinned.Names.Table(sprint.Readers), reader, map[string]string{sprint.ReaderTiers: tiers}))
}

// tierSetter and readerRemover are the store's verbs for a reader's tiers and a reader's
// removal; a store without them is edited through the backend, as the verbs did.
type tierSetter interface {
	SetReaderTiers(ctx context.Context, names []string, tiers string) error
}
type readerRemover interface {
	RemoveReaders(ctx context.Context, names []string) error
}

// editTiers is `set <reader> --tiers <tiers>` in the current epoch.
func editTiers(h *harness, reader, tiers string) error {
	if s, ok := any(h.st).(tierSetter); ok {
		return s.SetReaderTiers(h.ctx, []string{reader}, tiers)
	}
	pinned, err := h.st.pin(h.ctx)
	if err != nil {
		return err
	}
	return pinned.B.RowSet(h.ctx, pinned.Names.Table(sprint.Readers), reader, map[string]string{sprint.ReaderTiers: tiers})
}

// removeReader is `reader remove <reader>` in the current epoch.
func removeReader(h *harness, reader string) error {
	if s, ok := any(h.st).(readerRemover); ok {
		return s.RemoveReaders(h.ctx, []string{reader})
	}
	pinned, err := h.st.pin(h.ctx)
	if err != nil {
		return err
	}
	return pinned.B.RowsDel(h.ctx, pinned.Names.Table(sprint.Readers), []string{reader})
}

// removeMember takes a member out of the epoch as the fleet sync does: its control card
// off the table (the inventory no longer names it), then its row deleted.
func removeMember(h *harness, gone, keep string) error {
	h.t.Helper()
	h.must(FleetStep(sprint.FleetReq{Op: "sync", Who: "tester", Sync: []sprint.SyncMember{{Name: keep}}, Machines: []string{keep}}))
	_, err := h.st.DropMembers(h.ctx, []string{keep})
	return err
}

// crashAfterRestore runs a clear that stops after its restore, before its defaults.
func crashAfterRestore(h *harness) {
	h.t.Helper()
	h.st.beforeDefaults = func(*Store) error { return errors.New("crashed after the restore") }
	_, err := h.st.Clear(h.ctx)
	require.Error(h.t, err)
	h.st.beforeDefaults = nil
}

// scenarioATierSetAfterACrashSurvivesTheRerun is the cold read's sequence: reader-a reads
// pro in the epoch cleared; the clear crashes after the restore (crash); the coordinator
// sets reader-a to heavy in the new epoch; the clear run again leaves heavy.
func scenarioATierSetAfterACrashSurvivesTheRerun(t *testing.T, h *harness, crash func(*harness)) {
	upMembers(h)
	setTiersRaw(h, "reader-a", "pro")
	from := epochOf(t, h)
	crash(h)
	require.Equal(t, from+1, epochOf(t, h))
	require.Equal(t, "pro", readerTiersOf(h, "reader-a"), "the restore carried reader-a's tiers")

	require.NoError(t, editTiers(h, "reader-a", "heavy"), "set reader-a --tiers heavy in the new epoch")
	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.True(t, res.Owed)
	assert.Equal(t, from+1, epochOf(t, h), "no second advance")
	assert.Equal(t, "heavy", readerTiersOf(h, "reader-a"), "the new epoch's tiers are never overwritten by a re-run")
}

// scenarioARowRemovedAfterACrashStaysRemoved: the clear crashes after its restore; a reader
// and a member are removed in the new epoch; the clear run again leaves both removed, the
// member's control card included.
func scenarioARowRemovedAfterACrashStaysRemoved(t *testing.T, h *harness, crash func(*harness)) {
	upMembers(h)
	from := epochOf(t, h)
	crash(h)
	require.Equal(t, from+1, epochOf(t, h))
	before := h.snap()
	require.Contains(t, before.Readers.Rows(), "reader-b")
	require.True(t, before.Fleet.HasRow("m2"))

	require.NoError(t, removeReader(h, "reader-b"))
	require.NoError(t, removeMember(h, "m2", "m1"))
	s := h.snap()
	require.NotContains(t, s.Readers.Rows(), "reader-b")
	require.False(t, s.Fleet.HasRow("m2"))

	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.True(t, res.Owed)
	assert.Equal(t, from+1, epochOf(t, h))
	s = h.snap()
	assert.NotContains(t, s.Readers.Rows(), "reader-b", "a reader removed in the new epoch stays removed")
	assert.False(t, s.Fleet.HasRow("m2"), "a member's row removed in the new epoch stays removed")
	assert.Nil(t, s.MemberCtl("m2"), "and its control card is not recreated")
	assert.True(t, s.Fleet.HasRow("m1"), "the member kept stays")
}

// scenarioAChoiceSetAfterACrashIsKeptNotReportedReset (the cold read's finding 4): read
// cards are off in the epoch cleared; the clear crashes after the restore; the coordinator
// sets read cards off again in the new epoch; the clear run again keeps it and never
// reports "read_cards was off, now on".
func scenarioAChoiceSetAfterACrashIsKeptNotReportedReset(t *testing.T, h *harness, crash func(*harness)) {
	h.must(SetStep(sprint.SetReq{Who: "tester", ReadCards: sprint.SwitchOff, AlarmReady: sprint.SwitchOff}))
	crash(h)
	h.must(SetStep(sprint.SetReq{Who: "tester", ReadCards: sprint.SwitchOff}))
	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, sprint.SwitchOff, workProp(h, sprint.PropReadCards), "the new epoch's choice is kept")
	assert.NotContains(t, res.Defaults, "read_cards was off, now on", "no reset is reported for a setting not written")
	assert.Contains(t, res.Defaults, "alarm_ready was off, now on", "a reset actually written is reported")
}

func TestARerunAfterACrashKeepsATierSetSince(t *testing.T) {
	t.Parallel()
	scenarioATierSetAfterACrashSurvivesTheRerun(t, newHarness(t), crashAfterRestore)
}

// the cold read's exact crash point: after the restore and the defaults, at the flag's clear
func TestARerunAfterACrashAtTheSettleKeepsATierSetSince(t *testing.T) {
	t.Parallel()
	scenarioATierSetAfterACrashSurvivesTheRerun(t, newHarness(t), crashAtSettle)
}

func TestARerunAfterACrashKeepsARowRemovedSince(t *testing.T) {
	t.Parallel()
	scenarioARowRemovedAfterACrashStaysRemoved(t, newHarness(t), crashAfterRestore)
}

func TestARerunAfterACrashAtTheSettleKeepsARowRemovedSince(t *testing.T) {
	t.Parallel()
	scenarioARowRemovedAfterACrashStaysRemoved(t, newHarness(t), crashAtSettle)
}

func TestARerunReportsOnlyTheResetsItWrote(t *testing.T) {
	t.Parallel()
	scenarioAChoiceSetAfterACrashIsKeptNotReportedReset(t, newHarness(t), crashAfterRestore)
}

// crashAtSettle runs a clear the Mem store fails at the flag's clear: after the restore and
// the defaults.
func crashAtSettle(h *harness) {
	h.t.Helper()
	h.m.Fail = func(p string) error {
		if p == "settle-defaults" {
			return errors.New("crashed at the settle")
		}
		return nil
	}
	_, err := h.st.Clear(h.ctx)
	require.Error(h.t, err)
	h.m.Fail = nil
}
