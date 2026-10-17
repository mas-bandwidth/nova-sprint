package snaprowswire_test

// TestSnaprowsTextListsArePlain pins that every list of texts in the snaprows
// definitions is the plain text list the Go source holds ([]string): the JSON
// text form reads ["alpha","beta"], not a list of {"text":...} tables, and the
// binary wire carries the same two texts back out as the same JSON.

import (
	"encoding/json"
	"reflect"
	"testing"

	wire "github.com/mas-bandwidth/nova-sprint/internal/sprint/store/snaprowswire"
)

func TestSnaprowsTextListsArePlain(t *testing.T) {
	t.Parallel()
	t.Run("SnapshotTaken.pruned", func(t *testing.T) {
		t.Parallel()
		plainTextList(t, "SnapshotTaken", "pruned",
			(*wire.SnapshotTakenBuilder).Init,
			(*wire.SnapshotTakenBuilder).GetRoot,
			func(b *wire.SnapshotTakenBuilder) *wire.TableArena { return &b.Arena },
			wire.SnapshotTakenFromJson,
			wire.SnapshotTakenMeasure,
			wire.SnapshotTakenSave,
			wire.SnapshotTakenLoadMeasureReason,
			wire.SnapshotTakenLoad,
			wire.SnapshotTakenToJsonMeasure,
			wire.SnapshotTakenToJson,
		)
	})
	t.Run("NTable.hidden", func(t *testing.T) {
		t.Parallel()
		plainTextList(t, "NTable", "hidden",
			(*wire.NTableBuilder).Init,
			(*wire.NTableBuilder).GetRoot,
			func(b *wire.NTableBuilder) *wire.TableArena { return &b.Arena },
			wire.NTableFromJson,
			wire.NTableMeasure,
			wire.NTableSave,
			wire.NTableLoadMeasureReason,
			wire.NTableLoad,
			wire.NTableToJsonMeasure,
			wire.NTableToJson,
		)
	})
	t.Run("View.tables", func(t *testing.T) {
		t.Parallel()
		plainTextList(t, "View", "tables",
			(*wire.ViewBuilder).Init,
			(*wire.ViewBuilder).GetRoot,
			func(b *wire.ViewBuilder) *wire.TableArena { return &b.Arena },
			wire.ViewFromJson,
			wire.ViewMeasure,
			wire.ViewSave,
			wire.ViewLoadMeasureReason,
			wire.ViewLoad,
			wire.ViewToJsonMeasure,
			wire.ViewToJson,
		)
	})
	t.Run("BatchMemberDelta.fields_unset", func(t *testing.T) {
		t.Parallel()
		plainTextList(t, "BatchMemberDelta", "fields_unset",
			(*wire.BatchMemberDeltaBuilder).Init,
			(*wire.BatchMemberDeltaBuilder).GetRoot,
			func(b *wire.BatchMemberDeltaBuilder) *wire.TableArena { return &b.Arena },
			wire.BatchMemberDeltaFromJson,
			wire.BatchMemberDeltaMeasure,
			wire.BatchMemberDeltaSave,
			wire.BatchMemberDeltaLoadMeasureReason,
			wire.BatchMemberDeltaLoad,
			wire.BatchMemberDeltaToJsonMeasure,
			wire.BatchMemberDeltaToJson,
		)
	})
	t.Run("ChangeSnapshot.ids", func(t *testing.T) {
		t.Parallel()
		plainTextList(t, "ChangeSnapshot", "ids",
			(*wire.ChangeSnapshotBuilder).Init,
			(*wire.ChangeSnapshotBuilder).GetRoot,
			func(b *wire.ChangeSnapshotBuilder) *wire.TableArena { return &b.Arena },
			wire.ChangeSnapshotFromJson,
			wire.ChangeSnapshotMeasure,
			wire.ChangeSnapshotSave,
			wire.ChangeSnapshotLoadMeasureReason,
			wire.ChangeSnapshotLoad,
			wire.ChangeSnapshotToJsonMeasure,
			wire.ChangeSnapshotToJson,
		)
	})
	t.Run("EpochSnapshot.rows", func(t *testing.T) {
		t.Parallel()
		plainTextList(t, "EpochSnapshot", "rows",
			(*wire.EpochSnapshotBuilder).Init,
			(*wire.EpochSnapshotBuilder).GetRoot,
			func(b *wire.EpochSnapshotBuilder) *wire.TableArena { return &b.Arena },
			wire.EpochSnapshotFromJson,
			wire.EpochSnapshotMeasure,
			wire.EpochSnapshotSave,
			wire.EpochSnapshotLoadMeasureReason,
			wire.EpochSnapshotLoad,
			wire.EpochSnapshotToJsonMeasure,
			wire.EpochSnapshotToJson,
		)
	})
}

func plainTextList[B any, T any](t *testing.T, table, field string,
	init func(*B, ...wire.TableAllocator) bool,
	root func(*B) *T,
	arena func(*B) *wire.TableArena,
	from func(*B, []byte, *wire.TableReport) bool,
	measure func(*T, ...wire.TableWriteContext) int64,
	save func(*T, []byte, ...wire.TableWriteContext) int64,
	loadMeasure func([]byte) (int64, error),
	load func([]byte, []byte, *wire.TableReport) *T,
	jsonMeasure func(*T, ...wire.TableAllocator) int64,
	toJSON func(*T, []byte, ...wire.TableAllocator) int64,
) {
	t.Helper()
	b := new(B)
	if !init(b) {
		t.Fatalf("%s: the builder starts", table)
	}
	text := []byte(`{"` + field + `":["alpha","beta"]}`)
	var report wire.TableReport
	if !from(b, text, &report) {
		t.Fatalf("%s.%s: FromJson refused %s", table, field, text)
	}
	if report.KindMismatch != 0 || report.Clamped != 0 || report.Malformed {
		t.Fatalf("%s.%s: FromJson report KindMismatch=%d Clamped=%d Malformed=%v reason=%q",
			table, field, report.KindMismatch, report.Clamped, report.Malformed, report.Reason)
	}

	r := root(b)
	a := arena(b)
	n := measure(r, a)
	buf := make([]byte, n)
	if got := save(r, buf, a); got != n {
		t.Fatalf("%s.%s: Save wrote %d bytes, Measure said %d", table, field, got, n)
	}

	rn, err := loadMeasure(buf)
	if err != nil {
		t.Fatalf("%s.%s: LoadMeasureReason refused the wire: %v", table, field, err)
	}
	var read wire.TableReport
	loaded := load(make([]byte, rn), buf, &read)
	if loaded == nil {
		t.Fatalf("%s.%s: Load returned nothing", table, field)
	}

	out := make([]byte, jsonMeasure(loaded))
	out = out[:toJSON(loaded, out)]
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("%s.%s: ToJson is not JSON text: %v (%q)", table, field, err, out)
	}
	want := []any{"alpha", "beta"}
	if got := doc[field]; !reflect.DeepEqual(got, want) {
		t.Fatalf("%s.%s: ToJson holds %#v, want %#v (%q)", table, field, got, want, out)
	}
}
