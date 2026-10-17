package friendrowswire_test

import (
	"encoding/json"
	"reflect"
	"testing"

	wire "github.com/mas-bandwidth/nova-sprint/internal/sprint/store/friendrowswire"
)

// TestFriendrowsTextListsArePlain pins that a friend's running list, a beat's
// live set and its facts are the plain JSON list of strings the Go source
// already holds, ["a","b"], not a list of one-field wrapper tables
// (SPEC-TABLES 2.13.1). Each field reads such a list through the generated
// reader with no kind mismatch, clamp or malformed report, saves it to the
// wire, loads it back and writes the same list as text.
func TestFriendrowsTextListsArePlain(t *testing.T) {
	t.Parallel()

	cases := []textListCase{
		{"FriendReport", "running", textListOps((*wire.FriendReportBuilder).Init, (*wire.FriendReportBuilder).GetRoot, func(b *wire.FriendReportBuilder) *wire.TableArena { return &b.Arena }, wire.FriendReportFromJson, wire.FriendReportMeasure, wire.FriendReportSave, wire.FriendReportLoadMeasureReason, wire.FriendReportLoad, wire.FriendReportToJsonMeasure, wire.FriendReportToJson)},
		{"Beat", "live", textListOps((*wire.BeatBuilder).Init, (*wire.BeatBuilder).GetRoot, func(b *wire.BeatBuilder) *wire.TableArena { return &b.Arena }, wire.BeatFromJson, wire.BeatMeasure, wire.BeatSave, wire.BeatLoadMeasureReason, wire.BeatLoad, wire.BeatToJsonMeasure, wire.BeatToJson)},
		{"Beat", "facts", textListOps((*wire.BeatBuilder).Init, (*wire.BeatBuilder).GetRoot, func(b *wire.BeatBuilder) *wire.TableArena { return &b.Arena }, wire.BeatFromJson, wire.BeatMeasure, wire.BeatSave, wire.BeatLoadMeasureReason, wire.BeatLoad, wire.BeatToJsonMeasure, wire.BeatToJson)},
	}

	for _, c := range cases {
		c := c
		t.Run(c.table+"/"+c.field, func(t *testing.T) {
			t.Parallel()
			c.run(t, c.table, c.field)
		})
	}
}

// textListCase is one table field and the harness call that checks it.
type textListCase struct {
	table, field string
	run          func(*testing.T, string, string)
}

// textListOps adapts a generated table's functions to textListCase.run.
func textListOps[B any, T any](
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
) func(*testing.T, string, string) {
	return func(t *testing.T, table, field string) {
		plainTextList(t, table, field, init, root, arena, from, measure, save, loadMeasure, load, jsonMeasure, toJSON)
	}
}

// plainTextList reads {"<field>":["alpha","beta"]} through the generated
// reader, saves the root to the wire, loads it back and checks the field's
// text is the same list of strings.
func plainTextList[B any, T any](
	t *testing.T,
	table, field string,
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
	text := []byte(`{"` + field + `":["alpha","beta"]}`)
	b := new(B)
	if !init(b) {
		t.Fatalf("%s: the builder does not init", table)
	}
	var rep wire.TableReport
	ok := from(b, text, &rep)
	if !ok || rep.KindMismatch != 0 || rep.Clamped != 0 || rep.Malformed {
		t.Fatalf("%s.%s: FromJson(%s) ok=%v kind_mismatch=%d malformed=%v clamped=%d reason=%q", table, field, text, ok, rep.KindMismatch, rep.Malformed, rep.Clamped, rep.Reason)
	}
	r := root(b)
	a := arena(b)
	buf := make([]byte, measure(r, a))
	n := save(r, buf, a)
	if n < 0 {
		t.Fatalf("%s.%s: the root does not save to the wire", table, field)
	}
	size, err := loadMeasure(buf[:n])
	if err != nil {
		t.Fatalf("%s.%s: the wire does not measure: %v", table, field, err)
	}
	var lrep wire.TableReport
	loaded := load(make([]byte, size), buf[:n], &lrep)
	if loaded == nil {
		t.Fatalf("%s.%s: the wire does not load: %q", table, field, lrep.Reason)
	}
	out := make([]byte, jsonMeasure(loaded))
	jn := toJSON(loaded, out)
	if jn < 0 {
		t.Fatalf("%s.%s: the loaded root does not write its text", table, field)
	}
	var doc map[string]any
	if err := json.Unmarshal(out[:jn], &doc); err != nil {
		t.Fatalf("%s.%s: the written text is not JSON: %v", table, field, err)
	}
	got, ok := doc[field].([]any)
	want := []any{"alpha", "beta"}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("%s.%s: the written text holds %v, want %v", table, field, doc[field], want)
	}
}
