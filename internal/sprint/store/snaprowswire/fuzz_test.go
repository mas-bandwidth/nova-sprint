package snaprowswire

// FuzzSnaprowswire feeds hostile bytes, in three forms (binary wire, padded standard
// base64, JSON text), to every generated root. go test runs the checked-in seeds only;
// nightly: go test -run='^$' -fuzz='^FuzzSnaprowswire$' -fuzztime=5m ./internal/sprint/store/snaprowswire
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
	fuzzBuilder("BackupCounts", func(b *BackupCountsBuilder, w []byte, r *TableReport) bool { return BackupCountsLoadBuilder(b, w, r) }, func(b *BackupCountsBuilder, w []byte, r *TableReport) bool { return BackupCountsFromJson(b, w, r) }, func(v *BackupCounts) []byte {
		return fuzzImage(BackupCountsMeasure(v), func(b []byte) int64 { return BackupCountsSave(v, b) })
	}, func(v *BackupCounts) []byte {
		return fuzzImage(BackupCountsToJsonMeasure(v), func(b []byte) int64 { return BackupCountsToJson(v, b) })
	}),
	fuzzBuilder("BatchDelta", func(b *BatchDeltaBuilder, w []byte, r *TableReport) bool { return BatchDeltaLoadBuilder(b, w, r) }, func(b *BatchDeltaBuilder, w []byte, r *TableReport) bool { return BatchDeltaFromJson(b, w, r) }, func(v *BatchDelta) []byte {
		return fuzzImage(BatchDeltaMeasure(v), func(b []byte) int64 { return BatchDeltaSave(v, b) })
	}, func(v *BatchDelta) []byte {
		return fuzzImage(BatchDeltaToJsonMeasure(v), func(b []byte) int64 { return BatchDeltaToJson(v, b) })
	}),
	fuzzBuilder("BatchMemberDelta", func(b *BatchMemberDeltaBuilder, w []byte, r *TableReport) bool {
		return BatchMemberDeltaLoadBuilder(b, w, r)
	}, func(b *BatchMemberDeltaBuilder, w []byte, r *TableReport) bool {
		return BatchMemberDeltaFromJson(b, w, r)
	}, func(v *BatchMemberDelta) []byte {
		return fuzzImage(BatchMemberDeltaMeasure(v), func(b []byte) int64 { return BatchMemberDeltaSave(v, b) })
	}, func(v *BatchMemberDelta) []byte {
		return fuzzImage(BatchMemberDeltaToJsonMeasure(v), func(b []byte) int64 { return BatchMemberDeltaToJson(v, b) })
	}),
	fuzzBuilder("CardCostView", func(b *CardCostViewBuilder, w []byte, r *TableReport) bool { return CardCostViewLoadBuilder(b, w, r) }, func(b *CardCostViewBuilder, w []byte, r *TableReport) bool { return CardCostViewFromJson(b, w, r) }, func(v *CardCostView) []byte {
		return fuzzImage(CardCostViewMeasure(v), func(b []byte) int64 { return CardCostViewSave(v, b) })
	}, func(v *CardCostView) []byte {
		return fuzzImage(CardCostViewToJsonMeasure(v), func(b []byte) int64 { return CardCostViewToJson(v, b) })
	}),
	fuzzBuilder("Cell", func(b *CellBuilder, w []byte, r *TableReport) bool { return CellLoadBuilder(b, w, r) }, func(b *CellBuilder, w []byte, r *TableReport) bool { return CellFromJson(b, w, r) }, func(v *Cell) []byte { return fuzzImage(CellMeasure(v), func(b []byte) int64 { return CellSave(v, b) }) }, func(v *Cell) []byte {
		return fuzzImage(CellToJsonMeasure(v), func(b []byte) int64 { return CellToJson(v, b) })
	}),
	fuzzBuilder("ChangeSnapshot", func(b *ChangeSnapshotBuilder, w []byte, r *TableReport) bool {
		return ChangeSnapshotLoadBuilder(b, w, r)
	}, func(b *ChangeSnapshotBuilder, w []byte, r *TableReport) bool { return ChangeSnapshotFromJson(b, w, r) }, func(v *ChangeSnapshot) []byte {
		return fuzzImage(ChangeSnapshotMeasure(v), func(b []byte) int64 { return ChangeSnapshotSave(v, b) })
	}, func(v *ChangeSnapshot) []byte {
		return fuzzImage(ChangeSnapshotToJsonMeasure(v), func(b []byte) int64 { return ChangeSnapshotToJson(v, b) })
	}),
	fuzzBuilder("Column", func(b *ColumnBuilder, w []byte, r *TableReport) bool { return ColumnLoadBuilder(b, w, r) }, func(b *ColumnBuilder, w []byte, r *TableReport) bool { return ColumnFromJson(b, w, r) }, func(v *Column) []byte {
		return fuzzImage(ColumnMeasure(v), func(b []byte) int64 { return ColumnSave(v, b) })
	}, func(v *Column) []byte {
		return fuzzImage(ColumnToJsonMeasure(v), func(b []byte) int64 { return ColumnToJson(v, b) })
	}),
	fuzzBuilder("ColumnCount", func(b *ColumnCountBuilder, w []byte, r *TableReport) bool { return ColumnCountLoadBuilder(b, w, r) }, func(b *ColumnCountBuilder, w []byte, r *TableReport) bool { return ColumnCountFromJson(b, w, r) }, func(v *ColumnCount) []byte {
		return fuzzImage(ColumnCountMeasure(v), func(b []byte) int64 { return ColumnCountSave(v, b) })
	}, func(v *ColumnCount) []byte {
		return fuzzImage(ColumnCountToJsonMeasure(v), func(b []byte) int64 { return ColumnCountToJson(v, b) })
	}),
	fuzzBuilder("Consumer", func(b *ConsumerBuilder, w []byte, r *TableReport) bool { return ConsumerLoadBuilder(b, w, r) }, func(b *ConsumerBuilder, w []byte, r *TableReport) bool { return ConsumerFromJson(b, w, r) }, func(v *Consumer) []byte {
		return fuzzImage(ConsumerMeasure(v), func(b []byte) int64 { return ConsumerSave(v, b) })
	}, func(v *Consumer) []byte {
		return fuzzImage(ConsumerToJsonMeasure(v), func(b []byte) int64 { return ConsumerToJson(v, b) })
	}),
	fuzzBuilder("DumpKey", func(b *DumpKeyBuilder, w []byte, r *TableReport) bool { return DumpKeyLoadBuilder(b, w, r) }, func(b *DumpKeyBuilder, w []byte, r *TableReport) bool { return DumpKeyFromJson(b, w, r) }, func(v *DumpKey) []byte {
		return fuzzImage(DumpKeyMeasure(v), func(b []byte) int64 { return DumpKeySave(v, b) })
	}, func(v *DumpKey) []byte {
		return fuzzImage(DumpKeyToJsonMeasure(v), func(b []byte) int64 { return DumpKeyToJson(v, b) })
	}),
	fuzzBuilder("EpochEntry", func(b *EpochEntryBuilder, w []byte, r *TableReport) bool { return EpochEntryLoadBuilder(b, w, r) }, func(b *EpochEntryBuilder, w []byte, r *TableReport) bool { return EpochEntryFromJson(b, w, r) }, func(v *EpochEntry) []byte {
		return fuzzImage(EpochEntryMeasure(v), func(b []byte) int64 { return EpochEntrySave(v, b) })
	}, func(v *EpochEntry) []byte {
		return fuzzImage(EpochEntryToJsonMeasure(v), func(b []byte) int64 { return EpochEntryToJson(v, b) })
	}),
	fuzzBuilder("EpochFlag", func(b *EpochFlagBuilder, w []byte, r *TableReport) bool { return EpochFlagLoadBuilder(b, w, r) }, func(b *EpochFlagBuilder, w []byte, r *TableReport) bool { return EpochFlagFromJson(b, w, r) }, func(v *EpochFlag) []byte {
		return fuzzImage(EpochFlagMeasure(v), func(b []byte) int64 { return EpochFlagSave(v, b) })
	}, func(v *EpochFlag) []byte {
		return fuzzImage(EpochFlagToJsonMeasure(v), func(b []byte) int64 { return EpochFlagToJson(v, b) })
	}),
	fuzzBuilder("EpochSnapshot", func(b *EpochSnapshotBuilder, w []byte, r *TableReport) bool { return EpochSnapshotLoadBuilder(b, w, r) }, func(b *EpochSnapshotBuilder, w []byte, r *TableReport) bool { return EpochSnapshotFromJson(b, w, r) }, func(v *EpochSnapshot) []byte {
		return fuzzImage(EpochSnapshotMeasure(v), func(b []byte) int64 { return EpochSnapshotSave(v, b) })
	}, func(v *EpochSnapshot) []byte {
		return fuzzImage(EpochSnapshotToJsonMeasure(v), func(b []byte) int64 { return EpochSnapshotToJson(v, b) })
	}),
	fuzzBuilder("FieldChange", func(b *FieldChangeBuilder, w []byte, r *TableReport) bool { return FieldChangeLoadBuilder(b, w, r) }, func(b *FieldChangeBuilder, w []byte, r *TableReport) bool { return FieldChangeFromJson(b, w, r) }, func(v *FieldChange) []byte {
		return fuzzImage(FieldChangeMeasure(v), func(b []byte) int64 { return FieldChangeSave(v, b) })
	}, func(v *FieldChange) []byte {
		return fuzzImage(FieldChangeToJsonMeasure(v), func(b []byte) int64 { return FieldChangeToJson(v, b) })
	}),
	fuzzBuilder("Field", func(b *FieldBuilder, w []byte, r *TableReport) bool { return FieldLoadBuilder(b, w, r) }, func(b *FieldBuilder, w []byte, r *TableReport) bool { return FieldFromJson(b, w, r) }, func(v *Field) []byte {
		return fuzzImage(FieldMeasure(v), func(b []byte) int64 { return FieldSave(v, b) })
	}, func(v *Field) []byte {
		return fuzzImage(FieldToJsonMeasure(v), func(b []byte) int64 { return FieldToJson(v, b) })
	}),
	fuzzBuilder("Flag", func(b *FlagBuilder, w []byte, r *TableReport) bool { return FlagLoadBuilder(b, w, r) }, func(b *FlagBuilder, w []byte, r *TableReport) bool { return FlagFromJson(b, w, r) }, func(v *Flag) []byte { return fuzzImage(FlagMeasure(v), func(b []byte) int64 { return FlagSave(v, b) }) }, func(v *Flag) []byte {
		return fuzzImage(FlagToJsonMeasure(v), func(b []byte) int64 { return FlagToJson(v, b) })
	}),
	fuzzBuilder("Image", func(b *ImageBuilder, w []byte, r *TableReport) bool { return ImageLoadBuilder(b, w, r) }, func(b *ImageBuilder, w []byte, r *TableReport) bool { return ImageFromJson(b, w, r) }, func(v *Image) []byte {
		return fuzzImage(ImageMeasure(v), func(b []byte) int64 { return ImageSave(v, b) })
	}, func(v *Image) []byte {
		return fuzzImage(ImageToJsonMeasure(v), func(b []byte) int64 { return ImageToJson(v, b) })
	}),
	fuzzBuilder("LineSnapshot", func(b *LineSnapshotBuilder, w []byte, r *TableReport) bool { return LineSnapshotLoadBuilder(b, w, r) }, func(b *LineSnapshotBuilder, w []byte, r *TableReport) bool { return LineSnapshotFromJson(b, w, r) }, func(v *LineSnapshot) []byte {
		return fuzzImage(LineSnapshotMeasure(v), func(b []byte) int64 { return LineSnapshotSave(v, b) })
	}, func(v *LineSnapshot) []byte {
		return fuzzImage(LineSnapshotToJsonMeasure(v), func(b []byte) int64 { return LineSnapshotToJson(v, b) })
	}),
	fuzzBuilder("LogEntry", func(b *LogEntryBuilder, w []byte, r *TableReport) bool { return LogEntryLoadBuilder(b, w, r) }, func(b *LogEntryBuilder, w []byte, r *TableReport) bool { return LogEntryFromJson(b, w, r) }, func(v *LogEntry) []byte {
		return fuzzImage(LogEntryMeasure(v), func(b []byte) int64 { return LogEntrySave(v, b) })
	}, func(v *LogEntry) []byte {
		return fuzzImage(LogEntryToJsonMeasure(v), func(b []byte) int64 { return LogEntryToJson(v, b) })
	}),
	fuzzBuilder("LogSnapshot", func(b *LogSnapshotBuilder, w []byte, r *TableReport) bool { return LogSnapshotLoadBuilder(b, w, r) }, func(b *LogSnapshotBuilder, w []byte, r *TableReport) bool { return LogSnapshotFromJson(b, w, r) }, func(v *LogSnapshot) []byte {
		return fuzzImage(LogSnapshotMeasure(v), func(b []byte) int64 { return LogSnapshotSave(v, b) })
	}, func(v *LogSnapshot) []byte {
		return fuzzImage(LogSnapshotToJsonMeasure(v), func(b []byte) int64 { return LogSnapshotToJson(v, b) })
	}),
	fuzzBuilder("MemberEntry", func(b *MemberEntryBuilder, w []byte, r *TableReport) bool { return MemberEntryLoadBuilder(b, w, r) }, func(b *MemberEntryBuilder, w []byte, r *TableReport) bool { return MemberEntryFromJson(b, w, r) }, func(v *MemberEntry) []byte {
		return fuzzImage(MemberEntryMeasure(v), func(b []byte) int64 { return MemberEntrySave(v, b) })
	}, func(v *MemberEntry) []byte {
		return fuzzImage(MemberEntryToJsonMeasure(v), func(b []byte) int64 { return MemberEntryToJson(v, b) })
	}),
	fuzzBuilder("MemberSnapshot", func(b *MemberSnapshotBuilder, w []byte, r *TableReport) bool {
		return MemberSnapshotLoadBuilder(b, w, r)
	}, func(b *MemberSnapshotBuilder, w []byte, r *TableReport) bool { return MemberSnapshotFromJson(b, w, r) }, func(v *MemberSnapshot) []byte {
		return fuzzImage(MemberSnapshotMeasure(v), func(b []byte) int64 { return MemberSnapshotSave(v, b) })
	}, func(v *MemberSnapshot) []byte {
		return fuzzImage(MemberSnapshotToJsonMeasure(v), func(b []byte) int64 { return MemberSnapshotToJson(v, b) })
	}),
	fuzzBuilder("MemDumpMember", func(b *MemDumpMemberBuilder, w []byte, r *TableReport) bool { return MemDumpMemberLoadBuilder(b, w, r) }, func(b *MemDumpMemberBuilder, w []byte, r *TableReport) bool { return MemDumpMemberFromJson(b, w, r) }, func(v *MemDumpMember) []byte {
		return fuzzImage(MemDumpMemberMeasure(v), func(b []byte) int64 { return MemDumpMemberSave(v, b) })
	}, func(v *MemDumpMember) []byte {
		return fuzzImage(MemDumpMemberToJsonMeasure(v), func(b []byte) int64 { return MemDumpMemberToJson(v, b) })
	}),
	fuzzBuilder("MemSnapshot", func(b *MemSnapshotBuilder, w []byte, r *TableReport) bool { return MemSnapshotLoadBuilder(b, w, r) }, func(b *MemSnapshotBuilder, w []byte, r *TableReport) bool { return MemSnapshotFromJson(b, w, r) }, func(v *MemSnapshot) []byte {
		return fuzzImage(MemSnapshotMeasure(v), func(b []byte) int64 { return MemSnapshotSave(v, b) })
	}, func(v *MemSnapshot) []byte {
		return fuzzImage(MemSnapshotToJsonMeasure(v), func(b []byte) int64 { return MemSnapshotToJson(v, b) })
	}),
	fuzzBuilder("NoteEntry", func(b *NoteEntryBuilder, w []byte, r *TableReport) bool { return NoteEntryLoadBuilder(b, w, r) }, func(b *NoteEntryBuilder, w []byte, r *TableReport) bool { return NoteEntryFromJson(b, w, r) }, func(v *NoteEntry) []byte {
		return fuzzImage(NoteEntryMeasure(v), func(b []byte) int64 { return NoteEntrySave(v, b) })
	}, func(v *NoteEntry) []byte {
		return fuzzImage(NoteEntryToJsonMeasure(v), func(b []byte) int64 { return NoteEntryToJson(v, b) })
	}),
	fuzzBuilder("NoteSnapshot", func(b *NoteSnapshotBuilder, w []byte, r *TableReport) bool { return NoteSnapshotLoadBuilder(b, w, r) }, func(b *NoteSnapshotBuilder, w []byte, r *TableReport) bool { return NoteSnapshotFromJson(b, w, r) }, func(v *NoteSnapshot) []byte {
		return fuzzImage(NoteSnapshotMeasure(v), func(b []byte) int64 { return NoteSnapshotSave(v, b) })
	}, func(v *NoteSnapshot) []byte {
		return fuzzImage(NoteSnapshotToJsonMeasure(v), func(b []byte) int64 { return NoteSnapshotToJson(v, b) })
	}),
	fuzzBuilder("NTable", func(b *NTableBuilder, w []byte, r *TableReport) bool { return NTableLoadBuilder(b, w, r) }, func(b *NTableBuilder, w []byte, r *TableReport) bool { return NTableFromJson(b, w, r) }, func(v *NTable) []byte {
		return fuzzImage(NTableMeasure(v), func(b []byte) int64 { return NTableSave(v, b) })
	}, func(v *NTable) []byte {
		return fuzzImage(NTableToJsonMeasure(v), func(b []byte) int64 { return NTableToJson(v, b) })
	}),
	fuzzBuilder("OpEntry", func(b *OpEntryBuilder, w []byte, r *TableReport) bool { return OpEntryLoadBuilder(b, w, r) }, func(b *OpEntryBuilder, w []byte, r *TableReport) bool { return OpEntryFromJson(b, w, r) }, func(v *OpEntry) []byte {
		return fuzzImage(OpEntryMeasure(v), func(b []byte) int64 { return OpEntrySave(v, b) })
	}, func(v *OpEntry) []byte {
		return fuzzImage(OpEntryToJsonMeasure(v), func(b []byte) int64 { return OpEntryToJson(v, b) })
	}),
	fuzzBuilder("Pair", func(b *PairBuilder, w []byte, r *TableReport) bool { return PairLoadBuilder(b, w, r) }, func(b *PairBuilder, w []byte, r *TableReport) bool { return PairFromJson(b, w, r) }, func(v *Pair) []byte { return fuzzImage(PairMeasure(v), func(b []byte) int64 { return PairSave(v, b) }) }, func(v *Pair) []byte {
		return fuzzImage(PairToJsonMeasure(v), func(b []byte) int64 { return PairToJson(v, b) })
	}),
	fuzzBuilder("RawRecord", func(b *RawRecordBuilder, w []byte, r *TableReport) bool { return RawRecordLoadBuilder(b, w, r) }, func(b *RawRecordBuilder, w []byte, r *TableReport) bool { return RawRecordFromJson(b, w, r) }, func(v *RawRecord) []byte {
		return fuzzImage(RawRecordMeasure(v), func(b []byte) int64 { return RawRecordSave(v, b) })
	}, func(v *RawRecord) []byte {
		return fuzzImage(RawRecordToJsonMeasure(v), func(b []byte) int64 { return RawRecordToJson(v, b) })
	}),
	fuzzBuilder("RawUsage", func(b *RawUsageBuilder, w []byte, r *TableReport) bool { return RawUsageLoadBuilder(b, w, r) }, func(b *RawUsageBuilder, w []byte, r *TableReport) bool { return RawUsageFromJson(b, w, r) }, func(v *RawUsage) []byte {
		return fuzzImage(RawUsageMeasure(v), func(b []byte) int64 { return RawUsageSave(v, b) })
	}, func(v *RawUsage) []byte {
		return fuzzImage(RawUsageToJsonMeasure(v), func(b []byte) int64 { return RawUsageToJson(v, b) })
	}),
	fuzzBuilder("Receipt", func(b *ReceiptBuilder, w []byte, r *TableReport) bool { return ReceiptLoadBuilder(b, w, r) }, func(b *ReceiptBuilder, w []byte, r *TableReport) bool { return ReceiptFromJson(b, w, r) }, func(v *Receipt) []byte {
		return fuzzImage(ReceiptMeasure(v), func(b []byte) int64 { return ReceiptSave(v, b) })
	}, func(v *Receipt) []byte {
		return fuzzImage(ReceiptToJsonMeasure(v), func(b []byte) int64 { return ReceiptToJson(v, b) })
	}),
	fuzzBuilder("ResidueEntry", func(b *ResidueEntryBuilder, w []byte, r *TableReport) bool { return ResidueEntryLoadBuilder(b, w, r) }, func(b *ResidueEntryBuilder, w []byte, r *TableReport) bool { return ResidueEntryFromJson(b, w, r) }, func(v *ResidueEntry) []byte {
		return fuzzImage(ResidueEntryMeasure(v), func(b []byte) int64 { return ResidueEntrySave(v, b) })
	}, func(v *ResidueEntry) []byte {
		return fuzzImage(ResidueEntryToJsonMeasure(v), func(b []byte) int64 { return ResidueEntryToJson(v, b) })
	}),
	fuzzBuilder("ResidueSnap", func(b *ResidueSnapBuilder, w []byte, r *TableReport) bool { return ResidueSnapLoadBuilder(b, w, r) }, func(b *ResidueSnapBuilder, w []byte, r *TableReport) bool { return ResidueSnapFromJson(b, w, r) }, func(v *ResidueSnap) []byte {
		return fuzzImage(ResidueSnapMeasure(v), func(b []byte) int64 { return ResidueSnapSave(v, b) })
	}, func(v *ResidueSnap) []byte {
		return fuzzImage(ResidueSnapToJsonMeasure(v), func(b []byte) int64 { return ResidueSnapToJson(v, b) })
	}),
	fuzzBuilder("Row", func(b *RowBuilder, w []byte, r *TableReport) bool { return RowLoadBuilder(b, w, r) }, func(b *RowBuilder, w []byte, r *TableReport) bool { return RowFromJson(b, w, r) }, func(v *Row) []byte { return fuzzImage(RowMeasure(v), func(b []byte) int64 { return RowSave(v, b) }) }, func(v *Row) []byte {
		return fuzzImage(RowToJsonMeasure(v), func(b []byte) int64 { return RowToJson(v, b) })
	}),
	fuzzBuilder("ScoredMember", func(b *ScoredMemberBuilder, w []byte, r *TableReport) bool { return ScoredMemberLoadBuilder(b, w, r) }, func(b *ScoredMemberBuilder, w []byte, r *TableReport) bool { return ScoredMemberFromJson(b, w, r) }, func(v *ScoredMember) []byte {
		return fuzzImage(ScoredMemberMeasure(v), func(b []byte) int64 { return ScoredMemberSave(v, b) })
	}, func(v *ScoredMember) []byte {
		return fuzzImage(ScoredMemberToJsonMeasure(v), func(b []byte) int64 { return ScoredMemberToJson(v, b) })
	}),
	fuzzValue("SnapshotCounts", func(v *SnapshotCounts, w []byte, r *TableReport) bool { return SnapshotCountsLoad(v, w, r) }, func(v *SnapshotCounts, w []byte, r *TableReport) bool { return SnapshotCountsFromJson(v, w, r) }, func(v *SnapshotCounts) []byte {
		return fuzzImage(SnapshotCountsMeasure(v), func(b []byte) int64 { return SnapshotCountsSave(v, b) })
	}, func(v *SnapshotCounts) []byte {
		return fuzzImage(SnapshotCountsToJsonMeasure(v), func(b []byte) int64 { return SnapshotCountsToJson(v, b) })
	}),
	fuzzBuilder("SnapshotTaken", func(b *SnapshotTakenBuilder, w []byte, r *TableReport) bool { return SnapshotTakenLoadBuilder(b, w, r) }, func(b *SnapshotTakenBuilder, w []byte, r *TableReport) bool { return SnapshotTakenFromJson(b, w, r) }, func(v *SnapshotTaken) []byte {
		return fuzzImage(SnapshotTakenMeasure(v), func(b []byte) int64 { return SnapshotTakenSave(v, b) })
	}, func(v *SnapshotTaken) []byte {
		return fuzzImage(SnapshotTakenToJsonMeasure(v), func(b []byte) int64 { return SnapshotTakenToJson(v, b) })
	}),
	fuzzBuilder("SprintState", func(b *SprintStateBuilder, w []byte, r *TableReport) bool { return SprintStateLoadBuilder(b, w, r) }, func(b *SprintStateBuilder, w []byte, r *TableReport) bool { return SprintStateFromJson(b, w, r) }, func(v *SprintState) []byte {
		return fuzzImage(SprintStateMeasure(v), func(b []byte) int64 { return SprintStateSave(v, b) })
	}, func(v *SprintState) []byte {
		return fuzzImage(SprintStateToJsonMeasure(v), func(b []byte) int64 { return SprintStateToJson(v, b) })
	}),
	fuzzBuilder("StatePart", func(b *StatePartBuilder, w []byte, r *TableReport) bool { return StatePartLoadBuilder(b, w, r) }, func(b *StatePartBuilder, w []byte, r *TableReport) bool { return StatePartFromJson(b, w, r) }, func(v *StatePart) []byte {
		return fuzzImage(StatePartMeasure(v), func(b []byte) int64 { return StatePartSave(v, b) })
	}, func(v *StatePart) []byte {
		return fuzzImage(StatePartToJsonMeasure(v), func(b []byte) int64 { return StatePartToJson(v, b) })
	}),
	fuzzBuilder("TableEntry", func(b *TableEntryBuilder, w []byte, r *TableReport) bool { return TableEntryLoadBuilder(b, w, r) }, func(b *TableEntryBuilder, w []byte, r *TableReport) bool { return TableEntryFromJson(b, w, r) }, func(v *TableEntry) []byte {
		return fuzzImage(TableEntryMeasure(v), func(b []byte) int64 { return TableEntrySave(v, b) })
	}, func(v *TableEntry) []byte {
		return fuzzImage(TableEntryToJsonMeasure(v), func(b []byte) int64 { return TableEntryToJson(v, b) })
	}),
	fuzzBuilder("TableSnapshot", func(b *TableSnapshotBuilder, w []byte, r *TableReport) bool { return TableSnapshotLoadBuilder(b, w, r) }, func(b *TableSnapshotBuilder, w []byte, r *TableReport) bool { return TableSnapshotFromJson(b, w, r) }, func(v *TableSnapshot) []byte {
		return fuzzImage(TableSnapshotMeasure(v), func(b []byte) int64 { return TableSnapshotSave(v, b) })
	}, func(v *TableSnapshot) []byte {
		return fuzzImage(TableSnapshotToJsonMeasure(v), func(b []byte) int64 { return TableSnapshotToJson(v, b) })
	}),
	fuzzBuilder("TextRow", func(b *TextRowBuilder, w []byte, r *TableReport) bool { return TextRowLoadBuilder(b, w, r) }, func(b *TextRowBuilder, w []byte, r *TableReport) bool { return TextRowFromJson(b, w, r) }, func(v *TextRow) []byte {
		return fuzzImage(TextRowMeasure(v), func(b []byte) int64 { return TextRowSave(v, b) })
	}, func(v *TextRow) []byte {
		return fuzzImage(TextRowToJsonMeasure(v), func(b []byte) int64 { return TextRowToJson(v, b) })
	}),
	fuzzBuilder("TimeEntry", func(b *TimeEntryBuilder, w []byte, r *TableReport) bool { return TimeEntryLoadBuilder(b, w, r) }, func(b *TimeEntryBuilder, w []byte, r *TableReport) bool { return TimeEntryFromJson(b, w, r) }, func(v *TimeEntry) []byte {
		return fuzzImage(TimeEntryMeasure(v), func(b []byte) int64 { return TimeEntrySave(v, b) })
	}, func(v *TimeEntry) []byte {
		return fuzzImage(TimeEntryToJsonMeasure(v), func(b []byte) int64 { return TimeEntryToJson(v, b) })
	}),
	fuzzBuilder("TokenCount", func(b *TokenCountBuilder, w []byte, r *TableReport) bool { return TokenCountLoadBuilder(b, w, r) }, func(b *TokenCountBuilder, w []byte, r *TableReport) bool { return TokenCountFromJson(b, w, r) }, func(v *TokenCount) []byte {
		return fuzzImage(TokenCountMeasure(v), func(b []byte) int64 { return TokenCountSave(v, b) })
	}, func(v *TokenCount) []byte {
		return fuzzImage(TokenCountToJsonMeasure(v), func(b []byte) int64 { return TokenCountToJson(v, b) })
	}),
	fuzzValue("Tokens", func(v *Tokens, w []byte, r *TableReport) bool { return TokensLoad(v, w, r) }, func(v *Tokens, w []byte, r *TableReport) bool { return TokensFromJson(v, w, r) }, func(v *Tokens) []byte {
		return fuzzImage(TokensMeasure(v), func(b []byte) int64 { return TokensSave(v, b) })
	}, func(v *Tokens) []byte {
		return fuzzImage(TokensToJsonMeasure(v), func(b []byte) int64 { return TokensToJson(v, b) })
	}),
	fuzzBuilder("Total", func(b *TotalBuilder, w []byte, r *TableReport) bool { return TotalLoadBuilder(b, w, r) }, func(b *TotalBuilder, w []byte, r *TableReport) bool { return TotalFromJson(b, w, r) }, func(v *Total) []byte {
		return fuzzImage(TotalMeasure(v), func(b []byte) int64 { return TotalSave(v, b) })
	}, func(v *Total) []byte {
		return fuzzImage(TotalToJsonMeasure(v), func(b []byte) int64 { return TotalToJson(v, b) })
	}),
	fuzzBuilder("Usage", func(b *UsageBuilder, w []byte, r *TableReport) bool { return UsageLoadBuilder(b, w, r) }, func(b *UsageBuilder, w []byte, r *TableReport) bool { return UsageFromJson(b, w, r) }, func(v *Usage) []byte {
		return fuzzImage(UsageMeasure(v), func(b []byte) int64 { return UsageSave(v, b) })
	}, func(v *Usage) []byte {
		return fuzzImage(UsageToJsonMeasure(v), func(b []byte) int64 { return UsageToJson(v, b) })
	}),
	fuzzBuilder("ViewEntry", func(b *ViewEntryBuilder, w []byte, r *TableReport) bool { return ViewEntryLoadBuilder(b, w, r) }, func(b *ViewEntryBuilder, w []byte, r *TableReport) bool { return ViewEntryFromJson(b, w, r) }, func(v *ViewEntry) []byte {
		return fuzzImage(ViewEntryMeasure(v), func(b []byte) int64 { return ViewEntrySave(v, b) })
	}, func(v *ViewEntry) []byte {
		return fuzzImage(ViewEntryToJsonMeasure(v), func(b []byte) int64 { return ViewEntryToJson(v, b) })
	}),
	fuzzBuilder("View", func(b *ViewBuilder, w []byte, r *TableReport) bool { return ViewLoadBuilder(b, w, r) }, func(b *ViewBuilder, w []byte, r *TableReport) bool { return ViewFromJson(b, w, r) }, func(v *View) []byte { return fuzzImage(ViewMeasure(v), func(b []byte) int64 { return ViewSave(v, b) }) }, func(v *View) []byte {
		return fuzzImage(ViewToJsonMeasure(v), func(b []byte) int64 { return ViewToJson(v, b) })
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
	corpus, others := fuzzImages(t, filepath.Join("..", "..", "..", "..", "schemas", "snaprows", "fuzz", "*")), fuzzImages(t)
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

func FuzzSnaprowswire(f *testing.F) {
	fuzzSeedList(f) // fills fuzzSeeds; the seeds themselves are the checked-in files
	f.Fuzz(func(t *testing.T, sel uint8, in []byte) {
		root := fuzzRoots[int(sel)%len(fuzzRoots)]
		for form := fuzzWire; form <= fuzzJSON; form++ {
			wire, text, rep, err := root.load(form, in)
			if err != nil {
				require.True(t, strings.HasPrefix(fmt.Sprintf("%T", err), "snaprowswire.Table"), "%s as %d %q: a refusal with no name: %T %v", root.name, form, in, err, err)
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

// TestWriteFuzzSnaprowswireSeeds writes testdata/fuzz/FuzzSnaprowswire when NOVA_FUZZ_WRITE_SEEDS=1.
func TestWriteFuzzSnaprowswireSeeds(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_FUZZ_WRITE_SEEDS") != "1" {
		t.Skip("set NOVA_FUZZ_WRITE_SEEDS=1 to write testdata/fuzz/FuzzSnaprowswire")
	}
	dir := filepath.Join("testdata", "fuzz", "FuzzSnaprowswire")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	sels, ins := fuzzSeedList(t)
	for i := range ins {
		body := fmt.Sprintf("go test fuzz v1\nbyte('\\x%02x')\n[]byte(%s)\n", sels[i], strconv.Quote(string(ins[i])))
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("seed-%04d", i)), []byte(body), 0o644))
	}
}
