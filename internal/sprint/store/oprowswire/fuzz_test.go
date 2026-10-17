package oprowswire

// FuzzOprowswire feeds hostile bytes, in three forms (binary wire, padded standard
// base64, JSON text), to every generated root. go test runs the checked-in seeds only;
// nightly: go test -run='^$' -fuzz='^FuzzOprowswire$' -fuzztime=5m ./internal/sprint/store/oprowswire
// An input is [root selector byte][bytes].

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type fuzzForm int

const (
	fuzzWire fuzzForm = iota
	fuzzBase64
	fuzzJSON
	fuzzEmpty // run on a reset value, no input
)

// fuzzRoot is one generated root; run reads in as the form and returns the
// accepted value's wire and JSON images.
type fuzzRoot struct {
	name string
	run  func(f fuzzForm, in []byte, rep *TableReport) (wire, text []byte, ok bool)
}

func (r fuzzRoot) load(f fuzzForm, in []byte) (wire, text []byte, rep TableReport, err error) {
	if f == fuzzBase64 {
		if in, err = base64.StdEncoding.DecodeString(string(in)); err != nil {
			return nil, nil, rep, TableRefuseWireDamaged
		}
	}
	wire, text, ok := r.run(f, in, &rep)
	if err = rep.Err(); err == nil && !ok {
		err = TableRefuseWireDamaged
	}
	return wire, text, rep, err
}

func fuzzB64(b []byte) []byte { return []byte(base64.StdEncoding.EncodeToString(b)) }

func fuzzImage(n int64, save func([]byte) int64) []byte {
	if n < 0 {
		return nil
	}
	b := make([]byte, n)
	if save(b) != n {
		return nil
	}
	return b
}

// fuzzBuilder is a variable table read through its Builder (Lock packs it).
func fuzzBuilder[B any, PB interface {
	*B
	Init(...TableAllocator) bool
	Shutdown()
	Lock() bool
	AsConst() *R
}, R any](name string, loadWire, fromJSON func(PB, []byte, *TableReport) bool, wire, text func(*R) []byte) fuzzRoot {
	return fuzzRoot{name, func(f fuzzForm, in []byte, rep *TableReport) ([]byte, []byte, bool) {
		var b B
		pb := PB(&b)
		if !pb.Init() {
			return nil, nil, false
		}
		defer pb.Shutdown()
		ok := f == fuzzEmpty || (f == fuzzJSON && fromJSON(pb, in, rep)) || (f != fuzzJSON && loadWire(pb, in, rep))
		if ok && pb.Lock() {
			return wire(pb.AsConst()), text(pb.AsConst()), true
		}
		return nil, nil, false
	}}
}

// END builder
// fuzzValue is a fixed table, read into a plain value.
func fuzzValue[R any](name string, loadWire, fromJSON func(*R, []byte, *TableReport) bool, wire, text func(*R) []byte) fuzzRoot {
	return fuzzRoot{name, func(f fuzzForm, in []byte, rep *TableReport) ([]byte, []byte, bool) {
		var v R
		ok := f == fuzzEmpty || (f == fuzzJSON && fromJSON(&v, in, rep)) || (f != fuzzJSON && loadWire(&v, in, rep))
		if ok {
			return wire(&v), text(&v), true
		}
		return nil, nil, false
	}}
}

// fuzzRoots is every generated root, written by roots.sh.
var fuzzRoots = []fuzzRoot{
	fuzzBuilder("CardText", func(b *CardTextBuilder, w []byte, r *TableReport) bool { return CardTextLoadBuilder(b, w, r) }, func(b *CardTextBuilder, w []byte, r *TableReport) bool { return CardTextFromJson(b, w, r) }, func(v *CardText) []byte {
		return fuzzImage(CardTextMeasure(v), func(b []byte) int64 { return CardTextSave(v, b) })
	}, func(v *CardText) []byte {
		return fuzzImage(CardTextToJsonMeasure(v), func(b []byte) int64 { return CardTextToJson(v, b) })
	}),
	fuzzValue("Cell", func(v *Cell, w []byte, r *TableReport) bool { return CellLoad(v, w, r) }, func(v *Cell, w []byte, r *TableReport) bool { return CellFromJson(v, w, r) }, func(v *Cell) []byte { return fuzzImage(CellMeasure(v), func(b []byte) int64 { return CellSave(v, b) }) }, func(v *Cell) []byte {
		return fuzzImage(CellToJsonMeasure(v), func(b []byte) int64 { return CellToJson(v, b) })
	}),
	fuzzBuilder("CellSet", func(b *CellSetBuilder, w []byte, r *TableReport) bool { return CellSetLoadBuilder(b, w, r) }, func(b *CellSetBuilder, w []byte, r *TableReport) bool { return CellSetFromJson(b, w, r) }, func(v *CellSet) []byte {
		return fuzzImage(CellSetMeasure(v), func(b []byte) int64 { return CellSetSave(v, b) })
	}, func(v *CellSet) []byte {
		return fuzzImage(CellSetToJsonMeasure(v), func(b []byte) int64 { return CellSetToJson(v, b) })
	}),
	fuzzBuilder("ChangeEvent", func(b *ChangeEventBuilder, w []byte, r *TableReport) bool { return ChangeEventLoadBuilder(b, w, r) }, func(b *ChangeEventBuilder, w []byte, r *TableReport) bool { return ChangeEventFromJson(b, w, r) }, func(v *ChangeEvent) []byte {
		return fuzzImage(ChangeEventMeasure(v), func(b []byte) int64 { return ChangeEventSave(v, b) })
	}, func(v *ChangeEvent) []byte {
		return fuzzImage(ChangeEventToJsonMeasure(v), func(b []byte) int64 { return ChangeEventToJson(v, b) })
	}),
	fuzzValue("CoordinatorDelivery", func(v *CoordinatorDelivery, w []byte, r *TableReport) bool { return CoordinatorDeliveryLoad(v, w, r) }, func(v *CoordinatorDelivery, w []byte, r *TableReport) bool {
		return CoordinatorDeliveryFromJson(v, w, r)
	}, func(v *CoordinatorDelivery) []byte {
		return fuzzImage(CoordinatorDeliveryMeasure(v), func(b []byte) int64 { return CoordinatorDeliverySave(v, b) })
	}, func(v *CoordinatorDelivery) []byte {
		return fuzzImage(CoordinatorDeliveryToJsonMeasure(v), func(b []byte) int64 { return CoordinatorDeliveryToJson(v, b) })
	}),
	fuzzBuilder("DecisionDelivery", func(b *DecisionDeliveryBuilder, w []byte, r *TableReport) bool {
		return DecisionDeliveryLoadBuilder(b, w, r)
	}, func(b *DecisionDeliveryBuilder, w []byte, r *TableReport) bool {
		return DecisionDeliveryFromJson(b, w, r)
	}, func(v *DecisionDelivery) []byte {
		return fuzzImage(DecisionDeliveryMeasure(v), func(b []byte) int64 { return DecisionDeliverySave(v, b) })
	}, func(v *DecisionDelivery) []byte {
		return fuzzImage(DecisionDeliveryToJsonMeasure(v), func(b []byte) int64 { return DecisionDeliveryToJson(v, b) })
	}),
	fuzzValue("HeldLock", func(v *HeldLock, w []byte, r *TableReport) bool { return HeldLockLoad(v, w, r) }, func(v *HeldLock, w []byte, r *TableReport) bool { return HeldLockFromJson(v, w, r) }, func(v *HeldLock) []byte {
		return fuzzImage(HeldLockMeasure(v), func(b []byte) int64 { return HeldLockSave(v, b) })
	}, func(v *HeldLock) []byte {
		return fuzzImage(HeldLockToJsonMeasure(v), func(b []byte) int64 { return HeldLockToJson(v, b) })
	}),
	fuzzBuilder("LogLine", func(b *LogLineBuilder, w []byte, r *TableReport) bool { return LogLineLoadBuilder(b, w, r) }, func(b *LogLineBuilder, w []byte, r *TableReport) bool { return LogLineFromJson(b, w, r) }, func(v *LogLine) []byte {
		return fuzzImage(LogLineMeasure(v), func(b []byte) int64 { return LogLineSave(v, b) })
	}, func(v *LogLine) []byte {
		return fuzzImage(LogLineToJsonMeasure(v), func(b []byte) int64 { return LogLineToJson(v, b) })
	}),
	fuzzBuilder("Manifest", func(b *ManifestBuilder, w []byte, r *TableReport) bool { return ManifestLoadBuilder(b, w, r) }, func(b *ManifestBuilder, w []byte, r *TableReport) bool { return ManifestFromJson(b, w, r) }, func(v *Manifest) []byte {
		return fuzzImage(ManifestMeasure(v), func(b []byte) int64 { return ManifestSave(v, b) })
	}, func(v *Manifest) []byte {
		return fuzzImage(ManifestToJsonMeasure(v), func(b []byte) int64 { return ManifestToJson(v, b) })
	}),
	fuzzBuilder("MemberList", func(b *MemberListBuilder, w []byte, r *TableReport) bool { return MemberListLoadBuilder(b, w, r) }, func(b *MemberListBuilder, w []byte, r *TableReport) bool { return MemberListFromJson(b, w, r) }, func(v *MemberList) []byte {
		return fuzzImage(MemberListMeasure(v), func(b []byte) int64 { return MemberListSave(v, b) })
	}, func(v *MemberList) []byte {
		return fuzzImage(MemberListToJsonMeasure(v), func(b []byte) int64 { return MemberListToJson(v, b) })
	}),
	fuzzValue("Member", func(v *Member, w []byte, r *TableReport) bool { return MemberLoad(v, w, r) }, func(v *Member, w []byte, r *TableReport) bool { return MemberFromJson(v, w, r) }, func(v *Member) []byte {
		return fuzzImage(MemberMeasure(v), func(b []byte) int64 { return MemberSave(v, b) })
	}, func(v *Member) []byte {
		return fuzzImage(MemberToJsonMeasure(v), func(b []byte) int64 { return MemberToJson(v, b) })
	}),
	fuzzValue("Name128", func(v *Name128, w []byte, r *TableReport) bool { return Name128Load(v, w, r) }, func(v *Name128, w []byte, r *TableReport) bool { return Name128FromJson(v, w, r) }, func(v *Name128) []byte {
		return fuzzImage(Name128Measure(v), func(b []byte) int64 { return Name128Save(v, b) })
	}, func(v *Name128) []byte {
		return fuzzImage(Name128ToJsonMeasure(v), func(b []byte) int64 { return Name128ToJson(v, b) })
	}),
	fuzzValue("Name256", func(v *Name256, w []byte, r *TableReport) bool { return Name256Load(v, w, r) }, func(v *Name256, w []byte, r *TableReport) bool { return Name256FromJson(v, w, r) }, func(v *Name256) []byte {
		return fuzzImage(Name256Measure(v), func(b []byte) int64 { return Name256Save(v, b) })
	}, func(v *Name256) []byte {
		return fuzzImage(Name256ToJsonMeasure(v), func(b []byte) int64 { return Name256ToJson(v, b) })
	}),
	fuzzValue("Name400", func(v *Name400, w []byte, r *TableReport) bool { return Name400Load(v, w, r) }, func(v *Name400, w []byte, r *TableReport) bool { return Name400FromJson(v, w, r) }, func(v *Name400) []byte {
		return fuzzImage(Name400Measure(v), func(b []byte) int64 { return Name400Save(v, b) })
	}, func(v *Name400) []byte {
		return fuzzImage(Name400ToJsonMeasure(v), func(b []byte) int64 { return Name400ToJson(v, b) })
	}),
	fuzzBuilder("Note", func(b *NoteBuilder, w []byte, r *TableReport) bool { return NoteLoadBuilder(b, w, r) }, func(b *NoteBuilder, w []byte, r *TableReport) bool { return NoteFromJson(b, w, r) }, func(v *Note) []byte { return fuzzImage(NoteMeasure(v), func(b []byte) int64 { return NoteSave(v, b) }) }, func(v *Note) []byte {
		return fuzzImage(NoteToJsonMeasure(v), func(b []byte) int64 { return NoteToJson(v, b) })
	}),
	fuzzBuilder("OpRecord", func(b *OpRecordBuilder, w []byte, r *TableReport) bool { return OpRecordLoadBuilder(b, w, r) }, func(b *OpRecordBuilder, w []byte, r *TableReport) bool { return OpRecordFromJson(b, w, r) }, func(v *OpRecord) []byte {
		return fuzzImage(OpRecordMeasure(v), func(b []byte) int64 { return OpRecordSave(v, b) })
	}, func(v *OpRecord) []byte {
		return fuzzImage(OpRecordToJsonMeasure(v), func(b []byte) int64 { return OpRecordToJson(v, b) })
	}),
	fuzzValue("Pair", func(v *Pair, w []byte, r *TableReport) bool { return PairLoad(v, w, r) }, func(v *Pair, w []byte, r *TableReport) bool { return PairFromJson(v, w, r) }, func(v *Pair) []byte { return fuzzImage(PairMeasure(v), func(b []byte) int64 { return PairSave(v, b) }) }, func(v *Pair) []byte {
		return fuzzImage(PairToJsonMeasure(v), func(b []byte) int64 { return PairToJson(v, b) })
	}),
	fuzzValue("PropWrite", func(v *PropWrite, w []byte, r *TableReport) bool { return PropWriteLoad(v, w, r) }, func(v *PropWrite, w []byte, r *TableReport) bool { return PropWriteFromJson(v, w, r) }, func(v *PropWrite) []byte {
		return fuzzImage(PropWriteMeasure(v), func(b []byte) int64 { return PropWriteSave(v, b) })
	}, func(v *PropWrite) []byte {
		return fuzzImage(PropWriteToJsonMeasure(v), func(b []byte) int64 { return PropWriteToJson(v, b) })
	}),
	fuzzBuilder("QueuedChange", func(b *QueuedChangeBuilder, w []byte, r *TableReport) bool { return QueuedChangeLoadBuilder(b, w, r) }, func(b *QueuedChangeBuilder, w []byte, r *TableReport) bool { return QueuedChangeFromJson(b, w, r) }, func(v *QueuedChange) []byte {
		return fuzzImage(QueuedChangeMeasure(v), func(b []byte) int64 { return QueuedChangeSave(v, b) })
	}, func(v *QueuedChange) []byte {
		return fuzzImage(QueuedChangeToJsonMeasure(v), func(b []byte) int64 { return QueuedChangeToJson(v, b) })
	}),
	fuzzValue("Refusal", func(v *Refusal, w []byte, r *TableReport) bool { return RefusalLoad(v, w, r) }, func(v *Refusal, w []byte, r *TableReport) bool { return RefusalFromJson(v, w, r) }, func(v *Refusal) []byte {
		return fuzzImage(RefusalMeasure(v), func(b []byte) int64 { return RefusalSave(v, b) })
	}, func(v *Refusal) []byte {
		return fuzzImage(RefusalToJsonMeasure(v), func(b []byte) int64 { return RefusalToJson(v, b) })
	}),
	fuzzValue("RequestDigest", func(v *RequestDigest, w []byte, r *TableReport) bool { return RequestDigestLoad(v, w, r) }, func(v *RequestDigest, w []byte, r *TableReport) bool { return RequestDigestFromJson(v, w, r) }, func(v *RequestDigest) []byte {
		return fuzzImage(RequestDigestMeasure(v), func(b []byte) int64 { return RequestDigestSave(v, b) })
	}, func(v *RequestDigest) []byte {
		return fuzzImage(RequestDigestToJsonMeasure(v), func(b []byte) int64 { return RequestDigestToJson(v, b) })
	}),
	fuzzValue("SeatChange", func(v *SeatChange, w []byte, r *TableReport) bool { return SeatChangeLoad(v, w, r) }, func(v *SeatChange, w []byte, r *TableReport) bool { return SeatChangeFromJson(v, w, r) }, func(v *SeatChange) []byte {
		return fuzzImage(SeatChangeMeasure(v), func(b []byte) int64 { return SeatChangeSave(v, b) })
	}, func(v *SeatChange) []byte {
		return fuzzImage(SeatChangeToJsonMeasure(v), func(b []byte) int64 { return SeatChangeToJson(v, b) })
	}),
	fuzzBuilder("StepResult", func(b *StepResultBuilder, w []byte, r *TableReport) bool { return StepResultLoadBuilder(b, w, r) }, func(b *StepResultBuilder, w []byte, r *TableReport) bool { return StepResultFromJson(b, w, r) }, func(v *StepResult) []byte {
		return fuzzImage(StepResultMeasure(v), func(b []byte) int64 { return StepResultSave(v, b) })
	}, func(v *StepResult) []byte {
		return fuzzImage(StepResultToJsonMeasure(v), func(b []byte) int64 { return StepResultToJson(v, b) })
	}),
	fuzzValue("StreamTransition", func(v *StreamTransition, w []byte, r *TableReport) bool { return StreamTransitionLoad(v, w, r) }, func(v *StreamTransition, w []byte, r *TableReport) bool { return StreamTransitionFromJson(v, w, r) }, func(v *StreamTransition) []byte {
		return fuzzImage(StreamTransitionMeasure(v), func(b []byte) int64 { return StreamTransitionSave(v, b) })
	}, func(v *StreamTransition) []byte {
		return fuzzImage(StreamTransitionToJsonMeasure(v), func(b []byte) int64 { return StreamTransitionToJson(v, b) })
	}),
	fuzzValue("TableCount", func(v *TableCount, w []byte, r *TableReport) bool { return TableCountLoad(v, w, r) }, func(v *TableCount, w []byte, r *TableReport) bool { return TableCountFromJson(v, w, r) }, func(v *TableCount) []byte {
		return fuzzImage(TableCountMeasure(v), func(b []byte) int64 { return TableCountSave(v, b) })
	}, func(v *TableCount) []byte {
		return fuzzImage(TableCountToJsonMeasure(v), func(b []byte) int64 { return TableCountToJson(v, b) })
	}),
	fuzzValue("Text512", func(v *Text512, w []byte, r *TableReport) bool { return Text512Load(v, w, r) }, func(v *Text512, w []byte, r *TableReport) bool { return Text512FromJson(v, w, r) }, func(v *Text512) []byte {
		return fuzzImage(Text512Measure(v), func(b []byte) int64 { return Text512Save(v, b) })
	}, func(v *Text512) []byte {
		return fuzzImage(Text512ToJsonMeasure(v), func(b []byte) int64 { return Text512ToJson(v, b) })
	}),
	fuzzBuilder("Timers", func(b *TimersBuilder, w []byte, r *TableReport) bool { return TimersLoadBuilder(b, w, r) }, func(b *TimersBuilder, w []byte, r *TableReport) bool { return TimersFromJson(b, w, r) }, func(v *Timers) []byte {
		return fuzzImage(TimersMeasure(v), func(b []byte) int64 { return TimersSave(v, b) })
	}, func(v *Timers) []byte {
		return fuzzImage(TimersToJsonMeasure(v), func(b []byte) int64 { return TimersToJson(v, b) })
	}),
	fuzzValue("Timer", func(v *Timer, w []byte, r *TableReport) bool { return TimerLoad(v, w, r) }, func(v *Timer, w []byte, r *TableReport) bool { return TimerFromJson(v, w, r) }, func(v *Timer) []byte {
		return fuzzImage(TimerMeasure(v), func(b []byte) int64 { return TimerSave(v, b) })
	}, func(v *Timer) []byte {
		return fuzzImage(TimerToJsonMeasure(v), func(b []byte) int64 { return TimerToJson(v, b) })
	}),
}

var fuzzSeeds = map[string]bool{}

func fuzzImages(t testing.TB, globs ...string) (out [][]byte) {
	for _, glob := range globs {
		files, _ := filepath.Glob(glob)
		for _, file := range files {
			b, err := os.ReadFile(file)
			require.NoError(t, err)
			if ext := filepath.Ext(file); ext == ".bin" || ext == ".b64" || ext == ".json" {
				out = append(out, b)
			}
		}
	}
	return
}

// fuzzSeedList is every seed as (selector, bytes): per root the reset value in
// three forms, and every image against every root as wire and base64. The
// schema corpus is this build's own; the others only read or are refused.
func fuzzSeedList(t testing.TB) (sels []uint8, ins [][]byte) {
	add := func(i int, in []byte) { sels, ins = append(sels, uint8(i)), append(ins, in) }
	corpus, others := fuzzImages(t, filepath.Join("..", "..", "..", "..", "schemas", "oprows", "fuzz", "*")), fuzzImages(t)
	for i, root := range fuzzRoots {
		var rep TableReport
		if wire, text, _ := root.run(fuzzEmpty, nil, &rep); wire != nil && text != nil {
			fuzzSeeds[string(wire)] = true
			add(i, wire)
			add(i, fuzzB64(wire))
			add(i, text)
		}
		for n, img := range append(append([][]byte{}, corpus...), others...) {
			fuzzSeeds[string(img)] = fuzzSeeds[string(img)] || n < len(corpus)
			add(i, img)
			add(i, fuzzB64(img))
		}
	}
	return
}

func FuzzOprowswire(f *testing.F) {
	fuzzSeedList(f) // fills fuzzSeeds; the seeds themselves are the checked-in files
	f.Fuzz(func(t *testing.T, sel uint8, in []byte) {
		root := fuzzRoots[int(sel)%len(fuzzRoots)]
		for form := fuzzWire; form <= fuzzJSON; form++ {
			wire, text, rep, err := root.load(form, in)
			if err != nil {
				require.True(t, strings.HasPrefix(fmt.Sprintf("%T", err), "oprowswire.Table"), "%s as %d %q: a refusal with no name: %T %v", root.name, form, in, err, err)
				continue
			}
			require.NotNil(t, wire, "%s as %d %q: an accepted value does not re-encode", root.name, form, in)
			require.NotNil(t, text, "%s as %d %q: an accepted value does not re-encode", root.name, form, in)
			for _, again := range []struct {
				form fuzzForm
				in   []byte
			}{{fuzzWire, wire}, {fuzzBase64, fuzzB64(wire)}, {fuzzJSON, text}} {
				w, tx, _, err := root.load(again.form, again.in)
				require.NoError(t, err, "%s as %d %q: the %d image", root.name, form, in, again.form)
				require.Equal(t, wire, w, "%s as %d %q: the %d image does not read back to itself", root.name, form, in, again.form)
				require.Equal(t, text, tx, "%s as %d %q: the %d image does not read back to itself", root.name, form, in, again.form)
			}
			clean := rep.Unknown == 0 && rep.Widened == 0 && rep.Retained == 0 && rep.Clamped == 0 && rep.KindMismatch == 0 && rep.Duplicate == 0
			if form == fuzzWire && fuzzSeeds[string(in)] && clean {
				require.Equal(t, in, wire, "%s: an accepted seed wire does not reproduce", root.name)
			}
		}
	})
}

// TestWriteFuzzOprowswireSeeds writes testdata/fuzz/FuzzOprowswire when NOVA_FUZZ_WRITE_SEEDS=1.
func TestWriteFuzzOprowswireSeeds(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_FUZZ_WRITE_SEEDS") != "1" {
		t.Skip("set NOVA_FUZZ_WRITE_SEEDS=1 to write testdata/fuzz/FuzzOprowswire")
	}
	dir := filepath.Join("testdata", "fuzz", "FuzzOprowswire")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	sels, ins := fuzzSeedList(t)
	for i := range ins {
		body := fmt.Sprintf("go test fuzz v1\nbyte('\\x%02x')\n[]byte(%s)\n", sels[i], strconv.Quote(string(ins[i])))
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("seed-%04d", i)), []byte(body), 0o644))
	}
}
