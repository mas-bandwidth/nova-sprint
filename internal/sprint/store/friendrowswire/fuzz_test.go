package friendrowswire

// FuzzFriendrowswire feeds hostile bytes, in three forms (binary wire, padded standard
// base64, JSON text), to every generated root. go test runs the checked-in seeds only;
// nightly: go test -run='^$' -fuzz='^FuzzFriendrowswire$' -fuzztime=5m ./internal/sprint/store/friendrowswire
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
	fuzzBuilder("Beat", func(b *BeatBuilder, w []byte, r *TableReport) bool { return BeatLoadBuilder(b, w, r) }, func(b *BeatBuilder, w []byte, r *TableReport) bool { return BeatFromJson(b, w, r) }, func(v *Beat) []byte { return fuzzImage(BeatMeasure(v), func(b []byte) int64 { return BeatSave(v, b) }) }, func(v *Beat) []byte {
		return fuzzImage(BeatToJsonMeasure(v), func(b []byte) int64 { return BeatToJson(v, b) })
	}),
	fuzzBuilder("FactWord", func(b *FactWordBuilder, w []byte, r *TableReport) bool { return FactWordLoadBuilder(b, w, r) }, func(b *FactWordBuilder, w []byte, r *TableReport) bool { return FactWordFromJson(b, w, r) }, func(v *FactWord) []byte {
		return fuzzImage(FactWordMeasure(v), func(b []byte) int64 { return FactWordSave(v, b) })
	}, func(v *FactWord) []byte {
		return fuzzImage(FactWordToJsonMeasure(v), func(b []byte) int64 { return FactWordToJson(v, b) })
	}),
	fuzzBuilder("Files", func(b *FilesBuilder, w []byte, r *TableReport) bool { return FilesLoadBuilder(b, w, r) }, func(b *FilesBuilder, w []byte, r *TableReport) bool { return FilesFromJson(b, w, r) }, func(v *Files) []byte {
		return fuzzImage(FilesMeasure(v), func(b []byte) int64 { return FilesSave(v, b) })
	}, func(v *Files) []byte {
		return fuzzImage(FilesToJsonMeasure(v), func(b []byte) int64 { return FilesToJson(v, b) })
	}),
	fuzzBuilder("FriendEntry", func(b *FriendEntryBuilder, w []byte, r *TableReport) bool { return FriendEntryLoadBuilder(b, w, r) }, func(b *FriendEntryBuilder, w []byte, r *TableReport) bool { return FriendEntryFromJson(b, w, r) }, func(v *FriendEntry) []byte {
		return fuzzImage(FriendEntryMeasure(v), func(b []byte) int64 { return FriendEntrySave(v, b) })
	}, func(v *FriendEntry) []byte {
		return fuzzImage(FriendEntryToJsonMeasure(v), func(b []byte) int64 { return FriendEntryToJson(v, b) })
	}),
	fuzzBuilder("FriendReport", func(b *FriendReportBuilder, w []byte, r *TableReport) bool { return FriendReportLoadBuilder(b, w, r) }, func(b *FriendReportBuilder, w []byte, r *TableReport) bool { return FriendReportFromJson(b, w, r) }, func(v *FriendReport) []byte {
		return fuzzImage(FriendReportMeasure(v), func(b []byte) int64 { return FriendReportSave(v, b) })
	}, func(v *FriendReport) []byte {
		return fuzzImage(FriendReportToJsonMeasure(v), func(b []byte) int64 { return FriendReportToJson(v, b) })
	}),
	fuzzBuilder("FriendStatus", func(b *FriendStatusBuilder, w []byte, r *TableReport) bool { return FriendStatusLoadBuilder(b, w, r) }, func(b *FriendStatusBuilder, w []byte, r *TableReport) bool { return FriendStatusFromJson(b, w, r) }, func(v *FriendStatus) []byte {
		return fuzzImage(FriendStatusMeasure(v), func(b []byte) int64 { return FriendStatusSave(v, b) })
	}, func(v *FriendStatus) []byte {
		return fuzzImage(FriendStatusToJsonMeasure(v), func(b []byte) int64 { return FriendStatusToJson(v, b) })
	}),
	fuzzBuilder("Holder", func(b *HolderBuilder, w []byte, r *TableReport) bool { return HolderLoadBuilder(b, w, r) }, func(b *HolderBuilder, w []byte, r *TableReport) bool { return HolderFromJson(b, w, r) }, func(v *Holder) []byte {
		return fuzzImage(HolderMeasure(v), func(b []byte) int64 { return HolderSave(v, b) })
	}, func(v *Holder) []byte {
		return fuzzImage(HolderToJsonMeasure(v), func(b []byte) int64 { return HolderToJson(v, b) })
	}),
	fuzzBuilder("HoldView", func(b *HoldViewBuilder, w []byte, r *TableReport) bool { return HoldViewLoadBuilder(b, w, r) }, func(b *HoldViewBuilder, w []byte, r *TableReport) bool { return HoldViewFromJson(b, w, r) }, func(v *HoldView) []byte {
		return fuzzImage(HoldViewMeasure(v), func(b []byte) int64 { return HoldViewSave(v, b) })
	}, func(v *HoldView) []byte {
		return fuzzImage(HoldViewToJsonMeasure(v), func(b []byte) int64 { return HoldViewToJson(v, b) })
	}),
	fuzzBuilder("LaneHold", func(b *LaneHoldBuilder, w []byte, r *TableReport) bool { return LaneHoldLoadBuilder(b, w, r) }, func(b *LaneHoldBuilder, w []byte, r *TableReport) bool { return LaneHoldFromJson(b, w, r) }, func(v *LaneHold) []byte {
		return fuzzImage(LaneHoldMeasure(v), func(b []byte) int64 { return LaneHoldSave(v, b) })
	}, func(v *LaneHold) []byte {
		return fuzzImage(LaneHoldToJsonMeasure(v), func(b []byte) int64 { return LaneHoldToJson(v, b) })
	}),
	fuzzBuilder("Lane", func(b *LaneBuilder, w []byte, r *TableReport) bool { return LaneLoadBuilder(b, w, r) }, func(b *LaneBuilder, w []byte, r *TableReport) bool { return LaneFromJson(b, w, r) }, func(v *Lane) []byte { return fuzzImage(LaneMeasure(v), func(b []byte) int64 { return LaneSave(v, b) }) }, func(v *Lane) []byte {
		return fuzzImage(LaneToJsonMeasure(v), func(b []byte) int64 { return LaneToJson(v, b) })
	}),
	fuzzBuilder("LaneWait", func(b *LaneWaitBuilder, w []byte, r *TableReport) bool { return LaneWaitLoadBuilder(b, w, r) }, func(b *LaneWaitBuilder, w []byte, r *TableReport) bool { return LaneWaitFromJson(b, w, r) }, func(v *LaneWait) []byte {
		return fuzzImage(LaneWaitMeasure(v), func(b []byte) int64 { return LaneWaitSave(v, b) })
	}, func(v *LaneWait) []byte {
		return fuzzImage(LaneWaitToJsonMeasure(v), func(b []byte) int64 { return LaneWaitToJson(v, b) })
	}),
	fuzzBuilder("LiveSight", func(b *LiveSightBuilder, w []byte, r *TableReport) bool { return LiveSightLoadBuilder(b, w, r) }, func(b *LiveSightBuilder, w []byte, r *TableReport) bool { return LiveSightFromJson(b, w, r) }, func(v *LiveSight) []byte {
		return fuzzImage(LiveSightMeasure(v), func(b []byte) int64 { return LiveSightSave(v, b) })
	}, func(v *LiveSight) []byte {
		return fuzzImage(LiveSightToJsonMeasure(v), func(b []byte) int64 { return LiveSightToJson(v, b) })
	}),
	fuzzBuilder("LiveWord", func(b *LiveWordBuilder, w []byte, r *TableReport) bool { return LiveWordLoadBuilder(b, w, r) }, func(b *LiveWordBuilder, w []byte, r *TableReport) bool { return LiveWordFromJson(b, w, r) }, func(v *LiveWord) []byte {
		return fuzzImage(LiveWordMeasure(v), func(b []byte) int64 { return LiveWordSave(v, b) })
	}, func(v *LiveWord) []byte {
		return fuzzImage(LiveWordToJsonMeasure(v), func(b []byte) int64 { return LiveWordToJson(v, b) })
	}),
	fuzzBuilder("LoadSample", func(b *LoadSampleBuilder, w []byte, r *TableReport) bool { return LoadSampleLoadBuilder(b, w, r) }, func(b *LoadSampleBuilder, w []byte, r *TableReport) bool { return LoadSampleFromJson(b, w, r) }, func(v *LoadSample) []byte {
		return fuzzImage(LoadSampleMeasure(v), func(b []byte) int64 { return LoadSampleSave(v, b) })
	}, func(v *LoadSample) []byte {
		return fuzzImage(LoadSampleToJsonMeasure(v), func(b []byte) int64 { return LoadSampleToJson(v, b) })
	}),
	fuzzBuilder("Login", func(b *LoginBuilder, w []byte, r *TableReport) bool { return LoginLoadBuilder(b, w, r) }, func(b *LoginBuilder, w []byte, r *TableReport) bool { return LoginFromJson(b, w, r) }, func(v *Login) []byte {
		return fuzzImage(LoginMeasure(v), func(b []byte) int64 { return LoginSave(v, b) })
	}, func(v *Login) []byte {
		return fuzzImage(LoginToJsonMeasure(v), func(b []byte) int64 { return LoginToJson(v, b) })
	}),
	fuzzBuilder("Meter", func(b *MeterBuilder, w []byte, r *TableReport) bool { return MeterLoadBuilder(b, w, r) }, func(b *MeterBuilder, w []byte, r *TableReport) bool { return MeterFromJson(b, w, r) }, func(v *Meter) []byte {
		return fuzzImage(MeterMeasure(v), func(b []byte) int64 { return MeterSave(v, b) })
	}, func(v *Meter) []byte {
		return fuzzImage(MeterToJsonMeasure(v), func(b []byte) int64 { return MeterToJson(v, b) })
	}),
	fuzzBuilder("ReaderHold", func(b *ReaderHoldBuilder, w []byte, r *TableReport) bool { return ReaderHoldLoadBuilder(b, w, r) }, func(b *ReaderHoldBuilder, w []byte, r *TableReport) bool { return ReaderHoldFromJson(b, w, r) }, func(v *ReaderHold) []byte {
		return fuzzImage(ReaderHoldMeasure(v), func(b []byte) int64 { return ReaderHoldSave(v, b) })
	}, func(v *ReaderHold) []byte {
		return fuzzImage(ReaderHoldToJsonMeasure(v), func(b []byte) int64 { return ReaderHoldToJson(v, b) })
	}),
	fuzzBuilder("RunWord", func(b *RunWordBuilder, w []byte, r *TableReport) bool { return RunWordLoadBuilder(b, w, r) }, func(b *RunWordBuilder, w []byte, r *TableReport) bool { return RunWordFromJson(b, w, r) }, func(v *RunWord) []byte {
		return fuzzImage(RunWordMeasure(v), func(b []byte) int64 { return RunWordSave(v, b) })
	}, func(v *RunWord) []byte {
		return fuzzImage(RunWordToJsonMeasure(v), func(b []byte) int64 { return RunWordToJson(v, b) })
	}),
	fuzzBuilder("SeatChange", func(b *SeatChangeBuilder, w []byte, r *TableReport) bool { return SeatChangeLoadBuilder(b, w, r) }, func(b *SeatChangeBuilder, w []byte, r *TableReport) bool { return SeatChangeFromJson(b, w, r) }, func(v *SeatChange) []byte {
		return fuzzImage(SeatChangeMeasure(v), func(b []byte) int64 { return SeatChangeSave(v, b) })
	}, func(v *SeatChange) []byte {
		return fuzzImage(SeatChangeToJsonMeasure(v), func(b []byte) int64 { return SeatChangeToJson(v, b) })
	}),
	fuzzBuilder("ServerRecord", func(b *ServerRecordBuilder, w []byte, r *TableReport) bool { return ServerRecordLoadBuilder(b, w, r) }, func(b *ServerRecordBuilder, w []byte, r *TableReport) bool { return ServerRecordFromJson(b, w, r) }, func(v *ServerRecord) []byte {
		return fuzzImage(ServerRecordMeasure(v), func(b []byte) int64 { return ServerRecordSave(v, b) })
	}, func(v *ServerRecord) []byte {
		return fuzzImage(ServerRecordToJsonMeasure(v), func(b []byte) int64 { return ServerRecordToJson(v, b) })
	}),
	fuzzValue("Ticks", func(v *Ticks, w []byte, r *TableReport) bool { return TicksLoad(v, w, r) }, func(v *Ticks, w []byte, r *TableReport) bool { return TicksFromJson(v, w, r) }, func(v *Ticks) []byte {
		return fuzzImage(TicksMeasure(v), func(b []byte) int64 { return TicksSave(v, b) })
	}, func(v *Ticks) []byte {
		return fuzzImage(TicksToJsonMeasure(v), func(b []byte) int64 { return TicksToJson(v, b) })
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
	corpus, others := fuzzImages(t, filepath.Join("..", "..", "..", "..", "schemas", "friendrows", "fuzz", "*")), fuzzImages(t)
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

func FuzzFriendrowswire(f *testing.F) {
	fuzzSeedList(f) // fills fuzzSeeds; the seeds themselves are the checked-in files
	f.Fuzz(func(t *testing.T, sel uint8, in []byte) {
		root := fuzzRoots[int(sel)%len(fuzzRoots)]
		for form := fuzzWire; form <= fuzzJSON; form++ {
			wire, text, rep, err := root.load(form, in)
			if err != nil {
				require.True(t, strings.HasPrefix(fmt.Sprintf("%T", err), "friendrowswire.Table"), "%s as %d %q: a refusal with no name: %T %v", root.name, form, in, err, err)
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

// TestWriteFuzzFriendrowswireSeeds writes testdata/fuzz/FuzzFriendrowswire when NOVA_FUZZ_WRITE_SEEDS=1.
func TestWriteFuzzFriendrowswireSeeds(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_FUZZ_WRITE_SEEDS") != "1" {
		t.Skip("set NOVA_FUZZ_WRITE_SEEDS=1 to write testdata/fuzz/FuzzFriendrowswire")
	}
	dir := filepath.Join("testdata", "fuzz", "FuzzFriendrowswire")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	sels, ins := fuzzSeedList(t)
	for i := range ins {
		body := fmt.Sprintf("go test fuzz v1\nbyte('\\x%02x')\n[]byte(%s)\n", sels[i], strconv.Quote(string(ins[i])))
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("seed-%04d", i)), []byte(body), 0o644))
	}
}
