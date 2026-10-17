package viewrowswire_test

// TestViewRowsGolden is the golden test of the viewrows definitions
// (schemas/viewrows/viewrows.schema, schemas/viewrows/schema.lock,
// schemas/viewrows/tables.baseline and the generated
// internal/sprint/store/viewrowswire): the where record and its stats, the
// stop record, the doctor's refused-verb record and the members' beats, the
// check record, the fleet drop debt, the stats reset and tidy records, the
// stuck record, the load index and the tick's machine, heartbeat and held
// records.
//
// What it pins:
//
//   - every table of the schema round-trips through pkg/schemaread: the fixed
//     tables as the binary wire, padded standard base64 of it and the schema
//     JSON text; the variable tables the same three, each read counted on its
//     form (S2). Each table is run with a small instance and with a large one:
//     text of 70,000 bytes in every unbounded text field of the table,
//     200-entry open lists and 200-key maps;
//   - the real source records round-trip: a value of each Go struct the tables
//     replace (store.WhereRecord, store.Machine, store.OpRecord and the rest),
//     every field of it set, is carried into its table by the field names and
//     the types of the Go struct, read through the reader with no unknown
//     field, no clamp and no mismatch, and written back in each form. A Go
//     field with no table field fails the test, so does a table field no
//     source value reaches. The large instance holds 200 entries in every open
//     list and map of the record, five held streams, 70,000-byte texts and the
//     most the code keeps of a bounded list (10,000 landings);
//   - at each real bound a value at the bound is accepted and one past it is
//     refused by name (clamped), in every form that can carry it, never
//     clamped into the record; the declared bounds are the bounds of the code
//     that enforces them (the table of bounds below is the pull request's);
//   - a damaged image of each form is refused by name;
//   - schema.lock holds exactly the fixed layouts pinned here, the baseline
//     covers every table, and generate.go carries the repository's schema pin.
//
// The test uses no clock, no socket, no network and no program: the instances
// are written as JSON text through the generated reader, the wire is the
// generated writer's, the lock and the baseline are files of the repository,
// and every time a source record holds is a fixed offset from the test clock,
// which starts at 1000 seconds.
//
// Rules cited: S1 (a fixed table where every bound is one the code enforces,
// else a variable table with open lists and unbounded text), S2 (text
// transports carry padded standard base64 of the binary wire and the JSON
// text; a reader accepts each, refuses damage by name and counts reads per
// form), S5 (Go only).

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	wire "github.com/mas-bandwidth/nova-sprint/internal/sprint/store/viewrowswire"
	"github.com/mas-bandwidth/nova-tools/pkg/hostload"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const viewrowsHelp = "nova-sprint check"

// testClock is the test's clock in seconds: it starts at 1000, never 0, so a
// zero time stays distinguishable from a stamp the test wrote.
const testClock = 1000

// longText is the length of the text an unbounded text field holds in the
// large instance: past the 64 KiB a card text may be (sprint.MaxCardTextBytes).
const longText = 70000

// bigCount is the length of an open list and of a map in the large instance.
const bigCount = 200

// fixedLayouts is the layout hash of every fixed table, as schema.lock holds
// it: a change to a fixed table's layout is a change to this list.
var fixedLayouts = map[string]string{
	"CardID":          "0x273f481e85662568",
	"DayGap":          "0xce7142dd3a7652de",
	"FleetMachineRow": "0xd6b14a0eab185879",
	"Landing":         "0xa117046e05ffeb59",
	"LoadSample":      "0xc67f1d024a189f0d",
	"MeterTicks":      "0x1b03c0f2ab6ab9b7",
	"ReadCardCounts":  "0x925a9c0983268d8e",
	"ReadsWindowView": "0x1975ae7fc5988350",
	"ResetRow":        "0xc5f1961498ae96f9",
	"RowName":         "0xe105c98179a20402",
	"RttSample":       "0x07dfaa5c9cf4cfd4",
	"Span":            "0x8c09defdb9dafa76",
	"StageStat":       "0x682e886c08a44494",
	"Suppressed":      "0xe939e7e91d5bf2e5",
}

// ---------------------------------------------------------------------------
// The harness.

// report reduces the generated read report to what the strict read decides on.
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

// refusalOf asserts err is a refusal by name and returns its reason. The
// label is the table's name, then what was read after a "." or a ":".
func refusalOf(t *testing.T, label string, err error) string {
	t.Helper()
	require.Error(t, err, "%s: refused", label)
	var r *schemaread.Refusal
	require.True(t, errors.As(err, &r), "%s: every refusal is a *schemaread.Refusal: %v", label, err)
	name := label
	if i := strings.IndexAny(name, ".:/"); i >= 0 {
		name = name[:i]
	}
	assert.True(t, strings.HasPrefix(err.Error(), strings.ToUpper(name)+" REFUSED: "), "%s: %s", label, shortErr(err))
	assert.NotEmpty(t, r.Reason, "%s: a refusal names its reason", label)
	return r.Reason
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// decode reads JSON text into a generic value, numbers kept as written.
func decode(t *testing.T, text string) any {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(text))
	d.UseNumber()
	var v any
	require.NoError(t, d.Decode(&v), shortErr(errors.New(text)))
	return v
}

func encode(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// contains reports whether every key and value of want is in got: the
// sample's fields survive the round trip, whatever defaults the writer adds.
// A null in want is an absent optional.
func contains(want, got any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, wv := range w {
			if wv == nil {
				continue
			}
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
	case json.Number:
		g, ok := got.(json.Number)
		if !ok {
			return false
		}
		if w == g {
			return true
		}
		wf, werr := w.Float64()
		gf, gerr := g.Float64()
		return werr == nil && gerr == nil && wf == gf
	}
	return reflect.DeepEqual(want, got)
}

// withField is the sample with its top-level key set to v.
func withField(t *testing.T, sample, key string, v any) string {
	t.Helper()
	m := decode(t, sample).(map[string]any)
	m[key] = v
	return encode(t, m)
}

// stringFields is each top-level bounded string field of a table: its text
// key and its declared bound.
func stringFields(info *wire.TableTypeInfo) []wire.TableFieldInfo {
	var out []wire.TableFieldInfo
	for _, f := range info.Fields {
		if f.Kind == 12 && f.Counted && !f.IsArray && !f.Pointer && f.ArrayBound > 0 {
			out = append(out, f)
		}
	}
	return out
}

// arrayFields is each top-level bounded, counted array of a table (of records
// or of numbers): its open lists have no bound to find.
func arrayFields(info *wire.TableTypeInfo) []wire.TableFieldInfo {
	var out []wire.TableFieldInfo
	for _, f := range info.Fields {
		if f.IsArray && f.Counted && !f.List && !f.Map && !f.Pointer && f.ArrayBound > 0 {
			out = append(out, f)
		}
	}
	return out
}

// mapFields is each top-level map of a table.
func mapFields(info *wire.TableTypeInfo) []wire.TableFieldInfo {
	var out []wire.TableFieldInfo
	for _, f := range info.Fields {
		if f.Map {
			out = append(out, f)
		}
	}
	return out
}

// pastBound is the wire image good holds with its string of n marker bytes
// grown to n+1, its length prefix rewritten: the image of a writer that ignored
// the bound. The table wire writes a string as kind 12, the LEB128 length and
// the bytes, so the image differs from good only there.
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

// refuseDamage reads the damaged images of a good wire and asserts each is
// refused by name.
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

// ---------------------------------------------------------------------------
// The samples: an instance of each table, built from its own descriptor.

// namedSample is one instance of a table, as JSON text.
type namedSample struct {
	name string
	json string
}

// sampleOpts say how large an instance is: large is the root's own long text,
// 200-entry open lists and 200-key maps; what a nested table holds stays small.
type sampleOpts struct{ large bool }

func (o sampleOpts) count(f *wire.TableFieldInfo, depth int) int {
	n := 2
	if o.large && depth == 0 {
		n = bigCount
	}
	if f.Counted && f.ArrayBound > 0 && int(f.ArrayBound) < n {
		n = int(f.ArrayBound)
	}
	if !f.Counted && f.ArrayBound > 0 {
		n = int(f.ArrayBound)
	}
	return n
}

func sampleTable(ti *wire.TableTypeInfo, o sampleOpts, depth int) map[string]any {
	m := map[string]any{}
	for i := range ti.Fields {
		f := &ti.Fields[i]
		m[f.Json] = sampleField(f, o, depth)
	}
	return m
}

func sampleField(f *wire.TableFieldInfo, o sampleOpts, depth int) any {
	switch {
	case f.Map:
		entry := f.Table()
		n := 2
		if o.large && depth == 0 {
			n = bigCount
		}
		out := map[string]any{}
		for i := 0; i < n; i++ {
			out[fmt.Sprintf("k%03d", i)] = sampleValue(&entry.Fields[1], o, depth+1)
		}
		return out
	case f.IsArray:
		n := o.count(f, depth)
		out := make([]any, n)
		for i := range out {
			if f.Table != nil {
				out[i] = sampleTable(f.Table(), o, depth+1)
			} else {
				out[i] = scalarSample(f, i)
			}
		}
		return out
	}
	return sampleValue(f, o, depth)
}

func sampleValue(f *wire.TableFieldInfo, o sampleOpts, depth int) any {
	switch {
	case f.Table != nil:
		return sampleTable(f.Table(), o, depth+1)
	case f.Pointer:
		if o.large && depth == 0 {
			return strings.Repeat("t", longText)
		}
		return "text"
	case f.Kind == 12:
		return strings.Repeat("a", min(int(f.ArrayBound), 6))
	}
	return scalarSample(f, 0)
}

func scalarSample(f *wire.TableFieldInfo, i int) any {
	switch f.TypeName {
	case "bool":
		return true
	case "float64", "float32":
		return 1.5
	case "string":
		return "text"
	}
	return testClock + i
}

func defaultSamples(info *wire.TableTypeInfo) []namedSample {
	small, err := json.Marshal(sampleTable(info, sampleOpts{}, 0))
	if err != nil {
		panic(err)
	}
	if !info.Variable {
		return []namedSample{{"small", string(small)}}
	}
	large, err := json.Marshal(sampleTable(info, sampleOpts{large: true}, 0))
	if err != nil {
		panic(err)
	}
	return []namedSample{{"small", string(small)}, {"large", string(large)}}
}

// ---------------------------------------------------------------------------
// Fixed tables: a value, the binary wire, padded standard base64 of it and the
// JSON text of the value.

type fixedOps[T any] struct {
	reset         func(*T)
	save          func(*T, []byte) int64
	load          func(*T, []byte, *wire.TableReport) bool
	fromJSON      func(*T, []byte, *wire.TableReport) bool
	toJSONMeasure func(*T) int64
	toJSON        func(*T, []byte) int64
	info          func() *wire.TableTypeInfo
}

func (o fixedOps[T]) value(t *testing.T, text string) (T, wire.TableReport) {
	var v T
	o.reset(&v)
	var rep wire.TableReport
	require.True(t, o.fromJSON(&v, []byte(text), &rep), "the JSON reads")
	return v, rep
}

func (o fixedOps[T]) text(v *T) string {
	buf := make([]byte, o.toJSONMeasure(v))
	return string(buf[:o.toJSON(v, buf)])
}

func (o fixedOps[T]) codec(name string) *schemaread.Codec[T] {
	return &schemaread.Codec[T]{
		Name:  strings.ToUpper(name),
		Help:  viewrowsHelp,
		Reset: o.reset,
		Forms: []byte{1},
		LoadWire: func(v *T, w []byte) schemaread.Report {
			var rep wire.TableReport
			ok := o.load(v, w, &rep)
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
	}
}

// runFixed is the golden check of one fixed table (S1, S2).
func runFixed[T any](t *testing.T, name string, samples []namedSample, o fixedOps[T]) {
	info := o.info()
	require.False(t, info.Variable, "%s is a fixed table", name)
	full := samples == nil
	if full {
		samples = defaultSamples(info)
	}
	sample := samples[0].json
	v, rep := o.value(t, sample)
	require.Zero(t, rep.Clamped, "%s: the sample is within its bounds", name)
	require.Zero(t, rep.Unknown+rep.KindMismatch, "%s: the sample is the table's own fields", name)

	codec := o.codec(name)
	r := schemaread.NewReader(codec)
	good, err := r.Write(&v, schemaread.FormBinary)
	require.NoError(t, err, "%s writes", name)
	require.NotEmpty(t, good)

	// both forms round-trip and each read is counted once, on its form
	for _, data := range [][]byte{good, []byte(b64(good))} {
		got, res, err := r.Read(data)
		require.NoError(t, err, "%s", name)
		assert.True(t, reflect.DeepEqual(got, v), "%s reads the instance back", name)
		assert.Contains(t, []schemaread.Form{schemaread.FormBinary, schemaread.FormBase64}, res.Form, name)
	}
	text, err := r.Write(&v, schemaread.FormBase64)
	require.NoError(t, err)
	assert.Equal(t, b64(good), string(text), "%s: base64 text is padded standard base64 of the wire", name)
	c := r.Counts()
	assert.EqualValues(t, 1, c.Reads(schemaread.FormBinary), name)
	assert.EqualValues(t, 1, c.Reads(schemaread.FormBase64), name)

	// the JSON text of the value: written, read back, the same value and the
	// same wire
	js := o.text(&v)
	require.NotEmpty(t, js, "%s: the value has a JSON text", name)
	back, brep := o.value(t, js)
	require.Zero(t, brep.Clamped+brep.Unknown+brep.KindMismatch, "%s: its own JSON text reads clean", name)
	assert.True(t, reflect.DeepEqual(back, v), "%s: the JSON text reads the same value", name)
	again, err := r.Write(&back, schemaread.FormBinary)
	require.NoError(t, err)
	assert.Equal(t, good, again, "%s: the value read from JSON writes the same wire", name)
	require.True(t, contains(decode(t, sample), decode(t, js)), "%s: every field of the sample survives: %s", name, js)

	if !full {
		return
	}
	// a fixed table has no JSON form on the reader: S1 keeps its text to
	// base64 of the wire
	_, _, err = r.Read([]byte(sample))
	assert.Equal(t, schemaread.ReasonUnknownForm, refusalOf(t, name, err), "%s: JSON text is not a form of a fixed table", name)

	// each string at its bound reads; past its bound the writer refuses it and
	// the image a writer that ignored the bound would make is refused clamped
	for _, f := range stringFields(info) {
		n := int(f.ArrayBound)
		atBound, rep := o.value(t, withField(t, sample, f.Json, strings.Repeat("~", n)))
		require.Zero(t, rep.Clamped, "%s.%s at its bound", name, f.Name)
		img, err := r.Write(&atBound, schemaread.FormBinary)
		require.NoError(t, err, "%s.%s at its bound writes", name, f.Name)
		_, _, err = r.Read(img)
		require.NoError(t, err, "%s.%s at its bound reads", name, f.Name)

		_, rep = o.value(t, withField(t, sample, f.Json, strings.Repeat("~", n+1)))
		assert.Positive(t, rep.Clamped, "%s.%s: the JSON text past %d is reported clamped", name, f.Name, n)

		over := pastBound(t, img, n)
		for _, data := range [][]byte{over, []byte(b64(over))} {
			got, _, err := r.Read(data)
			assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, name+"."+f.Name, err), "%s.%s past %d is refused clamped", name, f.Name, n)
			var zero T
			assert.True(t, reflect.DeepEqual(got, zero), "%s.%s: a refused read returns nothing", name, f.Name)
		}

		big := atBound
		*(*int32)(unsafe.Add(unsafe.Pointer(&big), f.CountOffset)) = int32(n + 1)
		assert.Negative(t, o.save(&big, make([]byte, 1<<16)), "%s.%s: the writer refuses a length past %d", name, f.Name, n)
	}

	refuseDamage(t, name, r, good)
	assert.Positive(t, c.Refused(schemaread.ReasonClamped)+c.Refused(schemaread.ReasonMalformed)+c.Refused(schemaread.ReasonUnknownForm), "%s: refusals are counted", name)
}

// ---------------------------------------------------------------------------
// Variable tables: a document in a region, the binary wire, padded standard
// base64 of it and the JSON text.

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

// builder adapts a generated builder to varOps.fromJSON.
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
		Help:  viewrowsHelp,
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

// roundTrip carries one instance, as JSON text, through the three forms: read
// as JSON, written as the binary wire, padded base64 and JSON, each read back
// to the one document, each read counted once on its form. It returns the
// binary wire.
func roundTrip[T any](t *testing.T, name, label, sample string, o varOps[T]) []byte {
	t.Helper()
	codec := o.codec(name)
	r := schemaread.NewReader(codec)

	v, res, err := r.Read([]byte(sample))
	require.NoError(t, err, "%s/%s: the sample reads as JSON text", name, label)
	require.Equal(t, schemaread.FormJSON, res.Form)
	canon := string(codec.SaveJSON(&v))
	require.True(t, contains(decode(t, sample), decode(t, canon)), "%s/%s: every field of the sample survives", name, label)

	good, err := r.Write(&v, schemaread.FormBinary)
	require.NoError(t, err, "%s/%s writes", name, label)
	require.NotEmpty(t, good)
	text, err := r.Write(&v, schemaread.FormBase64)
	require.NoError(t, err)
	assert.Equal(t, b64(good), string(text), "%s/%s: base64 text is padded standard base64 of the wire", name, label)
	js, err := r.Write(&v, schemaread.FormJSON)
	require.NoError(t, err)

	for _, data := range [][]byte{good, text, js} {
		got, res, err := r.Read(data)
		require.NoError(t, err, "%s/%s", name, label)
		assert.Equal(t, canon, string(codec.SaveJSON(&got)), "%s/%s reads the document back from %s", name, label, res.Form)
		again, err := r.Write(&got, schemaread.FormBinary)
		require.NoError(t, err)
		assert.True(t, bytes.Equal(good, again), "%s/%s: the document read from %s writes the same wire", name, label, res.Form)
	}
	c := r.Counts()
	assert.EqualValues(t, 1, c.Reads(schemaread.FormBinary), "%s/%s", name, label)
	assert.EqualValues(t, 1, c.Reads(schemaread.FormBase64), "%s/%s", name, label)
	assert.EqualValues(t, 2, c.Reads(schemaread.FormJSON), "%s/%s: the sample and the written text", name, label)
	return good
}

// firstElem is the first entry of the sample's list key, or an empty record.
func firstElem(t *testing.T, sample, key string) any {
	m := decode(t, sample).(map[string]any)
	if l, ok := m[key].([]any); ok && len(l) > 0 {
		return l[0]
	}
	return map[string]any{}
}

func repeatOf(elem any, k int) []any {
	out := make([]any, k)
	for i := range out {
		out[i] = elem
	}
	return out
}

// runVariable is the golden check of one variable table (S1, S2). With no
// samples it runs the table's small and large instances and every bound; with
// samples it carries those alone.
func runVariable[T any](t *testing.T, name string, samples []namedSample, o varOps[T]) {
	info := o.info()
	require.True(t, info.Variable, "%s is a variable table", name)
	full := samples == nil
	if full {
		samples = defaultSamples(info)
	}
	var good []byte
	for _, s := range samples {
		if strings.HasPrefix(s.name, "refused") {
			// an instance past a bound of the code is refused clamped, never clamped into a record
			got, _, err := schemaread.NewReader(o.codec(name)).Read([]byte(s.json))
			assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, name+": "+s.name, err), "%s: %s is refused clamped", name, s.name)
			assert.Nil(t, got)
			continue
		}
		w := roundTrip(t, name, s.name, s.json, o)
		if good == nil {
			good = w
		}
	}
	if !full {
		return
	}
	sample := samples[0].json
	codec := o.codec(name)
	r := schemaread.NewReader(codec)

	// each string past its bound is refused clamped in every form
	for _, f := range stringFields(info) {
		n := int(f.ArrayBound)
		atBound := withField(t, sample, f.Json, strings.Repeat("~", n))
		av, _, err := r.Read([]byte(atBound))
		require.NoError(t, err, "%s.%s at its bound", name, f.Name)
		img, err := r.Write(&av, schemaread.FormBinary)
		require.NoError(t, err)
		over := pastBound(t, img, n)
		for _, data := range [][]byte{over, []byte(b64(over)), []byte(withField(t, sample, f.Json, strings.Repeat("~", n+1)))} {
			got, _, err := r.Read(data)
			assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, name+"."+f.Name, err), "%s.%s past %d is refused clamped", name, f.Name, n)
			assert.Nil(t, got, "%s.%s: a refused read returns nothing", name, f.Name)
		}
	}
	// each bounded list: the bound's count reads and round-trips in every
	// form, one more is refused clamped
	for _, f := range arrayFields(info) {
		n := int(f.ArrayBound)
		var elem any = json.Number(fmt.Sprint(testClock))
		if f.Table != nil {
			elem = firstElem(t, sample, f.Json)
		}
		roundTrip(t, name, f.Name+" at its bound", withField(t, sample, f.Json, repeatOf(elem, n)), o)
		got, _, err := r.Read([]byte(withField(t, sample, f.Json, repeatOf(elem, n+1))))
		assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, name+"."+f.Name, err), "%s.%s: %d entries past %d are refused clamped", name, f.Name, n+1, n)
		assert.Nil(t, got)
	}
	// each map: a key at its bound reads and round-trips, one byte more is
	// refused clamped
	for _, f := range mapFields(info) {
		kb := int(f.Table().Fields[0].ArrayBound)
		m := decode(t, sample).(map[string]any)[f.Json].(map[string]any)
		var val any
		for _, v := range m {
			val = v
			break
		}
		roundTrip(t, name, f.Name+" key at its bound", withField(t, sample, f.Json, map[string]any{strings.Repeat("~", kb): val}), o)
		got, _, err := r.Read([]byte(withField(t, sample, f.Json, map[string]any{strings.Repeat("~", kb+1): val})))
		assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, name+"."+f.Name, err), "%s.%s: a key of %d bytes is refused clamped", name, f.Name, kb+1)
		assert.Nil(t, got)
	}

	refuseDamage(t, name, r, good)
	got, _, err := r.Read([]byte(sample[:len(sample)/2]))
	assert.Equal(t, schemaread.ReasonMalformed, refusalOf(t, name+": truncated JSON", err))
	assert.Nil(t, got)
	c := r.Counts()
	assert.Positive(t, c.Refused(schemaread.ReasonClamped)+c.Refused(schemaread.ReasonMalformed), "%s: refusals are counted", name)
}

// tableCase is one table of the area: its name, its form and the harness call that
// runs it. The list is every table of the schema, in name order.
type tableCase struct {
	name  string
	fixed bool
	run   func(t *testing.T, name string, samples []namedSample)
}

func allTables() []tableCase {
	return []tableCase{
		{"ArchivedFleetCard", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.ArchivedFleetCard]{
				builder((*wire.ArchivedFleetCardBuilder).Init, (*wire.ArchivedFleetCardBuilder).GetRoot, func(b *wire.ArchivedFleetCardBuilder) *wire.TableArena { return &b.Arena }, wire.ArchivedFleetCardFromJson),
				wire.ArchivedFleetCardMeasure, wire.ArchivedFleetCardSave, wire.ArchivedFleetCardLoadMeasureReason, wire.ArchivedFleetCardLoad, wire.ArchivedFleetCardToJsonMeasure, wire.ArchivedFleetCardToJson, wire.ArchivedFleetCardTableType, nil})
		}},
		{"BatchManifest", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.BatchManifest]{
				builder((*wire.BatchManifestBuilder).Init, (*wire.BatchManifestBuilder).GetRoot, func(b *wire.BatchManifestBuilder) *wire.TableArena { return &b.Arena }, wire.BatchManifestFromJson),
				wire.BatchManifestMeasure, wire.BatchManifestSave, wire.BatchManifestLoadMeasureReason, wire.BatchManifestLoad, wire.BatchManifestToJsonMeasure, wire.BatchManifestToJson, wire.BatchManifestTableType, nil})
		}},
		{"BatchMemberEntry", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.BatchMemberEntry]{
				builder((*wire.BatchMemberEntryBuilder).Init, (*wire.BatchMemberEntryBuilder).GetRoot, func(b *wire.BatchMemberEntryBuilder) *wire.TableArena { return &b.Arena }, wire.BatchMemberEntryFromJson),
				wire.BatchMemberEntryMeasure, wire.BatchMemberEntrySave, wire.BatchMemberEntryLoadMeasureReason, wire.BatchMemberEntryLoad, wire.BatchMemberEntryToJsonMeasure, wire.BatchMemberEntryToJson, wire.BatchMemberEntryTableType, nil})
		}},
		{"Beat", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.Beat]{
				builder((*wire.BeatBuilder).Init, (*wire.BeatBuilder).GetRoot, func(b *wire.BeatBuilder) *wire.TableArena { return &b.Arena }, wire.BeatFromJson),
				wire.BeatMeasure, wire.BeatSave, wire.BeatLoadMeasureReason, wire.BeatLoad, wire.BeatToJsonMeasure, wire.BeatToJson, wire.BeatTableType, nil})
		}},
		{"CardID", true, func(t *testing.T, name string, samples []namedSample) {
			runFixed(t, name, samples, fixedOps[wire.CardID]{wire.CardIDReset, wire.CardIDSave, wire.CardIDLoad, wire.CardIDFromJson, wire.CardIDToJsonMeasure, wire.CardIDToJson, wire.CardIDTableType})
		}},
		{"CheckRecord", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.CheckRecord]{
				builder((*wire.CheckRecordBuilder).Init, (*wire.CheckRecordBuilder).GetRoot, func(b *wire.CheckRecordBuilder) *wire.TableArena { return &b.Arena }, wire.CheckRecordFromJson),
				wire.CheckRecordMeasure, wire.CheckRecordSave, wire.CheckRecordLoadMeasureReason, wire.CheckRecordLoad, wire.CheckRecordToJsonMeasure, wire.CheckRecordToJson, wire.CheckRecordTableType, nil})
		}},
		{"CheckedCard", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.CheckedCard]{
				builder((*wire.CheckedCardBuilder).Init, (*wire.CheckedCardBuilder).GetRoot, func(b *wire.CheckedCardBuilder) *wire.TableArena { return &b.Arena }, wire.CheckedCardFromJson),
				wire.CheckedCardMeasure, wire.CheckedCardSave, wire.CheckedCardLoadMeasureReason, wire.CheckedCardLoad, wire.CheckedCardToJsonMeasure, wire.CheckedCardToJson, wire.CheckedCardTableType, nil})
		}},
		{"CoordinatorDelivery", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.CoordinatorDelivery]{
				builder((*wire.CoordinatorDeliveryBuilder).Init, (*wire.CoordinatorDeliveryBuilder).GetRoot, func(b *wire.CoordinatorDeliveryBuilder) *wire.TableArena { return &b.Arena }, wire.CoordinatorDeliveryFromJson),
				wire.CoordinatorDeliveryMeasure, wire.CoordinatorDeliverySave, wire.CoordinatorDeliveryLoadMeasureReason, wire.CoordinatorDeliveryLoad, wire.CoordinatorDeliveryToJsonMeasure, wire.CoordinatorDeliveryToJson, wire.CoordinatorDeliveryTableType, nil})
		}},
		{"CostCheck", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.CostCheck]{
				builder((*wire.CostCheckBuilder).Init, (*wire.CostCheckBuilder).GetRoot, func(b *wire.CostCheckBuilder) *wire.TableArena { return &b.Arena }, wire.CostCheckFromJson),
				wire.CostCheckMeasure, wire.CostCheckSave, wire.CostCheckLoadMeasureReason, wire.CostCheckLoad, wire.CostCheckToJsonMeasure, wire.CostCheckToJson, wire.CostCheckTableType, nil})
		}},
		{"CostReconcileRecord", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.CostReconcileRecord]{
				builder((*wire.CostReconcileRecordBuilder).Init, (*wire.CostReconcileRecordBuilder).GetRoot, func(b *wire.CostReconcileRecordBuilder) *wire.TableArena { return &b.Arena }, wire.CostReconcileRecordFromJson),
				wire.CostReconcileRecordMeasure, wire.CostReconcileRecordSave, wire.CostReconcileRecordLoadMeasureReason, wire.CostReconcileRecordLoad, wire.CostReconcileRecordToJsonMeasure, wire.CostReconcileRecordToJson, wire.CostReconcileRecordTableType, nil})
		}},
		{"CriticalCard", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.CriticalCard]{
				builder((*wire.CriticalCardBuilder).Init, (*wire.CriticalCardBuilder).GetRoot, func(b *wire.CriticalCardBuilder) *wire.TableArena { return &b.Arena }, wire.CriticalCardFromJson),
				wire.CriticalCardMeasure, wire.CriticalCardSave, wire.CriticalCardLoadMeasureReason, wire.CriticalCardLoad, wire.CriticalCardToJsonMeasure, wire.CriticalCardToJson, wire.CriticalCardTableType, nil})
		}},
		{"DayGap", true, func(t *testing.T, name string, samples []namedSample) {
			runFixed(t, name, samples, fixedOps[wire.DayGap]{wire.DayGapReset, wire.DayGapSave, wire.DayGapLoad, wire.DayGapFromJson, wire.DayGapToJsonMeasure, wire.DayGapToJson, wire.DayGapTableType})
		}},
		{"DayGapPair", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.DayGapPair]{
				builder((*wire.DayGapPairBuilder).Init, (*wire.DayGapPairBuilder).GetRoot, func(b *wire.DayGapPairBuilder) *wire.TableArena { return &b.Arena }, wire.DayGapPairFromJson),
				wire.DayGapPairMeasure, wire.DayGapPairSave, wire.DayGapPairLoadMeasureReason, wire.DayGapPairLoad, wire.DayGapPairToJsonMeasure, wire.DayGapPairToJson, wire.DayGapPairTableType, nil})
		}},
		{"DecisionDelivery", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.DecisionDelivery]{
				builder((*wire.DecisionDeliveryBuilder).Init, (*wire.DecisionDeliveryBuilder).GetRoot, func(b *wire.DecisionDeliveryBuilder) *wire.TableArena { return &b.Arena }, wire.DecisionDeliveryFromJson),
				wire.DecisionDeliveryMeasure, wire.DecisionDeliverySave, wire.DecisionDeliveryLoadMeasureReason, wire.DecisionDeliveryLoad, wire.DecisionDeliveryToJsonMeasure, wire.DecisionDeliveryToJson, wire.DecisionDeliveryTableType, nil})
		}},
		{"FieldGuard", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.FieldGuard]{
				builder((*wire.FieldGuardBuilder).Init, (*wire.FieldGuardBuilder).GetRoot, func(b *wire.FieldGuardBuilder) *wire.TableArena { return &b.Arena }, wire.FieldGuardFromJson),
				wire.FieldGuardMeasure, wire.FieldGuardSave, wire.FieldGuardLoadMeasureReason, wire.FieldGuardLoad, wire.FieldGuardToJsonMeasure, wire.FieldGuardToJson, wire.FieldGuardTableType, nil})
		}},
		{"FieldGuardPair", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.FieldGuardPair]{
				builder((*wire.FieldGuardPairBuilder).Init, (*wire.FieldGuardPairBuilder).GetRoot, func(b *wire.FieldGuardPairBuilder) *wire.TableArena { return &b.Arena }, wire.FieldGuardPairFromJson),
				wire.FieldGuardPairMeasure, wire.FieldGuardPairSave, wire.FieldGuardPairLoadMeasureReason, wire.FieldGuardPairLoad, wire.FieldGuardPairToJsonMeasure, wire.FieldGuardPairToJson, wire.FieldGuardPairTableType, nil})
		}},
		{"FleetDropDebt", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.FleetDropDebt]{
				builder((*wire.FleetDropDebtBuilder).Init, (*wire.FleetDropDebtBuilder).GetRoot, func(b *wire.FleetDropDebtBuilder) *wire.TableArena { return &b.Arena }, wire.FleetDropDebtFromJson),
				wire.FleetDropDebtMeasure, wire.FleetDropDebtSave, wire.FleetDropDebtLoadMeasureReason, wire.FleetDropDebtLoad, wire.FleetDropDebtToJsonMeasure, wire.FleetDropDebtToJson, wire.FleetDropDebtTableType, nil})
		}},
		{"FleetMachineRow", true, func(t *testing.T, name string, samples []namedSample) {
			runFixed(t, name, samples, fixedOps[wire.FleetMachineRow]{wire.FleetMachineRowReset, wire.FleetMachineRowSave, wire.FleetMachineRowLoad, wire.FleetMachineRowFromJson, wire.FleetMachineRowToJsonMeasure, wire.FleetMachineRowToJson, wire.FleetMachineRowTableType})
		}},
		{"FriendReport", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.FriendReport]{
				builder((*wire.FriendReportBuilder).Init, (*wire.FriendReportBuilder).GetRoot, func(b *wire.FriendReportBuilder) *wire.TableArena { return &b.Arena }, wire.FriendReportFromJson),
				wire.FriendReportMeasure, wire.FriendReportSave, wire.FriendReportLoadMeasureReason, wire.FriendReportLoad, wire.FriendReportToJsonMeasure, wire.FriendReportToJson, wire.FriendReportTableType, nil})
		}},
		{"Heartbeat", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.Heartbeat]{
				builder((*wire.HeartbeatBuilder).Init, (*wire.HeartbeatBuilder).GetRoot, func(b *wire.HeartbeatBuilder) *wire.TableArena { return &b.Arena }, wire.HeartbeatFromJson),
				wire.HeartbeatMeasure, wire.HeartbeatSave, wire.HeartbeatLoadMeasureReason, wire.HeartbeatLoad, wire.HeartbeatToJsonMeasure, wire.HeartbeatToJson, wire.HeartbeatTableType, nil})
		}},
		{"HeldRecord", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.HeldRecord]{
				builder((*wire.HeldRecordBuilder).Init, (*wire.HeldRecordBuilder).GetRoot, func(b *wire.HeldRecordBuilder) *wire.TableArena { return &b.Arena }, wire.HeldRecordFromJson),
				wire.HeldRecordMeasure, wire.HeldRecordSave, wire.HeldRecordLoadMeasureReason, wire.HeldRecordLoad, wire.HeldRecordToJsonMeasure, wire.HeldRecordToJson, wire.HeldRecordTableType, nil})
		}},
		{"IntList", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.IntList]{
				builder((*wire.IntListBuilder).Init, (*wire.IntListBuilder).GetRoot, func(b *wire.IntListBuilder) *wire.TableArena { return &b.Arena }, wire.IntListFromJson),
				wire.IntListMeasure, wire.IntListSave, wire.IntListLoadMeasureReason, wire.IntListLoad, wire.IntListToJsonMeasure, wire.IntListToJson, wire.IntListTableType, nil})
		}},
		{"IntPair", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.IntPair]{
				builder((*wire.IntPairBuilder).Init, (*wire.IntPairBuilder).GetRoot, func(b *wire.IntPairBuilder) *wire.TableArena { return &b.Arena }, wire.IntPairFromJson),
				wire.IntPairMeasure, wire.IntPairSave, wire.IntPairLoadMeasureReason, wire.IntPairLoad, wire.IntPairToJsonMeasure, wire.IntPairToJson, wire.IntPairTableType, nil})
		}},
		{"KindTime", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.KindTime]{
				builder((*wire.KindTimeBuilder).Init, (*wire.KindTimeBuilder).GetRoot, func(b *wire.KindTimeBuilder) *wire.TableArena { return &b.Arena }, wire.KindTimeFromJson),
				wire.KindTimeMeasure, wire.KindTimeSave, wire.KindTimeLoadMeasureReason, wire.KindTimeLoad, wire.KindTimeToJsonMeasure, wire.KindTimeToJson, wire.KindTimeTableType, nil})
		}},
		{"Landing", true, func(t *testing.T, name string, samples []namedSample) {
			runFixed(t, name, samples, fixedOps[wire.Landing]{wire.LandingReset, wire.LandingSave, wire.LandingLoad, wire.LandingFromJson, wire.LandingToJsonMeasure, wire.LandingToJson, wire.LandingTableType})
		}},
		{"LaneQuiet", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.LaneQuiet]{
				builder((*wire.LaneQuietBuilder).Init, (*wire.LaneQuietBuilder).GetRoot, func(b *wire.LaneQuietBuilder) *wire.TableArena { return &b.Arena }, wire.LaneQuietFromJson),
				wire.LaneQuietMeasure, wire.LaneQuietSave, wire.LaneQuietLoadMeasureReason, wire.LaneQuietLoad, wire.LaneQuietToJsonMeasure, wire.LaneQuietToJson, wire.LaneQuietTableType, nil})
		}},
		{"Line", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.Line]{
				builder((*wire.LineBuilder).Init, (*wire.LineBuilder).GetRoot, func(b *wire.LineBuilder) *wire.TableArena { return &b.Arena }, wire.LineFromJson),
				wire.LineMeasure, wire.LineSave, wire.LineLoadMeasureReason, wire.LineLoad, wire.LineToJsonMeasure, wire.LineToJson, wire.LineTableType, nil})
		}},
		{"LineList", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.LineList]{
				builder((*wire.LineListBuilder).Init, (*wire.LineListBuilder).GetRoot, func(b *wire.LineListBuilder) *wire.TableArena { return &b.Arena }, wire.LineListFromJson),
				wire.LineListMeasure, wire.LineListSave, wire.LineListLoadMeasureReason, wire.LineListLoad, wire.LineListToJsonMeasure, wire.LineListToJson, wire.LineListTableType, nil})
		}},
		{"LiveSeenPair", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.LiveSeenPair]{
				builder((*wire.LiveSeenPairBuilder).Init, (*wire.LiveSeenPairBuilder).GetRoot, func(b *wire.LiveSeenPairBuilder) *wire.TableArena { return &b.Arena }, wire.LiveSeenPairFromJson),
				wire.LiveSeenPairMeasure, wire.LiveSeenPairSave, wire.LiveSeenPairLoadMeasureReason, wire.LiveSeenPairLoad, wire.LiveSeenPairToJsonMeasure, wire.LiveSeenPairToJson, wire.LiveSeenPairTableType, nil})
		}},
		{"LoadSample", true, func(t *testing.T, name string, samples []namedSample) {
			runFixed(t, name, samples, fixedOps[wire.LoadSample]{wire.LoadSampleReset, wire.LoadSampleSave, wire.LoadSampleLoad, wire.LoadSampleFromJson, wire.LoadSampleToJsonMeasure, wire.LoadSampleToJson, wire.LoadSampleTableType})
		}},
		{"LogIndex", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.LogIndex]{
				builder((*wire.LogIndexBuilder).Init, (*wire.LogIndexBuilder).GetRoot, func(b *wire.LogIndexBuilder) *wire.TableArena { return &b.Arena }, wire.LogIndexFromJson),
				wire.LogIndexMeasure, wire.LogIndexSave, wire.LogIndexLoadMeasureReason, wire.LogIndexLoad, wire.LogIndexToJsonMeasure, wire.LogIndexToJson, wire.LogIndexTableType, nil})
		}},
		{"LogIndexEntry", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.LogIndexEntry]{
				builder((*wire.LogIndexEntryBuilder).Init, (*wire.LogIndexEntryBuilder).GetRoot, func(b *wire.LogIndexEntryBuilder) *wire.TableArena { return &b.Arena }, wire.LogIndexEntryFromJson),
				wire.LogIndexEntryMeasure, wire.LogIndexEntrySave, wire.LogIndexEntryLoadMeasureReason, wire.LogIndexEntryLoad, wire.LogIndexEntryToJsonMeasure, wire.LogIndexEntryToJson, wire.LogIndexEntryTableType, nil})
		}},
		{"LogLine", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.LogLine]{
				builder((*wire.LogLineBuilder).Init, (*wire.LogLineBuilder).GetRoot, func(b *wire.LogLineBuilder) *wire.TableArena { return &b.Arena }, wire.LogLineFromJson),
				wire.LogLineMeasure, wire.LogLineSave, wire.LogLineLoadMeasureReason, wire.LogLineLoad, wire.LogLineToJsonMeasure, wire.LogLineToJson, wire.LogLineTableType, nil})
		}},
		{"Machine", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.Machine]{
				builder((*wire.MachineBuilder).Init, (*wire.MachineBuilder).GetRoot, func(b *wire.MachineBuilder) *wire.TableArena { return &b.Arena }, wire.MachineFromJson),
				wire.MachineMeasure, wire.MachineSave, wire.MachineLoadMeasureReason, wire.MachineLoad, wire.MachineToJsonMeasure, wire.MachineToJson, wire.MachineTableType, nil})
		}},
		{"MemberCreate", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.MemberCreate]{
				builder((*wire.MemberCreateBuilder).Init, (*wire.MemberCreateBuilder).GetRoot, func(b *wire.MemberCreateBuilder) *wire.TableArena { return &b.Arena }, wire.MemberCreateFromJson),
				wire.MemberCreateMeasure, wire.MemberCreateSave, wire.MemberCreateLoadMeasureReason, wire.MemberCreateLoad, wire.MemberCreateToJsonMeasure, wire.MemberCreateToJson, wire.MemberCreateTableType, nil})
		}},
		{"MemberExpect", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.MemberExpect]{
				builder((*wire.MemberExpectBuilder).Init, (*wire.MemberExpectBuilder).GetRoot, func(b *wire.MemberExpectBuilder) *wire.TableArena { return &b.Arena }, wire.MemberExpectFromJson),
				wire.MemberExpectMeasure, wire.MemberExpectSave, wire.MemberExpectLoadMeasureReason, wire.MemberExpectLoad, wire.MemberExpectToJsonMeasure, wire.MemberExpectToJson, wire.MemberExpectTableType, nil})
		}},
		{"MemberMove", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.MemberMove]{
				builder((*wire.MemberMoveBuilder).Init, (*wire.MemberMoveBuilder).GetRoot, func(b *wire.MemberMoveBuilder) *wire.TableArena { return &b.Arena }, wire.MemberMoveFromJson),
				wire.MemberMoveMeasure, wire.MemberMoveSave, wire.MemberMoveLoadMeasureReason, wire.MemberMoveLoad, wire.MemberMoveToJsonMeasure, wire.MemberMoveToJson, wire.MemberMoveTableType, nil})
		}},
		{"MeterFiles", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.MeterFiles]{
				builder((*wire.MeterFilesBuilder).Init, (*wire.MeterFilesBuilder).GetRoot, func(b *wire.MeterFilesBuilder) *wire.TableArena { return &b.Arena }, wire.MeterFilesFromJson),
				wire.MeterFilesMeasure, wire.MeterFilesSave, wire.MeterFilesLoadMeasureReason, wire.MeterFilesLoad, wire.MeterFilesToJsonMeasure, wire.MeterFilesToJson, wire.MeterFilesTableType, nil})
		}},
		{"MeterHolder", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.MeterHolder]{
				builder((*wire.MeterHolderBuilder).Init, (*wire.MeterHolderBuilder).GetRoot, func(b *wire.MeterHolderBuilder) *wire.TableArena { return &b.Arena }, wire.MeterHolderFromJson),
				wire.MeterHolderMeasure, wire.MeterHolderSave, wire.MeterHolderLoadMeasureReason, wire.MeterHolderLoad, wire.MeterHolderToJsonMeasure, wire.MeterHolderToJson, wire.MeterHolderTableType, nil})
		}},
		{"MeterState", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.MeterState]{
				builder((*wire.MeterStateBuilder).Init, (*wire.MeterStateBuilder).GetRoot, func(b *wire.MeterStateBuilder) *wire.TableArena { return &b.Arena }, wire.MeterStateFromJson),
				wire.MeterStateMeasure, wire.MeterStateSave, wire.MeterStateLoadMeasureReason, wire.MeterStateLoad, wire.MeterStateToJsonMeasure, wire.MeterStateToJson, wire.MeterStateTableType, nil})
		}},
		{"MeterTicks", true, func(t *testing.T, name string, samples []namedSample) {
			runFixed(t, name, samples, fixedOps[wire.MeterTicks]{wire.MeterTicksReset, wire.MeterTicksSave, wire.MeterTicksLoad, wire.MeterTicksFromJson, wire.MeterTicksToJsonMeasure, wire.MeterTicksToJson, wire.MeterTicksTableType})
		}},
		{"Note", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.Note]{
				builder((*wire.NoteBuilder).Init, (*wire.NoteBuilder).GetRoot, func(b *wire.NoteBuilder) *wire.TableArena { return &b.Arena }, wire.NoteFromJson),
				wire.NoteMeasure, wire.NoteSave, wire.NoteLoadMeasureReason, wire.NoteLoad, wire.NoteToJsonMeasure, wire.NoteToJson, wire.NoteTableType, nil})
		}},
		{"PlaceExpect", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.PlaceExpect]{
				builder((*wire.PlaceExpectBuilder).Init, (*wire.PlaceExpectBuilder).GetRoot, func(b *wire.PlaceExpectBuilder) *wire.TableArena { return &b.Arena }, wire.PlaceExpectFromJson),
				wire.PlaceExpectMeasure, wire.PlaceExpectSave, wire.PlaceExpectLoadMeasureReason, wire.PlaceExpectLoad, wire.PlaceExpectToJsonMeasure, wire.PlaceExpectToJson, wire.PlaceExpectTableType, nil})
		}},
		{"PriorityCards", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.PriorityCards]{
				builder((*wire.PriorityCardsBuilder).Init, (*wire.PriorityCardsBuilder).GetRoot, func(b *wire.PriorityCardsBuilder) *wire.TableArena { return &b.Arena }, wire.PriorityCardsFromJson),
				wire.PriorityCardsMeasure, wire.PriorityCardsSave, wire.PriorityCardsLoadMeasureReason, wire.PriorityCardsLoad, wire.PriorityCardsToJsonMeasure, wire.PriorityCardsToJson, wire.PriorityCardsTableType, nil})
		}},
		{"PropWrite", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.PropWrite]{
				builder((*wire.PropWriteBuilder).Init, (*wire.PropWriteBuilder).GetRoot, func(b *wire.PropWriteBuilder) *wire.TableArena { return &b.Arena }, wire.PropWriteFromJson),
				wire.PropWriteMeasure, wire.PropWriteSave, wire.PropWriteLoadMeasureReason, wire.PropWriteLoad, wire.PropWriteToJsonMeasure, wire.PropWriteToJson, wire.PropWriteTableType, nil})
		}},
		{"QueuedChange", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.QueuedChange]{
				builder((*wire.QueuedChangeBuilder).Init, (*wire.QueuedChangeBuilder).GetRoot, func(b *wire.QueuedChangeBuilder) *wire.TableArena { return &b.Arena }, wire.QueuedChangeFromJson),
				wire.QueuedChangeMeasure, wire.QueuedChangeSave, wire.QueuedChangeLoadMeasureReason, wire.QueuedChangeLoad, wire.QueuedChangeToJsonMeasure, wire.QueuedChangeToJson, wire.QueuedChangeTableType, nil})
		}},
		{"ReadCardCounts", true, func(t *testing.T, name string, samples []namedSample) {
			runFixed(t, name, samples, fixedOps[wire.ReadCardCounts]{wire.ReadCardCountsReset, wire.ReadCardCountsSave, wire.ReadCardCountsLoad, wire.ReadCardCountsFromJson, wire.ReadCardCountsToJsonMeasure, wire.ReadCardCountsToJson, wire.ReadCardCountsTableType})
		}},
		{"ReadDay", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.ReadDay]{
				builder((*wire.ReadDayBuilder).Init, (*wire.ReadDayBuilder).GetRoot, func(b *wire.ReadDayBuilder) *wire.TableArena { return &b.Arena }, wire.ReadDayFromJson),
				wire.ReadDayMeasure, wire.ReadDaySave, wire.ReadDayLoadMeasureReason, wire.ReadDayLoad, wire.ReadDayToJsonMeasure, wire.ReadDayToJson, wire.ReadDayTableType, nil})
		}},
		{"ReadDayPair", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.ReadDayPair]{
				builder((*wire.ReadDayPairBuilder).Init, (*wire.ReadDayPairBuilder).GetRoot, func(b *wire.ReadDayPairBuilder) *wire.TableArena { return &b.Arena }, wire.ReadDayPairFromJson),
				wire.ReadDayPairMeasure, wire.ReadDayPairSave, wire.ReadDayPairLoadMeasureReason, wire.ReadDayPairLoad, wire.ReadDayPairToJsonMeasure, wire.ReadDayPairToJson, wire.ReadDayPairTableType, nil})
		}},
		{"ReaderSpend", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.ReaderSpend]{
				builder((*wire.ReaderSpendBuilder).Init, (*wire.ReaderSpendBuilder).GetRoot, func(b *wire.ReaderSpendBuilder) *wire.TableArena { return &b.Arena }, wire.ReaderSpendFromJson),
				wire.ReaderSpendMeasure, wire.ReaderSpendSave, wire.ReaderSpendLoadMeasureReason, wire.ReaderSpendLoad, wire.ReaderSpendToJsonMeasure, wire.ReaderSpendToJson, wire.ReaderSpendTableType, nil})
		}},
		{"ReadsWindowView", true, func(t *testing.T, name string, samples []namedSample) {
			runFixed(t, name, samples, fixedOps[wire.ReadsWindowView]{wire.ReadsWindowViewReset, wire.ReadsWindowViewSave, wire.ReadsWindowViewLoad, wire.ReadsWindowViewFromJson, wire.ReadsWindowViewToJsonMeasure, wire.ReadsWindowViewToJson, wire.ReadsWindowViewTableType})
		}},
		{"RefusedRecord", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.RefusedRecord]{
				builder((*wire.RefusedRecordBuilder).Init, (*wire.RefusedRecordBuilder).GetRoot, func(b *wire.RefusedRecordBuilder) *wire.TableArena { return &b.Arena }, wire.RefusedRecordFromJson),
				wire.RefusedRecordMeasure, wire.RefusedRecordSave, wire.RefusedRecordLoadMeasureReason, wire.RefusedRecordLoad, wire.RefusedRecordToJsonMeasure, wire.RefusedRecordToJson, wire.RefusedRecordTableType, nil})
		}},
		{"RefusedVerb", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.RefusedVerb]{
				builder((*wire.RefusedVerbBuilder).Init, (*wire.RefusedVerbBuilder).GetRoot, func(b *wire.RefusedVerbBuilder) *wire.TableArena { return &b.Arena }, wire.RefusedVerbFromJson),
				wire.RefusedVerbMeasure, wire.RefusedVerbSave, wire.RefusedVerbLoadMeasureReason, wire.RefusedVerbLoad, wire.RefusedVerbToJsonMeasure, wire.RefusedVerbToJson, wire.RefusedVerbTableType, nil})
		}},
		{"ResetMark", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.ResetMark]{
				builder((*wire.ResetMarkBuilder).Init, (*wire.ResetMarkBuilder).GetRoot, func(b *wire.ResetMarkBuilder) *wire.TableArena { return &b.Arena }, wire.ResetMarkFromJson),
				wire.ResetMarkMeasure, wire.ResetMarkSave, wire.ResetMarkLoadMeasureReason, wire.ResetMarkLoad, wire.ResetMarkToJsonMeasure, wire.ResetMarkToJson, wire.ResetMarkTableType, nil})
		}},
		{"ResetResult", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.ResetResult]{
				builder((*wire.ResetResultBuilder).Init, (*wire.ResetResultBuilder).GetRoot, func(b *wire.ResetResultBuilder) *wire.TableArena { return &b.Arena }, wire.ResetResultFromJson),
				wire.ResetResultMeasure, wire.ResetResultSave, wire.ResetResultLoadMeasureReason, wire.ResetResultLoad, wire.ResetResultToJsonMeasure, wire.ResetResultToJson, wire.ResetResultTableType, nil})
		}},
		{"ResetRow", true, func(t *testing.T, name string, samples []namedSample) {
			runFixed(t, name, samples, fixedOps[wire.ResetRow]{wire.ResetRowReset, wire.ResetRowSave, wire.ResetRowLoad, wire.ResetRowFromJson, wire.ResetRowToJsonMeasure, wire.ResetRowToJson, wire.ResetRowTableType})
		}},
		{"ResetStream", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.ResetStream]{
				builder((*wire.ResetStreamBuilder).Init, (*wire.ResetStreamBuilder).GetRoot, func(b *wire.ResetStreamBuilder) *wire.TableArena { return &b.Arena }, wire.ResetStreamFromJson),
				wire.ResetStreamMeasure, wire.ResetStreamSave, wire.ResetStreamLoadMeasureReason, wire.ResetStreamLoad, wire.ResetStreamToJsonMeasure, wire.ResetStreamToJson, wire.ResetStreamTableType, nil})
		}},
		{"Route", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.Route]{
				builder((*wire.RouteBuilder).Init, (*wire.RouteBuilder).GetRoot, func(b *wire.RouteBuilder) *wire.TableArena { return &b.Arena }, wire.RouteFromJson),
				wire.RouteMeasure, wire.RouteSave, wire.RouteLoadMeasureReason, wire.RouteLoad, wire.RouteToJsonMeasure, wire.RouteToJson, wire.RouteTableType, nil})
		}},
		{"RoutePrices", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.RoutePrices]{
				builder((*wire.RoutePricesBuilder).Init, (*wire.RoutePricesBuilder).GetRoot, func(b *wire.RoutePricesBuilder) *wire.TableArena { return &b.Arena }, wire.RoutePricesFromJson),
				wire.RoutePricesMeasure, wire.RoutePricesSave, wire.RoutePricesLoadMeasureReason, wire.RoutePricesLoad, wire.RoutePricesToJsonMeasure, wire.RoutePricesToJson, wire.RoutePricesTableType, nil})
		}},
		{"RouteStat", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.RouteStat]{
				builder((*wire.RouteStatBuilder).Init, (*wire.RouteStatBuilder).GetRoot, func(b *wire.RouteStatBuilder) *wire.TableArena { return &b.Arena }, wire.RouteStatFromJson),
				wire.RouteStatMeasure, wire.RouteStatSave, wire.RouteStatLoadMeasureReason, wire.RouteStatLoad, wire.RouteStatToJsonMeasure, wire.RouteStatToJson, wire.RouteStatTableType, nil})
		}},
		{"RowName", true, func(t *testing.T, name string, samples []namedSample) {
			runFixed(t, name, samples, fixedOps[wire.RowName]{wire.RowNameReset, wire.RowNameSave, wire.RowNameLoad, wire.RowNameFromJson, wire.RowNameToJsonMeasure, wire.RowNameToJson, wire.RowNameTableType})
		}},
		{"RttSample", true, func(t *testing.T, name string, samples []namedSample) {
			runFixed(t, name, samples, fixedOps[wire.RttSample]{wire.RttSampleReset, wire.RttSampleSave, wire.RttSampleLoad, wire.RttSampleFromJson, wire.RttSampleToJsonMeasure, wire.RttSampleToJson, wire.RttSampleTableType})
		}},
		{"SeatChange", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.SeatChange]{
				builder((*wire.SeatChangeBuilder).Init, (*wire.SeatChangeBuilder).GetRoot, func(b *wire.SeatChangeBuilder) *wire.TableArena { return &b.Arena }, wire.SeatChangeFromJson),
				wire.SeatChangeMeasure, wire.SeatChangeSave, wire.SeatChangeLoadMeasureReason, wire.SeatChangeLoad, wire.SeatChangeToJsonMeasure, wire.SeatChangeToJson, wire.SeatChangeTableType, nil})
		}},
		{"SeatWaits", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.SeatWaits]{
				builder((*wire.SeatWaitsBuilder).Init, (*wire.SeatWaitsBuilder).GetRoot, func(b *wire.SeatWaitsBuilder) *wire.TableArena { return &b.Arena }, wire.SeatWaitsFromJson),
				wire.SeatWaitsMeasure, wire.SeatWaitsSave, wire.SeatWaitsLoadMeasureReason, wire.SeatWaitsLoad, wire.SeatWaitsToJsonMeasure, wire.SeatWaitsToJson, wire.SeatWaitsTableType, nil})
		}},
		{"SentinelCounts", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.SentinelCounts]{
				builder((*wire.SentinelCountsBuilder).Init, (*wire.SentinelCountsBuilder).GetRoot, func(b *wire.SentinelCountsBuilder) *wire.TableArena { return &b.Arena }, wire.SentinelCountsFromJson),
				wire.SentinelCountsMeasure, wire.SentinelCountsSave, wire.SentinelCountsLoadMeasureReason, wire.SentinelCountsLoad, wire.SentinelCountsToJsonMeasure, wire.SentinelCountsToJson, wire.SentinelCountsTableType, nil})
		}},
		{"Span", true, func(t *testing.T, name string, samples []namedSample) {
			runFixed(t, name, samples, fixedOps[wire.Span]{wire.SpanReset, wire.SpanSave, wire.SpanLoad, wire.SpanFromJson, wire.SpanToJsonMeasure, wire.SpanToJson, wire.SpanTableType})
		}},
		{"StageStat", true, func(t *testing.T, name string, samples []namedSample) {
			runFixed(t, name, samples, fixedOps[wire.StageStat]{wire.StageStatReset, wire.StageStatSave, wire.StageStatLoad, wire.StageStatFromJson, wire.StageStatToJsonMeasure, wire.StageStatToJson, wire.StageStatTableType})
		}},
		{"StageStatPair", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.StageStatPair]{
				builder((*wire.StageStatPairBuilder).Init, (*wire.StageStatPairBuilder).GetRoot, func(b *wire.StageStatPairBuilder) *wire.TableArena { return &b.Arena }, wire.StageStatPairFromJson),
				wire.StageStatPairMeasure, wire.StageStatPairSave, wire.StageStatPairLoadMeasureReason, wire.StageStatPairLoad, wire.StageStatPairToJsonMeasure, wire.StageStatPairToJson, wire.StageStatPairTableType, nil})
		}},
		{"StageStats", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.StageStats]{
				builder((*wire.StageStatsBuilder).Init, (*wire.StageStatsBuilder).GetRoot, func(b *wire.StageStatsBuilder) *wire.TableArena { return &b.Arena }, wire.StageStatsFromJson),
				wire.StageStatsMeasure, wire.StageStatsSave, wire.StageStatsLoadMeasureReason, wire.StageStatsLoad, wire.StageStatsToJsonMeasure, wire.StageStatsToJson, wire.StageStatsTableType, nil})
		}},
		{"StageTimes", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.StageTimes]{
				builder((*wire.StageTimesBuilder).Init, (*wire.StageTimesBuilder).GetRoot, func(b *wire.StageTimesBuilder) *wire.TableArena { return &b.Arena }, wire.StageTimesFromJson),
				wire.StageTimesMeasure, wire.StageTimesSave, wire.StageTimesLoadMeasureReason, wire.StageTimesLoad, wire.StageTimesToJsonMeasure, wire.StageTimesToJson, wire.StageTimesTableType, nil})
		}},
		{"StatsArchive", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.StatsArchive]{
				builder((*wire.StatsArchiveBuilder).Init, (*wire.StatsArchiveBuilder).GetRoot, func(b *wire.StatsArchiveBuilder) *wire.TableArena { return &b.Arena }, wire.StatsArchiveFromJson),
				wire.StatsArchiveMeasure, wire.StatsArchiveSave, wire.StatsArchiveLoadMeasureReason, wire.StatsArchiveLoad, wire.StatsArchiveToJsonMeasure, wire.StatsArchiveToJson, wire.StatsArchiveTableType, nil})
		}},
		{"StatsRecord", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.StatsRecord]{
				builder((*wire.StatsRecordBuilder).Init, (*wire.StatsRecordBuilder).GetRoot, func(b *wire.StatsRecordBuilder) *wire.TableArena { return &b.Arena }, wire.StatsRecordFromJson),
				wire.StatsRecordMeasure, wire.StatsRecordSave, wire.StatsRecordLoadMeasureReason, wire.StatsRecordLoad, wire.StatsRecordToJsonMeasure, wire.StatsRecordToJson, wire.StatsRecordTableType, nil})
		}},
		{"Stop", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.Stop]{
				builder((*wire.StopBuilder).Init, (*wire.StopBuilder).GetRoot, func(b *wire.StopBuilder) *wire.TableArena { return &b.Arena }, wire.StopFromJson),
				wire.StopMeasure, wire.StopSave, wire.StopLoadMeasureReason, wire.StopLoad, wire.StopToJsonMeasure, wire.StopToJson, wire.StopTableType, nil})
		}},
		{"StopLease", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.StopLease]{
				builder((*wire.StopLeaseBuilder).Init, (*wire.StopLeaseBuilder).GetRoot, func(b *wire.StopLeaseBuilder) *wire.TableArena { return &b.Arena }, wire.StopLeaseFromJson),
				wire.StopLeaseMeasure, wire.StopLeaseSave, wire.StopLeaseLoadMeasureReason, wire.StopLeaseLoad, wire.StopLeaseToJsonMeasure, wire.StopLeaseToJson, wire.StopLeaseTableType, nil})
		}},
		{"StopsRecord", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.StopsRecord]{
				builder((*wire.StopsRecordBuilder).Init, (*wire.StopsRecordBuilder).GetRoot, func(b *wire.StopsRecordBuilder) *wire.TableArena { return &b.Arena }, wire.StopsRecordFromJson),
				wire.StopsRecordMeasure, wire.StopsRecordSave, wire.StopsRecordLoadMeasureReason, wire.StopsRecordLoad, wire.StopsRecordToJsonMeasure, wire.StopsRecordToJson, wire.StopsRecordTableType, nil})
		}},
		{"StoreRTTRecord", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.StoreRTTRecord]{
				builder((*wire.StoreRTTRecordBuilder).Init, (*wire.StoreRTTRecordBuilder).GetRoot, func(b *wire.StoreRTTRecordBuilder) *wire.TableArena { return &b.Arena }, wire.StoreRTTRecordFromJson),
				wire.StoreRTTRecordMeasure, wire.StoreRTTRecordSave, wire.StoreRTTRecordLoadMeasureReason, wire.StoreRTTRecordLoad, wire.StoreRTTRecordToJsonMeasure, wire.StoreRTTRecordToJson, wire.StoreRTTRecordTableType, nil})
		}},
		{"StreamBase", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.StreamBase]{
				builder((*wire.StreamBaseBuilder).Init, (*wire.StreamBaseBuilder).GetRoot, func(b *wire.StreamBaseBuilder) *wire.TableArena { return &b.Arena }, wire.StreamBaseFromJson),
				wire.StreamBaseMeasure, wire.StreamBaseSave, wire.StreamBaseLoadMeasureReason, wire.StreamBaseLoad, wire.StreamBaseToJsonMeasure, wire.StreamBaseToJson, wire.StreamBaseTableType, nil})
		}},
		{"StreamTransition", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.StreamTransition]{
				builder((*wire.StreamTransitionBuilder).Init, (*wire.StreamTransitionBuilder).GetRoot, func(b *wire.StreamTransitionBuilder) *wire.TableArena { return &b.Arena }, wire.StreamTransitionFromJson),
				wire.StreamTransitionMeasure, wire.StreamTransitionSave, wire.StreamTransitionLoadMeasureReason, wire.StreamTransitionLoad, wire.StreamTransitionToJsonMeasure, wire.StreamTransitionToJson, wire.StreamTransitionTableType, nil})
		}},
		{"Stuck", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.Stuck]{
				builder((*wire.StuckBuilder).Init, (*wire.StuckBuilder).GetRoot, func(b *wire.StuckBuilder) *wire.TableArena { return &b.Arena }, wire.StuckFromJson),
				wire.StuckMeasure, wire.StuckSave, wire.StuckLoadMeasureReason, wire.StuckLoad, wire.StuckToJsonMeasure, wire.StuckToJson, wire.StuckTableType, nil})
		}},
		{"Suppressed", true, func(t *testing.T, name string, samples []namedSample) {
			runFixed(t, name, samples, fixedOps[wire.Suppressed]{wire.SuppressedReset, wire.SuppressedSave, wire.SuppressedLoad, wire.SuppressedFromJson, wire.SuppressedToJsonMeasure, wire.SuppressedToJson, wire.SuppressedTableType})
		}},
		{"TextPair", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.TextPair]{
				builder((*wire.TextPairBuilder).Init, (*wire.TextPairBuilder).GetRoot, func(b *wire.TextPairBuilder) *wire.TableArena { return &b.Arena }, wire.TextPairFromJson),
				wire.TextPairMeasure, wire.TextPairSave, wire.TextPairLoadMeasureReason, wire.TextPairLoad, wire.TextPairToJsonMeasure, wire.TextPairToJson, wire.TextPairTableType, nil})
		}},
		{"TidyCard", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.TidyCard]{
				builder((*wire.TidyCardBuilder).Init, (*wire.TidyCardBuilder).GetRoot, func(b *wire.TidyCardBuilder) *wire.TableArena { return &b.Arena }, wire.TidyCardFromJson),
				wire.TidyCardMeasure, wire.TidyCardSave, wire.TidyCardLoadMeasureReason, wire.TidyCardLoad, wire.TidyCardToJsonMeasure, wire.TidyCardToJson, wire.TidyCardTableType, nil})
		}},
		{"TidyInFlight", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.TidyInFlight]{
				builder((*wire.TidyInFlightBuilder).Init, (*wire.TidyInFlightBuilder).GetRoot, func(b *wire.TidyInFlightBuilder) *wire.TableArena { return &b.Arena }, wire.TidyInFlightFromJson),
				wire.TidyInFlightMeasure, wire.TidyInFlightSave, wire.TidyInFlightLoadMeasureReason, wire.TidyInFlightLoad, wire.TidyInFlightToJsonMeasure, wire.TidyInFlightToJson, wire.TidyInFlightTableType, nil})
		}},
		{"TidyResult", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.TidyResult]{
				builder((*wire.TidyResultBuilder).Init, (*wire.TidyResultBuilder).GetRoot, func(b *wire.TidyResultBuilder) *wire.TableArena { return &b.Arena }, wire.TidyResultFromJson),
				wire.TidyResultMeasure, wire.TidyResultSave, wire.TidyResultLoadMeasureReason, wire.TidyResultLoad, wire.TidyResultToJsonMeasure, wire.TidyResultToJson, wire.TidyResultTableType, nil})
		}},
		{"TidyRow", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.TidyRow]{
				builder((*wire.TidyRowBuilder).Init, (*wire.TidyRowBuilder).GetRoot, func(b *wire.TidyRowBuilder) *wire.TableArena { return &b.Arena }, wire.TidyRowFromJson),
				wire.TidyRowMeasure, wire.TidyRowSave, wire.TidyRowLoadMeasureReason, wire.TidyRowLoad, wire.TidyRowToJsonMeasure, wire.TidyRowToJson, wire.TidyRowTableType, nil})
		}},
		{"TierCosts", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.TierCosts]{
				builder((*wire.TierCostsBuilder).Init, (*wire.TierCostsBuilder).GetRoot, func(b *wire.TierCostsBuilder) *wire.TableArena { return &b.Arena }, wire.TierCostsFromJson),
				wire.TierCostsMeasure, wire.TierCostsSave, wire.TierCostsLoadMeasureReason, wire.TierCostsLoad, wire.TierCostsToJsonMeasure, wire.TierCostsToJson, wire.TierCostsTableType, nil})
		}},
		{"Timer", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.Timer]{
				builder((*wire.TimerBuilder).Init, (*wire.TimerBuilder).GetRoot, func(b *wire.TimerBuilder) *wire.TableArena { return &b.Arena }, wire.TimerFromJson),
				wire.TimerMeasure, wire.TimerSave, wire.TimerLoadMeasureReason, wire.TimerLoad, wire.TimerToJsonMeasure, wire.TimerToJson, wire.TimerTableType, nil})
		}},
		{"Timers", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.Timers]{
				builder((*wire.TimersBuilder).Init, (*wire.TimersBuilder).GetRoot, func(b *wire.TimersBuilder) *wire.TableArena { return &b.Arena }, wire.TimersFromJson),
				wire.TimersMeasure, wire.TimersSave, wire.TimersLoadMeasureReason, wire.TimersLoad, wire.TimersToJsonMeasure, wire.TimersToJson, wire.TimersTableType, nil})
		}},
		{"WhereRecord", false, func(t *testing.T, name string, samples []namedSample) {
			runVariable(t, name, samples, varOps[wire.WhereRecord]{
				builder((*wire.WhereRecordBuilder).Init, (*wire.WhereRecordBuilder).GetRoot, func(b *wire.WhereRecordBuilder) *wire.TableArena { return &b.Arena }, wire.WhereRecordFromJson),
				wire.WhereRecordMeasure, wire.WhereRecordSave, wire.WhereRecordLoadMeasureReason, wire.WhereRecordLoad, wire.WhereRecordToJsonMeasure, wire.WhereRecordToJson, wire.WhereRecordTableType, nil})
		}},
	}
}

// ---------------------------------------------------------------------------
// The source records: a value of each Go struct the tables replace, every
// field set, carried into its table by the struct's own field names and types.

var (
	timeType     = reflect.TypeOf(time.Time{})
	durationType = reflect.TypeOf(time.Duration(0))
)

// filler sets every field of a Go value to a value that is not the zero
// value: a counter in numbers and words, a time from the test clock, two
// entries in a list, three keys in a map, a pointer to a filled value.
type filler struct{ seq int }

func (fl *filler) next() int { fl.seq++; return fl.seq }

func (fl *filler) fill(v reflect.Value, depth int) {
	if depth > 10 {
		return
	}
	switch v.Type() {
	case timeType:
		v.Set(reflect.ValueOf(time.Unix(int64(testClock+fl.next()), 0).UTC()))
		return
	case durationType:
		v.SetInt(int64(fl.next()) * int64(time.Second))
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(fmt.Sprintf("s%d", fl.next()))
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(fl.next()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(fl.next()))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		fl.fill(p.Elem(), depth+1)
		v.Set(p)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				fl.fill(v.Field(i), depth+1)
			}
		}
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 2, 2)
		for i := 0; i < 2; i++ {
			fl.fill(s.Index(i), depth+1)
		}
		v.Set(s)
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			fl.fill(v.Index(i), depth+1)
		}
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		for i := 0; i < 3; i++ {
			k := reflect.New(v.Type().Key()).Elem()
			k.SetString(fmt.Sprintf("key%d", fl.next()))
			e := reflect.New(v.Type().Elem()).Elem()
			fl.fill(e, depth+1)
			m.SetMapIndex(k, e)
		}
		v.Set(m)
	}
}

// enlarge grows the record's own lists to the length n (to the most the code
// keeps where it keeps a bound), its maps to bigCount keys and its unbounded
// texts to longText bytes. What the record holds in a nested value stays small.
// caps names a list by its Go field and gives its length; skip a text.
func (fl *filler) enlarge(v reflect.Value, caps map[string]int, skip map[string]bool) {
	vt := v.Type()
	for i := 0; i < v.NumField(); i++ {
		sf := vt.Field(i)
		if !sf.IsExported() {
			continue
		}
		f := v.Field(i)
		switch f.Kind() {
		case reflect.String:
			if !skip[sf.Name] {
				f.SetString(strings.Repeat("t", longText))
			}
		case reflect.Slice:
			n := bigCount
			if c, ok := caps[sf.Name]; ok {
				n = c
			}
			s := reflect.MakeSlice(f.Type(), 0, n)
			for j := 0; j < n; j++ {
				e := reflect.New(f.Type().Elem()).Elem()
				if f.Type().Elem().Kind() == reflect.String && !skip[sf.Name] && j < 2 && n <= 10 {
					// a bounded list of lines holds long lines
					e.SetString(strings.Repeat("t", longText))
				} else {
					fl.fill(e, 1)
				}
				s = reflect.Append(s, e)
			}
			f.Set(s)
		case reflect.Map:
			m := reflect.MakeMap(f.Type())
			for j := 0; j < bigCount; j++ {
				k := reflect.New(f.Type().Key()).Elem()
				k.SetString(fmt.Sprintf("big%03d", j))
				e := reflect.New(f.Type().Elem()).Elem()
				fl.fill(e, 1)
				m.SetMapIndex(k, e)
			}
			f.Set(m)
		}
	}
}

// converter carries a Go value into the JSON of its table by the Go struct's
// field names (the json tag, else the Go name) and types. A Go field with no
// field in the table is an error; the table fields it reaches are recorded.
type converter struct {
	visited map[string]bool
	errs    []string
}

func jsonKey(sf reflect.StructField) (string, bool) {
	tag := sf.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		name = sf.Name
	}
	return name, true
}

func fieldByJSON(ti *wire.TableTypeInfo, key string) *wire.TableFieldInfo {
	for i := range ti.Fields {
		if ti.Fields[i].Json == key {
			return &ti.Fields[i]
		}
	}
	return nil
}

func (c *converter) fail(format string, args ...any) {
	c.errs = append(c.errs, fmt.Sprintf(format, args...))
}

func (c *converter) record(path string, v reflect.Value, ti *wire.TableTypeInfo) map[string]any {
	out := map[string]any{}
	vt := v.Type()
	for i := 0; i < vt.NumField(); i++ {
		sf := vt.Field(i)
		if !sf.IsExported() {
			continue
		}
		key, ok := jsonKey(sf)
		if !ok {
			continue
		}
		f := fieldByJSON(ti, key)
		if f == nil {
			c.fail("%s.%s: the Go field has no field in table %s", path, sf.Name, ti.Name)
			continue
		}
		c.visited[ti.Name+"."+f.Json] = true
		out[f.Json] = c.value(path+"."+sf.Name, v.Field(i), f)
	}
	return out
}

func (c *converter) scalar(path string, v reflect.Value) any {
	switch v.Kind() {
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint()
	case reflect.Float32, reflect.Float64:
		return v.Float()
	case reflect.String:
		return v.String()
	}
	c.fail("%s: no table type for the Go type %s", path, v.Type())
	return nil
}

// wrap carries a Go list, map or word into a table of one field that holds it.
func (c *converter) wrap(path string, v reflect.Value, f *wire.TableFieldInfo) any {
	if f.Table == nil || len(f.Table().Fields) != 1 {
		c.fail("%s: the Go type %s does not fit field %s (%s)", path, v.Type(), f.Name, f.DeclaredTypeName)
		return nil
	}
	ti := f.Table()
	wf := &ti.Fields[0]
	c.visited[ti.Name+"."+wf.Json] = true
	return map[string]any{wf.Json: c.value(path, v, wf)}
}

func (c *converter) elem(path string, e reflect.Value, f *wire.TableFieldInfo) any {
	if f.Table == nil {
		return c.scalar(path, e)
	}
	ti := f.Table()
	switch {
	case e.Kind() == reflect.Struct && e.Type() != timeType:
		return c.record(path, e, ti)
	case e.Kind() == reflect.Array && e.Len() == len(ti.Fields):
		out := map[string]any{}
		for i := range ti.Fields {
			c.visited[ti.Name+"."+ti.Fields[i].Json] = true
			out[ti.Fields[i].Json] = c.scalar(path, e.Index(i))
		}
		return out
	}
	return c.wrap(path, e, f)
}

func (c *converter) value(path string, v reflect.Value, f *wire.TableFieldInfo) any {
	switch v.Type() {
	case timeType:
		tm := v.Interface().(time.Time)
		if tm.IsZero() {
			return int64(0)
		}
		return tm.UnixNano()
	case durationType:
		return v.Int()
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		return c.value(path, v.Elem(), f)
	case reflect.Struct:
		if f.Table == nil {
			c.fail("%s: the Go struct %s is not a table in field %s", path, v.Type(), f.Name)
			return nil
		}
		return c.record(path, v, f.Table())
	case reflect.Slice, reflect.Array:
		if !f.IsArray {
			return c.wrap(path, v, f)
		}
		out := make([]any, v.Len())
		for i := range out {
			out[i] = c.elem(path, v.Index(i), f)
		}
		return out
	case reflect.Map:
		keys := make([]string, 0, v.Len())
		for _, k := range v.MapKeys() {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		switch {
		case f.Map:
			entry := f.Table()
			obj := map[string]any{}
			for _, k := range keys {
				obj[k] = c.value(path+"["+k+"]", v.MapIndex(reflect.ValueOf(k)), &entry.Fields[1])
			}
			return obj
		case f.IsArray && f.Table != nil && len(f.Table().Fields) == 2:
			pair := f.Table()
			c.visited[pair.Name+"."+pair.Fields[0].Json] = true
			c.visited[pair.Name+"."+pair.Fields[1].Json] = true
			out := make([]any, 0, len(keys))
			for _, k := range keys {
				out = append(out, map[string]any{
					pair.Fields[0].Json: k,
					pair.Fields[1].Json: c.value(path+"["+k+"]", v.MapIndex(reflect.ValueOf(k)), &pair.Fields[1]),
				})
			}
			return out
		}
		return c.wrap(path, v, f)
	case reflect.String:
		if f.Table != nil && !f.IsArray {
			return c.wrap(path, v, f)
		}
	}
	return c.scalar(path, v)
}

// dropDebtSource is the fleet-drop-debt record as the store keeps it: a JSON
// array of member names (fleet_rows.go putDropDebt), the members of its table.
type dropDebtSource struct {
	Members []string `json:"members"`
}

// logIndexSource is the load index as the stand-in keeps it (load.go
// indexLog): the cursor and the map of names to log stream ids.
type logIndexSource struct {
	Cursor  string              `json:"cursor"`
	Entries map[string][]string `json:"entries"`
}

type sourceCase struct {
	table string
	large func() any
	small func() any
}

func filled[T any](large bool, caps map[string]int, skip map[string]bool, fix func(*T)) any {
	var v T
	fl := &filler{}
	fl.fill(reflect.ValueOf(&v).Elem(), 0)
	if large {
		fl.enlarge(reflect.ValueOf(&v).Elem(), caps, skip)
	}
	if fix != nil {
		fix(&v)
	}
	return v
}

func sourceCases() []sourceCase {
	var out []sourceCase
	add := func(table string, build func(large bool) any) {
		out = append(out, sourceCase{table: table, small: func() any { return build(false) }, large: func() any { return build(true) }})
	}
	add("WhereRecord", func(large bool) any {
		return filled(large, map[string]int{"Landings": sprint.MaxRecentLandings, "Critical": 5, "Blockers": 3}, nil, func(w *store.WhereRecord) {
			// five held streams: the record holds every stream the sprint has
			for i := 0; i < 5; i++ {
				w.HeldStreams[fmt.Sprintf("held-stream-%d", i)] = i + 1
			}
			if !large {
				w.Landings = []int64{testClock, testClock + 1, testClock + 2}
			}
			if len(w.Critical) > 5 {
				w.Critical = w.Critical[:5]
			}
			if len(w.Blockers) > 3 {
				w.Blockers = w.Blockers[:3]
			}
		})
	})
	add("StoreRTTRecord", func(large bool) any { return filled[store.StoreRTTRecord](large, nil, nil, nil) })
	add("StopsRecord", func(large bool) any { return filled[store.StopsRecord](large, nil, nil, nil) })
	add("RefusedRecord", func(large bool) any { return filled[sprint.RefusedRecord](large, nil, nil, nil) })
	add("Beat", func(large bool) any { return filled[sprint.Beat](large, nil, nil, nil) })
	add("CheckRecord", func(large bool) any {
		return filled(large, map[string]int{"Findings": store.MaxCheckFindings}, map[string]bool{"Digest": true}, func(r *store.CheckRecord) {
			r.Digest = sprint.CardsDigest(nil)
			for i := range r.Cards {
				sum := sha256.Sum256([]byte(r.Cards[i].ID))
				r.Cards[i].Brief = hex.EncodeToString(sum[:])
			}
		})
	})
	add("FleetDropDebt", func(large bool) any { return filled[dropDebtSource](large, nil, nil, nil) })
	add("ResetMark", func(large bool) any { return filled[sprint.ResetMark](large, nil, nil, nil) })
	add("ResetResult", func(large bool) any { return filled[store.ResetResult](large, nil, nil, nil) })
	add("StatsRecord", func(large bool) any { return filled[store.StatsRecord](large, nil, nil, nil) })
	add("StatsArchive", func(large bool) any { return filled[store.StatsArchive](large, nil, nil, nil) })
	add("TidyResult", func(large bool) any { return filled[store.TidyResult](large, nil, nil, nil) })
	add("Stuck", func(large bool) any { return filled[store.Stuck](large, nil, nil, nil) })
	add("LogIndex", func(large bool) any { return filled[logIndexSource](large, nil, nil, nil) })
	add("Machine", func(large bool) any {
		return filled[store.Machine](large, map[string]int{"Spans": store.MaxStopSpans}, nil, nil)
	})
	add("Heartbeat", func(large bool) any { return filled[store.Heartbeat](large, nil, nil, nil) })
	add("HeldRecord", func(large bool) any { return filled[store.OpRecord](large, nil, nil, nil) })
	return out
}

// runSources carries a small and a large value of each source record through
// its table's three forms, and checks that the tables cover the sources and
// the sources cover the tables.
func runSources(t *testing.T) {
	info := map[string]*wire.TableTypeInfo{}
	for _, v := range wire.UnitView().Tables {
		info[v.Name] = v.Type
	}
	run := map[string]tableCase{}
	for _, tc := range allTables() {
		run[tc.name] = tc
	}

	conv := &converter{visited: map[string]bool{}}
	type job struct {
		table, label, js string
	}
	var jobs []job
	for _, sc := range sourceCases() {
		ti, ok := info[sc.table]
		require.True(t, ok, "%s is a table", sc.table)
		for _, w := range []struct {
			label string
			build func() any
		}{{"small", sc.small}, {"large", sc.large}} {
			rv := reflect.ValueOf(w.build())
			obj := conv.record(sc.table, rv, ti)
			jobs = append(jobs, job{sc.table, w.label, encode(t, obj)})
		}
	}
	require.Empty(t, conv.errs, "every field of every source record is a field of its table")

	// every field of every table is reached by a source record
	var unreached []string
	for _, v := range wire.UnitView().Tables {
		for _, f := range v.Type.Fields {
			if !conv.visited[v.Name+"."+f.Json] {
				unreached = append(unreached, v.Name+"."+f.Json)
			}
		}
	}
	sort.Strings(unreached)
	assert.Empty(t, unreached, "every field of every table is a field of a source record")

	for _, j := range jobs {
		t.Run(j.table+"/"+j.label, func(t *testing.T) {
			t.Parallel()
			run[j.table].run(t, j.table, []namedSample{{"source " + j.label, j.js}})
		})
	}

	// past the page the old form had: the large where record holds 200 entries
	// in every list and map and 10,000 landings; one landing more is refused
	for _, j := range jobs {
		if j.table != "WhereRecord" || j.label != "large" {
			continue
		}
		m := decode(t, j.js).(map[string]any)
		assert.Len(t, m["landings"], sprint.MaxRecentLandings, "the large where record holds the most landings the code keeps")
		assert.GreaterOrEqual(t, len(m["held_streams"].(map[string]any)), bigCount, "and 200 held streams")
		t.Run("WhereRecord/past the landings", func(t *testing.T) {
			t.Parallel()
			over := withField(t, j.js, "landings", repeatOf(json.Number(fmt.Sprint(testClock)), sprint.MaxRecentLandings+1))
			run["WhereRecord"].run(t, "WhereRecord", []namedSample{{"refused landings", over}})
		})
	}
}

// ---------------------------------------------------------------------------
// The bounds: each declared bound is the bound of the code that enforces it.

// bound is one declared bound of the schema: the table's field, the number the
// schema declares and the check that the code enforces that number, with the
// file and line of the enforcing code.
type bound struct {
	field string
	n     int
	where string
	check func(t *testing.T)
}

// idWord is the longest word sprint.ValidID takes and one byte more.
func idWord(t *testing.T) {
	t.Helper()
	require.Equal(t, 128, sprint.MaxIDLen)
	require.True(t, sprint.ValidID(strings.Repeat("a", sprint.MaxIDLen)))
	require.False(t, sprint.ValidID(strings.Repeat("a", sprint.MaxIDLen+1)))
}

// longestCardID is the longest id sprint.ValidCardID takes: four words, the
// second beginning "r" and the last a generation suffix "g" and a positive
// number (leading zeros are numbers).
func longestCardID() string {
	w := strings.Repeat("a", sprint.MaxIDLen)
	return strings.Join([]string{w, "r" + w[1:], w, "g" + strings.Repeat("0", sprint.MaxIDLen-2) + "1"}, ".")
}

func cardIDBound(t *testing.T) {
	t.Helper()
	id := longestCardID()
	require.Len(t, id, 515)
	require.True(t, sprint.ValidCardID(id), "a card id of 515 bytes is valid")
	require.False(t, sprint.ValidCardID(id+"a"), "one byte more is not")
	require.False(t, sprint.ValidCardID("a"+id), "a longer first word is not")
}

func rowBound(t *testing.T) {
	t.Helper()
	idWord(t)
	row := sprint.FriendRow(strings.Repeat("a", sprint.MaxIDLen))
	require.Len(t, row, 135)
	name, ok := sprint.FriendOfRow(row)
	require.True(t, ok)
	require.Len(t, name, sprint.MaxIDLen)
}

func bounds() []bound {
	return []bound{
		{"CardID.id", 515, "internal/sprint/schema.go:296-311 (ValidCardID)", cardIDBound},
		{"CriticalCard.id", 515, "internal/sprint/schema.go:296-311 (ValidCardID), weight.go:131-143", cardIDBound},
		{"CheckedCard.id", 515, "internal/sprint/schema.go:296-311 (ValidCardID), cardcheck.go:672-682", cardIDBound},
		{"FleetMachineRow.name", 128, "internal/sprint/fleet_sync.go:90 (ValidID of a member)", idWord},
		{"RowName.name", 135, "internal/sprint/select_batch.go:54 (friend. and a ValidID friend)", rowBound},
		{"Landing.worker", 135, "internal/sprint/landed_series.go:150-170 (a fleet row), select_batch.go:54", rowBound},
		{"Landing.stream", 128, "internal/sprint/steps_work.go:168 (ValidID of a stream)", idWord},
		{"ArchivedFleetCard.row", 135, "internal/sprint/archived_totals.go:38 (a fleet row), select_batch.go:54", rowBound},
		{"CheckedCard.stream", 128, "internal/sprint/steps_work.go:168 (ValidID of a stream)", idWord},
		{"CheckedCard.brief", 64, "internal/sprint/cardcheck.go:678-680 (sha256, hex)", func(t *testing.T) {
			sum := sha256.Sum256([]byte("brief"))
			require.Len(t, hex.EncodeToString(sum[:]), 64)
		}},
		{"CheckRecord.digest", 64, "internal/sprint/cardcheck.go:687-692 (CardsDigest: sha256, hex)", func(t *testing.T) {
			require.Len(t, sprint.CardsDigest(nil), 64)
			require.Len(t, sprint.CardsDigest([]sprint.CheckedCard{{ID: "a", Stream: "s", Brief: "b"}}), 64)
		}},
		{"LogLine.cause", 300, "internal/sprint/store/engine.go:1103-1104 and 1527-1528 (cut to MaxCause), log.go:68", func(t *testing.T) {
			require.Equal(t, 300, sprint.MaxCause)
		}},
		{"Timer.note", 512, "internal/sprint/timers.go:67-75 (ValidTimerNote), timers.go:21", func(t *testing.T) {
			require.Equal(t, 512, sprint.MaxTimerNote)
			require.NoError(t, sprint.ValidTimerNote(strings.Repeat("a", sprint.MaxTimerNote)))
			require.Error(t, sprint.ValidTimerNote(strings.Repeat("a", sprint.MaxTimerNote+1)))
		}},
		{"Note.primaries", 50, "internal/sprint/notes.go:406-412 (Note.Bound cuts to MaxListed)", func(t *testing.T) {
			require.Equal(t, 50, sprint.MaxListed)
			n := sprint.Note{Kind: sprint.Happened, Primaries: make([]string, sprint.MaxListed+1)}.Bound()
			require.Len(t, n.Primaries, sprint.MaxListed)
			n = sprint.Note{Kind: sprint.Happened, Primaries: make([]string, sprint.MaxListed)}.Bound()
			require.Len(t, n.Primaries, sprint.MaxListed)
		}},
		{"CheckRecord.findings", 200, "internal/sprint/store/check_record.go:80-81 (PutCardCheck cuts to MaxCheckFindings)", func(t *testing.T) {
			require.Equal(t, 200, store.MaxCheckFindings)
		}},
		{"Machine.spans", 1000, "internal/sprint/store/machine_transition.go:172-173 and 449-450 (cut to MaxStopSpans), tick.go:65", func(t *testing.T) {
			require.Equal(t, 1000, store.MaxStopSpans)
		}},
		{"WhereRecord.landings", 10000, "internal/sprint/rate.go:91 (RecentLandings keeps the newest MaxRecentLandings), where.go:176", func(t *testing.T) {
			require.Equal(t, 10000, sprint.MaxRecentLandings)
			now := time.Unix(testClock+20000, 0)
			first := time.Unix(testClock, 0)
			landed := make([]time.Time, sprint.MaxRecentLandings+1)
			for i := range landed {
				landed[i] = now.Add(-time.Duration(len(landed)-i) * time.Millisecond)
			}
			require.Len(t, sprint.RecentLandings(landed, nil, first, now), sprint.MaxRecentLandings)
		}},
		{"WhereRecord.critical", 5, "internal/sprint/weight.go:131-143 (Critical keeps n), where.go:162 (n is 5)", nil},
		{"WhereRecord.blockers", 3, "internal/sprint/idle.go:117-129 (IdleBlockers keeps n), where.go:166 (n is 3)", nil},
		{"MeterFiles.top", 10, "nova-tools pkg/hostload/files.go:147-155 (TopHolders keeps n), files.go:26 (FilesTop)", func(t *testing.T) {
			require.Equal(t, 10, hostload.FilesTop)
			hs := make([]hostload.Holder, hostload.FilesTop+5)
			for i := range hs {
				hs[i] = hostload.Holder{PID: i + 1, Open: i}
			}
			require.Len(t, hostload.TopHolders(hs, hostload.FilesTop), hostload.FilesTop)
		}},
		{"Heartbeat.revisions", 4, "internal/sprint/store/tick.go:195 (Revisions is [4]uint64)", func(t *testing.T) {
			var h store.Heartbeat
			require.Len(t, h.Revisions, 4)
		}},
		{"WhereRecord.held_streams.key", 128, "internal/sprint/steps_work.go:168 (ValidID of a stream)", idWord},
		{"WhereRecord.sentinels.key", 128, "internal/sprint/steps_work.go:168 (ValidID of a stream)", idWord},
		{"WhereRecord.streams.key", 128, "internal/sprint/steps_work.go:168 (ValidID of a stream)", idWord},
		{"WhereRecord.dealt_fleet.key", 128, "internal/sprint/store/presence.go:257 (ValidID of a friend)", idWord},
		{"WhereRecord.stream_priorities.key", 128, "internal/sprint/steps_work.go:168 (ValidID of a stream)", idWord},
		{"WhereRecord.row_cards.key", 135, "internal/sprint/select_batch.go:54 (a fleet row)", rowBound},
		{"WhereRecord.fix_states.key", 135, "internal/sprint/select_batch.go:54 (a fleet row)", rowBound},
		{"WhereRecord.row_working.key", 135, "internal/sprint/select_batch.go:54 (a fleet row)", rowBound},
		{"TierCosts.readers.key", 128, "internal/sprint/readers.go:176 (ValidID of a reader)", idWord},
		{"StageTimes.streams.key", 128, "internal/sprint/steps_work.go:168 (ValidID of a stream)", idWord},
		{"ResetMark.rows.key", 135, "internal/sprint/select_batch.go:54 (a fleet row)", rowBound},
		{"ResetMark.streams.key", 128, "internal/sprint/steps_work.go:168 (ValidID of a stream)", idWord},
		{"StatsRecord.streams.key", 128, "internal/sprint/steps_work.go:168 (ValidID of a stream)", idWord},
		{"StatsArchive.streams.key", 128, "internal/sprint/steps_work.go:168 (ValidID of a stream)", idWord},
	}
}

// declared is every bound the generated descriptors declare: a string's
// length, a bounded list's count, a fixed array's length and a map key's
// length, by "Table.field" ("Table.field.key" for a key).
func declared() map[string]int {
	out := map[string]int{}
	for _, v := range wire.UnitView().Tables {
		for _, f := range v.Type.Fields {
			switch {
			case f.Map:
				out[v.Name+"."+f.Name+".key"] = int(f.Table().Fields[0].ArrayBound)
			case f.IsArray && f.Counted && !f.List && f.ArrayBound > 0:
				out[v.Name+"."+f.Name] = int(f.ArrayBound)
			case f.IsArray && !f.Counted && f.ArrayBound > 0:
				out[v.Name+"."+f.Name] = int(f.ArrayBound)
			case f.Kind == 12 && f.Counted && !f.IsArray && !f.Pointer && f.ArrayBound > 0:
				out[v.Name+"."+f.Name] = int(f.ArrayBound)
			}
		}
	}
	return out
}

func TestViewRowsBounds(t *testing.T) {
	t.Parallel()
	want := map[string]int{}
	for _, b := range bounds() {
		want[b.field] = b.n
		t.Run(b.field, func(t *testing.T) {
			t.Parallel()
			assert.NotEmpty(t, b.where, "a bound names the code that enforces it")
			if b.check != nil {
				b.check(t)
			}
		})
	}
	assert.Equal(t, want, declared(), "the schema declares exactly the bounds of this table, and no other: a field the code does not bound has none")
}

// ---------------------------------------------------------------------------
// The lock, the baseline and the pin.

// readLock is each "fixed table <Name> layout=<hash>" of schema.lock and the
// record size its lineage line names.
func readLock(t *testing.T, path string) (layouts map[string]string, records map[string]int) {
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	layouts, records = map[string]string{}, map[string]int{}
	cur := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		w := strings.Fields(sc.Text())
		switch {
		case len(w) >= 4 && w[0] == "fixed" && w[1] == "table":
			cur = w[2]
			layouts[cur] = strings.TrimPrefix(w[3], "layout=")
		case len(w) >= 3 && w[0] == "lineage" && cur != "":
			for _, kv := range w {
				if n, ok := strings.CutPrefix(kv, "record="); ok {
					var size int
					require.NoError(t, json.Unmarshal([]byte(n), &size))
					records[cur] = size
				}
			}
		}
	}
	require.NoError(t, sc.Err())
	return layouts, records
}

func TestViewRowsGolden(t *testing.T) {
	t.Parallel()

	tables := allTables()
	var names, fixedNames []string
	for _, tc := range tables {
		names = append(names, tc.name)
		if tc.fixed {
			fixedNames = append(fixedNames, tc.name)
		}
	}
	var unit []string
	for _, v := range wire.UnitView().Tables {
		unit = append(unit, v.Name)
		fixed := !v.Type.Variable
		_, pinned := fixedLayouts[v.Name]
		assert.Equal(t, pinned, fixed, "%s: fixed exactly when its layout is pinned", v.Name)
	}
	sort.Strings(names)
	sort.Strings(unit)
	require.Equal(t, unit, names, "a case for every table of the unit")
	require.Len(t, fixedNames, len(fixedLayouts), "a case for every fixed table")

	for _, want := range []string{
		"WhereRecord", "StoreRTTRecord", "StopsRecord", "RefusedRecord", "Beat", "CheckRecord",
		"FleetDropDebt", "FleetMachineRow", "ResetMark", "ResetResult", "StatsRecord", "StatsArchive",
		"TidyResult", "Stuck", "LogIndex", "Machine", "Heartbeat", "HeldRecord",
	} {
		require.Contains(t, names, want, "the record %s is a table", want)
	}

	for _, tc := range tables {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.run(t, tc.name, nil)
		})
	}

	// the source records the tables replace round-trip, every field set
	t.Run("sources", func(t *testing.T) {
		t.Parallel()
		runSources(t)
	})

	// the fixed layouts are schema.lock's, each a small record under the
	// compiler's 4096-byte fixed advisory; the baseline covers every table;
	// generate.go pins the repository's schema pin
	t.Run("lock baseline and pin", func(t *testing.T) {
		t.Parallel()
		root := filepath.Join("..", "..", "..", "..", "schemas", "viewrows")
		layouts, records := readLock(t, filepath.Join(root, "schema.lock"))
		assert.Equal(t, fixedLayouts, layouts, "schema.lock holds the fixed layouts this test pins, and only those")
		for name := range fixedLayouts {
			assert.Less(t, records[name], 4096, "%s: a fixed record is a small thing (the 4096-byte advisory)", name)
		}

		base, err := os.ReadFile(filepath.Join(root, "tables.baseline"))
		require.NoError(t, err, "schemas/viewrows/tables.baseline is committed")
		var inBase []string
		for _, line := range strings.Split(string(base), "\n") {
			if n, ok := strings.CutPrefix(line, "table "); ok {
				inBase = append(inBase, strings.TrimSpace(n))
			}
			if n, ok := strings.CutPrefix(line, "fixed table "); ok {
				inBase = append(inBase, strings.TrimSpace(n))
			}
		}
		sort.Strings(inBase)
		var declaredTables []string
		for _, n := range unit {
			declaredTables = append(declaredTables, n)
		}
		// the baseline also lists the entry table of each map, which the unit's
		// table list does not
		for _, n := range declaredTables {
			assert.Contains(t, inBase, n, "the baseline covers %s", n)
		}

		// the bound walk is not vacuous: it finds the declared bounds
		assert.Len(t, stringFields(wire.CardIDTableType()), 1, "CardID's id")
		assert.Len(t, arrayFields(wire.NoteTableType()), 1, "Note's primaries")
		assert.Len(t, arrayFields(wire.WhereRecordTableType()), 3, "WhereRecord's landings, critical and blockers")
		assert.Len(t, mapFields(wire.WhereRecordTableType()), 8, "WhereRecord's eight maps")

		_, err = os.Stat(filepath.Join(root, "viewrows.schema"))
		require.NoError(t, err, "schemas/viewrows/viewrows.schema is committed")
		gen, err := os.ReadFile("generate.go")
		require.NoError(t, err)
		assert.Contains(t, string(gen), "schema/v2/cmd/schema@v2.5.1-0.20261014163537-4d9941fbc7fd generate --lang go --out . ../../../../schemas/viewrows", "generate.go pins the repository's schema pin")
	})
}
