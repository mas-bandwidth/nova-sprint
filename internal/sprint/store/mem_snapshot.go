package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	snapwire "github.com/mas-bandwidth/nova-sprint/internal/sprint/store/snaprowswire"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// A Mem is saved to one JSON document and loaded from it again: the file the
// command's `--redis mem:<file>` twin keeps between one verb and the next (for
// learning and tests, never a fleet's store). Snapshot and Restore are the
// whole of it: every field of the store's state, nothing of its test hooks
// (Fail, MaxWrite, Calls, LogWait, the per-epoch touch counts), so a restored
// store answers every call as the store it was taken from did.

// SnapshotVersion names the document's shape; a document of another version
// is refused, never guessed at.
const SnapshotVersion = 1

type memSnapshot struct {
	Version  int                       `json:"version"`
	Tables   map[string]*tableSnapshot `json:"tables"`
	Dropped  map[string]*residueSnap   `json:"dropped,omitempty"`
	Views    map[string]ntable.View    `json:"views,omitempty"`
	EpochSet bool                      `json:"epoch_set"`
	EpochN   uint64                    `json:"epoch"`
	Cleared  time.Time                 `json:"cleared"`
	Owed     bool                      `json:"owed"`
	// DefaultsOwed is the epoch the set-defaults owed flag names, 0 for none
	DefaultsOwed uint64                  `json:"defaults_owed,omitempty"`
	Logs         map[uint64]*logSnapshot `json:"logs,omitempty"`
	KV           map[string]string       `json:"kv,omitempty"`
	Seq          int                     `json:"seq"`
}

type tableSnapshot struct {
	Def     ntable.Table               `json:"def"`
	Epochs  map[uint64]*epochSnapshot  `json:"epochs,omitempty"`
	Wrote   map[uint64]bool            `json:"wrote,omitempty"`
	Rev     uint64                     `json:"rev"`
	Members map[string]*memberSnapshot `json:"members,omitempty"`
	Ops     map[string]opSnapshot      `json:"ops,omitempty"`
	// Changes is the table's change stream, which a read twin catches up
	// from (TableChanges): a restored store answers it as it did.
	Changes []changeSnapshot `json:"changes,omitempty"`
}

type epochSnapshot struct {
	Rows   []string                     `json:"rows,omitempty"`
	Hidden map[string]bool              `json:"hidden,omitempty"`
	Texts  map[string]map[string]string `json:"texts,omitempty"`
	Props  map[string]string            `json:"props,omitempty"`
}

type memberSnapshot struct {
	Epoch  uint64            `json:"epoch"`
	Placed bool              `json:"placed"`
	Row    string            `json:"row,omitempty"`
	Col    string            `json:"col,omitempty"`
	Score  float64           `json:"score"`
	Rev    uint64            `json:"rev"`
	Fields map[string]string `json:"fields,omitempty"`
}

type changeSnapshot struct {
	Epoch  uint64   `json:"epoch"`
	Before uint64   `json:"before"`
	After  uint64   `json:"after"`
	Verb   string   `json:"verb"`
	IDs    []string `json:"ids,omitempty"`
}

type opSnapshot struct {
	Body    string         `json:"body"`
	Receipt ntable.Receipt `json:"receipt"`
}

type residueSnap struct {
	Keys  map[string]bool `json:"keys,omitempty"`
	Table *tableSnapshot  `json:"table"`
}

type logSnapshot struct {
	Fence    *OpRecord              `json:"fence,omitempty"`
	Gen      uint64                 `json:"gen"`
	Done     map[string]string      `json:"done,omitempty"`
	Progress map[string]time.Time   `json:"progress,omitempty"`
	Inbox    []noteSnapshot         `json:"inbox,omitempty"`
	Lines    []lineSnapshot         `json:"lines,omitempty"`
	Notes    map[string]sprint.Note `json:"notes,omitempty"`
	Aliases  map[string]string      `json:"aliases,omitempty"`  // alias (j<n>) -> note id (sprint.Alias)
	Answered map[string]string      `json:"answered,omitempty"` // judgment id -> who answered it
	Open     map[string]string      `json:"open,omitempty"`
	Cursor   string                 `json:"cursor,omitempty"`
	// Queue is the work table's queue: the changes a verb queued that the
	// next tick's pump applies (sprint.QueueOf).
	Queue []sprint.QueuedChange `json:"queue,omitempty"`
}

type noteSnapshot struct {
	ID   string      `json:"id"`
	Note sprint.Note `json:"note"`
}

type lineSnapshot struct {
	ID   string      `json:"id"`
	Line sprint.Line `json:"line"`
}

func snapTable(t *memTable) *tableSnapshot {
	s := &tableSnapshot{Def: t.def, Rev: t.rev, Epochs: map[uint64]*epochSnapshot{}, Wrote: map[uint64]bool{}, Members: map[string]*memberSnapshot{}, Ops: map[string]opSnapshot{}}
	for e, ep := range t.epochs {
		s.Epochs[e] = &epochSnapshot{Rows: ep.rows, Hidden: ep.hidden, Texts: ep.texts, Props: ep.props}
	}
	maps.Copy(s.Wrote, t.wrote)
	for id, m := range t.members {
		s.Members[id] = &memberSnapshot{Epoch: m.epoch, Placed: m.placed, Row: m.row, Col: m.col, Score: m.score, Rev: m.rev, Fields: m.fields}
	}
	for id, o := range t.ops {
		s.Ops[id] = opSnapshot{Body: o.body, Receipt: o.receipt}
	}
	for _, c := range t.changes {
		s.Changes = append(s.Changes, changeSnapshot{Epoch: c.epoch, Before: c.before, After: c.after, Verb: c.verb, IDs: c.ids})
	}
	return s
}

func (s *tableSnapshot) table() *memTable {
	t := &memTable{def: s.Def, rev: s.Rev, epochs: map[uint64]*memEpoch{}, wrote: map[uint64]bool{}, members: map[string]*memMember{}, ops: map[string]memOp{}}
	for e, ep := range s.Epochs {
		me := &memEpoch{rows: ep.Rows, hidden: ep.Hidden, texts: ep.Texts, props: ep.Props}
		if me.texts == nil {
			me.texts = map[string]map[string]string{}
		}
		t.epochs[e] = me
	}
	maps.Copy(t.wrote, s.Wrote)
	for id, m := range s.Members {
		t.members[id] = &memMember{epoch: m.Epoch, placed: m.Placed, row: m.Row, col: m.Col, score: m.Score, rev: m.Rev, fields: m.Fields}
	}
	for id, o := range s.Ops {
		t.ops[id] = memOp{body: o.Body, receipt: o.Receipt}
	}
	for _, c := range s.Changes {
		t.changes = append(t.changes, memChange{epoch: c.Epoch, before: c.Before, after: c.After, verb: c.Verb, ids: c.IDs})
	}
	return t
}

// Snapshot is the store's whole state as one JSON document. The same state
// always gives the same bytes (maps are written in key order), so a caller
// can tell by comparing two snapshots whether a verb changed anything.
func (m *Mem) Snapshot() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := memSnapshot{Version: SnapshotVersion, Tables: map[string]*tableSnapshot{}, Dropped: map[string]*residueSnap{}, Views: m.views,
		EpochSet: m.epochSet, EpochN: m.epochN, Cleared: m.cleared, Owed: m.owed, DefaultsOwed: m.defaultsOwed, Logs: map[uint64]*logSnapshot{}, KV: m.kv, Seq: m.seq}
	for n, t := range m.tables {
		s.Tables[n] = snapTable(t)
	}
	for n, r := range m.dropped {
		s.Dropped[n] = &residueSnap{Keys: r.keys, Table: snapTable(r.table)}
	}
	for e, l := range m.logs {
		ls := &logSnapshot{Fence: l.fence, Gen: l.gen, Done: l.done, Progress: l.progress, Notes: l.notes, Aliases: l.aliases, Answered: l.answered, Open: l.open, Cursor: l.cursor, Queue: l.queue}
		for _, n := range l.inbox {
			ls.Inbox = append(ls.Inbox, noteSnapshot{ID: n.id, Note: n.note})
		}
		for _, ln := range l.lines {
			ls.Lines = append(ls.Lines, lineSnapshot{ID: ln.id, Line: ln.line})
		}
		s.Logs[e] = ls
	}
	return json.MarshalIndent(s, "", " ")
}

// Restore replaces the store's state with a snapshot's. A document that is
// not a snapshot of this version is refused and the store is left as it was.
//
// The document is read through the store row reader (memSnapshotRows): today's
// legacy JSON, the MemSnapshot table's binary image, padded standard base64 of
// that image, or the schema JSON text; a damaged document of any form is refused
// by name and counted (S2, S4).
func (m *Mem) Restore(doc []byte) error {
	s, _, err := memSnapshotRows.Read(doc)
	if err != nil {
		return fmt.Errorf("not a twin snapshot: %w", err)
	}
	return m.restoreSnapshot(s)
}

// restoreSnapshot replaces the store's state with a read document's. A document
// of another version is refused and the store is left as it was.
func (m *Mem) restoreSnapshot(s memSnapshot) error {
	if s.Version != SnapshotVersion {
		return fmt.Errorf("a twin snapshot of version %d; this build reads version %d", s.Version, SnapshotVersion)
	}
	tables, dropped, logs := map[string]*memTable{}, map[string]*memResidue{}, map[uint64]*memLog{}
	for n, t := range s.Tables {
		tables[n] = t.table()
	}
	for n, r := range s.Dropped {
		keys := r.Keys
		if keys == nil {
			keys = map[string]bool{}
		}
		dropped[n] = &memResidue{keys: keys, table: r.Table.table()}
	}
	for e, l := range s.Logs {
		ml := &memLog{fence: l.Fence, gen: l.Gen, done: l.Done, progress: l.Progress, notes: l.Notes, aliases: l.Aliases, answered: l.Answered, open: l.Open, cursor: l.Cursor, queue: l.Queue}
		if ml.done == nil {
			ml.done = map[string]string{}
		}
		if ml.progress == nil {
			ml.progress = map[string]time.Time{}
		}
		if ml.notes == nil {
			ml.notes = map[string]sprint.Note{}
		}
		if ml.open == nil {
			ml.open = map[string]string{}
		}
		for _, n := range l.Inbox {
			ml.inbox = append(ml.inbox, memNote{n.ID, n.Note})
		}
		for _, ln := range l.Lines {
			ml.lines = append(ml.lines, memLine{ln.ID, ln.Line})
		}
		logs[e] = ml
	}
	views, kv := s.Views, s.KV
	if views == nil {
		views = map[string]ntable.View{}
	}
	if kv == nil {
		kv = map[string]string{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tables, m.dropped, m.views, m.logs, m.kv = tables, dropped, views, logs, kv
	m.epochSet, m.epochN, m.cleared, m.owed, m.defaultsOwed, m.seq = s.EpochSet, s.EpochN, s.Cleared, s.Owed, s.DefaultsOwed, s.Seq
	return nil
}

// The snapshot document's dual read (schemas/snaprows/snaprows.schema; the owner's
// rules S1, S2 and S4). The twin's document and the pages its dump splits it into
// are VARIABLE tables of the snaprows area (S1: every list and text is open, as
// the restore copies them as it finds them). A reader accepts each record's
// table image (form byte 1), padded standard base64 of that image, the schema
// JSON text and today's legacy JSON, refuses damage by name and counts each read
// under the form read, through the store row reader (rowcodec.go). Writers still
// write the legacy JSON (S4: readers first); the image writer here is the form a
// later writer card emits and the proof's fixture.
//
// A table image is read by the generated load, written out as the schema JSON
// text, mapped field by field onto the legacy JSON tree (the inverse functions
// below, the schema's field-by-field account of each Go struct), and decoded by
// the legacy decoder, so one decoder makes every Go value whatever the form.

// snapVariableForm is the variable table's form byte (S1).
const snapVariableForm byte = 1

// snapOprowsUnread is the refusal of a table image whose log holds a record of
// the oprows area (the fence, the inbox, the lines, the notes, the queue): those
// records are oprows tables held as blobs (THE NESTED RECORDS OF ANOTHER AREA in
// snaprows.schema), and this reader does not decode them, so it refuses the
// image by name rather than read the record as absent (S2).
const snapOprowsUnread = "oprows_record_unread"

// snapImageUnmapped is the refusal of a table image whose schema JSON does not map
// onto the legacy record (S2).
const snapImageUnmapped = "image_unmapped"

// snapForms is one snaprows variable table: its measure, its image read out as
// the schema JSON text, and the schema JSON text written as its image (S1, S2).
// It is a function, not a record: nothing of it is serialized.
type snapForms func() (measure func([]byte) error, text, image func([]byte) ([]byte, RowReport))

// snapReport reduces a generated report to the row reader's (S2).
func snapReport(rep snapwire.TableReport, ok bool) RowReport {
	return RowReport{Unknown: int(rep.Unknown), KindMismatch: int(rep.KindMismatch), Clamped: int(rep.Clamped),
		Malformed: rep.Malformed || !ok, Duplicate: int(rep.Duplicate), Reason: rep.Reason}
}

// snapRefused says the report refuses the read; unknown fields alone are
// tolerated and counted (S2).
func snapRefused(r RowReport) bool {
	return r.Reason != "" || r.Malformed || r.Duplicate > 0 || r.KindMismatch > 0 || r.Clamped > 0
}

// snapVariable adapts one generated variable table (S1, S2).
func snapVariable[T any, B any](
	loadMeasure func([]byte) (int64, error),
	load func([]byte, []byte, *snapwire.TableReport) *T,
	jsonMeasure func(*T, ...snapwire.TableAllocator) int64,
	toJSON func(*T, []byte, ...snapwire.TableAllocator) int64,
	initB func(*B, ...snapwire.TableAllocator) bool,
	root func(*B) *T,
	arena func(*B) *snapwire.TableArena,
	fromJSON func(*B, []byte, *snapwire.TableReport) bool,
	measure func(*T, ...snapwire.TableWriteContext) int64,
	save func(*T, []byte, ...snapwire.TableWriteContext) int64,
) snapForms {
	text := func(w []byte) ([]byte, RowReport) {
		n, err := loadMeasure(w)
		if err != nil {
			return nil, RowReport{Malformed: true, Reason: err.Error()}
		}
		var rep snapwire.TableReport
		v := load(make([]byte, n), w, &rep)
		if rr := snapReport(rep, v != nil); snapRefused(rr) {
			return nil, rr
		}
		buf := make([]byte, jsonMeasure(v))
		k := toJSON(v, buf)
		if k < 0 {
			return nil, RowReport{Malformed: true}
		}
		return buf[:k], snapReport(rep, true)
	}
	image := func(t []byte) ([]byte, RowReport) {
		b := new(B)
		if !initB(b) {
			return nil, RowReport{Malformed: true}
		}
		var rep snapwire.TableReport
		ok := fromJSON(b, t, &rep)
		if rr := snapReport(rep, ok); snapRefused(rr) {
			return nil, rr
		}
		r, a := root(b), arena(b)
		m := measure(r, a)
		if m < 0 {
			return nil, RowReport{Malformed: true}
		}
		buf := make([]byte, m)
		k := save(r, buf, a)
		if k < 0 {
			return nil, RowReport{Malformed: true}
		}
		return buf[:k], snapReport(rep, true)
	}
	measure0 := func(w []byte) error { _, err := loadMeasure(w); return err }
	return func() (func([]byte) error, func([]byte) ([]byte, RowReport), func([]byte) ([]byte, RowReport)) {
		return measure0, text, image
	}
}

// snapListMarked says the schema JSON text holds key as a list: the table form
// writes each of those keys as a list, the legacy JSON as an object or not at
// all, so a JSON object without it is the legacy text (S2).
func snapListMarked(key string) func([]byte) bool {
	k := []byte(`"` + key + `"`)
	return func(in []byte) bool {
		for {
			i := bytes.Index(in, k)
			if i < 0 {
				return false
			}
			in = in[i+len(k):]
			rest := bytes.TrimLeft(in, " \t\r\n")
			if len(rest) == 0 || rest[0] != ':' {
				continue
			}
			rest = bytes.TrimLeft(rest[1:], " \t\r\n")
			if len(rest) > 0 && rest[0] == '[' {
				return true
			}
		}
	}
}

// snapFirstKey says the JSON object opens with key: the schema JSON text of a
// record whose legacy JSON opens with another key (S2).
func snapFirstKey(key string) func([]byte) bool {
	k := []byte(`"` + key + `"`)
	return func(in []byte) bool {
		t := bytes.TrimLeft(in, " \t\r\n")
		if len(t) == 0 || t[0] != '{' {
			return false
		}
		return bytes.HasPrefix(bytes.TrimLeft(t[1:], " \t\r\n"), k)
	}
}

// snapTree decodes JSON text as a tree with exact numbers.
func snapTree(text []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(text))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// snapMap runs a tree mapping; a tree whose shape the mapping does not expect
// (a null where a name is, a list where a map is) is an error, never a panic.
func snapMap(f func(any) (any, error), v any) (out any, err error) {
	defer func() {
		if r := recover(); r != nil {
			if r == snapOprowsUnread {
				out, err = nil, errors.New(snapOprowsUnread)
				return
			}
			out, err = nil, fmt.Errorf("%s: %v", snapImageUnmapped, r)
		}
	}()
	return f(v)
}

// snapRowFns reads one legacy record through a snaprows variable table: fwd maps
// the legacy JSON tree to the table's, inv the table's to the legacy's, marker
// recognises the schema JSON text, and decode is today's legacy decoder (S1, S2).
func snapRowFns[T any](name string, forms snapForms, marker func([]byte) bool, fwd, inv func(any) (any, error), decode func(*T, []byte) error) RowFns[T] {
	measure, text, image := forms()
	fromText := func(v *T, text []byte, rr RowReport) RowReport {
		tree, err := snapTree(text)
		if err != nil {
			return RowReport{Malformed: true, Reason: snapImageUnmapped}
		}
		legacy, err := snapMap(inv, tree)
		if err != nil {
			reason := snapImageUnmapped
			if err.Error() == snapOprowsUnread {
				reason = snapOprowsUnread
			}
			return RowReport{Reason: reason}
		}
		raw, err := json.Marshal(legacy)
		if err != nil {
			return RowReport{Reason: snapImageUnmapped}
		}
		if err := decode(v, raw); err != nil {
			return RowReport{Reason: snapImageUnmapped}
		}
		return rr
	}
	saveWire := func(v *T) []byte {
		raw, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		tree, err := snapTree(raw)
		if err != nil {
			return nil
		}
		out, err := snapMap(fwd, tree)
		if err != nil {
			return nil
		}
		js, err := json.Marshal(out)
		if err != nil {
			return nil
		}
		w, rr := image(js)
		if snapRefused(rr) {
			return nil
		}
		return w
	}
	return RowFns[T]{
		Name: name, Form: snapVariableForm,
		Reset:   func(v *T) { var zero T; *v = zero },
		Measure: measure,
		LoadWire: func(v *T, w []byte) RowReport {
			out, rr := text(w)
			if snapRefused(rr) {
				return rr
			}
			return fromText(v, out, rr)
		},
		LoadJSON: func(v *T, in []byte) RowReport {
			w, rr := image(in)
			if snapRefused(rr) {
				return rr
			}
			out, rr2 := text(w)
			if snapRefused(rr2) {
				return rr2
			}
			return fromText(v, out, rr)
		},
		Legacy: func(in []byte) bool {
			t := bytes.TrimLeft(in, " \t\r\n")
			return len(t) > 0 && t[0] == '{' && !marker(in)
		},
		LoadLegacy: func(v *T, in []byte) RowReport {
			if err := decode(v, in); err != nil {
				return RowReport{Malformed: true, Reason: "legacy_json_unreadable"}
			}
			return RowReport{}
		},
		SaveWire: saveWire,
		SaveJSON: func(v *T) []byte {
			w := saveWire(v)
			if w == nil {
				return nil
			}
			out, rr := text(w)
			if snapRefused(rr) {
				return nil
			}
			return out
		},
	}
}

// snapStrict is today's legacy decoder of the twin's document and its pages: an
// unknown field is refused, never dropped.
func snapStrict[T any](v *T, in []byte) error {
	d := json.NewDecoder(bytes.NewReader(in))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

// memSnapshotRows reads the twin's whole document (S1, S2, S4).
var memSnapshotRows = NewRowCodec(snapRowFns[memSnapshot]("MEMSNAPSHOT",
	snapVariable(snapwire.MemSnapshotLoadMeasureReason, snapwire.MemSnapshotLoad, snapwire.MemSnapshotToJsonMeasure, snapwire.MemSnapshotToJson,
		(*snapwire.MemSnapshotBuilder).Init, (*snapwire.MemSnapshotBuilder).GetRoot, func(b *snapwire.MemSnapshotBuilder) *snapwire.TableArena { return &b.Arena },
		snapwire.MemSnapshotFromJson, snapwire.MemSnapshotMeasure, snapwire.MemSnapshotSave),
	snapListMarked("tables"), fwdMemSnapshot, invMemSnapshot, snapStrict[memSnapshot]))

// ---- the tree helpers: the legacy JSON's maps are the tables' entry lists in key order ----

type snapObj = map[string]any

func snapNum(v int64) json.Number { return json.Number(fmt.Sprint(v)) }

// snapNs is a legacy time text as Unix nanoseconds UTC, 0 for the zero time.
func snapNs(v any) any {
	s, _ := v.(string)
	if s == "" {
		return snapNum(0)
	}
	tm, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic("a time that is not RFC 3339: " + s)
	}
	if tm.IsZero() {
		return snapNum(0)
	}
	return snapNum(tm.UnixNano())
}

// snapUnns is Unix nanoseconds as the legacy time text.
func snapUnns(v any) any {
	n, ok := v.(json.Number)
	if !ok {
		panic("a time that is not a number")
	}
	i, err := n.Int64()
	if err != nil {
		panic("a time that is not an integer")
	}
	if i == 0 {
		return time.Time{}.Format(time.RFC3339Nano)
	}
	return time.Unix(0, i).UTC().Format(time.RFC3339Nano)
}

func snapStr(v any) any {
	if v == nil {
		return ""
	}
	return v
}

func snapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func snapNumKeys(m map[string]any) []string {
	keys := snapKeys(m)
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.ParseUint(keys[i], 10, 64)
		b, _ := strconv.ParseUint(keys[j], 10, 64)
		return a < b
	})
	return keys
}

func snapMp(v any) map[string]any {
	if v == nil {
		return nil
	}
	return v.(map[string]any)
}

func snapLst(v any) []any {
	if v == nil {
		return nil
	}
	return v.([]any)
}

func snapPairs(v any, nameKey string) []any {
	out := []any{}
	m := snapMp(v)
	for _, k := range snapKeys(m) {
		out = append(out, snapObj{nameKey: k, "value": m[k]})
	}
	return out
}

func snapUnpairs(v any, nameKey string) any {
	l := snapLst(v)
	if len(l) == 0 {
		return nil
	}
	m := map[string]any{}
	for _, e := range l {
		m[e.(map[string]any)[nameKey].(string)] = e.(map[string]any)["value"]
	}
	return m
}

func snapWords(v any) []any {
	out := []any{}
	for _, s := range snapLst(v) {
		out = append(out, snapObj{"text": s})
	}
	return out
}

func snapUnwords(v any) any {
	l := snapLst(v)
	if len(l) == 0 {
		return nil
	}
	out := []any{}
	for _, e := range l {
		out = append(out, e.(map[string]any)["text"])
	}
	return out
}

func snapList(v any, f func(any) any) []any {
	out := []any{}
	for _, e := range snapLst(v) {
		out = append(out, f(e))
	}
	return out
}

func snapNilIfEmpty(l []any) any {
	if len(l) == 0 {
		return nil
	}
	return l
}

func snapFlags(v any) []any {
	out := []any{}
	m := snapMp(v)
	for _, k := range snapKeys(m) {
		out = append(out, snapObj{"name": k, "on": m[k]})
	}
	return out
}

func snapUnflags(v any) any {
	l := snapLst(v)
	if len(l) == 0 {
		return nil
	}
	m := map[string]any{}
	for _, e := range l {
		m[e.(map[string]any)["name"].(string)] = e.(map[string]any)["on"]
	}
	return m
}

func snapNumOr0(v any) any {
	if v == nil {
		return snapNum(0)
	}
	return v
}

func snapIsZero(v any) bool {
	n, _ := v.(json.Number)
	return n == "" || n == "0"
}

// ---- forward: the legacy JSON of the twin's records to the tables' JSON ----------

func fwdColumn(v any) any {
	c := snapMp(v)
	return snapObj{"name": c["Name"], "label": c["Label"], "projection": c["Projection"], "fold": c["Fold"], "width": c["Width"]}
}

func fwdCell(v any) any {
	c := snapMp(v)
	return snapObj{"key": c["Key"], "bound": c["Bound"], "count": c["Count"], "unread": c["Unread"], "unread_why": c["UnreadWhy"],
		"members": snapList(c["Members"], func(m any) any { return snapObj{"member": snapMp(m)["Member"], "score": snapMp(m)["Score"]} })}
}

func fwdRow(v any) any {
	r := snapMp(v)
	return snapObj{"key": r["Key"], "label": r["Label"], "exclude": r["Exclude"], "owner": r["Owner"], "hidden": r["Hidden"],
		"cells": snapList(r["Cells"], fwdCell), "texts": snapPairs(r["Texts"], "name")}
}

func fwdNTable(v any) any {
	d := snapMp(v)
	return snapObj{"epoch_key": d["EpochKey"], "epoch_field": d["EpochField"], "member_prefix": d["MemberPrefix"], "epoch": d["Epoch"], "revision": d["Revision"],
		"name": d["Name"], "columns": snapList(d["Columns"], fwdColumn), "footer_label": d["FooterLabel"], "hidden": snapWords(d["Hidden"]),
		"hidden_table": d["HiddenTable"], "sort": d["Sort"], "rows": snapList(d["Rows"], fwdRow), "props": snapPairs(d["Props"], "name")}
}

func fwdFieldChanges(v any) []any {
	out := []any{}
	m := snapMp(v)
	for _, k := range snapKeys(m) {
		f := snapMp(m[k])
		out = append(out, snapObj{"name": k, "before": f["before"], "after": f["after"], "before_bytes": snapNumOr0(f["before_bytes"]), "before_sha1": snapStr(f["before_sha1"]),
			"after_bytes": snapNumOr0(f["after_bytes"]), "after_sha1": snapStr(f["after_sha1"])})
	}
	return out
}

func fwdBatchMember(v any) any {
	b := snapMp(v)
	return snapObj{"id": b["id"], "before_place": b["before_place"], "after_place": b["after_place"], "before_score_text": b["before_score"], "after_score_text": b["after_score"],
		"before_rev": b["before_rev"], "after_rev": b["after_rev"], "fields_set": snapPairs(b["fields_set"], "name"), "fields_unset": snapWords(b["fields_unset"]),
		"fields": fwdFieldChanges(b["fields"])}
}

func fwdReceipt(v any) any {
	r := snapMp(v)
	var delta any
	if b := snapMp(r["BatchDelta"]); b != nil {
		delta = snapObj{"operation_id": b["operation_id"], "digest": b["digest"], "actor": b["actor"], "selected_count": b["selected_count"], "guard_count": b["guard_count"],
			"changed_count": b["changed_count"], "members": snapList(b["members"], fwdBatchMember), "props": snapPairs(b["props"], "name")}
	}
	return snapObj{"id": r["ID"], "epoch": r["Epoch"], "before": r["Before"], "after": r["After"], "outcome": r["Outcome"], "batch_delta": delta, "replay": r["Replay"]}
}

func fwdMember(v any) any {
	m := snapMp(v)
	return snapObj{"epoch": m["epoch"], "placed": m["placed"], "row": snapStr(m["row"]), "col": snapStr(m["col"]), "score": m["score"], "rev": m["rev"], "fields": snapPairs(m["fields"], "name")}
}

func fwdEpoch(v any) any {
	e := snapMp(v)
	texts := []any{}
	for _, row := range snapKeys(snapMp(e["texts"])) {
		texts = append(texts, snapObj{"row": row, "cells": snapPairs(snapMp(e["texts"])[row], "name")})
	}
	return snapObj{"rows": snapWords(e["rows"]), "hidden": snapFlags(e["hidden"]), "texts": texts, "props": snapPairs(e["props"], "name")}
}

func fwdTableSnapshot(v any) any {
	ts := snapMp(v)
	epochs, wrote, members, ops := []any{}, []any{}, []any{}, []any{}
	for _, k := range snapNumKeys(snapMp(ts["epochs"])) {
		n, _ := strconv.ParseUint(k, 10, 64)
		epochs = append(epochs, snapObj{"epoch": n, "snapshot": fwdEpoch(snapMp(ts["epochs"])[k])})
	}
	for _, k := range snapNumKeys(snapMp(ts["wrote"])) {
		n, _ := strconv.ParseUint(k, 10, 64)
		wrote = append(wrote, snapObj{"epoch": n, "on": snapMp(ts["wrote"])[k]})
	}
	for _, k := range snapKeys(snapMp(ts["members"])) {
		members = append(members, snapObj{"id": k, "member": fwdMember(snapMp(ts["members"])[k])})
	}
	for _, k := range snapKeys(snapMp(ts["ops"])) {
		o := snapMp(snapMp(ts["ops"])[k])
		ops = append(ops, snapObj{"key": k, "body": o["body"], "receipt": fwdReceipt(o["receipt"])})
	}
	changes := snapList(ts["changes"], func(c any) any {
		m := snapMp(c)
		return snapObj{"epoch": m["epoch"], "before": m["before"], "after": m["after"], "verb": m["verb"], "ids": snapWords(m["ids"])}
	})
	return snapObj{"def": fwdNTable(ts["def"]), "epochs": epochs, "wrote": wrote, "rev": ts["rev"], "members": members, "ops": ops, "changes": changes}
}

func fwdView(v any) any {
	w := snapMp(v)
	return snapObj{"name": w["Name"], "tables": snapWords(w["Tables"]), "title": w["Title"], "summary": w["Summary"], "state": w["State"]}
}

func fwdResidue(v any) any {
	r := snapMp(v)
	var ts any
	if r["table"] != nil {
		ts = fwdTableSnapshot(r["table"])
	}
	return snapObj{"keys": snapFlags(r["keys"]), "table_snapshot": ts}
}

// fwdLog maps a log; its oprows records are not written here (snapOprowsUnread).
func fwdLog(v any) any {
	l := snapMp(v)
	for _, k := range []string{"fence", "inbox", "lines", "notes", "queue"} {
		if l[k] != nil {
			panic(snapOprowsUnread)
		}
	}
	progress := []any{}
	for _, k := range snapKeys(snapMp(l["progress"])) {
		progress = append(progress, snapObj{"name": k, "at": snapNs(snapMp(l["progress"])[k])})
	}
	return snapObj{"fence": nil, "gen": l["gen"], "done": snapPairs(l["done"], "name"), "progress": progress, "inbox": []any{}, "lines": []any{}, "notes": []any{},
		"aliases": snapPairs(l["aliases"], "name"), "answered": snapPairs(l["answered"], "name"), "open": snapPairs(l["open"], "name"), "cursor": snapStr(l["cursor"]), "queue": []any{}}
}

func fwdMemSnapshot(v any) (any, error) {
	s := snapMp(v)
	tables, dropped, views, logs := []any{}, []any{}, []any{}, []any{}
	for _, k := range snapKeys(snapMp(s["tables"])) {
		tables = append(tables, snapObj{"name": k, "table_snapshot": fwdTableSnapshot(snapMp(s["tables"])[k])})
	}
	for _, k := range snapKeys(snapMp(s["dropped"])) {
		dropped = append(dropped, snapObj{"name": k, "residue": fwdResidue(snapMp(s["dropped"])[k])})
	}
	for _, k := range snapKeys(snapMp(s["views"])) {
		views = append(views, snapObj{"name": k, "view": fwdView(snapMp(s["views"])[k])})
	}
	for _, k := range snapNumKeys(snapMp(s["logs"])) {
		n, _ := strconv.ParseUint(k, 10, 64)
		logs = append(logs, snapObj{"epoch": n, "log": fwdLog(snapMp(s["logs"])[k])})
	}
	return snapObj{"version": s["version"], "tables": tables, "dropped": dropped, "views": views, "epoch_set": s["epoch_set"], "epoch": s["epoch"], "cleared": snapNs(s["cleared"]),
		"owed": s["owed"], "defaults_owed": snapNumOr0(s["defaults_owed"]), "logs": logs, "kv": snapPairs(s["kv"], "name"), "seq": s["seq"]}, nil
}

// ---- inverse: the tables' JSON to the legacy JSON of the twin's records ----------

func invColumn(v any) any {
	c := snapMp(v)
	return snapObj{"Name": c["name"], "Label": c["label"], "Projection": c["projection"], "Fold": c["fold"], "Width": c["width"]}
}

func invCell(v any) any {
	c := snapMp(v)
	return snapObj{"Key": c["key"], "Bound": c["bound"], "Count": c["count"], "Unread": c["unread"], "UnreadWhy": c["unread_why"],
		"Members": snapNilIfEmpty(snapList(c["members"], func(m any) any { return snapObj{"Member": snapMp(m)["member"], "Score": snapMp(m)["score"]} }))}
}

func invRow(v any) any {
	r := snapMp(v)
	return snapObj{"Key": r["key"], "Label": r["label"], "Exclude": r["exclude"], "Owner": r["owner"], "Hidden": r["hidden"],
		"Cells": snapNilIfEmpty(snapList(r["cells"], invCell)), "Texts": snapUnpairs(r["texts"], "name")}
}

func invNTable(v any) any {
	d := snapMp(v)
	return snapObj{"EpochKey": d["epoch_key"], "EpochField": d["epoch_field"], "MemberPrefix": d["member_prefix"], "Epoch": d["epoch"], "Revision": d["revision"],
		"Name": d["name"], "Columns": snapNilIfEmpty(snapList(d["columns"], invColumn)), "FooterLabel": d["footer_label"], "Hidden": snapUnwords(d["hidden"]),
		"HiddenTable": d["hidden_table"], "Sort": d["sort"], "Rows": snapNilIfEmpty(snapList(d["rows"], invRow)), "Props": snapUnpairs(d["props"], "name")}
}

func invBatchMember(m any) any {
	x := snapMp(m)
	fields := snapObj{}
	for _, e := range snapLst(x["fields"]) {
		f := snapMp(e)
		fc := snapObj{"before": f["before"], "after": f["after"]}
		if !snapIsZero(f["before_bytes"]) {
			fc["before_bytes"] = f["before_bytes"]
		}
		if !snapIsZero(f["after_bytes"]) {
			fc["after_bytes"] = f["after_bytes"]
		}
		if s, _ := f["before_sha1"].(string); s != "" {
			fc["before_sha1"] = s
		}
		if s, _ := f["after_sha1"].(string); s != "" {
			fc["after_sha1"] = s
		}
		fields[f["name"].(string)] = fc
	}
	set := snapUnpairs(x["fields_set"], "name")
	if set == nil {
		set = snapObj{}
	}
	unset := snapUnwords(x["fields_unset"])
	if unset == nil {
		unset = []any{}
	}
	return snapObj{"id": x["id"], "before_place": x["before_place"], "after_place": x["after_place"], "before_score": x["before_score_text"], "after_score": x["after_score_text"],
		"before_rev": x["before_rev"], "after_rev": x["after_rev"], "fields_set": set, "fields_unset": unset, "fields": fields}
}

func invReceipt(v any) any {
	r := snapMp(v)
	var delta any
	if b := snapMp(r["batch_delta"]); b != nil {
		delta = snapObj{"operation_id": b["operation_id"], "digest": b["digest"], "actor": b["actor"], "selected_count": b["selected_count"], "guard_count": b["guard_count"],
			"changed_count": b["changed_count"], "props": snapUnpairs(b["props"], "name"), "members": snapList(b["members"], invBatchMember)}
	}
	return snapObj{"ID": r["id"], "Epoch": r["epoch"], "Before": r["before"], "After": r["after"], "Outcome": r["outcome"], "BatchDelta": delta, "Replay": r["replay"]}
}

func invMember(v any) any {
	m := snapMp(v)
	mm := snapObj{"epoch": m["epoch"], "placed": m["placed"], "score": m["score"], "rev": m["rev"]}
	if s, _ := m["row"].(string); s != "" {
		mm["row"] = s
	}
	if s, _ := m["col"].(string); s != "" {
		mm["col"] = s
	}
	if f := snapUnpairs(m["fields"], "name"); f != nil {
		mm["fields"] = f
	}
	return mm
}

func invEpoch(v any) any {
	e := snapMp(v)
	texts := snapObj{}
	for _, r := range snapLst(e["texts"]) {
		row := snapMp(r)
		texts[row["row"].(string)] = snapUnpairs(row["cells"], "name")
	}
	out := snapObj{}
	if l := snapUnwords(e["rows"]); l != nil {
		out["rows"] = l
	}
	if h := snapUnflags(e["hidden"]); h != nil {
		out["hidden"] = h
	}
	if len(texts) > 0 {
		out["texts"] = texts
	}
	if p := snapUnpairs(e["props"], "name"); p != nil {
		out["props"] = p
	}
	return out
}

func invTableSnapshot(v any) any {
	ts := snapMp(v)
	out := snapObj{"def": invNTable(ts["def"]), "rev": ts["rev"]}
	epochs, wrote, members, ops := snapObj{}, snapObj{}, snapObj{}, snapObj{}
	for _, e := range snapLst(ts["epochs"]) {
		x := snapMp(e)
		epochs[fmt.Sprint(x["epoch"])] = invEpoch(x["snapshot"])
	}
	for _, e := range snapLst(ts["wrote"]) {
		x := snapMp(e)
		wrote[fmt.Sprint(x["epoch"])] = x["on"]
	}
	for _, e := range snapLst(ts["members"]) {
		x := snapMp(e)
		members[x["id"].(string)] = invMember(x["member"])
	}
	for _, e := range snapLst(ts["ops"]) {
		x := snapMp(e)
		ops[x["key"].(string)] = snapObj{"body": x["body"], "receipt": invReceipt(x["receipt"])}
	}
	for k, v := range map[string]snapObj{"epochs": epochs, "wrote": wrote, "members": members, "ops": ops} {
		if len(v) > 0 {
			out[k] = v
		}
	}
	if l := snapLst(ts["changes"]); len(l) > 0 {
		out["changes"] = snapList(l, func(c any) any {
			m := snapMp(c)
			ch := snapObj{"epoch": m["epoch"], "before": m["before"], "after": m["after"], "verb": m["verb"]}
			if ids := snapUnwords(m["ids"]); ids != nil {
				ch["ids"] = ids
			}
			return ch
		})
	}
	return out
}

func invView(v any) any {
	w := snapMp(v)
	return snapObj{"Name": w["name"], "Tables": snapUnwords(w["tables"]), "Title": w["title"], "Summary": w["summary"], "State": w["state"]}
}

func invResidue(v any) any {
	r := snapMp(v)
	res := snapObj{"table": nil}
	if r["table_snapshot"] != nil {
		res["table"] = invTableSnapshot(r["table_snapshot"])
	}
	if k := snapUnflags(r["keys"]); k != nil {
		res["keys"] = k
	}
	return res
}

// invLog maps a log back; an image that holds an oprows record is refused
// (snapOprowsUnread), never read with the record absent.
func invLog(v any) any {
	l := snapMp(v)
	if l["fence"] != nil {
		panic(snapOprowsUnread)
	}
	for _, k := range []string{"inbox", "lines", "notes", "queue"} {
		if len(snapLst(l[k])) > 0 {
			panic(snapOprowsUnread)
		}
	}
	lg := snapObj{"gen": l["gen"]}
	if d := snapUnpairs(l["done"], "name"); d != nil {
		lg["done"] = d
	}
	if p := snapLst(l["progress"]); len(p) > 0 {
		pr := snapObj{}
		for _, q := range p {
			pr[snapMp(q)["name"].(string)] = snapUnns(snapMp(q)["at"])
		}
		lg["progress"] = pr
	}
	for _, k := range []string{"aliases", "answered", "open"} {
		if m := snapUnpairs(l[k], "name"); m != nil {
			lg[k] = m
		}
	}
	if s, _ := l["cursor"].(string); s != "" {
		lg["cursor"] = s
	}
	return lg
}

func invMemSnapshot(v any) (any, error) {
	s := snapMp(v)
	tables, dropped, views, logs := snapObj{}, snapObj{}, snapObj{}, snapObj{}
	for _, e := range snapLst(s["tables"]) {
		x := snapMp(e)
		tables[x["name"].(string)] = invTableSnapshot(x["table_snapshot"])
	}
	for _, e := range snapLst(s["dropped"]) {
		x := snapMp(e)
		dropped[x["name"].(string)] = invResidue(x["residue"])
	}
	for _, e := range snapLst(s["views"]) {
		x := snapMp(e)
		views[x["name"].(string)] = invView(x["view"])
	}
	for _, e := range snapLst(s["logs"]) {
		x := snapMp(e)
		logs[fmt.Sprint(x["epoch"])] = invLog(x["log"])
	}
	out := snapObj{"version": s["version"], "tables": tables, "epoch_set": s["epoch_set"], "epoch": s["epoch"], "cleared": snapUnns(s["cleared"]), "owed": s["owed"], "seq": s["seq"]}
	if len(dropped) > 0 {
		out["dropped"] = dropped
	}
	if len(views) > 0 {
		out["views"] = views
	}
	if len(logs) > 0 {
		out["logs"] = logs
	}
	if !snapIsZero(s["defaults_owed"]) {
		out["defaults_owed"] = s["defaults_owed"]
	}
	if kv := snapUnpairs(s["kv"], "name"); kv != nil {
		out["kv"] = kv
	}
	return out, nil
}
