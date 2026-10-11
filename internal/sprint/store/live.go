package store

import (
	"context"
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// LiveReturnStep is the reconcile of a STOPPED machine (sprint.LiveReturns; tla/LiveRuns.tla
// Tick): the STOPPED tick's one step, which returns each working card its row's live set has
// not named for sprint.LiveGrace, with the STOP's receipt. A STOP debt does not refuse it
// (engine.go: it is the owner's receipt, read off the owner's beats), and it starts and
// reports no work. The beats are the tick's own read.
func LiveReturnStep(beats map[string]sprint.Beat) Step {
	return Step{Verb: sprint.LiveReturnVerb, Actor: sprint.MachineActor, Load: tables(sprint.Fleet), Mirrors: true,
		Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.LiveReturns(s, beats, true) }}
}

// liveStopped is a STOPPED tick's reconcile: a STOPPED machine takes and lands nothing new,
// and still makes working what runs. Nothing is read or written while no beat carries a
// live set (workers from before v1.2.6).
func (st *Store) liveStopped(ctx context.Context, beats map[string]sprint.Beat, res *TickResult) error {
	known := false
	for _, b := range beats {
		known = known || b.LiveKnown
	}
	if !known {
		return nil
	}
	r, err := st.Run(ctx, LiveReturnStep(beats))
	if err != nil {
		return fmt.Errorf("live: %w", err)
	}
	if len(r.Moved) > 0 || len(r.Refused) > 0 {
		res.Parts = append(res.Parts, PartResult{Name: sprint.PartLive, Result: r})
	}
	return nil
}

// stopOwned says a STOP debt entry still owns its card (tla/LiveRuns.tla Owned): the card
// does not carry the entry's return receipt, stopped_from_gen at the generation the STOP
// captured with a later generation, which a stop-return (or the reconcile's return of a
// STOPPED machine) writes as it moves the card working -> ready. Only a return moves an
// owned card (stopDebtMutation refuses every other step that would), so in the model a
// card owned is a card still working at that generation (StopMarkerIsWorking,
// OwnedLeavesWithReceipt); the receipt is the code's reading of it, and it survives a later
// move of the returned card (a hold, a give), which before v1.2.6 froze it: the debt matched
// by id until start, and hold refused its whole row (2026-10-10). A card the step did not
// read, or out of the table, is owned: the safe side, as before.
func stopOwned(s *sprint.Snapshot, d StopLease) bool {
	if s == nil {
		return true
	}
	t := s.Fleet
	if d.Table == sprint.Readers {
		t = s.Readers
	}
	if t == nil {
		return true
	}
	c := t.Card(d.ID)
	if c == nil || !c.Placed() {
		return true
	}
	return !(c.Int("stopped_from_gen") == d.Gen && c.Int("gen") > d.Gen)
}

// owedDebt is the debt entries that still own their cards.
func owedDebt(s *sprint.Snapshot, debt []StopLease) []StopLease {
	var owed []StopLease
	for _, d := range debt {
		if stopOwned(s, d) {
			owed = append(owed, d)
		}
	}
	return owed
}

// FleetBeats is every fleet row's beat as the tick reads them: each member's by its name,
// each friend's by hers (sprint.RowBeat maps a row to its beat). Where counts the working
// cells against them (sprint.LiveCountKeys).
func (st *Store) FleetBeats(ctx context.Context) (map[string]sprint.Beat, error) {
	_, beats, err := st.fleetBeats(ctx, nil)
	return beats, err
}
