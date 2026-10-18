package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAFriendRowsEnvFaultIsAnsweredByItsRemedy(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("only friend amy"))
	amy := workerSeatFixture(FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash,pro"})
	dealStarted(w, amy)
	row := FriendRow("amy")
	w.s.Fleet.Put(&Card{ID: CtlID(row), Row: row, Col: Ctl, Fields: map[string]string{"kind": "member", "status": Up, FieldWidth: "2"}})
	wc := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, wc)
	w.must(Finish(w.s, FinishReq{As: row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Failed: true, Report: dPushRefused}))

	cause, reason := w.s.MemberEnvFault(row)
	require.Equal(t, EnvPush, cause)
	require.Contains(t, reason, "could not read Username")
	open := w.openOn(MemberSubject(row))
	require.Len(t, open, 1)
	assert.Equal(t, []string{"fleet up " + row + " --probe " + EnvProbePassed, "wait"}, open[0].Note.Decisions)

	width := w.s.Fleet.Card(CtlID(row)).F(FieldWidth)
	status := w.s.Fleet.Card(CtlID(row)).F("status")
	p := FleetStep(w.s, FleetReq{Op: "up", Member: row, Probe: EnvProbePassed})
	require.Empty(t, p.Refused, "%+v", p.Refused)
	w.must(p)
	assert.Empty(t, w.s.MemberCtl(row).F(FieldEnvFault))
	assert.Empty(t, w.s.MemberCtl(row).F(FieldEnvFaultReason))
	assert.Empty(t, w.s.MemberCtl(row).F(FieldEnvFaultAt))
	assert.False(t, w.s.EnvHeld(row), "the environment hold clears with its fault")
	assert.Empty(t, w.openOn(MemberSubject(row)), "the judgment closes with its fault")
	assert.Equal(t, width, w.s.MemberCtl(row).F(FieldWidth), "friend width belongs to nova-hook")
	assert.Equal(t, status, w.s.MemberCtl(row).F("status"), "friend status belongs to nova-hook")
}

func TestFleetUpOfAFriendRowWithNoFaultIsRefusedByName(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	row := FriendRow("amy")
	w.s.Fleet.Put(&Card{ID: CtlID(row), Row: row, Col: Ctl, Fields: map[string]string{"kind": "member", "status": Up, FieldWidth: "2"}})
	p := FleetStep(w.s, FleetReq{Op: "up", Member: row, Probe: EnvProbePassed})
	require.Len(t, p.Refused, 1)
	assert.Equal(t, "a friend row is not a fleet member; fleet up "+row+" --probe passed clears only its environment fault", p.Refused[0].Why)
}
