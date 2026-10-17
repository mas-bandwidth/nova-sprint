package friendrowswire

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
	"unsafe"

	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
)

// clock is the test clock: 1000.0 seconds, in Unix nanoseconds. A zero time
// stays zero, so a missed stamp cannot pass for this one.
const clock int64 = 1000 * 1000 * 1000 * 1000

// friendRowsHelp is the run: text of a friendrows refusal.
const friendRowsHelp = "nova-sprint store friendrows"

// TestFriendRowsGolden pins the friendrows fixed tables (S1): each record
// round-trips in binary wire, padded standard base64 of that wire, and the
// schema JSON text (S2), through pkg/schemaread. A value past each declared
// bound, and a damaged image of each form, is refused by name. The strict
// reader returns the zero value; it does not hand back a clamped one.
func TestFriendRowsGolden(t *testing.T) {
	t.Parallel()
	golden(t, row[FriendEntry]("FriendEntry", FriendEntryReset, FriendEntryFixedMeasure, FriendEntryFixedSave, FriendEntryFixedLoad, FriendEntryFromJson, FriendEntryToJsonMeasure, FriendEntryToJson, FriendEntryTableType, len(FriendEntryFixedPlan.Entries)))
	golden(t, row[FriendStatus]("FriendStatus", FriendStatusReset, FriendStatusFixedMeasure, FriendStatusFixedSave, FriendStatusFixedLoad, FriendStatusFromJson, FriendStatusToJsonMeasure, FriendStatusToJson, FriendStatusTableType, len(FriendStatusFixedPlan.Entries)))
	golden(t, row[LoadSample]("LoadSample", LoadSampleReset, LoadSampleFixedMeasure, LoadSampleFixedSave, LoadSampleFixedLoad, LoadSampleFromJson, LoadSampleToJsonMeasure, LoadSampleToJson, LoadSampleTableType, len(LoadSampleFixedPlan.Entries)))
	golden(t, row[Ticks]("Ticks", TicksReset, TicksFixedMeasure, TicksFixedSave, TicksFixedLoad, TicksFromJson, TicksToJsonMeasure, TicksToJson, TicksTableType, len(TicksFixedPlan.Entries)))
	golden(t, row[Holder]("Holder", HolderReset, HolderFixedMeasure, HolderFixedSave, HolderFixedLoad, HolderFromJson, HolderToJsonMeasure, HolderToJson, HolderTableType, len(HolderFixedPlan.Entries)))
	golden(t, row[Files]("Files", FilesReset, FilesFixedMeasure, FilesFixedSave, FilesFixedLoad, FilesFromJson, FilesToJsonMeasure, FilesToJson, FilesTableType, len(FilesFixedPlan.Entries)))
	golden(t, row[Meter]("Meter", MeterReset, MeterFixedMeasure, MeterFixedSave, MeterFixedLoad, MeterFromJson, MeterToJsonMeasure, MeterToJson, MeterTableType, len(MeterFixedPlan.Entries)))
	golden(t, row[RunWord]("RunWord", RunWordReset, RunWordFixedMeasure, RunWordFixedSave, RunWordFixedLoad, RunWordFromJson, RunWordToJsonMeasure, RunWordToJson, RunWordTableType, len(RunWordFixedPlan.Entries)))
	golden(t, row[FriendReport]("FriendReport", FriendReportReset, FriendReportFixedMeasure, FriendReportFixedSave, FriendReportFixedLoad, FriendReportFromJson, FriendReportToJsonMeasure, FriendReportToJson, FriendReportTableType, len(FriendReportFixedPlan.Entries)))
	golden(t, row[LiveWord]("LiveWord", LiveWordReset, LiveWordFixedMeasure, LiveWordFixedSave, LiveWordFixedLoad, LiveWordFromJson, LiveWordToJsonMeasure, LiveWordToJson, LiveWordTableType, len(LiveWordFixedPlan.Entries)))
	golden(t, row[FactWord]("FactWord", FactWordReset, FactWordFixedMeasure, FactWordFixedSave, FactWordFixedLoad, FactWordFromJson, FactWordToJsonMeasure, FactWordToJson, FactWordTableType, len(FactWordFixedPlan.Entries)))
	golden(t, row[LiveSight]("LiveSight", LiveSightReset, LiveSightFixedMeasure, LiveSightFixedSave, LiveSightFixedLoad, LiveSightFromJson, LiveSightToJsonMeasure, LiveSightToJson, LiveSightTableType, len(LiveSightFixedPlan.Entries)))
	golden(t, row[Beat]("Beat", BeatReset, BeatFixedMeasure, BeatFixedSave, BeatFixedLoad, BeatFromJson, BeatToJsonMeasure, BeatToJson, BeatTableType, len(BeatFixedPlan.Entries)))
	golden(t, row[ReaderHold]("ReaderHold", ReaderHoldReset, ReaderHoldFixedMeasure, ReaderHoldFixedSave, ReaderHoldFixedLoad, ReaderHoldFromJson, ReaderHoldToJsonMeasure, ReaderHoldToJson, ReaderHoldTableType, len(ReaderHoldFixedPlan.Entries)))
	golden(t, row[HoldView]("HoldView", HoldViewReset, HoldViewFixedMeasure, HoldViewFixedSave, HoldViewFixedLoad, HoldViewFromJson, HoldViewToJsonMeasure, HoldViewToJson, HoldViewTableType, len(HoldViewFixedPlan.Entries)))
	golden(t, row[LaneHold]("LaneHold", LaneHoldReset, LaneHoldFixedMeasure, LaneHoldFixedSave, LaneHoldFixedLoad, LaneHoldFromJson, LaneHoldToJsonMeasure, LaneHoldToJson, LaneHoldTableType, len(LaneHoldFixedPlan.Entries)))
	golden(t, row[LaneWait]("LaneWait", LaneWaitReset, LaneWaitFixedMeasure, LaneWaitFixedSave, LaneWaitFixedLoad, LaneWaitFromJson, LaneWaitToJsonMeasure, LaneWaitToJson, LaneWaitTableType, len(LaneWaitFixedPlan.Entries)))
	golden(t, row[Lane]("Lane", LaneReset, LaneFixedMeasure, LaneFixedSave, LaneFixedLoad, LaneFromJson, LaneToJsonMeasure, LaneToJson, LaneTableType, len(LaneFixedPlan.Entries)))
	golden(t, row[SeatChange]("SeatChange", SeatChangeReset, SeatChangeFixedMeasure, SeatChangeFixedSave, SeatChangeFixedLoad, SeatChangeFromJson, SeatChangeToJsonMeasure, SeatChangeToJson, SeatChangeTableType, len(SeatChangeFixedPlan.Entries)))
	golden(t, row[ServerRecord]("ServerRecord", ServerRecordReset, ServerRecordFixedMeasure, ServerRecordFixedSave, ServerRecordFixedLoad, ServerRecordFromJson, ServerRecordToJsonMeasure, ServerRecordToJson, ServerRecordTableType, len(ServerRecordFixedPlan.Entries)))
	golden(t, row[Login]("Login", LoginReset, LoginFixedMeasure, LoginFixedSave, LoginFixedLoad, LoginFromJson, LoginToJsonMeasure, LoginToJson, LoginTableType, len(LoginFixedPlan.Entries)))
}

// tableRow is one fixed table's generated functions.
type tableRow[T comparable] struct {
	name          string
	reset         func(*T)
	measure       func(int64) int64
	save          func([]T, []byte) int64
	load          func([]T, []byte, []TableFixedEntry, *TableReport) int64
	fromJSON      func(*T, []byte, *TableReport) bool
	toJSONMeasure func(*T) int64
	toJSON        func(*T, []byte) int64
	table         func() *TableTypeInfo
	planEntries   int
}

func row[T comparable](
	name string,
	reset func(*T),
	measure func(int64) int64,
	save func([]T, []byte) int64,
	load func([]T, []byte, []TableFixedEntry, *TableReport) int64,
	fromJSON func(*T, []byte, *TableReport) bool,
	toJSONMeasure func(*T) int64,
	toJSON func(*T, []byte) int64,
	table func() *TableTypeInfo,
	planEntries int,
) tableRow[T] {
	return tableRow[T]{
		name: name, reset: reset, measure: measure, save: save, load: load,
		fromJSON: fromJSON, toJSONMeasure: toJSONMeasure, toJSON: toJSON,
		table: table, planEntries: planEntries,
	}
}

func golden[T comparable](t *testing.T, g tableRow[T]) {
	t.Helper()
	t.Run(g.name, func(t *testing.T) {
		t.Parallel()
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

		got := g.read(t, wire, schemaread.FormBinary)
		if got != sample {
			t.Fatalf("%s binary round trip differs", g.name)
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

		g.pastBounds(t, &sample)
		g.damaged(t, wire, text)
	})
}

func (g tableRow[T]) wire(t *testing.T, v *T) []byte {
	t.Helper()
	n := g.measure(1)
	if n < 1 {
		t.Fatalf("%s measure = %d", g.name, n)
	}
	buf := make([]byte, n)
	if g.save([]T{*v}, buf) != n {
		t.Fatalf("%s save short", g.name)
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
		Forms: []byte{TableFixedForm},
		LoadWire: func(v *T, wire []byte) schemaread.Report {
			var rep TableReport
			vals := [1]T{*v}
			if g.load(vals[:], wire, make([]TableFixedEntry, 2*g.planEntries), &rep) < 0 && rep.Reason == "" && !rep.Malformed {
				rep.Malformed = true
			}
			*v = vals[0]
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
	if err == nil {
		t.Fatalf("%s accepted a value that should be %s", g.name, want)
	}
	var ref *schemaread.Refusal
	if !errors.As(err, &ref) {
		t.Fatalf("%s err = %v, want a named refusal", g.name, err)
	}
	if ref.Reason != want {
		t.Fatalf("%s reason = %s, want %s", g.name, ref.Reason, want)
	}
	if got != zero {
		t.Fatalf("%s refused %s and returned a non-zero value", g.name, want)
	}
}

// pastBounds walks every declared bound of the table, including a bound on a
// nested table, and refuses one step past it by the name clamped.
func (g tableRow[T]) pastBounds(t *testing.T, sample *T) {
	t.Helper()
	var walk func(info *TableTypeInfo, at uint32, present []uint32)
	walk = func(info *TableTypeInfo, at uint32, present []uint32) {
		for i := range info.Fields {
			f := info.Fields[i]
			path := present
			if f.Optional {
				path = append(append([]uint32{}, present...), at+f.PresentOffset)
			}
			off := at + f.Offset
			if f.Counted && f.TypeName == "string" && !f.IsArray {
				g.poke(t, sample, func(p unsafe.Pointer) {
					setPresent(p, path)
					putOverlong(unsafe.Add(p, off), f.ArrayBound, unsafe.Add(p, at+f.CountOffset))
				})
			}
			if f.Counted && f.IsArray {
				g.poke(t, sample, func(p unsafe.Pointer) {
					setPresent(p, path)
					*(*int32)(unsafe.Add(p, at+f.CountOffset)) = f.ArrayBound + 1
				})
			}
			if f.HasRange {
				g.poke(t, sample, func(p unsafe.Pointer) {
					setPresent(p, path)
					putUnderMin(unsafe.Add(p, off), f)
				})
			}
			if f.Table != nil {
				walk(f.Table(), off, path)
			}
		}
	}
	walk(g.table(), 0, nil)
}

func (g tableRow[T]) poke(t *testing.T, sample *T, mutate func(unsafe.Pointer)) {
	t.Helper()
	var v T
	g.reset(&v)
	// copy the in-range sample, then move one field past its bound
	v = *sample
	mutate(unsafe.Pointer(&v))
	g.refuse(t, g.wire(t, &v), schemaread.ReasonClamped)
}

func (g tableRow[T]) damaged(t *testing.T, wire, text []byte) {
	t.Helper()
	short := wire[:4]
	g.refuse(t, short, schemaread.ReasonMalformed)
	flipped := bytes.Clone(wire)
	flipped[TableFixedHashAt] ^= 0xff
	g.refuse(t, flipped, "layout_newer")
	unknown := bytes.Clone(wire)
	unknown[0] = 4
	g.refuse(t, unknown, schemaread.ReasonUnknownForm)
	g.refuse(t, append(append([]byte{}, text...), '!'), schemaread.ReasonMalformed)
	g.refuse(t, []byte("{"), schemaread.ReasonMalformed)
}

func setPresent(p unsafe.Pointer, offs []uint32) {
	for _, off := range offs {
		*(*bool)(unsafe.Add(p, off)) = true
	}
}

// putOverlong fills a string's bytes with 'a' and sets its length one past the
// declared max. The bytes that remain after the clamp are well-formed text, so
// the refusal is the bound and not an ill-formed payload.
func putOverlong(dst unsafe.Pointer, bound int32, length unsafe.Pointer) {
	b := unsafe.Slice((*byte)(dst), int(bound))
	for i := range b {
		b[i] = 'a'
	}
	*(*int32)(length) = bound + 1
}

// putUnderMin writes one less than the declared minimum. A one-sided code
// bound is declared with the storage type's maximum beside it, and that
// maximum does not fit in the next value of the type, so the step past the
// bound is below the minimum.
func putUnderMin(dst unsafe.Pointer, f TableFieldInfo) {
	switch f.TypeName {
	case "int32":
		*(*int32)(dst) = int32(f.RangeMin) - 1
	case "int64":
		*(*int64)(dst) = int64(f.RangeMin) - 1
	default:
		*(*int64)(dst) = -1
	}
}

// fill writes an in-range non-zero value. Times are clock, never the zero time.
func fill(p unsafe.Pointer, info *TableTypeInfo) {
	for i := range info.Fields {
		f := &info.Fields[i]
		if f.Optional {
			*(*bool)(unsafe.Add(p, f.PresentOffset)) = true
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
		case f.TypeName == "string":
			b := unsafe.Slice((*byte)(base), int(f.ArrayBound))
			b[0] = 'a'
			*(*int32)(unsafe.Add(p, f.CountOffset)) = 1
		case f.TypeName == "bool":
			*(*bool)(base) = true
		case f.TypeName == "int32":
			*(*int32)(base) = inInt32(f)
		case f.TypeName == "uint32":
			*(*uint32)(base) = 1
		case f.TypeName == "int64":
			*(*int64)(base) = clock
		case f.TypeName == "uint64":
			*(*uint64)(base) = uint64(clock)
		case f.TypeName == "float64":
			*(*float64)(base) = 1
		}
	}
}

func inInt32(f *TableFieldInfo) int32 {
	if f.HasRange && f.RangeMin > 0 {
		return int32(f.RangeMin)
	}
	return 1
}
