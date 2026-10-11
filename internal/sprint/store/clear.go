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
	Finished  string      // a pending operation of the old epoch clear finished first
	Abandoned string      // one it could not finish, abandoned with the old epoch
	Restored  bool        // a clear cut before it restored its shape, finished first
	Machine   string      // the machine's state before clear set it STOPPED
	Refused   []StopLease `json:"refused,omitempty"`
}

// Clear stops the sprint and clears all work in it: it sets the machine
// STOPPED, then refuses only live work/read leases (SPEC-SPRINT section 14).
// It finishes or abandons the old epoch's pending operation, then advances
// the epoch once. All sprint state is keyed by epoch, so the new epoch starts
// empty without restoring any of the old shape (the blank-slate clear design).
// Nothing is deleted; the old epoch stays readable (At), and its writers are
// refused as stale. The new epoch gets only its own STOPPED announcement.
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
	for t, rows := range map[string][]string{sprint.Work: sh.Streams, sprint.Merge: sh.Streams, sprint.Readers: sh.Readers, sprint.Fleet: sh.Members} {
		if len(rows) == 0 {
			continue
		}
		if err := st.B.RowsAdd(ctx, st.Names.Table(t), rows); err != nil {
			return nil, fmt.Errorf("the rows of %s: %w", st.Names.Table(t), err)
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
