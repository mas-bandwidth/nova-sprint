package store

import (
	"context"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/viewrowswire"
)

// The check record (v1.2.17, start-refuses-until-a-clean-check; docs/SPEC-SPRINT.md section
// 14, "The card check"): every run of nova-sprint check cards writes one, the epoch's, beside
// the machine's own record (keyMachine). It says when and by whom the cards were checked,
// clean or the findings by kind, and every card checked (sprint.CheckedCard: its id, stream,
// tier, priority, needs and the sha256 of its brief) with one digest over them. A failed run
// overwrites a clean one. start reads it before the machine changes and refuses with none, a
// failed one, or a clean one whose digest is not the cards' now, recomputed from the live table
// (tla/MachineReady.tla Check, StartRefusedCheck, StartOnlyChecked). It is keyed by epoch as
// the machine's records are: one of another epoch reads as none, so a clear leaves no record
// in the new epoch (tla/MachineReady.tla ClearAdvance, recFresh).

const keyCheckRecord = "cardcheck" // STRING, the check record (JSON)

// CheckRecord is the check record.
type CheckRecord struct {
	Epoch uint64    `json:"epoch"`
	At    time.Time `json:"at"`
	By    string    `json:"by"`
	Clean bool      `json:"clean"`
	// Streams is the streams a check limited by --stream covered; none is every stream. A
	// limited check never opens the start gate (it did not see every card).
	Streams []string `json:"streams,omitempty"`
	// Kinds is the findings by kind (sprint.CardCheckKinds), "" when clean; Findings their
	// lines, at most MaxCheckFindings of them.
	Kinds    string               `json:"kinds,omitempty"`
	Findings []string             `json:"findings,omitempty"`
	Cards    []sprint.CheckedCard `json:"cards"`
	Digest   string               `json:"digest"`
}

// MaxCheckFindings bounds the finding lines a check record keeps (Kinds counts them all).
const MaxCheckFindings = 200

// checkOps is the check record's generated variable table (viewrowswire CheckRecord; S1).
var checkOps = viewOps[viewrowswire.CheckRecord]{"CheckRecord",
	viewBuilder((*viewrowswire.CheckRecordBuilder).Init, (*viewrowswire.CheckRecordBuilder).GetRoot, func(b *viewrowswire.CheckRecordBuilder) *viewrowswire.TableArena { return &b.Arena }, viewrowswire.CheckRecordFromJson),
	viewrowswire.CheckRecordMeasure, viewrowswire.CheckRecordSave, viewrowswire.CheckRecordLoadMeasureReason, viewrowswire.CheckRecordLoad, viewrowswire.CheckRecordToJsonMeasure, viewrowswire.CheckRecordToJson}

// CheckRecordOf is the epoch's check record of a stored value, and whether there is one: none
// when absent, unreadable, or another epoch's. It reads in any of the record's forms (S2)
// through c, the Store's reader, or the loose reader with none. A record that does not read
// is none, on purpose: with none, start refuses (tla/MachineReady.tla StartRefusedCheck), so
// a damaged record can never open the start gate. The refusal is counted on the reader by
// its name (S4).
func CheckRecordOf(raw string, ok bool, epoch uint64, c ...*RowCodec[CheckRecord]) (CheckRecord, bool) {
	r, ok, err := viewRead(viewCodec(viewRowsLoose.check, c), raw, ok)
	if err != nil || !ok || r.Epoch != epoch {
		return CheckRecord{}, false
	}
	return r, true
}

// CardCheck reads the epoch's check record: false when there is none for this epoch.
func (st *Store) CardCheck(ctx context.Context) (CheckRecord, bool, error) {
	st, err := st.pin(ctx)
	if err != nil {
		return CheckRecord{}, false, err
	}
	kv, err := st.kv()
	if err != nil {
		return CheckRecord{}, false, err
	}
	raw, ok, err := kv.GetKey(ctx, keyCheckRecord)
	if err != nil {
		return CheckRecord{}, false, err
	}
	r, ok := CheckRecordOf(raw, ok, st.epoch, st.viewRows().check)
	return r, ok, nil
}

// PutCardCheck writes the epoch's check record, over any before it: r is stamped with the
// pinned epoch, and its findings cut to MaxCheckFindings.
func (st *Store) PutCardCheck(ctx context.Context, r CheckRecord) (CheckRecord, error) {
	st, err := st.pin(ctx)
	if err != nil {
		return r, err
	}
	r.Epoch = st.epoch
	if len(r.Findings) > MaxCheckFindings {
		r.Findings = r.Findings[:MaxCheckFindings]
	}
	return r, st.putJSON(ctx, keyCheckRecord, r)
}
