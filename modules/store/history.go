package store

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/jackc/pgx/v5"
)

// ValidateHistory checks, in tx, that the store's log is the whole history of
// its tables, and names the first thing that is not, in this order: the
// events run from seq 1 to the last with no gap; each event's receipt is its
// own (op, seq, kind, actor, accepted and time agree with the row); each event
// is in the epoch the log is in at that point (1, then each accepted clear's
// new epoch) and the epoch row is the log's last; the policy versions stored
// are exactly the ones the log wrote, each equal to its last image there, and
// the active one is the log's last activation; every cursor is at or below
// the last seq; and Rebuild over the events is the snapshot (checkRebuild).
// It only reads. RestoreEnd runs it on a loaded dump; a verb can run it on a
// live store in a REPEATABLE READ transaction.
func ValidateHistory(ctx context.Context, tx pgx.Tx) error {
	if err := validateHistory(ctx, tx); err != nil {
		return fmt.Errorf("store: history: %w", err)
	}
	return nil
}

func validateHistory(ctx context.Context, tx pgx.Tx) error {
	var events []Event
	for after := int64(0); ; {
		page, err := readEvents(ctx, tx, after, MaxEvents)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		events, after = append(events, page...), page[len(page)-1].Seq
	}
	snap, err := snapshotTx(ctx, tx)
	if err != nil {
		return err
	}
	stored, err := all(ctx, tx, scanPolicy, func(v PolicyVersion) int { return v.Version }, "SELECT "+policyCols+" FROM sprint.policy")
	if err != nil {
		return err
	}
	for _, check := range []func() error{
		func() error { return checkSeqs(events) },
		func() error { return checkReceipts(events) },
		func() error { return checkEpochs(events, snap.Epoch) },
		func() error { return checkPolicies(events, stored) },
		func() error { return checkCursors(snap.Cursors, snap.Seq) },
		func() error { return checkRebuild(snap, events) },
	} {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

// checkSeqs: the events are seqs 1, 2, ... with no gap (they come in order).
func checkSeqs(events []Event) error {
	for i, e := range events {
		if want := int64(i) + 1; e.Seq != want {
			return fmt.Errorf("the log has no event at seq %d (the next is seq %d)", want, e.Seq)
		}
	}
	return nil
}

// checkReceipts: each event's stored receipt names that event.
func checkReceipts(events []Event) error {
	for _, e := range events {
		r := e.Receipt
		if r.Op != e.Op || r.Seq != e.Seq || r.Kind != e.Kind || r.Actor != e.Actor || r.Accepted != e.Accepted || !r.At.Equal(e.At) {
			return fmt.Errorf("the receipt stored with event seq %d (op %s) is not its own: it names op %s at seq %d", e.Seq, e.Op, r.Op, r.Seq)
		}
	}
	return nil
}

// checkEpochs: each event is in the log's epoch at that point, and the epoch
// row is the log's last epoch.
func checkEpochs(events []Event, epoch int) error {
	in := 1
	for _, e := range events {
		if e.Epoch != in {
			return fmt.Errorf("event seq %d is in epoch %d, but the log is in epoch %d there", e.Seq, e.Epoch, in)
		}
		if e.Accepted && e.Receipt.NewEpoch != 0 {
			in = e.Receipt.NewEpoch
		}
	}
	if epoch != in {
		return fmt.Errorf("the epoch row is %d, but the log's last epoch is %d", epoch, in)
	}
	return nil
}

// checkPolicies: the stored versions are the ones the log wrote, each equal to
// its last after-image, and the active one is the log's.
func checkPolicies(events []Event, stored map[int]PolicyVersion) error {
	logged := map[int]PolicyVersion{}
	for _, e := range events {
		if e.Accepted && e.After != nil {
			maps.Copy(logged, e.After.Policies)
		}
	}
	for _, v := range slices.Sorted(maps.Keys(stored)) {
		p, ok := logged[v]
		switch {
		case !ok:
			return fmt.Errorf("policy version %d is stored, but no event in the log wrote it", v)
		case !reflect.DeepEqual(p, stored[v]):
			return fmt.Errorf("policy version %d is not as the log last wrote it (active %t stored, %t in the log)", v, stored[v].Active, p.Active)
		}
	}
	for _, v := range slices.Sorted(maps.Keys(logged)) {
		if _, ok := stored[v]; !ok {
			return fmt.Errorf("the log wrote policy version %d, but it is not stored", v)
		}
	}
	return nil
}

// checkCursors: no cursor is past the last seq.
func checkCursors(cursors map[string]int64, last int64) error {
	for _, name := range slices.Sorted(maps.Keys(cursors)) {
		if cursors[name] > last {
			return fmt.Errorf("cursor %s is at seq %d, past the log's last seq %d", name, cursors[name], last)
		}
	}
	return nil
}
