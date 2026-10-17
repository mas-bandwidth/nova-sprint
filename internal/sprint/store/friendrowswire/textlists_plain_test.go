package friendrowswire_test

import (
	"encoding/json"
	"testing"

	wire "github.com/mas-bandwidth/nova-sprint/internal/sprint/store/friendrowswire"
)

// plainTextList reads {"<field>":["alpha","beta"]} as JSON, writes the root to
// the wire and reads it back, and requires the JSON text of the read value to
// hold the same plain list at the field.
func plainTextList[B any, T any](
	t *testing.T, table, field string,
	init func(*B) bool,
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
	var b B
	if !init(&b) {
		t.Fatalf("%s.%s: builder init", table, field)
	}
	var rep wire.TableReport
	if !from(&b, text, &rep) {
		t.Fatalf("%s.%s: FromJson refused: %q", table, field, rep.Reason)
	}
	if rep.KindMismatch != 0 || rep.Clamped != 0 || rep.Malformed {
		t.Fatalf("%s.%s: FromJson ok=%v kind_mismatch=%d clamped=%d malformed=%v reason=%q",
			table, field, true, rep.KindMismatch, rep.Clamped, rep.Malformed, rep.Reason)
	}
	r := root(&b)
	size := measure(r, arena(&b))
	buf := make([]byte, size)
	wireSize := save(r, buf, arena(&b))
	if wireSize < 0 {
		t.Fatalf("%s.%s: save", table, field)
	}
	buf = buf[:wireSize]
	regionSize, err := loadMeasure(buf)
	if err != nil {
		t.Fatalf("%s.%s: load measure: %v", table, field, err)
	}
	var read wire.TableReport
	loaded := load(make([]byte, regionSize), buf, &read)
	if loaded == nil {
		t.Fatalf("%s.%s: load refused: %q", table, field, read.Reason)
	}
	jsonSize := jsonMeasure(loaded)
	jsonBuf := make([]byte, jsonSize)
	jsonBuf = jsonBuf[:toJSON(loaded, jsonBuf)]
	var doc map[string]any
	if err := json.Unmarshal(jsonBuf, &doc); err != nil {
		t.Fatalf("%s.%s: ToJson is not JSON: %v: %s", table, field, err, jsonBuf)
	}
	got, ok := doc[field].([]any)
	if !ok {
		t.Fatalf("%s.%s: %s is %T, not a list: %s", table, field, field, doc[field], jsonBuf)
	}
	want := []string{"alpha", "beta"}
	if len(got) != len(want) {
		t.Fatalf("%s.%s: %s has %d elements, want %d: %s", table, field, field, len(got), len(want), jsonBuf)
	}
	for i, w := range want {
		s, ok := got[i].(string)
		if !ok || s != w {
			t.Fatalf("%s.%s: %s[%d] is %v, want %q: %s", table, field, field, i, got[i], w, jsonBuf)
		}
	}
}

func TestFriendrowsTextListsArePlain(t *testing.T) {
	t.Parallel()
	t.Run("FriendReport.running", func(t *testing.T) {
		t.Parallel()
		plainTextList(
			t, "FriendReport", "running",
			func(b *wire.FriendReportBuilder) bool { return b.Init() },
			func(b *wire.FriendReportBuilder) *wire.FriendReport { return b.GetRoot() },
			func(b *wire.FriendReportBuilder) *wire.TableArena { return &b.Arena },
			wire.FriendReportFromJson,
			wire.FriendReportMeasure,
			wire.FriendReportSave,
			wire.FriendReportLoadMeasureReason,
			wire.FriendReportLoad,
			wire.FriendReportToJsonMeasure,
			wire.FriendReportToJson,
		)
	})
	t.Run("Beat.live", func(t *testing.T) {
		t.Parallel()
		plainTextList(
			t, "Beat", "live",
			func(b *wire.BeatBuilder) bool { return b.Init() },
			func(b *wire.BeatBuilder) *wire.Beat { return b.GetRoot() },
			func(b *wire.BeatBuilder) *wire.TableArena { return &b.Arena },
			wire.BeatFromJson,
			wire.BeatMeasure,
			wire.BeatSave,
			wire.BeatLoadMeasureReason,
			wire.BeatLoad,
			wire.BeatToJsonMeasure,
			wire.BeatToJson,
		)
	})
	t.Run("Beat.facts", func(t *testing.T) {
		t.Parallel()
		plainTextList(
			t, "Beat", "facts",
			func(b *wire.BeatBuilder) bool { return b.Init() },
			func(b *wire.BeatBuilder) *wire.Beat { return b.GetRoot() },
			func(b *wire.BeatBuilder) *wire.TableArena { return &b.Arena },
			wire.BeatFromJson,
			wire.BeatMeasure,
			wire.BeatSave,
			wire.BeatLoadMeasureReason,
			wire.BeatLoad,
			wire.BeatToJsonMeasure,
			wire.BeatToJson,
		)
	})
}
