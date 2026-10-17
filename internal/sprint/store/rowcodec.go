package store

import (
	"bytes"
	"encoding/json"
	"sync"

	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
)

// The one store row reader. A record card adopts it by passing a generated
// fixed table's load and measure functions and the record's legacy JSON
// decoder. The done record adopts it in done.go; other records still have
// their own paths until their cards. Reads go through pkg/schemaread, which detects the form,
// refuses damage by name and counts each read once under the form read.
//
// S1: a server-owned record is a fixed table (form byte 3).
// S2: a reader accepts the fixed image, padded standard base64 of that image,
// and the schema JSON text, and today's legacy JSON when the caller
// recognises it. A damaged value is `<NAME> REFUSED: <reason>; run:
// nova-sprint check`.
// S4: the per-form counter is the evidence a later card reads before that
// record's writer switches. Writers do not switch here.

// rowCheckHelp is the command a damaged store row points at. S2.
const rowCheckHelp = "nova-sprint check"

// fixedFormByte is the fixed table's form byte, the generated TableFixedForm. S1.
const fixedFormByte byte = 3

// RowReport is the slice of a generated table report the strict read decides
// on. Duplicate text keys are malformed: a key given twice is ambiguous and
// is never last-wins. S2.
type RowReport struct {
	Unknown      int
	KindMismatch int
	Clamped      int
	Malformed    bool
	Duplicate    int
	Reason       string
}

// schema is the strict-read report. A duplicate key refuses the read.
func (r RowReport) schema() schemaread.Report {
	return schemaread.Report{
		Unknown:      r.Unknown,
		KindMismatch: r.KindMismatch,
		Clamped:      r.Clamped,
		Malformed:    r.Malformed || r.Duplicate > 0,
		Reason:       r.Reason,
	}
}

// RowFns are one record's functions. The generated fixed-table load takes a
// plan and a generated report this package does not name, so the record card
// adapts those into LoadWire and LoadJSON. Measure is the generated measure
// of one wire image; an error's text is the refusal reason. Nil Measure lets
// the load decide.
type RowFns[T any] struct {
	Name string // the refusal prefix, the record's name

	// Form is the fixed form byte. Zero means fixedFormByte. S1.
	Form byte
	// Forms replaces Form when set. It lets a record's load inspect every wire
	// form byte and preserve its existing refusal reasons. S2.
	Forms []byte

	Reset       func(*T)
	Measure     func([]byte) error
	LoadWire    func(*T, []byte) RowReport
	LoadJSON    func(*T, []byte) RowReport
	Legacy      func([]byte) bool // today's legacy JSON; nil means an object is schema JSON. S2.
	LoadLegacy  func(*T, []byte) RowReport
	Constraints func(*T, *schemaread.Checker)
	SaveWire    func(*T) []byte
	SaveJSON    func(*T) []byte
}

// RowCodec reads and counts one store record.
type RowCodec[T any] struct {
	name string
	r    *schemaread.Reader[T]
}

// NewRowCodec reads fns through the strict read. The refusal names fns.Name
// and points at nova-sprint check. S1, S2.
func NewRowCodec[T any](fns RowFns[T]) *RowCodec[T] {
	form := fns.Form
	if form == 0 {
		form = fixedFormByte
	}
	forms := fns.Forms
	if len(forms) == 0 {
		forms = []byte{form}
	}
	load := func(fn func(*T, []byte) RowReport) func(*T, []byte) schemaread.Report {
		return func(v *T, in []byte) schemaread.Report {
			if fn == nil {
				return schemaread.Report{Malformed: true}
			}
			return fn(v, in).schema()
		}
	}
	c := &RowCodec[T]{name: fns.Name}
	c.r = schemaread.NewReader(&schemaread.Codec[T]{
		Name:        fns.Name,
		Help:        rowCheckHelp,
		Reset:       fns.Reset,
		Forms:       forms,
		Measure:     fns.Measure,
		LoadWire:    load(fns.LoadWire),
		LoadJSON:    load(fns.LoadJSON),
		Legacy:      fns.Legacy,
		LoadLegacy:  load(fns.LoadLegacy),
		Constraints: fns.Constraints,
		SaveWire:    fns.SaveWire,
		SaveJSON:    fns.SaveJSON,
	})
	return c
}

// Read detects the form of in, loads it and counts the read once under that
// form. A refusal is a *schemaread.Refusal and the value is zero. S2.
func (c *RowCodec[T]) Read(in []byte) (T, schemaread.Form, error) {
	v, res, err := c.r.Read(in)
	return v, res.Form, err
}

// Write checks v and emits it as form: the fixed image, padded standard
// base64 of that image, or the schema JSON text. Legacy is read only.
// Writers of records do not switch here; this is the form a later writer
// card emits. S2, S4.
func (c *RowCodec[T]) Write(v *T, form schemaread.Form) ([]byte, error) {
	return c.r.Write(v, form)
}

// Counts is the per-form read counter for this record. S2, S4.
func (c *RowCodec[T]) Counts() *schemaread.Counts { return c.r.Counts() }

func (c *RowCodec[T]) rowName() string {
	if c == nil {
		return ""
	}
	return c.name
}

func (c *RowCodec[T]) rowSnapshot() map[string]uint64 {
	if c == nil || c.r == nil {
		return map[string]uint64{}
	}
	return c.r.Counts().Snapshot()
}

// rowSource is one registered record's counter.
type rowSource interface {
	rowName() string
	rowSnapshot() map[string]uint64
}

// rowReg is the readers one store Check counts. It is keyed by the store
// pointer so one test's records are not another's.
type rowReg struct {
	mu   sync.Mutex
	list []rowSource
}

// rowRegs holds each store's readers. A store that never registers has no entry.
var rowRegs sync.Map

// Register counts this reader on st's CheckReport. S4: the per-form counter
// is the evidence Check exports. A record card calls it when the store is
// built.
func (c *RowCodec[T]) Register(st *Store) {
	if c == nil || st == nil {
		return
	}
	v, _ := rowRegs.LoadOrStore(st, &rowReg{})
	r := v.(*rowReg)
	r.mu.Lock()
	for _, registered := range r.list {
		if registered == c {
			r.mu.Unlock()
			return
		}
	}
	r.list = append(r.list, c)
	r.mu.Unlock()
}

// rowFormCounts is the per-record snapshots Check puts on its report. An
// unregistered store is an empty object, never nil, so check --json always
// prints row_forms. S4.
func (st *Store) rowFormCounts() map[string]map[string]uint64 {
	linkRowReader()
	out := map[string]map[string]uint64{}
	if st == nil {
		return out
	}
	// S4: a fresh Store lists DONERECORD at zero before the first page read.
	st.doneRows()
	v, ok := rowRegs.Load(st)
	if !ok {
		return out
	}
	r := v.(*rowReg)
	r.mu.Lock()
	list := append([]rowSource(nil), r.list...)
	r.mu.Unlock()
	for _, s := range list {
		out[s.rowName()] = s.rowSnapshot()
	}
	return out
}

// linkRow is one empty value, so check can reach the reader before any record
// registers. Nothing of it is stored.
type linkRow struct{}

var linkRowOnce sync.Once

// linkRowReader reaches the reader's entry points from check. No stored record
// is read here; each adopting card registers its codec and reports its counts.
// The check command is what carries the reader in the binary. S4.
func linkRowReader() {
	linkRowOnce.Do(func() {
		c := NewRowCodec(RowFns[linkRow]{Name: "ROW"})
		c.Register(nil)
		_, _, _ = c.Read(nil)
		_, _ = c.Write(&linkRow{}, schemaread.FormBinary)
		_ = c.Counts()
		_ = LegacyObject(nil)
		_ = DecodeLegacy(&linkRow{}, nil)
	})
}

// LegacyObject reports today's legacy JSON, as distinct from the schema JSON
// text. Both open with '{'. The generated writer ends the schema text with
// one newline and the reader accepts either; encoding/json writes no newline.
// A caller whose legacy text also ends with a newline passes its own Legacy. S2.
func LegacyObject(in []byte) bool {
	t := bytes.TrimLeft(in, " \t\r\n")
	if len(t) == 0 || t[0] != '{' {
		return false
	}
	return !bytes.HasSuffix(bytes.TrimRight(in, " \t"), []byte{'\n'})
}

// DecodeLegacy is a record's legacy JSON decoder: encoding/json into v, or
// legacy_json_unreadable when the text is not that JSON. S2.
func DecodeLegacy[T any](v *T, in []byte) RowReport {
	if err := json.Unmarshal(in, v); err != nil {
		return RowReport{Malformed: true, Reason: "legacy_json_unreadable"}
	}
	return RowReport{}
}
