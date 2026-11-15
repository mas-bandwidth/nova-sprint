package steps

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// retireSim is two bench rows serving pro, r1 holding p1.w1 (taken) and
// p2.w1 (dealt, not taken), and r2 open.
func retireSim(t *testing.T) *sim {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.producer("p2", "s1", "pro", "a/y")
	s.run(2)
	s.take("p1.w1")
	s.requireState("p2.w1", "working")
	require.Equal(t, "r1", s.card("p1.w1").Holder)
	require.Equal(t, "r1", s.card("p2.w1").Holder)
	s.row("r2", 2, "pro")
	return s
}

// TestTickRetiresARowNovaConfigNoLongerNames: the tick after nova-config
// stops naming a row retires it (Glenn, 2026-11-15: "the nova-sprint system
// and dash follows the set of machines defined automatically in
// nova-config"; tla/RowAlive.tla Retire): its whole hand, taken and not,
// goes back to the queue released with no blame in the same op, and the row
// is in no read. The cards are dealt again to a row that is open.
func TestTickRetiresARowNovaConfigNoLongerNames(t *testing.T) {
	t.Parallel()
	s := retireSim(t)
	delete(s.roster.Workers, "r1")
	s.tick()
	require.Empty(t, s.rejected)
	_, shown := s.snap().Rows["r1"]
	assert.False(t, shown, "a retired row is in no read")
	for _, id := range []string{"p1.w1", "p2.w1"} {
		s.requireState(id, "fail")
		assert.Equal(t, card.ReleasedPrefix+card.Returned("r1", "retired"), s.card(id).Reason, id)
		assert.Empty(t, s.card(id).Result, "released, no blame: %s", id)
	}
	assert.Empty(t, s.openJudgements("roster"), "a retired row is not a roster judgement")
	s.run(2)
	for _, p := range []string{"p1", "p2"} {
		s.requireState(p, "working")
	}
	for id, c := range s.snap().Cards {
		if c.Kind == "work" && c.State == "working" {
			assert.Equal(t, "r2", c.Holder, "%s dealt again to the open row", id)
		}
	}
	s.row("r1", 2, "pro") // nova-config names it again and it hooks in: a new row
	assert.Equal(t, "up", s.snap().Rows["r1"].State)
}

// TestTickRetiresNothingOnAnEmptyRoster: a roster with no worker and no kin
// is a fault, never a fleet with no rows (a failed read plans no tick at all:
// run RosterUnread), so nothing is retired and no hand moves.
func TestTickRetiresNothingOnAnEmptyRoster(t *testing.T) {
	t.Parallel()
	s := retireSim(t)
	s.roster.Workers, s.roster.Kin = map[string]roster.Worker{}, map[string]roster.Kin{}
	s.tick()
	for _, n := range []string{"r1", "r2"} {
		_, shown := s.snap().Rows[n]
		assert.True(t, shown, "%s kept", n)
	}
	s.requireState("p1.w1", "working")
	assert.Equal(t, "r1", s.card("p1.w1").Holder)
}

// TestTickKeepsRowsNovaConfigNamesOutsideTheRoster: a row nova-config lists
// that is not a roster row (a machine that is not a bench, a disabled friend)
// and a row hooked as the coordinator are never retired: each keeps the
// roster judgement, dealt nothing.
func TestTickKeepsRowsNovaConfigNamesOutsideTheRoster(t *testing.T) {
	t.Parallel()
	s := retireSim(t)
	s.must("hooks", store.PutRow{Name: "studio", Session: "hook-studio", Role: "worker"}, store.PutRow{Name: "carol", Session: "hook-carol", Role: "coordinator"})
	s.roster.Listed = map[string]bool{"studio": true}
	s.tick()
	require.Empty(t, s.rejected)
	for _, n := range []string{"studio", "carol"} {
		_, shown := s.snap().Rows[n]
		assert.True(t, shown, "%s kept", n)
	}
	assert.ElementsMatch(t, []string{"roster.carol.1", "roster.studio.1"}, s.openJudgements("roster"))
}

// TestRetireVerbReturnsTheHandInItsOp: the coordinator's retire returns the
// row's whole hand and retires it in one op; a row that does not exist, and
// one already retired (in no read), are refused.
func TestRetireVerbReturnsTheHandInItsOp(t *testing.T) {
	t.Parallel()
	s := retireSim(t)
	rc, ref := s.verb(Request{Actor: "rowan", Row: "r1", Event: card.Retire{Reason: RetiredReason}})
	require.Nil(t, ref)
	require.True(t, rc.Accepted, rc.Reason)
	_, shown := s.snap().Rows["r1"]
	assert.False(t, shown)
	for _, id := range []string{"p1.w1", "p2.w1"} {
		s.requireState(id, "fail")
	}
	_, ref = s.verb(Request{Actor: "rowan", Row: "r1", Event: card.Retire{Reason: RetiredReason}})
	require.NotNil(t, ref)
	assert.Equal(t, "row r1 does not exist", ref.Reason)
	_, ref = s.verb(Request{Actor: "bob", Row: "r2", Event: card.Retire{Reason: RetiredReason}})
	require.NotNil(t, ref, "retire is the coordinator's")
}

// TestRetirableJudgesTheRoster: Retirable is false on an empty roster, for a
// row nova-config names in any way, and for a hooked coordinator.
func TestRetirableJudgesTheRoster(t *testing.T) {
	t.Parallel()
	ro := roster.Roster{Workers: map[string]roster.Worker{"b": {Name: "b"}}, Kin: map[string]roster.Kin{"k": {Name: "k"}},
		Refused: []roster.Refused{{Name: "x"}}, Listed: map[string]bool{"m": true}}
	for name, want := range map[string]bool{"b": false, "k": false, "x": false, "m": false, "gone": true} {
		assert.Equal(t, want, Retirable(ro, store.Row{Name: name, Role: "worker"}), name)
	}
	assert.False(t, Retirable(ro, store.Row{Name: "gone", Role: "coordinator"}), "a hooked coordinator")
	assert.False(t, Retirable(roster.Roster{}, store.Row{Name: "gone"}), "an empty roster")
}
