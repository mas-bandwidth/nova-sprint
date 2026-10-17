package viewrowswire

// FuzzViewrowswire feeds hostile bytes, in three forms (binary wire, padded standard
// base64, JSON text), to every generated root. go test runs the checked-in seeds only;
// nightly: go test -run='^$' -fuzz='^FuzzViewrowswire$' -fuzztime=5m ./internal/sprint/store/viewrowswire
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
	fuzzBuilder("ArchivedFleetCard", func(b *ArchivedFleetCardBuilder, w []byte, r *TableReport) bool {
		return ArchivedFleetCardLoadBuilder(b, w, r)
	}, func(b *ArchivedFleetCardBuilder, w []byte, r *TableReport) bool {
		return ArchivedFleetCardFromJson(b, w, r)
	}, func(v *ArchivedFleetCard) []byte {
		return fuzzImage(ArchivedFleetCardMeasure(v), func(b []byte) int64 { return ArchivedFleetCardSave(v, b) })
	}, func(v *ArchivedFleetCard) []byte {
		return fuzzImage(ArchivedFleetCardToJsonMeasure(v), func(b []byte) int64 { return ArchivedFleetCardToJson(v, b) })
	}),
	fuzzBuilder("BatchManifest", func(b *BatchManifestBuilder, w []byte, r *TableReport) bool { return BatchManifestLoadBuilder(b, w, r) }, func(b *BatchManifestBuilder, w []byte, r *TableReport) bool { return BatchManifestFromJson(b, w, r) }, func(v *BatchManifest) []byte {
		return fuzzImage(BatchManifestMeasure(v), func(b []byte) int64 { return BatchManifestSave(v, b) })
	}, func(v *BatchManifest) []byte {
		return fuzzImage(BatchManifestToJsonMeasure(v), func(b []byte) int64 { return BatchManifestToJson(v, b) })
	}),
	fuzzBuilder("BatchMemberEntry", func(b *BatchMemberEntryBuilder, w []byte, r *TableReport) bool {
		return BatchMemberEntryLoadBuilder(b, w, r)
	}, func(b *BatchMemberEntryBuilder, w []byte, r *TableReport) bool {
		return BatchMemberEntryFromJson(b, w, r)
	}, func(v *BatchMemberEntry) []byte {
		return fuzzImage(BatchMemberEntryMeasure(v), func(b []byte) int64 { return BatchMemberEntrySave(v, b) })
	}, func(v *BatchMemberEntry) []byte {
		return fuzzImage(BatchMemberEntryToJsonMeasure(v), func(b []byte) int64 { return BatchMemberEntryToJson(v, b) })
	}),
	fuzzBuilder("Beat", func(b *BeatBuilder, w []byte, r *TableReport) bool { return BeatLoadBuilder(b, w, r) }, func(b *BeatBuilder, w []byte, r *TableReport) bool { return BeatFromJson(b, w, r) }, func(v *Beat) []byte { return fuzzImage(BeatMeasure(v), func(b []byte) int64 { return BeatSave(v, b) }) }, func(v *Beat) []byte {
		return fuzzImage(BeatToJsonMeasure(v), func(b []byte) int64 { return BeatToJson(v, b) })
	}),
	fuzzBuilder("CardID", func(b *CardIDBuilder, w []byte, r *TableReport) bool { return CardIDLoadBuilder(b, w, r) }, func(b *CardIDBuilder, w []byte, r *TableReport) bool { return CardIDFromJson(b, w, r) }, func(v *CardID) []byte {
		return fuzzImage(CardIDMeasure(v), func(b []byte) int64 { return CardIDSave(v, b) })
	}, func(v *CardID) []byte {
		return fuzzImage(CardIDToJsonMeasure(v), func(b []byte) int64 { return CardIDToJson(v, b) })
	}),
	fuzzBuilder("CheckedCard", func(b *CheckedCardBuilder, w []byte, r *TableReport) bool { return CheckedCardLoadBuilder(b, w, r) }, func(b *CheckedCardBuilder, w []byte, r *TableReport) bool { return CheckedCardFromJson(b, w, r) }, func(v *CheckedCard) []byte {
		return fuzzImage(CheckedCardMeasure(v), func(b []byte) int64 { return CheckedCardSave(v, b) })
	}, func(v *CheckedCard) []byte {
		return fuzzImage(CheckedCardToJsonMeasure(v), func(b []byte) int64 { return CheckedCardToJson(v, b) })
	}),
	fuzzBuilder("CheckRecord", func(b *CheckRecordBuilder, w []byte, r *TableReport) bool { return CheckRecordLoadBuilder(b, w, r) }, func(b *CheckRecordBuilder, w []byte, r *TableReport) bool { return CheckRecordFromJson(b, w, r) }, func(v *CheckRecord) []byte {
		return fuzzImage(CheckRecordMeasure(v), func(b []byte) int64 { return CheckRecordSave(v, b) })
	}, func(v *CheckRecord) []byte {
		return fuzzImage(CheckRecordToJsonMeasure(v), func(b []byte) int64 { return CheckRecordToJson(v, b) })
	}),
	fuzzBuilder("CoordinatorDelivery", func(b *CoordinatorDeliveryBuilder, w []byte, r *TableReport) bool {
		return CoordinatorDeliveryLoadBuilder(b, w, r)
	}, func(b *CoordinatorDeliveryBuilder, w []byte, r *TableReport) bool {
		return CoordinatorDeliveryFromJson(b, w, r)
	}, func(v *CoordinatorDelivery) []byte {
		return fuzzImage(CoordinatorDeliveryMeasure(v), func(b []byte) int64 { return CoordinatorDeliverySave(v, b) })
	}, func(v *CoordinatorDelivery) []byte {
		return fuzzImage(CoordinatorDeliveryToJsonMeasure(v), func(b []byte) int64 { return CoordinatorDeliveryToJson(v, b) })
	}),
	fuzzBuilder("CostCheck", func(b *CostCheckBuilder, w []byte, r *TableReport) bool { return CostCheckLoadBuilder(b, w, r) }, func(b *CostCheckBuilder, w []byte, r *TableReport) bool { return CostCheckFromJson(b, w, r) }, func(v *CostCheck) []byte {
		return fuzzImage(CostCheckMeasure(v), func(b []byte) int64 { return CostCheckSave(v, b) })
	}, func(v *CostCheck) []byte {
		return fuzzImage(CostCheckToJsonMeasure(v), func(b []byte) int64 { return CostCheckToJson(v, b) })
	}),
	fuzzBuilder("CostReconcileRecord", func(b *CostReconcileRecordBuilder, w []byte, r *TableReport) bool {
		return CostReconcileRecordLoadBuilder(b, w, r)
	}, func(b *CostReconcileRecordBuilder, w []byte, r *TableReport) bool {
		return CostReconcileRecordFromJson(b, w, r)
	}, func(v *CostReconcileRecord) []byte {
		return fuzzImage(CostReconcileRecordMeasure(v), func(b []byte) int64 { return CostReconcileRecordSave(v, b) })
	}, func(v *CostReconcileRecord) []byte {
		return fuzzImage(CostReconcileRecordToJsonMeasure(v), func(b []byte) int64 { return CostReconcileRecordToJson(v, b) })
	}),
	fuzzBuilder("CriticalCard", func(b *CriticalCardBuilder, w []byte, r *TableReport) bool { return CriticalCardLoadBuilder(b, w, r) }, func(b *CriticalCardBuilder, w []byte, r *TableReport) bool { return CriticalCardFromJson(b, w, r) }, func(v *CriticalCard) []byte {
		return fuzzImage(CriticalCardMeasure(v), func(b []byte) int64 { return CriticalCardSave(v, b) })
	}, func(v *CriticalCard) []byte {
		return fuzzImage(CriticalCardToJsonMeasure(v), func(b []byte) int64 { return CriticalCardToJson(v, b) })
	}),
	fuzzValue("DayGap", func(v *DayGap, w []byte, r *TableReport) bool { return DayGapLoad(v, w, r) }, func(v *DayGap, w []byte, r *TableReport) bool { return DayGapFromJson(v, w, r) }, func(v *DayGap) []byte {
		return fuzzImage(DayGapMeasure(v), func(b []byte) int64 { return DayGapSave(v, b) })
	}, func(v *DayGap) []byte {
		return fuzzImage(DayGapToJsonMeasure(v), func(b []byte) int64 { return DayGapToJson(v, b) })
	}),
	fuzzBuilder("DayGapPair", func(b *DayGapPairBuilder, w []byte, r *TableReport) bool { return DayGapPairLoadBuilder(b, w, r) }, func(b *DayGapPairBuilder, w []byte, r *TableReport) bool { return DayGapPairFromJson(b, w, r) }, func(v *DayGapPair) []byte {
		return fuzzImage(DayGapPairMeasure(v), func(b []byte) int64 { return DayGapPairSave(v, b) })
	}, func(v *DayGapPair) []byte {
		return fuzzImage(DayGapPairToJsonMeasure(v), func(b []byte) int64 { return DayGapPairToJson(v, b) })
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
	fuzzBuilder("FieldGuard", func(b *FieldGuardBuilder, w []byte, r *TableReport) bool { return FieldGuardLoadBuilder(b, w, r) }, func(b *FieldGuardBuilder, w []byte, r *TableReport) bool { return FieldGuardFromJson(b, w, r) }, func(v *FieldGuard) []byte {
		return fuzzImage(FieldGuardMeasure(v), func(b []byte) int64 { return FieldGuardSave(v, b) })
	}, func(v *FieldGuard) []byte {
		return fuzzImage(FieldGuardToJsonMeasure(v), func(b []byte) int64 { return FieldGuardToJson(v, b) })
	}),
	fuzzBuilder("FieldGuardPair", func(b *FieldGuardPairBuilder, w []byte, r *TableReport) bool {
		return FieldGuardPairLoadBuilder(b, w, r)
	}, func(b *FieldGuardPairBuilder, w []byte, r *TableReport) bool { return FieldGuardPairFromJson(b, w, r) }, func(v *FieldGuardPair) []byte {
		return fuzzImage(FieldGuardPairMeasure(v), func(b []byte) int64 { return FieldGuardPairSave(v, b) })
	}, func(v *FieldGuardPair) []byte {
		return fuzzImage(FieldGuardPairToJsonMeasure(v), func(b []byte) int64 { return FieldGuardPairToJson(v, b) })
	}),
	fuzzBuilder("FleetDropDebt", func(b *FleetDropDebtBuilder, w []byte, r *TableReport) bool { return FleetDropDebtLoadBuilder(b, w, r) }, func(b *FleetDropDebtBuilder, w []byte, r *TableReport) bool { return FleetDropDebtFromJson(b, w, r) }, func(v *FleetDropDebt) []byte {
		return fuzzImage(FleetDropDebtMeasure(v), func(b []byte) int64 { return FleetDropDebtSave(v, b) })
	}, func(v *FleetDropDebt) []byte {
		return fuzzImage(FleetDropDebtToJsonMeasure(v), func(b []byte) int64 { return FleetDropDebtToJson(v, b) })
	}),
	fuzzBuilder("FleetMachineRow", func(b *FleetMachineRowBuilder, w []byte, r *TableReport) bool {
		return FleetMachineRowLoadBuilder(b, w, r)
	}, func(b *FleetMachineRowBuilder, w []byte, r *TableReport) bool {
		return FleetMachineRowFromJson(b, w, r)
	}, func(v *FleetMachineRow) []byte {
		return fuzzImage(FleetMachineRowMeasure(v), func(b []byte) int64 { return FleetMachineRowSave(v, b) })
	}, func(v *FleetMachineRow) []byte {
		return fuzzImage(FleetMachineRowToJsonMeasure(v), func(b []byte) int64 { return FleetMachineRowToJson(v, b) })
	}),
	fuzzBuilder("FriendReport", func(b *FriendReportBuilder, w []byte, r *TableReport) bool { return FriendReportLoadBuilder(b, w, r) }, func(b *FriendReportBuilder, w []byte, r *TableReport) bool { return FriendReportFromJson(b, w, r) }, func(v *FriendReport) []byte {
		return fuzzImage(FriendReportMeasure(v), func(b []byte) int64 { return FriendReportSave(v, b) })
	}, func(v *FriendReport) []byte {
		return fuzzImage(FriendReportToJsonMeasure(v), func(b []byte) int64 { return FriendReportToJson(v, b) })
	}),
	fuzzBuilder("Heartbeat", func(b *HeartbeatBuilder, w []byte, r *TableReport) bool { return HeartbeatLoadBuilder(b, w, r) }, func(b *HeartbeatBuilder, w []byte, r *TableReport) bool { return HeartbeatFromJson(b, w, r) }, func(v *Heartbeat) []byte {
		return fuzzImage(HeartbeatMeasure(v), func(b []byte) int64 { return HeartbeatSave(v, b) })
	}, func(v *Heartbeat) []byte {
		return fuzzImage(HeartbeatToJsonMeasure(v), func(b []byte) int64 { return HeartbeatToJson(v, b) })
	}),
	fuzzBuilder("HeldRecord", func(b *HeldRecordBuilder, w []byte, r *TableReport) bool { return HeldRecordLoadBuilder(b, w, r) }, func(b *HeldRecordBuilder, w []byte, r *TableReport) bool { return HeldRecordFromJson(b, w, r) }, func(v *HeldRecord) []byte {
		return fuzzImage(HeldRecordMeasure(v), func(b []byte) int64 { return HeldRecordSave(v, b) })
	}, func(v *HeldRecord) []byte {
		return fuzzImage(HeldRecordToJsonMeasure(v), func(b []byte) int64 { return HeldRecordToJson(v, b) })
	}),
	fuzzBuilder("IntList", func(b *IntListBuilder, w []byte, r *TableReport) bool { return IntListLoadBuilder(b, w, r) }, func(b *IntListBuilder, w []byte, r *TableReport) bool { return IntListFromJson(b, w, r) }, func(v *IntList) []byte {
		return fuzzImage(IntListMeasure(v), func(b []byte) int64 { return IntListSave(v, b) })
	}, func(v *IntList) []byte {
		return fuzzImage(IntListToJsonMeasure(v), func(b []byte) int64 { return IntListToJson(v, b) })
	}),
	fuzzBuilder("IntListPair", func(b *IntListPairBuilder, w []byte, r *TableReport) bool { return IntListPairLoadBuilder(b, w, r) }, func(b *IntListPairBuilder, w []byte, r *TableReport) bool { return IntListPairFromJson(b, w, r) }, func(v *IntListPair) []byte {
		return fuzzImage(IntListPairMeasure(v), func(b []byte) int64 { return IntListPairSave(v, b) })
	}, func(v *IntListPair) []byte {
		return fuzzImage(IntListPairToJsonMeasure(v), func(b []byte) int64 { return IntListPairToJson(v, b) })
	}),
	fuzzBuilder("IntPair", func(b *IntPairBuilder, w []byte, r *TableReport) bool { return IntPairLoadBuilder(b, w, r) }, func(b *IntPairBuilder, w []byte, r *TableReport) bool { return IntPairFromJson(b, w, r) }, func(v *IntPair) []byte {
		return fuzzImage(IntPairMeasure(v), func(b []byte) int64 { return IntPairSave(v, b) })
	}, func(v *IntPair) []byte {
		return fuzzImage(IntPairToJsonMeasure(v), func(b []byte) int64 { return IntPairToJson(v, b) })
	}),
	fuzzBuilder("KindTime", func(b *KindTimeBuilder, w []byte, r *TableReport) bool { return KindTimeLoadBuilder(b, w, r) }, func(b *KindTimeBuilder, w []byte, r *TableReport) bool { return KindTimeFromJson(b, w, r) }, func(v *KindTime) []byte {
		return fuzzImage(KindTimeMeasure(v), func(b []byte) int64 { return KindTimeSave(v, b) })
	}, func(v *KindTime) []byte {
		return fuzzImage(KindTimeToJsonMeasure(v), func(b []byte) int64 { return KindTimeToJson(v, b) })
	}),
	fuzzBuilder("Landing", func(b *LandingBuilder, w []byte, r *TableReport) bool { return LandingLoadBuilder(b, w, r) }, func(b *LandingBuilder, w []byte, r *TableReport) bool { return LandingFromJson(b, w, r) }, func(v *Landing) []byte {
		return fuzzImage(LandingMeasure(v), func(b []byte) int64 { return LandingSave(v, b) })
	}, func(v *Landing) []byte {
		return fuzzImage(LandingToJsonMeasure(v), func(b []byte) int64 { return LandingToJson(v, b) })
	}),
	fuzzBuilder("LaneQuiet", func(b *LaneQuietBuilder, w []byte, r *TableReport) bool { return LaneQuietLoadBuilder(b, w, r) }, func(b *LaneQuietBuilder, w []byte, r *TableReport) bool { return LaneQuietFromJson(b, w, r) }, func(v *LaneQuiet) []byte {
		return fuzzImage(LaneQuietMeasure(v), func(b []byte) int64 { return LaneQuietSave(v, b) })
	}, func(v *LaneQuiet) []byte {
		return fuzzImage(LaneQuietToJsonMeasure(v), func(b []byte) int64 { return LaneQuietToJson(v, b) })
	}),
	fuzzBuilder("Line", func(b *LineBuilder, w []byte, r *TableReport) bool { return LineLoadBuilder(b, w, r) }, func(b *LineBuilder, w []byte, r *TableReport) bool { return LineFromJson(b, w, r) }, func(v *Line) []byte { return fuzzImage(LineMeasure(v), func(b []byte) int64 { return LineSave(v, b) }) }, func(v *Line) []byte {
		return fuzzImage(LineToJsonMeasure(v), func(b []byte) int64 { return LineToJson(v, b) })
	}),
	fuzzBuilder("LineList", func(b *LineListBuilder, w []byte, r *TableReport) bool { return LineListLoadBuilder(b, w, r) }, func(b *LineListBuilder, w []byte, r *TableReport) bool { return LineListFromJson(b, w, r) }, func(v *LineList) []byte {
		return fuzzImage(LineListMeasure(v), func(b []byte) int64 { return LineListSave(v, b) })
	}, func(v *LineList) []byte {
		return fuzzImage(LineListToJsonMeasure(v), func(b []byte) int64 { return LineListToJson(v, b) })
	}),
	fuzzBuilder("LineListPair", func(b *LineListPairBuilder, w []byte, r *TableReport) bool { return LineListPairLoadBuilder(b, w, r) }, func(b *LineListPairBuilder, w []byte, r *TableReport) bool { return LineListPairFromJson(b, w, r) }, func(v *LineListPair) []byte {
		return fuzzImage(LineListPairMeasure(v), func(b []byte) int64 { return LineListPairSave(v, b) })
	}, func(v *LineListPair) []byte {
		return fuzzImage(LineListPairToJsonMeasure(v), func(b []byte) int64 { return LineListPairToJson(v, b) })
	}),
	fuzzBuilder("LiveSeenPair", func(b *LiveSeenPairBuilder, w []byte, r *TableReport) bool { return LiveSeenPairLoadBuilder(b, w, r) }, func(b *LiveSeenPairBuilder, w []byte, r *TableReport) bool { return LiveSeenPairFromJson(b, w, r) }, func(v *LiveSeenPair) []byte {
		return fuzzImage(LiveSeenPairMeasure(v), func(b []byte) int64 { return LiveSeenPairSave(v, b) })
	}, func(v *LiveSeenPair) []byte {
		return fuzzImage(LiveSeenPairToJsonMeasure(v), func(b []byte) int64 { return LiveSeenPairToJson(v, b) })
	}),
	fuzzValue("LoadSample", func(v *LoadSample, w []byte, r *TableReport) bool { return LoadSampleLoad(v, w, r) }, func(v *LoadSample, w []byte, r *TableReport) bool { return LoadSampleFromJson(v, w, r) }, func(v *LoadSample) []byte {
		return fuzzImage(LoadSampleMeasure(v), func(b []byte) int64 { return LoadSampleSave(v, b) })
	}, func(v *LoadSample) []byte {
		return fuzzImage(LoadSampleToJsonMeasure(v), func(b []byte) int64 { return LoadSampleToJson(v, b) })
	}),
	fuzzBuilder("LogIndexEntry", func(b *LogIndexEntryBuilder, w []byte, r *TableReport) bool { return LogIndexEntryLoadBuilder(b, w, r) }, func(b *LogIndexEntryBuilder, w []byte, r *TableReport) bool { return LogIndexEntryFromJson(b, w, r) }, func(v *LogIndexEntry) []byte {
		return fuzzImage(LogIndexEntryMeasure(v), func(b []byte) int64 { return LogIndexEntrySave(v, b) })
	}, func(v *LogIndexEntry) []byte {
		return fuzzImage(LogIndexEntryToJsonMeasure(v), func(b []byte) int64 { return LogIndexEntryToJson(v, b) })
	}),
	fuzzBuilder("LogIndex", func(b *LogIndexBuilder, w []byte, r *TableReport) bool { return LogIndexLoadBuilder(b, w, r) }, func(b *LogIndexBuilder, w []byte, r *TableReport) bool { return LogIndexFromJson(b, w, r) }, func(v *LogIndex) []byte {
		return fuzzImage(LogIndexMeasure(v), func(b []byte) int64 { return LogIndexSave(v, b) })
	}, func(v *LogIndex) []byte {
		return fuzzImage(LogIndexToJsonMeasure(v), func(b []byte) int64 { return LogIndexToJson(v, b) })
	}),
	fuzzBuilder("LogLine", func(b *LogLineBuilder, w []byte, r *TableReport) bool { return LogLineLoadBuilder(b, w, r) }, func(b *LogLineBuilder, w []byte, r *TableReport) bool { return LogLineFromJson(b, w, r) }, func(v *LogLine) []byte {
		return fuzzImage(LogLineMeasure(v), func(b []byte) int64 { return LogLineSave(v, b) })
	}, func(v *LogLine) []byte {
		return fuzzImage(LogLineToJsonMeasure(v), func(b []byte) int64 { return LogLineToJson(v, b) })
	}),
	fuzzBuilder("Machine", func(b *MachineBuilder, w []byte, r *TableReport) bool { return MachineLoadBuilder(b, w, r) }, func(b *MachineBuilder, w []byte, r *TableReport) bool { return MachineFromJson(b, w, r) }, func(v *Machine) []byte {
		return fuzzImage(MachineMeasure(v), func(b []byte) int64 { return MachineSave(v, b) })
	}, func(v *Machine) []byte {
		return fuzzImage(MachineToJsonMeasure(v), func(b []byte) int64 { return MachineToJson(v, b) })
	}),
	fuzzBuilder("MemberCreate", func(b *MemberCreateBuilder, w []byte, r *TableReport) bool { return MemberCreateLoadBuilder(b, w, r) }, func(b *MemberCreateBuilder, w []byte, r *TableReport) bool { return MemberCreateFromJson(b, w, r) }, func(v *MemberCreate) []byte {
		return fuzzImage(MemberCreateMeasure(v), func(b []byte) int64 { return MemberCreateSave(v, b) })
	}, func(v *MemberCreate) []byte {
		return fuzzImage(MemberCreateToJsonMeasure(v), func(b []byte) int64 { return MemberCreateToJson(v, b) })
	}),
	fuzzBuilder("MemberExpect", func(b *MemberExpectBuilder, w []byte, r *TableReport) bool { return MemberExpectLoadBuilder(b, w, r) }, func(b *MemberExpectBuilder, w []byte, r *TableReport) bool { return MemberExpectFromJson(b, w, r) }, func(v *MemberExpect) []byte {
		return fuzzImage(MemberExpectMeasure(v), func(b []byte) int64 { return MemberExpectSave(v, b) })
	}, func(v *MemberExpect) []byte {
		return fuzzImage(MemberExpectToJsonMeasure(v), func(b []byte) int64 { return MemberExpectToJson(v, b) })
	}),
	fuzzBuilder("MemberMove", func(b *MemberMoveBuilder, w []byte, r *TableReport) bool { return MemberMoveLoadBuilder(b, w, r) }, func(b *MemberMoveBuilder, w []byte, r *TableReport) bool { return MemberMoveFromJson(b, w, r) }, func(v *MemberMove) []byte {
		return fuzzImage(MemberMoveMeasure(v), func(b []byte) int64 { return MemberMoveSave(v, b) })
	}, func(v *MemberMove) []byte {
		return fuzzImage(MemberMoveToJsonMeasure(v), func(b []byte) int64 { return MemberMoveToJson(v, b) })
	}),
	fuzzBuilder("MeterFiles", func(b *MeterFilesBuilder, w []byte, r *TableReport) bool { return MeterFilesLoadBuilder(b, w, r) }, func(b *MeterFilesBuilder, w []byte, r *TableReport) bool { return MeterFilesFromJson(b, w, r) }, func(v *MeterFiles) []byte {
		return fuzzImage(MeterFilesMeasure(v), func(b []byte) int64 { return MeterFilesSave(v, b) })
	}, func(v *MeterFiles) []byte {
		return fuzzImage(MeterFilesToJsonMeasure(v), func(b []byte) int64 { return MeterFilesToJson(v, b) })
	}),
	fuzzBuilder("MeterHolder", func(b *MeterHolderBuilder, w []byte, r *TableReport) bool { return MeterHolderLoadBuilder(b, w, r) }, func(b *MeterHolderBuilder, w []byte, r *TableReport) bool { return MeterHolderFromJson(b, w, r) }, func(v *MeterHolder) []byte {
		return fuzzImage(MeterHolderMeasure(v), func(b []byte) int64 { return MeterHolderSave(v, b) })
	}, func(v *MeterHolder) []byte {
		return fuzzImage(MeterHolderToJsonMeasure(v), func(b []byte) int64 { return MeterHolderToJson(v, b) })
	}),
	fuzzBuilder("MeterState", func(b *MeterStateBuilder, w []byte, r *TableReport) bool { return MeterStateLoadBuilder(b, w, r) }, func(b *MeterStateBuilder, w []byte, r *TableReport) bool { return MeterStateFromJson(b, w, r) }, func(v *MeterState) []byte {
		return fuzzImage(MeterStateMeasure(v), func(b []byte) int64 { return MeterStateSave(v, b) })
	}, func(v *MeterState) []byte {
		return fuzzImage(MeterStateToJsonMeasure(v), func(b []byte) int64 { return MeterStateToJson(v, b) })
	}),
	fuzzValue("MeterTicks", func(v *MeterTicks, w []byte, r *TableReport) bool { return MeterTicksLoad(v, w, r) }, func(v *MeterTicks, w []byte, r *TableReport) bool { return MeterTicksFromJson(v, w, r) }, func(v *MeterTicks) []byte {
		return fuzzImage(MeterTicksMeasure(v), func(b []byte) int64 { return MeterTicksSave(v, b) })
	}, func(v *MeterTicks) []byte {
		return fuzzImage(MeterTicksToJsonMeasure(v), func(b []byte) int64 { return MeterTicksToJson(v, b) })
	}),
	fuzzBuilder("Note", func(b *NoteBuilder, w []byte, r *TableReport) bool { return NoteLoadBuilder(b, w, r) }, func(b *NoteBuilder, w []byte, r *TableReport) bool { return NoteFromJson(b, w, r) }, func(v *Note) []byte { return fuzzImage(NoteMeasure(v), func(b []byte) int64 { return NoteSave(v, b) }) }, func(v *Note) []byte {
		return fuzzImage(NoteToJsonMeasure(v), func(b []byte) int64 { return NoteToJson(v, b) })
	}),
	fuzzBuilder("PlaceExpect", func(b *PlaceExpectBuilder, w []byte, r *TableReport) bool { return PlaceExpectLoadBuilder(b, w, r) }, func(b *PlaceExpectBuilder, w []byte, r *TableReport) bool { return PlaceExpectFromJson(b, w, r) }, func(v *PlaceExpect) []byte {
		return fuzzImage(PlaceExpectMeasure(v), func(b []byte) int64 { return PlaceExpectSave(v, b) })
	}, func(v *PlaceExpect) []byte {
		return fuzzImage(PlaceExpectToJsonMeasure(v), func(b []byte) int64 { return PlaceExpectToJson(v, b) })
	}),
	fuzzBuilder("PriorityCards", func(b *PriorityCardsBuilder, w []byte, r *TableReport) bool { return PriorityCardsLoadBuilder(b, w, r) }, func(b *PriorityCardsBuilder, w []byte, r *TableReport) bool { return PriorityCardsFromJson(b, w, r) }, func(v *PriorityCards) []byte {
		return fuzzImage(PriorityCardsMeasure(v), func(b []byte) int64 { return PriorityCardsSave(v, b) })
	}, func(v *PriorityCards) []byte {
		return fuzzImage(PriorityCardsToJsonMeasure(v), func(b []byte) int64 { return PriorityCardsToJson(v, b) })
	}),
	fuzzBuilder("PropWrite", func(b *PropWriteBuilder, w []byte, r *TableReport) bool { return PropWriteLoadBuilder(b, w, r) }, func(b *PropWriteBuilder, w []byte, r *TableReport) bool { return PropWriteFromJson(b, w, r) }, func(v *PropWrite) []byte {
		return fuzzImage(PropWriteMeasure(v), func(b []byte) int64 { return PropWriteSave(v, b) })
	}, func(v *PropWrite) []byte {
		return fuzzImage(PropWriteToJsonMeasure(v), func(b []byte) int64 { return PropWriteToJson(v, b) })
	}),
	fuzzBuilder("QueuedChange", func(b *QueuedChangeBuilder, w []byte, r *TableReport) bool { return QueuedChangeLoadBuilder(b, w, r) }, func(b *QueuedChangeBuilder, w []byte, r *TableReport) bool { return QueuedChangeFromJson(b, w, r) }, func(v *QueuedChange) []byte {
		return fuzzImage(QueuedChangeMeasure(v), func(b []byte) int64 { return QueuedChangeSave(v, b) })
	}, func(v *QueuedChange) []byte {
		return fuzzImage(QueuedChangeToJsonMeasure(v), func(b []byte) int64 { return QueuedChangeToJson(v, b) })
	}),
	fuzzValue("ReadCardCounts", func(v *ReadCardCounts, w []byte, r *TableReport) bool { return ReadCardCountsLoad(v, w, r) }, func(v *ReadCardCounts, w []byte, r *TableReport) bool { return ReadCardCountsFromJson(v, w, r) }, func(v *ReadCardCounts) []byte {
		return fuzzImage(ReadCardCountsMeasure(v), func(b []byte) int64 { return ReadCardCountsSave(v, b) })
	}, func(v *ReadCardCounts) []byte {
		return fuzzImage(ReadCardCountsToJsonMeasure(v), func(b []byte) int64 { return ReadCardCountsToJson(v, b) })
	}),
	fuzzBuilder("ReadDayPair", func(b *ReadDayPairBuilder, w []byte, r *TableReport) bool { return ReadDayPairLoadBuilder(b, w, r) }, func(b *ReadDayPairBuilder, w []byte, r *TableReport) bool { return ReadDayPairFromJson(b, w, r) }, func(v *ReadDayPair) []byte {
		return fuzzImage(ReadDayPairMeasure(v), func(b []byte) int64 { return ReadDayPairSave(v, b) })
	}, func(v *ReadDayPair) []byte {
		return fuzzImage(ReadDayPairToJsonMeasure(v), func(b []byte) int64 { return ReadDayPairToJson(v, b) })
	}),
	fuzzBuilder("ReadDay", func(b *ReadDayBuilder, w []byte, r *TableReport) bool { return ReadDayLoadBuilder(b, w, r) }, func(b *ReadDayBuilder, w []byte, r *TableReport) bool { return ReadDayFromJson(b, w, r) }, func(v *ReadDay) []byte {
		return fuzzImage(ReadDayMeasure(v), func(b []byte) int64 { return ReadDaySave(v, b) })
	}, func(v *ReadDay) []byte {
		return fuzzImage(ReadDayToJsonMeasure(v), func(b []byte) int64 { return ReadDayToJson(v, b) })
	}),
	fuzzBuilder("ReaderSpendPair", func(b *ReaderSpendPairBuilder, w []byte, r *TableReport) bool {
		return ReaderSpendPairLoadBuilder(b, w, r)
	}, func(b *ReaderSpendPairBuilder, w []byte, r *TableReport) bool {
		return ReaderSpendPairFromJson(b, w, r)
	}, func(v *ReaderSpendPair) []byte {
		return fuzzImage(ReaderSpendPairMeasure(v), func(b []byte) int64 { return ReaderSpendPairSave(v, b) })
	}, func(v *ReaderSpendPair) []byte {
		return fuzzImage(ReaderSpendPairToJsonMeasure(v), func(b []byte) int64 { return ReaderSpendPairToJson(v, b) })
	}),
	fuzzBuilder("ReaderSpend", func(b *ReaderSpendBuilder, w []byte, r *TableReport) bool { return ReaderSpendLoadBuilder(b, w, r) }, func(b *ReaderSpendBuilder, w []byte, r *TableReport) bool { return ReaderSpendFromJson(b, w, r) }, func(v *ReaderSpend) []byte {
		return fuzzImage(ReaderSpendMeasure(v), func(b []byte) int64 { return ReaderSpendSave(v, b) })
	}, func(v *ReaderSpend) []byte {
		return fuzzImage(ReaderSpendToJsonMeasure(v), func(b []byte) int64 { return ReaderSpendToJson(v, b) })
	}),
	fuzzValue("ReadsWindowView", func(v *ReadsWindowView, w []byte, r *TableReport) bool { return ReadsWindowViewLoad(v, w, r) }, func(v *ReadsWindowView, w []byte, r *TableReport) bool { return ReadsWindowViewFromJson(v, w, r) }, func(v *ReadsWindowView) []byte {
		return fuzzImage(ReadsWindowViewMeasure(v), func(b []byte) int64 { return ReadsWindowViewSave(v, b) })
	}, func(v *ReadsWindowView) []byte {
		return fuzzImage(ReadsWindowViewToJsonMeasure(v), func(b []byte) int64 { return ReadsWindowViewToJson(v, b) })
	}),
	fuzzBuilder("RefusedRecord", func(b *RefusedRecordBuilder, w []byte, r *TableReport) bool { return RefusedRecordLoadBuilder(b, w, r) }, func(b *RefusedRecordBuilder, w []byte, r *TableReport) bool { return RefusedRecordFromJson(b, w, r) }, func(v *RefusedRecord) []byte {
		return fuzzImage(RefusedRecordMeasure(v), func(b []byte) int64 { return RefusedRecordSave(v, b) })
	}, func(v *RefusedRecord) []byte {
		return fuzzImage(RefusedRecordToJsonMeasure(v), func(b []byte) int64 { return RefusedRecordToJson(v, b) })
	}),
	fuzzBuilder("RefusedVerb", func(b *RefusedVerbBuilder, w []byte, r *TableReport) bool { return RefusedVerbLoadBuilder(b, w, r) }, func(b *RefusedVerbBuilder, w []byte, r *TableReport) bool { return RefusedVerbFromJson(b, w, r) }, func(v *RefusedVerb) []byte {
		return fuzzImage(RefusedVerbMeasure(v), func(b []byte) int64 { return RefusedVerbSave(v, b) })
	}, func(v *RefusedVerb) []byte {
		return fuzzImage(RefusedVerbToJsonMeasure(v), func(b []byte) int64 { return RefusedVerbToJson(v, b) })
	}),
	fuzzBuilder("ResetMark", func(b *ResetMarkBuilder, w []byte, r *TableReport) bool { return ResetMarkLoadBuilder(b, w, r) }, func(b *ResetMarkBuilder, w []byte, r *TableReport) bool { return ResetMarkFromJson(b, w, r) }, func(v *ResetMark) []byte {
		return fuzzImage(ResetMarkMeasure(v), func(b []byte) int64 { return ResetMarkSave(v, b) })
	}, func(v *ResetMark) []byte {
		return fuzzImage(ResetMarkToJsonMeasure(v), func(b []byte) int64 { return ResetMarkToJson(v, b) })
	}),
	fuzzBuilder("ResetResult", func(b *ResetResultBuilder, w []byte, r *TableReport) bool { return ResetResultLoadBuilder(b, w, r) }, func(b *ResetResultBuilder, w []byte, r *TableReport) bool { return ResetResultFromJson(b, w, r) }, func(v *ResetResult) []byte {
		return fuzzImage(ResetResultMeasure(v), func(b []byte) int64 { return ResetResultSave(v, b) })
	}, func(v *ResetResult) []byte {
		return fuzzImage(ResetResultToJsonMeasure(v), func(b []byte) int64 { return ResetResultToJson(v, b) })
	}),
	fuzzBuilder("ResetRowPair", func(b *ResetRowPairBuilder, w []byte, r *TableReport) bool { return ResetRowPairLoadBuilder(b, w, r) }, func(b *ResetRowPairBuilder, w []byte, r *TableReport) bool { return ResetRowPairFromJson(b, w, r) }, func(v *ResetRowPair) []byte {
		return fuzzImage(ResetRowPairMeasure(v), func(b []byte) int64 { return ResetRowPairSave(v, b) })
	}, func(v *ResetRowPair) []byte {
		return fuzzImage(ResetRowPairToJsonMeasure(v), func(b []byte) int64 { return ResetRowPairToJson(v, b) })
	}),
	fuzzValue("ResetRow", func(v *ResetRow, w []byte, r *TableReport) bool { return ResetRowLoad(v, w, r) }, func(v *ResetRow, w []byte, r *TableReport) bool { return ResetRowFromJson(v, w, r) }, func(v *ResetRow) []byte {
		return fuzzImage(ResetRowMeasure(v), func(b []byte) int64 { return ResetRowSave(v, b) })
	}, func(v *ResetRow) []byte {
		return fuzzImage(ResetRowToJsonMeasure(v), func(b []byte) int64 { return ResetRowToJson(v, b) })
	}),
	fuzzBuilder("ResetStreamPair", func(b *ResetStreamPairBuilder, w []byte, r *TableReport) bool {
		return ResetStreamPairLoadBuilder(b, w, r)
	}, func(b *ResetStreamPairBuilder, w []byte, r *TableReport) bool {
		return ResetStreamPairFromJson(b, w, r)
	}, func(v *ResetStreamPair) []byte {
		return fuzzImage(ResetStreamPairMeasure(v), func(b []byte) int64 { return ResetStreamPairSave(v, b) })
	}, func(v *ResetStreamPair) []byte {
		return fuzzImage(ResetStreamPairToJsonMeasure(v), func(b []byte) int64 { return ResetStreamPairToJson(v, b) })
	}),
	fuzzBuilder("ResetStream", func(b *ResetStreamBuilder, w []byte, r *TableReport) bool { return ResetStreamLoadBuilder(b, w, r) }, func(b *ResetStreamBuilder, w []byte, r *TableReport) bool { return ResetStreamFromJson(b, w, r) }, func(v *ResetStream) []byte {
		return fuzzImage(ResetStreamMeasure(v), func(b []byte) int64 { return ResetStreamSave(v, b) })
	}, func(v *ResetStream) []byte {
		return fuzzImage(ResetStreamToJsonMeasure(v), func(b []byte) int64 { return ResetStreamToJson(v, b) })
	}),
	fuzzBuilder("RoutePrices", func(b *RoutePricesBuilder, w []byte, r *TableReport) bool { return RoutePricesLoadBuilder(b, w, r) }, func(b *RoutePricesBuilder, w []byte, r *TableReport) bool { return RoutePricesFromJson(b, w, r) }, func(v *RoutePrices) []byte {
		return fuzzImage(RoutePricesMeasure(v), func(b []byte) int64 { return RoutePricesSave(v, b) })
	}, func(v *RoutePrices) []byte {
		return fuzzImage(RoutePricesToJsonMeasure(v), func(b []byte) int64 { return RoutePricesToJson(v, b) })
	}),
	fuzzBuilder("Route", func(b *RouteBuilder, w []byte, r *TableReport) bool { return RouteLoadBuilder(b, w, r) }, func(b *RouteBuilder, w []byte, r *TableReport) bool { return RouteFromJson(b, w, r) }, func(v *Route) []byte {
		return fuzzImage(RouteMeasure(v), func(b []byte) int64 { return RouteSave(v, b) })
	}, func(v *Route) []byte {
		return fuzzImage(RouteToJsonMeasure(v), func(b []byte) int64 { return RouteToJson(v, b) })
	}),
	fuzzBuilder("RouteStat", func(b *RouteStatBuilder, w []byte, r *TableReport) bool { return RouteStatLoadBuilder(b, w, r) }, func(b *RouteStatBuilder, w []byte, r *TableReport) bool { return RouteStatFromJson(b, w, r) }, func(v *RouteStat) []byte {
		return fuzzImage(RouteStatMeasure(v), func(b []byte) int64 { return RouteStatSave(v, b) })
	}, func(v *RouteStat) []byte {
		return fuzzImage(RouteStatToJsonMeasure(v), func(b []byte) int64 { return RouteStatToJson(v, b) })
	}),
	fuzzBuilder("RowName", func(b *RowNameBuilder, w []byte, r *TableReport) bool { return RowNameLoadBuilder(b, w, r) }, func(b *RowNameBuilder, w []byte, r *TableReport) bool { return RowNameFromJson(b, w, r) }, func(v *RowName) []byte {
		return fuzzImage(RowNameMeasure(v), func(b []byte) int64 { return RowNameSave(v, b) })
	}, func(v *RowName) []byte {
		return fuzzImage(RowNameToJsonMeasure(v), func(b []byte) int64 { return RowNameToJson(v, b) })
	}),
	fuzzValue("RttSample", func(v *RttSample, w []byte, r *TableReport) bool { return RttSampleLoad(v, w, r) }, func(v *RttSample, w []byte, r *TableReport) bool { return RttSampleFromJson(v, w, r) }, func(v *RttSample) []byte {
		return fuzzImage(RttSampleMeasure(v), func(b []byte) int64 { return RttSampleSave(v, b) })
	}, func(v *RttSample) []byte {
		return fuzzImage(RttSampleToJsonMeasure(v), func(b []byte) int64 { return RttSampleToJson(v, b) })
	}),
	fuzzBuilder("SeatChange", func(b *SeatChangeBuilder, w []byte, r *TableReport) bool { return SeatChangeLoadBuilder(b, w, r) }, func(b *SeatChangeBuilder, w []byte, r *TableReport) bool { return SeatChangeFromJson(b, w, r) }, func(v *SeatChange) []byte {
		return fuzzImage(SeatChangeMeasure(v), func(b []byte) int64 { return SeatChangeSave(v, b) })
	}, func(v *SeatChange) []byte {
		return fuzzImage(SeatChangeToJsonMeasure(v), func(b []byte) int64 { return SeatChangeToJson(v, b) })
	}),
	fuzzBuilder("SeatWaits", func(b *SeatWaitsBuilder, w []byte, r *TableReport) bool { return SeatWaitsLoadBuilder(b, w, r) }, func(b *SeatWaitsBuilder, w []byte, r *TableReport) bool { return SeatWaitsFromJson(b, w, r) }, func(v *SeatWaits) []byte {
		return fuzzImage(SeatWaitsMeasure(v), func(b []byte) int64 { return SeatWaitsSave(v, b) })
	}, func(v *SeatWaits) []byte {
		return fuzzImage(SeatWaitsToJsonMeasure(v), func(b []byte) int64 { return SeatWaitsToJson(v, b) })
	}),
	fuzzBuilder("SentinelCounts", func(b *SentinelCountsBuilder, w []byte, r *TableReport) bool {
		return SentinelCountsLoadBuilder(b, w, r)
	}, func(b *SentinelCountsBuilder, w []byte, r *TableReport) bool { return SentinelCountsFromJson(b, w, r) }, func(v *SentinelCounts) []byte {
		return fuzzImage(SentinelCountsMeasure(v), func(b []byte) int64 { return SentinelCountsSave(v, b) })
	}, func(v *SentinelCounts) []byte {
		return fuzzImage(SentinelCountsToJsonMeasure(v), func(b []byte) int64 { return SentinelCountsToJson(v, b) })
	}),
	fuzzBuilder("SentinelPair", func(b *SentinelPairBuilder, w []byte, r *TableReport) bool { return SentinelPairLoadBuilder(b, w, r) }, func(b *SentinelPairBuilder, w []byte, r *TableReport) bool { return SentinelPairFromJson(b, w, r) }, func(v *SentinelPair) []byte {
		return fuzzImage(SentinelPairMeasure(v), func(b []byte) int64 { return SentinelPairSave(v, b) })
	}, func(v *SentinelPair) []byte {
		return fuzzImage(SentinelPairToJsonMeasure(v), func(b []byte) int64 { return SentinelPairToJson(v, b) })
	}),
	fuzzValue("Span", func(v *Span, w []byte, r *TableReport) bool { return SpanLoad(v, w, r) }, func(v *Span, w []byte, r *TableReport) bool { return SpanFromJson(v, w, r) }, func(v *Span) []byte { return fuzzImage(SpanMeasure(v), func(b []byte) int64 { return SpanSave(v, b) }) }, func(v *Span) []byte {
		return fuzzImage(SpanToJsonMeasure(v), func(b []byte) int64 { return SpanToJson(v, b) })
	}),
	fuzzBuilder("StageStatPair", func(b *StageStatPairBuilder, w []byte, r *TableReport) bool { return StageStatPairLoadBuilder(b, w, r) }, func(b *StageStatPairBuilder, w []byte, r *TableReport) bool { return StageStatPairFromJson(b, w, r) }, func(v *StageStatPair) []byte {
		return fuzzImage(StageStatPairMeasure(v), func(b []byte) int64 { return StageStatPairSave(v, b) })
	}, func(v *StageStatPair) []byte {
		return fuzzImage(StageStatPairToJsonMeasure(v), func(b []byte) int64 { return StageStatPairToJson(v, b) })
	}),
	fuzzBuilder("StageStats", func(b *StageStatsBuilder, w []byte, r *TableReport) bool { return StageStatsLoadBuilder(b, w, r) }, func(b *StageStatsBuilder, w []byte, r *TableReport) bool { return StageStatsFromJson(b, w, r) }, func(v *StageStats) []byte {
		return fuzzImage(StageStatsMeasure(v), func(b []byte) int64 { return StageStatsSave(v, b) })
	}, func(v *StageStats) []byte {
		return fuzzImage(StageStatsToJsonMeasure(v), func(b []byte) int64 { return StageStatsToJson(v, b) })
	}),
	fuzzValue("StageStat", func(v *StageStat, w []byte, r *TableReport) bool { return StageStatLoad(v, w, r) }, func(v *StageStat, w []byte, r *TableReport) bool { return StageStatFromJson(v, w, r) }, func(v *StageStat) []byte {
		return fuzzImage(StageStatMeasure(v), func(b []byte) int64 { return StageStatSave(v, b) })
	}, func(v *StageStat) []byte {
		return fuzzImage(StageStatToJsonMeasure(v), func(b []byte) int64 { return StageStatToJson(v, b) })
	}),
	fuzzBuilder("StageTimes", func(b *StageTimesBuilder, w []byte, r *TableReport) bool { return StageTimesLoadBuilder(b, w, r) }, func(b *StageTimesBuilder, w []byte, r *TableReport) bool { return StageTimesFromJson(b, w, r) }, func(v *StageTimes) []byte {
		return fuzzImage(StageTimesMeasure(v), func(b []byte) int64 { return StageTimesSave(v, b) })
	}, func(v *StageTimes) []byte {
		return fuzzImage(StageTimesToJsonMeasure(v), func(b []byte) int64 { return StageTimesToJson(v, b) })
	}),
	fuzzBuilder("StatsArchive", func(b *StatsArchiveBuilder, w []byte, r *TableReport) bool { return StatsArchiveLoadBuilder(b, w, r) }, func(b *StatsArchiveBuilder, w []byte, r *TableReport) bool { return StatsArchiveFromJson(b, w, r) }, func(v *StatsArchive) []byte {
		return fuzzImage(StatsArchiveMeasure(v), func(b []byte) int64 { return StatsArchiveSave(v, b) })
	}, func(v *StatsArchive) []byte {
		return fuzzImage(StatsArchiveToJsonMeasure(v), func(b []byte) int64 { return StatsArchiveToJson(v, b) })
	}),
	fuzzBuilder("StatsRecord", func(b *StatsRecordBuilder, w []byte, r *TableReport) bool { return StatsRecordLoadBuilder(b, w, r) }, func(b *StatsRecordBuilder, w []byte, r *TableReport) bool { return StatsRecordFromJson(b, w, r) }, func(v *StatsRecord) []byte {
		return fuzzImage(StatsRecordMeasure(v), func(b []byte) int64 { return StatsRecordSave(v, b) })
	}, func(v *StatsRecord) []byte {
		return fuzzImage(StatsRecordToJsonMeasure(v), func(b []byte) int64 { return StatsRecordToJson(v, b) })
	}),
	fuzzBuilder("StopLease", func(b *StopLeaseBuilder, w []byte, r *TableReport) bool { return StopLeaseLoadBuilder(b, w, r) }, func(b *StopLeaseBuilder, w []byte, r *TableReport) bool { return StopLeaseFromJson(b, w, r) }, func(v *StopLease) []byte {
		return fuzzImage(StopLeaseMeasure(v), func(b []byte) int64 { return StopLeaseSave(v, b) })
	}, func(v *StopLease) []byte {
		return fuzzImage(StopLeaseToJsonMeasure(v), func(b []byte) int64 { return StopLeaseToJson(v, b) })
	}),
	fuzzBuilder("StopsRecord", func(b *StopsRecordBuilder, w []byte, r *TableReport) bool { return StopsRecordLoadBuilder(b, w, r) }, func(b *StopsRecordBuilder, w []byte, r *TableReport) bool { return StopsRecordFromJson(b, w, r) }, func(v *StopsRecord) []byte {
		return fuzzImage(StopsRecordMeasure(v), func(b []byte) int64 { return StopsRecordSave(v, b) })
	}, func(v *StopsRecord) []byte {
		return fuzzImage(StopsRecordToJsonMeasure(v), func(b []byte) int64 { return StopsRecordToJson(v, b) })
	}),
	fuzzBuilder("Stop", func(b *StopBuilder, w []byte, r *TableReport) bool { return StopLoadBuilder(b, w, r) }, func(b *StopBuilder, w []byte, r *TableReport) bool { return StopFromJson(b, w, r) }, func(v *Stop) []byte { return fuzzImage(StopMeasure(v), func(b []byte) int64 { return StopSave(v, b) }) }, func(v *Stop) []byte {
		return fuzzImage(StopToJsonMeasure(v), func(b []byte) int64 { return StopToJson(v, b) })
	}),
	fuzzBuilder("StoreRTTRecord", func(b *StoreRTTRecordBuilder, w []byte, r *TableReport) bool {
		return StoreRTTRecordLoadBuilder(b, w, r)
	}, func(b *StoreRTTRecordBuilder, w []byte, r *TableReport) bool { return StoreRTTRecordFromJson(b, w, r) }, func(v *StoreRTTRecord) []byte {
		return fuzzImage(StoreRTTRecordMeasure(v), func(b []byte) int64 { return StoreRTTRecordSave(v, b) })
	}, func(v *StoreRTTRecord) []byte {
		return fuzzImage(StoreRTTRecordToJsonMeasure(v), func(b []byte) int64 { return StoreRTTRecordToJson(v, b) })
	}),
	fuzzBuilder("StreamBasePair", func(b *StreamBasePairBuilder, w []byte, r *TableReport) bool {
		return StreamBasePairLoadBuilder(b, w, r)
	}, func(b *StreamBasePairBuilder, w []byte, r *TableReport) bool { return StreamBasePairFromJson(b, w, r) }, func(v *StreamBasePair) []byte {
		return fuzzImage(StreamBasePairMeasure(v), func(b []byte) int64 { return StreamBasePairSave(v, b) })
	}, func(v *StreamBasePair) []byte {
		return fuzzImage(StreamBasePairToJsonMeasure(v), func(b []byte) int64 { return StreamBasePairToJson(v, b) })
	}),
	fuzzBuilder("StreamBase", func(b *StreamBaseBuilder, w []byte, r *TableReport) bool { return StreamBaseLoadBuilder(b, w, r) }, func(b *StreamBaseBuilder, w []byte, r *TableReport) bool { return StreamBaseFromJson(b, w, r) }, func(v *StreamBase) []byte {
		return fuzzImage(StreamBaseMeasure(v), func(b []byte) int64 { return StreamBaseSave(v, b) })
	}, func(v *StreamBase) []byte {
		return fuzzImage(StreamBaseToJsonMeasure(v), func(b []byte) int64 { return StreamBaseToJson(v, b) })
	}),
	fuzzBuilder("StreamStagesPair", func(b *StreamStagesPairBuilder, w []byte, r *TableReport) bool {
		return StreamStagesPairLoadBuilder(b, w, r)
	}, func(b *StreamStagesPairBuilder, w []byte, r *TableReport) bool {
		return StreamStagesPairFromJson(b, w, r)
	}, func(v *StreamStagesPair) []byte {
		return fuzzImage(StreamStagesPairMeasure(v), func(b []byte) int64 { return StreamStagesPairSave(v, b) })
	}, func(v *StreamStagesPair) []byte {
		return fuzzImage(StreamStagesPairToJsonMeasure(v), func(b []byte) int64 { return StreamStagesPairToJson(v, b) })
	}),
	fuzzBuilder("StreamTransition", func(b *StreamTransitionBuilder, w []byte, r *TableReport) bool {
		return StreamTransitionLoadBuilder(b, w, r)
	}, func(b *StreamTransitionBuilder, w []byte, r *TableReport) bool {
		return StreamTransitionFromJson(b, w, r)
	}, func(v *StreamTransition) []byte {
		return fuzzImage(StreamTransitionMeasure(v), func(b []byte) int64 { return StreamTransitionSave(v, b) })
	}, func(v *StreamTransition) []byte {
		return fuzzImage(StreamTransitionToJsonMeasure(v), func(b []byte) int64 { return StreamTransitionToJson(v, b) })
	}),
	fuzzBuilder("Stuck", func(b *StuckBuilder, w []byte, r *TableReport) bool { return StuckLoadBuilder(b, w, r) }, func(b *StuckBuilder, w []byte, r *TableReport) bool { return StuckFromJson(b, w, r) }, func(v *Stuck) []byte {
		return fuzzImage(StuckMeasure(v), func(b []byte) int64 { return StuckSave(v, b) })
	}, func(v *Stuck) []byte {
		return fuzzImage(StuckToJsonMeasure(v), func(b []byte) int64 { return StuckToJson(v, b) })
	}),
	fuzzValue("Suppressed", func(v *Suppressed, w []byte, r *TableReport) bool { return SuppressedLoad(v, w, r) }, func(v *Suppressed, w []byte, r *TableReport) bool { return SuppressedFromJson(v, w, r) }, func(v *Suppressed) []byte {
		return fuzzImage(SuppressedMeasure(v), func(b []byte) int64 { return SuppressedSave(v, b) })
	}, func(v *Suppressed) []byte {
		return fuzzImage(SuppressedToJsonMeasure(v), func(b []byte) int64 { return SuppressedToJson(v, b) })
	}),
	fuzzBuilder("TextPair", func(b *TextPairBuilder, w []byte, r *TableReport) bool { return TextPairLoadBuilder(b, w, r) }, func(b *TextPairBuilder, w []byte, r *TableReport) bool { return TextPairFromJson(b, w, r) }, func(v *TextPair) []byte {
		return fuzzImage(TextPairMeasure(v), func(b []byte) int64 { return TextPairSave(v, b) })
	}, func(v *TextPair) []byte {
		return fuzzImage(TextPairToJsonMeasure(v), func(b []byte) int64 { return TextPairToJson(v, b) })
	}),
	fuzzBuilder("TidyCard", func(b *TidyCardBuilder, w []byte, r *TableReport) bool { return TidyCardLoadBuilder(b, w, r) }, func(b *TidyCardBuilder, w []byte, r *TableReport) bool { return TidyCardFromJson(b, w, r) }, func(v *TidyCard) []byte {
		return fuzzImage(TidyCardMeasure(v), func(b []byte) int64 { return TidyCardSave(v, b) })
	}, func(v *TidyCard) []byte {
		return fuzzImage(TidyCardToJsonMeasure(v), func(b []byte) int64 { return TidyCardToJson(v, b) })
	}),
	fuzzBuilder("TidyInFlight", func(b *TidyInFlightBuilder, w []byte, r *TableReport) bool { return TidyInFlightLoadBuilder(b, w, r) }, func(b *TidyInFlightBuilder, w []byte, r *TableReport) bool { return TidyInFlightFromJson(b, w, r) }, func(v *TidyInFlight) []byte {
		return fuzzImage(TidyInFlightMeasure(v), func(b []byte) int64 { return TidyInFlightSave(v, b) })
	}, func(v *TidyInFlight) []byte {
		return fuzzImage(TidyInFlightToJsonMeasure(v), func(b []byte) int64 { return TidyInFlightToJson(v, b) })
	}),
	fuzzBuilder("TidyResult", func(b *TidyResultBuilder, w []byte, r *TableReport) bool { return TidyResultLoadBuilder(b, w, r) }, func(b *TidyResultBuilder, w []byte, r *TableReport) bool { return TidyResultFromJson(b, w, r) }, func(v *TidyResult) []byte {
		return fuzzImage(TidyResultMeasure(v), func(b []byte) int64 { return TidyResultSave(v, b) })
	}, func(v *TidyResult) []byte {
		return fuzzImage(TidyResultToJsonMeasure(v), func(b []byte) int64 { return TidyResultToJson(v, b) })
	}),
	fuzzBuilder("TidyRow", func(b *TidyRowBuilder, w []byte, r *TableReport) bool { return TidyRowLoadBuilder(b, w, r) }, func(b *TidyRowBuilder, w []byte, r *TableReport) bool { return TidyRowFromJson(b, w, r) }, func(v *TidyRow) []byte {
		return fuzzImage(TidyRowMeasure(v), func(b []byte) int64 { return TidyRowSave(v, b) })
	}, func(v *TidyRow) []byte {
		return fuzzImage(TidyRowToJsonMeasure(v), func(b []byte) int64 { return TidyRowToJson(v, b) })
	}),
	fuzzBuilder("TierCostsPair", func(b *TierCostsPairBuilder, w []byte, r *TableReport) bool { return TierCostsPairLoadBuilder(b, w, r) }, func(b *TierCostsPairBuilder, w []byte, r *TableReport) bool { return TierCostsPairFromJson(b, w, r) }, func(v *TierCostsPair) []byte {
		return fuzzImage(TierCostsPairMeasure(v), func(b []byte) int64 { return TierCostsPairSave(v, b) })
	}, func(v *TierCostsPair) []byte {
		return fuzzImage(TierCostsPairToJsonMeasure(v), func(b []byte) int64 { return TierCostsPairToJson(v, b) })
	}),
	fuzzBuilder("TierCosts", func(b *TierCostsBuilder, w []byte, r *TableReport) bool { return TierCostsLoadBuilder(b, w, r) }, func(b *TierCostsBuilder, w []byte, r *TableReport) bool { return TierCostsFromJson(b, w, r) }, func(v *TierCosts) []byte {
		return fuzzImage(TierCostsMeasure(v), func(b []byte) int64 { return TierCostsSave(v, b) })
	}, func(v *TierCosts) []byte {
		return fuzzImage(TierCostsToJsonMeasure(v), func(b []byte) int64 { return TierCostsToJson(v, b) })
	}),
	fuzzBuilder("Timers", func(b *TimersBuilder, w []byte, r *TableReport) bool { return TimersLoadBuilder(b, w, r) }, func(b *TimersBuilder, w []byte, r *TableReport) bool { return TimersFromJson(b, w, r) }, func(v *Timers) []byte {
		return fuzzImage(TimersMeasure(v), func(b []byte) int64 { return TimersSave(v, b) })
	}, func(v *Timers) []byte {
		return fuzzImage(TimersToJsonMeasure(v), func(b []byte) int64 { return TimersToJson(v, b) })
	}),
	fuzzBuilder("Timer", func(b *TimerBuilder, w []byte, r *TableReport) bool { return TimerLoadBuilder(b, w, r) }, func(b *TimerBuilder, w []byte, r *TableReport) bool { return TimerFromJson(b, w, r) }, func(v *Timer) []byte {
		return fuzzImage(TimerMeasure(v), func(b []byte) int64 { return TimerSave(v, b) })
	}, func(v *Timer) []byte {
		return fuzzImage(TimerToJsonMeasure(v), func(b []byte) int64 { return TimerToJson(v, b) })
	}),
	fuzzBuilder("WhereRecord", func(b *WhereRecordBuilder, w []byte, r *TableReport) bool { return WhereRecordLoadBuilder(b, w, r) }, func(b *WhereRecordBuilder, w []byte, r *TableReport) bool { return WhereRecordFromJson(b, w, r) }, func(v *WhereRecord) []byte {
		return fuzzImage(WhereRecordMeasure(v), func(b []byte) int64 { return WhereRecordSave(v, b) })
	}, func(v *WhereRecord) []byte {
		return fuzzImage(WhereRecordToJsonMeasure(v), func(b []byte) int64 { return WhereRecordToJson(v, b) })
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
	corpus, others := fuzzImages(t, filepath.Join("..", "..", "..", "..", "schemas", "viewrows", "fuzz", "*")), fuzzImages(t)
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

func FuzzViewrowswire(f *testing.F) {
	fuzzSeedList(f) // fills fuzzSeeds; the seeds themselves are the checked-in files
	f.Fuzz(func(t *testing.T, sel uint8, in []byte) {
		root := fuzzRoots[int(sel)%len(fuzzRoots)]
		for form := fuzzWire; form <= fuzzJSON; form++ {
			wire, text, rep, err := root.load(form, in)
			if err != nil {
				require.True(t, strings.HasPrefix(fmt.Sprintf("%T", err), "viewrowswire.Table"), "%s as %d %q: a refusal with no name: %T %v", root.name, form, in, err, err)
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

// TestWriteFuzzViewrowswireSeeds writes testdata/fuzz/FuzzViewrowswire when NOVA_FUZZ_WRITE_SEEDS=1.
func TestWriteFuzzViewrowswireSeeds(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_FUZZ_WRITE_SEEDS") != "1" {
		t.Skip("set NOVA_FUZZ_WRITE_SEEDS=1 to write testdata/fuzz/FuzzViewrowswire")
	}
	dir := filepath.Join("testdata", "fuzz", "FuzzViewrowswire")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	sels, ins := fuzzSeedList(t)
	for i := range ins {
		body := fmt.Sprintf("go test fuzz v1\nbyte('\\x%02x')\n[]byte(%s)\n", sels[i], strconv.Quote(string(ins[i])))
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("seed-%04d", i)), []byte(body), 0o644))
	}
}
