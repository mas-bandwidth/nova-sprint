package snaprowswire_test

// TestSnapshotAndCostGolden is the golden test of the snaprows definitions
// (schemas/snaprows/snaprows.schema, schemas/snaprows/schema.lock,
// schemas/snaprows/tables.baseline and the generated
// internal/sprint/store/snaprowswire): the store snapshot records, the twin's
// snapshot document with the nova-tables records it holds, the restore state and
// the card cost records.
//
// The split it pins (S1, the coordinator's form ruling of 2026-10-17): a record is
// a FIXED table only where every bound it declares is a bound the code enforces.
// Two records are fixed (SnapshotCounts and Tokens hold integers only, so they
// declare no bound); the other forty-nine hold a text or a list the Go struct
// leaves open, so they are VARIABLE tables with open lists and unbounded text.
//
// What the test pins:
//
//   - every table round-trips as the binary wire, padded standard base64 of it and
//     the schema JSON text, each read strictly through pkg/schemaread (S2); the
//     base64 text is the wire; every field of every sample survives;
//   - every open list reads and writes 200 entries, and every unbounded text and
//     byte field a long value, in all three forms; a null text and an empty text
//     are different values;
//   - each real bound accepts the value at the bound and refuses one past it by
//     name (clamped), in the binary wire, in padded base64 and in JSON text: the
//     consumers of a card's cost (64), a view's state (64 bytes), a table epoch's
//     properties (64), a table's columns (1000), a member's id (256 bytes) and a
//     member's field value (64 KiB, the reader's constraint, held at every depth);
//   - a damaged image of each form is refused by name, for every table;
//   - a real twin document, a real cost view, a real dump and the snapshot verb's
//     records, taken from the Go values, survive the tables field by field;
//   - nothing serialized in the area lacks a table: every struct of the four
//     source files is mapped to a table or named as not serialized, and every
//     serialized Go field has a table field;
//   - the nested records of the oprows area decode with the oprows tables;
//   - schema.lock holds exactly the fixed layouts, the baseline covers every
//     table and generate.go pins the repository's schema pin.
//
// The test uses no clock, no socket, no network and no program. Times are values
// the sample names; the clock of any test starts at 1000.0 seconds, and no test
// here reads one.
//
// Rules cited: S1, S2, S5 (Go only).

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	ow "github.com/mas-bandwidth/nova-sprint/internal/sprint/store/oprowswire"
	wire "github.com/mas-bandwidth/nova-sprint/internal/sprint/store/snaprowswire"
	"github.com/mas-bandwidth/nova-tools/pkg/cardcost"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const snapHelp = "nova-sprint help snapshot"

// fixedLayouts is the layout hash of every fixed table, as schema.lock holds it.
var fixedLayouts = map[string]string{
	"SnapshotCounts": "0xfa7773a35a121a6a",
	"Tokens":         "0x271937985ce3e7d8",
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

// refusalOf asserts err is a refusal by name and returns its reason.
func refusalOf(t *testing.T, label string, err error) string {
	t.Helper()
	require.Error(t, err, "%s: refused", label)
	var r *schemaread.Refusal
	require.True(t, errors.As(err, &r), "%s: every refusal is a *schemaread.Refusal: %v", label, err)
	name := label
	if i := strings.IndexAny(name, ".:"); i >= 0 {
		name = name[:i]
	}
	assert.True(t, strings.HasPrefix(err.Error(), strings.ToUpper(name)+" REFUSED: "), "%s: %s", label, err.Error())
	assert.NotEmpty(t, r.Reason, "%s: a refusal names its reason", label)
	return r.Reason
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

// setPath is the sample with the value at path (keys and list indexes) set to v.
func setPath(t *testing.T, sample string, path []any, v any) string {
	root := decode(t, sample)
	cur := root
	for i, p := range path {
		last := i == len(path)-1
		switch k := p.(type) {
		case string:
			m, ok := cur.(map[string]any)
			require.True(t, ok, "path %v at %v", path, k)
			if last {
				m[k] = v
			} else {
				cur = m[k]
			}
		case int:
			l, ok := cur.([]any)
			require.True(t, ok && k < len(l), "path %v at %d", path, k)
			if last {
				l[k] = v
			} else {
				cur = l[k]
			}
		}
	}
	return encode(t, root)
}

// getPath reads the value at path of a decoded document.
func getPath(t *testing.T, doc any, path []any) any {
	cur := doc
	for _, p := range path {
		switch k := p.(type) {
		case string:
			cur = cur.(map[string]any)[k]
		case int:
			cur = cur.([]any)[k]
		}
	}
	return cur
}

func boundStrings(info *wire.TableTypeInfo) []wire.TableFieldInfo {
	var out []wire.TableFieldInfo
	for _, f := range info.Fields {
		if f.Kind == 12 && f.Counted && !f.IsArray && !f.Pointer && f.ArrayBound > 0 {
			out = append(out, f)
		}
	}
	return out
}

func boundLists(info *wire.TableTypeInfo) []wire.TableFieldInfo {
	var out []wire.TableFieldInfo
	for _, f := range info.Fields {
		if f.IsArray && f.Counted && !f.List && !f.Map && !f.Pointer && f.Kind == 13 && f.ArrayBound > 0 {
			out = append(out, f)
		}
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

// pastBound is the wire image good holds with its string of n marker bytes grown
// to n+1, its length prefix rewritten: the image of a writer that ignored the bound.
func pastBound(t *testing.T, good []byte, n int) []byte {
	t.Helper()
	enc := func(k int) []byte {
		b := binary.AppendUvarint([]byte{12}, uint64(k))
		return append(b, bytes.Repeat([]byte{'~'}, k)...)
	}
	at := enc(n)
	require.Equal(t, 1, bytes.Count(good, at), "the at-bound string is in the image once")
	return bytes.Replace(good, at, enc(n+1), 1)
}

// pastCount is the wire image good holds with the count of its one list of n
// entries rewritten to n+1: the image of a writer that ignored the bound claims
// one entry more than the bound. The wire writes a list as kind 13 and the LEB128
// count.
func pastCount(t *testing.T, good []byte, n int) []byte {
	t.Helper()
	enc := func(k int) []byte { return binary.AppendUvarint([]byte{13}, uint64(k)) }
	at, over := enc(n), enc(n+1)
	require.Equal(t, len(at), len(over), "the count is the same width")
	require.Equal(t, 1, bytes.Count(good, at), "the at-bound count is in the image once")
	return bytes.Replace(good, at, over, 1)
}

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

// ---------------------------------------------------------------------------
// Fixed tables: a value, the binary wire, padded standard base64 of it and the
// JSON text.

type fixedOps[T any] struct {
	reset       func(*T)
	save        func(*T, []byte) int64
	load        func(*T, []byte, *wire.TableReport) bool
	fromJSON    func(*T, []byte, *wire.TableReport) bool
	jsonMeasure func(*T) int64
	toJSON      func(*T, []byte) int64
	info        func() *wire.TableTypeInfo
}

func (o fixedOps[T]) codec(name string) *schemaread.Codec[T] {
	return &schemaread.Codec[T]{
		Name:  strings.ToUpper(name),
		Help:  snapHelp,
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
	assert.Empty(t, boundStrings(info), "%s declares no bound: it holds integers only", name)
	assert.Empty(t, boundLists(info), "%s declares no bound: it holds integers only", name)
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
	info        func() *wire.TableTypeInfo
	constraints func(**T, *schemaread.Checker)
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
		Help:  snapHelp,
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
		Constraints: o.constraints,
	}
}

// roundTrip reads text and requires the three forms of it to read back the one
// document, and the base64 to be the wire.
func roundTrip[T any](t *testing.T, label string, r *schemaread.Reader[*T], codec *schemaread.Codec[*T], text string) string {
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
	return canon
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

	// every open list holds 200 entries
	for _, f := range openLists(info) {
		roundTrip(t, name+"."+f.Name+" with 200 entries", r, codec, withField(t, sample, f.Json, repeatElem(t, sample, f.Json, 200)))
	}
	// every unbounded text holds a long value, and null is not the empty text
	long := strings.Repeat("é-long text of the area ", 4000)
	for _, f := range pointerFields(info, "string") {
		long := long
		canonLong := roundTrip(t, name+"."+f.Name+" with long text", r, codec, withField(t, sample, f.Json, long))
		assert.Equal(t, long, decode(t, canonLong).(map[string]any)[f.Json], "%s.%s holds the long text whole", name, f.Name)
		assert.Nil(t, decode(t, roundTrip(t, name+"."+f.Name+" null", r, codec, withField(t, sample, f.Json, nil))).(map[string]any)[f.Json], "%s.%s: null stays null", name, f.Name)
		assert.Equal(t, "", decode(t, roundTrip(t, name+"."+f.Name+" empty", r, codec, withField(t, sample, f.Json, ""))).(map[string]any)[f.Json], "%s.%s: the empty text stays empty", name, f.Name)
	}
	for _, f := range pointerFields(info, "bytes") {
		blob := b64(bytes.Repeat([]byte{0xa5, 0x01, 0x00}, 40000))
		canonBlob := roundTrip(t, name+"."+f.Name+" with a long blob", r, codec, withField(t, sample, f.Json, blob))
		assert.Equal(t, blob, decode(t, canonBlob).(map[string]any)[f.Json], "%s.%s holds the blob whole", name, f.Name)
	}

	// each bounded string: the value at the bound is accepted; one past is
	// refused clamped in the binary wire, padded base64 and JSON text
	for _, f := range boundStrings(info) {
		n := int(f.ArrayBound)
		at := withField(t, sample, f.Json, strings.Repeat("~", n))
		av, _, err := r.Read([]byte(at))
		require.NoError(t, err, "%s.%s at its bound %d", name, f.Name, n)
		img, err := r.Write(&av, schemaread.FormBinary)
		require.NoError(t, err)
		for _, data := range [][]byte{img, []byte(b64(img)), []byte(at)} {
			_, _, err := r.Read(data)
			require.NoError(t, err, "%s.%s at its bound reads", name, f.Name)
		}
		over := pastBound(t, img, n)
		for form, data := range map[string][]byte{"binary": over, "base64": []byte(b64(over)), "JSON": []byte(withField(t, sample, f.Json, strings.Repeat("~", n+1)))} {
			got, _, err := r.Read(data)
			assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, name+"."+f.Name, err), "%s.%s past %d in %s is refused clamped", name, f.Name, n, form)
			assert.Nil(t, got, "%s.%s: a refused read returns nothing", name, f.Name)
		}
	}
	// each bounded list: n entries are accepted; n+1 is refused clamped in the
	// binary wire, padded base64 and JSON text
	for _, f := range boundLists(info) {
		n := int(f.ArrayBound)
		at := withField(t, sample, f.Json, repeatElem(t, sample, f.Json, n))
		av, _, err := r.Read([]byte(at))
		require.NoError(t, err, "%s.%s at its bound %d", name, f.Name, n)
		img, err := r.Write(&av, schemaread.FormBinary)
		require.NoError(t, err)
		for _, data := range [][]byte{img, []byte(b64(img)), []byte(at)} {
			_, _, err := r.Read(data)
			require.NoError(t, err, "%s.%s at its bound reads", name, f.Name)
		}
		over := pastCount(t, img, n)
		for form, data := range map[string][]byte{"binary": over, "base64": []byte(b64(over)), "JSON": []byte(withField(t, sample, f.Json, repeatElem(t, sample, f.Json, n+1)))} {
			got, _, err := r.Read(data)
			assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, name+"."+f.Name, err), "%s.%s past %d in %s is refused clamped", name, f.Name, n, form)
			assert.Nil(t, got, "%s.%s: a refused read returns nothing", name, f.Name)
		}
	}

	refuseDamage(t, name, r, good)
	got, _, err := r.Read([]byte(sample[:len(sample)/2]))
	assert.Equal(t, schemaread.ReasonMalformed, refusalOf(t, name+": truncated JSON", err))
	assert.Nil(t, got)
	assert.Positive(t, c.Refused(schemaread.ReasonMalformed), "%s: refusals are counted", name)
}

// ---------------------------------------------------------------------------
// The nested records of the oprows area: real images of its tables.

func oprowsImage[T any, B any](t *testing.T, text string, init func(*B, ...ow.TableAllocator) bool, from func(*B, []byte, *ow.TableReport) bool,
	root func(*B) *T, arena func(*B) *ow.TableArena, measure func(*T, ...ow.TableWriteContext) int64, save func(*T, []byte, ...ow.TableWriteContext) int64) string {
	t.Helper()
	b := new(B)
	require.True(t, init(b))
	var rep ow.TableReport
	require.True(t, from(b, []byte(text), &rep), "the oprows sample reads: %s", text)
	r := root(b)
	buf := make([]byte, measure(r, arena(b)))
	n := save(r, buf, arena(b))
	require.Positive(t, n)
	return b64(buf[:n])
}

func oprowsNote(t *testing.T) string {
	return oprowsImage(t, `{"id":"n12","kind":"judgment","stream":"v15-schema-tables","what":"the card is stuck"}`,
		(*ow.NoteBuilder).Init, ow.NoteFromJson, (*ow.NoteBuilder).GetRoot, func(b *ow.NoteBuilder) *ow.TableArena { return &b.Arena }, ow.NoteMeasure, ow.NoteSave)
}

func oprowsLine(t *testing.T) string {
	return oprowsImage(t, `{"kind":"move","at":1792000000000000000,"epoch":21,"card":"card-a","stream":"v15-schema-tables","from":"m1:working","to":"m1:done"}`,
		(*ow.LogLineBuilder).Init, ow.LogLineFromJson, (*ow.LogLineBuilder).GetRoot, func(b *ow.LogLineBuilder) *ow.TableArena { return &b.Arena }, ow.LogLineMeasure, ow.LogLineSave)
}

func oprowsFence(t *testing.T) string {
	return oprowsImage(t, `{"id":"finish-ab12","verb":"finish","at":1792000000000000000,"streams":[{"value":"v15-schema-tables"}],"caller_op":"cli-77"}`,
		(*ow.OpRecordBuilder).Init, ow.OpRecordFromJson, (*ow.OpRecordBuilder).GetRoot, func(b *ow.OpRecordBuilder) *ow.TableArena { return &b.Arena }, ow.OpRecordMeasure, ow.OpRecordSave)
}

func oprowsQueued(t *testing.T) string {
	return oprowsImage(t, `{"verb":"finish","actor":"beta","moved":"a8-defs-store-snapshot-and-cost"}`,
		(*ow.QueuedChangeBuilder).Init, ow.QueuedChangeFromJson, (*ow.QueuedChangeBuilder).GetRoot, func(b *ow.QueuedChangeBuilder) *ow.TableArena { return &b.Arena }, ow.QueuedChangeMeasure, ow.QueuedChangeSave)
}

// oprowsJSON decodes an image with the oprows table of the kind and gives its JSON.
func oprowsJSON(t *testing.T, kind, image string) map[string]any {
	t.Helper()
	w, err := base64.StdEncoding.DecodeString(image)
	require.NoError(t, err)
	var rep ow.TableReport
	var out []byte
	switch kind {
	case "Note":
		n, err := ow.NoteLoadMeasureReason(w)
		require.NoError(t, err)
		r := ow.NoteLoad(make([]byte, n), w, &rep)
		require.NotNil(t, r)
		out = make([]byte, ow.NoteToJsonMeasure(r))
		out = out[:ow.NoteToJson(r, out)]
	case "LogLine":
		n, err := ow.LogLineLoadMeasureReason(w)
		require.NoError(t, err)
		r := ow.LogLineLoad(make([]byte, n), w, &rep)
		require.NotNil(t, r)
		out = make([]byte, ow.LogLineToJsonMeasure(r))
		out = out[:ow.LogLineToJson(r, out)]
	case "OpRecord":
		n, err := ow.OpRecordLoadMeasureReason(w)
		require.NoError(t, err)
		r := ow.OpRecordLoad(make([]byte, n), w, &rep)
		require.NotNil(t, r)
		out = make([]byte, ow.OpRecordToJsonMeasure(r))
		out = out[:ow.OpRecordToJson(r, out)]
	case "QueuedChange":
		n, err := ow.QueuedChangeLoadMeasureReason(w)
		require.NoError(t, err)
		r := ow.QueuedChangeLoad(make([]byte, n), w, &rep)
		require.NotNil(t, r)
		out = make([]byte, ow.QueuedChangeToJsonMeasure(r))
		out = out[:ow.QueuedChangeToJson(r, out)]
	}
	require.Zero(t, rep.Clamped)
	return decode(t, string(out)).(map[string]any)
}

// ---------------------------------------------------------------------------
// The samples: one instance of each table, every field filled, as JSON text.

func arr(v ...any) []any { return v }

func words(s ...string) []any {
	out := []any{}
	for _, w := range s {
		out = append(out, obj{"text": w})
	}
	return out
}

// sampleSet builds every sample; t is the running test, for the oprows images.
func sampleSet(t *testing.T) map[string]string {
	tokens := obj{"input": 19541, "cache_read": 36336, "cache_write": -1, "output": 692, "reasoning": 70, "requests": 6, "max_prompt": 9861}
	rawRecord := obj{"harness": "muse", "execution": "fleet", "source_schema": "muse-jsonl-v1", "producer_version": "1.2.3", "billing": "subscription",
		"tokens": arr(obj{"name": "input_tokens", "count": 4200}, obj{"name": "output_tokens", "count": 90})}
	rawUsage := obj{"version": 1, "records": arr(rawRecord)}
	usage := obj{"billing": "subscription", "receipt_version": 2, "raw_usage": rawUsage, "wall": "49.49s", "budget": "20088/400000", "tokens": tokens,
		"model": "opencode/deepseek-v4-pro", "actual_usd": "0.04219614", "actual_by": "harness", "wait_s": 3, "run_s": 52, "price_route": "pro-a",
		"prices": "in:0.27,cr:0.07,out:1.1,ro:true", "predicted_usd": "0.00680779", "long_context": true, "unpriced": "no-tokens"}
	total := obj{"records": 2, "tokens": tokens, "wait_s": 6, "run_s": 104, "predicted_usd": "0.01361558", "predicted_records": 2, "actual_usd": "0.08439228",
		"actual_records": 2, "actual_by": "harness", "charged_usd": "0.08439228", "charged_records": 2}
	consumer := obj{"kind": "work", "card": "a8-defs-store-snapshot-and-cost", "attempt": 2, "take": 1, "gen": 1, "who": "friend.rowan", "route": "pro-a",
		"model": "opencode/deepseek-v4-pro", "tier": "pro", "end": "landed", "at": "2026-10-17T01:02:03Z", "key": "a8-defs-store-snapshot-and-cost#g1",
		"usage": usage, "cap": "30m", "overrun": "4m"}
	counts := obj{"keys": 128, "cards": -1}
	column := obj{"name": "state", "label": "State", "projection": "count", "fold": "sum", "width": 8}
	scored := obj{"member": "s1-1", "score": 0.5}
	cell := obj{"key": "k:r1:state", "bound": true, "count": 2, "members": arr(scored, obj{"member": "s1-2", "score": 1.25}), "unread": true, "unread_why": "key K is string, expected zset"}
	row := obj{"key": "r1", "label": "Row one", "exclude": "s1-9", "owner": "finish", "cells": arr(cell), "texts": arr(obj{"name": "state", "value": "active"}), "hidden": true}
	ntab := obj{"epoch_key": "e", "epoch_field": "n", "member_prefix": "w:", "epoch": 21, "revision": 7, "name": "t-work", "columns": arr(column),
		"footer_label": "total", "hidden": words("note"), "hidden_table": true, "sort": "-name", "rows": arr(row), "props": arr(obj{"name": "stream", "value": "s1"})}
	view := obj{"name": "sprint", "tables": words("t-work", "t-fleet"), "title": "The sprint", "summary": "state", "state": "STOPPED"}
	fchange := obj{"name": "brief", "before": nil, "after": "t", "before_bytes": 0, "before_sha1": "", "after_bytes": 1, "after_sha1": "da4b9237bacccdf19c0760cab7aec4a8359010b0"}
	bmember := obj{"id": "s1-1", "before_place": "", "after_place": "r1:state", "before_score_text": nil, "after_score_text": "0.5", "before_rev": "0", "after_rev": "1",
		"fields_set": arr(obj{"name": "brief", "value": "t"}), "fields_unset": words("old"), "fields": arr(fchange)}
	bdelta := obj{"operation_id": "op1", "digest": "d1", "actor": "beta", "selected_count": 1, "guard_count": 0, "changed_count": 1, "members": arr(bmember),
		"props": arr(obj{"name": "stream", "value": "s1"})}
	receipt := obj{"id": "1-0", "epoch": 21, "before": 1, "after": 2, "outcome": "changed", "batch_delta": bdelta, "replay": true}
	field := obj{"name": "brief", "value": "a card brief"}
	member := obj{"epoch": 21, "placed": true, "row": "r1", "col": "state", "score": 0.5, "rev": 1, "fields": arr(field, obj{"name": "gen", "value": "1"})}
	memberEntry := obj{"id": "s1-1", "member": member}
	change := obj{"epoch": 21, "before": 1, "after": 2, "verb": "apply", "ids": words("s1-1")}
	opEntry := obj{"key": "21:op1", "body": "{}", "receipt": receipt}
	epochSnap := obj{"rows": words("r1", "r2"), "hidden": arr(obj{"name": "r2", "on": true}), "texts": arr(obj{"row": "r1", "cells": arr(obj{"name": "state", "value": "active"})}),
		"props": arr(obj{"name": "stream", "value": "s1"})}
	tableSnap := obj{"def": ntab, "epochs": arr(obj{"epoch": 21, "snapshot": epochSnap}), "wrote": arr(obj{"epoch": 21, "on": true}), "rev": 2,
		"members": arr(memberEntry), "ops": arr(opEntry), "changes": arr(change)}
	tableEntry := obj{"name": "t-work", "table_snapshot": tableSnap}
	residue := obj{"keys": arr(obj{"name": "w:s1-1", "on": true}), "table_snapshot": tableSnap}
	note, line, fence, queued := oprowsNote(t), oprowsLine(t), oprowsFence(t), oprowsQueued(t)
	logSnap := obj{"fence": fence, "gen": 3, "done": arr(obj{"name": "caller-1", "value": "ok"}), "progress": arr(obj{"name": "s1", "at": 1792000000000000000}),
		"inbox": arr(obj{"id": "1-0", "note": note}), "lines": arr(obj{"id": "1-0", "line": line}), "notes": arr(obj{"id": "n12", "note": note}),
		"aliases": arr(obj{"name": "j12", "value": "n12"}), "answered": arr(obj{"name": "n12", "value": "coordinator"}),
		"open": arr(obj{"name": "s1/s1-1", "value": "n12"}), "cursor": "1-0", "queue": arr(obj{"data": queued})}
	memSnap := obj{"version": 1, "tables": arr(tableEntry), "dropped": arr(obj{"name": "t-old", "residue": residue}),
		"views": arr(obj{"name": "sprint", "view": view}), "epoch_set": true, "epoch": 21, "cleared": 1792000000000000000, "owed": true,
		"defaults_owed": 21, "logs": arr(obj{"epoch": 21, "log": logSnap}), "kv": arr(obj{"name": "coordinator", "value": "rowan"}), "seq": 9}

	s := map[string]any{
		"Word": obj{"text": "alpha"}, "Pair": obj{"name": "stream", "value": "s1"}, "Flag": obj{"name": "r2", "on": true}, "EpochFlag": obj{"epoch": 21, "on": true},
		"Image": obj{"data": queued}, "TokenCount": obj{"name": "input_tokens", "count": 4200}, "RawRecord": rawRecord, "RawUsage": rawUsage, "Usage": usage,
		"Total": total, "Consumer": consumer,
		"CardCostView": obj{"consumers": arr(consumer, consumer), "total": total, "cut": 3, "carried_usd": "0.5", "carried_from": "a7", "carried_to": "a9"},
		"SnapshotTaken": obj{"file": "snapshot-20261017T010203Z.rdb", "sha256": strings.Repeat("ab", 32), "size": 1048576, "counts": counts, "restore": "semantic",
			"pruned": words("snapshot-20261010T010203Z.rdb")},
		"ColumnCount": obj{"column": "ready", "count": 4}, "BackupCounts": obj{"keys": 40, "columns": arr(obj{"column": "ready", "count": 4}, obj{"column": "working", "count": 2})},
		"DumpKey": obj{"key": "nova:work:meta", "ttl": 0, "payload": "AAECAw=="}, "StatePart": obj{"name": "epoch", "value": "{\"N\":21}"},
		"SprintState": obj{"parts": arr(obj{"name": "coordinator", "value": "rowan"}, obj{"name": "epoch", "value": "{\"N\":21}"})},
		"Column":      column, "ScoredMember": scored, "Cell": cell, "Row": row, "NTable": ntab, "View": view, "FieldChange": fchange,
		"BatchMemberDelta": bmember, "BatchDelta": bdelta, "Receipt": receipt, "Field": field, "MemberSnapshot": member, "MemberEntry": memberEntry,
		"ChangeSnapshot": change, "OpEntry": opEntry, "TextRow": epochSnap["texts"].([]any)[0], "EpochSnapshot": epochSnap,
		"EpochEntry": obj{"epoch": 21, "snapshot": epochSnap}, "TableSnapshot": tableSnap, "TableEntry": tableEntry, "ResidueSnap": residue,
		"ResidueEntry": obj{"name": "t-old", "residue": residue}, "ViewEntry": obj{"name": "sprint", "view": view},
		"NoteSnapshot": obj{"id": "1-0", "note": note}, "LineSnapshot": obj{"id": "1-0", "line": line}, "NoteEntry": obj{"id": "n12", "note": note},
		"TimeEntry": obj{"name": "s1", "at": 1792000000000000000}, "LogSnapshot": logSnap, "LogEntry": obj{"epoch": 21, "log": logSnap}, "MemSnapshot": memSnap,
		"MemDumpMember": obj{"table_name": "t-work", "member": member},
		"Tokens":        tokens, "SnapshotCounts": counts,
	}
	out := map[string]string{}
	for k, v := range s {
		out[k] = encode(t, v)
	}
	return out
}

// ---------------------------------------------------------------------------
// The fixed cases.

func fixedCases(s map[string]string) map[string]func(t *testing.T) {
	return map[string]func(t *testing.T){
		"SnapshotCounts": func(t *testing.T) {
			runFixed(t, "SnapshotCounts", s["SnapshotCounts"], fixedOps[wire.SnapshotCounts]{wire.SnapshotCountsReset, wire.SnapshotCountsSave, wire.SnapshotCountsLoad,
				wire.SnapshotCountsFromJson, wire.SnapshotCountsToJsonMeasure, wire.SnapshotCountsToJson, wire.SnapshotCountsTableType})
		},
		"Tokens": func(t *testing.T) {
			runFixed(t, "Tokens", s["Tokens"], fixedOps[wire.Tokens]{wire.TokensReset, wire.TokensSave, wire.TokensLoad,
				wire.TokensFromJson, wire.TokensToJsonMeasure, wire.TokensToJson, wire.TokensTableType})
		},
	}
}

// ---------------------------------------------------------------------------
// The Go values the tables replace, as the tables' JSON.

func jnum(v int64) json.Number { return json.Number(strconv.FormatInt(v, 10)) }

func ns(v any) any {
	s, _ := v.(string)
	if s == "" {
		return jnum(0)
	}
	tm, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || tm.IsZero() {
		return jnum(0)
	}
	return jnum(tm.UnixNano())
}

func unns(v any) any {
	n, _ := v.(json.Number)
	i, _ := n.Int64()
	if i == 0 {
		return time.Time{}.Format(time.RFC3339Nano)
	}
	return time.Unix(0, i).UTC().Format(time.RFC3339Nano)
}

func str(v any) any {
	if v == nil {
		return ""
	}
	return v
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func numKeys(m map[string]any) []string {
	keys := sortedKeys(m)
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.ParseUint(keys[i], 10, 64)
		b, _ := strconv.ParseUint(keys[j], 10, 64)
		return a < b
	})
	return keys
}

func mp(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func lst(v any) []any {
	l, _ := v.([]any)
	return l
}

// pairsOf is a map[string]string as the entries the tables hold.
func pairsOf(v any, nameKey string) []any {
	out := []any{}
	m := mp(v)
	for _, k := range sortedKeys(m) {
		out = append(out, obj{nameKey: k, "value": m[k]})
	}
	return out
}

func unpairs(v any, nameKey string) any {
	l := lst(v)
	if len(l) == 0 {
		return nil
	}
	m := map[string]any{}
	for _, e := range l {
		m[e.(map[string]any)[nameKey].(string)] = e.(map[string]any)["value"]
	}
	return m
}

func wordsOf(v any) []any {
	out := []any{}
	for _, s := range lst(v) {
		out = append(out, obj{"text": s})
	}
	return out
}

func unwordsOf(v any) any {
	l := lst(v)
	if len(l) == 0 {
		return nil
	}
	out := []any{}
	for _, e := range l {
		out = append(out, e.(map[string]any)["text"])
	}
	return out
}

func mapList(v any, f func(any) any) []any {
	out := []any{}
	for _, e := range lst(v) {
		out = append(out, f(e))
	}
	return out
}

func nilIfEmpty(l []any) any {
	if len(l) == 0 {
		return nil
	}
	return l
}

func flagsOf(v any) []any {
	out := []any{}
	m := mp(v)
	for _, k := range sortedKeys(m) {
		out = append(out, obj{"name": k, "on": m[k]})
	}
	return out
}

func unflags(v any) any {
	l := lst(v)
	if len(l) == 0 {
		return nil
	}
	m := map[string]any{}
	for _, e := range l {
		m[e.(map[string]any)["name"].(string)] = e.(map[string]any)["on"]
	}
	return m
}

// ---- forward: the Go JSON of the twin's document to the tables' JSON ----------

func fwdColumn(v any) any {
	c := mp(v)
	return obj{"name": c["Name"], "label": c["Label"], "projection": c["Projection"], "fold": c["Fold"], "width": c["Width"]}
}

func fwdCell(v any) any {
	c := mp(v)
	return obj{"key": c["Key"], "bound": c["Bound"], "count": c["Count"], "unread": c["Unread"], "unread_why": c["UnreadWhy"],
		"members": mapList(c["Members"], func(m any) any { return obj{"member": mp(m)["Member"], "score": mp(m)["Score"]} })}
}

func fwdRow(v any) any {
	r := mp(v)
	return obj{"key": r["Key"], "label": r["Label"], "exclude": r["Exclude"], "owner": r["Owner"], "hidden": r["Hidden"],
		"cells": mapList(r["Cells"], fwdCell), "texts": pairsOf(r["Texts"], "name")}
}

func fwdTable(v any) any {
	d := mp(v)
	return obj{"epoch_key": d["EpochKey"], "epoch_field": d["EpochField"], "member_prefix": d["MemberPrefix"], "epoch": d["Epoch"], "revision": d["Revision"],
		"name": d["Name"], "columns": mapList(d["Columns"], fwdColumn), "footer_label": d["FooterLabel"], "hidden": wordsOf(d["Hidden"]),
		"hidden_table": d["HiddenTable"], "sort": d["Sort"], "rows": mapList(d["Rows"], fwdRow), "props": pairsOf(d["Props"], "name")}
}

func fwdFieldChanges(v any) []any {
	out := []any{}
	m := mp(v)
	for _, k := range sortedKeys(m) {
		f := mp(m[k])
		out = append(out, obj{"name": k, "before": f["before"], "after": f["after"], "before_bytes": jnumOf(f["before_bytes"]), "before_sha1": str(f["before_sha1"]),
			"after_bytes": jnumOf(f["after_bytes"]), "after_sha1": str(f["after_sha1"])})
	}
	return out
}

func jnumOf(v any) any {
	if v == nil {
		return jnum(0)
	}
	return v
}

func fwdBatchMember(v any) any {
	b := mp(v)
	return obj{"id": b["id"], "before_place": b["before_place"], "after_place": b["after_place"], "before_score_text": b["before_score"], "after_score_text": b["after_score"],
		"before_rev": b["before_rev"], "after_rev": b["after_rev"], "fields_set": pairsOf(b["fields_set"], "name"), "fields_unset": wordsOf(b["fields_unset"]),
		"fields": fwdFieldChanges(b["fields"])}
}

func fwdReceipt(v any) any {
	r := mp(v)
	var delta any
	if b := mp(r["BatchDelta"]); b != nil {
		delta = obj{"operation_id": b["operation_id"], "digest": b["digest"], "actor": b["actor"], "selected_count": b["selected_count"], "guard_count": b["guard_count"],
			"changed_count": b["changed_count"], "members": mapList(b["members"], fwdBatchMember), "props": pairsOf(b["props"], "name")}
	}
	return obj{"id": r["ID"], "epoch": r["Epoch"], "before": r["Before"], "after": r["After"], "outcome": r["Outcome"], "batch_delta": delta, "replay": r["Replay"]}
}

func fwdMember(v any) any {
	m := mp(v)
	return obj{"epoch": m["epoch"], "placed": m["placed"], "row": str(m["row"]), "col": str(m["col"]), "score": m["score"], "rev": m["rev"], "fields": pairsOf(m["fields"], "name")}
}

func fwdEpoch(v any) any {
	e := mp(v)
	texts := []any{}
	for _, row := range sortedKeys(mp(e["texts"])) {
		texts = append(texts, obj{"row": row, "cells": pairsOf(mp(e["texts"])[row], "name")})
	}
	return obj{"rows": wordsOf(e["rows"]), "hidden": flagsOf(e["hidden"]), "texts": texts, "props": pairsOf(e["props"], "name")}
}

func fwdTableSnapshot(v any) any {
	ts := mp(v)
	epochs, wrote, members, ops := []any{}, []any{}, []any{}, []any{}
	for _, k := range numKeys(mp(ts["epochs"])) {
		n, _ := strconv.ParseUint(k, 10, 64)
		epochs = append(epochs, obj{"epoch": n, "snapshot": fwdEpoch(mp(ts["epochs"])[k])})
	}
	for _, k := range numKeys(mp(ts["wrote"])) {
		n, _ := strconv.ParseUint(k, 10, 64)
		wrote = append(wrote, obj{"epoch": n, "on": mp(ts["wrote"])[k]})
	}
	for _, k := range sortedKeys(mp(ts["members"])) {
		members = append(members, obj{"id": k, "member": fwdMember(mp(ts["members"])[k])})
	}
	for _, k := range sortedKeys(mp(ts["ops"])) {
		o := mp(mp(ts["ops"])[k])
		ops = append(ops, obj{"key": k, "body": o["body"], "receipt": fwdReceipt(o["receipt"])})
	}
	changes := mapList(ts["changes"], func(c any) any {
		m := mp(c)
		return obj{"epoch": m["epoch"], "before": m["before"], "after": m["after"], "verb": m["verb"], "ids": wordsOf(m["ids"])}
	})
	return obj{"def": fwdTable(ts["def"]), "epochs": epochs, "wrote": wrote, "rev": ts["rev"], "members": members, "ops": ops, "changes": changes}
}

func fwdView(v any) any {
	w := mp(v)
	return obj{"name": w["Name"], "tables": wordsOf(w["Tables"]), "title": w["Title"], "summary": w["Summary"], "state": w["State"]}
}

func fwdLog(t *testing.T, v any) any {
	l := mp(v)
	for _, k := range []string{"fence", "inbox", "lines", "notes", "queue"} {
		require.Empty(t, l[k], "the real document keeps its oprows records out of this conversion: %s", k)
	}
	progress := []any{}
	for _, k := range sortedKeys(mp(l["progress"])) {
		progress = append(progress, obj{"name": k, "at": ns(mp(l["progress"])[k])})
	}
	return obj{"fence": nil, "gen": l["gen"], "done": pairsOf(l["done"], "name"), "progress": progress, "inbox": []any{}, "lines": []any{}, "notes": []any{},
		"aliases": pairsOf(l["aliases"], "name"), "answered": pairsOf(l["answered"], "name"), "open": pairsOf(l["open"], "name"), "cursor": str(l["cursor"]), "queue": []any{}}
}

func fwdMem(t *testing.T, doc string) obj {
	s := mp(decode(t, doc))
	tables, dropped, views, logs := []any{}, []any{}, []any{}, []any{}
	for _, k := range sortedKeys(mp(s["tables"])) {
		tables = append(tables, obj{"name": k, "table_snapshot": fwdTableSnapshot(mp(s["tables"])[k])})
	}
	for _, k := range sortedKeys(mp(s["dropped"])) {
		r := mp(mp(s["dropped"])[k])
		var ts any
		if r["table"] != nil {
			ts = fwdTableSnapshot(r["table"])
		}
		dropped = append(dropped, obj{"name": k, "residue": obj{"keys": flagsOf(r["keys"]), "table_snapshot": ts}})
	}
	for _, k := range sortedKeys(mp(s["views"])) {
		views = append(views, obj{"name": k, "view": fwdView(mp(s["views"])[k])})
	}
	for _, k := range numKeys(mp(s["logs"])) {
		n, _ := strconv.ParseUint(k, 10, 64)
		logs = append(logs, obj{"epoch": n, "log": fwdLog(t, mp(s["logs"])[k])})
	}
	return obj{"version": s["version"], "tables": tables, "dropped": dropped, "views": views, "epoch_set": s["epoch_set"], "epoch": s["epoch"], "cleared": ns(s["cleared"]),
		"owed": s["owed"], "defaults_owed": jnumOf(s["defaults_owed"]), "logs": logs, "kv": pairsOf(s["kv"], "name"), "seq": s["seq"]}
}

// ---- inverse: the tables' JSON to the Go JSON of the twin's document ----------

func invColumn(v any) any {
	c := mp(v)
	return obj{"Name": c["name"], "Label": c["label"], "Projection": c["projection"], "Fold": c["fold"], "Width": c["width"]}
}

func invCell(v any) any {
	c := mp(v)
	return obj{"Key": c["key"], "Bound": c["bound"], "Count": c["count"], "Unread": c["unread"], "UnreadWhy": c["unread_why"],
		"Members": nilIfEmpty(mapList(c["members"], func(m any) any { return obj{"Member": mp(m)["member"], "Score": mp(m)["score"]} }))}
}

func invRow(v any) any {
	r := mp(v)
	return obj{"Key": r["key"], "Label": r["label"], "Exclude": r["exclude"], "Owner": r["owner"], "Hidden": r["hidden"],
		"Cells": nilIfEmpty(mapList(r["cells"], invCell)), "Texts": unpairs(r["texts"], "name")}
}

func invTable(v any) any {
	d := mp(v)
	return obj{"EpochKey": d["epoch_key"], "EpochField": d["epoch_field"], "MemberPrefix": d["member_prefix"], "Epoch": d["epoch"], "Revision": d["revision"],
		"Name": d["name"], "Columns": nilIfEmpty(mapList(d["columns"], invColumn)), "FooterLabel": d["footer_label"], "Hidden": unwordsOf(d["hidden"]),
		"HiddenTable": d["hidden_table"], "Sort": d["sort"], "Rows": nilIfEmpty(mapList(d["rows"], invRow)), "Props": unpairs(d["props"], "name")}
}

func invReceipt(v any) any {
	r := mp(v)
	var delta any
	if b := mp(r["batch_delta"]); b != nil {
		delta = obj{"operation_id": b["operation_id"], "digest": b["digest"], "actor": b["actor"], "selected_count": b["selected_count"], "guard_count": b["guard_count"],
			"changed_count": b["changed_count"], "props": unpairs(b["props"], "name"), "members": mapList(b["members"], func(m any) any {
				x := mp(m)
				fields := obj{}
				for _, e := range lst(x["fields"]) {
					f := mp(e)
					fc := obj{"before": f["before"], "after": f["after"]}
					if n, _ := f["before_bytes"].(json.Number); n != "0" && n != "" {
						fc["before_bytes"] = f["before_bytes"]
					}
					if n, _ := f["after_bytes"].(json.Number); n != "0" && n != "" {
						fc["after_bytes"] = f["after_bytes"]
					}
					if f["before_sha1"] != "" {
						fc["before_sha1"] = f["before_sha1"]
					}
					if f["after_sha1"] != "" {
						fc["after_sha1"] = f["after_sha1"]
					}
					fields[f["name"].(string)] = fc
				}
				set := unpairs(x["fields_set"], "name")
				if set == nil {
					set = obj{}
				}
				unset := unwordsOf(x["fields_unset"])
				if unset == nil {
					unset = []any{}
				}
				return obj{"id": x["id"], "before_place": x["before_place"], "after_place": x["after_place"], "before_score": x["before_score_text"], "after_score": x["after_score_text"],
					"before_rev": x["before_rev"], "after_rev": x["after_rev"], "fields_set": set, "fields_unset": unset, "fields": fields}
			})}
	}
	return obj{"ID": r["id"], "Epoch": r["epoch"], "Before": r["before"], "After": r["after"], "Outcome": r["outcome"], "BatchDelta": delta, "Replay": r["replay"]}
}

func invEpoch(v any) any {
	e := mp(v)
	texts := obj{}
	for _, r := range lst(e["texts"]) {
		row := mp(r)
		texts[row["row"].(string)] = unpairs(row["cells"], "name")
	}
	out := obj{}
	if l := unwordsOf(e["rows"]); l != nil {
		out["rows"] = l
	}
	if h := unflags(e["hidden"]); h != nil {
		out["hidden"] = h
	}
	if len(texts) > 0 {
		out["texts"] = texts
	}
	if p := unpairs(e["props"], "name"); p != nil {
		out["props"] = p
	}
	return out
}

func invTableSnapshot(v any) any {
	ts := mp(v)
	out := obj{"def": invTable(ts["def"]), "rev": ts["rev"]}
	epochs, wrote, members, ops := obj{}, obj{}, obj{}, obj{}
	for _, e := range lst(ts["epochs"]) {
		x := mp(e)
		epochs[fmt.Sprint(x["epoch"])] = invEpoch(x["snapshot"])
	}
	for _, e := range lst(ts["wrote"]) {
		x := mp(e)
		wrote[fmt.Sprint(x["epoch"])] = x["on"]
	}
	for _, e := range lst(ts["members"]) {
		x := mp(e)
		m := mp(x["member"])
		mm := obj{"epoch": m["epoch"], "placed": m["placed"], "score": m["score"], "rev": m["rev"]}
		if m["row"] != "" {
			mm["row"] = m["row"]
		}
		if m["col"] != "" {
			mm["col"] = m["col"]
		}
		if f := unpairs(m["fields"], "name"); f != nil {
			mm["fields"] = f
		}
		members[x["id"].(string)] = mm
	}
	for _, e := range lst(ts["ops"]) {
		x := mp(e)
		ops[x["key"].(string)] = obj{"body": x["body"], "receipt": invReceipt(x["receipt"])}
	}
	for k, v := range map[string]obj{"epochs": epochs, "wrote": wrote, "members": members, "ops": ops} {
		if len(v) > 0 {
			out[k] = v
		}
	}
	if l := lst(ts["changes"]); len(l) > 0 {
		out["changes"] = mapList(l, func(c any) any {
			m := mp(c)
			ch := obj{"epoch": m["epoch"], "before": m["before"], "after": m["after"], "verb": m["verb"]}
			if ids := unwordsOf(m["ids"]); ids != nil {
				ch["ids"] = ids
			}
			return ch
		})
	}
	return out
}

func invMem(v any) obj {
	s := mp(v)
	tables, dropped, views, logs := obj{}, obj{}, obj{}, obj{}
	for _, e := range lst(s["tables"]) {
		x := mp(e)
		tables[x["name"].(string)] = invTableSnapshot(x["table_snapshot"])
	}
	for _, e := range lst(s["dropped"]) {
		x := mp(e)
		r := mp(x["residue"])
		res := obj{"table": nil}
		if r["table_snapshot"] != nil {
			res["table"] = invTableSnapshot(r["table_snapshot"])
		}
		if k := unflags(r["keys"]); k != nil {
			res["keys"] = k
		}
		dropped[x["name"].(string)] = res
	}
	for _, e := range lst(s["views"]) {
		x := mp(e)
		w := mp(x["view"])
		views[x["name"].(string)] = obj{"Name": w["name"], "Tables": unwordsOf(w["tables"]), "Title": w["title"], "Summary": w["summary"], "State": w["state"]}
	}
	for _, e := range lst(s["logs"]) {
		x := mp(e)
		l := mp(x["log"])
		lg := obj{"gen": l["gen"]}
		if d := unpairs(l["done"], "name"); d != nil {
			lg["done"] = d
		}
		if p := lst(l["progress"]); len(p) > 0 {
			pr := obj{}
			for _, q := range p {
				pr[mp(q)["name"].(string)] = unns(mp(q)["at"])
			}
			lg["progress"] = pr
		}
		for _, k := range []string{"aliases", "answered", "open"} {
			if m := unpairs(l[k], "name"); m != nil {
				lg[k] = m
			}
		}
		if l["cursor"] != "" {
			lg["cursor"] = l["cursor"]
		}
		logs[fmt.Sprint(x["epoch"])] = lg
	}
	out := obj{"version": s["version"], "tables": tables, "epoch_set": s["epoch_set"], "epoch": s["epoch"], "cleared": unns(s["cleared"]), "owed": s["owed"], "seq": s["seq"]}
	if len(dropped) > 0 {
		out["dropped"] = dropped
	}
	if len(views) > 0 {
		out["views"] = views
	}
	if len(logs) > 0 {
		out["logs"] = logs
	}
	if n, _ := s["defaults_owed"].(json.Number); n != "0" && n != "" {
		out["defaults_owed"] = s["defaults_owed"]
	}
	if kv := unpairs(s["kv"], "name"); kv != nil {
		out["kv"] = kv
	}
	return out
}

// ---------------------------------------------------------------------------
// A real twin document through the tables.

// goReceipt is a receipt as the twin records an applied batch.
func goReceipt() obj {
	return obj{"ID": "1-0", "Epoch": 21, "Before": 1, "After": 2, "Outcome": "changed", "Replay": false,
		"BatchDelta": obj{"operation_id": "op1", "digest": "d1", "actor": "beta", "selected_count": 1, "guard_count": 0, "changed_count": 1,
			"members": arr(obj{"id": "s1-1", "before_place": "", "after_place": "r1:state", "before_score": nil, "after_score": "0.5", "before_rev": "0", "after_rev": "1",
				"fields_set": obj{"brief": "t"}, "fields_unset": arr("old"),
				"fields": obj{"brief": obj{"before": nil, "after": "t", "after_bytes": 1, "after_sha1": "da4b9237bacccdf19c0760cab7aec4a8359010b0"}}}),
			"props": obj{"stream": "s1"}}}
}

func goTable(name string, brief string) obj {
	return obj{
		"def": obj{"EpochKey": "e", "EpochField": "n", "MemberPrefix": "w:", "Epoch": 21, "Revision": 7, "Name": name,
			"Columns": arr(obj{"Name": "state", "Label": "State", "Projection": "count", "Fold": "sum", "Width": 8}), "FooterLabel": "total", "Hidden": arr("note"), "HiddenTable": true,
			"Sort": "-name", "Rows": arr(obj{"Key": "r1", "Label": "Row one", "Exclude": "s1-9", "Owner": "finish", "Hidden": true, "Texts": obj{"state": "active"},
				"Cells": arr(obj{"Key": "k:r1:state", "Bound": true, "Count": 2, "Unread": true, "UnreadWhy": "unread",
					"Members": arr(obj{"Member": "s1-1", "Score": 0.5}, obj{"Member": "s1-2", "Score": 1.25})})}),
			"Props": obj{"stream": "s1"}},
		"epochs":  obj{"0": obj{"rows": arr("r1", "r2"), "hidden": obj{"r2": true}, "texts": obj{"r1": obj{"state": "active"}}, "props": obj{"stream": "s1"}}, "21": obj{"rows": arr("r3")}},
		"wrote":   obj{"0": true, "21": true},
		"rev":     2,
		"members": obj{"s1-1": obj{"epoch": 21, "placed": true, "row": "r1", "col": "state", "score": 0.5, "rev": 1, "fields": obj{"brief": brief, "gen": "1"}}, "s1-2": obj{"epoch": 21, "placed": false, "score": 0, "rev": 0}},
		"ops":     obj{"21:op1": obj{"body": "{}", "receipt": goReceipt()}},
		"changes": arr(obj{"epoch": 21, "before": 1, "after": 2, "verb": "apply", "ids": arr("s1-1")}),
	}
}

// pastViewState, pastMemberID, pastColumns and pastProps are values past the limits
// the writers enforce (ntable.MaxViewState, LimitMemberIDBytes, LimitColumns and
// LimitTableProps): the snapshot copies them as they are, so the tables hold them.
var (
	pastViewState = strings.Repeat("s", ntable.MaxViewState+1)
	pastMemberID  = strings.Repeat("i", ntable.LimitMemberIDBytes+1)
)

func pastColumns() []any {
	cols := make([]any, 0, ntable.LimitColumns+1)
	for i := 0; i <= ntable.LimitColumns; i++ {
		cols = append(cols, obj{"Name": "c" + strconv.Itoa(i), "Label": "C", "Projection": "count", "Fold": "sum", "Width": 8})
	}
	return cols
}

func pastProps() obj {
	props := obj{}
	for i := 0; i <= ntable.LimitTableProps; i++ {
		props["p"+strconv.Itoa(i)] = "v"
	}
	return props
}

func goMemDoc(t *testing.T) string {
	work := goTable("t-work", strings.Repeat("b", 2*ntable.LimitFieldValueBytes))
	work["def"].(obj)["Columns"] = pastColumns()
	work["epochs"].(obj)["0"].(obj)["props"] = pastProps()
	work["members"].(obj)[pastMemberID] = obj{"epoch": 21, "placed": false, "score": 0, "rev": 0}
	doc := obj{"version": 1, "tables": obj{"t-work": work, "t-fleet": goTable("t-fleet", "short")},
		"dropped":   obj{"t-old": obj{"keys": obj{"w:s1-1": true}, "table": goTable("t-old", "old")}},
		"views":     obj{"sprint": obj{"Name": "sprint", "Tables": arr("t-work", "t-fleet"), "Title": "The sprint", "Summary": "state", "State": pastViewState}},
		"epoch_set": true, "epoch": 21, "cleared": "2026-10-17T01:02:03.000000004Z", "owed": true, "defaults_owed": 21,
		"logs": obj{"21": obj{"gen": 3, "done": obj{"caller-1": "ok"}, "progress": obj{"s1": "2026-10-17T01:02:03Z", "s2": "0001-01-01T00:00:00Z"},
			"aliases": obj{"j12": "n12"}, "answered": obj{"n12": "coordinator"}, "open": obj{"s1/s1-1": "n12"}, "cursor": "1-0"}, "3": obj{"gen": 1}},
		"kv": obj{"coordinator": "rowan"}, "seq": 9}
	return encode(t, doc)
}

func memVarOps() varOps[wire.MemSnapshot] {
	return varOps[wire.MemSnapshot]{
		builder((*wire.MemSnapshotBuilder).Init, (*wire.MemSnapshotBuilder).GetRoot, func(b *wire.MemSnapshotBuilder) *wire.TableArena { return &b.Arena }, wire.MemSnapshotFromJson),
		wire.MemSnapshotMeasure, wire.MemSnapshotSave, wire.MemSnapshotLoadMeasureReason, wire.MemSnapshotLoad, wire.MemSnapshotToJsonMeasure, wire.MemSnapshotToJson, wire.MemSnapshotTableType,
		nil}
}

func TestSnapshotDocumentSurvivesTheTables(t *testing.T) {
	t.Parallel()
	first := store.NewMem()
	require.NoError(t, first.Restore([]byte(goMemDoc(t))), "the document is a twin snapshot")
	s1, err := first.Snapshot()
	require.NoError(t, err)

	fwd := fwdMem(t, string(s1))
	text := encode(t, fwd)
	ops := memVarOps()
	codec := ops.codec("MemSnapshot")
	r := schemaread.NewReader(codec)
	v, _, err := r.Read([]byte(text))
	require.NoError(t, err, "the twin's document reads as the MemSnapshot table")
	canon := string(codec.SaveJSON(&v))
	require.True(t, contains(decode(t, text), decode(t, canon)), "every field of the document survives the table: %s", canon)

	bin, err := r.Write(&v, schemaread.FormBinary)
	require.NoError(t, err)
	for form, data := range map[string][]byte{"binary": bin, "base64": []byte(b64(bin)), "JSON": []byte(canon)} {
		got, _, err := r.Read(data)
		require.NoError(t, err, form)
		back := invMem(decode(t, string(codec.SaveJSON(&got))))
		second := store.NewMem()
		require.NoError(t, second.Restore([]byte(encode(t, back))), "%s: the document the table gives back is a twin snapshot", form)
		s2, err := second.Snapshot()
		require.NoError(t, err)
		assert.Equal(t, string(s1), string(s2), "the document is the same after the tables, read from %s", form)
	}
	// a field value of twice the write bound is held whole: the snapshot copies it as it is
	assert.Contains(t, canon, strings.Repeat("b", 2*ntable.LimitFieldValueBytes))
	// and so is every value past a limit the writers enforce: a view state of 65
	// bytes, 1001 columns, 65 epoch props and a member id of 257 bytes
	assert.Contains(t, canon, pastViewState)
	assert.Contains(t, canon, pastMemberID)
	assert.Contains(t, canon, `"c`+strconv.Itoa(ntable.LimitColumns)+`"`)
	assert.Contains(t, canon, `"p`+strconv.Itoa(ntable.LimitTableProps)+`"`)
}

// ---- the dump of a real twin, key by key ------------------------------------

func TestDumpOfARealTwinSurvivesTheTables(t *testing.T) {
	t.Parallel()
	m := store.NewMem()
	require.NoError(t, m.Restore([]byte(goMemDoc(t))))
	keys, err := store.MemDump(m, 21)
	require.NoError(t, err)
	require.NotEmpty(t, keys)
	dumpOps := varOps[wire.DumpKey]{
		builder((*wire.DumpKeyBuilder).Init, (*wire.DumpKeyBuilder).GetRoot, func(b *wire.DumpKeyBuilder) *wire.TableArena { return &b.Arena }, wire.DumpKeyFromJson),
		wire.DumpKeyMeasure, wire.DumpKeySave, wire.DumpKeyLoadMeasureReason, wire.DumpKeyLoad, wire.DumpKeyToJsonMeasure, wire.DumpKeyToJson, wire.DumpKeyTableType, nil}
	memberOps := varOps[wire.MemDumpMember]{
		builder((*wire.MemDumpMemberBuilder).Init, (*wire.MemDumpMemberBuilder).GetRoot, func(b *wire.MemDumpMemberBuilder) *wire.TableArena { return &b.Arena }, wire.MemDumpMemberFromJson),
		wire.MemDumpMemberMeasure, wire.MemDumpMemberSave, wire.MemDumpMemberLoadMeasureReason, wire.MemDumpMemberLoad, wire.MemDumpMemberToJsonMeasure, wire.MemDumpMemberToJson, wire.MemDumpMemberTableType,
		nil}
	dr := schemaread.NewReader(dumpOps.codec("DumpKey"))
	mr := schemaread.NewReader(memberOps.codec("MemDumpMember"))
	members := 0
	for _, k := range keys {
		text := encode(t, obj{"key": k.Key, "ttl": k.TTL, "payload": k.Payload})
		v, _, err := dr.Read([]byte(text))
		require.NoError(t, err, k.Key)
		assert.True(t, contains(decode(t, text), decode(t, string(dumpOps.codec("DumpKey").SaveJSON(&v)))), "the dump key %s survives", k.Key)
		bin, err := dr.Write(&v, schemaread.FormBinary)
		require.NoError(t, err)
		for _, data := range [][]byte{bin, []byte(b64(bin))} {
			got, _, err := dr.Read(data)
			require.NoError(t, err)
			assert.Equal(t, string(k.Payload), string(wire.TableBytesAt(&got.Payload).Data[:wire.TableBytesAt(&got.Payload).Length]), "the payload of %s is held whole", k.Key)
		}
		var pm map[string]any
		d := json.NewDecoder(bytes.NewReader(k.Payload))
		d.UseNumber()
		if d.Decode(&pm) != nil || pm["member"] == nil || pm["table"] == nil {
			continue
		}
		members++
		mtext := encode(t, obj{"table_name": pm["table"], "member": fwdMember(pm["member"])})
		mv, _, err := mr.Read([]byte(mtext))
		require.NoError(t, err, k.Key)
		assert.True(t, contains(decode(t, mtext), decode(t, string(memberOps.codec("MemDumpMember").SaveJSON(&mv)))), "the dump member of %s survives", k.Key)
		mbin, err := mr.Write(&mv, schemaread.FormBinary)
		require.NoError(t, err)
		_, _, err = mr.Read([]byte(b64(mbin)))
		require.NoError(t, err)
	}
	assert.Positive(t, members, "the dump holds member keys")
}

// ---- the snapshot verb's records and the restore state ----------------------

func TestSnapshotRecordsFromGoValues(t *testing.T) {
	t.Parallel()
	taken := store.SnapshotTaken{File: "snapshot-20261017T010203Z.rdb", SHA256: strings.Repeat("0f", 32), Bytes: 1 << 20, Counts: store.SnapshotCounts{Keys: 128, Cards: -1},
		Restore: store.RestoreSemantic, Pruned: []string{"snapshot-20261010T010203Z.rdb", "snapshot-20261011T010203Z.rdb"}}
	raw, err := json.Marshal(taken)
	require.NoError(t, err)
	g := mp(decode(t, string(raw)))
	text := encode(t, obj{"file": g["file"], "sha256": g["sha256"], "size": g["bytes"], "counts": g["counts"], "restore": g["restore"], "pruned": wordsOf(g["pruned"])})
	takenOps := varOps[wire.SnapshotTaken]{
		builder((*wire.SnapshotTakenBuilder).Init, (*wire.SnapshotTakenBuilder).GetRoot, func(b *wire.SnapshotTakenBuilder) *wire.TableArena { return &b.Arena }, wire.SnapshotTakenFromJson),
		wire.SnapshotTakenMeasure, wire.SnapshotTakenSave, wire.SnapshotTakenLoadMeasureReason, wire.SnapshotTakenLoad, wire.SnapshotTakenToJsonMeasure, wire.SnapshotTakenToJson, wire.SnapshotTakenTableType, nil}
	roundTrip(t, "SnapshotTaken", schemaread.NewReader(takenOps.codec("SnapshotTaken")), takenOps.codec("SnapshotTaken"), text)

	bc := store.BackupCounts{Keys: 40, Columns: map[string]int{"ready": 4, "working": 2}}
	raw, err = json.Marshal(bc)
	require.NoError(t, err)
	g = mp(decode(t, string(raw)))
	cols := []any{}
	for _, k := range sortedKeys(mp(g["columns"])) {
		cols = append(cols, obj{"column": k, "count": mp(g["columns"])[k]})
	}
	bcOps := varOps[wire.BackupCounts]{
		builder((*wire.BackupCountsBuilder).Init, (*wire.BackupCountsBuilder).GetRoot, func(b *wire.BackupCountsBuilder) *wire.TableArena { return &b.Arena }, wire.BackupCountsFromJson),
		wire.BackupCountsMeasure, wire.BackupCountsSave, wire.BackupCountsLoadMeasureReason, wire.BackupCountsLoad, wire.BackupCountsToJsonMeasure, wire.BackupCountsToJson, wire.BackupCountsTableType, nil}
	roundTrip(t, "BackupCounts", schemaread.NewReader(bcOps.codec("BackupCounts")), bcOps.codec("BackupCounts"), encode(t, obj{"keys": g["keys"], "columns": cols}))

	st := store.SprintState{Parts: map[string]string{"epoch": `{"N":21}`, "coordinator": "rowan", "row work s1-1": strings.Repeat("p", 70000)}}
	raw, err = json.Marshal(st)
	require.NoError(t, err)
	g = mp(decode(t, string(raw)))
	stOps := varOps[wire.SprintState]{
		builder((*wire.SprintStateBuilder).Init, (*wire.SprintStateBuilder).GetRoot, func(b *wire.SprintStateBuilder) *wire.TableArena { return &b.Arena }, wire.SprintStateFromJson),
		wire.SprintStateMeasure, wire.SprintStateSave, wire.SprintStateLoadMeasureReason, wire.SprintStateLoad, wire.SprintStateToJsonMeasure, wire.SprintStateToJson, wire.SprintStateTableType, nil}
	canon := roundTrip(t, "SprintState", schemaread.NewReader(stOps.codec("SprintState")), stOps.codec("SprintState"), encode(t, obj{"parts": pairsOf(g["parts"], "name")}))
	assert.Contains(t, canon, strings.Repeat("p", 70000), "a part of 70000 bytes is held whole")
}

// ---- the card cost records from a real card ---------------------------------

func renameKey(v any, from, to string) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			if k == from {
				k = to
			}
			out[k] = renameKey(e, from, to)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = renameKey(e, from, to)
		}
		return out
	}
	return v
}

func TestCardCostFromARealCard(t *testing.T) {
	t.Parallel()
	u := cardcost.ParseUsage("input=19541 cache_read=36336 cache_write=0 output=692 reasoning=70 requests=6 max_prompt=9861 model=opencode/deepseek-v4-pro cost=0.04219614")
	line := func(card string, gen int) string {
		return fmt.Sprintf("kind=work card=%s attempt=1 take=0 gen=%d who=friend.rowan on_route=pro-a on_model=opencode/deepseek-v4-pro on_tier=pro end=landed at=2026-10-17T01:0%d:03Z lane_cap=30m lane_overrun=4m %s",
			card, gen, gen, u.String())
	}
	card := &sprint.Card{ID: "a8", Fields: map[string]string{
		sprint.FieldCostRecord + "a8#g1": line("a8", 1), sprint.FieldCostRecord + "a8#g2": line("a8", 2),
		sprint.FieldCostTotal: cardcost.NoTotal().Add(u).Add(u).String(), sprint.FieldCostCut: "3",
	}}
	view := sprint.CardCostOf(card)
	require.Len(t, view.Consumers, 2)
	raw, err := json.Marshal(view)
	require.NoError(t, err)
	text := encode(t, renameKey(decode(t, string(raw)), "long", "long_context"))
	ops := varOps[wire.CardCostView]{
		builder((*wire.CardCostViewBuilder).Init, (*wire.CardCostViewBuilder).GetRoot, func(b *wire.CardCostViewBuilder) *wire.TableArena { return &b.Arena }, wire.CardCostViewFromJson),
		wire.CardCostViewMeasure, wire.CardCostViewSave, wire.CardCostViewLoadMeasureReason, wire.CardCostViewLoad, wire.CardCostViewToJsonMeasure, wire.CardCostViewToJson, wire.CardCostViewTableType, nil}
	r := schemaread.NewReader(ops.codec("CardCostView"))
	v, _, err := r.Read([]byte(text))
	require.NoError(t, err, "the card's cost view reads as the CardCostView table: %s", text)
	canon := string(ops.codec("CardCostView").SaveJSON(&v))
	assert.True(t, contains(decode(t, text), decode(t, canon)), "every field of the cost view survives:\n%s\n%s", text, canon)
	bin, err := r.Write(&v, schemaread.FormBinary)
	require.NoError(t, err)
	got, _, err := r.Read([]byte(b64(bin)))
	require.NoError(t, err)
	assert.Equal(t, canon, string(ops.codec("CardCostView").SaveJSON(&got)))
	assert.EqualValues(t, 2, v.Consumers.Count)

	// a card that holds more cost records than sprint.MaxCostRecords: CardCostOf
	// appends every one it finds, and the open list holds every one
	many := &sprint.Card{ID: "a9", Fields: map[string]string{}}
	for i := 0; i < sprint.MaxCostRecords+6; i++ {
		many.Fields[fmt.Sprintf("%sa9#g%d", sprint.FieldCostRecord, i)] = line("a9", i%10)
	}
	bigView := sprint.CardCostOf(many)
	require.Greater(t, len(bigView.Consumers), sprint.MaxCostRecords)
	rawBig, err := json.Marshal(bigView)
	require.NoError(t, err)
	bigText := encode(t, renameKey(decode(t, string(rawBig)), "long", "long_context"))
	bv, _, err := r.Read([]byte(bigText))
	require.NoError(t, err, "a cost view past sprint.MaxCostRecords reads")
	assert.EqualValues(t, len(bigView.Consumers), bv.Consumers.Count)
	binBig, err := r.Write(&bv, schemaread.FormBinary)
	require.NoError(t, err)
	_, _, err = r.Read([]byte(b64(binBig)))
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// A bound is declared only where every reader path enforces it (S1).

// TestNoRecordDeclaresAnUnenforcedBound pins the area's bound table: the snapshot
// read and the restore copy every list and text as they find it, so no table
// declares a bound, and every record the restore reads is open. The limits the
// code enforces on a write or on a batch apply are not bounds of these records.
func TestNoRecordDeclaresAnUnenforcedBound(t *testing.T) {
	t.Parallel()
	for table, info := range tableInfos() {
		assert.Empty(t, boundLists(info), "%s declares a list bound the restore does not enforce", table)
		assert.Empty(t, boundStrings(info), "%s declares a text bound the restore does not enforce", table)
	}
}

// ---------------------------------------------------------------------------
// Nothing serialized in the area lacks a table.

// mapped is every struct of the four source files that is serialized, and the
// table that holds it. A struct of those files absent here must be named in
// notSerialized with the reason it is not.
var mapped = map[string]string{
	"SnapshotCounts": "SnapshotCounts", "SnapshotTaken": "SnapshotTaken", "DumpKey": "DumpKey", "BackupCounts": "BackupCounts", "memDumpMember": "MemDumpMember",
	"SprintState": "SprintState", "memSnapshot": "MemSnapshot", "tableSnapshot": "TableSnapshot", "epochSnapshot": "EpochSnapshot", "memberSnapshot": "MemberSnapshot",
	"changeSnapshot": "ChangeSnapshot", "opSnapshot": "OpEntry", "residueSnap": "ResidueSnap", "logSnapshot": "LogSnapshot", "noteSnapshot": "NoteSnapshot",
	"lineSnapshot": "LineSnapshot", "Consumer": "Consumer", "CardCostView": "CardCostView",
}

var notSerialized = map[string]string{
	"Snapshotter":   "the verb's worker; it holds a directory, a count and the source and the twin",
	"RDBTwin":       "an empty type: the integrity check of an RDB",
	"MemSource":     "a store handle and the names; it writes the document, it is not one",
	"MemTwin":       "the names; it reads the document, it is not one",
	"DecideCostReq": "a request in memory (a stream, a call's usage by card); the records it writes are Consumer",
}

// external is the serialized types of nova-tools the area's records hold, and the
// table that holds each.
var external = map[string]struct {
	typ   reflect.Type
	table string
}{
	"cardcost.Usage":          {reflect.TypeOf(cardcost.Usage{}), "Usage"},
	"cardcost.Tokens":         {reflect.TypeOf(cardcost.Tokens{}), "Tokens"},
	"cardcost.Total":          {reflect.TypeOf(cardcost.Total{}), "Total"},
	"cardcost.RawUsage":       {reflect.TypeOf(cardcost.RawUsage{}), "RawUsage"},
	"cardcost.RawRecord":      {reflect.TypeOf(cardcost.RawRecord{}), "RawRecord"},
	"ntable.Table":            {reflect.TypeOf(ntable.Table{}), "NTable"},
	"ntable.Column":           {reflect.TypeOf(ntable.Column{}), "Column"},
	"ntable.Row":              {reflect.TypeOf(ntable.Row{}), "Row"},
	"ntable.Cell":             {reflect.TypeOf(ntable.Cell{}), "Cell"},
	"ntable.Member":           {reflect.TypeOf(ntable.Member{}), "ScoredMember"},
	"ntable.View":             {reflect.TypeOf(ntable.View{}), "View"},
	"ntable.Receipt":          {reflect.TypeOf(ntable.Receipt{}), "Receipt"},
	"ntable.BatchDelta":       {reflect.TypeOf(ntable.BatchDelta{}), "BatchDelta"},
	"ntable.BatchMemberDelta": {reflect.TypeOf(ntable.BatchMemberDelta{}), "BatchMemberDelta"},
	"ntable.FieldChange":      {reflect.TypeOf(ntable.FieldChange{}), "FieldChange"},
}

// renamed are the Go fields whose table field has another name: the schema
// language or C++ reserves the Go one.
var renamed = map[string]string{
	"SnapshotTaken.bytes": "size", "Usage.long": "long_context", "memDumpMember.table": "table_name", "residueSnap.table": "table_snapshot",
	"BatchMemberDelta.before_score": "before_score_text", "BatchMemberDelta.after_score": "after_score_text",
}

func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' && !(s[i-1] >= 'A' && s[i-1] <= 'Z') {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

func jsonName(goName, tag string) (name string, skip bool) {
	if tag == "-" {
		return "", true
	}
	if i := strings.Index(tag, ","); i >= 0 {
		tag = tag[:i]
	}
	if tag != "" {
		return tag, false
	}
	return snake(goName), false
}

func hasField(info *wire.TableTypeInfo, name string) bool {
	for _, f := range info.Fields {
		if f.Json == name {
			return true
		}
	}
	return false
}

func TestNothingSerializedLacksATable(t *testing.T) {
	t.Parallel()
	infos := tableInfos()
	files := []string{
		filepath.Join("..", "snapshot.go"), filepath.Join("..", "mem_snapshot.go"), filepath.Join("..", "restore.go"), filepath.Join("..", "..", "cost.go"),
	}
	seen := map[string]bool{}
	fset := token.NewFileSet()
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, err)
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, isStruct := ts.Type.(*ast.StructType)
				if !isStruct {
					continue
				}
				name := ts.Name.Name
				seen[name] = true
				table, isMapped := mapped[name]
				_, isNot := notSerialized[name]
				require.True(t, isMapped != isNot, "the struct %s of %s is serialized (mapped to a table) or named not serialized, not both and not neither", name, filepath.Base(path))
				if isNot {
					continue
				}
				info, ok := infos[table]
				require.True(t, ok, "%s: the table %s exists", name, table)
				for _, fld := range st.Fields.List {
					tag := ""
					if fld.Tag != nil {
						tag, _ = strconv.Unquote(fld.Tag.Value)
						tag = reflect.StructTag(tag).Get("json")
					}
					for _, id := range fld.Names {
						jn, skip := jsonName(id.Name, tag)
						if skip {
							continue
						}
						if r, ok := renamed[name+"."+jn]; ok {
							jn = r
						}
						assert.True(t, hasField(info, jn), "%s.%s (json %s) has no field in the table %s", name, id.Name, jn, table)
					}
				}
			}
		}
	}
	for name := range mapped {
		assert.True(t, seen[name], "the mapped struct %s is in the source files", name)
	}
	for name := range notSerialized {
		assert.True(t, seen[name], "the struct %s named not serialized is in the source files", name)
	}
	for name, e := range external {
		info, ok := infos[e.table]
		require.True(t, ok, "%s: the table %s exists", name, e.table)
		short := name[strings.Index(name, ".")+1:]
		for i := 0; i < e.typ.NumField(); i++ {
			fld := e.typ.Field(i)
			jn, skip := jsonName(fld.Name, fld.Tag.Get("json"))
			if skip {
				continue
			}
			if r, ok := renamed[short+"."+jn]; ok {
				jn = r
			}
			assert.True(t, hasField(info, jn), "%s.%s (json %s) has no field in the table %s", name, fld.Name, jn, e.table)
		}
	}
	// every table is held by a struct of the area or is an entry of a map or list
	entries := map[string]bool{"Word": true, "Pair": true, "Flag": true, "EpochFlag": true, "Image": true, "TokenCount": true, "ColumnCount": true, "StatePart": true,
		"FieldChange": true, "Field": true, "MemberEntry": true, "OpEntry": true, "TextRow": true, "EpochEntry": true, "TableEntry": true, "ResidueEntry": true,
		"ViewEntry": true, "NoteEntry": true, "TimeEntry": true, "LogEntry": true}
	held := map[string]bool{}
	for _, tb := range mapped {
		held[tb] = true
	}
	for _, e := range external {
		held[e.table] = true
	}
	for name := range infos {
		assert.True(t, held[name] || entries[name], "the table %s holds no struct of the area and is no entry of a map", name)
	}
}

// ---------------------------------------------------------------------------
// The nested records of the oprows area decode with the oprows tables.

func TestNestedRecordsAreOprowsTables(t *testing.T) {
	t.Parallel()
	s := sampleSet(t)
	log := mp(decode(t, s["LogSnapshot"]))
	assert.Equal(t, "n12", oprowsJSON(t, "Note", mp(lst(log["inbox"])[0])["note"].(string))["id"])
	assert.Equal(t, "n12", oprowsJSON(t, "Note", mp(lst(log["notes"])[0])["note"].(string))["id"])
	assert.Equal(t, "card-a", oprowsJSON(t, "LogLine", mp(lst(log["lines"])[0])["line"].(string))["card"])
	assert.Equal(t, "finish-ab12", oprowsJSON(t, "OpRecord", log["fence"].(string))["id"])
	assert.Equal(t, "beta", oprowsJSON(t, "QueuedChange", mp(lst(log["queue"])[0])["data"].(string))["actor"])
}

// ---------------------------------------------------------------------------
// The lock, the baseline and the pin.

func readLock(t *testing.T, path string) (layouts map[string]string) {
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	layouts = map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		w := strings.Fields(sc.Text())
		if len(w) >= 4 && w[0] == "fixed" && w[1] == "table" {
			layouts[w[2]] = strings.TrimPrefix(w[3], "layout=")
		}
	}
	require.NoError(t, sc.Err())
	return layouts
}

func TestSnapshotAndCostGolden(t *testing.T) {
	t.Parallel()

	s := sampleSet(t)
	fixed, variable := fixedCases(s), variableCases(s)
	require.Len(t, fixed, len(fixedLayouts), "a case for every fixed table")
	require.Len(t, variable, len(variableNames), "a case for every variable table")

	var names []string
	for _, v := range wire.UnitView().Tables {
		names = append(names, v.Name)
	}
	var want []string
	for name := range fixedLayouts {
		want = append(want, name)
	}
	want = append(want, variableNames...)
	sort.Strings(want)
	sort.Strings(names)
	require.Equal(t, want, names, "the area's tables")
	require.Len(t, want, 51)

	for name, run := range fixed {
		t.Run("fixed/"+name, func(t *testing.T) {
			t.Parallel()
			run(t)
		})
	}
	for name, run := range variable {
		t.Run("variable/"+name, func(t *testing.T) {
			t.Parallel()
			run(t)
		})
	}

	t.Run("lock baseline and pin", func(t *testing.T) {
		t.Parallel()
		root := filepath.Join("..", "..", "..", "..", "schemas", "snaprows")
		assert.Equal(t, fixedLayouts, readLock(t, filepath.Join(root, "schema.lock")), "schema.lock holds the fixed layouts this test pins, and only those")

		base, err := os.ReadFile(filepath.Join(root, "tables.baseline"))
		require.NoError(t, err, "schemas/snaprows/tables.baseline is committed")
		var inBase []string
		for _, line := range strings.Split(string(base), "\n") {
			if n, ok := strings.CutPrefix(line, "table "); ok {
				inBase = append(inBase, strings.TrimSpace(n))
			}
		}
		sort.Strings(inBase)
		assert.Equal(t, want, inBase, "the baseline covers every table of the area")

		// the open walk is not vacuous: the five fields that were once declared bounded are open
		named := func(fs []wire.TableFieldInfo, name string) bool {
			for _, f := range fs {
				if f.Name == name {
					return true
				}
			}
			return false
		}
		assert.True(t, named(openLists(wire.CardCostViewTableType()), "consumers"))
		assert.True(t, named(openLists(wire.NTableTableType()), "columns"))
		assert.True(t, named(openLists(wire.EpochSnapshotTableType()), "props"))
		assert.True(t, named(pointerFields(wire.ViewTableType(), "string"), "state"))
		assert.True(t, named(pointerFields(wire.MemberEntryTableType(), "string"), "id"))

		_, err = os.Stat(filepath.Join(root, "snaprows.schema"))
		require.NoError(t, err, "schemas/snaprows/snaprows.schema is committed")
		gen, err := os.ReadFile("generate.go")
		require.NoError(t, err)
		assert.Contains(t, string(gen), "schema/v2/cmd/schema@v2.5.1-0.20261014163537-4d9941fbc7fd generate --lang go --out . ../../../../schemas/snaprows", "generate.go pins the repository's schema pin")
	})
}

// variableCases is the mechanical part of this file: one entry per variable table.
func variableCases(s map[string]string) map[string]func(t *testing.T) {
	return map[string]func(t *testing.T){
		"Word": func(t *testing.T) {
			runVariable(t, "Word", s["Word"], varOps[wire.Word]{
				builder((*wire.WordBuilder).Init, (*wire.WordBuilder).GetRoot, func(b *wire.WordBuilder) *wire.TableArena { return &b.Arena }, wire.WordFromJson),
				wire.WordMeasure, wire.WordSave, wire.WordLoadMeasureReason, wire.WordLoad, wire.WordToJsonMeasure, wire.WordToJson, wire.WordTableType, nil})
		},
		"Pair": func(t *testing.T) {
			runVariable(t, "Pair", s["Pair"], varOps[wire.Pair]{
				builder((*wire.PairBuilder).Init, (*wire.PairBuilder).GetRoot, func(b *wire.PairBuilder) *wire.TableArena { return &b.Arena }, wire.PairFromJson),
				wire.PairMeasure, wire.PairSave, wire.PairLoadMeasureReason, wire.PairLoad, wire.PairToJsonMeasure, wire.PairToJson, wire.PairTableType, nil})
		},
		"Flag": func(t *testing.T) {
			runVariable(t, "Flag", s["Flag"], varOps[wire.Flag]{
				builder((*wire.FlagBuilder).Init, (*wire.FlagBuilder).GetRoot, func(b *wire.FlagBuilder) *wire.TableArena { return &b.Arena }, wire.FlagFromJson),
				wire.FlagMeasure, wire.FlagSave, wire.FlagLoadMeasureReason, wire.FlagLoad, wire.FlagToJsonMeasure, wire.FlagToJson, wire.FlagTableType, nil})
		},
		"EpochFlag": func(t *testing.T) {
			runVariable(t, "EpochFlag", s["EpochFlag"], varOps[wire.EpochFlag]{
				builder((*wire.EpochFlagBuilder).Init, (*wire.EpochFlagBuilder).GetRoot, func(b *wire.EpochFlagBuilder) *wire.TableArena { return &b.Arena }, wire.EpochFlagFromJson),
				wire.EpochFlagMeasure, wire.EpochFlagSave, wire.EpochFlagLoadMeasureReason, wire.EpochFlagLoad, wire.EpochFlagToJsonMeasure, wire.EpochFlagToJson, wire.EpochFlagTableType, nil})
		},
		"Image": func(t *testing.T) {
			runVariable(t, "Image", s["Image"], varOps[wire.Image]{
				builder((*wire.ImageBuilder).Init, (*wire.ImageBuilder).GetRoot, func(b *wire.ImageBuilder) *wire.TableArena { return &b.Arena }, wire.ImageFromJson),
				wire.ImageMeasure, wire.ImageSave, wire.ImageLoadMeasureReason, wire.ImageLoad, wire.ImageToJsonMeasure, wire.ImageToJson, wire.ImageTableType, nil})
		},
		"TokenCount": func(t *testing.T) {
			runVariable(t, "TokenCount", s["TokenCount"], varOps[wire.TokenCount]{
				builder((*wire.TokenCountBuilder).Init, (*wire.TokenCountBuilder).GetRoot, func(b *wire.TokenCountBuilder) *wire.TableArena { return &b.Arena }, wire.TokenCountFromJson),
				wire.TokenCountMeasure, wire.TokenCountSave, wire.TokenCountLoadMeasureReason, wire.TokenCountLoad, wire.TokenCountToJsonMeasure, wire.TokenCountToJson, wire.TokenCountTableType, nil})
		},
		"RawRecord": func(t *testing.T) {
			runVariable(t, "RawRecord", s["RawRecord"], varOps[wire.RawRecord]{
				builder((*wire.RawRecordBuilder).Init, (*wire.RawRecordBuilder).GetRoot, func(b *wire.RawRecordBuilder) *wire.TableArena { return &b.Arena }, wire.RawRecordFromJson),
				wire.RawRecordMeasure, wire.RawRecordSave, wire.RawRecordLoadMeasureReason, wire.RawRecordLoad, wire.RawRecordToJsonMeasure, wire.RawRecordToJson, wire.RawRecordTableType, nil})
		},
		"RawUsage": func(t *testing.T) {
			runVariable(t, "RawUsage", s["RawUsage"], varOps[wire.RawUsage]{
				builder((*wire.RawUsageBuilder).Init, (*wire.RawUsageBuilder).GetRoot, func(b *wire.RawUsageBuilder) *wire.TableArena { return &b.Arena }, wire.RawUsageFromJson),
				wire.RawUsageMeasure, wire.RawUsageSave, wire.RawUsageLoadMeasureReason, wire.RawUsageLoad, wire.RawUsageToJsonMeasure, wire.RawUsageToJson, wire.RawUsageTableType, nil})
		},
		"Usage": func(t *testing.T) {
			runVariable(t, "Usage", s["Usage"], varOps[wire.Usage]{
				builder((*wire.UsageBuilder).Init, (*wire.UsageBuilder).GetRoot, func(b *wire.UsageBuilder) *wire.TableArena { return &b.Arena }, wire.UsageFromJson),
				wire.UsageMeasure, wire.UsageSave, wire.UsageLoadMeasureReason, wire.UsageLoad, wire.UsageToJsonMeasure, wire.UsageToJson, wire.UsageTableType, nil})
		},
		"Total": func(t *testing.T) {
			runVariable(t, "Total", s["Total"], varOps[wire.Total]{
				builder((*wire.TotalBuilder).Init, (*wire.TotalBuilder).GetRoot, func(b *wire.TotalBuilder) *wire.TableArena { return &b.Arena }, wire.TotalFromJson),
				wire.TotalMeasure, wire.TotalSave, wire.TotalLoadMeasureReason, wire.TotalLoad, wire.TotalToJsonMeasure, wire.TotalToJson, wire.TotalTableType, nil})
		},
		"Consumer": func(t *testing.T) {
			runVariable(t, "Consumer", s["Consumer"], varOps[wire.Consumer]{
				builder((*wire.ConsumerBuilder).Init, (*wire.ConsumerBuilder).GetRoot, func(b *wire.ConsumerBuilder) *wire.TableArena { return &b.Arena }, wire.ConsumerFromJson),
				wire.ConsumerMeasure, wire.ConsumerSave, wire.ConsumerLoadMeasureReason, wire.ConsumerLoad, wire.ConsumerToJsonMeasure, wire.ConsumerToJson, wire.ConsumerTableType, nil})
		},
		"CardCostView": func(t *testing.T) {
			runVariable(t, "CardCostView", s["CardCostView"], varOps[wire.CardCostView]{
				builder((*wire.CardCostViewBuilder).Init, (*wire.CardCostViewBuilder).GetRoot, func(b *wire.CardCostViewBuilder) *wire.TableArena { return &b.Arena }, wire.CardCostViewFromJson),
				wire.CardCostViewMeasure, wire.CardCostViewSave, wire.CardCostViewLoadMeasureReason, wire.CardCostViewLoad, wire.CardCostViewToJsonMeasure, wire.CardCostViewToJson, wire.CardCostViewTableType, nil})
		},
		"SnapshotTaken": func(t *testing.T) {
			runVariable(t, "SnapshotTaken", s["SnapshotTaken"], varOps[wire.SnapshotTaken]{
				builder((*wire.SnapshotTakenBuilder).Init, (*wire.SnapshotTakenBuilder).GetRoot, func(b *wire.SnapshotTakenBuilder) *wire.TableArena { return &b.Arena }, wire.SnapshotTakenFromJson),
				wire.SnapshotTakenMeasure, wire.SnapshotTakenSave, wire.SnapshotTakenLoadMeasureReason, wire.SnapshotTakenLoad, wire.SnapshotTakenToJsonMeasure, wire.SnapshotTakenToJson, wire.SnapshotTakenTableType, nil})
		},
		"ColumnCount": func(t *testing.T) {
			runVariable(t, "ColumnCount", s["ColumnCount"], varOps[wire.ColumnCount]{
				builder((*wire.ColumnCountBuilder).Init, (*wire.ColumnCountBuilder).GetRoot, func(b *wire.ColumnCountBuilder) *wire.TableArena { return &b.Arena }, wire.ColumnCountFromJson),
				wire.ColumnCountMeasure, wire.ColumnCountSave, wire.ColumnCountLoadMeasureReason, wire.ColumnCountLoad, wire.ColumnCountToJsonMeasure, wire.ColumnCountToJson, wire.ColumnCountTableType, nil})
		},
		"BackupCounts": func(t *testing.T) {
			runVariable(t, "BackupCounts", s["BackupCounts"], varOps[wire.BackupCounts]{
				builder((*wire.BackupCountsBuilder).Init, (*wire.BackupCountsBuilder).GetRoot, func(b *wire.BackupCountsBuilder) *wire.TableArena { return &b.Arena }, wire.BackupCountsFromJson),
				wire.BackupCountsMeasure, wire.BackupCountsSave, wire.BackupCountsLoadMeasureReason, wire.BackupCountsLoad, wire.BackupCountsToJsonMeasure, wire.BackupCountsToJson, wire.BackupCountsTableType, nil})
		},
		"DumpKey": func(t *testing.T) {
			runVariable(t, "DumpKey", s["DumpKey"], varOps[wire.DumpKey]{
				builder((*wire.DumpKeyBuilder).Init, (*wire.DumpKeyBuilder).GetRoot, func(b *wire.DumpKeyBuilder) *wire.TableArena { return &b.Arena }, wire.DumpKeyFromJson),
				wire.DumpKeyMeasure, wire.DumpKeySave, wire.DumpKeyLoadMeasureReason, wire.DumpKeyLoad, wire.DumpKeyToJsonMeasure, wire.DumpKeyToJson, wire.DumpKeyTableType, nil})
		},
		"StatePart": func(t *testing.T) {
			runVariable(t, "StatePart", s["StatePart"], varOps[wire.StatePart]{
				builder((*wire.StatePartBuilder).Init, (*wire.StatePartBuilder).GetRoot, func(b *wire.StatePartBuilder) *wire.TableArena { return &b.Arena }, wire.StatePartFromJson),
				wire.StatePartMeasure, wire.StatePartSave, wire.StatePartLoadMeasureReason, wire.StatePartLoad, wire.StatePartToJsonMeasure, wire.StatePartToJson, wire.StatePartTableType, nil})
		},
		"SprintState": func(t *testing.T) {
			runVariable(t, "SprintState", s["SprintState"], varOps[wire.SprintState]{
				builder((*wire.SprintStateBuilder).Init, (*wire.SprintStateBuilder).GetRoot, func(b *wire.SprintStateBuilder) *wire.TableArena { return &b.Arena }, wire.SprintStateFromJson),
				wire.SprintStateMeasure, wire.SprintStateSave, wire.SprintStateLoadMeasureReason, wire.SprintStateLoad, wire.SprintStateToJsonMeasure, wire.SprintStateToJson, wire.SprintStateTableType, nil})
		},
		"Column": func(t *testing.T) {
			runVariable(t, "Column", s["Column"], varOps[wire.Column]{
				builder((*wire.ColumnBuilder).Init, (*wire.ColumnBuilder).GetRoot, func(b *wire.ColumnBuilder) *wire.TableArena { return &b.Arena }, wire.ColumnFromJson),
				wire.ColumnMeasure, wire.ColumnSave, wire.ColumnLoadMeasureReason, wire.ColumnLoad, wire.ColumnToJsonMeasure, wire.ColumnToJson, wire.ColumnTableType, nil})
		},
		"ScoredMember": func(t *testing.T) {
			runVariable(t, "ScoredMember", s["ScoredMember"], varOps[wire.ScoredMember]{
				builder((*wire.ScoredMemberBuilder).Init, (*wire.ScoredMemberBuilder).GetRoot, func(b *wire.ScoredMemberBuilder) *wire.TableArena { return &b.Arena }, wire.ScoredMemberFromJson),
				wire.ScoredMemberMeasure, wire.ScoredMemberSave, wire.ScoredMemberLoadMeasureReason, wire.ScoredMemberLoad, wire.ScoredMemberToJsonMeasure, wire.ScoredMemberToJson, wire.ScoredMemberTableType, nil})
		},
		"Cell": func(t *testing.T) {
			runVariable(t, "Cell", s["Cell"], varOps[wire.Cell]{
				builder((*wire.CellBuilder).Init, (*wire.CellBuilder).GetRoot, func(b *wire.CellBuilder) *wire.TableArena { return &b.Arena }, wire.CellFromJson),
				wire.CellMeasure, wire.CellSave, wire.CellLoadMeasureReason, wire.CellLoad, wire.CellToJsonMeasure, wire.CellToJson, wire.CellTableType, nil})
		},
		"Row": func(t *testing.T) {
			runVariable(t, "Row", s["Row"], varOps[wire.Row]{
				builder((*wire.RowBuilder).Init, (*wire.RowBuilder).GetRoot, func(b *wire.RowBuilder) *wire.TableArena { return &b.Arena }, wire.RowFromJson),
				wire.RowMeasure, wire.RowSave, wire.RowLoadMeasureReason, wire.RowLoad, wire.RowToJsonMeasure, wire.RowToJson, wire.RowTableType, nil})
		},
		"NTable": func(t *testing.T) {
			runVariable(t, "NTable", s["NTable"], varOps[wire.NTable]{
				builder((*wire.NTableBuilder).Init, (*wire.NTableBuilder).GetRoot, func(b *wire.NTableBuilder) *wire.TableArena { return &b.Arena }, wire.NTableFromJson),
				wire.NTableMeasure, wire.NTableSave, wire.NTableLoadMeasureReason, wire.NTableLoad, wire.NTableToJsonMeasure, wire.NTableToJson, wire.NTableTableType, nil})
		},
		"View": func(t *testing.T) {
			runVariable(t, "View", s["View"], varOps[wire.View]{
				builder((*wire.ViewBuilder).Init, (*wire.ViewBuilder).GetRoot, func(b *wire.ViewBuilder) *wire.TableArena { return &b.Arena }, wire.ViewFromJson),
				wire.ViewMeasure, wire.ViewSave, wire.ViewLoadMeasureReason, wire.ViewLoad, wire.ViewToJsonMeasure, wire.ViewToJson, wire.ViewTableType, nil})
		},
		"FieldChange": func(t *testing.T) {
			runVariable(t, "FieldChange", s["FieldChange"], varOps[wire.FieldChange]{
				builder((*wire.FieldChangeBuilder).Init, (*wire.FieldChangeBuilder).GetRoot, func(b *wire.FieldChangeBuilder) *wire.TableArena { return &b.Arena }, wire.FieldChangeFromJson),
				wire.FieldChangeMeasure, wire.FieldChangeSave, wire.FieldChangeLoadMeasureReason, wire.FieldChangeLoad, wire.FieldChangeToJsonMeasure, wire.FieldChangeToJson, wire.FieldChangeTableType, nil})
		},
		"BatchMemberDelta": func(t *testing.T) {
			runVariable(t, "BatchMemberDelta", s["BatchMemberDelta"], varOps[wire.BatchMemberDelta]{
				builder((*wire.BatchMemberDeltaBuilder).Init, (*wire.BatchMemberDeltaBuilder).GetRoot, func(b *wire.BatchMemberDeltaBuilder) *wire.TableArena { return &b.Arena }, wire.BatchMemberDeltaFromJson),
				wire.BatchMemberDeltaMeasure, wire.BatchMemberDeltaSave, wire.BatchMemberDeltaLoadMeasureReason, wire.BatchMemberDeltaLoad, wire.BatchMemberDeltaToJsonMeasure, wire.BatchMemberDeltaToJson, wire.BatchMemberDeltaTableType, nil})
		},
		"BatchDelta": func(t *testing.T) {
			runVariable(t, "BatchDelta", s["BatchDelta"], varOps[wire.BatchDelta]{
				builder((*wire.BatchDeltaBuilder).Init, (*wire.BatchDeltaBuilder).GetRoot, func(b *wire.BatchDeltaBuilder) *wire.TableArena { return &b.Arena }, wire.BatchDeltaFromJson),
				wire.BatchDeltaMeasure, wire.BatchDeltaSave, wire.BatchDeltaLoadMeasureReason, wire.BatchDeltaLoad, wire.BatchDeltaToJsonMeasure, wire.BatchDeltaToJson, wire.BatchDeltaTableType, nil})
		},
		"Receipt": func(t *testing.T) {
			runVariable(t, "Receipt", s["Receipt"], varOps[wire.Receipt]{
				builder((*wire.ReceiptBuilder).Init, (*wire.ReceiptBuilder).GetRoot, func(b *wire.ReceiptBuilder) *wire.TableArena { return &b.Arena }, wire.ReceiptFromJson),
				wire.ReceiptMeasure, wire.ReceiptSave, wire.ReceiptLoadMeasureReason, wire.ReceiptLoad, wire.ReceiptToJsonMeasure, wire.ReceiptToJson, wire.ReceiptTableType, nil})
		},
		"Field": func(t *testing.T) {
			runVariable(t, "Field", s["Field"], varOps[wire.Field]{
				builder((*wire.FieldBuilder).Init, (*wire.FieldBuilder).GetRoot, func(b *wire.FieldBuilder) *wire.TableArena { return &b.Arena }, wire.FieldFromJson),
				wire.FieldMeasure, wire.FieldSave, wire.FieldLoadMeasureReason, wire.FieldLoad, wire.FieldToJsonMeasure, wire.FieldToJson, wire.FieldTableType, nil})
		},
		"MemberSnapshot": func(t *testing.T) {
			runVariable(t, "MemberSnapshot", s["MemberSnapshot"], varOps[wire.MemberSnapshot]{
				builder((*wire.MemberSnapshotBuilder).Init, (*wire.MemberSnapshotBuilder).GetRoot, func(b *wire.MemberSnapshotBuilder) *wire.TableArena { return &b.Arena }, wire.MemberSnapshotFromJson),
				wire.MemberSnapshotMeasure, wire.MemberSnapshotSave, wire.MemberSnapshotLoadMeasureReason, wire.MemberSnapshotLoad, wire.MemberSnapshotToJsonMeasure, wire.MemberSnapshotToJson, wire.MemberSnapshotTableType, nil})
		},
		"MemberEntry": func(t *testing.T) {
			runVariable(t, "MemberEntry", s["MemberEntry"], varOps[wire.MemberEntry]{
				builder((*wire.MemberEntryBuilder).Init, (*wire.MemberEntryBuilder).GetRoot, func(b *wire.MemberEntryBuilder) *wire.TableArena { return &b.Arena }, wire.MemberEntryFromJson),
				wire.MemberEntryMeasure, wire.MemberEntrySave, wire.MemberEntryLoadMeasureReason, wire.MemberEntryLoad, wire.MemberEntryToJsonMeasure, wire.MemberEntryToJson, wire.MemberEntryTableType, nil})
		},
		"ChangeSnapshot": func(t *testing.T) {
			runVariable(t, "ChangeSnapshot", s["ChangeSnapshot"], varOps[wire.ChangeSnapshot]{
				builder((*wire.ChangeSnapshotBuilder).Init, (*wire.ChangeSnapshotBuilder).GetRoot, func(b *wire.ChangeSnapshotBuilder) *wire.TableArena { return &b.Arena }, wire.ChangeSnapshotFromJson),
				wire.ChangeSnapshotMeasure, wire.ChangeSnapshotSave, wire.ChangeSnapshotLoadMeasureReason, wire.ChangeSnapshotLoad, wire.ChangeSnapshotToJsonMeasure, wire.ChangeSnapshotToJson, wire.ChangeSnapshotTableType, nil})
		},
		"OpEntry": func(t *testing.T) {
			runVariable(t, "OpEntry", s["OpEntry"], varOps[wire.OpEntry]{
				builder((*wire.OpEntryBuilder).Init, (*wire.OpEntryBuilder).GetRoot, func(b *wire.OpEntryBuilder) *wire.TableArena { return &b.Arena }, wire.OpEntryFromJson),
				wire.OpEntryMeasure, wire.OpEntrySave, wire.OpEntryLoadMeasureReason, wire.OpEntryLoad, wire.OpEntryToJsonMeasure, wire.OpEntryToJson, wire.OpEntryTableType, nil})
		},
		"TextRow": func(t *testing.T) {
			runVariable(t, "TextRow", s["TextRow"], varOps[wire.TextRow]{
				builder((*wire.TextRowBuilder).Init, (*wire.TextRowBuilder).GetRoot, func(b *wire.TextRowBuilder) *wire.TableArena { return &b.Arena }, wire.TextRowFromJson),
				wire.TextRowMeasure, wire.TextRowSave, wire.TextRowLoadMeasureReason, wire.TextRowLoad, wire.TextRowToJsonMeasure, wire.TextRowToJson, wire.TextRowTableType, nil})
		},
		"EpochSnapshot": func(t *testing.T) {
			runVariable(t, "EpochSnapshot", s["EpochSnapshot"], varOps[wire.EpochSnapshot]{
				builder((*wire.EpochSnapshotBuilder).Init, (*wire.EpochSnapshotBuilder).GetRoot, func(b *wire.EpochSnapshotBuilder) *wire.TableArena { return &b.Arena }, wire.EpochSnapshotFromJson),
				wire.EpochSnapshotMeasure, wire.EpochSnapshotSave, wire.EpochSnapshotLoadMeasureReason, wire.EpochSnapshotLoad, wire.EpochSnapshotToJsonMeasure, wire.EpochSnapshotToJson, wire.EpochSnapshotTableType, nil})
		},
		"EpochEntry": func(t *testing.T) {
			runVariable(t, "EpochEntry", s["EpochEntry"], varOps[wire.EpochEntry]{
				builder((*wire.EpochEntryBuilder).Init, (*wire.EpochEntryBuilder).GetRoot, func(b *wire.EpochEntryBuilder) *wire.TableArena { return &b.Arena }, wire.EpochEntryFromJson),
				wire.EpochEntryMeasure, wire.EpochEntrySave, wire.EpochEntryLoadMeasureReason, wire.EpochEntryLoad, wire.EpochEntryToJsonMeasure, wire.EpochEntryToJson, wire.EpochEntryTableType, nil})
		},
		"TableSnapshot": func(t *testing.T) {
			runVariable(t, "TableSnapshot", s["TableSnapshot"], varOps[wire.TableSnapshot]{
				builder((*wire.TableSnapshotBuilder).Init, (*wire.TableSnapshotBuilder).GetRoot, func(b *wire.TableSnapshotBuilder) *wire.TableArena { return &b.Arena }, wire.TableSnapshotFromJson),
				wire.TableSnapshotMeasure, wire.TableSnapshotSave, wire.TableSnapshotLoadMeasureReason, wire.TableSnapshotLoad, wire.TableSnapshotToJsonMeasure, wire.TableSnapshotToJson, wire.TableSnapshotTableType, nil})
		},
		"TableEntry": func(t *testing.T) {
			runVariable(t, "TableEntry", s["TableEntry"], varOps[wire.TableEntry]{
				builder((*wire.TableEntryBuilder).Init, (*wire.TableEntryBuilder).GetRoot, func(b *wire.TableEntryBuilder) *wire.TableArena { return &b.Arena }, wire.TableEntryFromJson),
				wire.TableEntryMeasure, wire.TableEntrySave, wire.TableEntryLoadMeasureReason, wire.TableEntryLoad, wire.TableEntryToJsonMeasure, wire.TableEntryToJson, wire.TableEntryTableType, nil})
		},
		"ResidueSnap": func(t *testing.T) {
			runVariable(t, "ResidueSnap", s["ResidueSnap"], varOps[wire.ResidueSnap]{
				builder((*wire.ResidueSnapBuilder).Init, (*wire.ResidueSnapBuilder).GetRoot, func(b *wire.ResidueSnapBuilder) *wire.TableArena { return &b.Arena }, wire.ResidueSnapFromJson),
				wire.ResidueSnapMeasure, wire.ResidueSnapSave, wire.ResidueSnapLoadMeasureReason, wire.ResidueSnapLoad, wire.ResidueSnapToJsonMeasure, wire.ResidueSnapToJson, wire.ResidueSnapTableType, nil})
		},
		"ResidueEntry": func(t *testing.T) {
			runVariable(t, "ResidueEntry", s["ResidueEntry"], varOps[wire.ResidueEntry]{
				builder((*wire.ResidueEntryBuilder).Init, (*wire.ResidueEntryBuilder).GetRoot, func(b *wire.ResidueEntryBuilder) *wire.TableArena { return &b.Arena }, wire.ResidueEntryFromJson),
				wire.ResidueEntryMeasure, wire.ResidueEntrySave, wire.ResidueEntryLoadMeasureReason, wire.ResidueEntryLoad, wire.ResidueEntryToJsonMeasure, wire.ResidueEntryToJson, wire.ResidueEntryTableType, nil})
		},
		"ViewEntry": func(t *testing.T) {
			runVariable(t, "ViewEntry", s["ViewEntry"], varOps[wire.ViewEntry]{
				builder((*wire.ViewEntryBuilder).Init, (*wire.ViewEntryBuilder).GetRoot, func(b *wire.ViewEntryBuilder) *wire.TableArena { return &b.Arena }, wire.ViewEntryFromJson),
				wire.ViewEntryMeasure, wire.ViewEntrySave, wire.ViewEntryLoadMeasureReason, wire.ViewEntryLoad, wire.ViewEntryToJsonMeasure, wire.ViewEntryToJson, wire.ViewEntryTableType, nil})
		},
		"NoteSnapshot": func(t *testing.T) {
			runVariable(t, "NoteSnapshot", s["NoteSnapshot"], varOps[wire.NoteSnapshot]{
				builder((*wire.NoteSnapshotBuilder).Init, (*wire.NoteSnapshotBuilder).GetRoot, func(b *wire.NoteSnapshotBuilder) *wire.TableArena { return &b.Arena }, wire.NoteSnapshotFromJson),
				wire.NoteSnapshotMeasure, wire.NoteSnapshotSave, wire.NoteSnapshotLoadMeasureReason, wire.NoteSnapshotLoad, wire.NoteSnapshotToJsonMeasure, wire.NoteSnapshotToJson, wire.NoteSnapshotTableType, nil})
		},
		"LineSnapshot": func(t *testing.T) {
			runVariable(t, "LineSnapshot", s["LineSnapshot"], varOps[wire.LineSnapshot]{
				builder((*wire.LineSnapshotBuilder).Init, (*wire.LineSnapshotBuilder).GetRoot, func(b *wire.LineSnapshotBuilder) *wire.TableArena { return &b.Arena }, wire.LineSnapshotFromJson),
				wire.LineSnapshotMeasure, wire.LineSnapshotSave, wire.LineSnapshotLoadMeasureReason, wire.LineSnapshotLoad, wire.LineSnapshotToJsonMeasure, wire.LineSnapshotToJson, wire.LineSnapshotTableType, nil})
		},
		"NoteEntry": func(t *testing.T) {
			runVariable(t, "NoteEntry", s["NoteEntry"], varOps[wire.NoteEntry]{
				builder((*wire.NoteEntryBuilder).Init, (*wire.NoteEntryBuilder).GetRoot, func(b *wire.NoteEntryBuilder) *wire.TableArena { return &b.Arena }, wire.NoteEntryFromJson),
				wire.NoteEntryMeasure, wire.NoteEntrySave, wire.NoteEntryLoadMeasureReason, wire.NoteEntryLoad, wire.NoteEntryToJsonMeasure, wire.NoteEntryToJson, wire.NoteEntryTableType, nil})
		},
		"TimeEntry": func(t *testing.T) {
			runVariable(t, "TimeEntry", s["TimeEntry"], varOps[wire.TimeEntry]{
				builder((*wire.TimeEntryBuilder).Init, (*wire.TimeEntryBuilder).GetRoot, func(b *wire.TimeEntryBuilder) *wire.TableArena { return &b.Arena }, wire.TimeEntryFromJson),
				wire.TimeEntryMeasure, wire.TimeEntrySave, wire.TimeEntryLoadMeasureReason, wire.TimeEntryLoad, wire.TimeEntryToJsonMeasure, wire.TimeEntryToJson, wire.TimeEntryTableType, nil})
		},
		"LogSnapshot": func(t *testing.T) {
			runVariable(t, "LogSnapshot", s["LogSnapshot"], varOps[wire.LogSnapshot]{
				builder((*wire.LogSnapshotBuilder).Init, (*wire.LogSnapshotBuilder).GetRoot, func(b *wire.LogSnapshotBuilder) *wire.TableArena { return &b.Arena }, wire.LogSnapshotFromJson),
				wire.LogSnapshotMeasure, wire.LogSnapshotSave, wire.LogSnapshotLoadMeasureReason, wire.LogSnapshotLoad, wire.LogSnapshotToJsonMeasure, wire.LogSnapshotToJson, wire.LogSnapshotTableType, nil})
		},
		"LogEntry": func(t *testing.T) {
			runVariable(t, "LogEntry", s["LogEntry"], varOps[wire.LogEntry]{
				builder((*wire.LogEntryBuilder).Init, (*wire.LogEntryBuilder).GetRoot, func(b *wire.LogEntryBuilder) *wire.TableArena { return &b.Arena }, wire.LogEntryFromJson),
				wire.LogEntryMeasure, wire.LogEntrySave, wire.LogEntryLoadMeasureReason, wire.LogEntryLoad, wire.LogEntryToJsonMeasure, wire.LogEntryToJson, wire.LogEntryTableType, nil})
		},
		"MemSnapshot": func(t *testing.T) {
			runVariable(t, "MemSnapshot", s["MemSnapshot"], varOps[wire.MemSnapshot]{
				builder((*wire.MemSnapshotBuilder).Init, (*wire.MemSnapshotBuilder).GetRoot, func(b *wire.MemSnapshotBuilder) *wire.TableArena { return &b.Arena }, wire.MemSnapshotFromJson),
				wire.MemSnapshotMeasure, wire.MemSnapshotSave, wire.MemSnapshotLoadMeasureReason, wire.MemSnapshotLoad, wire.MemSnapshotToJsonMeasure, wire.MemSnapshotToJson, wire.MemSnapshotTableType, nil})
		},
		"MemDumpMember": func(t *testing.T) {
			runVariable(t, "MemDumpMember", s["MemDumpMember"], varOps[wire.MemDumpMember]{
				builder((*wire.MemDumpMemberBuilder).Init, (*wire.MemDumpMemberBuilder).GetRoot, func(b *wire.MemDumpMemberBuilder) *wire.TableArena { return &b.Arena }, wire.MemDumpMemberFromJson),
				wire.MemDumpMemberMeasure, wire.MemDumpMemberSave, wire.MemDumpMemberLoadMeasureReason, wire.MemDumpMemberLoad, wire.MemDumpMemberToJsonMeasure, wire.MemDumpMemberToJson, wire.MemDumpMemberTableType, nil})
		},
	}
}

var variableNames = []string{"Word", "Pair", "Flag", "EpochFlag", "Image", "TokenCount", "RawRecord", "RawUsage", "Usage", "Total", "Consumer", "CardCostView", "SnapshotTaken", "ColumnCount", "BackupCounts", "DumpKey", "StatePart", "SprintState", "Column", "ScoredMember", "Cell", "Row", "NTable", "View", "FieldChange", "BatchMemberDelta", "BatchDelta", "Receipt", "Field", "MemberSnapshot", "MemberEntry", "ChangeSnapshot", "OpEntry", "TextRow", "EpochSnapshot", "EpochEntry", "TableSnapshot", "TableEntry", "ResidueSnap", "ResidueEntry", "ViewEntry", "NoteSnapshot", "LineSnapshot", "NoteEntry", "TimeEntry", "LogSnapshot", "LogEntry", "MemSnapshot", "MemDumpMember"}

// tableInfos is every table of the area by name.
func tableInfos() map[string]*wire.TableTypeInfo {
	return map[string]*wire.TableTypeInfo{
		"BackupCounts":     wire.BackupCountsTableType(),
		"BatchDelta":       wire.BatchDeltaTableType(),
		"BatchMemberDelta": wire.BatchMemberDeltaTableType(),
		"CardCostView":     wire.CardCostViewTableType(),
		"Cell":             wire.CellTableType(),
		"ChangeSnapshot":   wire.ChangeSnapshotTableType(),
		"Column":           wire.ColumnTableType(),
		"ColumnCount":      wire.ColumnCountTableType(),
		"Consumer":         wire.ConsumerTableType(),
		"DumpKey":          wire.DumpKeyTableType(),
		"EpochEntry":       wire.EpochEntryTableType(),
		"EpochFlag":        wire.EpochFlagTableType(),
		"EpochSnapshot":    wire.EpochSnapshotTableType(),
		"Field":            wire.FieldTableType(),
		"FieldChange":      wire.FieldChangeTableType(),
		"Flag":             wire.FlagTableType(),
		"Image":            wire.ImageTableType(),
		"LineSnapshot":     wire.LineSnapshotTableType(),
		"LogEntry":         wire.LogEntryTableType(),
		"LogSnapshot":      wire.LogSnapshotTableType(),
		"MemDumpMember":    wire.MemDumpMemberTableType(),
		"MemSnapshot":      wire.MemSnapshotTableType(),
		"MemberEntry":      wire.MemberEntryTableType(),
		"MemberSnapshot":   wire.MemberSnapshotTableType(),
		"NTable":           wire.NTableTableType(),
		"NoteEntry":        wire.NoteEntryTableType(),
		"NoteSnapshot":     wire.NoteSnapshotTableType(),
		"OpEntry":          wire.OpEntryTableType(),
		"Pair":             wire.PairTableType(),
		"RawRecord":        wire.RawRecordTableType(),
		"RawUsage":         wire.RawUsageTableType(),
		"Receipt":          wire.ReceiptTableType(),
		"ResidueEntry":     wire.ResidueEntryTableType(),
		"ResidueSnap":      wire.ResidueSnapTableType(),
		"Row":              wire.RowTableType(),
		"ScoredMember":     wire.ScoredMemberTableType(),
		"SnapshotCounts":   wire.SnapshotCountsTableType(),
		"SnapshotTaken":    wire.SnapshotTakenTableType(),
		"SprintState":      wire.SprintStateTableType(),
		"StatePart":        wire.StatePartTableType(),
		"TableEntry":       wire.TableEntryTableType(),
		"TableSnapshot":    wire.TableSnapshotTableType(),
		"TextRow":          wire.TextRowTableType(),
		"TimeEntry":        wire.TimeEntryTableType(),
		"TokenCount":       wire.TokenCountTableType(),
		"Tokens":           wire.TokensTableType(),
		"Total":            wire.TotalTableType(),
		"Usage":            wire.UsageTableType(),
		"View":             wire.ViewTableType(),
		"ViewEntry":        wire.ViewEntryTableType(),
		"Word":             wire.WordTableType(),
	}
}
