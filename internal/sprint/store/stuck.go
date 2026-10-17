package store

import (
	"context"
	"encoding/json"
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

// stuckPage is one stuck record decoded by the stuck row reader (S1, S2).
type stuckPage struct {
	rec Stuck
}

// stuckSchema is the schema JSON of the stuck record: times in Unix nanoseconds.
type stuckSchema struct {
	Op         string `json:"op"`
	Verb       string `json:"verb"`
	At         int64  `json:"at"`
	Seen       int64  `json:"seen"`
	Why        string `json:"why"`
	Repaired   string `json:"repaired,omitempty"`
	RepairedAt int64  `json:"repaired_at,omitempty"`
}

func stuckToSchema(s Stuck) stuckSchema {
	return stuckSchema{Op: s.Op, Verb: s.Verb, At: rowTimeNanos(s.At), Seen: rowTimeNanos(s.Seen),
		Why: s.Why, Repaired: s.Repaired, RepairedAt: rowTimeNanos(s.RepairedAt)}
}

func stuckFromSchema(z stuckSchema) Stuck {
	return Stuck{Op: z.Op, Verb: z.Verb, At: doneTimeOf(z.At), Seen: doneTimeOf(z.Seen),
		Why: z.Why, Repaired: z.Repaired, RepairedAt: doneTimeOf(z.RepairedAt)}
}

// stuckRowFns adapts the variable table and today's legacy JSON reader to the store row codec (S1, S2).
func stuckRowFns() RowFns[stuckPage] {
	return RowFns[stuckPage]{
		Name: "STUCK", Forms: variableWireForms(),
		Reset:    func(v *stuckPage) { *v = stuckPage{} },
		LoadWire: stuckLoadWire, LoadJSON: stuckLoadJSON,
		Legacy:     legacyObject,
		LoadLegacy: stuckLoadLegacy,
		SaveWire:   func(v *stuckPage) []byte { return stuckSaveWireRec(v.rec) },
		SaveJSON:   func(v *stuckPage) []byte { return stuckSaveJSONRec(v.rec) },
	}
}

// stuckRows makes and registers the one stuck reader on this Store (S4).
func (st *Store) stuckRows() *RowCodec[stuckPage] {
	if st == nil {
		return nil
	}
	lazyMu.Lock()
	defer lazyMu.Unlock()
	b := st.viewCodecsLocked()
	if b.stuck == nil {
		b.stuck = NewRowCodec(stuckRowFns())
	}
	b.stuck.Register(st)
	return b.stuck
}

// stuckLoadWire adapts the generated variable table to the stuck record (S1, S2).
func stuckLoadWire(v *stuckPage, wire []byte) RowReport {
	size, err := viewrowswire.StuckLoadMeasureReason(wire)
	if err != nil {
		return RowReport{Reason: err.Error()}
	}
	var rep viewrowswire.TableReport
	out := viewrowswire.StuckLoad(make([]byte, size), wire, &rep)
	if out == nil {
		return tableReportRow(rep)
	}
	buf := make([]byte, viewrowswire.StuckToJsonMeasure(out))
	n := viewrowswire.StuckToJson(out, buf)
	var z stuckSchema
	if err := json.Unmarshal(buf[:n], &z); err != nil {
		return RowReport{Malformed: true, Reason: "malformed"}
	}
	v.rec = stuckFromSchema(z)
	return RowReport{}
}

// stuckLoadJSON adapts the generated schema JSON form to the stuck record (S2).
func stuckLoadJSON(v *stuckPage, text []byte) RowReport {
	var builder viewrowswire.StuckBuilder
	if !builder.Init() {
		return RowReport{Malformed: true, Reason: "malformed"}
	}
	var rep viewrowswire.TableReport
	if !viewrowswire.StuckFromJson(&builder, text, &rep) {
		return tableReportRow(rep)
	}
	root := builder.GetRoot()
	buf := make([]byte, viewrowswire.StuckMeasure(root, &builder.Arena))
	n := viewrowswire.StuckSave(root, buf, &builder.Arena)
	if n < 0 {
		return RowReport{Malformed: true, Reason: "malformed"}
	}
	return stuckLoadWire(v, buf[:n])
}

// stuckSaveWireRec writes the stuck record as the variable table wire (S2).
func stuckSaveWireRec(rec Stuck) []byte {
	b, err := json.Marshal(stuckToSchema(rec))
	if err != nil {
		return nil
	}
	var builder viewrowswire.StuckBuilder
	if !builder.Init() {
		return nil
	}
	var rep viewrowswire.TableReport
	if !viewrowswire.StuckFromJson(&builder, b, &rep) {
		return nil
	}
	root := builder.GetRoot()
	size := viewrowswire.StuckMeasure(root, &builder.Arena)
	if size < 0 {
		return nil
	}
	buf := make([]byte, size)
	n := viewrowswire.StuckSave(root, buf, &builder.Arena)
	if n < 0 {
		return nil
	}
	return buf[:n]
}

// stuckSaveJSONRec writes the stuck record as the schema JSON text (S2).
func stuckSaveJSONRec(rec Stuck) []byte {
	b, err := json.Marshal(stuckToSchema(rec))
	if err != nil {
		return nil
	}
	return b
}

// stuckLoadLegacy keeps today's JSON stuck record decoder (S2, S4).
func stuckLoadLegacy(v *stuckPage, raw []byte) RowReport {
	if err := json.Unmarshal(raw, &v.rec); err != nil {
		return RowReport{Malformed: true, Reason: "legacy_json_unreadable"}
	}
	return RowReport{}
}

// stuck reads the stuck record; ok is false when there is none or the store
// keeps no machine records.
func (st *Store) stuck(ctx context.Context) (Stuck, bool, error) {
	kv, ok := st.B.(KV)
	if !ok {
		return Stuck{}, false, nil
	}
	raw, held, err := kv.GetKey(ctx, keyStuck)
	if err != nil {
		return Stuck{}, false, err
	}
	if !held {
		return Stuck{}, false, nil
	}
	page, _, err := st.stuckRows().Read([]byte(raw))
	if err != nil {
		return Stuck{}, false, fmt.Errorf("the machine's %s record is unreadable: %w", keyStuck, err)
	}
	return page.rec, page.rec.Op != "", nil
}

// stuckOf is the stuck record the fence was read with (Fence.Stuck); ok is
// false when there is none.
func stuckOf(f Fence) (Stuck, bool, error) {
	var s Stuck
	if f.Stuck == "" {
		return s, false, nil
	}
	if err := json.Unmarshal([]byte(f.Stuck), &s); err != nil {
		return s, false, fmt.Errorf("the machine's %s record is unreadable: %w", keyStuck, err)
	}
	return s, s.Op != "", nil
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
