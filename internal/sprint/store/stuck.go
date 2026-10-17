package store

import (
	"context"
	"fmt"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/viewrowswire"
)

// A stuck operation: one pending past its grace that the tick could not
// finish. Besides the machine line (the tick fails and says why), the tick
// keeps a stuck record; the first step that writes after the fence is free
// again, a verb's or the tick's, writes one judgment saying the operation was
// stuck, for how long, and what repair did, and its commit deletes the record.

// keyStuck is the stuck record, one for the sprint (a machine key).
const keyStuck = "stuck"

// Stuck is the stuck record: the operation, when it was written, when the
// tick first could not finish it and why, and what repair did when a repair
// finished or abandoned it.
type Stuck struct {
	Op         string    `json:"op"`
	Verb       string    `json:"verb"`
	At         time.Time `json:"at"`
	Seen       time.Time `json:"seen"`
	Why        string    `json:"why"`
	Repaired   string    `json:"repaired,omitempty"`
	RepairedAt time.Time `json:"repaired_at,omitempty"`
}

// stuckOps is the stuck record's generated variable table (viewrowswire Stuck; S1).
var stuckOps = viewOps[viewrowswire.Stuck]{"Stuck",
	viewBuilder((*viewrowswire.StuckBuilder).Init, (*viewrowswire.StuckBuilder).GetRoot, func(b *viewrowswire.StuckBuilder) *viewrowswire.TableArena { return &b.Arena }, viewrowswire.StuckFromJson),
	viewrowswire.StuckMeasure, viewrowswire.StuckSave, viewrowswire.StuckLoadMeasureReason, viewrowswire.StuckLoad, viewrowswire.StuckToJsonMeasure, viewrowswire.StuckToJson}

// stuckRead reads one stored stuck record through c, in any of its forms (S2): a record
// that does not read is the named refusal, counted on c (S4).
func stuckRead(c *RowCodec[Stuck], raw string, held bool) (Stuck, bool, error) {
	s, held, err := viewRead(c, raw, held)
	if err != nil {
		return Stuck{}, false, fmt.Errorf("the machine's %s record is unreadable: %w", keyStuck, err)
	}
	return s, held && s.Op != "", nil
}

// stuck reads the stuck record; ok is false when there is none or the store
// keeps no machine records.
func (st *Store) stuck(ctx context.Context) (Stuck, bool, error) {
	kv, ok := st.B.(KV)
	if !ok {
		return Stuck{}, false, nil
	}
	raw, held, err := kv.GetKey(ctx, keyStuck)
	if err != nil || !held {
		return Stuck{}, false, err
	}
	return stuckRead(st.viewRows().stuck, raw, held)
}

// stuckOf is the stuck record the fence was read with (Fence.Stuck); ok is
// false when there is none. It reads with the loose readers: the fence carries
// no Store.
func stuckOf(f Fence) (Stuck, bool, error) {
	if f.Stuck == "" {
		return Stuck{}, false, nil
	}
	return stuckRead(viewRowsLoose.stuck, f.Stuck, true)
}

// markStuck records an operation the tick could not finish; a record of the
// same operation keeps when it was first seen.
func (st *Store) markStuck(ctx context.Context, op OpRecord, why string) error {
	s, ok, err := st.stuck(ctx)
	if err != nil {
		return err
	}
	if ok && s.Op == op.ID && s.Repaired == "" {
		return nil
	}
	return st.putJSON(ctx, keyStuck, Stuck{Op: op.ID, Verb: op.Verb, At: op.At, Seen: st.now(), Why: why})
}

// repaired records what repair did to a stuck operation.
func (st *Store) repaired(ctx context.Context, r RepairResult) error {
	if r.Done == RepairOpen {
		return nil
	}
	s, ok, err := st.stuck(ctx)
	if err != nil || !ok || s.Op != r.Op || s.Repaired != "" {
		return err
	}
	s.Repaired, s.RepairedAt = r.Done, st.now()
	if r.Detail != "" {
		s.Repaired += ": " + r.Detail
	}
	return st.putJSON(ctx, keyStuck, s)
}

// stuckNote is the judgment of a stuck operation no longer pending: how long
// it was stuck and what repair did.
func stuckNote(s Stuck, now time.Time, who string) sprint.Note {
	end := s.RepairedAt
	if end.IsZero() {
		end = now
	}
	did := s.Repaired
	if did == "" {
		did = "not recorded; the operation is no longer pending"
	}
	return sprint.Note{Kind: sprint.Judgment, Type: sprint.NOpStuck, StreamLevel: true, Who: who, At: now, Marked: true,
		Decisions: append([]string(nil), sprint.Decisions[sprint.NOpStuck]...),
		What: fmt.Sprintf("operation %s (%s) was stuck %s (the tick first could not finish it at %s: %s); repair: %s",
			s.Op, s.Verb, end.Sub(s.At).Round(time.Second), s.Seen.UTC().Format(time.RFC3339), s.Why, did)}
}
