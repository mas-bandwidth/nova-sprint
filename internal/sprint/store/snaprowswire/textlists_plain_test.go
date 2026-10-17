package snaprowswire_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	wire "github.com/mas-bandwidth/nova-sprint/internal/sprint/store/snaprowswire"
)

// plainTextList reads {"<field>":["alpha","beta"]} into table X and pins that the
// text list is plain: FromJson accepts the list of strings (no kind mismatch, no
// clamping, no malformed framing), and the value saved to the wire with the arena
// as the write context and loaded back ToJson the same list of strings.
func plainTextList[B any, T any](t *testing.T, table, field string, values []string,
	init func(*B, ...wire.TableAllocator) bool,
	root func(*B) *T,
	arena func(*B) *wire.TableArena,
	from func(*B, []byte, *wire.TableReport) bool,
	measure func(*T, ...wire.TableWriteContext) int64,
	save func(*T, []byte, ...wire.TableWriteContext) int64,
	loadMeasure func([]byte) (int64, error),
	load func(region, wire []byte, rep *wire.TableReport) *T,
	jsonMeasure func(*T, ...wire.TableAllocator) int64,
	toJSON func(*T, []byte, ...wire.TableAllocator) int64) {
	t.Helper()
	if values == nil {
		values = []string{"alpha", "beta"}
	}
	encoded, err := json.Marshal(map[string][]string{field: values})
	if err != nil {
		t.Fatalf("%s.%s: encode input: %v", table, field, err)
	}
	text := string(encoded)
	b := new(B)
	if !init(b) {
		t.Fatalf("%s: builder init failed", table)
	}
	var rep wire.TableReport
	if !from(b, []byte(text), &rep) {
		t.Fatalf("%s.%s: FromJson %q rejected a plain list", table, field, text)
	}
	if rep.KindMismatch != 0 || rep.Clamped != 0 || rep.Malformed {
		t.Fatalf("%s.%s: FromJson %q report=%+v", table, field, text, rep)
	}
	ar := arena(b)
	r := root(b)
	buf := make([]byte, measure(r, ar))
	n := save(r, buf, ar)
	if n < 0 {
		t.Fatalf("%s.%s: save refused", table, field)
	}
	w := buf[:n]
	ln, err := loadMeasure(w)
	if err != nil {
		t.Fatalf("%s.%s: loadMeasure: %v", table, field, err)
	}
	var lrep wire.TableReport
	lv := load(make([]byte, ln), w, &lrep)
	if lv == nil {
		t.Fatalf("%s.%s: load returned nothing", table, field)
	}
	jb := make([]byte, jsonMeasure(lv))
	m := toJSON(lv, jb)
	var got map[string]any
	if err := json.Unmarshal(jb[:m], &got); err != nil {
		t.Fatalf("%s.%s: ToJson decode: %v", table, field, err)
	}
	want := make([]any, len(values))
	for i, v := range values {
		want[i] = v
	}
	if !reflect.DeepEqual(got[field], want) {
		t.Fatalf("%s.%s: ToJson %v, want %v at %s", table, field, got[field], want, field)
	}
}

func TestSnaprowsTextListsArePlain(t *testing.T) {
	t.Parallel()
	cases := []struct {
		table string
		field string
	}{
		{"SnapshotTaken", "pruned"},
		{"NTable", "hidden"},
		{"View", "tables"},
		{"BatchMemberDelta", "fields_unset"},
		{"ChangeSnapshot", "ids"},
		{"EpochSnapshot", "rows"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.table+"."+c.field, func(t *testing.T) {
			t.Parallel()
			switch c.table {
			case "SnapshotTaken":
				plainTextList(t, c.table, c.field, nil,
					(*wire.SnapshotTakenBuilder).Init, (*wire.SnapshotTakenBuilder).GetRoot,
					func(b *wire.SnapshotTakenBuilder) *wire.TableArena { return &b.Arena },
					wire.SnapshotTakenFromJson, wire.SnapshotTakenMeasure, wire.SnapshotTakenSave,
					wire.SnapshotTakenLoadMeasureReason, wire.SnapshotTakenLoad,
					wire.SnapshotTakenToJsonMeasure, wire.SnapshotTakenToJson)
			case "NTable":
				plainTextList(t, c.table, c.field, nil,
					(*wire.NTableBuilder).Init, (*wire.NTableBuilder).GetRoot,
					func(b *wire.NTableBuilder) *wire.TableArena { return &b.Arena },
					wire.NTableFromJson, wire.NTableMeasure, wire.NTableSave,
					wire.NTableLoadMeasureReason, wire.NTableLoad,
					wire.NTableToJsonMeasure, wire.NTableToJson)
			case "View":
				plainTextList(t, c.table, c.field, nil,
					(*wire.ViewBuilder).Init, (*wire.ViewBuilder).GetRoot,
					func(b *wire.ViewBuilder) *wire.TableArena { return &b.Arena },
					wire.ViewFromJson, wire.ViewMeasure, wire.ViewSave,
					wire.ViewLoadMeasureReason, wire.ViewLoad,
					wire.ViewToJsonMeasure, wire.ViewToJson)
			case "BatchMemberDelta":
				plainTextList(t, c.table, c.field, nil,
					(*wire.BatchMemberDeltaBuilder).Init, (*wire.BatchMemberDeltaBuilder).GetRoot,
					func(b *wire.BatchMemberDeltaBuilder) *wire.TableArena { return &b.Arena },
					wire.BatchMemberDeltaFromJson, wire.BatchMemberDeltaMeasure, wire.BatchMemberDeltaSave,
					wire.BatchMemberDeltaLoadMeasureReason, wire.BatchMemberDeltaLoad,
					wire.BatchMemberDeltaToJsonMeasure, wire.BatchMemberDeltaToJson)
			case "ChangeSnapshot":
				plainTextList(t, c.table, c.field, nil,
					(*wire.ChangeSnapshotBuilder).Init, (*wire.ChangeSnapshotBuilder).GetRoot,
					func(b *wire.ChangeSnapshotBuilder) *wire.TableArena { return &b.Arena },
					wire.ChangeSnapshotFromJson, wire.ChangeSnapshotMeasure, wire.ChangeSnapshotSave,
					wire.ChangeSnapshotLoadMeasureReason, wire.ChangeSnapshotLoad,
					wire.ChangeSnapshotToJsonMeasure, wire.ChangeSnapshotToJson)
			case "EpochSnapshot":
				plainTextList(t, c.table, c.field, nil,
					(*wire.EpochSnapshotBuilder).Init, (*wire.EpochSnapshotBuilder).GetRoot,
					func(b *wire.EpochSnapshotBuilder) *wire.TableArena { return &b.Arena },
					wire.EpochSnapshotFromJson, wire.EpochSnapshotMeasure, wire.EpochSnapshotSave,
					wire.EpochSnapshotLoadMeasureReason, wire.EpochSnapshotLoad,
					wire.EpochSnapshotToJsonMeasure, wire.EpochSnapshotToJson)
			}
		})
	}
	t.Run("SnapshotTaken.pruned-long", func(t *testing.T) {
		t.Parallel()
		plainTextList(t, "SnapshotTaken", "pruned",
			[]string{strings.Repeat("é-long text of the area ", 4000)},
			(*wire.SnapshotTakenBuilder).Init, (*wire.SnapshotTakenBuilder).GetRoot,
			func(b *wire.SnapshotTakenBuilder) *wire.TableArena { return &b.Arena },
			wire.SnapshotTakenFromJson, wire.SnapshotTakenMeasure, wire.SnapshotTakenSave,
			wire.SnapshotTakenLoadMeasureReason, wire.SnapshotTakenLoad,
			wire.SnapshotTakenToJsonMeasure, wire.SnapshotTakenToJson)
	})
}
