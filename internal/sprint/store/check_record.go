package store

import (
	"context"
	"encoding/json"
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

// checkPage is one check record decoded by the check row reader (S1, S2).
type checkPage struct {
	rec CheckRecord
}

// lineSchema is one text of a schema list of lines.
type lineSchema struct {
	Text string `json:"text"`
}

// checkSchema is the schema JSON of the check record: times in Unix nanoseconds.
type checkSchema struct {
	Epoch    uint64               `json:"epoch"`
	At       int64                `json:"at"`
	By       string               `json:"by"`
	Clean    bool                 `json:"clean"`
	Streams  []lineSchema         `json:"streams,omitempty"`
	Kinds    string               `json:"kinds,omitempty"`
	Findings []lineSchema         `json:"findings,omitempty"`
	Cards    []sprint.CheckedCard `json:"cards"`
	Digest   string               `json:"digest"`
}

func linesToSchema(ss []string) []lineSchema {
	if ss == nil {
		return nil
	}
	out := make([]lineSchema, len(ss))
	for i, s := range ss {
		out[i] = lineSchema{Text: s}
	}
	return out
}

func linesFromSchema(ls []lineSchema) []string {
	if ls == nil {
		return nil
	}
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Text
	}
	return out
}

func checkToSchema(r CheckRecord) checkSchema {
	return checkSchema{Epoch: r.Epoch, At: rowTimeNanos(r.At), By: r.By, Clean: r.Clean,
		Streams: linesToSchema(r.Streams), Kinds: r.Kinds, Findings: linesToSchema(r.Findings),
		Cards: r.Cards, Digest: r.Digest}
}

func checkFromSchema(z checkSchema) CheckRecord {
	return CheckRecord{Epoch: z.Epoch, At: doneTimeOf(z.At), By: z.By, Clean: z.Clean,
		Streams: linesFromSchema(z.Streams), Kinds: z.Kinds, Findings: linesFromSchema(z.Findings),
		Cards: z.Cards, Digest: z.Digest}
}

// checkRowFns adapts the variable table and today's legacy JSON reader to the store row codec (S1, S2).
func checkRowFns() RowFns[checkPage] {
	return RowFns[checkPage]{
		Name: "CHECKRECORD", Forms: variableWireForms(),
		Reset:    func(v *checkPage) { *v = checkPage{} },
		LoadWire: checkLoadWire, LoadJSON: checkLoadJSON,
		Legacy:     legacyObject,
		LoadLegacy: checkLoadLegacy,
		SaveWire:   func(v *checkPage) []byte { return checkSaveWireRec(v.rec) },
		SaveJSON:   func(v *checkPage) []byte { return checkSaveJSONRec(v.rec) },
	}
}

// checkRows makes and registers the one check reader on this Store (S4).
func (st *Store) checkRows() *RowCodec[checkPage] {
	if st == nil {
		return nil
	}
	lazyMu.Lock()
	defer lazyMu.Unlock()
	b := st.viewCodecsLocked()
	if b.check == nil {
		b.check = NewRowCodec(checkRowFns())
	}
	b.check.Register(st)
	return b.check
}

// checkLoadWire adapts the generated variable table to the check record (S1, S2).
func checkLoadWire(v *checkPage, wire []byte) RowReport {
	size, err := viewrowswire.CheckRecordLoadMeasureReason(wire)
	if err != nil {
		return RowReport{Reason: err.Error()}
	}
	var rep viewrowswire.TableReport
	out := viewrowswire.CheckRecordLoad(make([]byte, size), wire, &rep)
	if out == nil {
		return tableReportRow(rep)
	}
	buf := make([]byte, viewrowswire.CheckRecordToJsonMeasure(out))
	n := viewrowswire.CheckRecordToJson(out, buf)
	var z checkSchema
	if err := json.Unmarshal(buf[:n], &z); err != nil {
		return RowReport{Malformed: true, Reason: "malformed"}
	}
	v.rec = checkFromSchema(z)
	return RowReport{}
}

// checkLoadJSON adapts the generated schema JSON form to the check record (S2).
func checkLoadJSON(v *checkPage, text []byte) RowReport {
	var builder viewrowswire.CheckRecordBuilder
	if !builder.Init() {
		return RowReport{Malformed: true, Reason: "malformed"}
	}
	var rep viewrowswire.TableReport
	if !viewrowswire.CheckRecordFromJson(&builder, text, &rep) {
		return tableReportRow(rep)
	}
	root := builder.GetRoot()
	buf := make([]byte, viewrowswire.CheckRecordMeasure(root, &builder.Arena))
	n := viewrowswire.CheckRecordSave(root, buf, &builder.Arena)
	if n < 0 {
		return RowReport{Malformed: true, Reason: "malformed"}
	}
	return checkLoadWire(v, buf[:n])
}

// checkSaveWireRec writes the check record as the variable table wire (S2).
func checkSaveWireRec(r CheckRecord) []byte {
	b, err := json.Marshal(checkToSchema(r))
	if err != nil {
		return nil
	}
	var builder viewrowswire.CheckRecordBuilder
	if !builder.Init() {
		return nil
	}
	var rep viewrowswire.TableReport
	if !viewrowswire.CheckRecordFromJson(&builder, b, &rep) {
		return nil
	}
	root := builder.GetRoot()
	size := viewrowswire.CheckRecordMeasure(root, &builder.Arena)
	if size < 0 {
		return nil
	}
	buf := make([]byte, size)
	n := viewrowswire.CheckRecordSave(root, buf, &builder.Arena)
	if n < 0 {
		return nil
	}
	return buf[:n]
}

// checkSaveJSONRec writes the check record as the schema JSON text (S2).
func checkSaveJSONRec(r CheckRecord) []byte {
	b, err := json.Marshal(checkToSchema(r))
	if err != nil {
		return nil
	}
	return b
}

// checkLoadLegacy keeps today's JSON check record decoder (S2, S4).
func checkLoadLegacy(v *checkPage, raw []byte) RowReport {
	if err := json.Unmarshal(raw, &v.rec); err != nil {
		return RowReport{Malformed: true, Reason: "legacy_json_unreadable"}
	}
	return RowReport{}
}

// CheckRecordOf is the epoch's check record of a stored value, and whether there is one: none
// when absent, unreadable, or another epoch's.
func CheckRecordOf(raw string, ok bool, epoch uint64) (CheckRecord, bool) {
	var r CheckRecord
	if !ok || json.Unmarshal([]byte(raw), &r) != nil || r.Epoch != epoch {
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
	if !ok {
		return CheckRecord{}, false, nil
	}
	page, _, err := st.checkRows().Read([]byte(raw))
	if err != nil {
		return CheckRecord{}, false, nil
	}
	if page.rec.Epoch != st.epoch {
		return CheckRecord{}, false, nil
	}
	return page.rec, true, nil
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
