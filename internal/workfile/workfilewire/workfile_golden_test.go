package workfilewire_test

// TestWorkfileTablesGolden is the golden test of the workfile area's definitions
// (schemas/workfile/workfile.schema, schemas/workfile/landledger.schema, their
// schema.lock and tables.baseline, the fuzz corpus schemas/workfile/fuzz/ and the
// generated internal/workfile/workfilewire): the work file tree, the roadmap
// carry records and the land ledgers.
//
// The split it pins (S1, the coordinator's form ruling of 2026-10-17): a record is
// a FIXED table only where every bound it declares is a bound the code enforces.
// One record is fixed (Counts holds integers only, so it declares no bound); the
// other eighteen hold a text or a list the Go code leaves open, so they are
// VARIABLE tables with open lists and unbounded text.
//
// What the test pins:
//
//   - every table round-trips as the binary wire, padded standard base64 of it and
//     the schema JSON text, each read strictly through pkg/schemaread (S2); the
//     base64 text is the wire; every field of every sample survives; the reads are
//     counted per form;
//   - every table reads and rewrites the hand-editable text form byte for byte,
//     and the value it reads is the value the JSON text gives;
//   - every open list holds 200 entries and every unbounded text a long value, in
//     every form; a null text and an empty text are different values;
//   - each declared range (the tree's numbers, 1..1<<31 and 0..1<<31 for a
//     reference; an item's card count and the ledger's byte offsets, never
//     negative) accepts the values at its ends and refuses one past either end by
//     name in the binary wire, padded base64, the JSON text and the text form;
//   - a damaged image of each form is refused by name, for every table;
//   - a real work file tree (written and read back by internal/workfile), real
//     roadmap carry records and a land ledger document survive the tables field by
//     field, and the testdata text-form files are those values, rewritten byte for
//     byte, with their comments;
//   - nothing serialized in the area lacks a table: every struct of the area's
//     source files is mapped to a table or named as not serialized, and every
//     serialized Go field has a table field;
//   - schema.lock holds exactly the fixed layout, the baseline covers every table,
//     generate.go pins the compiler the text form needs, and every fuzz instance's
//     wire is what the generated Save writes from its JSON.
//
// The test uses no clock, no socket, no network and no program. Times are text
// values the samples name; no test here reads a clock.
//
// Rules cited: S1, S2, S5 (Go only).

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/workfile"
	wire "github.com/mas-bandwidth/nova-sprint/internal/workfile/workfilewire"
	roadmapdoc "github.com/mas-bandwidth/nova-tools/pkg/roadmap"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const workHelp = "nova-work help"

// thePin is the schema compiler commit generate.go names: the repository's one
// schema pin (schemas/README.md), which carries the hand-editable text form the
// golden text-form files need.
const thePin = "v2.5.1-0.20261017153557-54e0eda7d20b"

// fixedLayouts is the layout hash of every fixed table, as schema.lock holds it.
var fixedLayouts = map[string]string{
	"Counts": "0xed016939191ee6cb",
}

// ---------------------------------------------------------------------------
// The harness.

type obj = map[string]any

func report(r wire.TableReport, ok bool) schemaread.Report {
	return schemaread.Report{
		Unknown:      int(r.Unknown),
		KindMismatch: int(r.KindMismatch),
		Clamped:      int(r.Clamped),
		Malformed:    r.Malformed || !ok,
		Reason:       r.Reason,
	}
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// refusalOf asserts err is a schemaread refusal by name and returns its reason.
func refusalOf(t *testing.T, label string, err error) string {
	t.Helper()
	require.Error(t, err, "%s: refused", label)
	var r *schemaread.Refusal
	require.True(t, errors.As(err, &r), "%s: every refusal is a *schemaread.Refusal: %v", label, err)
	assert.NotEmpty(t, r.Reason, "%s: a refusal names its reason", label)
	return r.Reason
}

// textRefusalOf asserts err is a text-form refusal by name and returns the name.
func textRefusalOf(t *testing.T, label string, err error) string {
	t.Helper()
	require.Error(t, err, "%s: refused", label)
	var r *wire.TableTextFormError
	require.True(t, errors.As(err, &r), "%s: every text-form refusal is a *TableTextFormError: %v", label, err)
	assert.True(t, strings.HasPrefix(r.Name, "text_"), "%s: the refusal is named: %q", label, r.Name)
	return r.Name
}

func decode(t *testing.T, text string) any {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(text))
	d.UseNumber()
	var v any
	require.NoError(t, d.Decode(&v), text)
	return v
}

func encode(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// contains reports whether every key and value of want is in got.
func contains(want, got any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, wv := range w {
			gv, ok := g[k]
			if !ok || !contains(wv, gv) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !contains(w[i], g[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(want, got)
}

// withField is the sample with its top-level key set to v.
func withField(t *testing.T, sample, key string, v any) string {
	m := decode(t, sample).(map[string]any)
	m[key] = v
	return encode(t, m)
}

func repeatElem(t *testing.T, sample, key string, k int) []any {
	m := decode(t, sample).(map[string]any)
	var elem any = map[string]any{}
	if l, ok := m[key].([]any); ok && len(l) > 0 {
		elem = l[0]
	}
	out := make([]any, k)
	for i := range out {
		out[i] = elem
	}
	return out
}

func openLists(info *wire.TableTypeInfo) []wire.TableFieldInfo {
	var out []wire.TableFieldInfo
	for _, f := range info.Fields {
		if f.IsArray && f.Counted && f.List {
			out = append(out, f)
		}
	}
	return out
}

func pointerFields(info *wire.TableTypeInfo, decl string) []wire.TableFieldInfo {
	var out []wire.TableFieldInfo
	for _, f := range info.Fields {
		if f.Pointer && f.DeclaredTypeName == decl {
			out = append(out, f)
		}
	}
	return out
}

// boundFields are the fields that declare a bound the reader holds: a bounded
// string or list, or an integer range.
func boundFields(info *wire.TableTypeInfo) (sized, ranged []wire.TableFieldInfo) {
	for _, f := range info.Fields {
		if f.ArrayBound > 0 && !f.Pointer && !f.List {
			sized = append(sized, f)
		}
		if f.HasRange {
			ranged = append(ranged, f)
		}
	}
	return sized, ranged
}

// textLimits are the text form's limits for the long values the test writes: the
// contract's defaults are far below a work file.
var textLimits = wire.TableTextFormLimits{Bytes: 64 << 20, Tokens: 1 << 22, Nodes: 1 << 22, Allocation: 64 << 20}

// textForm writes a JSON document in the hand-editable text form (the schema
// repository's docs/SPEC-TABLES.md section 16.9): an object is (:key value ...),
// a list is (...), a string is the JSON string, null is null.
func textForm(t *testing.T, v any) string {
	t.Helper()
	var b strings.Builder
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteByte('(')
			for i, k := range keys {
				if i > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(":" + k + " ")
				walk(x[k])
			}
			b.WriteByte(')')
		case []any:
			b.WriteByte('(')
			for i, e := range x {
				if i > 0 {
					b.WriteByte(' ')
				}
				walk(e)
			}
			b.WriteByte(')')
		case nil:
			b.WriteString("null")
		case bool:
			b.WriteString(strconv.FormatBool(x))
		case json.Number:
			b.WriteString(x.String())
		case string:
			q, err := json.Marshal(x)
			require.NoError(t, err)
			b.Write(q)
		default:
			t.Fatalf("textForm: %T", v)
		}
	}
	walk(v)
	return b.String()
}

// damage is every damaged image of one good wire image, in its two forms.
func refuseDamage[T any](t *testing.T, name string, r *schemaread.Reader[T], good []byte) {
	t.Helper()
	damaged := append([]byte(nil), good...)
	damaged[len(damaged)-1] ^= 0xff
	cases := map[string][]byte{
		"a damaged trailer":           damaged,
		"a damaged trailer in base64": []byte(b64(damaged)),
		"a truncated image":           good[:len(good)-1],
		"a truncated image in base64": []byte(b64(good[:len(good)-1])),
		"bad base64":                  []byte("!" + b64(good)[1:]),
		"an unknown form byte":        append([]byte{0x09}, good[1:]...),
	}
	for what, data := range cases {
		got, _, err := r.Read(data)
		refusalOf(t, name+": "+what, err)
		var zero T
		assert.True(t, reflect.DeepEqual(got, zero), "%s: %s: a refused read returns nothing", name, what)
	}
	_, _, err := r.Read(append([]byte{0x09}, good[1:]...))
	var ref *schemaread.Refusal
	require.True(t, errors.As(err, &ref))
	assert.Equal(t, schemaread.ReasonUnknownForm, ref.Reason, "%s: an unknown form byte is unknown_form", name)
}

// le is the wire's spelling of an int64 field: its kind byte (5) and the value,
// eight bytes little-endian.
func le(v int64) []byte {
	return binary.LittleEndian.AppendUint64([]byte{5}, uint64(v))
}

// pastRange is the wire image good holds with its one int64 field of value at
// rewritten to over: the image of a writer that ignored the range.
func pastRange(t *testing.T, good []byte, at, over int64) []byte {
	t.Helper()
	require.Equal(t, 1, bytes.Count(good, le(at)), "the in-range value %d is in the image once", at)
	return bytes.Replace(good, le(at), le(over), 1)
}

// ---------------------------------------------------------------------------
// Fixed tables: a value, the binary wire, padded standard base64 of it, the JSON
// text and the hand-editable text form.

type fixedOps[T any] struct {
	reset       func(*T)
	save        func(*T, []byte) int64
	load        func(*T, []byte, *wire.TableReport) bool
	fromJSON    func(*T, []byte, *wire.TableReport) bool
	jsonMeasure func(*T) int64
	toJSON      func(*T, []byte) int64
	loadTF      func([]byte, wire.TableTextFormLimits) (*wire.TableTextFormDocument, *T, error)
	saveTF      func(*wire.TableTextFormDocument) ([]byte, error)
	info        func() *wire.TableTypeInfo
}

func (o fixedOps[T]) codec(name string) *schemaread.Codec[T] {
	return &schemaread.Codec[T]{
		Name:  strings.ToUpper(name),
		Help:  workHelp,
		Reset: o.reset,
		Forms: []byte{1},
		LoadWire: func(v *T, w []byte) schemaread.Report {
			var rep wire.TableReport
			ok := o.load(v, w, &rep)
			return report(rep, ok)
		},
		LoadJSON: func(v *T, text []byte) schemaread.Report {
			var rep wire.TableReport
			ok := o.fromJSON(v, text, &rep)
			return report(rep, ok)
		},
		SaveWire: func(v *T) []byte {
			buf := make([]byte, 1<<16)
			n := o.save(v, buf)
			if n < 0 {
				return nil
			}
			return buf[:n]
		},
		SaveJSON: func(v *T) []byte {
			buf := make([]byte, o.jsonMeasure(v))
			return buf[:o.toJSON(v, buf)]
		},
	}
}

func runFixed[T any](t *testing.T, name, sample string, o fixedOps[T]) {
	info := o.info()
	require.False(t, info.Variable, "%s is a fixed table", name)
	sized, ranged := boundFields(info)
	assert.Empty(t, sized, "%s declares no bound: it holds integers only", name)
	assert.Empty(t, ranged, "%s declares no bound: it holds integers only", name)
	codec := o.codec(name)
	r := schemaread.NewReader(codec)

	v, res, err := r.Read([]byte(sample))
	require.NoError(t, err, "%s: the sample reads as JSON text", name)
	require.Equal(t, schemaread.FormJSON, res.Form)
	canon := string(codec.SaveJSON(&v))
	require.True(t, contains(decode(t, sample), decode(t, canon)), "%s: every field of the sample survives: %s", name, canon)
	for _, f := range info.Fields {
		_, ok := decode(t, sample).(map[string]any)[f.Json]
		assert.True(t, ok, "%s: the sample fills %s", name, f.Json)
	}

	good, err := r.Write(&v, schemaread.FormBinary)
	require.NoError(t, err, "%s writes", name)
	require.NotEmpty(t, good)
	text, err := r.Write(&v, schemaread.FormBase64)
	require.NoError(t, err)
	assert.Equal(t, b64(good), string(text), "%s: base64 text is padded standard base64 of the wire", name)
	js, err := r.Write(&v, schemaread.FormJSON)
	require.NoError(t, err)
	for _, data := range [][]byte{good, text, js} {
		got, res, err := r.Read(data)
		require.NoError(t, err, "%s", name)
		assert.True(t, reflect.DeepEqual(got, v), "%s reads the instance back from %s", name, res.Form)
	}
	c := r.Counts()
	assert.EqualValues(t, 1, c.Reads(schemaread.FormBinary), name)
	assert.EqualValues(t, 1, c.Reads(schemaread.FormBase64), name)
	assert.EqualValues(t, 2, c.Reads(schemaread.FormJSON), "%s: the sample and the written text", name)

	// the hand-editable text form: read, the same value, rewritten byte for byte
	tf := "; " + name + "\n" + textForm(t, decode(t, sample)) + "\n"
	doc, tv, err := o.loadTF([]byte(tf), textLimits)
	require.NoError(t, err, "%s reads its text form", name)
	assert.True(t, reflect.DeepEqual(*tv, v), "%s: the text form is the value the JSON text gives", name)
	again, err := o.saveTF(doc)
	require.NoError(t, err)
	assert.Equal(t, tf, string(again), "%s rewrites its text form byte for byte", name)
	_, _, err = o.loadTF([]byte(tf[:len(tf)-3]), textLimits)
	assert.Equal(t, "text_unterminated", textRefusalOf(t, name+": a truncated text form", err))
	_, _, err = o.loadTF([]byte("(:surprise 1)"), textLimits)
	assert.Equal(t, "text_unknown_key", textRefusalOf(t, name+": an unknown key", err))

	refuseDamage(t, name, r, good)
	got, _, err := r.Read([]byte(sample[:len(sample)/2]))
	assert.Equal(t, schemaread.ReasonMalformed, refusalOf(t, name+": truncated JSON", err))
	var zero T
	assert.True(t, reflect.DeepEqual(got, zero))
	assert.Positive(t, c.Refused(schemaread.ReasonMalformed), "%s: refusals are counted", name)
}

// ---------------------------------------------------------------------------
// Variable tables.

type varOps[T any] struct {
	fromJSON    func([]byte, *wire.TableReport) (*T, *wire.TableArena, bool)
	measure     func(*T, ...wire.TableWriteContext) int64
	save        func(*T, []byte, ...wire.TableWriteContext) int64
	loadMeasure func([]byte) (int64, error)
	load        func(region, w []byte, rep *wire.TableReport) *T
	jsonMeasure func(*T, ...wire.TableAllocator) int64
	toJSON      func(*T, []byte, ...wire.TableAllocator) int64
	loadTF      func([]byte, wire.TableTextFormLimits) (*wire.TableTextFormDocument, *T, error)
	saveTF      func(*wire.TableTextFormDocument) ([]byte, error)
	info        func() *wire.TableTypeInfo
}

func builder[B any, T any](init func(*B, ...wire.TableAllocator) bool, root func(*B) *T, arena func(*B) *wire.TableArena, from func(*B, []byte, *wire.TableReport) bool) func([]byte, *wire.TableReport) (*T, *wire.TableArena, bool) {
	return func(text []byte, rep *wire.TableReport) (*T, *wire.TableArena, bool) {
		b := new(B)
		if !init(b) {
			return nil, nil, false
		}
		ok := from(b, text, rep)
		return root(b), arena(b), ok
	}
}

func (o varOps[T]) loadRegion(w []byte, rep *wire.TableReport) *T {
	n, err := o.loadMeasure(w)
	if err != nil {
		return nil
	}
	return o.load(make([]byte, n), w, rep)
}

func (o varOps[T]) codec(name string) *schemaread.Codec[*T] {
	return &schemaread.Codec[*T]{
		Name:  strings.ToUpper(name),
		Help:  workHelp,
		Forms: []byte{1},
		Measure: func(w []byte) error {
			_, err := o.loadMeasure(w)
			return err
		},
		LoadWire: func(v **T, w []byte) schemaread.Report {
			var rep wire.TableReport
			out := o.loadRegion(w, &rep)
			*v = out
			return report(rep, out != nil)
		},
		LoadJSON: func(v **T, text []byte) schemaread.Report {
			var rep wire.TableReport
			root, arena, ok := o.fromJSON(text, &rep)
			if !ok || root == nil {
				return report(rep, false)
			}
			buf := make([]byte, o.measure(root, arena))
			n := o.save(root, buf, arena)
			if n < 0 {
				return report(rep, false)
			}
			var lrep wire.TableReport
			out := o.loadRegion(buf[:n], &lrep)
			*v = out
			return report(rep, out != nil)
		},
		SaveWire: func(v **T) []byte {
			m := o.measure(*v)
			if m < 0 {
				return nil
			}
			buf := make([]byte, m)
			n := o.save(*v, buf)
			if n < 0 {
				return nil
			}
			return buf[:n]
		},
		SaveJSON: func(v **T) []byte {
			buf := make([]byte, o.jsonMeasure(*v))
			return buf[:o.toJSON(*v, buf)]
		},
	}
}

// textJSON is the canonical JSON of the value a text form reads: the value is
// written to the wire and read back, so it compares with any other read.
func (o varOps[T]) textJSON(t *testing.T, label string, text []byte) (*wire.TableTextFormDocument, string) {
	t.Helper()
	doc, v, err := o.loadTF(text, textLimits)
	require.NoError(t, err, "%s reads its text form", label)
	buf := make([]byte, o.measure(v))
	n := o.save(v, buf)
	require.Positive(t, n, "%s: the text form's value writes", label)
	var rep wire.TableReport
	out := o.loadRegion(buf[:n], &rep)
	require.NotNil(t, out, "%s: the text form's value reads back", label)
	js := make([]byte, o.jsonMeasure(out))
	return doc, string(js[:o.toJSON(out, js)])
}

// textRoundTrip writes the JSON text in the text form and requires it to read to
// the same document and to rewrite byte for byte.
func (o varOps[T]) textRoundTrip(t *testing.T, label, canon string) {
	t.Helper()
	tf := "; " + label + "\n" + textForm(t, decode(t, canon)) + " ; end\n"
	doc, got := o.textJSON(t, label, []byte(tf))
	assert.Equal(t, canon, got, "%s: the text form is the document the JSON text gives", label)
	again, err := o.saveTF(doc)
	require.NoError(t, err)
	assert.Equal(t, tf, string(again), "%s rewrites its text form byte for byte", label)
}

// roundTrip reads text and requires the four forms of it to read back the one
// document, and the base64 to be the wire.
func roundTrip[T any](t *testing.T, label string, o varOps[T], r *schemaread.Reader[*T], codec *schemaread.Codec[*T], text string) string {
	t.Helper()
	v, _, err := r.Read([]byte(text))
	require.NoError(t, err, "%s reads", label)
	canon := string(codec.SaveJSON(&v))
	bin, err := r.Write(&v, schemaread.FormBinary)
	require.NoError(t, err, "%s writes", label)
	asText, err := r.Write(&v, schemaread.FormBase64)
	require.NoError(t, err)
	assert.Equal(t, b64(bin), string(asText), "%s: base64 text is the wire", label)
	for form, data := range map[string][]byte{"binary": bin, "base64": asText, "JSON": []byte(canon)} {
		got, _, err := r.Read(data)
		require.NoError(t, err, "%s reads from %s", label, form)
		assert.Equal(t, canon, string(codec.SaveJSON(&got)), "%s reads back from %s", label, form)
	}
	o.textRoundTrip(t, label, canon)
	return canon
}

// int64At is the sample's integer at key.
func int64At(t *testing.T, sample, key string) int64 {
	n, err := decode(t, sample).(map[string]any)[key].(json.Number).Int64()
	require.NoError(t, err)
	return n
}

func runVariable[T any](t *testing.T, name, sample string, o varOps[T]) {
	info := o.info()
	require.True(t, info.Variable, "%s is a variable table", name)
	codec := o.codec(name)
	r := schemaread.NewReader(codec)

	top := decode(t, sample).(map[string]any)
	for _, f := range info.Fields {
		_, ok := top[f.Json]
		assert.True(t, ok, "%s: the sample fills %s", name, f.Json)
	}

	v, res, err := r.Read([]byte(sample))
	require.NoError(t, err, "%s: the sample reads as JSON text", name)
	require.Equal(t, schemaread.FormJSON, res.Form)
	canon := string(codec.SaveJSON(&v))
	require.True(t, contains(decode(t, sample), decode(t, canon)), "%s: every field of the sample survives: %s", name, canon)

	good, err := r.Write(&v, schemaread.FormBinary)
	require.NoError(t, err, "%s writes", name)
	require.NotEmpty(t, good)
	text, err := r.Write(&v, schemaread.FormBase64)
	require.NoError(t, err)
	assert.Equal(t, b64(good), string(text), "%s: base64 text is padded standard base64 of the wire", name)
	js, err := r.Write(&v, schemaread.FormJSON)
	require.NoError(t, err)
	for _, data := range [][]byte{good, text, js} {
		got, res, err := r.Read(data)
		require.NoError(t, err, "%s", name)
		assert.Equal(t, canon, string(codec.SaveJSON(&got)), "%s reads the document back from %s", name, res.Form)
		again, err := r.Write(&got, schemaread.FormBinary)
		require.NoError(t, err)
		assert.Equal(t, good, again, "%s: the document read from %s writes the same wire", name, res.Form)
	}
	c := r.Counts()
	assert.EqualValues(t, 1, c.Reads(schemaread.FormBinary), name)
	assert.EqualValues(t, 1, c.Reads(schemaread.FormBase64), name)
	assert.EqualValues(t, 2, c.Reads(schemaread.FormJSON), "%s: the sample and the written text", name)
	o.textRoundTrip(t, name, canon)

	// every open list holds 200 entries
	for _, f := range openLists(info) {
		roundTrip(t, name+"."+f.Name+" with 200 entries", o, r, codec, withField(t, sample, f.Json, repeatElem(t, sample, f.Json, 200)))
	}
	// every unbounded text holds a long value, and null is not the empty text
	long := strings.Repeat("é-long text of the area; \"quoted\" ", 300)
	for _, f := range pointerFields(info, "string") {
		canonLong := roundTrip(t, name+"."+f.Name+" with long text", o, r, codec, withField(t, sample, f.Json, long))
		assert.Equal(t, long, decode(t, canonLong).(map[string]any)[f.Json], "%s.%s holds the long text whole", name, f.Name)
		assert.Nil(t, decode(t, roundTrip(t, name+"."+f.Name+" null", o, r, codec, withField(t, sample, f.Json, nil))).(map[string]any)[f.Json], "%s.%s: null stays null", name, f.Name)
		assert.Equal(t, "", decode(t, roundTrip(t, name+"."+f.Name+" empty", o, r, codec, withField(t, sample, f.Json, ""))).(map[string]any)[f.Json], "%s.%s: the empty text stays empty", name, f.Name)
	}
	for _, f := range pointerFields(info, "bytes") {
		blob := b64(bytes.Repeat([]byte{0xa5, 0x01, 0x00}, 3000))
		canonBlob := roundTrip(t, name+"."+f.Name+" with a long blob", o, r, codec, withField(t, sample, f.Json, blob))
		assert.Equal(t, blob, decode(t, canonBlob).(map[string]any)[f.Json], "%s.%s holds the blob whole", name, f.Name)
	}

	// each declared range: both ends are accepted; one past either end is refused
	// by name in the binary wire, padded base64, the JSON text and the text form
	sized, ranged := boundFields(info)
	assert.Empty(t, sized, "%s: no string or list bound is declared (THE FORM)", name)
	for _, f := range ranged {
		lo, hi := f.RangeIntMin, f.RangeIntMax
		for _, end := range []int64{lo, hi} {
			roundTrip(t, name+"."+f.Name+" at "+strconv.FormatInt(end, 10), o, r, codec, withField(t, sample, f.Json, json.Number(strconv.FormatInt(end, 10))))
		}
		type past struct{ inside, over int64 }
		var pasts []past
		if lo > math.MinInt64 {
			pasts = append(pasts, past{lo + 1, lo - 1})
		}
		if hi < math.MaxInt64 {
			pasts = append(pasts, past{hi, hi + 1})
		}
		require.NotEmpty(t, pasts, "%s.%s declares a range with an end", name, f.Name)
		for _, p := range pasts {
			at := withField(t, sample, f.Json, json.Number(strconv.FormatInt(p.inside, 10)))
			av, _, err := r.Read([]byte(at))
			require.NoError(t, err, "%s.%s at %d", name, f.Name, p.inside)
			img, err := r.Write(&av, schemaread.FormBinary)
			require.NoError(t, err)
			over := pastRange(t, img, p.inside, p.over)
			overJSON := withField(t, sample, f.Json, json.Number(strconv.FormatInt(p.over, 10)))
			for form, data := range map[string][]byte{"binary": over, "base64": []byte(b64(over)), "JSON": []byte(overJSON)} {
				got, _, err := r.Read(data)
				assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, name+"."+f.Name, err), "%s.%s at %d in %s is refused by name", name, f.Name, p.over, form)
				assert.Nil(t, got, "%s.%s: a refused read returns nothing", name, f.Name)
			}
			_, _, err = o.loadTF([]byte(textForm(t, decode(t, overJSON))), textLimits)
			assert.Equal(t, "text_bound", textRefusalOf(t, name+"."+f.Name+" in the text form", err), "%s.%s at %d in the text form", name, f.Name, p.over)
		}
	}

	refuseDamage(t, name, r, good)
	got, _, err := r.Read([]byte(sample[:len(sample)/2]))
	assert.Equal(t, schemaread.ReasonMalformed, refusalOf(t, name+": truncated JSON", err))
	assert.Nil(t, got)
	assert.Positive(t, c.Refused(schemaread.ReasonMalformed), "%s: refusals are counted", name)
	tf := textForm(t, decode(t, sample))
	_, _, err = o.loadTF([]byte(tf[:len(tf)-1]), textLimits)
	assert.Equal(t, "text_unterminated", textRefusalOf(t, name+": a truncated text form", err))
	_, _, err = o.loadTF([]byte("(:surprise 1)"), textLimits)
	assert.Equal(t, "text_unknown_key", textRefusalOf(t, name+": an unknown key", err))
	_, _, err = o.loadTF([]byte(tf+" ()"), textLimits)
	assert.Equal(t, "text_syntax", textRefusalOf(t, name+": a second root form", err))
}

// ---------------------------------------------------------------------------
// The operations of every table.

func wordOps() varOps[wire.Word] {
	return varOps[wire.Word]{builder((*wire.WordBuilder).Init, (*wire.WordBuilder).GetRoot, func(b *wire.WordBuilder) *wire.TableArena { return &b.Arena }, wire.WordFromJson),
		wire.WordMeasure, wire.WordSave, wire.WordLoadMeasureReason, wire.WordLoad, wire.WordToJsonMeasure, wire.WordToJson, wire.WordLoadTextForm, wire.WordSaveTextForm, wire.WordTableType}
}

func treeOps() varOps[wire.Tree] {
	return varOps[wire.Tree]{builder((*wire.TreeBuilder).Init, (*wire.TreeBuilder).GetRoot, func(b *wire.TreeBuilder) *wire.TableArena { return &b.Arena }, wire.TreeFromJson),
		wire.TreeMeasure, wire.TreeSave, wire.TreeLoadMeasureReason, wire.TreeLoad, wire.TreeToJsonMeasure, wire.TreeToJson, wire.TreeLoadTextForm, wire.TreeSaveTextForm, wire.TreeTableType}
}

func repoOps() varOps[wire.Repo] {
	return varOps[wire.Repo]{builder((*wire.RepoBuilder).Init, (*wire.RepoBuilder).GetRoot, func(b *wire.RepoBuilder) *wire.TableArena { return &b.Arena }, wire.RepoFromJson),
		wire.RepoMeasure, wire.RepoSave, wire.RepoLoadMeasureReason, wire.RepoLoad, wire.RepoToJsonMeasure, wire.RepoToJson, wire.RepoLoadTextForm, wire.RepoSaveTextForm, wire.RepoTableType}
}

func issueOps() varOps[wire.Issue] {
	return varOps[wire.Issue]{builder((*wire.IssueBuilder).Init, (*wire.IssueBuilder).GetRoot, func(b *wire.IssueBuilder) *wire.TableArena { return &b.Arena }, wire.IssueFromJson),
		wire.IssueMeasure, wire.IssueSave, wire.IssueLoadMeasureReason, wire.IssueLoad, wire.IssueToJsonMeasure, wire.IssueToJson, wire.IssueLoadTextForm, wire.IssueSaveTextForm, wire.IssueTableType}
}

func milestoneOps() varOps[wire.Milestone] {
	return varOps[wire.Milestone]{builder((*wire.MilestoneBuilder).Init, (*wire.MilestoneBuilder).GetRoot, func(b *wire.MilestoneBuilder) *wire.TableArena { return &b.Arena }, wire.MilestoneFromJson),
		wire.MilestoneMeasure, wire.MilestoneSave, wire.MilestoneLoadMeasureReason, wire.MilestoneLoad, wire.MilestoneToJsonMeasure, wire.MilestoneToJson, wire.MilestoneLoadTextForm, wire.MilestoneSaveTextForm, wire.MilestoneTableType}
}

func commentOps() varOps[wire.Comment] {
	return varOps[wire.Comment]{builder((*wire.CommentBuilder).Init, (*wire.CommentBuilder).GetRoot, func(b *wire.CommentBuilder) *wire.TableArena { return &b.Arena }, wire.CommentFromJson),
		wire.CommentMeasure, wire.CommentSave, wire.CommentLoadMeasureReason, wire.CommentLoad, wire.CommentToJsonMeasure, wire.CommentToJson, wire.CommentLoadTextForm, wire.CommentSaveTextForm, wire.CommentTableType}
}

func referenceOps() varOps[wire.Reference] {
	return varOps[wire.Reference]{builder((*wire.ReferenceBuilder).Init, (*wire.ReferenceBuilder).GetRoot, func(b *wire.ReferenceBuilder) *wire.TableArena { return &b.Arena }, wire.ReferenceFromJson),
		wire.ReferenceMeasure, wire.ReferenceSave, wire.ReferenceLoadMeasureReason, wire.ReferenceLoad, wire.ReferenceToJsonMeasure, wire.ReferenceToJson, wire.ReferenceLoadTextForm, wire.ReferenceSaveTextForm, wire.ReferenceTableType}
}

func linkedPROps() varOps[wire.LinkedPR] {
	return varOps[wire.LinkedPR]{builder((*wire.LinkedPRBuilder).Init, (*wire.LinkedPRBuilder).GetRoot, func(b *wire.LinkedPRBuilder) *wire.TableArena { return &b.Arena }, wire.LinkedPRFromJson),
		wire.LinkedPRMeasure, wire.LinkedPRSave, wire.LinkedPRLoadMeasureReason, wire.LinkedPRLoad, wire.LinkedPRToJsonMeasure, wire.LinkedPRToJson, wire.LinkedPRLoadTextForm, wire.LinkedPRSaveTextForm, wire.LinkedPRTableType}
}

func keptPairOps() varOps[wire.KeptPair] {
	return varOps[wire.KeptPair]{builder((*wire.KeptPairBuilder).Init, (*wire.KeptPairBuilder).GetRoot, func(b *wire.KeptPairBuilder) *wire.TableArena { return &b.Arena }, wire.KeptPairFromJson),
		wire.KeptPairMeasure, wire.KeptPairSave, wire.KeptPairLoadMeasureReason, wire.KeptPairLoad, wire.KeptPairToJsonMeasure, wire.KeptPairToJson, wire.KeptPairLoadTextForm, wire.KeptPairSaveTextForm, wire.KeptPairTableType}
}

func carryItemOps() varOps[wire.CarryItem] {
	return varOps[wire.CarryItem]{builder((*wire.CarryItemBuilder).Init, (*wire.CarryItemBuilder).GetRoot, func(b *wire.CarryItemBuilder) *wire.TableArena { return &b.Arena }, wire.CarryItemFromJson),
		wire.CarryItemMeasure, wire.CarryItemSave, wire.CarryItemLoadMeasureReason, wire.CarryItemLoad, wire.CarryItemToJsonMeasure, wire.CarryItemToJson, wire.CarryItemLoadTextForm, wire.CarryItemSaveTextForm, wire.CarryItemTableType}
}

func carryFixOps() varOps[wire.CarryFix] {
	return varOps[wire.CarryFix]{builder((*wire.CarryFixBuilder).Init, (*wire.CarryFixBuilder).GetRoot, func(b *wire.CarryFixBuilder) *wire.TableArena { return &b.Arena }, wire.CarryFixFromJson),
		wire.CarryFixMeasure, wire.CarryFixSave, wire.CarryFixLoadMeasureReason, wire.CarryFixLoad, wire.CarryFixToJsonMeasure, wire.CarryFixToJson, wire.CarryFixLoadTextForm, wire.CarryFixSaveTextForm, wire.CarryFixTableType}
}

func carryNoteOps() varOps[wire.CarryNote] {
	return varOps[wire.CarryNote]{builder((*wire.CarryNoteBuilder).Init, (*wire.CarryNoteBuilder).GetRoot, func(b *wire.CarryNoteBuilder) *wire.TableArena { return &b.Arena }, wire.CarryNoteFromJson),
		wire.CarryNoteMeasure, wire.CarryNoteSave, wire.CarryNoteLoadMeasureReason, wire.CarryNoteLoad, wire.CarryNoteToJsonMeasure, wire.CarryNoteToJson, wire.CarryNoteLoadTextForm, wire.CarryNoteSaveTextForm, wire.CarryNoteTableType}
}

func carryOps() varOps[wire.Carry] {
	return varOps[wire.Carry]{builder((*wire.CarryBuilder).Init, (*wire.CarryBuilder).GetRoot, func(b *wire.CarryBuilder) *wire.TableArena { return &b.Arena }, wire.CarryFromJson),
		wire.CarryMeasure, wire.CarrySave, wire.CarryLoadMeasureReason, wire.CarryLoad, wire.CarryToJsonMeasure, wire.CarryToJson, wire.CarryLoadTextForm, wire.CarrySaveTextForm, wire.CarryTableType}
}

func ledgerFormOps() varOps[wire.LedgerForm] {
	return varOps[wire.LedgerForm]{builder((*wire.LedgerFormBuilder).Init, (*wire.LedgerFormBuilder).GetRoot, func(b *wire.LedgerFormBuilder) *wire.TableArena { return &b.Arena }, wire.LedgerFormFromJson),
		wire.LedgerFormMeasure, wire.LedgerFormSave, wire.LedgerFormLoadMeasureReason, wire.LedgerFormLoad, wire.LedgerFormToJsonMeasure, wire.LedgerFormToJson, wire.LedgerFormLoadTextForm, wire.LedgerFormSaveTextForm, wire.LedgerFormTableType}
}

func ledgerListOps() varOps[wire.LedgerList] {
	return varOps[wire.LedgerList]{builder((*wire.LedgerListBuilder).Init, (*wire.LedgerListBuilder).GetRoot, func(b *wire.LedgerListBuilder) *wire.TableArena { return &b.Arena }, wire.LedgerListFromJson),
		wire.LedgerListMeasure, wire.LedgerListSave, wire.LedgerListLoadMeasureReason, wire.LedgerListLoad, wire.LedgerListToJsonMeasure, wire.LedgerListToJson, wire.LedgerListLoadTextForm, wire.LedgerListSaveTextForm, wire.LedgerListTableType}
}

func ledgerDocOps() varOps[wire.LedgerDoc] {
	return varOps[wire.LedgerDoc]{builder((*wire.LedgerDocBuilder).Init, (*wire.LedgerDocBuilder).GetRoot, func(b *wire.LedgerDocBuilder) *wire.TableArena { return &b.Arena }, wire.LedgerDocFromJson),
		wire.LedgerDocMeasure, wire.LedgerDocSave, wire.LedgerDocLoadMeasureReason, wire.LedgerDocLoad, wire.LedgerDocToJsonMeasure, wire.LedgerDocToJson, wire.LedgerDocLoadTextForm, wire.LedgerDocSaveTextForm, wire.LedgerDocTableType}
}

func ledgerEntryOps() varOps[wire.LedgerEntry] {
	return varOps[wire.LedgerEntry]{builder((*wire.LedgerEntryBuilder).Init, (*wire.LedgerEntryBuilder).GetRoot, func(b *wire.LedgerEntryBuilder) *wire.TableArena { return &b.Arena }, wire.LedgerEntryFromJson),
		wire.LedgerEntryMeasure, wire.LedgerEntrySave, wire.LedgerEntryLoadMeasureReason, wire.LedgerEntryLoad, wire.LedgerEntryToJsonMeasure, wire.LedgerEntryToJson, wire.LedgerEntryLoadTextForm, wire.LedgerEntrySaveTextForm, wire.LedgerEntryTableType}
}

func ledgerEntriesOps() varOps[wire.LedgerEntries] {
	return varOps[wire.LedgerEntries]{builder((*wire.LedgerEntriesBuilder).Init, (*wire.LedgerEntriesBuilder).GetRoot, func(b *wire.LedgerEntriesBuilder) *wire.TableArena { return &b.Arena }, wire.LedgerEntriesFromJson),
		wire.LedgerEntriesMeasure, wire.LedgerEntriesSave, wire.LedgerEntriesLoadMeasureReason, wire.LedgerEntriesLoad, wire.LedgerEntriesToJsonMeasure, wire.LedgerEntriesToJson, wire.LedgerEntriesLoadTextForm, wire.LedgerEntriesSaveTextForm, wire.LedgerEntriesTableType}
}

func countsOps() fixedOps[wire.Counts] {
	return fixedOps[wire.Counts]{wire.CountsReset, wire.CountsSave, wire.CountsLoad, wire.CountsFromJson, wire.CountsToJsonMeasure, wire.CountsToJson, wire.CountsLoadTextForm, wire.CountsSaveTextForm, wire.CountsTableType}
}

// ---------------------------------------------------------------------------
// The samples: every field of every table filled, with values the code writes.

func jn(v int64) json.Number { return json.Number(strconv.FormatInt(v, 10)) }

func sampleSet(t *testing.T) map[string]string {
	milestone := obj{"number": jn(3), "title": "v1.5"}
	comment := obj{"id": "IC_kwDO1", "url": "https://github.com/acme/a/issues/7#issuecomment-1", "author": "bob",
		"author_association": "COLLABORATOR", "created": "2026-10-01T10:05:00Z", "updated": "2026-10-01T10:06:00Z", "body": "Seen it; twice.\n\"Always\"."}
	reference := obj{"kind": "PullRequest", "repo": "acme/b", "number": jn(12), "url": "https://github.com/acme/b/pull/12",
		"actor": "carol", "at": "2026-10-02T09:00:00Z", "will_close": true}
	linked := obj{"repo": "acme/b", "number": jn(12), "url": "https://github.com/acme/b/pull/12", "state": "OPEN"}
	issue := obj{"number": jn(7), "url": "https://github.com/acme/a/issues/7", "node_id": "I_kwDO7", "title": "Crash on start",
		"state": "OPEN", "state_reason": "REOPENED", "origin": "internal", "author": "alice", "author_association": "MEMBER",
		"created": "2026-10-01T10:00:00Z", "updated": "2026-10-02T11:00:00Z", "closed": "", "locked": true, "lock_reason": "TOO_HEATED",
		"labels": words("bug", "p1"), "assignees": words("bob"), "milestone": milestone, "body": "It crashes.\nEvery time.",
		"comments": []any{comment}, "references": []any{reference}, "linked_prs": []any{linked}}
	repo := obj{"name": "acme/a", "url": "https://github.com/acme/a", "archived": true, "issues": []any{issue}}
	tree := obj{"format": workfile.Format, "source": "github", "org": "acme", "fetched": "2026-10-15T08:00:00Z",
		"scope": words("acme/a", "acme/b"), "repos": []any{repo}}
	word := obj{"text": "bug"}
	kept := obj{"key": "item-why", "value": "the reason it matters"}
	item := obj{"id": "gpu-contact", "group": "physics", "date": "2026-10-16", "title": "GPU contact", "text": "Contact on the GPU.",
		"why": "speed", "area": "fast3d", "exists": "the CPU path", "release": "v1.7", "origin": "issue #4", "cards": jn(3), "kept": []any{kept}}
	fix := obj{"id": "gpu-contact", "release": "v1.7.0", "status": "planned", "title": "GPU contact", "text": "Contact on the GPU.",
		"origin": "roadmap item gpu-contact", "kept": []any{kept}}
	note := obj{"id": "gpu-contact", "release": "", "title": "GPU contact", "text": "Contact on the GPU. Done: #12.", "date": "2026-10-17", "kept": []any{kept}}
	carry := obj{"items": []any{item}, "fixes": []any{fix}, "done": []any{note}}
	form := obj{"key": "fixed:a", "canon": `(fixed "a" :release "v1.2.14" :title "a" :origin "o")`, "off": jn(48), "end": jn(101)}
	scalar := obj{"key": "top:title", "canon": `"t"`, "off": jn(34), "end": jn(37)}
	list := obj{"name": "items", "off": jn(47), "end": jn(102), "bare": false, "items": []any{form}}
	doc := obj{"data": b64([]byte(evidenceFile)), "lists": []any{list}, "scalars": []any{scalar}}
	entry := obj{"key": "fixed:a", "canon": `(fixed "a" :release "v1.2.14" :title "a" :origin "o")`}
	entries := obj{"entries": []any{entry}}
	counts := obj{"repos": jn(2), "issues": jn(9), "comments": jn(31), "references": jn(5), "linked_prs": jn(4)}
	out := map[string]string{}
	for name, v := range map[string]obj{"Word": word, "Tree": tree, "Repo": repo, "Issue": issue, "Milestone": milestone, "Comment": comment,
		"Reference": reference, "LinkedPR": linked, "Counts": counts, "KeptPair": kept, "CarryItem": item, "CarryFix": fix,
		"CarryNote": note, "Carry": carry, "LedgerForm": form, "LedgerList": list, "LedgerDoc": doc, "LedgerEntry": entry,
		"LedgerEntries": entries} {
		out[name] = encode(t, v)
	}
	return out
}

// evidenceFile is an evidence ledger as the lander reads one (land_ledgers.go): one
// wrapping form, a title and one entry.
const evidenceFile = "; evidence\n(evidence \"v1\"\n :title \"t\"\n :items\n ((fixed \"a\" :release \"v1.2.14\" :title \"a\" :origin \"o\")))\n"

// ---------------------------------------------------------------------------
// The real records.

// realTree is a work file tree with every field the writer writes, both kinds of
// reference and the numbers at their ends, as internal/workfile writes and reads it.
func realTree() *workfile.Tree {
	return &workfile.Tree{Source: "github", Org: "acme", Fetched: "2026-10-15T08:00:00Z", Scope: []string{"acme/a", "acme/b"},
		Repos: []workfile.Repo{{Name: "acme/a", URL: "https://github.com/acme/a", Issues: []workfile.Issue{{
			Number: 7, URL: workfile.IssueURL("acme/a", 7), NodeID: "I_kwDO7", Title: "Crash on start", State: "OPEN",
			Origin: "internal", Author: "alice", AuthorAssociation: "MEMBER", Created: "2026-10-01T10:00:00Z",
			Updated: "2026-10-02T11:00:00Z", Labels: []string{"bug", "p1"}, Assignees: []string{"bob"},
			Milestone: &workfile.Milestone{Number: 3, Title: "v1.5"}, Body: "It crashes.\nEvery time; \"always\".",
			Comments: []workfile.Comment{{ID: "IC_kwDO1", URL: "https://github.com/acme/a/issues/7#issuecomment-1", Author: "bob",
				AuthorAssociation: "COLLABORATOR", Created: "2026-10-01T10:05:00Z", Updated: "2026-10-01T10:06:00Z", Body: "Seen it."}},
			References: []workfile.Reference{
				{Kind: "PullRequest", Repo: "acme/b", Number: 12, URL: "https://github.com/acme/b/pull/12", Actor: "carol", At: "2026-10-02T09:00:00Z", WillClose: true},
				{Actor: "dave", At: "2026-10-02T09:30:00Z"}},
			LinkedPRs: []workfile.LinkedPR{{Repo: "acme/b", Number: 12, URL: "https://github.com/acme/b/pull/12", State: "OPEN"}},
		}, {
			Number: 1 << 31, URL: workfile.IssueURL("acme/a", 1<<31), NodeID: "I_kwDO8", Title: "Old", State: "CLOSED",
			StateReason: "NOT_PLANNED", Origin: "external", Author: "erin", AuthorAssociation: "NONE", Created: "2026-09-01T10:00:00Z",
			Updated: "2026-09-02T10:00:00Z", Closed: "2026-09-03T10:00:00Z", Locked: true, LockReason: "RESOLVED",
		}}}, {Name: "acme/b", URL: "https://github.com/acme/b", Archived: true}}}
}

// strs is a Go []string as the Word list that holds it.
func strs(xs []string) []any {
	out := []any{}
	for _, x := range xs {
		out = append(out, obj{"text": x})
	}
	return out
}

func words(xs ...string) []any { return strs(xs) }

// fwdTree is the Tree table's JSON text of a work file tree, field by field.
func fwdTree(tr *workfile.Tree) obj {
	repos := []any{}
	for _, r := range tr.Repos {
		issues := []any{}
		for _, is := range r.Issues {
			var ms any
			if is.Milestone != nil {
				ms = obj{"number": jn(int64(is.Milestone.Number)), "title": is.Milestone.Title}
			}
			comments, refs, prs := []any{}, []any{}, []any{}
			for _, c := range is.Comments {
				comments = append(comments, obj{"id": c.ID, "url": c.URL, "author": c.Author, "author_association": c.AuthorAssociation,
					"created": c.Created, "updated": c.Updated, "body": c.Body})
			}
			for _, x := range is.References {
				refs = append(refs, obj{"kind": x.Kind, "repo": x.Repo, "number": jn(int64(x.Number)), "url": x.URL, "actor": x.Actor, "at": x.At, "will_close": x.WillClose})
			}
			for _, p := range is.LinkedPRs {
				prs = append(prs, obj{"repo": p.Repo, "number": jn(int64(p.Number)), "url": p.URL, "state": p.State})
			}
			issues = append(issues, obj{"number": jn(int64(is.Number)), "url": is.URL, "node_id": is.NodeID, "title": is.Title,
				"state": is.State, "state_reason": is.StateReason, "origin": is.Origin, "author": is.Author,
				"author_association": is.AuthorAssociation, "created": is.Created, "updated": is.Updated, "closed": is.Closed,
				"locked": is.Locked, "lock_reason": is.LockReason, "labels": strs(is.Labels), "assignees": strs(is.Assignees),
				"milestone": ms, "body": is.Body, "comments": comments, "references": refs, "linked_prs": prs})
		}
		repos = append(repos, obj{"name": r.Name, "url": r.URL, "archived": r.Archived, "issues": issues})
	}
	return obj{"format": workfile.Format, "source": tr.Source, "org": tr.Org, "fetched": tr.Fetched, "scope": strs(tr.Scope), "repos": repos}
}

func txt(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) int {
	n, _ := v.(json.Number).Int64()
	return int(n)
}

func lst(v any) []any {
	l, _ := v.([]any)
	return l
}

func strList(v any) []string {
	var out []string
	for _, x := range lst(v) {
		out = append(out, txt(x.(map[string]any)["text"]))
	}
	return out
}

// invTree is the work file tree a Tree table's JSON text holds.
func invTree(v any) *workfile.Tree {
	m := v.(map[string]any)
	tr := &workfile.Tree{Source: txt(m["source"]), Org: txt(m["org"]), Fetched: txt(m["fetched"]), Scope: strList(m["scope"])}
	for _, rv := range lst(m["repos"]) {
		rm := rv.(map[string]any)
		r := workfile.Repo{Name: txt(rm["name"]), URL: txt(rm["url"]), Archived: rm["archived"] == true}
		for _, iv := range lst(rm["issues"]) {
			im := iv.(map[string]any)
			is := workfile.Issue{Number: num(im["number"]), URL: txt(im["url"]), NodeID: txt(im["node_id"]), Title: txt(im["title"]),
				State: txt(im["state"]), StateReason: txt(im["state_reason"]), Origin: txt(im["origin"]), Author: txt(im["author"]),
				AuthorAssociation: txt(im["author_association"]), Created: txt(im["created"]), Updated: txt(im["updated"]),
				Closed: txt(im["closed"]), Locked: im["locked"] == true, LockReason: txt(im["lock_reason"]),
				Labels: strList(im["labels"]), Assignees: strList(im["assignees"]), Body: txt(im["body"])}
			if ms, ok := im["milestone"].(map[string]any); ok {
				is.Milestone = &workfile.Milestone{Number: num(ms["number"]), Title: txt(ms["title"])}
			}
			for _, cv := range lst(im["comments"]) {
				c := cv.(map[string]any)
				is.Comments = append(is.Comments, workfile.Comment{ID: txt(c["id"]), URL: txt(c["url"]), Author: txt(c["author"]),
					AuthorAssociation: txt(c["author_association"]), Created: txt(c["created"]), Updated: txt(c["updated"]), Body: txt(c["body"])})
			}
			for _, xv := range lst(im["references"]) {
				x := xv.(map[string]any)
				is.References = append(is.References, workfile.Reference{Kind: txt(x["kind"]), Repo: txt(x["repo"]), Number: num(x["number"]),
					URL: txt(x["url"]), Actor: txt(x["actor"]), At: txt(x["at"]), WillClose: x["will_close"] == true})
			}
			for _, pv := range lst(im["linked_prs"]) {
				p := pv.(map[string]any)
				is.LinkedPRs = append(is.LinkedPRs, workfile.LinkedPR{Repo: txt(p["repo"]), Number: num(p["number"]), URL: txt(p["url"]), State: txt(p["state"])})
			}
			r.Issues = append(r.Issues, is)
		}
		tr.Repos = append(tr.Repos, r)
	}
	return tr
}

// realCarry is one move's records with every field the carry writes.
func realCarry() ([]roadmapdoc.Item, []roadmapdoc.Fix, []roadmapdoc.Note) {
	kept := []roadmapdoc.Pair{{Key: "fix-status", Value: "planned"}, {Key: "item-origin", Value: ""}}
	return []roadmapdoc.Item{{ID: "gpu-contact", Group: "physics", Title: "GPU contact", Text: "Contact\non the GPU.", Why: "speed",
			Date: "2026-10-16", Area: "fast3d", Exists: "the CPU path", Cards: 3, Origin: "issue #4", Release: "v1.7", Kept: kept}},
		[]roadmapdoc.Fix{{ID: "gpu-contact", Release: "v1.7.0", Title: "GPU contact", Text: "", Origin: "roadmap item gpu-contact", Status: "planned", Kept: kept}},
		[]roadmapdoc.Note{{ID: "gpu-contact", Title: "GPU contact", Text: "Contact on the GPU. Done: #12.", Date: "2026-10-17", Kept: kept}}
}

func fwdKept(k []roadmapdoc.Pair) []any {
	out := []any{}
	for _, p := range k {
		out = append(out, obj{"key": p.Key, "value": p.Value})
	}
	return out
}

func invKept(v any) []roadmapdoc.Pair {
	var out []roadmapdoc.Pair
	for _, pv := range lst(v) {
		p := pv.(map[string]any)
		out = append(out, roadmapdoc.Pair{Key: txt(p["key"]), Value: txt(p["value"])})
	}
	return out
}

func fwdCarry(items []roadmapdoc.Item, fixes []roadmapdoc.Fix, notes []roadmapdoc.Note) obj {
	is, fs, ns := []any{}, []any{}, []any{}
	for _, it := range items {
		is = append(is, obj{"id": it.ID, "group": it.Group, "date": it.Date, "title": it.Title, "text": it.Text, "why": it.Why,
			"area": it.Area, "exists": it.Exists, "release": it.Release, "origin": it.Origin, "cards": jn(int64(it.Cards)), "kept": fwdKept(it.Kept)})
	}
	for _, fx := range fixes {
		fs = append(fs, obj{"id": fx.ID, "release": fx.Release, "status": fx.Status, "title": fx.Title, "text": fx.Text, "origin": fx.Origin, "kept": fwdKept(fx.Kept)})
	}
	for _, n := range notes {
		ns = append(ns, obj{"id": n.ID, "release": n.Release, "title": n.Title, "text": n.Text, "date": n.Date, "kept": fwdKept(n.Kept)})
	}
	return obj{"items": is, "fixes": fs, "done": ns}
}

func invCarry(v any) ([]roadmapdoc.Item, []roadmapdoc.Fix, []roadmapdoc.Note) {
	m := v.(map[string]any)
	var items []roadmapdoc.Item
	var fixes []roadmapdoc.Fix
	var notes []roadmapdoc.Note
	for _, x := range lst(m["items"]) {
		it := x.(map[string]any)
		items = append(items, roadmapdoc.Item{ID: txt(it["id"]), Group: txt(it["group"]), Date: txt(it["date"]), Title: txt(it["title"]),
			Text: txt(it["text"]), Why: txt(it["why"]), Area: txt(it["area"]), Exists: txt(it["exists"]), Release: txt(it["release"]),
			Origin: txt(it["origin"]), Cards: num(it["cards"]), Kept: invKept(it["kept"])})
	}
	for _, x := range lst(m["fixes"]) {
		fx := x.(map[string]any)
		fixes = append(fixes, roadmapdoc.Fix{ID: txt(fx["id"]), Release: txt(fx["release"]), Status: txt(fx["status"]), Title: txt(fx["title"]),
			Text: txt(fx["text"]), Origin: txt(fx["origin"]), Kept: invKept(fx["kept"])})
	}
	for _, x := range lst(m["done"]) {
		n := x.(map[string]any)
		notes = append(notes, roadmapdoc.Note{ID: txt(n["id"]), Release: txt(n["release"]), Title: txt(n["title"]), Text: txt(n["text"]),
			Date: txt(n["date"]), Kept: invKept(n["kept"])})
	}
	return items, fixes, notes
}

// readAll reads one document through its reader from the JSON text, and requires
// its binary wire, padded base64 and text form to read back the same document.
func readAll[T any](t *testing.T, label string, o varOps[T], text string) string {
	t.Helper()
	codec := o.codec(label)
	r := schemaread.NewReader(codec)
	return roundTrip(t, label, o, r, codec, text)
}

// ---------------------------------------------------------------------------
// The coverage of the source files.

// snake is a Go field name in the table's snake case.
func snake(name string) string {
	var b strings.Builder
	for i, c := range name {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				p := name[i-1]
				if p >= 'a' && p <= 'z' || p >= '0' && p <= '9' {
					b.WriteByte('_')
				}
			}
			b.WriteRune(c + ('a' - 'A'))
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// structFields is every named struct type of a Go source file with its field
// names.
func structFields(t *testing.T, file string) map[string][]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	require.NoError(t, err, file)
	out := map[string][]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		out[ts.Name.Name] = nil
		if st, ok := ts.Type.(*ast.StructType); ok {
			for _, fl := range st.Fields.List {
				for _, nm := range fl.Names {
					out[ts.Name.Name] = append(out[ts.Name.Name], nm.Name)
				}
			}
		}
		return true
	})
	return out
}

func tableFields(info *wire.TableTypeInfo) map[string]bool {
	out := map[string]bool{}
	for _, f := range info.Fields {
		out[f.Name] = true
	}
	return out
}

// ---------------------------------------------------------------------------
// The test.

func TestWorkfileTablesGolden(t *testing.T) {
	t.Parallel()
	s := sampleSet(t)

	t.Run("every table round-trips in every form", func(t *testing.T) {
		t.Parallel()
		for name, run := range map[string]func(t *testing.T){
			"Word":          func(t *testing.T) { runVariable(t, "Word", s["Word"], wordOps()) },
			"Tree":          func(t *testing.T) { runVariable(t, "Tree", s["Tree"], treeOps()) },
			"Repo":          func(t *testing.T) { runVariable(t, "Repo", s["Repo"], repoOps()) },
			"Issue":         func(t *testing.T) { runVariable(t, "Issue", s["Issue"], issueOps()) },
			"Milestone":     func(t *testing.T) { runVariable(t, "Milestone", s["Milestone"], milestoneOps()) },
			"Comment":       func(t *testing.T) { runVariable(t, "Comment", s["Comment"], commentOps()) },
			"Reference":     func(t *testing.T) { runVariable(t, "Reference", s["Reference"], referenceOps()) },
			"LinkedPR":      func(t *testing.T) { runVariable(t, "LinkedPR", s["LinkedPR"], linkedPROps()) },
			"KeptPair":      func(t *testing.T) { runVariable(t, "KeptPair", s["KeptPair"], keptPairOps()) },
			"CarryItem":     func(t *testing.T) { runVariable(t, "CarryItem", s["CarryItem"], carryItemOps()) },
			"CarryFix":      func(t *testing.T) { runVariable(t, "CarryFix", s["CarryFix"], carryFixOps()) },
			"CarryNote":     func(t *testing.T) { runVariable(t, "CarryNote", s["CarryNote"], carryNoteOps()) },
			"Carry":         func(t *testing.T) { runVariable(t, "Carry", s["Carry"], carryOps()) },
			"LedgerForm":    func(t *testing.T) { runVariable(t, "LedgerForm", s["LedgerForm"], ledgerFormOps()) },
			"LedgerList":    func(t *testing.T) { runVariable(t, "LedgerList", s["LedgerList"], ledgerListOps()) },
			"LedgerDoc":     func(t *testing.T) { runVariable(t, "LedgerDoc", s["LedgerDoc"], ledgerDocOps()) },
			"LedgerEntry":   func(t *testing.T) { runVariable(t, "LedgerEntry", s["LedgerEntry"], ledgerEntryOps()) },
			"LedgerEntries": func(t *testing.T) { runVariable(t, "LedgerEntries", s["LedgerEntries"], ledgerEntriesOps()) },
			"Counts":        func(t *testing.T) { runFixed(t, "Counts", s["Counts"], countsOps()) },
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				run(t)
			})
		}
	})

	t.Run("the declared ranges are the code's", func(t *testing.T) {
		t.Parallel()
		want := map[string][2]int64{
			"Issue.number":     {1, 1 << 31},
			"Milestone.number": {1, 1 << 31},
			"LinkedPR.number":  {1, 1 << 31},
			"Reference.number": {0, 1 << 31},
			"CarryItem.cards":  {0, math.MaxInt64},
			"LedgerForm.off":   {0, math.MaxInt64},
			"LedgerForm.end":   {0, math.MaxInt64},
			"LedgerList.off":   {0, math.MaxInt64},
			"LedgerList.end":   {0, math.MaxInt64},
		}
		got := map[string][2]int64{}
		for _, info := range []*wire.TableTypeInfo{wire.WordTableType(), wire.TreeTableType(), wire.RepoTableType(), wire.IssueTableType(), wire.MilestoneTableType(),
			wire.CommentTableType(), wire.ReferenceTableType(), wire.LinkedPRTableType(), wire.CountsTableType(), wire.KeptPairTableType(),
			wire.CarryItemTableType(), wire.CarryFixTableType(), wire.CarryNoteTableType(), wire.CarryTableType(), wire.LedgerFormTableType(),
			wire.LedgerListTableType(), wire.LedgerDocTableType(), wire.LedgerEntryTableType(), wire.LedgerEntriesTableType()} {
			for _, f := range info.Fields {
				if f.HasRange {
					got[info.Name+"."+f.Name] = [2]int64{f.RangeIntMin, f.RangeIntMax}
				}
			}
		}
		assert.Equal(t, want, got, "every declared range, and only these")
		// the tree's bound is the writer's: a number past it is refused by Encode
		over := realTree()
		over.Repos[0].Issues[1].Number = 1<<31 + 1
		_, err := workfile.Encode(over)
		require.Error(t, err, "Encode refuses an issue number past 1<<31")
	})

	t.Run("a real work file tree survives the tables", func(t *testing.T) {
		t.Parallel()
		tr := realTree()
		file, err := workfile.Encode(tr)
		require.NoError(t, err, "the tree is one internal/workfile writes")
		back, err := workfile.Decode("tree.sexp", file, workfile.Limits(1<<20))
		require.NoError(t, err, "and reads")
		require.Equal(t, tr, back)
		canon := readAll(t, "Tree", treeOps(), encode(t, fwdTree(tr)))
		assert.Equal(t, tr, invTree(decode(t, canon)), "every field of the tree survives the Tree table")

		// the testdata text form is this tree, with its comments, rewritten byte for byte
		text, err := os.ReadFile(filepath.Join("testdata", "tree.sexp"))
		require.NoError(t, err)
		require.True(t, bytes.Contains(text, []byte(";")), "the testdata tree holds comments")
		doc, got := treeOps().textJSON(t, "testdata/tree.sexp", text)
		assert.Equal(t, canon, got, "testdata/tree.sexp is the real tree")
		again, err := wire.TreeSaveTextForm(doc)
		require.NoError(t, err)
		assert.Equal(t, string(text), string(again), "testdata/tree.sexp rewrites byte for byte")
		assert.NotEmpty(t, doc.Comments(), "the comments are kept")
		// one edit changes only its value's bytes
		edited, _, err := wire.TreeEditTextForm(doc, wire.TableTextFormEdit{Path: "org", Text: `"acme-renamed"`})
		require.NoError(t, err)
		out, err := wire.TreeSaveTextForm(edited)
		require.NoError(t, err)
		assert.Equal(t, strings.Replace(string(text), `"acme"`, `"acme-renamed"`, 1), string(out), "an edit of :org changes only its value")
	})

	t.Run("real carry records survive the tables", func(t *testing.T) {
		t.Parallel()
		items, fixes, notes := realCarry()
		canon := readAll(t, "Carry", carryOps(), encode(t, fwdCarry(items, fixes, notes)))
		gi, gf, gn := invCarry(decode(t, canon))
		assert.Equal(t, items, gi, "every field of the item survives")
		assert.Equal(t, fixes, gf, "every field of the fix survives")
		assert.Equal(t, notes, gn, "every field of the note survives")
	})

	t.Run("the land ledger testdata is the ledger document", func(t *testing.T) {
		t.Parallel()
		canon := readAll(t, "LedgerDoc", ledgerDocOps(), s["LedgerDoc"])
		text, err := os.ReadFile(filepath.Join("testdata", "ledger.sexp"))
		require.NoError(t, err)
		require.True(t, bytes.Contains(text, []byte(";")), "the testdata ledger holds comments")
		doc, got := ledgerDocOps().textJSON(t, "testdata/ledger.sexp", text)
		assert.Equal(t, canon, got, "testdata/ledger.sexp is the sample ledger document")
		again, err := wire.LedgerDocSaveTextForm(doc)
		require.NoError(t, err)
		assert.Equal(t, string(text), string(again), "testdata/ledger.sexp rewrites byte for byte")
		// the document's spans are the evidence file's: each names its own bytes
		data := []byte(evidenceFile)
		m := decode(t, canon).(map[string]any)
		for _, l := range lst(m["lists"]) {
			lm := l.(map[string]any)
			assert.Equal(t, "(", string(data[num(lm["off"])]), "a list's span opens its list")
			for _, it := range lst(lm["items"]) {
				im := it.(map[string]any)
				assert.Equal(t, txt(im["canon"]), string(data[num(im["off"]):num(im["end"])]), "a member's span is its text")
			}
		}
		for _, sc := range lst(m["scalars"]) {
			sm := sc.(map[string]any)
			assert.Equal(t, txt(sm["canon"]), string(data[num(sm["off"]):num(sm["end"])]), "a scalar's span is its text")
		}
	})

	t.Run("every serialized struct of the area has its table", func(t *testing.T) {
		t.Parallel()
		root := filepath.Join("..", "..", "..")
		// the struct each table replaces: Go name -> table, with the table fields
		// the struct has no field for
		mapped := map[string]struct {
			table *wire.TableTypeInfo
			extra []string
		}{
			"Tree": {wire.TreeTableType(), []string{"format"}}, "Repo": {wire.RepoTableType(), nil}, "Issue": {wire.IssueTableType(), nil},
			"Milestone": {wire.MilestoneTableType(), nil}, "Comment": {wire.CommentTableType(), nil}, "Reference": {wire.ReferenceTableType(), nil},
			"LinkedPR": {wire.LinkedPRTableType(), nil}, "Counts": {wire.CountsTableType(), nil},
			"keyedForm": {wire.LedgerFormTableType(), nil}, "formList": {wire.LedgerListTableType(), nil}, "formDoc": {wire.LedgerDocTableType(), nil},
		}
		// never serialized: the reader's state, the carry's Pair list helper (its
		// element is KeptPair), the static ledger families and a splice in memory
		notSerialized := map[string]string{
			"decoder":    "the tree reader's state (decode.go)",
			"kept":       "a []roadmapdoc.Pair with methods; its element is KeptPair",
			"landLedger": "functions and a command line, a static table (landledger.go)",
			"formEdit":   "one splice of the tip's text in memory (land_forms.go)",
		}
		seen := map[string]bool{}
		for _, file := range []string{"internal/workfile/tree.go", "internal/workfile/decode.go", "internal/workfile/encode.go",
			"internal/roadmap/carry.go", "internal/roadmap/splice.go", "cmd/nova-sprint/landsexp.go", "cmd/nova-sprint/land_ledgers.go",
			"cmd/nova-sprint/landledger.go", "cmd/nova-sprint/land_forms.go"} {
			for name, fields := range structFields(t, filepath.Join(root, file)) {
				seen[name] = true
				if why, ok := notSerialized[name]; ok {
					assert.NotEmpty(t, why)
					continue
				}
				m, ok := mapped[name]
				if !assert.True(t, ok, "%s: struct %s is mapped to a table or named as not serialized", file, name) {
					continue
				}
				have := tableFields(m.table)
				want := map[string]bool{}
				for _, f := range fields {
					want[snake(f)] = true
					assert.True(t, have[snake(f)], "%s.%s has the table field %s.%s", name, f, m.table.Name, snake(f))
				}
				for _, e := range m.extra {
					want[e] = true
				}
				for f := range have {
					assert.True(t, want[f], "%s.%s is a field of %s", m.table.Name, f, name)
				}
			}
		}
		for name := range mapped {
			assert.True(t, seen[name], "%s is still a struct of the area", name)
		}
		for name := range notSerialized {
			assert.True(t, seen[name], "%s is still a struct of the area", name)
		}
		// the carry's records are nova-tools pkg/roadmap's structs
		for _, c := range []struct {
			goType reflect.Type
			table  *wire.TableTypeInfo
		}{{reflect.TypeOf(roadmapdoc.Item{}), wire.CarryItemTableType()}, {reflect.TypeOf(roadmapdoc.Fix{}), wire.CarryFixTableType()},
			{reflect.TypeOf(roadmapdoc.Note{}), wire.CarryNoteTableType()}, {reflect.TypeOf(roadmapdoc.Pair{}), wire.KeptPairTableType()}} {
			have := tableFields(c.table)
			assert.Equal(t, c.goType.NumField(), len(have), "%s has one table field per Go field", c.table.Name)
			for i := 0; i < c.goType.NumField(); i++ {
				f := c.goType.Field(i).Name
				assert.True(t, have[snake(f)], "%s.%s has the table field %s.%s", c.goType.Name(), f, c.table.Name, snake(f))
			}
		}
	})

	t.Run("the lock, the baseline, the pin and the fuzz corpus", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join("..", "..", "..", "schemas", "workfile")
		lock, err := os.ReadFile(filepath.Join(dir, "schema.lock"))
		require.NoError(t, err)
		gotFixed := map[string]string{}
		sc := bufio.NewScanner(bytes.NewReader(lock))
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) >= 4 && f[0] == "fixed" && f[1] == "table" {
				gotFixed[f[2]] = strings.TrimPrefix(f[3], "layout=")
			}
		}
		assert.Equal(t, fixedLayouts, gotFixed, "schema.lock holds exactly the fixed layouts")
		base, err := os.ReadFile(filepath.Join(dir, "tables.baseline"))
		require.NoError(t, err)
		for _, name := range []string{"Word", "Tree", "Repo", "Issue", "Milestone", "Comment", "Reference", "LinkedPR", "Counts", "KeptPair",
			"CarryItem", "CarryFix", "CarryNote", "Carry", "LedgerForm", "LedgerList", "LedgerDoc", "LedgerEntry", "LedgerEntries"} {
			assert.Contains(t, string(base), "\ntable "+name+"\n", "the baseline covers %s", name)
		}
		gen, err := os.ReadFile("generate.go")
		require.NoError(t, err)
		assert.Contains(t, string(gen), "//go:generate go run github.com/mas-bandwidth/schema/v2/cmd/schema@"+thePin+" generate --lang go --surface wire,fixed,json,cook,block,view,message,retain,textform --out . ../../../schemas/workfile\n",
			"generate.go pins the compiler the text form needs")

		// every fuzz instance's wire is what the generated Save writes from its JSON
		manifest, err := os.ReadFile(filepath.Join(dir, "fuzz", "MANIFEST.txt"))
		require.NoError(t, err)
		saves := map[string]func(*testing.T, string) []byte{
			"Tree": saveOf(treeOps()), "Carry": saveOf(carryOps()), "LedgerDoc": saveOf(ledgerDocOps()), "LedgerEntries": saveOf(ledgerEntriesOps()),
		}
		instances := 0
		sc = bufio.NewScanner(bytes.NewReader(manifest))
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) == 0 || f[0] != "instance" {
				continue
			}
			require.Len(t, f, 5, sc.Text())
			instances++
			save, ok := saves[f[3]]
			require.True(t, ok, "instance %s has a root the driver reads: %s", f[1], f[3])
			js, err := os.ReadFile(filepath.Join(dir, "fuzz", f[1]+".json"))
			require.NoError(t, err)
			bin, err := os.ReadFile(filepath.Join("..", "..", "..", f[4]))
			require.NoError(t, err)
			assert.Equal(t, save(t, string(js)), bin, "%s: the wire is the generated Save of its JSON", f[1])
		}
		assert.GreaterOrEqual(t, instances, len(saves), "every root the driver reads has an instance")
	})
}

// saveOf is the binary wire the generated Save writes from a JSON text.
func saveOf[T any](o varOps[T]) func(*testing.T, string) []byte {
	return func(t *testing.T, text string) []byte {
		t.Helper()
		var rep wire.TableReport
		root, arena, ok := o.fromJSON([]byte(text), &rep)
		require.True(t, ok, "the JSON reads: %+v", rep)
		buf := make([]byte, o.measure(root, arena))
		n := o.save(root, buf, arena)
		require.Positive(t, n)
		return buf[:n]
	}
}
