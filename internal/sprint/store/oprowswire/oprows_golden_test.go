package oprowswire_test

// TestOpRowsGolden is the golden test of the oprows definitions
// (schemas/oprows/oprows.schema, schemas/oprows/schema.lock,
// schemas/oprows/tables.baseline and the generated
// internal/sprint/store/oprowswire): every table of the area (the operation
// journal record, the held lock, the notes and note lines, the timers, the
// cells and moved members of a change event, the engine request digest, op
// record and step result, and the teardown member list) round-trips through
// pkg/schemaread in its forms; a value past each declared bound is refused by
// name, never clamped; a damaged image of each form is refused by name; the
// fixed layouts are pinned to schema.lock; and generate.go pins the
// repository's schema pin.
//
// The split it pins (S1): the sixteen small, bounded, server-owned records are
// FIXED tables and travel as the binary wire and padded standard base64 of it;
// the twelve records sized by content (the operation record, which holds its
// lists, result and timers whole, the notes and note lines, the card texts,
// the queued changes, the manifests, the step result, the timers and the
// member and cell lists) are VARIABLE tables and travel as the binary wire,
// padded standard base64 and the schema JSON text form (S2).
//
// The test uses no clock, no socket, no network and no program: the instances
// are written as JSON text through the generated reader, the wire is the
// generated writer's, and the lock and the baseline are files of the
// repository.
//
// Rules cited: S1 (a fixed table for small bounded server-owned records, a
// variable table for a document sized by content), S2 (text transports carry
// padded standard base64 of the binary wire and the JSON text; a reader
// accepts each, refuses damage by name and counts reads per form), S5 (Go
// only).

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unsafe"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	wire "github.com/mas-bandwidth/nova-sprint/internal/sprint/store/oprowswire"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const oprowsHelp = "nova-sprint check"

// fixedLayouts is the layout hash of every fixed table, as schema.lock holds
// it: a change to a fixed table's layout is a change to this list.
var fixedLayouts = map[string]string{
	"Cell":                "0x7d8b038fcc8dac24",
	"CoordinatorDelivery": "0x02be928920d5b8f6",
	"HeldLock":            "0xba2313a3dd6a1ce9",
	"Member":              "0x3c9c8f19a5691779",
	"Name128":             "0x4a9b78f6c6ae4aa9",
	"Name256":             "0xc09811fabb2409bf",
	"Name400":             "0x81015bbc86dd58ec",
	"Pair":                "0x7ea080e0e2995d09",
	"PropWrite":           "0x06ddc7353005a14b",
	"Refusal":             "0x7cc4646515f2ee44",
	"RequestDigest":       "0x054e28944b58a095",
	"SeatChange":          "0x3ec7ee9d0b523821",
	"StreamTransition":    "0x0df044b3793ee1fc",
	"TableCount":          "0x870634f92c444abb",
	"Text512":             "0x48331ce381a97429",
	"Timer":               "0x763d9fb7b98588ca",
}

// variableTables is every variable table of the area.
var variableTables = []string{
	"CardText", "CellSet", "ChangeEvent", "DecisionDelivery", "LogLine", "Manifest",
	"MemberList", "Note", "OpRecord", "QueuedChange", "StepResult", "Timers",
}

// ---------------------------------------------------------------------------
// The samples: one instance of each table, as JSON text.

const (
	timerJSON  = `{"id":"t1","wakes":"friend.alpha","due":1792000600000000000,"note":"check the landing","by":"coordinator","set":1792000000000000000}`
	timer2JSON = `{"id":"t2","wakes":"friend.beta","due":1792000900000000000,"note":"","by":"gamma","set":1792000000000000000}`
	cellJSON   = `{"row":"member-01","column":"status","text":"working"}`
	propJSON   = `{"table_name":"work","name":"promoted","value":"a8","was":"a7","was_absent":false}`
	seatJSON   = `{"holder":"coordinator","generation":4,"from":"gamma","at":1792000000000000000,"by":"owner","taken":false,"approved_by":"owner","reason":"the seat returns"}`
	transJSON  = `{"state":"stopped","cause":"a red gate","waiting":3,"owners":[{"value":"beta"},{"value":"gamma"}]}`
	coordJSON  = `{"covered_by":"","from":"coordinator","to":"friend.gamma","token":"tok-2","subject":"a judgment","body":"see j12","episode":"e1","sent":true,"retry_before":1792000300000000000}`
	decideJSON = `{"lane":"friend.beta","to":"friend.beta","from":"coordinator","set":"j12","body":"decided: land","token":"tok-1","prepared_at":1792000000000000000,"retry_before":1792000300000000000,"sent_at":0,"fault":"","reports":[{"value":"card-a"}],"unknown_receipts":1}`
	noteJSON   = `{"id":"n12","safe_default":"hold","default_coordinator_told":true,"default_owner_told":false,"alias":"j12","kind":"judgment","note_type":"card.stuck","stream":"v15-schema-tables","primaries":[{"value":"card-a"},{"value":"card-b"}],"count":2,"what":"the card is stuck","who":"coordinator","attempt":2,"before":1,"at":1792000000000000000,"decisions":[{"value":"land"},{"value":"rework"}],"marked":false,"answers":"","suspects":[{"value":"card-c"}],"card":"card-a","other":"","tier":"heavy","other_stream":"","stream_level":false,"sprint_level":false,"needs":[{"value":"card-b"}],"review":0,"review_set":0,"to":"coordinator","hint":"read the gate","stream_transition":` + transJSON + `,"decision_delivery":` + decideJSON + `}`
	textJSON   = `{"name":"fix","text":"move the journal to variable tables"}`
	lineJSON   = `{"kind":"move","at":1792000000000000000,"epoch":21,"op":"finish-ab12","card":"card-a","cards":[{"value":"card-a"}],"primary":"card-a","stream":"v15-schema-tables","table_name":"work","from":"m1:working","to":"m1:done","removed":false,"gen":2,"actor":"beta","verb":"finish","cause":"landed","answers":[{"value":"j12"}],"text":[` + textJSON + `,{"name":"report","text":"gate green"}],"set":[{"name":"head","value":"4d9941fbc7fd"}],"note":` + noteJSON + `}`
	queuedJSON = `{"entry":"AQIDBAUG","prop":` + propJSON + `,"verb":"finish","actor":"beta","moved":"a8-defs-store-op-rows"}`
	manifJSON  = `{"table_name":"sprint:work","data":"eyJiYXRjaCI6MX0="}`
	resultJSON = `{"verb":"finish","op":"finish-ab12","moved":[{"value":"card-a"}],"refused":[{"id":"card-b","why":"the card is held"}],"notes":1,"raised":[{"value":"n12 card.stuck"}],"said":[{"value":"NOTE landed"}],"attempts":1,"replay":false,"repaired":[],"pending":"","skipped":[{"value":"a stale entry"}],"args":"0123456789abcdef01234567","lost":false,"tables":[{"name":"work","count":3}],"drained":[{"verb":"drain","moved":[{"value":"card-c"}],"tables":[{"name":"work","count":1}]}]}`
	opJSON     = `{"id":"finish-ab12","is_lock":false,"verb":"finish","at":1792000000000000000,"manifests":[` + manifJSON + `],"notes":[` + noteJSON + `],"decided":[],"log":[` + lineJSON + `],"updates":[],"closes":[{"value":"n11|card-a"}],"streams":[{"value":"v15-schema-tables"}],"caller_op":"cli-77","result":` + resultJSON + `,"stuck":"","queue":[` + queuedJSON + `],"drain":1,"seat":` + seatJSON + `,"close_timers":[{"value":"t0"}],"timers":{"open_timers":[` + timerJSON + `,` + timer2JSON + `]}}`
)

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
	if i := strings.IndexAny(name, ".:"); i >= 0 {
		name = name[:i]
	}
	assert.True(t, strings.HasPrefix(err.Error(), strings.ToUpper(name)+" REFUSED: "), "%s: %s", label, err.Error())
	assert.NotEmpty(t, r.Reason, "%s: a refusal names its reason", label)
	return r.Reason
}

// decode reads JSON text into a generic value, numbers kept as written.
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

// contains reports whether every key and value of want is in got: the
// sample's fields survive the round trip, whatever defaults the writer adds.
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

// arrayFields is each top-level bounded array of records of a table.
func arrayFields(info *wire.TableTypeInfo) []wire.TableFieldInfo {
	var out []wire.TableFieldInfo
	for _, f := range info.Fields {
		if f.IsArray && f.Counted && !f.List && !f.Map && !f.Pointer && f.Kind == 13 && f.ArrayBound > 0 {
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
// Fixed tables: a value, the binary wire and padded standard base64 of it.

type fixedOps[T any] struct {
	reset    func(*T)
	save     func(*T, []byte) int64
	load     func(*T, []byte, *wire.TableReport) bool
	fromJSON func(*T, []byte, *wire.TableReport) bool
	info     func() *wire.TableTypeInfo
}

func (o fixedOps[T]) value(t *testing.T, text string) (T, wire.TableReport) {
	var v T
	o.reset(&v)
	var rep wire.TableReport
	require.True(t, o.fromJSON(&v, []byte(text), &rep), "the JSON reads")
	return v, rep
}

func (o fixedOps[T]) codec(name string) *schemaread.Codec[T] {
	return &schemaread.Codec[T]{
		Name:  strings.ToUpper(name),
		Help:  oprowsHelp,
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
func runFixed[T any](t *testing.T, name, sample string, o fixedOps[T]) {
	info := o.info()
	require.False(t, info.Variable, "%s is a fixed table", name)
	v, rep := o.value(t, sample)
	require.Zero(t, rep.Clamped, "%s: the sample is within its bounds", name)

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

	// a fixed table has no JSON form here: S1 keeps text to base64 of the wire
	_, _, err = r.Read([]byte(sample))
	assert.Equal(t, schemaread.ReasonUnknownForm, refusalOf(t, name, err), "%s: JSON text is not a form of a fixed table", name)

	// each string past its bound: the writer refuses it, and the image a
	// writer that ignored the bound would make is refused clamped
	for _, f := range stringFields(info) {
		n := int(f.ArrayBound)
		atBound, rep := o.value(t, withField(t, sample, f.Json, strings.Repeat("~", n)))
		require.Zero(t, rep.Clamped, "%s.%s at its bound", name, f.Name)
		img, err := r.Write(&atBound, schemaread.FormBinary)
		require.NoError(t, err, "%s.%s at its bound writes", name, f.Name)
		_, _, err = r.Read(img)
		require.NoError(t, err, "%s.%s at its bound reads", name, f.Name)

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
	// each bounded list past its count: the writer refuses it and the text
	// reader reports the clamp, never silently
	for _, f := range arrayFields(info) {
		n := int(f.ArrayBound)
		var rep wire.TableReport
		var w T
		o.reset(&w)
		o.fromJSON(&w, []byte(withField(t, sample, f.Json, repeatElem(t, sample, f.Json, n+1))), &rep)
		assert.Positive(t, rep.Clamped, "%s.%s: %d entries past %d are reported clamped", name, f.Name, n+1, n)

		big := v
		*(*int32)(unsafe.Add(unsafe.Pointer(&big), f.CountOffset)) = int32(n + 1)
		assert.Negative(t, o.save(&big, make([]byte, 1<<16)), "%s.%s: the writer refuses a count past %d", name, f.Name, n)
	}

	refuseDamage(t, name, r, good)
	assert.Positive(t, c.Refused(schemaread.ReasonClamped)+c.Refused(schemaread.ReasonMalformed)+c.Refused(schemaread.ReasonUnknownForm), "%s: refusals are counted", name)
}

// repeatElem is a list of k copies of the first entry of the sample's list
// key, or of an empty record when the sample's list is empty.
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
		Help:  oprowsHelp,
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

// runVariable is the golden check of one variable table (S1, S2).
func runVariable[T any](t *testing.T, name, sample string, o varOps[T]) {
	info := o.info()
	require.True(t, info.Variable, "%s is a variable table", name)
	codec := o.codec(name)
	r := schemaread.NewReader(codec)

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

	// the three forms read back the one document, each counted once
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
	// each bounded list past its count is refused clamped
	for _, f := range arrayFields(info) {
		n := int(f.ArrayBound)
		got, _, err := r.Read([]byte(withField(t, sample, f.Json, repeatElem(t, sample, f.Json, n+1))))
		assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, name+"."+f.Name, err), "%s.%s: %d entries past %d are refused clamped", name, f.Name, n+1, n)
		assert.Nil(t, got)
	}

	refuseDamage(t, name, r, good)
	got, _, err := r.Read([]byte(sample[:len(sample)/2]))
	assert.Equal(t, schemaread.ReasonMalformed, refusalOf(t, name+": truncated JSON", err))
	assert.Nil(t, got)
	assert.Positive(t, c.Refused(schemaread.ReasonClamped)+c.Refused(schemaread.ReasonMalformed), "%s: refusals are counted", name)
}

// cardTextBound is the constraint a blob cannot declare: a card text is at
// most sprint.MaxCardTextBytes (store.MaxCardTextBytes), and the reader holds
// it there.
func cardTextBound(field string, ref *wire.TableRef, c *schemaread.Checker) {
	s := wire.TableStringAt(ref)
	c.Text(field, string(s.Data[:s.Length]), store.MaxCardTextBytes)
}

// manifestBound is the constraint a blob cannot declare: a nova-tables batch
// manifest is at most ntable.LimitManifestBytes.
func manifestBound(field string, ref *wire.TableRef, c *schemaread.Checker) {
	if d := wire.TableBytesAt(ref); d.Length > ntable.LimitManifestBytes {
		c.Bound(field, d.Length, 0, ntable.LimitManifestBytes)
	}
}

// logLineBounds holds each card text a log line carries to
// sprint.MaxCardTextBytes (cardTextBound).
func logLineBounds(field string, l *wire.LogLine, c *schemaread.Checker) {
	for i := int32(0); i < l.TextCount; i++ {
		cardTextBound(field, &l.Text[i].Text, c)
	}
}

// opRecordBounds is the operation record's constraint: the bounds its leaves
// hold on their own hold inside it too. Each manifest is held to
// ntable.LimitManifestBytes (manifestBound) and each card text of each log
// line to sprint.MaxCardTextBytes (logLineBounds). The notes, the step result,
// the queued changes and the timers hold no blob bound by the code.
func opRecordBounds(v **wire.OpRecord, c *schemaread.Checker) {
	r := *v
	for i := int32(0); i < r.Manifests.Count; i++ {
		manifestBound("manifests.data", &r.Manifests.At(i).Data, c)
	}
	for i := int32(0); i < r.Log.Count; i++ {
		logLineBounds("log.text", r.Log.At(i), c)
	}
}

// ---------------------------------------------------------------------------
// The tables.

func fixedCases() map[string]func(t *testing.T) {
	return map[string]func(t *testing.T){
		"Name128": func(t *testing.T) {
			runFixed(t, "Name128", `{"value":"timer-1"}`, fixedOps[wire.Name128]{wire.Name128Reset, wire.Name128Save, wire.Name128Load, wire.Name128FromJson, wire.Name128TableType})
		},
		"Name256": func(t *testing.T) {
			runFixed(t, "Name256", `{"value":"card-a.r1.beta.g2"}`, fixedOps[wire.Name256]{wire.Name256Reset, wire.Name256Save, wire.Name256Load, wire.Name256FromJson, wire.Name256TableType})
		},
		"Name400": func(t *testing.T) {
			runFixed(t, "Name400", `{"value":"j12|card-a.r1.beta.g2"}`, fixedOps[wire.Name400]{wire.Name400Reset, wire.Name400Save, wire.Name400Load, wire.Name400FromJson, wire.Name400TableType})
		},
		"Text512": func(t *testing.T) {
			runFixed(t, "Text512", `{"value":"a line of words"}`, fixedOps[wire.Text512]{wire.Text512Reset, wire.Text512Save, wire.Text512Load, wire.Text512FromJson, wire.Text512TableType})
		},
		"RequestDigest": func(t *testing.T) {
			d := store.ArgsOf(map[string]any{"card": "a", "to": "done"})
			require.Len(t, d, 24, "store.ArgsOf is 24 hex characters")
			runFixed(t, "RequestDigest", `{"digest":"`+d+`"}`, fixedOps[wire.RequestDigest]{wire.RequestDigestReset, wire.RequestDigestSave, wire.RequestDigestLoad, wire.RequestDigestFromJson, wire.RequestDigestTableType})
		},
		"HeldLock": func(t *testing.T) {
			runFixed(t, "HeldLock", `{"id":"finish-ab12-lock","verb":"finish lock","at":1792000000000000000}`, fixedOps[wire.HeldLock]{wire.HeldLockReset, wire.HeldLockSave, wire.HeldLockLoad, wire.HeldLockFromJson, wire.HeldLockTableType})
		},
		"Timer": func(t *testing.T) {
			runFixed(t, "Timer", timerJSON, fixedOps[wire.Timer]{wire.TimerReset, wire.TimerSave, wire.TimerLoad, wire.TimerFromJson, wire.TimerTableType})
		},
		"Member": func(t *testing.T) {
			runFixed(t, "Member", `{"id":"card-a.r1.beta"}`, fixedOps[wire.Member]{wire.MemberReset, wire.MemberSave, wire.MemberLoad, wire.MemberFromJson, wire.MemberTableType})
		},
		"Cell": func(t *testing.T) {
			runFixed(t, "Cell", cellJSON, fixedOps[wire.Cell]{wire.CellReset, wire.CellSave, wire.CellLoad, wire.CellFromJson, wire.CellTableType})
		},
		"Pair": func(t *testing.T) {
			runFixed(t, "Pair", `{"name":"head","value":"4d9941fbc7fd"}`, fixedOps[wire.Pair]{wire.PairReset, wire.PairSave, wire.PairLoad, wire.PairFromJson, wire.PairTableType})
		},
		"Refusal": func(t *testing.T) {
			runFixed(t, "Refusal", `{"id":"a8-defs-store-op-rows","why":"the card is held"}`, fixedOps[wire.Refusal]{wire.RefusalReset, wire.RefusalSave, wire.RefusalLoad, wire.RefusalFromJson, wire.RefusalTableType})
		},
		"TableCount": func(t *testing.T) {
			runFixed(t, "TableCount", `{"name":"work","count":3}`, fixedOps[wire.TableCount]{wire.TableCountReset, wire.TableCountSave, wire.TableCountLoad, wire.TableCountFromJson, wire.TableCountTableType})
		},
		"CoordinatorDelivery": func(t *testing.T) {
			runFixed(t, "CoordinatorDelivery", coordJSON, fixedOps[wire.CoordinatorDelivery]{wire.CoordinatorDeliveryReset, wire.CoordinatorDeliverySave, wire.CoordinatorDeliveryLoad, wire.CoordinatorDeliveryFromJson, wire.CoordinatorDeliveryTableType})
		},
		"SeatChange": func(t *testing.T) {
			runFixed(t, "SeatChange", seatJSON, fixedOps[wire.SeatChange]{wire.SeatChangeReset, wire.SeatChangeSave, wire.SeatChangeLoad, wire.SeatChangeFromJson, wire.SeatChangeTableType})
		},
		"StreamTransition": func(t *testing.T) {
			runFixed(t, "StreamTransition", transJSON, fixedOps[wire.StreamTransition]{wire.StreamTransitionReset, wire.StreamTransitionSave, wire.StreamTransitionLoad, wire.StreamTransitionFromJson, wire.StreamTransitionTableType})
		},
		"PropWrite": func(t *testing.T) {
			runFixed(t, "PropWrite", propJSON, fixedOps[wire.PropWrite]{wire.PropWriteReset, wire.PropWriteSave, wire.PropWriteLoad, wire.PropWriteFromJson, wire.PropWriteTableType})
		},
	}
}

func variableCases() map[string]func(t *testing.T) {
	return map[string]func(t *testing.T){
		"Timers": func(t *testing.T) {
			runVariable(t, "Timers", `{"open_timers":[`+timerJSON+`,`+timer2JSON+`]}`, varOps[wire.Timers]{
				builder((*wire.TimersBuilder).Init, (*wire.TimersBuilder).GetRoot, func(b *wire.TimersBuilder) *wire.TableArena { return &b.Arena }, wire.TimersFromJson),
				wire.TimersMeasure, wire.TimersSave, wire.TimersLoadMeasureReason, wire.TimersLoad, wire.TimersToJsonMeasure, wire.TimersToJson, wire.TimersTableType, nil})
		},
		"MemberList": func(t *testing.T) {
			runVariable(t, "MemberList", `{"members":[{"id":"card-a"},{"id":"card-b.r1.beta"}]}`, varOps[wire.MemberList]{
				builder((*wire.MemberListBuilder).Init, (*wire.MemberListBuilder).GetRoot, func(b *wire.MemberListBuilder) *wire.TableArena { return &b.Arena }, wire.MemberListFromJson),
				wire.MemberListMeasure, wire.MemberListSave, wire.MemberListLoadMeasureReason, wire.MemberListLoad, wire.MemberListToJsonMeasure, wire.MemberListToJson, wire.MemberListTableType, nil})
		},
		"ChangeEvent": func(t *testing.T) {
			runVariable(t, "ChangeEvent", `{"epoch":"21","verb":"apply","rev_before":7,"rev_after":8,"members":[{"id":"card-a"}],"batch_members":[{"id":"card-a"},{"id":"card-b"}]}`, varOps[wire.ChangeEvent]{
				builder((*wire.ChangeEventBuilder).Init, (*wire.ChangeEventBuilder).GetRoot, func(b *wire.ChangeEventBuilder) *wire.TableArena { return &b.Arena }, wire.ChangeEventFromJson),
				wire.ChangeEventMeasure, wire.ChangeEventSave, wire.ChangeEventLoadMeasureReason, wire.ChangeEventLoad, wire.ChangeEventToJsonMeasure, wire.ChangeEventToJson, wire.ChangeEventTableType, nil})
		},
		"CellSet": func(t *testing.T) {
			runVariable(t, "CellSet", `{"cells":[`+cellJSON+`,{"row":"member-02","column":"gen","text":"3"}]}`, varOps[wire.CellSet]{
				builder((*wire.CellSetBuilder).Init, (*wire.CellSetBuilder).GetRoot, func(b *wire.CellSetBuilder) *wire.TableArena { return &b.Arena }, wire.CellSetFromJson),
				wire.CellSetMeasure, wire.CellSetSave, wire.CellSetLoadMeasureReason, wire.CellSetLoad, wire.CellSetToJsonMeasure, wire.CellSetToJson, wire.CellSetTableType, nil})
		},
		"DecisionDelivery": func(t *testing.T) {
			runVariable(t, "DecisionDelivery", decideJSON, varOps[wire.DecisionDelivery]{
				builder((*wire.DecisionDeliveryBuilder).Init, (*wire.DecisionDeliveryBuilder).GetRoot, func(b *wire.DecisionDeliveryBuilder) *wire.TableArena { return &b.Arena }, wire.DecisionDeliveryFromJson),
				wire.DecisionDeliveryMeasure, wire.DecisionDeliverySave, wire.DecisionDeliveryLoadMeasureReason, wire.DecisionDeliveryLoad, wire.DecisionDeliveryToJsonMeasure, wire.DecisionDeliveryToJson, wire.DecisionDeliveryTableType, nil})
		},
		"Note": func(t *testing.T) {
			runVariable(t, "Note", noteJSON, varOps[wire.Note]{
				builder((*wire.NoteBuilder).Init, (*wire.NoteBuilder).GetRoot, func(b *wire.NoteBuilder) *wire.TableArena { return &b.Arena }, wire.NoteFromJson),
				wire.NoteMeasure, wire.NoteSave, wire.NoteLoadMeasureReason, wire.NoteLoad, wire.NoteToJsonMeasure, wire.NoteToJson, wire.NoteTableType, nil})
		},
		"CardText": func(t *testing.T) {
			ops := varOps[wire.CardText]{
				builder((*wire.CardTextBuilder).Init, (*wire.CardTextBuilder).GetRoot, func(b *wire.CardTextBuilder) *wire.TableArena { return &b.Arena }, wire.CardTextFromJson),
				wire.CardTextMeasure, wire.CardTextSave, wire.CardTextLoadMeasureReason, wire.CardTextLoad, wire.CardTextToJsonMeasure, wire.CardTextToJson, wire.CardTextTableType,
				func(v **wire.CardText, c *schemaread.Checker) { cardTextBound("text", &(*v).Text, c) }}
			runVariable(t, "CardText", textJSON, ops)

			// the blob's bound is the reader's constraint: sprint.MaxCardTextBytes
			// is read whole and one byte past it is refused clamped
			r := schemaread.NewReader(ops.codec("CardText"))
			whole := strings.Repeat("w", sprint.MaxCardTextBytes)
			v, _, err := r.Read([]byte(withField(t, textJSON, "text", whole)))
			require.NoError(t, err, "a card text of sprint.MaxCardTextBytes reads")
			s := wire.TableStringAt(&v.Text)
			assert.Equal(t, whole, string(s.Data[:s.Length]), "the card text is held whole, never chunked")
			got, _, err := r.Read([]byte(withField(t, textJSON, "text", whole+"w")))
			assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, "CardText.text", err))
			assert.Nil(t, got)
		},
		"LogLine": func(t *testing.T) {
			runVariable(t, "LogLine", lineJSON, varOps[wire.LogLine]{
				builder((*wire.LogLineBuilder).Init, (*wire.LogLineBuilder).GetRoot, func(b *wire.LogLineBuilder) *wire.TableArena { return &b.Arena }, wire.LogLineFromJson),
				wire.LogLineMeasure, wire.LogLineSave, wire.LogLineLoadMeasureReason, wire.LogLineLoad, wire.LogLineToJsonMeasure, wire.LogLineToJson, wire.LogLineTableType,
				func(v **wire.LogLine, c *schemaread.Checker) { logLineBounds("text", *v, c) }})
		},
		"QueuedChange": func(t *testing.T) {
			runVariable(t, "QueuedChange", queuedJSON, varOps[wire.QueuedChange]{
				builder((*wire.QueuedChangeBuilder).Init, (*wire.QueuedChangeBuilder).GetRoot, func(b *wire.QueuedChangeBuilder) *wire.TableArena { return &b.Arena }, wire.QueuedChangeFromJson),
				wire.QueuedChangeMeasure, wire.QueuedChangeSave, wire.QueuedChangeLoadMeasureReason, wire.QueuedChangeLoad, wire.QueuedChangeToJsonMeasure, wire.QueuedChangeToJson, wire.QueuedChangeTableType, nil})
		},
		"Manifest": func(t *testing.T) {
			ops := varOps[wire.Manifest]{
				builder((*wire.ManifestBuilder).Init, (*wire.ManifestBuilder).GetRoot, func(b *wire.ManifestBuilder) *wire.TableArena { return &b.Arena }, wire.ManifestFromJson),
				wire.ManifestMeasure, wire.ManifestSave, wire.ManifestLoadMeasureReason, wire.ManifestLoad, wire.ManifestToJsonMeasure, wire.ManifestToJson, wire.ManifestTableType,
				func(v **wire.Manifest, c *schemaread.Checker) { manifestBound("data", &(*v).Data, c) }}
			runVariable(t, "Manifest", manifJSON, ops)

			// the blob's bound is the reader's constraint: a manifest of
			// ntable.LimitManifestBytes reads whole, one byte past it is refused
			r := schemaread.NewReader(ops.codec("Manifest"))
			whole := bytes.Repeat([]byte{0x5a}, ntable.LimitManifestBytes)
			v, _, err := r.Read([]byte(withField(t, manifJSON, "data", b64(whole))))
			require.NoError(t, err, "a manifest of ntable.LimitManifestBytes reads")
			d := wire.TableBytesAt(&v.Data)
			assert.Equal(t, whole, d.Data[:d.Length], "the manifest is held whole, never chunked")
			got, _, err := r.Read([]byte(withField(t, manifJSON, "data", b64(append(whole, 0x5a)))))
			assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, "Manifest.data", err))
			assert.Nil(t, got)
		},
		"StepResult": func(t *testing.T) {
			runVariable(t, "StepResult", resultJSON, varOps[wire.StepResult]{
				builder((*wire.StepResultBuilder).Init, (*wire.StepResultBuilder).GetRoot, func(b *wire.StepResultBuilder) *wire.TableArena { return &b.Arena }, wire.StepResultFromJson),
				wire.StepResultMeasure, wire.StepResultSave, wire.StepResultLoadMeasureReason, wire.StepResultLoad, wire.StepResultToJsonMeasure, wire.StepResultToJson, wire.StepResultTableType, nil})
		},
		"OpRecord": func(t *testing.T) {
			ops := varOps[wire.OpRecord]{
				builder((*wire.OpRecordBuilder).Init, (*wire.OpRecordBuilder).GetRoot, func(b *wire.OpRecordBuilder) *wire.TableArena { return &b.Arena }, wire.OpRecordFromJson),
				wire.OpRecordMeasure, wire.OpRecordSave, wire.OpRecordLoadMeasureReason, wire.OpRecordLoad, wire.OpRecordToJsonMeasure, wire.OpRecordToJson, wire.OpRecordTableType,
				opRecordBounds}
			runVariable(t, "OpRecord", opJSON, ops)
			opRecordLeafBounds(t, ops)
		},
	}
}

// opRecordLeafBounds pins the leaf bounds inside the operation record: a
// manifest of ntable.LimitManifestBytes and a card text of
// sprint.MaxCardTextBytes inside a log line are read whole in binary, padded
// standard base64 and JSON text; one byte past either is refused clamped, by
// the table's name and the field's, in each form, and the writer refuses it.
func opRecordLeafBounds(t *testing.T, ops varOps[wire.OpRecord]) {
	codec := ops.codec("OpRecord")
	r := schemaread.NewReader(codec)
	withManifest := func(data []byte) string {
		m := decode(t, opJSON).(map[string]any)
		m["manifests"].([]any)[0].(map[string]any)["data"] = b64(data)
		return encode(t, m)
	}
	withCardText := func(text string) string {
		m := decode(t, opJSON).(map[string]any)
		line := m["log"].([]any)[0].(map[string]any)
		line["text"].([]any)[0].(map[string]any)["text"] = text
		return encode(t, m)
	}
	manifest := bytes.Repeat([]byte{0x5a}, ntable.LimitManifestBytes)
	cardText := strings.Repeat("w", sprint.MaxCardTextBytes)
	cases := []struct {
		name, field, at, over string
		held                  func(*wire.OpRecord) int64
	}{
		{"a manifest at ntable.LimitManifestBytes", "manifests.data", withManifest(manifest), withManifest(append(manifest, 0x5a)),
			func(v *wire.OpRecord) int64 { return wire.TableBytesAt(&v.Manifests.At(0).Data).Length }},
		{"a card text at sprint.MaxCardTextBytes in a log line", "log.text", withCardText(cardText), withCardText(cardText + "w"),
			func(v *wire.OpRecord) int64 { return int64(wire.TableStringAt(&v.Log.At(0).Text[0].Text).Length) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// at the bound: accepted in every form, the leaf held whole
			v, _, err := r.Read([]byte(tc.at))
			require.NoError(t, err, "OpRecord.%s at its bound reads as JSON text", tc.field)
			bin, err := r.Write(&v, schemaread.FormBinary)
			require.NoError(t, err, "OpRecord.%s at its bound writes", tc.field)
			want := tc.held(v)
			for _, data := range [][]byte{bin, []byte(b64(bin)), []byte(tc.at)} {
				got, res, err := r.Read(data)
				require.NoError(t, err, "OpRecord.%s at its bound reads", tc.field)
				require.NotNil(t, got)
				assert.Equal(t, want, tc.held(got), "OpRecord.%s is held whole from %s", tc.field, res.Form)
			}

			// one byte past: the image a writer that ignored the bound makes
			var w *wire.OpRecord
			rep := codec.LoadJSON(&w, []byte(tc.over))
			require.False(t, rep.Malformed, "the past-bound document loads without its constraint")
			require.Zero(t, rep.Clamped)
			require.NotNil(t, w)
			assert.Equal(t, want+1, tc.held(w), "the past-bound leaf is one byte past")
			_, err = r.Write(&w, schemaread.FormBinary)
			assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, "OpRecord."+tc.field, err), "the writer refuses OpRecord.%s one byte past its bound", tc.field)
			over := codec.SaveWire(&w)
			require.NotEmpty(t, over)
			forms := map[string][]byte{"binary": over, "base64": []byte(b64(over)), "JSON": []byte(tc.over)}
			for form, data := range forms {
				got, _, err := r.Read(data)
				assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, "OpRecord."+tc.field, err), "OpRecord.%s one byte past its bound in %s is refused clamped", tc.field, form)
				var ref *schemaread.Refusal
				require.True(t, errors.As(err, &ref))
				assert.Equal(t, tc.field, ref.Field, "the refusal in %s names the field", form)
				assert.Nil(t, got, "OpRecord.%s in %s: a refused read returns nothing", tc.field, form)
			}
		})
	}
}

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

func TestOpRowsGolden(t *testing.T) {
	t.Parallel()

	fixed, variable := fixedCases(), variableCases()
	require.Len(t, fixed, len(fixedLayouts), "a case for every fixed table")
	require.Len(t, variable, len(variableTables), "a case for every variable table")
	for name := range fixedLayouts {
		require.Contains(t, fixed, name)
	}
	for _, name := range variableTables {
		require.Contains(t, variable, name)
	}

	// the unit holds exactly these tables: the header-plus-part tables of the
	// journal (OpNote, OpLogLine, OpQueued, Chunk, TextRef) are gone, because
	// a variable table holds its record whole
	var names []string
	for _, v := range wire.UnitView().Tables {
		names = append(names, v.Name)
	}
	want := append([]string(nil), variableTables...)
	for name := range fixedLayouts {
		want = append(want, name)
	}
	sort.Strings(want)
	sort.Strings(names)
	require.Equal(t, want, names, "the area's tables")

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

	// the fixed layouts are schema.lock's, each a small record under the
	// compiler's 4096-byte fixed advisory; the baseline covers every table;
	// generate.go pins the repository's schema pin
	t.Run("lock baseline and pin", func(t *testing.T) {
		t.Parallel()
		root := filepath.Join("..", "..", "..", "..", "schemas", "oprows")
		layouts, records := readLock(t, filepath.Join(root, "schema.lock"))
		assert.Equal(t, fixedLayouts, layouts, "schema.lock holds the fixed layouts this test pins, and only those")
		for name := range fixedLayouts {
			assert.Less(t, records[name], 4096, "%s: a fixed record is a small thing (the 4096-byte advisory)", name)
		}

		base, err := os.ReadFile(filepath.Join(root, "tables.baseline"))
		require.NoError(t, err, "schemas/oprows/tables.baseline is committed")
		var inBase []string
		for _, line := range strings.Split(string(base), "\n") {
			if n, ok := strings.CutPrefix(line, "table "); ok {
				inBase = append(inBase, strings.TrimSpace(n))
			}
		}
		sort.Strings(inBase)
		assert.Equal(t, want, inBase, "the baseline covers every table of the area")

		// the bound walk is not vacuous: it finds the declared bounds
		assert.Len(t, stringFields(wire.CellTableType()), 3, "Cell's three strings")
		assert.Len(t, arrayFields(wire.StreamTransitionTableType()), 1, "StreamTransition's owners")
		assert.Len(t, stringFields(wire.OpRecordTableType()), 4, "OpRecord's id, verb, caller_op and stuck strings")
		assert.Len(t, arrayFields(wire.OpRecordTableType()), 3, "OpRecord's closes, streams and close_timers; its unbounded lists are sized by content")

		_, err = os.Stat(filepath.Join(root, "oprows.schema"))
		require.NoError(t, err, "schemas/oprows/oprows.schema is committed")
		gen, err := os.ReadFile("generate.go")
		require.NoError(t, err)
		assert.Contains(t, string(gen), "schema/v2/cmd/schema@v2.5.1-0.20261014163537-4d9941fbc7fd generate --lang go --out . ../../../../schemas/oprows", "generate.go pins the repository's schema pin")
	})
}
