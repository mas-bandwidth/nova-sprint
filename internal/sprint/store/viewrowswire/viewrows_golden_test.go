package viewrowswire

// TestViewRowsGolden is the golden test of the view-row definitions
// (schemas/viewrows). Every fixed table round-trips as binary wire and as
// padded standard base64 of that wire through pkg/schemaread (S1, S2). A
// value past each counted bound is refused by name and is not returned
// clamped. A damaged image of each form is refused by name.

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"unsafe"

	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/require"
)

// testClock is the test's clock, in seconds. It is an argument, and it is
// never 0, so a zero time stays distinguishable from a stamp the test wrote.
const testClock = 1000.0

const testClockNS = int64(testClock * 1e9)

const helpText = "nova-sprint -h"

const noCompanion uint32 = 0xffffffff

func zeroOf[T any]() T {
	var z T
	return z
}

type wireTable struct {
	name string
	run  func(t *testing.T)
}

func TestViewRowsGolden(t *testing.T) {
	t.Parallel()
	tables := allTables()
	require.Equal(t, schemaTableCount(t), len(tables), "every fixed table is in the golden test")
	for _, want := range []string{
		"WhereRecord", "StoreRTTRecord", "StopsRecord", "RefusedRecord", "Beat",
		"CheckRecord", "FleetDropDebt", "FleetMachineRow", "StatsRecord", "StatsArchive",
		"ResetResult", "ResetMark", "TidyResult", "Stuck", "LogIndex", "Machine",
		"Heartbeat", "HeldRecord",
	} {
		found := false
		for _, table := range tables {
			if table.name == want {
				found = true
				break
			}
		}
		require.True(t, found, "missing record %s", want)
	}
	for _, table := range tables {
		t.Run(table.name, func(t *testing.T) {
			t.Parallel()
			table.run(t)
		})
	}
}

func schemaTableCount(t *testing.T) int {
	t.Helper()
	b, err := os.ReadFile("../../../../schemas/viewrows/viewrows.schema")
	require.NoError(t, err)
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "fixed table ") {
			n++
		}
	}
	return n
}

func bind[T any](
	name string,
	info func() *TableTypeInfo,
	save func([]T, []byte) int64,
	load func([]T, []byte, []TableFixedEntry, *TableReport) int64,
	measure func(int64) int64,
	planCount int32,
) wireTable {
	return wireTable{name: name, run: func(t *testing.T) {
		t.Helper()
		sample := new(T)
		stampSample(unsafe.Pointer(sample), info())
		codec := &schemaread.Codec[T]{
			Name: name, Help: helpText,
			Reset: func(v *T) { *v = zeroOf[T]() },
			Forms: []byte{TableFixedForm},
			LoadWire: func(dst *T, wire []byte) schemaread.Report {
				var rep TableReport
				var vals [1]T
				n := load(vals[:], wire, make([]TableFixedEntry, int(planCount)), &rep)
				if n < 0 {
					if rep.Reason == "" && !rep.Malformed {
						rep.Malformed = true
					}
					return reportOf(&rep)
				}
				if n != 1 {
					return schemaread.Report{Malformed: true, Reason: schemaread.ReasonWireDamaged}
				}
				*dst = vals[0]
				return reportOf(&rep)
			},
			SaveWire: func(v *T) []byte {
				n := measure(1)
				if n < 0 {
					return nil
				}
				buf := make([]byte, n)
				if save([]T{*v}, buf) != n {
					return nil
				}
				return buf
			},
		}
		reader := schemaread.NewReader(codec)
		bin, err := reader.Write(sample, schemaread.FormBinary)
		require.NoError(t, err)
		require.NotEmpty(t, bin)
		require.Equal(t, TableFixedForm, bin[0])
		got, res, err := reader.Read(bin)
		require.NoError(t, err)
		require.Equal(t, schemaread.FormBinary, res.Form)
		require.Equal(t, *sample, got)

		text, err := reader.Write(sample, schemaread.FormBase64)
		require.NoError(t, err)
		require.Equal(t, 0, len(text)%4, "base64 is padded")
		_, err = base64.StdEncoding.DecodeString(string(text))
		require.NoError(t, err)
		got, res, err = reader.Read(text)
		require.NoError(t, err)
		require.Equal(t, schemaread.FormBase64, res.Form)
		require.Equal(t, *sample, got)

		short := bin
		if len(short) > 20 {
			short = short[:20]
		} else {
			short = short[:1]
		}
		got, _, err = reader.Read(short)
		require.Error(t, err)
		require.Equal(t, zeroOf[T](), got)
		requireNamed(t, err)

		raw := append([]byte(nil), text...)
		if len(raw) > 8 {
			if raw[8] == 'A' {
				raw[8] = 'B'
			} else {
				raw[8] = 'A'
			}
		}
		got, _, err = reader.Read(raw)
		require.Error(t, err)
		require.Equal(t, zeroOf[T](), got)
		requireNamed(t, err)

		for i := range info().Fields {
			f := &info().Fields[i]
			if !f.Counted || f.ArrayBound <= 0 || f.CountOffset == noCompanion {
				continue
			}
			t.Run(f.Name, func(t *testing.T) {
				forged := new(T)
				*forged = *sample
				forgeBound(unsafe.Pointer(forged), f)
				over, err := reader.Write(forged, schemaread.FormBinary)
				// The fixed writer stores the length it is given. schemaread
				// refuses it on the way out when a constraint is set; this
				// reader has none, so the image is what the load must refuse.
				if err != nil {
					requireNamed(t, err)
					return
				}
				require.NotNil(t, over)
				got, _, err := reader.Read(over)
				require.Error(t, err)
				require.Equal(t, zeroOf[T](), got, "a value past %s is not returned clamped", f.Name)
				var ref *schemaread.Refusal
				require.ErrorAs(t, err, &ref)
				require.Equal(t, schemaread.ReasonClamped, ref.Reason, "field %s", f.Name)
			})
		}
	}}
}

func reportOf(r *TableReport) schemaread.Report {
	return schemaread.Report{
		Unknown: int(r.Unknown), KindMismatch: int(r.KindMismatch), Clamped: int(r.Clamped),
		Malformed: r.Malformed, Reason: r.Reason,
	}
}

func requireNamed(t *testing.T, err error) {
	t.Helper()
	var ref *schemaread.Refusal
	require.ErrorAs(t, err, &ref)
	require.NotEmpty(t, ref.Reason)
}

func stampSample(p unsafe.Pointer, info *TableTypeInfo) {
	if info == nil {
		return
	}
	for i := range info.Fields {
		f := &info.Fields[i]
		if f.IsArray || f.Optional || f.Counted || f.TypeName != "int64" {
			continue
		}
		*(*int64)(unsafe.Add(p, f.Offset)) = testClockNS
		break
	}
	for i := range info.Fields {
		f := &info.Fields[i]
		if f.IsArray || f.Optional || f.TypeName != "string" || f.ArrayBound < 2 || f.CountOffset == noCompanion {
			continue
		}
		b := unsafe.Slice((*byte)(unsafe.Add(p, f.Offset)), 2)
		b[0], b[1] = 'o', 'k'
		*(*int32)(unsafe.Add(p, f.CountOffset)) = 2
		break
	}
}

func forgeBound(p unsafe.Pointer, f *TableFieldInfo) {
	if f.Optional && f.PresentOffset != noCompanion {
		*(*bool)(unsafe.Add(p, f.PresentOffset)) = true
	}
	if f.TypeName == "string" && !f.IsArray {
		b := unsafe.Slice((*byte)(unsafe.Add(p, f.Offset)), f.ArrayBound)
		for i := range b {
			b[i] = 'a'
		}
	}
	*(*int32)(unsafe.Add(p, f.CountOffset)) = f.ArrayBound + 1
}

func allTables() []wireTable {
	return []wireTable{
		bind[CardID]("CardID", CardIDTableType, CardIDFixedSave, CardIDFixedLoad, CardIDFixedMeasure, CardIDFixedPlan.Count),
		bind[RowName]("RowName", RowNameTableType, RowNameFixedSave, RowNameFixedLoad, RowNameFixedMeasure, RowNameFixedPlan.Count),
		bind[MemberName]("MemberName", MemberNameTableType, MemberNameFixedSave, MemberNameFixedLoad, MemberNameFixedMeasure, MemberNameFixedPlan.Count),
		bind[StreamID]("StreamID", StreamIDTableType, StreamIDFixedSave, StreamIDFixedLoad, StreamIDFixedMeasure, StreamIDFixedPlan.Count),
		bind[Stamp80]("Stamp80", Stamp80TableType, Stamp80FixedSave, Stamp80FixedLoad, Stamp80FixedMeasure, Stamp80FixedPlan.Count),
		bind[TierWord]("TierWord", TierWordTableType, TierWordFixedSave, TierWordFixedLoad, TierWordFixedMeasure, TierWordFixedPlan.Count),
		bind[Prose256]("Prose256", Prose256TableType, Prose256FixedSave, Prose256FixedLoad, Prose256FixedMeasure, Prose256FixedPlan.Count),
		bind[Word64]("Word64", Word64TableType, Word64FixedSave, Word64FixedLoad, Word64FixedMeasure, Word64FixedPlan.Count),
		bind[Word16]("Word16", Word16TableType, Word16FixedSave, Word16FixedLoad, Word16FixedMeasure, Word16FixedPlan.Count),
		bind[Money32]("Money32", Money32TableType, Money32FixedSave, Money32FixedLoad, Money32FixedMeasure, Money32FixedPlan.Count),
		bind[LiveKey]("LiveKey", LiveKeyTableType, LiveKeyFixedSave, LiveKeyFixedLoad, LiveKeyFixedMeasure, LiveKeyFixedPlan.Count),
		bind[StageStat]("StageStat", StageStatTableType, StageStatFixedSave, StageStatFixedLoad, StageStatFixedMeasure, StageStatFixedPlan.Count),
		bind[StageStatEntry]("StageStatEntry", StageStatEntryTableType, StageStatEntryFixedSave, StageStatEntryFixedLoad, StageStatEntryFixedMeasure, StageStatEntryFixedPlan.Count),
		bind[StreamStageEntry]("StreamStageEntry", StreamStageEntryTableType, StreamStageEntryFixedSave, StreamStageEntryFixedLoad, StreamStageEntryFixedMeasure, StreamStageEntryFixedPlan.Count),
		bind[StageTimes]("StageTimes", StageTimesTableType, StageTimesFixedSave, StageTimesFixedLoad, StageTimesFixedMeasure, StageTimesFixedPlan.Count),
		bind[ReadDay]("ReadDay", ReadDayTableType, ReadDayFixedSave, ReadDayFixedLoad, ReadDayFixedMeasure, ReadDayFixedPlan.Count),
		bind[ReadDayEntry]("ReadDayEntry", ReadDayEntryTableType, ReadDayEntryFixedSave, ReadDayEntryFixedLoad, ReadDayEntryFixedMeasure, ReadDayEntryFixedPlan.Count),
		bind[ReaderSpend]("ReaderSpend", ReaderSpendTableType, ReaderSpendFixedSave, ReaderSpendFixedLoad, ReaderSpendFixedMeasure, ReaderSpendFixedPlan.Count),
		bind[ReaderSpendEntry]("ReaderSpendEntry", ReaderSpendEntryTableType, ReaderSpendEntryFixedSave, ReaderSpendEntryFixedLoad, ReaderSpendEntryFixedMeasure, ReaderSpendEntryFixedPlan.Count),
		bind[DayGap]("DayGap", DayGapTableType, DayGapFixedSave, DayGapFixedLoad, DayGapFixedMeasure, DayGapFixedPlan.Count),
		bind[DayGapEntry]("DayGapEntry", DayGapEntryTableType, DayGapEntryFixedSave, DayGapEntryFixedLoad, DayGapEntryFixedMeasure, DayGapEntryFixedPlan.Count),
		bind[CostReconcileRecord]("CostReconcileRecord", CostReconcileRecordTableType, CostReconcileRecordFixedSave, CostReconcileRecordFixedLoad, CostReconcileRecordFixedMeasure, CostReconcileRecordFixedPlan.Count),
		bind[TierCountEntry]("TierCountEntry", TierCountEntryTableType, TierCountEntryFixedSave, TierCountEntryFixedLoad, TierCountEntryFixedMeasure, TierCountEntryFixedPlan.Count),
		bind[CostTierEntry]("CostTierEntry", CostTierEntryTableType, CostTierEntryFixedSave, CostTierEntryFixedLoad, CostTierEntryFixedMeasure, CostTierEntryFixedPlan.Count),
		bind[TierCosts]("TierCosts", TierCostsTableType, TierCostsFixedSave, TierCostsFixedLoad, TierCostsFixedMeasure, TierCostsFixedPlan.Count),
		bind[CostCheck]("CostCheck", CostCheckTableType, CostCheckFixedSave, CostCheckFixedLoad, CostCheckFixedMeasure, CostCheckFixedPlan.Count),
		bind[CriticalCard]("CriticalCard", CriticalCardTableType, CriticalCardFixedSave, CriticalCardFixedLoad, CriticalCardFixedMeasure, CriticalCardFixedPlan.Count),
		bind[Landing]("Landing", LandingTableType, LandingFixedSave, LandingFixedLoad, LandingFixedMeasure, LandingFixedPlan.Count),
		bind[ArchivedFleetCard]("ArchivedFleetCard", ArchivedFleetCardTableType, ArchivedFleetCardFixedSave, ArchivedFleetCardFixedLoad, ArchivedFleetCardFixedMeasure, ArchivedFleetCardFixedPlan.Count),
		bind[ColCountEntry]("ColCountEntry", ColCountEntryTableType, ColCountEntryFixedSave, ColCountEntryFixedLoad, ColCountEntryFixedMeasure, ColCountEntryFixedPlan.Count),
		bind[SentinelCounts]("SentinelCounts", SentinelCountsTableType, SentinelCountsFixedSave, SentinelCountsFixedLoad, SentinelCountsFixedMeasure, SentinelCountsFixedPlan.Count),
		bind[SentinelEntry]("SentinelEntry", SentinelEntryTableType, SentinelEntryFixedSave, SentinelEntryFixedLoad, SentinelEntryFixedMeasure, SentinelEntryFixedPlan.Count),
		bind[HeldStreamEntry]("HeldStreamEntry", HeldStreamEntryTableType, HeldStreamEntryFixedSave, HeldStreamEntryFixedLoad, HeldStreamEntryFixedMeasure, HeldStreamEntryFixedPlan.Count),
		bind[StreamTierEntry]("StreamTierEntry", StreamTierEntryTableType, StreamTierEntryFixedSave, StreamTierEntryFixedLoad, StreamTierEntryFixedMeasure, StreamTierEntryFixedPlan.Count),
		bind[DealtEntry]("DealtEntry", DealtEntryTableType, DealtEntryFixedSave, DealtEntryFixedLoad, DealtEntryFixedMeasure, DealtEntryFixedPlan.Count),
		bind[PriorityEntry]("PriorityEntry", PriorityEntryTableType, PriorityEntryFixedSave, PriorityEntryFixedLoad, PriorityEntryFixedMeasure, PriorityEntryFixedPlan.Count),
		bind[StreamPriorityEntry]("StreamPriorityEntry", StreamPriorityEntryTableType, StreamPriorityEntryFixedSave, StreamPriorityEntryFixedLoad, StreamPriorityEntryFixedMeasure, StreamPriorityEntryFixedPlan.Count),
		bind[RowFieldEntry]("RowFieldEntry", RowFieldEntryTableType, RowFieldEntryFixedSave, RowFieldEntryFixedLoad, RowFieldEntryFixedMeasure, RowFieldEntryFixedPlan.Count),
		bind[RowCardsEntry]("RowCardsEntry", RowCardsEntryTableType, RowCardsEntryFixedSave, RowCardsEntryFixedLoad, RowCardsEntryFixedMeasure, RowCardsEntryFixedPlan.Count),
		bind[FixStateEntry]("FixStateEntry", FixStateEntryTableType, FixStateEntryFixedSave, FixStateEntryFixedLoad, FixStateEntryFixedMeasure, FixStateEntryFixedPlan.Count),
		bind[RowWorkingEntry]("RowWorkingEntry", RowWorkingEntryTableType, RowWorkingEntryFixedSave, RowWorkingEntryFixedLoad, RowWorkingEntryFixedMeasure, RowWorkingEntryFixedPlan.Count),
		bind[ReadCardCounts]("ReadCardCounts", ReadCardCountsTableType, ReadCardCountsFixedSave, ReadCardCountsFixedLoad, ReadCardCountsFixedMeasure, ReadCardCountsFixedPlan.Count),
		bind[ReadsWindowView]("ReadsWindowView", ReadsWindowViewTableType, ReadsWindowViewFixedSave, ReadsWindowViewFixedLoad, ReadsWindowViewFixedMeasure, ReadsWindowViewFixedPlan.Count),
		bind[StreamTransition]("StreamTransition", StreamTransitionTableType, StreamTransitionFixedSave, StreamTransitionFixedLoad, StreamTransitionFixedMeasure, StreamTransitionFixedPlan.Count),
		bind[DecisionDelivery]("DecisionDelivery", DecisionDeliveryTableType, DecisionDeliveryFixedSave, DecisionDeliveryFixedLoad, DecisionDeliveryFixedMeasure, DecisionDeliveryFixedPlan.Count),
		bind[CoordinatorDelivery]("CoordinatorDelivery", CoordinatorDeliveryTableType, CoordinatorDeliveryFixedSave, CoordinatorDeliveryFixedLoad, CoordinatorDeliveryFixedMeasure, CoordinatorDeliveryFixedPlan.Count),
		bind[Note]("Note", NoteTableType, NoteFixedSave, NoteFixedLoad, NoteFixedMeasure, NoteFixedPlan.Count),
		bind[WhereRecord]("WhereRecord", WhereRecordTableType, WhereRecordFixedSave, WhereRecordFixedLoad, WhereRecordFixedMeasure, WhereRecordFixedPlan.Count),
		bind[RttSample]("RttSample", RttSampleTableType, RttSampleFixedSave, RttSampleFixedLoad, RttSampleFixedMeasure, RttSampleFixedPlan.Count),
		bind[StoreRTTRecord]("StoreRTTRecord", StoreRTTRecordTableType, StoreRTTRecordFixedSave, StoreRTTRecordFixedLoad, StoreRTTRecordFixedMeasure, StoreRTTRecordFixedPlan.Count),
		bind[SeatWaits]("SeatWaits", SeatWaitsTableType, SeatWaitsFixedSave, SeatWaitsFixedLoad, SeatWaitsFixedMeasure, SeatWaitsFixedPlan.Count),
		bind[Stop]("Stop", StopTableType, StopFixedSave, StopFixedLoad, StopFixedMeasure, StopFixedPlan.Count),
		bind[StopsRecord]("StopsRecord", StopsRecordTableType, StopsRecordFixedSave, StopsRecordFixedLoad, StopsRecordFixedMeasure, StopsRecordFixedPlan.Count),
		bind[RefusedVerb]("RefusedVerb", RefusedVerbTableType, RefusedVerbFixedSave, RefusedVerbFixedLoad, RefusedVerbFixedMeasure, RefusedVerbFixedPlan.Count),
		bind[RefusedRecord]("RefusedRecord", RefusedRecordTableType, RefusedRecordFixedSave, RefusedRecordFixedLoad, RefusedRecordFixedMeasure, RefusedRecordFixedPlan.Count),
		bind[LoadSample]("LoadSample", LoadSampleTableType, LoadSampleFixedSave, LoadSampleFixedLoad, LoadSampleFixedMeasure, LoadSampleFixedPlan.Count),
		bind[MeterTicks]("MeterTicks", MeterTicksTableType, MeterTicksFixedSave, MeterTicksFixedLoad, MeterTicksFixedMeasure, MeterTicksFixedPlan.Count),
		bind[MeterHolder]("MeterHolder", MeterHolderTableType, MeterHolderFixedSave, MeterHolderFixedLoad, MeterHolderFixedMeasure, MeterHolderFixedPlan.Count),
		bind[MeterFiles]("MeterFiles", MeterFilesTableType, MeterFilesFixedSave, MeterFilesFixedLoad, MeterFilesFixedMeasure, MeterFilesFixedPlan.Count),
		bind[MeterState]("MeterState", MeterStateTableType, MeterStateFixedSave, MeterStateFixedLoad, MeterStateFixedMeasure, MeterStateFixedPlan.Count),
		bind[FriendReport]("FriendReport", FriendReportTableType, FriendReportFixedSave, FriendReportFixedLoad, FriendReportFixedMeasure, FriendReportFixedPlan.Count),
		bind[LiveSeenEntry]("LiveSeenEntry", LiveSeenEntryTableType, LiveSeenEntryFixedSave, LiveSeenEntryFixedLoad, LiveSeenEntryFixedMeasure, LiveSeenEntryFixedPlan.Count),
		bind[Beat]("Beat", BeatTableType, BeatFixedSave, BeatFixedLoad, BeatFixedMeasure, BeatFixedPlan.Count),
		bind[CheckedCard]("CheckedCard", CheckedCardTableType, CheckedCardFixedSave, CheckedCardFixedLoad, CheckedCardFixedMeasure, CheckedCardFixedPlan.Count),
		bind[CheckRecord]("CheckRecord", CheckRecordTableType, CheckRecordFixedSave, CheckRecordFixedLoad, CheckRecordFixedMeasure, CheckRecordFixedPlan.Count),
		bind[FleetMachineRow]("FleetMachineRow", FleetMachineRowTableType, FleetMachineRowFixedSave, FleetMachineRowFixedLoad, FleetMachineRowFixedMeasure, FleetMachineRowFixedPlan.Count),
		bind[FleetDropDebt]("FleetDropDebt", FleetDropDebtTableType, FleetDropDebtFixedSave, FleetDropDebtFixedLoad, FleetDropDebtFixedMeasure, FleetDropDebtFixedPlan.Count),
		bind[ResetRow]("ResetRow", ResetRowTableType, ResetRowFixedSave, ResetRowFixedLoad, ResetRowFixedMeasure, ResetRowFixedPlan.Count),
		bind[ResetRowEntry]("ResetRowEntry", ResetRowEntryTableType, ResetRowEntryFixedSave, ResetRowEntryFixedLoad, ResetRowEntryFixedMeasure, ResetRowEntryFixedPlan.Count),
		bind[ResetStream]("ResetStream", ResetStreamTableType, ResetStreamFixedSave, ResetStreamFixedLoad, ResetStreamFixedMeasure, ResetStreamFixedPlan.Count),
		bind[ResetStreamEntry]("ResetStreamEntry", ResetStreamEntryTableType, ResetStreamEntryFixedSave, ResetStreamEntryFixedLoad, ResetStreamEntryFixedMeasure, ResetStreamEntryFixedPlan.Count),
		bind[ResetMark]("ResetMark", ResetMarkTableType, ResetMarkFixedSave, ResetMarkFixedLoad, ResetMarkFixedMeasure, ResetMarkFixedPlan.Count),
		bind[ResetResult]("ResetResult", ResetResultTableType, ResetResultFixedSave, ResetResultFixedLoad, ResetResultFixedMeasure, ResetResultFixedPlan.Count),
		bind[KindTimeEntry]("KindTimeEntry", KindTimeEntryTableType, KindTimeEntryFixedSave, KindTimeEntryFixedLoad, KindTimeEntryFixedMeasure, KindTimeEntryFixedPlan.Count),
		bind[StreamBase]("StreamBase", StreamBaseTableType, StreamBaseFixedSave, StreamBaseFixedLoad, StreamBaseFixedMeasure, StreamBaseFixedPlan.Count),
		bind[StreamBaseEntry]("StreamBaseEntry", StreamBaseEntryTableType, StreamBaseEntryFixedSave, StreamBaseEntryFixedLoad, StreamBaseEntryFixedMeasure, StreamBaseEntryFixedPlan.Count),
		bind[TidyInFlight]("TidyInFlight", TidyInFlightTableType, TidyInFlightFixedSave, TidyInFlightFixedLoad, TidyInFlightFixedMeasure, TidyInFlightFixedPlan.Count),
		bind[StatsRecord]("StatsRecord", StatsRecordTableType, StatsRecordFixedSave, StatsRecordFixedLoad, StatsRecordFixedMeasure, StatsRecordFixedPlan.Count),
		bind[TidyCard]("TidyCard", TidyCardTableType, TidyCardFixedSave, TidyCardFixedLoad, TidyCardFixedMeasure, TidyCardFixedPlan.Count),
		bind[TidyRow]("TidyRow", TidyRowTableType, TidyRowFixedSave, TidyRowFixedLoad, TidyRowFixedMeasure, TidyRowFixedPlan.Count),
		bind[RoutePrices]("RoutePrices", RoutePricesTableType, RoutePricesFixedSave, RoutePricesFixedLoad, RoutePricesFixedMeasure, RoutePricesFixedPlan.Count),
		bind[Route]("Route", RouteTableType, RouteFixedSave, RouteFixedLoad, RouteFixedMeasure, RouteFixedPlan.Count),
		bind[RouteStat]("RouteStat", RouteStatTableType, RouteStatFixedSave, RouteStatFixedLoad, RouteStatFixedMeasure, RouteStatFixedPlan.Count),
		bind[StatsArchive]("StatsArchive", StatsArchiveTableType, StatsArchiveFixedSave, StatsArchiveFixedLoad, StatsArchiveFixedMeasure, StatsArchiveFixedPlan.Count),
		bind[TidyResult]("TidyResult", TidyResultTableType, TidyResultFixedSave, TidyResultFixedLoad, TidyResultFixedMeasure, TidyResultFixedPlan.Count),
		bind[Stuck]("Stuck", StuckTableType, StuckFixedSave, StuckFixedLoad, StuckFixedMeasure, StuckFixedPlan.Count),
		bind[LogIndexEntry]("LogIndexEntry", LogIndexEntryTableType, LogIndexEntryFixedSave, LogIndexEntryFixedLoad, LogIndexEntryFixedMeasure, LogIndexEntryFixedPlan.Count),
		bind[LogIndex]("LogIndex", LogIndexTableType, LogIndexFixedSave, LogIndexFixedLoad, LogIndexFixedMeasure, LogIndexFixedPlan.Count),
		bind[Span]("Span", SpanTableType, SpanFixedSave, SpanFixedLoad, SpanFixedMeasure, SpanFixedPlan.Count),
		bind[StopLease]("StopLease", StopLeaseTableType, StopLeaseFixedSave, StopLeaseFixedLoad, StopLeaseFixedMeasure, StopLeaseFixedPlan.Count),
		bind[Machine]("Machine", MachineTableType, MachineFixedSave, MachineFixedLoad, MachineFixedMeasure, MachineFixedPlan.Count),
		bind[LaneQuiet]("LaneQuiet", LaneQuietTableType, LaneQuietFixedSave, LaneQuietFixedLoad, LaneQuietFixedMeasure, LaneQuietFixedPlan.Count),
		bind[Suppressed]("Suppressed", SuppressedTableType, SuppressedFixedSave, SuppressedFixedLoad, SuppressedFixedMeasure, SuppressedFixedPlan.Count),
		bind[Heartbeat]("Heartbeat", HeartbeatTableType, HeartbeatFixedSave, HeartbeatFixedLoad, HeartbeatFixedMeasure, HeartbeatFixedPlan.Count),
		bind[StringKV]("StringKV", StringKVTableType, StringKVFixedSave, StringKVFixedLoad, StringKVFixedMeasure, StringKVFixedPlan.Count),
		bind[PlaceExpect]("PlaceExpect", PlaceExpectTableType, PlaceExpectFixedSave, PlaceExpectFixedLoad, PlaceExpectFixedMeasure, PlaceExpectFixedPlan.Count),
		bind[GuardText]("GuardText", GuardTextTableType, GuardTextFixedSave, GuardTextFixedLoad, GuardTextFixedMeasure, GuardTextFixedPlan.Count),
		bind[FieldGuard]("FieldGuard", FieldGuardTableType, FieldGuardFixedSave, FieldGuardFixedLoad, FieldGuardFixedMeasure, FieldGuardFixedPlan.Count),
		bind[FieldGuardEntry]("FieldGuardEntry", FieldGuardEntryTableType, FieldGuardEntryFixedSave, FieldGuardEntryFixedLoad, FieldGuardEntryFixedMeasure, FieldGuardEntryFixedPlan.Count),
		bind[MemberExpect]("MemberExpect", MemberExpectTableType, MemberExpectFixedSave, MemberExpectFixedLoad, MemberExpectFixedMeasure, MemberExpectFixedPlan.Count),
		bind[MemberCreate]("MemberCreate", MemberCreateTableType, MemberCreateFixedSave, MemberCreateFixedLoad, MemberCreateFixedMeasure, MemberCreateFixedPlan.Count),
		bind[MemberMove]("MemberMove", MemberMoveTableType, MemberMoveFixedSave, MemberMoveFixedLoad, MemberMoveFixedMeasure, MemberMoveFixedPlan.Count),
		bind[BatchMemberEntry]("BatchMemberEntry", BatchMemberEntryTableType, BatchMemberEntryFixedSave, BatchMemberEntryFixedLoad, BatchMemberEntryFixedMeasure, BatchMemberEntryFixedPlan.Count),
		bind[PropName]("PropName", PropNameTableType, PropNameFixedSave, PropNameFixedLoad, PropNameFixedMeasure, PropNameFixedPlan.Count),
		bind[BatchManifest]("BatchManifest", BatchManifestTableType, BatchManifestFixedSave, BatchManifestFixedLoad, BatchManifestFixedMeasure, BatchManifestFixedPlan.Count),
		bind[PropWrite]("PropWrite", PropWriteTableType, PropWriteFixedSave, PropWriteFixedLoad, PropWriteFixedMeasure, PropWriteFixedPlan.Count),
		bind[QueuedChange]("QueuedChange", QueuedChangeTableType, QueuedChangeFixedSave, QueuedChangeFixedLoad, QueuedChangeFixedMeasure, QueuedChangeFixedPlan.Count),
		bind[LogLine]("LogLine", LogLineTableType, LogLineFixedSave, LogLineFixedLoad, LogLineFixedMeasure, LogLineFixedPlan.Count),
		bind[Timer]("Timer", TimerTableType, TimerFixedSave, TimerFixedLoad, TimerFixedMeasure, TimerFixedPlan.Count),
		bind[Timers]("Timers", TimersTableType, TimersFixedSave, TimersFixedLoad, TimersFixedMeasure, TimersFixedPlan.Count),
		bind[SeatChange]("SeatChange", SeatChangeTableType, SeatChangeFixedSave, SeatChangeFixedLoad, SeatChangeFixedMeasure, SeatChangeFixedPlan.Count),
		bind[HeldRecord]("HeldRecord", HeldRecordTableType, HeldRecordFixedSave, HeldRecordFixedLoad, HeldRecordFixedMeasure, HeldRecordFixedPlan.Count),
	}
}
