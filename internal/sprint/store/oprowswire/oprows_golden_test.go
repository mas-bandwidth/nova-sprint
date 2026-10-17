package oprowswire_test

// TestOpRowsGolden is the golden test of the oprows definitions
// (schemas/oprows/oprows.schema, schemas/oprows/schema.lock, the generated
// internal/sprint/store/oprowswire): every table of the area (the operation
// journal header and its parts, the held lock, the notes and note lines, the
// timers, the cells and moved members of a change event, the request digest,
// the step result and the teardown member list) round-trips in both of its
// forms, the binary wire and padded standard base64 of that wire, read through
// pkg/schemaread; a value past a declared bound is refused by name, on the
// write and on the read, and never clamped; a damaged image of each form is
// refused by name; the declared bounds that come from a constant of the code
// are that constant; and the generated Go is the committed lock.
//
// The test uses no clock, no socket, no network and no program: the instances
// are written as JSON text through the generated reader, the wire is the
// generated writer's, and the lock is a file of the repository.
//
// Rules cited: S1 (a fixed table for server-owned bounded records), S2 (text
// transports carry the padded standard base64 of the binary wire; a reader
// accepts either, refuses damage by name and counts reads per form), S5
// (Go only), and the schema's section 21 (an older reader refuses a newer
// layout as layout_newer).

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	wire "github.com/mas-bandwidth/nova-sprint/internal/sprint/store/oprowswire"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const oprowsHelp = "nova-sprint check"

var b64 = base64.StdEncoding.EncodeToString

// every is one table under test, whatever its Go type.
type every interface {
	run(t *testing.T)
	table() string
	bodyBytes() int
	wireHash() uint64
}

// tab is one fixed table of the area: the generated functions it is read and
// written with, one typical instance as JSON text and a closure that sets the
// first bounded field one past its bound, as a broken writer could (the
// generated writer checks no bound).
type tab[T comparable] struct {
	name     string
	body     int
	hash     uint64
	layout   []byte
	reset    func(*T)
	save     func([]T, []byte) int64
	load     func([]T, []byte, []wire.TableFixedEntry, *wire.TableReport) int64
	measure  func(int64) int64
	planRoom int
	fromJSON func(*T, []byte, *wire.TableReport) bool
	sample   func() string
	post     func(*T)
	over     func(*T)
	small    bool // the byte-flip sweep runs over the whole image
}

func (x *tab[T]) table() string    { return x.name }
func (x *tab[T]) bodyBytes() int   { return x.body }
func (x *tab[T]) wireHash() uint64 { return x.hash }

func (x *tab[T]) saveWire(v *T) []byte {
	n := x.measure(1)
	buf := make([]byte, n)
	if x.save([]T{*v}, buf) != n {
		return nil
	}
	return buf
}

func (x *tab[T]) loadWire(v *T, w []byte) schemaread.Report {
	out := make([]T, 1)
	plan := make([]wire.TableFixedEntry, x.planRoom)
	var rep wire.TableReport
	n := x.load(out, w, plan, &rep)
	r := schemaread.Report{Unknown: int(rep.Unknown), KindMismatch: int(rep.KindMismatch), Clamped: int(rep.Clamped), Malformed: rep.Malformed}
	switch {
	case n < 0 && rep.Reason != "":
		r.Reason = rep.Reason
	case n < 0:
		r.Malformed = true
	case n != 1:
		r.Reason = "record_count"
	default:
		*v = out[0]
	}
	return r
}

// codec is the strict read and write over the generated functions (UNION.md
// section 4.6): the read refuses every clamp; the write validates before it
// writes, because the generated writer checks no bound, by reading back what
// it would write with the reader that refuses.
func (x *tab[T]) codec() *schemaread.Codec[T] {
	return &schemaread.Codec[T]{
		Name:  strings.ToUpper(x.name),
		Help:  oprowsHelp,
		Reset: x.reset,
		Forms: []byte{wire.TableFixedForm},
		LoadWire: func(v *T, w []byte) schemaread.Report {
			return x.loadWire(v, w)
		},
		Constraints: func(v *T, c *schemaread.Checker) {
			var back T
			x.reset(&back)
			w := x.saveWire(v)
			if w == nil {
				c.Text("record", "\xff", 1)
				return
			}
			rep := x.loadWire(&back, w)
			c.Bound("record", int64(rep.Clamped), 0, 0)
			if rep.Reason != "" || rep.Malformed {
				c.Text("record", "\xff", 1)
			}
		},
		SaveWire: x.saveWire,
	}
}

func (x *tab[T]) instance(t *testing.T) T {
	t.Helper()
	var v T
	x.reset(&v)
	var rep wire.TableReport
	require.True(t, x.fromJSON(&v, []byte(x.sample()), &rep), "%s: the sample reads", x.name)
	require.Equal(t, wire.TableReport{}, rep, "%s: the sample reads clean", x.name)
	if x.post != nil {
		x.post(&v)
	}
	return v
}

// refusalOf is the reason a read was refused by, checking the house style.
func refusalOf(t *testing.T, name string, err error) string {
	t.Helper()
	require.Error(t, err)
	var r *schemaread.Refusal
	require.True(t, errors.As(err, &r), "%s: every refusal is a *schemaread.Refusal: %v", name, err)
	assert.True(t, strings.HasPrefix(err.Error(), strings.ToUpper(name)+" REFUSED: "), err.Error())
	assert.True(t, strings.HasSuffix(err.Error(), "; run: "+oprowsHelp), err.Error())
	if r.Field != "" {
		return r.Reason + " (" + r.Field + ")"
	}
	return r.Reason
}

func (x *tab[T]) run(t *testing.T) {
	r := schemaread.NewReader(x.codec())
	v := x.instance(t)
	good := x.saveWire(&v)
	require.NotNil(t, good, "%s: the instance writes", x.name)

	// the size: the form byte, the layout, one record at its declared size
	assert.EqualValues(t, wire.TableFixedHeaderBytes+4+len(x.layout)+8+x.body, len(good), "%s: one record at its bound", x.name)
	assert.LessOrEqual(t, x.body, 65536, "%s: under the fixed form's ceiling", x.name)
	assert.Equal(t, wire.TableFixedForm, good[0], "%s opens with the fixed form byte", x.name)

	// both forms round-trip and are counted once each, on the form read
	for _, in := range []struct {
		form schemaread.Form
		data []byte
	}{
		{schemaread.FormBinary, good},
		{schemaread.FormBase64, []byte(b64(good))},
	} {
		got, res, err := r.Read(in.data)
		require.NoError(t, err, "%s as %s", x.name, in.form)
		assert.Equal(t, in.form, res.Form, x.name)
		assert.True(t, got == v, "%s as %s reads the instance", x.name, in.form)
		for _, form := range []schemaread.Form{schemaread.FormBinary, schemaread.FormBase64} {
			out, err := r.Write(&v, form)
			require.NoError(t, err, "%s written as %s", x.name, form)
			back, res, err := r.Read(out)
			require.NoError(t, err, "%s written then read as %s", x.name, form)
			assert.Equal(t, form, res.Form, x.name)
			assert.True(t, back == v, "%s round-trips as %s", x.name, form)
		}
	}
	out, err := r.Write(&v, schemaread.FormBase64)
	require.NoError(t, err)
	assert.Equal(t, b64(good), string(out), "%s: the base64 text is the padded standard base64 of the whole wire", x.name)
	c := r.Counts()
	assert.EqualValues(t, 3, c.Reads(schemaread.FormBinary), "%s: each read counted once, on its form", x.name)
	assert.EqualValues(t, 3, c.Reads(schemaread.FormBase64), x.name)
	assert.EqualValues(t, 0, c.Reads(schemaread.FormJSON), x.name)
	assert.EqualValues(t, 0, c.Reads(schemaread.FormLegacy), x.name)

	// a value past its first bound: refused on the write, and the image a
	// broken writer would write is refused on the read, in both forms, by name
	// and never clamped into a record
	big := v
	x.over(&big)
	for _, form := range []schemaread.Form{schemaread.FormBinary, schemaread.FormBase64} {
		w, err := r.Write(&big, form)
		assert.Nil(t, w, "%s: an over-bound value writes nothing as %s", x.name, form)
		assert.Contains(t, []string{"clamped (record)", "malformed (record)"}, refusalOf(t, x.name, err), "%s write as %s", x.name, form)
	}
	img := x.saveWire(&big)
	require.NotNil(t, img)
	for _, in := range [][]byte{img, []byte(b64(img))} {
		got, _, err := r.Read(in)
		// a count past its bound is clamped; a text past its bound is
		// text_ill_formed (its length names bytes past the field)
		assert.Contains(t, []string{"clamped", "text_ill_formed"}, refusalOf(t, x.name, err), "%s: an over-bound image is refused by name", x.name)
		var zero T
		assert.True(t, got == zero, "%s: a refused read returns nothing", x.name)
	}

	// damaged images of the binary form, by name
	layoutAt := wire.TableFixedHeaderBytes + 4
	recordAt := layoutAt + len(x.layout)
	mut := func(f func(b []byte) []byte) []byte { return f(append([]byte(nil), good...)) }
	cases := []struct {
		name   string
		in     []byte
		reason string
	}{
		{"form byte of the variable form", mut(func(b []byte) []byte { b[0] = 1; return b }), "unknown_form"},
		{"form byte of the message form", mut(func(b []byte) []byte { b[0] = 2; return b }), "unknown_form"},
		{"an unknown newer form", mut(func(b []byte) []byte { b[0] = 9; return b }), "unknown_form"},
		{"a file hash of a newer layout", mut(func(b []byte) []byte { b[wire.TableFixedHashAt] ^= 0xff; return b }), "layout_newer"},
		{"a layout byte flipped", mut(func(b []byte) []byte { b[layoutAt+8] ^= 0xff; return b }), "layout_malformed"},
		{"a record hash flipped", mut(func(b []byte) []byte { b[recordAt] ^= 0xff; return b }), "no_layout"},
		{"a reserved byte set", mut(func(b []byte) []byte { b[3] = 1; return b }), "malformed"},
		{"truncated by one byte", good[:len(good)-1], "malformed"},
		{"truncated inside the header", good[:10], "malformed"},
		{"truncated inside the layout", good[:layoutAt+len(x.layout)/2], "layout_malformed"},
		{"truncated before the record", good[:recordAt], "record_count"},
		{"a byte of trailing junk", append(append([]byte(nil), good...), 7), "malformed"},
		{"two records", append(append([]byte(nil), good...), good[recordAt:]...), "batch_too_large"},
		{"the form byte alone", good[:1], "malformed"},
		{"empty", nil, "unknown_form"},
	}
	for _, tc := range cases {
		got, _, err := r.Read(tc.in)
		assert.Equal(t, tc.reason, refusalOf(t, x.name, err), "%s: %s", x.name, tc.name)
		var zero T
		assert.True(t, got == zero, "%s: %s: a refused read returns nothing", x.name, tc.name)
	}

	// damaged images of the base64 form, by name
	text := b64(good)
	for _, tc := range []struct{ name, in string }{
		{"base64 that does not decode", text[:len(text)-3]},
		{"base64 with trailing junk", text + " junk"},
		{"base64 with a bad character", "!" + text[1:]},
		{"base64 of a truncated wire", b64(good[:len(good)-1])},
		{"base64 of a wire with a flipped hash", b64(cases[3].in)},
	} {
		_, _, err := r.Read([]byte(tc.in))
		require.Error(t, err, "%s: %s", x.name, tc.name)
		var ref *schemaread.Refusal
		require.True(t, errors.As(err, &ref), "%s: %s: %v", x.name, tc.name, err)
		assert.Contains(t, []string{"malformed", "layout_newer", "unknown_form"}, ref.Reason, "%s: %s", x.name, tc.name)
	}
	assert.Positive(t, r.Counts().Refused("malformed"), "%s: a refusal is counted under its reason", x.name)

	// every cut of the image is refused, never read as a shorter record
	step := max(1, len(good)/97)
	for i := 0; i < len(good); i += step {
		_, _, err := r.Read(good[:i])
		assert.Error(t, err, "%s: cut at byte %d", x.name, i)
	}

	// the small tables: every byte of each form flipped three ways is refused
	// by name or reads a record that passes every bound
	if x.small {
		mutants := 0
		for _, in := range [][]byte{good, []byte(b64(good))} {
			for i := range in {
				for _, b := range []byte{0x00, in[i] ^ 0x01, in[i] ^ 0x80} {
					m := append([]byte(nil), in...)
					m[i] = b
					mutants++
					got, _, err := r.Read(m)
					if err != nil {
						var ref *schemaread.Refusal
						require.True(t, errors.As(err, &ref), "%s mutant %d: %v", x.name, mutants, err)
						continue
					}
					require.NoError(t, r.Check(&got), "%s: a mutant that reads passes every bound", x.name)
				}
			}
		}
		assert.Greater(t, mutants, 300, x.name)
	}
}

// ---------------------------------------------------------------------------
// the instances

func mk[T comparable](name string, body int, hash uint64, layout []byte, reset func(*T), save func([]T, []byte) int64,
	load func([]T, []byte, []wire.TableFixedEntry, *wire.TableReport) int64, measure func(int64) int64,
	planEntries int, fromJSON func(*T, []byte, *wire.TableReport) bool, sample func() string, over func(*T)) *tab[T] {
	return &tab[T]{name: name, body: body, hash: hash, layout: layout, reset: reset, save: save, load: load, measure: measure,
		planRoom: 2 * planEntries, fromJSON: fromJSON, sample: sample, over: over}
}

func lit(s string) func() string { return func() string { return s } }

func tables() []every {
	var all []every
	add := func(e every) { all = append(all, e) }

	t1 := mk("Name128", wire.Name128FixedBodyBytes, wire.Name128FixedHash, wire.Name128FixedLayout, wire.Name128Reset, wire.Name128FixedSave, wire.Name128FixedLoad, wire.Name128FixedMeasure, len(wire.Name128FixedPlan.Entries), wire.Name128FromJson, lit(`{"value":"timer-1"}`), func(v *wire.Name128) { v.ValueLength = int32(len(v.Value)) + 1 })
	t1.small = true
	add(t1)
	t2 := mk("Name256", wire.Name256FixedBodyBytes, wire.Name256FixedHash, wire.Name256FixedLayout, wire.Name256Reset, wire.Name256FixedSave, wire.Name256FixedLoad, wire.Name256FixedMeasure, len(wire.Name256FixedPlan.Entries), wire.Name256FromJson, lit(`{"value":"card-a.r1.emma.g2"}`), func(v *wire.Name256) { v.ValueLength = int32(len(v.Value)) + 1 })
	t2.small = true
	add(t2)
	t3 := mk("Name400", wire.Name400FixedBodyBytes, wire.Name400FixedHash, wire.Name400FixedLayout, wire.Name400Reset, wire.Name400FixedSave, wire.Name400FixedLoad, wire.Name400FixedMeasure, len(wire.Name400FixedPlan.Entries), wire.Name400FromJson, lit(`{"value":"j12|card-a.r1.emma.g2"}`), func(v *wire.Name400) { v.ValueLength = int32(len(v.Value)) + 1 })
	t3.small = true
	add(t3)
	t4 := mk("Text512", wire.Text512FixedBodyBytes, wire.Text512FixedHash, wire.Text512FixedLayout, wire.Text512Reset, wire.Text512FixedSave, wire.Text512FixedLoad, wire.Text512FixedMeasure, len(wire.Text512FixedPlan.Entries), wire.Text512FromJson, lit(`{"value":"a line of words"}`), func(v *wire.Text512) { v.ValueLength = int32(len(v.Value)) + 1 })
	t4.small = true
	add(t4)
	t5 := mk("RequestDigest", wire.RequestDigestFixedBodyBytes, wire.RequestDigestFixedHash, wire.RequestDigestFixedLayout, wire.RequestDigestReset, wire.RequestDigestFixedSave, wire.RequestDigestFixedLoad, wire.RequestDigestFixedMeasure, len(wire.RequestDigestFixedPlan.Entries), wire.RequestDigestFromJson,
		func() string {
			return `{"digest":"` + store.ArgsOf(map[string]any{"card": "a", "to": "done"}) + `"}`
		}, func(v *wire.RequestDigest) { v.DigestLength = int32(len(v.Digest)) + 1 })
	t5.small = true
	add(t5)
	t6 := mk("HeldLock", wire.HeldLockFixedBodyBytes, wire.HeldLockFixedHash, wire.HeldLockFixedLayout, wire.HeldLockReset, wire.HeldLockFixedSave, wire.HeldLockFixedLoad, wire.HeldLockFixedMeasure, len(wire.HeldLockFixedPlan.Entries), wire.HeldLockFromJson,
		lit(`{"id":"finish-ab12-lock","verb":"finish lock","at":1792000000000000000}`), func(v *wire.HeldLock) { v.IdLength = int32(len(v.Id)) + 1 })
	t6.small = true
	add(t6)
	timer := `{"id":"t1","wakes":"friend.rowan","due":1792000600000000000,"note":"check the landing","by":"rowan","set":1792000000000000000}`
	t7 := mk("Timer", wire.TimerFixedBodyBytes, wire.TimerFixedHash, wire.TimerFixedLayout, wire.TimerReset, wire.TimerFixedSave, wire.TimerFixedLoad, wire.TimerFixedMeasure, len(wire.TimerFixedPlan.Entries), wire.TimerFromJson,
		lit(timer), func(v *wire.Timer) { v.NoteLength = int32(len(v.Note)) + 1 })
	t7.small = true
	add(t7)
	add(mk("Timers", wire.TimersFixedBodyBytes, wire.TimersFixedHash, wire.TimersFixedLayout, wire.TimersReset, wire.TimersFixedSave, wire.TimersFixedLoad, wire.TimersFixedMeasure, len(wire.TimersFixedPlan.Entries), wire.TimersFromJson,
		lit(`{"open_timers":[`+timer+`,`+strings.Replace(timer, `"t1"`, `"t2"`, 1)+`]}`), func(v *wire.Timers) { v.OpenTimersCount = int32(len(v.OpenTimers)) + 1 }))
	t9 := mk("Member", wire.MemberFixedBodyBytes, wire.MemberFixedHash, wire.MemberFixedLayout, wire.MemberReset, wire.MemberFixedSave, wire.MemberFixedLoad, wire.MemberFixedMeasure, len(wire.MemberFixedPlan.Entries), wire.MemberFromJson,
		lit(`{"id":"card-a.r1.emma"}`), func(v *wire.Member) { v.IdLength = int32(len(v.Id)) + 1 })
	t9.small = true
	add(t9)
	add(mk("MemberList", wire.MemberListFixedBodyBytes, wire.MemberListFixedHash, wire.MemberListFixedLayout, wire.MemberListReset, wire.MemberListFixedSave, wire.MemberListFixedLoad, wire.MemberListFixedMeasure, len(wire.MemberListFixedPlan.Entries), wire.MemberListFromJson,
		lit(`{"members":[{"id":"a"},{"id":"b"},{"id":"card-c.r1.emma"}]}`), func(v *wire.MemberList) { v.MembersCount = int32(len(v.Members)) + 1 }))
	add(mk("ChangeEvent", wire.ChangeEventFixedBodyBytes, wire.ChangeEventFixedHash, wire.ChangeEventFixedLayout, wire.ChangeEventReset, wire.ChangeEventFixedSave, wire.ChangeEventFixedLoad, wire.ChangeEventFixedMeasure, len(wire.ChangeEventFixedPlan.Entries), wire.ChangeEventFromJson,
		lit(`{"epoch":"7","verb":"apply","rev_before":41,"rev_after":42,"members":[{"id":"c1"}],"batch_members":[{"id":"c1"},{"id":"c2"}]}`), func(v *wire.ChangeEvent) { v.EpochLength = int32(len(v.Epoch)) + 1 }))
	t12 := mk("Cell", wire.CellFixedBodyBytes, wire.CellFixedHash, wire.CellFixedLayout, wire.CellReset, wire.CellFixedSave, wire.CellFixedLoad, wire.CellFixedMeasure, len(wire.CellFixedPlan.Entries), wire.CellFromJson,
		lit(`{"row":"member-01","column":"status","text":"working"}`), func(v *wire.Cell) { v.ColumnLength = int32(len(v.Column)) + 1 })
	t12.small = true
	add(t12)
	add(mk("CellSet", wire.CellSetFixedBodyBytes, wire.CellSetFixedHash, wire.CellSetFixedLayout, wire.CellSetReset, wire.CellSetFixedSave, wire.CellSetFixedLoad, wire.CellSetFixedMeasure, len(wire.CellSetFixedPlan.Entries), wire.CellSetFromJson,
		lit(`{"cells":[{"row":"member-01","column":"status","text":"working"},{"row":"member-02","column":"status","text":"idle"}]}`), func(v *wire.CellSet) { v.CellsCount = int32(len(v.Cells)) + 1 }))
	st := `{"state":"stopped","cause":"a red gate","waiting":3,"owners":[{"value":"emma"},{"value":"stella"}]}`
	add(mk("StreamTransition", wire.StreamTransitionFixedBodyBytes, wire.StreamTransitionFixedHash, wire.StreamTransitionFixedLayout, wire.StreamTransitionReset, wire.StreamTransitionFixedSave, wire.StreamTransitionFixedLoad, wire.StreamTransitionFixedMeasure, len(wire.StreamTransitionFixedPlan.Entries), wire.StreamTransitionFromJson,
		lit(st), func(v *wire.StreamTransition) { v.CauseLength = int32(len(v.Cause)) + 1 }))
	dd := `{"lane":"friend.emma","to":"friend.emma","from":"rowan","set":"v15-1","body":"answer the judgment j12","token":"tok-1","prepared_at":1792000000000000000,"retry_before":1792000300000000000,"sent_at":0,"fault":"","reports":[{"value":"r1"}],"unknown_receipts":0}`
	add(mk("DecisionDelivery", wire.DecisionDeliveryFixedBodyBytes, wire.DecisionDeliveryFixedHash, wire.DecisionDeliveryFixedLayout, wire.DecisionDeliveryReset, wire.DecisionDeliveryFixedSave, wire.DecisionDeliveryFixedLoad, wire.DecisionDeliveryFixedMeasure, len(wire.DecisionDeliveryFixedPlan.Entries), wire.DecisionDeliveryFromJson,
		lit(dd), func(v *wire.DecisionDelivery) { v.BodyLength = int32(len(v.Body)) + 1 }))
	cd := `{"covered_by":"","from":"rowan","to":"friend.stella","token":"tok-2","subject":"a judgment","body":"see j12","episode":"e1","sent":true,"retry_before":1792000300000000000}`
	add(mk("CoordinatorDelivery", wire.CoordinatorDeliveryFixedBodyBytes, wire.CoordinatorDeliveryFixedHash, wire.CoordinatorDeliveryFixedLayout, wire.CoordinatorDeliveryReset, wire.CoordinatorDeliveryFixedSave, wire.CoordinatorDeliveryFixedLoad, wire.CoordinatorDeliveryFixedMeasure, len(wire.CoordinatorDeliveryFixedPlan.Entries), wire.CoordinatorDeliveryFromJson,
		lit(cd), func(v *wire.CoordinatorDelivery) { v.SubjectLength = int32(len(v.Subject)) + 1 }))
	note := `{"id":"n-7-1","safe_default":"none","default_coordinator_told":true,"default_owner_told":false,"alias":"j12","kind":"judgment","note_type":"a red branch","stream":"v15-schema-tables","primaries":[{"value":"a8-defs-store-op-rows"},{"value":"a8-defs-store-x"}],"count":2,"what":"the gate is red","who":"rowan","attempt":1,"before":0,"at":1792000000000000000,"decisions":[{"value":"hold"},{"value":"redeal"}],"marked":true,"answers":"","suspects":[{"value":"a8-defs-store-x"}],"card":"a8-defs-store-x","other":"","tier":"pro","other_stream":"","stream_level":false,"sprint_level":false,"needs":[{"value":"a7"}],"review":1792000900000000000,"review_set":1792000000000000000,"to":"rowan","hint":"run: nova-sprint inbox","stream_transition":` + st + `,"decision_delivery":` + dd + `,"coordinator_delivery":` + cd + `}`
	add(mk("Note", wire.NoteFixedBodyBytes, wire.NoteFixedHash, wire.NoteFixedLayout, wire.NoteReset, wire.NoteFixedSave, wire.NoteFixedLoad, wire.NoteFixedMeasure, len(wire.NoteFixedPlan.Entries), wire.NoteFromJson,
		lit(note), func(v *wire.Note) { v.PrimariesCount = int32(len(v.Primaries)) + 1 }))
	t19 := mk("TextRef", wire.TextRefFixedBodyBytes, wire.TextRefFixedHash, wire.TextRefFixedLayout, wire.TextRefReset, wire.TextRefFixedSave, wire.TextRefFixedLoad, wire.TextRefFixedMeasure, len(wire.TextRefFixedPlan.Entries), wire.TextRefFromJson,
		lit(`{"name":"report","size":2048}`), func(v *wire.TextRef) { v.NameLength = int32(len(v.Name)) + 1 })
	t19.small = true
	add(t19)
	t20 := mk("Pair", wire.PairFixedBodyBytes, wire.PairFixedHash, wire.PairFixedLayout, wire.PairReset, wire.PairFixedSave, wire.PairFixedLoad, wire.PairFixedMeasure, len(wire.PairFixedPlan.Entries), wire.PairFromJson,
		lit(`{"name":"head","value":"4d9941fbc7fd"}`), func(v *wire.Pair) { v.NameLength = int32(len(v.Name)) + 1 })
	t20.small = true
	add(t20)
	line := `{"kind":"move","at":1792000000000000000,"epoch":7,"op":"finish-ab12-1","card":"a8-defs-store-op-rows","cards":[{"value":"a8-defs-store-op-rows"}],"primary":"a8-defs-store-op-rows","stream":"v15-schema-tables","table_name":"work","from":"emma:working","to":"rowan:done","removed":false,"gen":3,"actor":"rowan","verb":"finish","cause":"the gate is green","answers":[{"value":"j12"}],"text":[{"name":"report","size":2048}],"set":[{"name":"head","value":"4d9941fbc7fd"}],"note":` + note + `}`
	add(mk("LogLine", wire.LogLineFixedBodyBytes, wire.LogLineFixedHash, wire.LogLineFixedLayout, wire.LogLineReset, wire.LogLineFixedSave, wire.LogLineFixedLoad, wire.LogLineFixedMeasure, len(wire.LogLineFixedPlan.Entries), wire.LogLineFromJson,
		lit(line), func(v *wire.LogLine) { v.CauseLength = int32(len(v.Cause)) + 1 }))
	seat := `{"holder":"rowan","generation":4,"from":"stella","at":1792000000000000000,"by":"glenn","taken":false,"approved_by":"glenn","reason":"the coordinator is rowan again"}`
	add(mk("SeatChange", wire.SeatChangeFixedBodyBytes, wire.SeatChangeFixedHash, wire.SeatChangeFixedLayout, wire.SeatChangeReset, wire.SeatChangeFixedSave, wire.SeatChangeFixedLoad, wire.SeatChangeFixedMeasure, len(wire.SeatChangeFixedPlan.Entries), wire.SeatChangeFromJson,
		lit(seat), func(v *wire.SeatChange) { v.ReasonLength = int32(len(v.Reason)) + 1 }))
	prop := `{"table_name":"work","name":"promoted","value":"a8","was":"a7","was_absent":false}`
	add(mk("PropWrite", wire.PropWriteFixedBodyBytes, wire.PropWriteFixedHash, wire.PropWriteFixedLayout, wire.PropWriteReset, wire.PropWriteFixedSave, wire.PropWriteFixedLoad, wire.PropWriteFixedMeasure, len(wire.PropWriteFixedPlan.Entries), wire.PropWriteFromJson,
		lit(prop), func(v *wire.PropWrite) { v.ValueLength = int32(len(v.Value)) + 1 }))
	queued := `{"entry_bytes":120,"prop":` + prop + `,"verb":"finish","actor":"emma","moved":"a8-defs-store-op-rows"}`
	add(mk("QueuedChange", wire.QueuedChangeFixedBodyBytes, wire.QueuedChangeFixedHash, wire.QueuedChangeFixedLayout, wire.QueuedChangeReset, wire.QueuedChangeFixedSave, wire.QueuedChangeFixedLoad, wire.QueuedChangeFixedMeasure, len(wire.QueuedChangeFixedPlan.Entries), wire.QueuedChangeFromJson,
		lit(queued), func(v *wire.QueuedChange) { v.VerbLength = int32(len(v.Verb)) + 1 }))
	add(mk("OpRecord", wire.OpRecordFixedBodyBytes, wire.OpRecordFixedHash, wire.OpRecordFixedLayout, wire.OpRecordReset, wire.OpRecordFixedSave, wire.OpRecordFixedLoad, wire.OpRecordFixedMeasure, len(wire.OpRecordFixedPlan.Entries), wire.OpRecordFromJson,
		lit(`{"id":"finish-ab12-1","is_lock":false,"verb":"finish","at":1792000000000000000,"manifests":2,"notes":1,"decided":0,"log":3,"updates":0,"closes":[{"value":"j12|a8"}],"streams":[{"value":"v15-schema-tables"}],"caller_op":"finish-ab12","has_result":true,"stuck":"","queue":1,"drain":0,"seat":`+seat+`,"close_timers":[{"value":"t1"}],"has_timers":true}`),
		func(v *wire.OpRecord) { v.IdLength = int32(len(v.Id)) + 1 }))
	add(mk("OpNote", wire.OpNoteFixedBodyBytes, wire.OpNoteFixedHash, wire.OpNoteFixedLayout, wire.OpNoteReset, wire.OpNoteFixedSave, wire.OpNoteFixedLoad, wire.OpNoteFixedMeasure, len(wire.OpNoteFixedPlan.Entries), wire.OpNoteFromJson,
		lit(`{"op_id":"finish-ab12-1","role":"notes","index":0,"note":`+note+`}`), func(v *wire.OpNote) { v.RoleLength = int32(len(v.Role)) + 1 }))
	add(mk("OpLogLine", wire.OpLogLineFixedBodyBytes, wire.OpLogLineFixedHash, wire.OpLogLineFixedLayout, wire.OpLogLineReset, wire.OpLogLineFixedSave, wire.OpLogLineFixedLoad, wire.OpLogLineFixedMeasure, len(wire.OpLogLineFixedPlan.Entries), wire.OpLogLineFromJson,
		lit(`{"op_id":"finish-ab12-1","index":2,"line":`+line+`}`), func(v *wire.OpLogLine) { v.OpIdLength = int32(len(v.OpId)) + 1 }))
	add(mk("OpQueued", wire.OpQueuedFixedBodyBytes, wire.OpQueuedFixedHash, wire.OpQueuedFixedLayout, wire.OpQueuedReset, wire.OpQueuedFixedSave, wire.OpQueuedFixedLoad, wire.OpQueuedFixedMeasure, len(wire.OpQueuedFixedPlan.Entries), wire.OpQueuedFromJson,
		lit(`{"op_id":"finish-ab12-1","index":0,"change":`+queued+`}`), func(v *wire.OpQueued) { v.OpIdLength = int32(len(v.OpId)) + 1 }))
	chunk := mk("Chunk", wire.ChunkFixedBodyBytes, wire.ChunkFixedHash, wire.ChunkFixedLayout, wire.ChunkReset, wire.ChunkFixedSave, wire.ChunkFixedLoad, wire.ChunkFixedMeasure, len(wire.ChunkFixedPlan.Entries), wire.ChunkFromJson,
		lit(`{"op_id":"finish-ab12-1","kind":"manifest","key":"work","index":0,"count":2}`), func(v *wire.Chunk) { v.DataLength = int32(len(v.Data)) + 1 })
	chunk.post = func(v *wire.Chunk) {
		body := []byte(`{"schema":1,"table":"work","members":[]}`)
		v.DataLength = int32(copy(v.Data[:], body))
	}
	add(chunk)
	t33 := mk("Refusal", wire.RefusalFixedBodyBytes, wire.RefusalFixedHash, wire.RefusalFixedLayout, wire.RefusalReset, wire.RefusalFixedSave, wire.RefusalFixedLoad, wire.RefusalFixedMeasure, len(wire.RefusalFixedPlan.Entries), wire.RefusalFromJson,
		lit(`{"id":"a8-defs-store-op-rows","why":"the card is held"}`), func(v *wire.Refusal) { v.WhyLength = int32(len(v.Why)) + 1 })
	t33.small = true
	add(t33)
	t34 := mk("TableCount", wire.TableCountFixedBodyBytes, wire.TableCountFixedHash, wire.TableCountFixedLayout, wire.TableCountReset, wire.TableCountFixedSave, wire.TableCountFixedLoad, wire.TableCountFixedMeasure, len(wire.TableCountFixedPlan.Entries), wire.TableCountFromJson,
		lit(`{"name":"work","count":3}`), func(v *wire.TableCount) { v.NameLength = int32(len(v.Name)) + 1 })
	t34.small = true
	add(t34)
	add(mk("StepResult", wire.StepResultFixedBodyBytes, wire.StepResultFixedHash, wire.StepResultFixedLayout, wire.StepResultReset, wire.StepResultFixedSave, wire.StepResultFixedLoad, wire.StepResultFixedMeasure, len(wire.StepResultFixedPlan.Entries), wire.StepResultFromJson,
		lit(`{"verb":"finish","op":"finish-ab12-1","moved":[{"value":"a8"},{"value":"a9"}],"refused":[{"id":"a10","why":"held"}],"notes":1,"raised":[{"value":"j12 a red branch"}],"said":[{"value":"NOTE the gate is red"}],"attempts":1,"replay":false,"repaired":[],"pending":"","skipped":[{"value":"a11: expectation moved"}],"args":"`+store.ArgsOf("x")+`","lost":false,"tables":[{"name":"work","count":2}],"drained":0}`),
		func(v *wire.StepResult) { v.VerbLength = int32(len(v.Verb)) + 1 }))
	return all
}

// lockBlock is the schema.lock text of one table: from its header line to the
// next table's.
func lockBlock(t *testing.T, lock, name string) string {
	t.Helper()
	head := "fixed table " + name + " layout="
	i := strings.Index(lock, "\n"+head)
	require.GreaterOrEqual(t, i, 0, "schema.lock holds table %s", name)
	rest := lock[i+1:]
	if j := strings.Index(rest[1:], "\nfixed table "); j >= 0 {
		rest = rest[:j+1]
	}
	return rest
}

func TestOpRowsGolden(t *testing.T) {
	t.Parallel()

	all := tables()
	require.Len(t, all, 31, "the area's tables")

	// each table, in its own subtest
	for _, e := range all {
		t.Run(e.table(), func(t *testing.T) {
			t.Parallel()
			e.run(t)
		})
	}

	// the generated Go is the committed lock and the committed baseline: the
	// pin (generate.go) wrote the Go from the schema the lock and the baseline
	// were written from
	t.Run("the committed lock and baseline name every table", func(t *testing.T) {
		t.Parallel()
		root := filepath.Join("..", "..", "..", "..", "schemas", "oprows")
		lockRaw, err := os.ReadFile(filepath.Join(root, "schema.lock"))
		require.NoError(t, err, "schemas/oprows/schema.lock is committed")
		baseRaw, err := os.ReadFile(filepath.Join(root, "tables.baseline"))
		require.NoError(t, err, "schemas/oprows/tables.baseline is committed")
		_, err = os.Stat(filepath.Join(root, "oprows.schema"))
		require.NoError(t, err, "schemas/oprows/oprows.schema is committed")
		lock, base := string(lockRaw), string(baseRaw)
		assert.True(t, strings.HasPrefix(lock, "schema-lock "), "schema.lock opens with its format line")
		for _, e := range all {
			block := lockBlock(t, lock, e.table())
			assert.Contains(t, base, e.table(), "tables.baseline names %s", e.table())
			assert.Contains(t, block, fmt.Sprintf("record=%d", e.bodyBytes()), "%s: schema.lock's newest lineage holds the record at the bytes the Go is generated at", e.table())
		}
	})

	// the lock's wire hash of each table is the generated Go's hash
	t.Run("generated hashes are the lock's", func(t *testing.T) {
		t.Parallel()
		lockRaw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "schemas", "oprows", "schema.lock"))
		require.NoError(t, err)
		lock := string(lockRaw)
		for _, e := range all {
			h := e.wireHash()
			require.NotZero(t, h, "%s has a hash", e.table())
			assert.Contains(t, lockBlock(t, lock, e.table()), fmt.Sprintf("lineage wire=0x%016x", h), "%s: the Go's wire hash is the lock's", e.table())
		}
	})

	// the bounds the code enforces are the declared bounds: each is named by
	// the constant of the code it comes from
	t.Run("declared bounds are the code's constants", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, sprint.MaxIDLen, len(wire.Name128{}.Value), "sprint.MaxIDLen: an identity word")
		assert.Equal(t, ntable.LimitMemberIDBytes, len(wire.Name256{}.Value), "ntable.LimitMemberIDBytes: a card or member id")
		assert.Equal(t, ntable.LimitMemberIDBytes, len(wire.Member{}.Id), "ntable.LimitMemberIDBytes: a moved member")
		assert.Equal(t, ntable.LimitChangedEntries, len(wire.MemberList{}.Members), "ntable.LimitChangedEntries: the most records one batch changes")
		assert.Equal(t, sprint.MaxListed, len(wire.Note{}.Primaries), "sprint.MaxListed: Note.Bound cuts primaries to it")
		assert.Equal(t, sprint.MaxCause, len(wire.LogLine{}.Cause), "sprint.MaxCause: a log line's cause")
		assert.Equal(t, sprint.MaxCause, len(wire.StreamTransition{}.Cause), "sprint.MaxCause")
		assert.Equal(t, sprint.MaxTimerNote, len(wire.Timer{}.Note), "sprint.MaxTimerNote: a timer's note")
		assert.Equal(t, len(store.ArgsOf("x")), len(wire.RequestDigest{}.Digest), "store.ArgsOf: 24 hex characters")
	})
}
