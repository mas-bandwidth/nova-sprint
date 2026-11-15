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

// planningTick is the tick Tick builds on s's view, its limits read, with
// the cards in claimed already claimed by an earlier step of the tick.
func planningTick(s *sim, claimed ...string) *tick {
	s.fold()
	tk := newTick(s.v, s.inputs())
	tk.o = newOracle(s.ask.Ask(tk.questions()), Engine)
	tk.lim, tk.tmo = tk.o.limits(), tk.o.timeouts()
	for _, id := range claimed {
		tk.claimed[id] = struct{}{}
	}
	return tk
}

// TestRetireReturnsWhatItCanThenRetiresOnALaterTick: a tick that cannot
// return the whole hand of a row nova-config no longer names (p1.w1 claimed
// by an earlier step) withdraws the rest and leaves the row unretired, since
// the store refuses a retire while the row holds a card; the next tick,
// with nothing claimed, returns the rest and retires the row
// (tla/RowAlive.tla RetireDue, Sweep).
func TestRetireReturnsWhatItCanThenRetiresOnALaterTick(t *testing.T) {
	t.Parallel()
	s := retireSim(t)
	delete(s.roster.Workers, "r1")
	tk := planningTick(s, "p1.w1")
	tk.retire("r1")
	require.Len(t, tk.groups, 1, "one withdraw, no retire: %+v", tk.groups)
	assert.Equal(t, "withdraw", tk.groups[0].kind)
	assert.Equal(t, "p2.w1", tk.groups[0].about)
	s.tick()
	require.Empty(t, s.rejected)
	_, shown := s.snap().Rows["r1"]
	assert.False(t, shown, "retired by the tick after")
	for _, id := range []string{"p1.w1", "p2.w1"} {
		s.requireState(id, "fail")
	}
}

// TestRetireVerbRefusesAHandItCannotReturn: the retire verb returns the
// whole hand in its op or refuses: with p1.w1 claimed it names the card it
// cannot return and the next tick.
func TestRetireVerbRefusesAHandItCannotReturn(t *testing.T) {
	t.Parallel()
	s := retireSim(t)
	tk := planningTick(s, "p1.w1")
	moves, ref := tk.requestMoves(Request{Actor: "rowan", Row: "r1", Event: card.Retire{Reason: RetiredReason}})
	require.NotNil(t, ref, "planned %d moves", len(moves))
	assert.Equal(t, "row r1 holds a card this op cannot return to the queue; the next tick returns it, then retire again", ref.Reason)
}

// TestRetireRefusedWhenAHookLandsFirst: a hook that lands between the
// tick's read and its commit (nova-config named the row again and its
// worker hooked in) moves the row's gen, so the retire planned on the old
// view is refused by its RowGen expect and the row and its hand stay put;
// the next tick reads the hook and, nova-config naming the row, retires
// nothing.
func TestRetireRefusedWhenAHookLandsFirst(t *testing.T) {
	t.Parallel()
	s := retireSim(t)
	r1 := s.roster.Workers["r1"]
	delete(s.roster.Workers, "r1")
	s.fold()
	s.must("hook", store.PutRow{Name: "r1", Session: "hook-r1-again", Role: "worker", Replaces: "hook-r1"})
	s.planTick() // on the view and roster read before the hook
	require.Len(t, s.rejected, 1, "the retire planned before the hook must be refused")
	assert.Contains(t, s.rejected[0].Reason, "row r1 is at gen", "refused by the RowGen expect")
	_, shown := s.snap().Rows["r1"]
	assert.True(t, shown, "r1 stays")
	for _, id := range []string{"p1.w1", "p2.w1"} {
		assert.Equal(t, "r1", s.card(id).Holder, id)
		s.requireState(id, "working")
	}
	s.roster.Workers["r1"] = r1
	s.tick()
	_, shown = s.snap().Rows["r1"]
	assert.True(t, shown, "nova-config names r1: nothing retired")
}

// TestTickRetiresAHeldUnhookedRow: the live space3 (2026-11-15): up, held,
// unhooked, an empty hand, and nova-config no longer names it. The hold is
// the coordinator's "deal it nothing", which no longer matters once the
// row is out of the fleet: the tick retires it like any other.
func TestTickRetiresAHeldUnhookedRow(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 2, "pro")
	s.must("hold", store.SetRowHeld{Row: "r2", Held: true, Reason: "measurement machine; held"})
	s.must("unhook", store.PutRow{Name: "r2"})
	s.run(2)
	require.Equal(t, store.Row{Name: "r2", State: "up", Held: true, Reason: "measurement machine; held", Gen: s.snap().Rows["r2"].Gen}, s.snap().Rows["r2"])
	delete(s.roster.Workers, "r2")
	s.tick()
	require.Empty(t, s.rejected)
	_, shown := s.snap().Rows["r2"]
	assert.False(t, shown, "the held row is retired")
	assert.Empty(t, s.openJudgements("roster"))
}
