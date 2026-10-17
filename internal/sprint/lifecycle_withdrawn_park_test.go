package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The lifecycle half of the ready park (the cold re-read of PR 471): the
// server-default park of a ready primary into waiting is lawful only for a
// primary with no withdrawn work card; one whose work card was withdrawn is
// held in place, so decisionPark refuses moving it (lifecycle.go decisionPark;
// tla/SprintTables.tla DecisionParkStep, MovesOnlyByTable, Rule2Withdrawn).
func TestDecisionParkingLifecycleRefusesParkingAWithdrawnReadyPrimary(t *testing.T) {
	t.Parallel()
	s := lifecycleCoverPre()
	s.Fleet = NewTable(Fleet)
	s.Fleet.SetRows([]string{"m1"})
	c := s.Work.Card("p2")
	s.Open = []Open{{Key: OpenKey("j-bound", c.ID), Note: Note{ID: "j-bound", Kind: Judgment, Type: NBound, Primaries: []string{c.ID}}}}
	e := moveEntry(c, c.Row, Waiting, map[string]string{FieldHeld: "now", "held_by": ServerDefaultActor, "held_reason": "bound", "held_judgment": "j-bound", "decision_held_from": Ready})
	plan := func() Plan {
		p := Plan{Units: []Unit{{Key: c.ID, Changes: []Change{change(Work, e)}}}}
		p.on(s)
		return Lawful(p)
	}
	require.Empty(t, plan().Refused, "a ready primary with no withdrawn work card is parked in waiting")
	s.Fleet.Put(&Card{ID: "p2.w1", Row: "m1", Col: Withdrawn, Rev: 1, Fields: map[string]string{"primary": "p2", "stream": "s1", "attempt": "1"}})
	require.True(t, withdrawnCard(s, c), "the fixture: p2 holds a withdrawn work card")
	require.NotEmpty(t, plan().Refused, "a ready primary whose work card was withdrawn is never parked in waiting")
}
