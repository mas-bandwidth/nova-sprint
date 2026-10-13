package store

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// ClearResult is what clear did.
type ClearResult struct {
	From, To uint64
	At       time.Time
	// Held is what the old epoch held: primaries, work cards, read cards and
	// merge cards on the tables, and open judgments.
	Held      map[string]int
	Finished  string      // a pending operation of the old epoch clear finished first
	Abandoned string      // one it could not finish, abandoned with the old epoch
	Restored  bool        // a clear cut before it restored its shape, finished first
	Machine   string      // the machine's state before clear stopped it and left it READY
	Refused   []StopLease `json:"refused,omitempty"`
	// Defaults is each setting the old epoch held away from its working default, as
	// "<name> was <v>, now <default>": the set-defaults step does not carry it
	// (setDefaults; sprint.Defaults).
	Defaults []string `json:"defaults,omitempty"`
	// Kept is each working-default setting the new epoch already held away from its default
	// (a set that landed after the advance), "<name> kept at <v>, set in the new epoch": the
	// step writes nothing over it, so no reset names it (sprint.SetDefaults).
	Kept []string `json:"kept,omitempty"`
	// Owed says this clear advanced nothing: the epoch's advance flagged it owed its step
	// (EpochState.DefaultsOwed) and the clear that made it stopped before the step was
	// done, so this one finished the step there, carrying from the epoch that advance left
	// (From), into To, the same epoch (tla/ClearDefaults.tla FinishOnlyWhatIsOwed,
	// CarrySurvives, MembersRestored).
	Owed bool `json:"owed,omitempty"`
	// OwedFinished is what an owed clear finished: the members and readers restored from
	// From, and the working defaults set ("members:<n>", "readers:<n>", "defaults").
	OwedFinished []string `json:"owed_finished,omitempty"`
}

// Clear stops the sprint and clears all work in it: it sets the machine
// STOPPED, then refuses only live work/read leases (SPEC-SPRINT section 14);
// the new epoch's machine, not started since, reads READY (section 14, "READY").
// It finishes or abandons the old epoch's pending operation, then advances
// the epoch once. All sprint state is keyed by epoch, so the new epoch starts
// with none of the old work; its configuration, the fleet's members and the
// readers, is restored from the old shape (restoreMembers), like the fleet's
// members always were (the tabula-rasa-for-work clear design).
// Nothing is deleted; the old epoch stays readable (At), and its writers are
// refused as stale. The new epoch gets only its own STOPPED announcement
// (tla/AttemptsEpochs.tla Clear).
// tla/StopReturn.tla Clear
func (st *Store) Clear(ctx context.Context) (ClearResult, error) {
	var res ClearResult
	es, err := st.EpochNow(ctx)
	if err != nil {
		return res, err
	}
	// The machine first: no tick begins after it is STOPPED, and a tick in
	// flight is refused as stale at its next part. Clear leaves it STOPPED.
	if _, ok := st.B.(KV); ok {
		before, _, _, err := st.SetMachine(ctx, false)
		if err != nil {
			return res, fmt.Errorf("stopping the machine: %w", err)
		}
		res.Machine = before.StateWord()
		// A receipt may already have moved a captured card out of Working or
		// Reading; only leases still live after STOP block this epoch advance.
		live, err := st.Load(ctx, []string{sprint.Fleet, sprint.Readers}, nil)
		if err != nil {
			return res, err
		}
		res.Refused = activeStopLeases(live)
		if len(res.Refused) > 0 {
			leases := make([]string, 0, len(res.Refused))
			for _, lease := range res.Refused {
				leases = append(leases, fmt.Sprintf("%s@%d (%s row %s)", lease.ID, lease.Gen, lease.Table, lease.Row))
			}
			return res, fmt.Errorf("clear stopped the machine with %d captured owner work/read leases: %s; cancel and stop-return each child on its owner row, then run clear again", len(res.Refused), strings.Join(leases, ", "))
		}
		// The people and their goals are the sprint's and are kept; their
		// push times start again with the new sprint.
		if err := st.ResetGoalPushes(ctx); err != nil {
			return res, fmt.Errorf("resetting the goals' pushes: %w", err)
		}
	}
	st, err = st.repin(ctx)
	if err != nil {
		return res, err
	}
	res.Restored = es.Owed
	f, err := st.B.ReadFence(ctx)
	if err != nil {
		return res, err
	}
	if f.Pending != nil {
		r, err := st.finish(ctx, *f.Pending)
		if err != nil {
			return res, err
		}
		if r.Done == RepairOpen {
			if err := st.B.Release(ctx, abandonment(*f.Pending, st.Actor, st.now()), true); err != nil {
				return res, err
			}
			res.Abandoned = f.Pending.ID
		} else {
			res.Finished = f.Pending.ID
		}
	}
	// A clear that advanced and stopped before its owed step was done (its failure says:
	// run clear again) left the flag its advance wrote: this clear finishes the step there
	// and advances nothing, so the members and the coordinator's carried settings are never
	// lost to an empty epoch. Only the flag says so: an epoch without it (an older
	// binary's clear, a fresh init, epoch 0) is never owed.
	if cur, err := st.EpochNow(ctx); err != nil {
		return res, err
	} else if cur.N == st.epoch && cur.N > 0 && cur.DefaultsOwed {
		return st.finishOwed(ctx, res)
	}
	old, err := st.Load(ctx, All, nil)
	if err != nil {
		return res, err
	}
	res.From, res.To, res.At = st.epoch, st.epoch+1, st.now()
	ok, err := st.root.AdvanceEpoch(ctx, st.epoch, res.At)
	if err != nil {
		return res, fmt.Errorf("%w: advancing the sprint's epoch: %v; run: nova-sprint clear again", ErrUnknown, err)
	}
	if !ok {
		return res, fmt.Errorf("the sprint left epoch %d while it was cleared (another clear); nothing was changed by this one; run: nova-sprint where", st.epoch)
	}
	next, _, err := st.repinOnly(ctx)
	if err != nil {
		return res, err
	}
	if next.epoch != res.To {
		// Another clear advanced past this one's epoch, having performed the
		// restore this one owed first.
		return res, fmt.Errorf("the sprint left epoch %d as it was restored (another clear); run: nova-sprint where", res.To)
	}
	// AdvanceEpoch leaves a restore marker for older clear implementations;
	// settle it directly so pin never copies the frozen epoch into this one.
	if err := next.root.SettleEpoch(ctx, next.epoch); err != nil {
		return res, err
	}
	// The advance flagged the new epoch owed its step, in the same write: restoreMembers,
	// then the working defaults, then the flag cleared (owedStep).
	if res.Defaults, res.Kept, err = next.owedStep(ctx, old); err != nil {
		return res, err
	}
	res.Held = map[string]int{"primaries": placed(old.Work, ""), "work cards": placed(old.Fleet, sprint.Ctl),
		"read cards": placed(old.Readers, ""), "merge cards": placed(old.Merge, sprint.Ctl), "open judgments": len(old.Open)}
	return res, st.clearReady(ctx, next, res)
}

// clearReady writes the new epoch's READY record: its machine is READY (docs/SPEC-SPRINT.md
// section 14, "READY"; tla/MachineReady.tla ClearAdvance, ClearLeavesReady): it has not started
// since this clear, so the record as read now is READY with nothing written to it; the
// view's state text says so, and the new epoch's own record says why.
func (st *Store) clearReady(ctx context.Context, next *Store, res ClearResult) error {
	if kv, ok := st.B.(KV); ok {
		m, _, err := next.Machine(ctx)
		if err != nil {
			return err
		}
		if err := kv.ShowState(ctx, next.Names.View(), ViewState(m)); err != nil {
			return err
		}
		at := res.To
		if _, err := next.Run(ctx, Step{Verb: "clear", Epoch: &at, Plan: func(s *sprint.Snapshot) sprint.Plan {
			return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Happened, Type: sprint.NMachineStopped, Who: st.Actor, At: s.Now,
				What: "the machine is READY after the clear of epoch " + strconv.FormatUint(res.From, 10) + ": it has not started since; when the new sprint is ready: nova-sprint start"}}}
		}}); err != nil {
			return err
		}
	}
	return nil
}

func placed(t *sprint.Table, except string) int {
	n := 0
	for _, c := range t.Cards() {
		if c.Placed() && c.Col != except {
			n++
		}
	}
	return n
}

// restoreMembers carries the sprint's configuration across a clear: the
// readers' rows and the fleet's members with their rows, widths, status and
// holds (the machines are still there, and a hold is part of a member's state
// — the sync's mark on its own hold crosses a clear; a reader row is the
// sprint's setting, not its work). The members' and readers' cards, and the
// old epoch's streams, reads, merges and open judgments, are left behind. It
// reads the shape from old, the old epoch frozen since the advance, and writes
// it at the new one. It runs at most once per owed step, whatever clears run at once
// (tla/ClearDefaults.tla RestoreRow, RestoreSettled, RestoreDone, RerunKeepsEpochWrites):
//   - its rows and the readers' tiers go in one guarded exchange (Backend.GuardedRows) that
//     reads, in the same exchange, that the restore is still pending (the flag names the
//     epoch, the marker is absent); a clear that finds the marker another clear wrote writes
//     nothing (GuardSettled);
//   - its last write, the members' control cards, carries the marker
//     (sprint.PropMembersRestored) in the same manifest, and a step that finds the marker
//     in its snapshot writes nothing at all;
//   - no verb edits or removes a member's or a reader's row while the restore is pending,
//     each checking it in the same exchange as its write (ErrRestorePending).
//
// So while the restore is pending every row it writes is its own, and the tiers are
// written as the old epoch held them; once the marker is written nothing of the restore
// is written again, and an epoch's own write, an intentional empty tiers included, stays.
func (st *Store) restoreMembers(ctx context.Context, old *sprint.Snapshot) error {
	sh := sprint.ShapeOf(old)
	sh.Streams = nil
	if len(sh.ReaderTiers) > 0 {
		// the column is the definition's (a migration's), not the epoch's rows
		if err := st.EnsureReaderTiers(ctx); err != nil {
			return fmt.Errorf("the readers' tiers column: %w", err)
		}
	}
	readers, fleet := st.Names.Table(sprint.Readers), st.Names.Table(sprint.Fleet)
	var writes []RowWrite
	if len(sh.Readers) > 0 {
		writes = append(writes, RowWrite{Op: "add", Table: readers, Rows: sh.Readers})
		for _, rd := range slices.Sorted(maps.Keys(sh.ReaderTiers)) {
			if spot := strings.TrimSpace(sh.ReaderTiers[rd]); spot != "" {
				writes = append(writes, RowWrite{Op: "set", Table: readers, Rows: []string{rd}, Texts: map[string]string{sprint.ReaderTiers: spot}})
			}
		}
	}
	if len(sh.Members) > 0 {
		writes = append(writes, RowWrite{Op: "add", Table: fleet, Rows: sh.Members})
	}
	if st.beforeRestore != nil {
		if err := st.beforeRestore(st); err != nil {
			return err
		}
	}
	switch o, err := st.B.GuardedRows(ctx, st.restoreGuard(true), writes); {
	case err != nil:
		return fmt.Errorf("the rows of the members and the readers: %w", err)
	case o == GuardSettled:
		return nil // another clear restored the epoch and wrote its marker
	case o == GuardMoved:
		return fmt.Errorf("the sprint left epoch %d as it was restored (another clear); run: nova-sprint where", st.epoch)
	}
	at := st.epoch
	res, err := st.Run(ctx, Step{Verb: "clear", Load: All, Mirrors: true, Epoch: &at,
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			if _, marked := s.Fleet.Prop(sprint.PropMembersRestored); marked {
				return sprint.Plan{} // another clear's restore is done: nothing of this one is written
			}
			p := sprint.RestoreShape(s, sh)
			// the restore's marker, in the same write as the control cards (the fleet table's
			// one manifest)
			p.Props = append(p.Props, sprint.PropWrite{Table: sprint.Fleet, Name: sprint.PropMembersRestored, Value: "1", WasAbsent: true})
			return p
		}})
	if err != nil {
		return err
	}
	if len(res.Refused) > 0 {
		if done, derr := st.membersRestored(ctx); derr == nil && done {
			return nil // another clear's marker landed between the plan and the write
		}
		return fmt.Errorf("the member control cards: %s: %s", res.Refused[0].Key, res.Refused[0].Why)
	}
	return nil
}

// restoreGuard is the guard of a write at the pinned epoch: the restore's own writes want
// it pending (pending true), a verb's want it not (pending false).
func (st *Store) restoreGuard(pending bool) RestoreGuard {
	return RestoreGuard{Pending: pending, MarkerTable: st.Names.Table(sprint.Fleet), Marker: sprint.PropMembersRestored}
}

// guardRefusal is a verb's refusal for a guarded exchange that wrote nothing.
func (st *Store) guardRefusal(o GuardOutcome, verb string) error {
	switch o {
	case GuardPending:
		return fmt.Errorf("%w: %s changes no member's or reader's row until it has; nothing was changed; run: nova-sprint clear (it finishes the step), then %s again", ErrRestorePending, verb, verb)
	case GuardMoved:
		return fmt.Errorf("%w: the sprint left epoch %d while %s ran; nothing was changed; run %s again", ErrClearedUnderVerb, st.epoch, verb, verb)
	}
	return nil
}

// ErrClearedUnderVerb is a guarded verb that found the sprint at another epoch than the one it
// pinned: a clear advanced between its read and its write, and it wrote nothing.
var ErrClearedUnderVerb = errors.New("the sprint was cleared under this verb")

// owedStep is the step an epoch's advance flagged it owed (EpochState.DefaultsOwed), at the
// pinned epoch: restoreMembers, then the working defaults, both carried from old, the epoch
// the advance left; then the flag is cleared (SettleDefaults), only after both have run.
// The flag lives in the epoch record and the step writes the tables, and neither store
// writes the two in one write (the table layer's function writes only its tables), so the
// flag's clear is a write of its own after the last of them, and the step is idempotent
// instead: restoreMembers adds only the rows and control cards the epoch lacks (RowsAdd,
// RestoreShape) and the defaults write only the settings it lacks (sprint.SetDefaults), so
// a clear run again after a crash anywhere in it finishes it (tla/ClearDefaults.tla
// RestoreRow, RestoreDone, SetDefaults, SettleDefaults). The step's phase is read off two
// records: the flag with no restore marker is restore pending, the flag with the marker is
// defaults pending, no flag is settled; the restore runs only while it is pending, so it
// runs at most once (RerunKeepsEpochWrites). It returns the defaults' resets and the
// settings it kept.
func (st *Store) owedStep(ctx context.Context, old *sprint.Snapshot) (resets, kept []string, err error) {
	// The fleet's members and the readers are not sprint work: their rows,
	// widths and status survive a clear (the machines and the readers are still
	// there); their holds, leases, stop records, alarms, streams, cards and
	// read cards are the old epoch's and are left behind. The marker read here is
	// only the fast path: restoreMembers' writes check it again, each in its own
	// exchange, so a clear that read it absent and finds it written writes nothing.
	done, err := st.membersRestored(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !done {
		if err := st.restoreMembers(ctx, old); err != nil {
			return nil, nil, fmt.Errorf("restoring the members and the readers in epoch %d: %w; run: nova-sprint clear again", st.epoch, err)
		}
	}
	// Then set defaults: every working default written into the new epoch where it is
	// absent, the coordinator's carried settings (the threshold alarms, the base) carried
	// from the old epoch, the rest at their defaults, never an old value of them
	// (tla/ClearDefaults.tla SetDefaults, DefaultsAfterClear, CarrySurvives, NeverAbsent).
	if st.beforeDefaults != nil {
		if err := st.beforeDefaults(st); err != nil {
			return nil, nil, err
		}
	}
	resets, kept, err = st.setDefaults(ctx, old)
	if err != nil {
		return nil, nil, err
	}
	if err := st.root.SettleDefaults(ctx, st.epoch); err != nil {
		return nil, nil, fmt.Errorf("settling the step epoch %d owed: %w; run: nova-sprint clear again", st.epoch, err)
	}
	return resets, kept, nil
}

// membersRestored says the pinned epoch holds the restore's marker
// (sprint.PropMembersRestored): its members and readers were restored by its owed step.
func (st *Store) membersRestored(ctx context.Context) (bool, error) {
	s, err := st.Load(ctx, []string{sprint.Fleet}, nil)
	if err != nil {
		return false, err
	}
	_, ok := s.Fleet.Prop(sprint.PropMembersRestored)
	return ok, nil
}

// ErrRestorePending is a verb refused because the epoch's owed step has not restored its
// members and readers yet: a clear run again finishes it, and the restore must never write
// over a member's or a reader's row the epoch changed (tla/ClearDefaults.tla EditRow,
// DeleteRow, RerunKeepsEpochWrites).
var ErrRestorePending = errors.New("the clear of this epoch has not restored its members and readers yet")

// RestorePending says the sprint's epoch is owed its step (EpochState.DefaultsOwed) and its
// restore's marker is absent: the members and the readers are not restored yet.
func (st *Store) RestorePending(ctx context.Context) (bool, error) {
	pinned, err := st.pin(ctx)
	if err != nil {
		return false, err
	}
	es, err := pinned.EpochNow(ctx)
	if err != nil {
		return false, err
	}
	if es.N != pinned.epoch || !es.DefaultsOwed {
		return false, nil
	}
	done, err := pinned.membersRestored(ctx)
	return !done, err
}

// guardedRows is a verb's row writes at the epoch it pins, in one exchange with the check
// that the epoch's restore is not pending and that the sprint is still at that epoch
// (Backend.GuardedRows): the check and the write can never see two epochs or two phases.
func (st *Store) guardedRows(ctx context.Context, verb string, writes func(pinned *Store) []RowWrite) error {
	pinned, err := st.pin(ctx)
	if err != nil {
		return err
	}
	if st.beforeGuardedWrite != nil {
		st.beforeGuardedWrite()
	}
	o, err := pinned.B.GuardedRows(ctx, pinned.restoreGuard(false), writes(pinned))
	if err != nil {
		return err
	}
	return pinned.guardRefusal(o, verb)
}

// SetReaderTiers sets the tiers cell of each named reader row ("" is every tier), in one
// exchange with the restore guard (guardedRows): refused while the epoch's restore is
// pending, and never written at an epoch the verb did not check.
func (st *Store) SetReaderTiers(ctx context.Context, names []string, tiers string) error {
	return st.guardedRows(ctx, "reader tiers", func(pinned *Store) []RowWrite {
		table := pinned.Names.Table(sprint.Readers)
		var w []RowWrite
		for _, n := range names {
			w = append(w, RowWrite{Op: "set", Table: table, Rows: []string{n}, Texts: map[string]string{sprint.ReaderTiers: tiers}})
		}
		return w
	})
}

// AddReaders adds the named reader rows and, when tiers is given, their tiers cells, in one
// exchange with the restore guard (guardedRows).
func (st *Store) AddReaders(ctx context.Context, names []string, tiers *string) error {
	return st.guardedRows(ctx, "reader add", func(pinned *Store) []RowWrite {
		table := pinned.Names.Table(sprint.Readers)
		w := []RowWrite{{Op: "add", Table: table, Rows: names}}
		if tiers != nil {
			for _, n := range names {
				w = append(w, RowWrite{Op: "set", Table: table, Rows: []string{n}, Texts: map[string]string{sprint.ReaderTiers: *tiers}})
			}
		}
		return w
	})
}

// RemoveReaders deletes the named reader rows, in one exchange with the restore guard
// (guardedRows), and then their records (beats, holds).
func (st *Store) RemoveReaders(ctx context.Context, names []string) error {
	if err := st.guardedRows(ctx, "reader remove", func(pinned *Store) []RowWrite {
		return []RowWrite{{Op: "del", Table: pinned.Names.Table(sprint.Readers), Rows: names}}
	}); err != nil {
		return err
	}
	pinned, err := st.pin(ctx)
	if err != nil {
		return err
	}
	if err := pinned.ForgetReaders(ctx, names); err != nil {
		return fmt.Errorf("the rows were removed, their records were not: %w", err)
	}
	return nil
}

// finishOwed is a clear run again over an epoch flagged owed its step: it reads the epoch
// the advance left, frozen since, performs the step at the pinned epoch (owedStep), and
// advances nothing. It reports what it finished (OwedFinished); any part that fails is an
// error, never a success with nothing done.
func (st *Store) finishOwed(ctx context.Context, res ClearResult) (ClearResult, error) {
	res.Owed, res.From, res.To, res.At = true, st.epoch-1, st.epoch, st.now()
	res.Held = map[string]int{}
	old, err := st.owedSource(ctx)
	if err != nil {
		return res, err
	}
	if res.Defaults, res.Kept, err = st.owedStep(ctx, old); err != nil {
		return res, err
	}
	sh := sprint.ShapeOf(old)
	res.OwedFinished = []string{"members:" + strconv.Itoa(len(sh.Members)), "readers:" + strconv.Itoa(len(sh.Readers)), "defaults"}
	return res, st.clearReady(ctx, st, res)
}

// owedSource is the epoch an owed epoch's advance left (its number less one) as read: each
// table as it was, and an empty one where that epoch never wrote the table (NOTABLE).
func (st *Store) owedSource(ctx context.Context) (*sprint.Snapshot, error) {
	from := st.At(st.epoch - 1)
	s, err := from.Load(ctx, All, nil)
	if refusalCode(err) != "NOTABLE" {
		if err != nil {
			return nil, fmt.Errorf("reading epoch %d, the epoch epoch %d's advance left: %w", st.epoch-1, st.epoch, err)
		}
		return s, nil
	}
	var out *sprint.Snapshot
	tables := map[string]*sprint.Table{}
	for _, t := range All {
		one, err := from.Load(ctx, []string{t}, nil)
		if refusalCode(err) == "NOTABLE" {
			tables[t] = sprint.NewTable(st.Names.Table(t))
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading epoch %d, the epoch epoch %d's advance left: %w", st.epoch-1, st.epoch, err)
		}
		tables[t], out = one.T(t), one
	}
	if out == nil {
		out = &sprint.Snapshot{Now: st.now(), Epoch: st.epoch - 1, Prefix: st.Names.Prefix}
	}
	out.Work, out.Readers, out.Merge, out.Fleet = tables[sprint.Work], tables[sprint.Readers], tables[sprint.Merge], tables[sprint.Fleet]
	return out, nil
}

// setDefaults is the clear's set-defaults step (tla/ClearDefaults.tla SetDefaults): one
// step at the new epoch writing every work-table property row of sprint.Defaults at its
// Value (sprint.SetDefaults), by "clear". The bars are nova-config's and never written
// here (their default is read-side, Bars.WithDefaults); the shape is restoreMembers'.
// It returns each setting it wrote that old held away from its default, "<name> was <v>,
// now <default>", and each the new epoch held away from it and it kept.
func (st *Store) setDefaults(ctx context.Context, old *sprint.Snapshot) (resets, kept []string, err error) {
	var oldProps map[string]string
	if old != nil && old.Work != nil {
		oldProps = old.Work.Props()
	}
	at := st.epoch
	res, err := st.Run(ctx, Step{Verb: "clear", Load: tables(sprint.Work), Epoch: &at,
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			p, r, k := sprint.SetDefaults(s, oldProps)
			resets, kept = r, k
			return p
		}})
	if err != nil {
		return nil, nil, fmt.Errorf("setting the working defaults in epoch %d: %w; run: nova-sprint clear again", at, err)
	}
	if len(res.Refused) > 0 {
		return nil, nil, fmt.Errorf("setting the working defaults in epoch %d: %s: %s; run: nova-sprint clear again", at, res.Refused[0].Key, res.Refused[0].Why)
	}
	return resets, kept, nil
}

// restore performs the restore the pinned epoch owes: it reads epoch from,
// frozen since the advance (every write to it is refused as stale), and
// writes its shape at the pinned epoch: the rows of the four tables, then the
// control cards (one step, holding the pinned epoch), then the display cells;
// it removes a fence the old epoch still holds (its writer is refused as
// stale at its next write), and records the restore done. It is the old
// epoch as read.
func (st *Store) restore(ctx context.Context, from uint64) (*sprint.Snapshot, error) {
	old := st.At(from)
	snap, err := old.Load(ctx, All, nil)
	if err != nil {
		return nil, fmt.Errorf("reading epoch %d to restore its shape: %w", from, err)
	}
	sh := sprint.ShapeOf(snap)
	readersTable := st.Names.Table(sprint.Readers)
	for t, rows := range map[string][]string{sprint.Work: sh.Streams, sprint.Merge: sh.Streams, sprint.Readers: sh.Readers, sprint.Fleet: sh.Members} {
		if len(rows) == 0 {
			continue
		}
		if err := st.B.RowsAdd(ctx, st.Names.Table(t), rows); err != nil {
			return nil, fmt.Errorf("the rows of %s: %w", st.Names.Table(t), err)
		}
	}
	if len(sh.ReaderTiers) > 0 {
		if err := st.EnsureReaderTiers(ctx); err != nil {
			return nil, fmt.Errorf("the readers' tiers column: %w", err)
		}
		for rd, tiers := range sh.ReaderTiers {
			if spot := strings.TrimSpace(tiers); spot != "" {
				if err := st.B.RowSet(ctx, readersTable, rd, map[string]string{sprint.ReaderTiers: spot}); err != nil {
					return nil, fmt.Errorf("the tiers of reader %s: %w", rd, err)
				}
			}
		}
	}
	// an archived stream stays archived: its rows are hidden at the new epoch
	if err := st.hideStreams(ctx, sprint.ArchivedStreams(snap), true); err != nil {
		return nil, err
	}
	at := st.epoch
	res, err := st.Run(ctx, Step{Verb: "clear", Load: All, Mirrors: true, Epoch: &at,
		Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.RestoreShape(s, sh) }})
	if err != nil {
		return nil, err
	}
	if len(res.Refused) > 0 {
		return nil, fmt.Errorf("the control cards: %s: %s", res.Refused[0].Key, res.Refused[0].Why)
	}
	f, err := old.B.ReadFence(ctx)
	if err != nil {
		return nil, err
	}
	if f.Pending != nil {
		if err := old.B.Release(ctx, *f.Pending, false); err != nil {
			return nil, fmt.Errorf("removing the fence of epoch %d (operation %s): %w", from, f.Pending.ID, err)
		}
	}
	return snap, st.root.SettleEpoch(ctx, at)
}
