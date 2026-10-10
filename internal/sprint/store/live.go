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
// reports no work. The beats are the tick's own read. Its return settles the STOP debt of
// each card it returned, as a stop-return does (engine.go after, settleStopDebt;
// tla/LiveRuns.tla Settle), so clear, hold and fleet down are not refused before START.
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

// FleetBeats is every fleet row's beat as the tick reads them: each member's by its name,
// each friend's by hers (sprint.RowBeat maps a row to its beat). Where counts the working
// cells against them (sprint.LiveCountKeys).
func (st *Store) FleetBeats(ctx context.Context) (map[string]sprint.Beat, error) {
	_, beats, err := st.fleetBeats(ctx, nil)
	return beats, err
}
