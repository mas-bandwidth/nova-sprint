package store

import (
	"context"
	"fmt"
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
	Finished  string // a pending operation of the old epoch clear finished first
	Abandoned string // one it could not finish, abandoned with the old epoch
	Restored  bool   // a clear cut before it restored its shape, finished first
	Machine   string // the machine's state before clear set it STOPPED
	// Refused is each live lease clear refused on: the card, its generation
	// and its owner row, as start lists them.
	Refused []StopLease `json:"refused,omitempty"`
}

// Clear stops the sprint and starts the next epoch as a blank slate: it sets
// the machine STOPPED, as its first act, and leaves it STOPPED, then advances
// the sprint's epoch once, atomically. Every table and sprint record is keyed
// by epoch, so the new epoch is empty by construction: clear copies no shape
// into it (no restore step to forget). Nothing is deleted: the old epoch stays
// where it is and readable (At), and every writer still holding it is refused
// by the table layer as stale, so from the advance on the old epoch is frozen.
// Clear refuses only leases still live in Fleet Working or Readers Reading
// after STOP (tla/StopReturn.tla Stop; SPEC-SPRINT section 14); a lease whose
// card was already stop-returned (back to Ready or Asked) or is no longer
// working does not block, and each one that remains is listed with its
// card@generation and owner row. A pending operation of the old epoch is
// finished first, or abandoned with the old epoch.
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
		// Reading; only leases still live after STOP block this epoch advance,
		// and clear lists each one it refuses on, card@gen and owner row.
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
		// Another clear advanced past this one's epoch.
		return res, fmt.Errorf("the sprint left epoch %d as it was restored (another clear); run: nova-sprint where", res.To)
	}
	// AdvanceEpoch leaves a restore marker for older clear implementations;
	// settle it directly so pin never copies the frozen epoch into this one.
	// The new epoch is blank by construction: no work, merge, stream, reader or
	// member rows, no lease or stop debt, no hold, alarm, notification cursor
	// or session (SPEC-SPRINT section 13, the blank-slate rule).
	if err := next.root.SettleEpoch(ctx, next.epoch); err != nil {
		return res, err
	}
	res.Held = map[string]int{"primaries": placed(old.Work, ""), "work cards": placed(old.Fleet, sprint.Ctl),
		"read cards": placed(old.Readers, ""), "merge cards": placed(old.Merge, sprint.Ctl), "open judgments": len(old.Open)}
	// The new epoch's own record that the machine is STOPPED, and why.
	if _, ok := st.B.(KV); ok {
		at := res.To
		if _, err := next.Run(ctx, Step{Verb: "clear", Epoch: &at, Plan: func(s *sprint.Snapshot) sprint.Plan {
			return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Happened, Type: sprint.NMachineStopped, Who: st.Actor, At: s.Now,
				What: "the machine is STOPPED by the clear of epoch " + strconv.FormatUint(res.From, 10) + "; when the new sprint is ready: nova-sprint start"}}}
		}}); err != nil {
			return res, err
		}
	}
	return res, nil
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

// restore settles the restore marker a pre-blank-slate clear may have left owed
// at the pinned epoch (a clear cut between its advance and its restore). The
// blank-slate design copies no shape (SPEC-SPRINT section 13, the blank-slate
// rule): the epoch is left empty, the marker is cleared, and a later verb does
// not re-read it.
func (st *Store) restore(ctx context.Context, _ uint64) (*sprint.Snapshot, error) {
	if err := st.root.SettleEpoch(ctx, st.epoch); err != nil {
		return nil, err
	}
	return nil, nil
}
