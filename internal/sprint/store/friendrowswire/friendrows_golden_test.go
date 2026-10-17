package friendrowswire

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
)

// clock is the test clock: 1000.0 seconds. A zero time stays zero, so a missed
// stamp cannot pass for this one.
const clock int64 = 1000

// friendRowsHelp is the run: text of a friendrows refusal.
const friendRowsHelp = "nova-sprint store friendrows"

// TestFriendRowsGolden pins the friendrows tables (S1) in both forms: each
// record round-trips in binary wire, padded standard base64 of that wire, and
// the schema JSON text (S2), through pkg/schemaread. A value past each
// declared bound, and a damaged image of each form, is refused by name. The
// strict reader returns the zero value; it does not hand back a clamped one.
// No table declares a bound: every value the code does not refuse round-trips
// whole in all three forms (openValues), and no value is refused past a bound.
func TestFriendRowsGolden(t *testing.T) {
	t.Parallel()
	// The one fixed table: two uint64 counters, bounded by their type alone.
	golden(t, row[Ticks]("Ticks", TicksReset, TicksMeasure, TicksSave, TicksLoad, TicksFromJson, TicksToJsonMeasure, TicksToJson, TicksTableType))

	// The variable tables: documents sized by their content.
	vars := []varRow{
		vrow("FriendEntry", FriendEntryTableType, FriendEntryLoadMeasureReason, FriendEntryLoad),
		vrow("FriendStatus", FriendStatusTableType, FriendStatusLoadMeasureReason, FriendStatusLoad),
		vrow("LoadSample", LoadSampleTableType, LoadSampleLoadMeasureReason, LoadSampleLoad),
		vrow("Holder", HolderTableType, HolderLoadMeasureReason, HolderLoad),
		vrow("Files", FilesTableType, FilesLoadMeasureReason, FilesLoad),
		vrow("Meter", MeterTableType, MeterLoadMeasureReason, MeterLoad),
		vrow("FriendReport", FriendReportTableType, FriendReportLoadMeasureReason, FriendReportLoad),
		vrow("LiveSight", LiveSightTableType, LiveSightLoadMeasureReason, LiveSightLoad),
		vrow("Beat", BeatTableType, BeatLoadMeasureReason, BeatLoad),
		vrow("ReaderHold", ReaderHoldTableType, ReaderHoldLoadMeasureReason, ReaderHoldLoad),
		vrow("HoldView", HoldViewTableType, HoldViewLoadMeasureReason, HoldViewLoad),
		vrow("LaneHold", LaneHoldTableType, LaneHoldLoadMeasureReason, LaneHoldLoad),
		vrow("LaneWait", LaneWaitTableType, LaneWaitLoadMeasureReason, LaneWaitLoad),
		vrow("Lane", LaneTableType, LaneLoadMeasureReason, LaneLoad),
		vrow("SeatChange", SeatChangeTableType, SeatChangeLoadMeasureReason, SeatChangeLoad),
		vrow("ServerRecord", ServerRecordTableType, ServerRecordLoadMeasureReason, ServerRecordLoad),
		vrow("Login", LoginTableType, LoginLoadMeasureReason, LoginLoad),
	}
	byName := make(map[string]varRow, len(vars))
	for _, g := range vars {
		variableGolden(t, g)
		byName[g.name] = g
	}
	onlyRealBounds(t, TicksTableType(), vars)
	longReasons(t, byName["SeatChange"], byName["HoldView"])
	intRanges(t, vars...)
	openValues(t, byName)
	longTimestamps(t, byName["LoadSample"])
}

// tableRow is one fixed table's generated functions. A unit that holds a
// variable table emits no fixed form (form byte 3) for any table, so a fixed
// table here rides the table wire (form byte 1): it keeps its fixed class,
// a bounded by-value body and its block form, and is an element of the
// variable records that hold it.
type tableRow[T comparable] struct {
	name          string
	reset         func(*T)
	measure       func(*T) int64
	save          func(*T, []byte) int64
	load          func(*T, []byte, *TableReport) bool
	fromJSON      func(*T, []byte, *TableReport) bool
	toJSONMeasure func(*T) int64
	toJSON        func(*T, []byte) int64
	table         func() *TableTypeInfo
}

func row[T comparable](
	name string,
	reset func(*T),
	measure func(*T) int64,
	save func(*T, []byte) int64,
	load func(*T, []byte, *TableReport) bool,
	fromJSON func(*T, []byte, *TableReport) bool,
	toJSONMeasure func(*T) int64,
	toJSON func(*T, []byte) int64,
	table func() *TableTypeInfo,
) tableRow[T] {
	return tableRow[T]{
		name: name, reset: reset, measure: measure, save: save, load: load,
		fromJSON: fromJSON, toJSONMeasure: toJSONMeasure, toJSON: toJSON,
		table: table,
	}
}

func golden[T comparable](t *testing.T, g tableRow[T]) {
	t.Helper()
	t.Run(g.name, func(t *testing.T) {
		t.Parallel()
		if g.table().Variable {
			t.Fatalf("%s is not a fixed table", g.name)
		}
		var sample T
		g.reset(&sample)
		fill(unsafe.Pointer(&sample), g.table())
		wire := g.wire(t, &sample)
		again := g.wire(t, &sample)
		if !bytes.Equal(wire, again) {
			t.Fatalf("two saves of one %s differ", g.name)
		}
		text := []byte(base64.StdEncoding.EncodeToString(wire))
		jsonText := g.json(t, &sample)
		deprecatedAliasesOmitted(t, g.table(), jsonText)

		got := g.read(t, wire, schemaread.FormBinary)
		if got != sample {
			t.Fatalf("%s binary round trip differs: sample=%+v got=%+v", g.name, sample, got)
		}
		got = g.read(t, text, schemaread.FormBase64)
		if got != sample {
			t.Fatalf("%s base64 round trip differs", g.name)
		}
		got = g.read(t, jsonText, schemaread.FormJSON)
		if got != sample {
			t.Fatalf("%s json round trip differs", g.name)
		}
		back := g.wire(t, &got)
		if !bytes.Equal(wire, back) {
			t.Fatalf("%s saved again after a read differs", g.name)
		}

		pastBounds(t, g.name, g.table(), jsonText, g.refuse)
		damaged(t, wire, schemaread.ReasonMalformed, g.refuse)
	})
}

func (g tableRow[T]) wire(t *testing.T, v *T) []byte {
	t.Helper()
	n := g.measure(v)
	if n < 1 {
		t.Fatalf("%s measure = %d", g.name, n)
	}
	buf := make([]byte, n)
	if g.save(v, buf) != n {
		t.Fatalf("%s save short", g.name)
	}
	if buf[0] != 1 {
		t.Fatalf("%s form byte = %d, want the table wire 1", g.name, buf[0])
	}
	return buf
}

func (g tableRow[T]) json(t *testing.T, v *T) []byte {
	t.Helper()
	n := g.toJSONMeasure(v)
	if n < 1 {
		t.Fatalf("%s json measure = %d", g.name, n)
	}
	buf := make([]byte, n)
	if g.toJSON(v, buf) != n {
		t.Fatalf("%s json short", g.name)
	}
	return buf
}

func (g tableRow[T]) reader() *schemaread.Reader[T] {
	return schemaread.NewReader(&schemaread.Codec[T]{
		Name:  g.name,
		Help:  friendRowsHelp,
		Reset: g.reset,
		LoadWire: func(v *T, wire []byte) schemaread.Report {
			var rep TableReport
			if !g.load(v, wire, &rep) && rep.Reason == "" && !rep.Malformed {
				rep.Malformed = true
			}
			return reportOf(&rep)
		},
		LoadJSON: func(v *T, text []byte) schemaread.Report {
			var rep TableReport
			if !g.fromJSON(v, text, &rep) && rep.Reason == "" && !rep.Malformed {
				rep.Malformed = true
			}
			return reportOf(&rep)
		},
	})
}

func deprecatedAliasesOmitted(t *testing.T, info *TableTypeInfo, text []byte) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(text, &object); err != nil {
		t.Fatalf("%s JSON: %v", info.Name, err)
	}
	for i := range info.Fields {
		f := &info.Fields[i]
		if !f.Optional {
			continue
		}
		for _, tag := range f.Tags {
			if tag == "deprecated" {
				if _, ok := object[f.Json]; ok {
					t.Errorf("%s emitted deprecated field %q at its default", info.Name, f.Json)
				}
			}
		}
	}
}

func reportOf(r *TableReport) schemaread.Report {
	return schemaread.Report{
		Unknown:      int(r.Unknown),
		KindMismatch: int(r.KindMismatch),
		Clamped:      int(r.Clamped),
		Malformed:    r.Malformed || r.Duplicate > 0,
		Reason:       r.Reason,
	}
}

func (g tableRow[T]) read(t *testing.T, in []byte, want schemaread.Form) T {
	t.Helper()
	got, res, err := g.reader().Read(in)
	if err != nil {
		t.Fatalf("%s read %s: %v", g.name, want, err)
	}
	if res.Form != want {
		t.Fatalf("%s form = %s, want %s", g.name, res.Form, want)
	}
	return got
}

func (g tableRow[T]) refuse(t *testing.T, in []byte, want string) {
	t.Helper()
	var zero T
	got, _, err := g.reader().Read(in)
	refusedAs(t, g.name, err, want)
	if got != zero {
		t.Fatalf("%s refused %s and returned a non-zero value", g.name, want)
	}
}

// refusedAs requires err to be a named refusal with the reason want.
func refusedAs(t *testing.T, name string, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s accepted a value that should be %s", name, want)
	}
	var ref *schemaread.Refusal
	if !errors.As(err, &ref) {
		t.Fatalf("%s err = %v, want a named refusal", name, err)
	}
	if ref.Reason != want {
		t.Fatalf("%s reason = %s, want %s", name, ref.Reason, want)
	}
}

// refuser reads in and requires a refusal named want.
type refuser func(t *testing.T, in []byte, want string)

// pastBounds walks every declared bound of a table, through nested tables,
// pointers and array and list elements, and refuses a JSON text one step
// past it by the name clamped. The generated writer refuses a value past a
// bound (invalid_value), so the step past is written as text. A list ([]T)
// and a pointer text (*string) declare no bound, so neither has a step past
// one.
func pastBounds(t *testing.T, name string, info *TableTypeInfo, text []byte, refuse refuser) {
	t.Helper()
	var walk func(info *TableTypeInfo, path []any)
	walk = func(info *TableTypeInfo, path []any) {
		for i := range info.Fields {
			f := info.Fields[i]
			at := append(append([]any{}, path...), f.Json)
			switch {
			case f.Counted && f.TypeName == "string" && !f.IsArray:
				refuseEdit(t, name, text, at, refuse, func(any) any { return strings.Repeat("a", int(f.ArrayBound)+1) })
			case f.Counted && f.IsArray && !f.List:
				refuseEdit(t, name, text, at, refuse, func(v any) any {
					list := v.([]any)
					for len(list) <= int(f.ArrayBound) {
						list = append(list, list[0])
					}
					return list
				})
			case f.HasRange:
				refuseEdit(t, name, text, at, refuse, func(any) any { return json.Number(fmt.Sprint(int64(f.RangeMin) - 1)) })
			}
			if f.Table != nil {
				if f.IsArray {
					at = append(at, 0)
				}
				walk(f.Table(), at)
			}
		}
	}
	walk(info, nil)
}

// refuseEdit replaces the value at path in the JSON text and requires the
// reader to refuse the result as clamped.
func refuseEdit(t *testing.T, name string, text []byte, path []any, refuse refuser, edit func(any) any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(text))
	dec.UseNumber()
	var top any
	if err := dec.Decode(&top); err != nil {
		t.Fatalf("%s json text: %v", name, err)
	}
	cur := top
	for _, k := range path[:len(path)-1] {
		switch key := k.(type) {
		case string:
			cur = cur.(map[string]any)[key]
		case int:
			cur = cur.([]any)[key]
		}
		if cur == nil {
			t.Fatalf("%s json text has no %v", name, path)
		}
	}
	m := cur.(map[string]any)
	last := path[len(path)-1].(string)
	if _, ok := m[last]; !ok {
		t.Fatalf("%s json text has no %v", name, path)
	}
	m[last] = edit(m[last])
	out, err := json.Marshal(top)
	if err != nil {
		t.Fatalf("%s json edit: %v", name, err)
	}
	refuse(t, out, schemaread.ReasonClamped)
}

// damaged refuses each damaged image of a table-wire record by name: a cut
// wire, a flipped trailer, an unknown or fixed form byte, cut base64, base64
// with a stray byte, and an unclosed JSON text. cut is the reason a damaged
// wire is refused by: a variable table's generated LoadMeasureReason names
// wire_damaged; a fixed table's table-wire Load, which has no measure, reports
// the damage as malformed.
func damaged(t *testing.T, wire []byte, cut string, refuse refuser) {
	t.Helper()
	text := []byte(base64.StdEncoding.EncodeToString(wire))
	refuse(t, wire[:4], cut)
	refuse(t, wire[:len(wire)-1], cut)
	flipped := bytes.Clone(wire)
	flipped[len(flipped)-1] ^= 0xff
	refuse(t, flipped, cut)
	fixed := bytes.Clone(wire)
	fixed[0] = TableFixedForm
	refuse(t, fixed, schemaread.ReasonUnknownForm)
	unknown := bytes.Clone(wire)
	unknown[0] = 4
	refuse(t, unknown, schemaread.ReasonUnknownForm)
	refuse(t, []byte(base64.StdEncoding.EncodeToString(wire[:len(wire)-1])), cut)
	refuse(t, append(append([]byte{}, text...), '!'), schemaread.ReasonMalformed)
	refuse(t, []byte("{"), schemaread.ReasonMalformed)
}

// fill writes an in-range non-zero value. Times are clock, never the zero time.
func fill(p unsafe.Pointer, info *TableTypeInfo) {
	for i := range info.Fields {
		f := &info.Fields[i]
		deprecated := false
		for _, tag := range f.Tags {
			deprecated = deprecated || tag == "deprecated"
		}
		if f.Optional {
			*(*bool)(unsafe.Add(p, f.PresentOffset)) = !deprecated
		}
		if deprecated {
			continue // the deprecated slot holds its default, as the writer does.
		}
		base := unsafe.Add(p, f.Offset)
		switch {
		case f.Table != nil && !f.IsArray:
			fill(base, f.Table())
		case f.IsArray && f.Counted:
			*(*int32)(unsafe.Add(p, f.CountOffset)) = 1
			if f.Table != nil {
				fill(base, f.Table())
			}
		default:
			fillScalar(p, base, f)
		}
	}
}

// fillScalar writes one in-range non-zero scalar or inline string.
func fillScalar(p, base unsafe.Pointer, f *TableFieldInfo) {
	switch f.TypeName {
	case "string":
		b := unsafe.Slice((*byte)(base), int(f.ArrayBound))
		b[0] = 'a'
		*(*int32)(unsafe.Add(p, f.CountOffset)) = 1
	case "bool":
		*(*bool)(base) = true
	case "int32":
		*(*int32)(base) = inInt32(f)
	case "uint32":
		*(*uint32)(base) = 1
	case "int64":
		*(*int64)(base) = clock
	case "uint64":
		*(*uint64)(base) = uint64(clock)
	case "float64":
		*(*float64)(base) = 1
	}
}

func inInt32(f *TableFieldInfo) int32 {
	if f.HasRange && f.RangeMin > 0 {
		return int32(f.RangeMin)
	}
	return 1
}

// varRow is one variable table's generated functions (SPEC-TABLES §2.2): its
// descriptor, its wire measure and its region load. The save and the JSON
// text go through the descriptor, as the generated Save and FromJson do.
type varRow struct {
	name    string
	info    *TableTypeInfo
	measure func([]byte) (int64, error)
	load    func(region, wire []byte, report *TableReport) unsafe.Pointer
}

func vrow[R any](name string, info func() *TableTypeInfo, measure func([]byte) (int64, error), load func([]byte, []byte, *TableReport) *R) varRow {
	return varRow{name: name, info: info(), measure: measure, load: func(region, wire []byte, rep *TableReport) unsafe.Pointer {
		return unsafe.Pointer(load(region, wire, rep))
	}}
}

// doc is a variable record a read hands back: the region it was loaded into
// and its root there. A refused read is the zero doc, a nil root.
type doc struct {
	region []byte
	root   unsafe.Pointer
}

// draft is a variable record under construction in an arena.
type draft struct {
	arena  TableArena
	worker TableWorker
	ref    TableRef
}

func (g varRow) draft(t *testing.T) *draft {
	t.Helper()
	d := &draft{}
	if !d.arena.Init() {
		t.Fatalf("%s arena init", g.name)
	}
	d.worker = TableWorker{Arena: &d.arena}
	p, ref := d.worker.Alloc(int64(g.info.Size))
	if p == nil {
		t.Fatalf("%s root alloc", g.name)
	}
	g.info.Reset(p)
	d.ref = ref
	t.Cleanup(d.arena.Shutdown)
	return d
}

func (d *draft) root() unsafe.Pointer { return d.arena.At(d.ref) }

// wire saves the draft; a value past a declared bound is refused by the
// writer and returns nil.
func (g varRow) wire(d *draft) []byte {
	n := tableRegionSave(d.root(), g.info, nil, true, &d.arena)
	if n < 1 {
		return nil
	}
	buf := make([]byte, n)
	if tableRegionSave(d.root(), g.info, buf, false, &d.arena) != n {
		return nil
	}
	return buf
}

// wireOf saves a doc a read returned, from its region.
func (g varRow) wireOf(t *testing.T, v doc) []byte {
	t.Helper()
	n := tableRegionSave(v.root, g.info, nil, true, nil)
	if n < 1 {
		t.Fatalf("%s measure of a read = %d", g.name, n)
	}
	buf := make([]byte, n)
	if tableRegionSave(v.root, g.info, buf, false, nil) != n {
		t.Fatalf("%s save of a read short", g.name)
	}
	return buf
}

func (g varRow) jsonOf(t *testing.T, v doc) []byte {
	t.Helper()
	n := tableRegionToJson(v.root, g.info, nil)
	if n < 1 {
		t.Fatalf("%s json measure = %d", g.name, n)
	}
	buf := make([]byte, n)
	if tableRegionToJson(v.root, g.info, buf) != n {
		t.Fatalf("%s json short", g.name)
	}
	return buf
}

// loadWire is the generated LoadMeasure and Load over the whole wire.
func (g varRow) loadWire(v *doc, wire []byte) schemaread.Report {
	var rep TableReport
	n, err := g.measure(wire)
	if err != nil || n < 1 {
		return schemaread.Report{Malformed: true}
	}
	region := make([]byte, n)
	root := g.load(region, wire, &rep)
	if root == nil && rep.Reason == "" && !rep.Malformed {
		rep.Malformed = true
	}
	*v = doc{region: region, root: root}
	return reportOf(&rep)
}

// loadJSON is the generated FromJson into an arena, then the wire into a
// region, so a doc is always a region whatever form it arrived in.
func (g varRow) loadJSON(v *doc, text []byte) schemaread.Report {
	var rep TableReport
	var arena TableArena
	if !arena.Init() {
		return schemaread.Report{Malformed: true}
	}
	defer arena.Shutdown()
	w := TableWorker{Arena: &arena}
	p, ref := w.Alloc(int64(g.info.Size))
	if p == nil {
		return schemaread.Report{Malformed: true}
	}
	g.info.Reset(p)
	if !tableRegionFromJson(arena.At(ref), g.info, &w, text, &rep) && rep.Reason == "" && !rep.Malformed {
		rep.Malformed = true
	}
	if r := reportOf(&rep); r.Malformed || r.Reason != "" || r.Clamped > 0 || r.KindMismatch > 0 {
		return r
	}
	n := tableRegionSave(arena.At(ref), g.info, nil, true, &arena)
	if n < 1 {
		return schemaread.Report{Malformed: true}
	}
	buf := make([]byte, n)
	tableRegionSave(arena.At(ref), g.info, buf, false, &arena)
	return g.loadWire(v, buf)
}

func (g varRow) reader() *schemaread.Reader[doc] {
	return schemaread.NewReader(&schemaread.Codec[doc]{
		Name:  g.name,
		Help:  friendRowsHelp,
		Reset: func(v *doc) { *v = doc{} },
		Measure: func(wire []byte) error {
			_, err := g.measure(wire)
			return err
		},
		LoadWire: g.loadWire,
		LoadJSON: g.loadJSON,
	})
}

func (g varRow) read(t *testing.T, in []byte, want schemaread.Form) doc {
	t.Helper()
	got, res, err := g.reader().Read(in)
	if err != nil {
		t.Fatalf("%s read %s: %v", g.name, want, err)
	}
	if res.Form != want {
		t.Fatalf("%s form = %s, want %s", g.name, res.Form, want)
	}
	if got.root == nil {
		t.Fatalf("%s read %s returned no record", g.name, want)
	}
	return got
}

func (g varRow) refuse(t *testing.T, in []byte, want string) {
	t.Helper()
	got, _, err := g.reader().Read(in)
	refusedAs(t, g.name, err, want)
	if got.root != nil || got.region != nil {
		t.Fatalf("%s refused %s and returned a record", g.name, want)
	}
}

// roundTrip reads wire in all three forms and requires each read to save
// back to the same wire; it returns the binary read.
func (g varRow) roundTrip(t *testing.T, wire []byte) doc {
	t.Helper()
	if wire == nil {
		t.Fatalf("%s writer refused an in-range record", g.name)
	}
	if wire[0] != 1 {
		t.Fatalf("%s form byte = %d, want the variable form 1", g.name, wire[0])
	}
	got := g.read(t, wire, schemaread.FormBinary)
	if !bytes.Equal(g.wireOf(t, got), wire) {
		t.Fatalf("%s binary round trip differs", g.name)
	}
	text := []byte(base64.StdEncoding.EncodeToString(wire))
	if b := g.read(t, text, schemaread.FormBase64); !bytes.Equal(g.wireOf(t, b), wire) {
		t.Fatalf("%s base64 round trip differs", g.name)
	}
	jsonText := g.jsonOf(t, got)
	if j := g.read(t, jsonText, schemaread.FormJSON); !bytes.Equal(g.wireOf(t, j), wire) {
		t.Fatalf("%s json round trip differs", g.name)
	}
	return got
}

func variableGolden(t *testing.T, g varRow) {
	t.Helper()
	t.Run(g.name, func(t *testing.T) {
		t.Parallel()
		if !g.info.Variable {
			t.Fatalf("%s is not a variable table", g.name)
		}
		d := g.draft(t)
		vfill(t, &d.worker, d.root(), g.info)
		wire := g.wire(d)
		if again := g.wire(d); !bytes.Equal(wire, again) {
			t.Fatalf("two saves of one %s differ", g.name)
		}
		got := g.roundTrip(t, wire)
		pastBounds(t, g.name, g.info, g.jsonOf(t, got), g.refuse)
		damaged(t, wire, schemaread.ReasonWireDamaged, g.refuse)
	})
}

// vfill writes an in-range non-zero value into an arena record: one element
// in each list, a one-byte text behind each pointer, and fill's scalars.
func vfill(t *testing.T, w *TableWorker, p unsafe.Pointer, info *TableTypeInfo) {
	t.Helper()
	for i := range info.Fields {
		f := &info.Fields[i]
		deprecated := false
		for _, tag := range f.Tags {
			deprecated = deprecated || tag == "deprecated"
		}
		if f.Optional {
			*(*bool)(unsafe.Add(p, f.PresentOffset)) = !deprecated
		}
		if deprecated {
			continue // the deprecated slot holds its default, as the writer does.
		}
		base := unsafe.Add(p, f.Offset)
		switch {
		case f.Pointer && f.Table == nil:
			TableStringEmplace(w, (*TableRef)(base), []byte("a"))
		case f.Pointer:
			q, ref := w.Alloc(int64(f.Table().Size))
			if q == nil {
				t.Fatalf("%s.%s alloc", info.Name, f.Name)
			}
			f.Table().Reset(q)
			vfill(t, w, q, f.Table())
			*(*TableRef)(base) = ref
		case f.TextList != 0:
			e := tableContainerAppend(w, (*tableContainer)(base), int64(f.ElemSize))
			if e == nil {
				t.Fatalf("%s.%s append", info.Name, f.Name)
			}
			TableStringEmplace(w, (*TableRef)(e), []byte("a"))
		case f.List:
			e := tableContainerAppend(w, (*tableContainer)(base), int64(f.ElemSize))
			if e == nil {
				t.Fatalf("%s.%s append", info.Name, f.Name)
			}
			f.Table().Reset(e)
			vfill(t, w, e, f.Table())
		case f.Table != nil && !f.IsArray:
			vfill(t, w, base, f.Table())
		case f.IsArray && f.Counted:
			*(*int32)(unsafe.Add(p, f.CountOffset)) = 1
			if f.Table != nil {
				vfill(t, w, base, f.Table())
			}
		default:
			fillScalar(p, base, f)
		}
	}
}

// longReasons pins unbounded reasons at 70 KB through every supported form.
func longReasons(t *testing.T, seat, hold varRow) {
	t.Run("SeatChangeReason70KB", func(t *testing.T) {
		t.Parallel()
		reason := bytes.Repeat([]byte("seat reason "), 70*1024/12+1)[:70*1024]
		d := seat.draft(t)
		s := (*SeatChange)(d.root())
		s.Generation = 7
		TableStringEmplace(&d.worker, &s.Reason, reason)
		got := (*SeatChange)(seat.roundTrip(t, seat.wire(d)).root)
		v := TableStringAt(&got.Reason)
		if v.Length != int64(len(reason)) || !bytes.Equal(v.Data[:v.Length], reason) {
			t.Fatalf("reason after a read is %d bytes, want %d", v.Length, len(reason))
		}
	})
	t.Run("HoldViewReason70KB", func(t *testing.T) {
		t.Parallel()
		d := hold.draft(t)
		text := hold.jsonOf(t, doc{root: d.root()})
		reason := strings.Repeat("r", 70*1024)
		text = replaceJSONField(t, text, "reason", reason)
		fromJSON := hold.read(t, text, schemaread.FormJSON)
		assertStringField(t, hold.info, fromJSON.root, "reason", reason)
		wire := hold.wireOf(t, fromJSON)
		for _, in := range [][]byte{wire, []byte(base64.StdEncoding.EncodeToString(wire)), hold.jsonOf(t, fromJSON)} {
			form := schemaread.FormBinary
			if bytes.Equal(in, []byte(base64.StdEncoding.EncodeToString(wire))) {
				form = schemaread.FormBase64
			}
			if len(in) > 0 && in[0] == '{' {
				form = schemaread.FormJSON
			}
			got := hold.read(t, in, form)
			assertStringField(t, hold.info, got.root, "reason", reason)
		}
	})
}

func replaceJSONField(t *testing.T, text []byte, key string, value any) []byte {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(text, &obj); err != nil {
		t.Fatal(err)
	}
	obj[key] = value
	out, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func assertStringField(t *testing.T, info *TableTypeInfo, p unsafe.Pointer, name, want string) {
	t.Helper()
	for i := range info.Fields {
		f := &info.Fields[i]
		if f.Json == name {
			v := TableStringAt((*TableRef)(unsafe.Add(p, f.Offset)))
			if string(v.Data[:v.Length]) != want {
				t.Fatalf("%s.%s length/value mismatch", info.Name, name)
			}
			return
		}
	}
	t.Fatalf("%s has no %s field", info.Name, name)
}

func intRanges(t *testing.T, rows ...varRow) {
	t.Helper()
	wantFields := map[string][]string{"FriendEntry": {"width"}, "Holder": {"pid", "open"}, "Files": {"open", "max", "warn", "alarm"}, "FriendReport": {"working", "queue", "width", "stop_returns", "paced"}, "Beat": {"cores", "stop_returns"}, "ServerRecord": {"pid"}}
	byName := make(map[string]varRow, len(rows))
	for _, r := range rows {
		byName[r.name] = r
	}
	for name, fields := range wantFields {
		g, ok := byName[name]
		if !ok {
			t.Fatalf("missing row %s", name)
		}
		for _, field := range fields {
			found := false
			for i := range g.info.Fields {
				f := &g.info.Fields[i]
				if f.Json == field {
					found = true
					if f.TypeName != "int64" {
						t.Errorf("%s.%s type = %s, want int64", name, field, f.TypeName)
					}
					break
				}
			}
			if !found {
				t.Errorf("%s missing field %s", name, field)
			}
		}
	}
	for _, tc := range []struct {
		row, field string
		value      int64
	}{{"Files", "max", math.MaxInt64}, {"FriendReport", "working", 2147483648}, {"FriendEntry", "width", 2147483648}} {
		g := byName[tc.row]
		d := g.draft(t)
		vfill(t, &d.worker, d.root(), g.info)
		text := replaceJSONField(t, g.jsonOf(t, doc{root: d.root()}), tc.field, json.Number(strconv.FormatInt(tc.value, 10)))
		got := g.read(t, text, schemaread.FormJSON)
		if n := int64Field(t, g.info, got.root, tc.field); n != tc.value {
			t.Errorf("%s.%s = %d, want %d", tc.row, tc.field, n, tc.value)
		}
		wire := g.wireOf(t, got)
		for _, in := range [][]byte{wire, []byte(base64.StdEncoding.EncodeToString(wire))} {
			form := schemaread.FormBinary
			if !bytes.Equal(in, wire) {
				form = schemaread.FormBase64
			}
			again := g.read(t, in, form)
			if n := int64Field(t, g.info, again.root, tc.field); n != tc.value {
				t.Errorf("%s.%s after %s = %d", tc.row, tc.field, form, n)
			}
		}
	}
}

func int64Field(t *testing.T, info *TableTypeInfo, p unsafe.Pointer, name string) int64 {
	t.Helper()
	for i := range info.Fields {
		f := &info.Fields[i]
		if f.Json == name {
			return *(*int64)(unsafe.Add(p, f.Offset))
		}
	}
	t.Fatalf("%s missing %s", info.Name, name)
	return 0
}

// onlyRealBounds requires that no table declares a bound: FriendStatus.status
// is open text because the reader (FriendRows) shows any word outside
// FriendStates as down and accepts any length (store/friends.go). A fixed
// array, a counted text or a numeric range anywhere is a bound some path does
// not enforce.
func onlyRealBounds(t *testing.T, fixed *TableTypeInfo, rows []varRow) {
	t.Helper()
	found := map[string]int{}
	seen := map[string]bool{}
	var walk func(info *TableTypeInfo)
	walk = func(info *TableTypeInfo) {
		if seen[info.Name] {
			return
		}
		seen[info.Name] = true
		for i := range info.Fields {
			f := &info.Fields[i]
			switch {
			case f.Counted && f.TypeName == "string" && !f.IsArray:
				found[info.Name+"."+f.Json] = int(f.ArrayBound)
			case f.Counted && f.IsArray && !f.List:
				found[info.Name+"."+f.Json] = int(f.ArrayBound)
			case f.HasRange:
				found[info.Name+"."+f.Json] = -1
			}
			if f.Table != nil {
				walk(f.Table())
			}
		}
	}
	walk(fixed)
	for _, r := range rows {
		walk(r.info)
	}
	want := map[string]int{}
	if len(found) != len(want) {
		t.Errorf("declared bounds = %v, want only %v", found, want)
	}
	for k, v := range want {
		if found[k] != v {
			t.Errorf("declared bounds = %v, want %s = %d", found, k, v)
		}
	}
}

// longTime is an RFC3339Nano text past the 35 bytes a time.Time marshals to:
// time.Time accepts any count of fraction digits.
var longTime = "2026-10-17T01:02:03." + strings.Repeat("123456789", 8) + "Z"

// longText returns n bytes of one letter.
func longText(n int) string { return strings.Repeat("a", n) }

// longEntries returns n copies of the object.
func longTextEntries(n int, text string) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = text
	}
	return out
}

func longEntries(n int, obj map[string]any) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = obj
	}
	return out
}

// openValues pins a value past every removed bound in all three forms: each
// case is read as JSON text, saved as wire, read back from the binary wire,
// the padded standard base64 and the JSON text, and each read must hold the
// values it was given. The cases are the removed caps: Beat.samples past 11,
// live past 512, top holders past 10, reason past
// 1024, a negative paced, token_cap, working, queue and stop_returns, billing
// past 16, and every string(35) time past 35 bytes, and the texts of the
// tables that were fixed.
func openValues(t *testing.T, rows map[string]varRow) {
	t.Helper()
	long := longText(5000)
	sample := map[string]any{"pct": 1.5, "at": longTime}
	cases := []struct {
		name string
		in   map[string]any
	}{
		{"FriendEntry", map[string]any{"at": longTime, "until": longTime, "by": longText(300), "mode": longText(40), "pronouns": longText(40), "session": longText(40), "billing": longText(100), "reason": long, "token_cap": json.Number("-5"), "token_cap_set": true, "width": json.Number("-3")}},
		{"FriendStatus", map[string]any{"status": longText(40), "at": longTime, "until": longTime, "reason": long, "evidence": long}},
		{"LoadSample", sample},
		{"FriendReport", map[string]any{
			"running": []any{longText(4097), longText(70 * 1024)},
			"working": json.Number("-1"), "queue": json.Number("-1"), "stop_returns": json.Number("-1"), "paced": json.Number("-1"), "width": json.Number("-1"),
			"reason": longText(1025), "active": longTime, "until": longTime, "started": longTime, "present": longTime,
		}},
		{"LiveSight", map[string]any{"entry": longText(400), "at": longTime}},
		{"LaneHold", map[string]any{"who": longText(300), "claimed": true, "since": longTime, "seen": longTime}},
		{"LaneWait", map[string]any{"who": longText(300), "since": longTime, "seen": longTime}},
		{"Lane", map[string]any{
			"holders": longEntries(3, map[string]any{"who": longText(300), "claimed": true, "since": longTime, "seen": longTime}),
			"queue":   longEntries(3, map[string]any{"who": longText(300), "since": longTime, "seen": longTime}),
		}},
		{"Beat", map[string]any{
			"at": longTime, "how": longText(40), "live_since": longTime, "draining_since": longTime,
			"stop_returns": json.Number("-1"),
			"samples":      longEntries(12, sample),
			"live":         longTextEntries(513, longText(400)),
			"facts":        longTextEntries(3, longText(200)),
			"live_seen":    longEntries(3, map[string]any{"entry": longText(400), "at": longTime}),
			"meter":        map[string]any{"top_at": longTime, "files": map[string]any{"at": longTime, "top_at": longTime, "top": longEntries(11, map[string]any{"pid": json.Number("1"), "cmd": "x", "user": "u", "open": json.Number("1")})}},
			"friend":       map[string]any{"running": longTextEntries(2, longText(4097)), "paced": json.Number("-1"), "reason": longText(1025)},
		}},
		{"ReaderHold", map[string]any{"at": longTime, "by": longText(300)}},
		{"HoldView", map[string]any{"kind": longText(40), "name": longText(300), "at": longText(100), "by": longText(300), "who": longText(300)}},
		{"SeatChange", map[string]any{"holder": longText(300), "from": longText(300), "at": longTime, "by": longText(300), "approved_by": longText(300)}},
		{"ServerRecord", map[string]any{"actor": longText(300), "at": longTime, "started": longTime}},
	}
	for _, c := range cases {
		g, ok := rows[c.name]
		if !ok {
			t.Fatalf("no row %s", c.name)
		}
		t.Run("Open"+c.name, func(t *testing.T) {
			t.Parallel()
			text, err := json.Marshal(c.in)
			if err != nil {
				t.Fatal(err)
			}
			first := g.read(t, text, schemaread.FormJSON)
			got := g.roundTrip(t, g.wireOf(t, first))
			want := decodeJSON(t, text)
			have := decodeJSON(t, g.jsonOf(t, got))
			if why := subset(want, have); why != "" {
				t.Fatalf("%s: %s", g.name, why)
			}
		})
	}
}

func decodeJSON(t *testing.T, text []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(text))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("json text: %v", err)
	}
	return v
}

// subset says why have does not hold every value of want ("" when it does).
func subset(want, have any) string {
	switch w := want.(type) {
	case map[string]any:
		h, ok := have.(map[string]any)
		if !ok {
			return fmt.Sprintf("want an object, have %T", have)
		}
		for k, v := range w {
			hv, ok := h[k]
			if !ok {
				return fmt.Sprintf("field %q lost", k)
			}
			if why := subset(v, hv); why != "" {
				return k + ": " + why
			}
		}
		return ""
	case []any:
		h, ok := have.([]any)
		if !ok || len(h) != len(w) {
			return fmt.Sprintf("want a list of %d, have %d", len(w), len(h))
		}
		for i := range w {
			if why := subset(w[i], h[i]); why != "" {
				return fmt.Sprintf("[%d]: %s", i, why)
			}
		}
		return ""
	}
	if fmt.Sprint(want) != fmt.Sprint(have) {
		return fmt.Sprintf("value %.40v changed to %.40v", want, have)
	}
	return ""
}

// longTimestamps pins that a time.Time source value longer than 35 bytes
// reaches the table whole: the source decoder accepts it and the open text
// keeps it, in every form.
func longTimestamps(t *testing.T, g varRow) {
	t.Run("LongTimestamps", func(t *testing.T) {
		t.Parallel()
		var source time.Time
		if err := json.Unmarshal([]byte(strconv.Quote(longTime)), &source); err != nil {
			t.Fatalf("time.Time refused a long RFC3339 value: %v", err)
		}
		if len(longTime) <= 35 {
			t.Fatalf("the long time is %d bytes", len(longTime))
		}
		text := []byte(`{"at":"` + longTime + `","pct":1}`)
		got := g.roundTrip(t, g.wireOf(t, g.read(t, text, schemaread.FormJSON)))
		if v := stringOf(t, g.info, got.root, "at"); v != longTime {
			t.Fatalf("at = %q, want %q", v, longTime)
		}
		// the table keeps source text and does not judge its time domain.
		for _, odd := range []string{"10000-01-01T00:00:00Z", "2025-02-30T00:00:00Z"} {
			in := []byte(`{"at":"` + odd + `","pct":1}`)
			if _, _, err := g.reader().Read(in); err != nil {
				t.Errorf("open text refused %q: %v", odd, err)
			}
		}
	})
}

func stringOf(t *testing.T, info *TableTypeInfo, p unsafe.Pointer, name string) string {
	t.Helper()
	for i := range info.Fields {
		f := &info.Fields[i]
		if f.Json == name {
			v := TableStringAt((*TableRef)(unsafe.Add(p, f.Offset)))
			return string(v.Data[:v.Length])
		}
	}
	t.Fatalf("%s has no %s field", info.Name, name)
	return ""
}
