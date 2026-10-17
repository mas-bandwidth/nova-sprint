package oprowswire_test

// TestOpRowsGolden is the golden test of the oprows definitions
// (schemas/oprows/oprows.schema, schemas/oprows/schema.lock, the generated
// internal/sprint/store/oprowswire): every table of the area (the operation
// journal record, the held lock, the notes and note lines, the timers, the
// cells and moved members of a change event, the engine request digest, op
// record and step result, and the teardown member list) round-trips in both of
// its forms, the binary wire and padded standard base64 of that wire, read
// through pkg/schemaread; a value past a declared bound is refused by name,
// never clamped; a damaged image of each form is refused by name; the
// generated Go is the committed lock and baseline; and generate.go pins the
// repository's schema pin.
//
// The test uses no clock, no socket, no network and no program: the instances
// are written as JSON text through the generated reader, the wire is the
// generated writer's, and the lock is a file of the repository.
//
// Rules cited: S1 (a fixed table for server-owned bounded records), S2 (text
// transports carry the padded standard base64 of the binary wire; a reader
// accepts either, refuses damage by name and counts reads per form), S5
// (Go only).

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	wire "github.com/mas-bandwidth/nova-sprint/internal/sprint/store/oprowswire"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const oprowsHelp = "nova-sprint check"

// tab is one table under test, whatever its Go type. The generated functions
// are passed in as closures so one harness covers the record (value) tables
// and the list (builder) tables alike.
type tab struct {
	name   string
	sample string
	reset  func() any
	save   func(v any, buf []byte) int64
	load   func(wire []byte) (any, bool)
	json   func(text []byte) (any, bool)
	over   func(v any)
	small  bool
}

func (x *tab) run(t *testing.T) {
	v, ok := x.json([]byte(x.sample))
	require.True(t, ok, "%s: the sample reads", x.name)

	codec := &schemaread.Codec[any]{
		Name:  strings.ToUpper(x.name),
		Help:  oprowsHelp,
		Forms: []byte{1},
		LoadWire: func(v *any, w []byte) schemaread.Report {
			out, ok := x.load(w)
			if !ok {
				return schemaread.Report{Malformed: true}
			}
			*v = out
			return schemaread.Report{}
		},
		LoadJSON: func(v *any, text []byte) schemaread.Report {
			out, ok := x.json(text)
			if !ok {
				return schemaread.Report{Malformed: true}
			}
			*v = out
			return schemaread.Report{}
		},
		SaveWire: func(v *any) []byte {
			buf := make([]byte, 1<<16)
			n := x.save(*v, buf)
			if n < 0 {
				return nil
			}
			return buf[:n]
		},
	}

	r := schemaread.NewReader[any](codec)
	good := codec.SaveWire(&v)
	require.NotNil(t, good, "%s: the instance writes", x.name)

	// both forms round-trip and are counted once each, on the form read
	for _, data := range [][]byte{good, []byte(b64(good))} {
		got, res, err := r.Read(data)
		require.NoError(t, err, "%s", x.name)
		assert.True(t, reflect.DeepEqual(got, v), "%s reads the instance", x.name)
		assert.True(t, res.Form == schemaread.FormBinary || res.Form == schemaread.FormBase64, "%s", x.name)
	}
	out, err := r.Write(&v, schemaread.FormBase64)
	require.NoError(t, err)
	assert.Equal(t, b64(good), string(out), "%s: base64 text is padded standard base64 of the wire", x.name)
	c := r.Counts()
	assert.EqualValues(t, 1, c.Reads(schemaread.FormBinary), "%s", x.name)
	assert.EqualValues(t, 1, c.Reads(schemaread.FormBase64), "%s", x.name)
	if x.small {
		assert.EqualValues(t, 0, c.Reads(schemaread.FormJSON), "%s", x.name)
	}

	// a value past its first bound is refused by name, never clamped
	big := v
	x.over(big)
	if wB := codec.SaveWire(&big); wB != nil {
		got, _, err := r.Read(wB)
		assert.Error(t, err, "%s: an over-bound image is refused", x.name)
		assert.True(t, got == nil, "%s: a refused read returns nothing", x.name)
	}

	// damaged images of each form are refused by name
	damaged := append([]byte(nil), good...)
	damaged[len(damaged)-1] ^= 0xff
	for _, data := range [][]byte{damaged, []byte(b64(damaged))} {
		got, _, err := r.Read(data)
		assert.Error(t, err, "%s: a damaged image is refused", x.name)
		assert.True(t, got == nil, "%s: a refused read returns nothing", x.name)
	}
	_, _, err = r.Read(good[:len(good)-1])
	assert.Error(t, err, "%s: truncated", x.name)
	_, _, err = r.Read([]byte("!" + b64(good)[1:]))
	assert.Error(t, err, "%s: bad base64", x.name)
	assert.Positive(t, c.Refused("malformed"), "%s: a refusal is counted", x.name)
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func refusalOf(t *testing.T, name string, err error) string {
	t.Helper()
	require.Error(t, err)
	var r *schemaread.Refusal
	require.True(t, errors.As(err, &r), "%s: every refusal is a *schemaread.Refusal: %v", name, err)
	assert.True(t, strings.HasPrefix(err.Error(), strings.ToUpper(name)+" REFUSED: "), err.Error())
	return r.Reason
}

func tables() []*tab {
	var all []*tab
	// Sample instances and the over-bound closure that sets the first bounded
	// field one past its bound, as a broken writer could.
	add := func(x *tab) { all = append(all, x) }

	add(&tab{name: "Name128", sample: `{"value":"timer-1"}`, small: true,
		reset: func() any { return new(wire.Name128) },
		save: func(v any, b []byte) int64 { return wire.Name128Save(v.(*wire.Name128), b) },
		load: func(w []byte) (any, bool) { v := new(wire.Name128); ok := wire.Name128Load(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.Name128); ok := wire.Name128FromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.Name128).ValueLength = int32(len(v.(*wire.Name128).Value)) + 1 }})
	add(&tab{name: "Name256", sample: `{"value":"card-a.r1.emma.g2"}`, small: true,
		reset: func() any { return new(wire.Name256) },
		save: func(v any, b []byte) int64 { return wire.Name256Save(v.(*wire.Name256), b) },
		load: func(w []byte) (any, bool) { v := new(wire.Name256); ok := wire.Name256Load(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.Name256); ok := wire.Name256FromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.Name256).ValueLength = int32(len(v.(*wire.Name256).Value)) + 1 }})
	add(&tab{name: "Name400", sample: `{"value":"j12|card-a.r1.emma.g2"}`, small: true,
		reset: func() any { return new(wire.Name400) },
		save: func(v any, b []byte) int64 { return wire.Name400Save(v.(*wire.Name400), b) },
		load: func(w []byte) (any, bool) { v := new(wire.Name400); ok := wire.Name400Load(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.Name400); ok := wire.Name400FromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.Name400).ValueLength = int32(len(v.(*wire.Name400).Value)) + 1 }})
	add(&tab{name: "Text512", sample: `{"value":"a line of words"}`, small: true,
		reset: func() any { return new(wire.Text512) },
		save: func(v any, b []byte) int64 { return wire.Text512Save(v.(*wire.Text512), b) },
		load: func(w []byte) (any, bool) { v := new(wire.Text512); ok := wire.Text512Load(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.Text512); ok := wire.Text512FromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.Text512).ValueLength = int32(len(v.(*wire.Text512).Value)) + 1 }})
	add(&tab{name: "RequestDigest", sample: `{"digest":"` + store.ArgsOf(map[string]any{"card": "a", "to": "done"}) + `"}`, small: true,
		reset: func() any { return new(wire.RequestDigest) },
		save: func(v any, b []byte) int64 { return wire.RequestDigestSave(v.(*wire.RequestDigest), b) },
		load: func(w []byte) (any, bool) { v := new(wire.RequestDigest); ok := wire.RequestDigestLoad(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.RequestDigest); ok := wire.RequestDigestFromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.RequestDigest).DigestLength = int32(len(v.(*wire.RequestDigest).Digest)) + 1 }})
	add(&tab{name: "HeldLock", sample: `{"id":"finish-ab12-lock","verb":"finish lock","at":1792000000000000000}`, small: true,
		reset: func() any { return new(wire.HeldLock) },
		save: func(v any, b []byte) int64 { return wire.HeldLockSave(v.(*wire.HeldLock), b) },
		load: func(w []byte) (any, bool) { v := new(wire.HeldLock); ok := wire.HeldLockLoad(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.HeldLock); ok := wire.HeldLockFromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.HeldLock).IdLength = int32(len(v.(*wire.HeldLock).Id)) + 1 }})
	timer := `{"id":"t1","wakes":"friend.rowan","due":1792000600000000000,"note":"check the landing","by":"rowan","set":1792000000000000000}`
	add(&tab{name: "Timer", sample: timer, small: true,
		reset: func() any { return new(wire.Timer) },
		save: func(v any, b []byte) int64 { return wire.TimerSave(v.(*wire.Timer), b) },
		load: func(w []byte) (any, bool) { v := new(wire.Timer); ok := wire.TimerLoad(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.Timer); ok := wire.TimerFromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.Timer).NoteLength = int32(len(v.(*wire.Timer).Note)) + 1 }})
	add(&tab{name: "Member", sample: `{"id":"card-a.r1.emma"}`, small: true,
		reset: func() any { return new(wire.Member) },
		save: func(v any, b []byte) int64 { return wire.MemberSave(v.(*wire.Member), b) },
		load: func(w []byte) (any, bool) { v := new(wire.Member); ok := wire.MemberLoad(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.Member); ok := wire.MemberFromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.Member).IdLength = int32(len(v.(*wire.Member).Id)) + 1 }})
	add(&tab{name: "Cell", sample: `{"row":"member-01","column":"status","text":"working"}`, small: true,
		reset: func() any { return new(wire.Cell) },
		save: func(v any, b []byte) int64 { return wire.CellSave(v.(*wire.Cell), b) },
		load: func(w []byte) (any, bool) { v := new(wire.Cell); ok := wire.CellLoad(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.Cell); ok := wire.CellFromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.Cell).ColumnLength = int32(len(v.(*wire.Cell).Column)) + 1 }})
	add(&tab{name: "TextRef", sample: `{"name":"report","size":2048}`, small: true,
		reset: func() any { return new(wire.TextRef) },
		save: func(v any, b []byte) int64 { return wire.TextRefSave(v.(*wire.TextRef), b) },
		load: func(w []byte) (any, bool) { v := new(wire.TextRef); ok := wire.TextRefLoad(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.TextRef); ok := wire.TextRefFromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.TextRef).NameLength = int32(len(v.(*wire.TextRef).Name)) + 1 }})
	add(&tab{name: "Pair", sample: `{"name":"head","value":"4d9941fbc7fd"}`, small: true,
		reset: func() any { return new(wire.Pair) },
		save: func(v any, b []byte) int64 { return wire.PairSave(v.(*wire.Pair), b) },
		load: func(w []byte) (any, bool) { v := new(wire.Pair); ok := wire.PairLoad(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.Pair); ok := wire.PairFromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.Pair).NameLength = int32(len(v.(*wire.Pair).Name)) + 1 }})
	add(&tab{name: "Refusal", sample: `{"id":"a8-defs-store-op-rows","why":"the card is held"}`, small: true,
		reset: func() any { return new(wire.Refusal) },
		save: func(v any, b []byte) int64 { return wire.RefusalSave(v.(*wire.Refusal), b) },
		load: func(w []byte) (any, bool) { v := new(wire.Refusal); ok := wire.RefusalLoad(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.Refusal); ok := wire.RefusalFromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.Refusal).WhyLength = int32(len(v.(*wire.Refusal).Why)) + 1 }})
	add(&tab{name: "TableCount", sample: `{"name":"work","count":3}`, small: true,
		reset: func() any { return new(wire.TableCount) },
		save: func(v any, b []byte) int64 { return wire.TableCountSave(v.(*wire.TableCount), b) },
		load: func(w []byte) (any, bool) { v := new(wire.TableCount); ok := wire.TableCountLoad(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.TableCount); ok := wire.TableCountFromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.TableCount).NameLength = int32(len(v.(*wire.TableCount).Name)) + 1 }})
	add(&tab{name: "CoordinatorDelivery", sample: `{"covered_by":"","from":"rowan","to":"friend.stella","token":"tok-2","subject":"a judgment","body":"see j12","episode":"e1","sent":true,"retry_before":1792000300000000000}`,
		reset: func() any { return new(wire.CoordinatorDelivery) },
		save: func(v any, b []byte) int64 { return wire.CoordinatorDeliverySave(v.(*wire.CoordinatorDelivery), b) },
		load: func(w []byte) (any, bool) { v := new(wire.CoordinatorDelivery); ok := wire.CoordinatorDeliveryLoad(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.CoordinatorDelivery); ok := wire.CoordinatorDeliveryFromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.CoordinatorDelivery).SubjectLength = int32(len(v.(*wire.CoordinatorDelivery).Subject)) + 1 }})
	add(&tab{name: "SeatChange", sample: `{"holder":"rowan","generation":4,"from":"stella","at":1792000000000000000,"by":"glenn","taken":false,"approved_by":"glenn","reason":"the coordinator is rowan again"}`,
		reset: func() any { return new(wire.SeatChange) },
		save: func(v any, b []byte) int64 { return wire.SeatChangeSave(v.(*wire.SeatChange), b) },
		load: func(w []byte) (any, bool) { v := new(wire.SeatChange); ok := wire.SeatChangeLoad(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.SeatChange); ok := wire.SeatChangeFromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.SeatChange).ReasonLength = int32(len(v.(*wire.SeatChange).Reason)) + 1 }})
	add(&tab{name: "StreamTransition", sample: `{"state":"stopped","cause":"a red gate","waiting":3,"owners":[{"value":"emma"},{"value":"stella"}]}`,
		reset: func() any { return new(wire.StreamTransition) },
		save: func(v any, b []byte) int64 { return wire.StreamTransitionSave(v.(*wire.StreamTransition), b) },
		load: func(w []byte) (any, bool) { v := new(wire.StreamTransition); ok := wire.StreamTransitionLoad(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.StreamTransition); ok := wire.StreamTransitionFromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.StreamTransition).CauseLength = int32(len(v.(*wire.StreamTransition).Cause)) + 1 }})
	add(&tab{name: "PropWrite", sample: `{"table_name":"work","name":"promoted","value":"a8","was":"a7","was_absent":false}`,
		reset: func() any { return new(wire.PropWrite) },
		save: func(v any, b []byte) int64 { return wire.PropWriteSave(v.(*wire.PropWrite), b) },
		load: func(w []byte) (any, bool) { v := new(wire.PropWrite); ok := wire.PropWriteLoad(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.PropWrite); ok := wire.PropWriteFromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.PropWrite).ValueLength = int32(len(v.(*wire.PropWrite).Value)) + 1 }})
	add(&tab{name: "QueuedChange", sample: `{"entry_bytes":120,"prop":{"table_name":"work","name":"promoted","value":"a8","was":"a7","was_absent":false},"verb":"finish","actor":"emma","moved":"a8-defs-store-op-rows"}`,
		reset: func() any { return new(wire.QueuedChange) },
		save: func(v any, b []byte) int64 { return wire.QueuedChangeSave(v.(*wire.QueuedChange), b) },
		load: func(w []byte) (any, bool) { v := new(wire.QueuedChange); ok := wire.QueuedChangeLoad(v, w, &wire.TableReport{}); return v, ok },
		json: func(t []byte) (any, bool) { v := new(wire.QueuedChange); ok := wire.QueuedChangeFromJson(v, t, &wire.TableReport{}); return v, ok },
		over: func(v any) { v.(*wire.QueuedChange).VerbLength = int32(len(v.(*wire.QueuedChange).Verb)) + 1 }})
	return all
}

func TestOpRowsGolden(t *testing.T) {
	t.Parallel()

	all := tables()
	require.Len(t, all, 18, "the area's record tables")

	for _, e := range all {
		e := e
		t.Run(e.name, func(t *testing.T) {
			t.Parallel()
			e.run(t)
		})
	}

	// the generated Go is committed with the lock and the baseline, and the
	// pin generate.go names is the repository's pin
	t.Run("committed lock baseline and pin", func(t *testing.T) {
		t.Parallel()
		root := filepath.Join("..", "..", "..", "..", "schemas", "oprows")
		_, err := os.Stat(filepath.Join(root, "schema.lock"))
		require.NoError(t, err, "schemas/oprows/schema.lock is committed")
		_, err = os.Stat(filepath.Join(root, "tables.baseline"))
		require.NoError(t, err, "schemas/oprows/tables.baseline is committed")
		_, err = os.Stat(filepath.Join(root, "oprows.schema"))
		require.NoError(t, err, "schemas/oprows/oprows.schema is committed")
		gen, err := os.ReadFile(filepath.Join("generate.go"))
		require.NoError(t, err)
		assert.Contains(t, string(gen), "4d9941fbc7fd", "generate.go pins the repository's schema pin")
	})
}
