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
	return Step{Verb: sprint.LiveReturnVerb, Actor: sprint.MachineActor, Load: tables(sprint.Work, sprint.Fleet), Mirrors: true, Friends: true,
		Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.LiveReturns(s, beats, true) }}
}

// liveStopped is a STOPPED tick's reconcile: a STOPPED machine takes and lands nothing new,
// and still reconciles live runs and explicit nova-hook unhooks. A friend unhook
// needs no work-report beat.
// tla/LiveRuns.tla Tick
func (st *Store) liveStopped(ctx context.Context, beats map[string]sprint.Beat, res *TickResult) error {
	known := false
	for _, b := range beats {
		known = known || b.LiveKnown
	}
	if !known {
		seats, err := st.FriendSeats(ctx, st.now())
		if err != nil {
			return fmt.Errorf("friend hook: %w", err)
		}
		for _, seat := range seats {
			known = known || seat.Unhooked
		}
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
	if c == nil {
		return true
	}
	return !(c.Int("stopped_from_gen") == d.Gen && c.Int("gen") > d.Gen)
}

// stopDebtExtras reads each STOP debt entry's own record by id, placed or not (a load's
// extras), beside the whole tables: a card the STOPPED reconcile returned off the table
// (LiveReturnStep: a friend's work card retired by FriendReturn, a read card retired lost by
// readLostUnit) keeps its return receipt (stopped_from_gen, gen+1) on that kept record, which
// a read of the placed cells never sees. Without it stopOwned found no card and held the
// entry owned, and START refused for good: nothing could return a card off the table
// (2026-10-18: 32 such debts held `nova-sprint start` refused). A card removed without its
// receipt stays owned (the safe side), so out-of-band removal still cannot erase a debt.
// tla/StopReturn.tla Retire, GoneNeverBlocksStart
func stopDebtExtras(debt []StopLease) func(*sprint.Snapshot) map[string][]string {
	if len(debt) == 0 {
		return nil
	}
	return func(s *sprint.Snapshot) map[string][]string {
		out := map[string][]string{}
		for _, d := range debt {
			if d.Table != sprint.Fleet && d.Table != sprint.Readers {
				continue
			}
			if s.T(d.Table).Placed(d.ID) == nil {
				out[d.Table] = append(out[d.Table], d.ID)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
}

// stopRetired says a STOP debt entry is settled by a receipt on a card that has left its
// table: the reconcile's return retired it with the STOP's receipt (stopDebtExtras read it).
func stopRetired(s *sprint.Snapshot, d StopLease) bool {
	if stopOwned(s, d) || s == nil {
		return false
	}
	t := s.Fleet
	if d.Table == sprint.Readers {
		t = s.Readers
	}
	c := t.Card(d.ID)
	return c != nil && !c.Placed()
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
