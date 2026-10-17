package viewrowswire_test

import (
	"encoding/json"
	"reflect"
	"testing"

	wire "github.com/mas-bandwidth/nova-sprint/internal/sprint/store/viewrowswire"
)

// TestViewrowsTextListsArePlain pins that every view-row text list is the
// plain JSON list of strings its Go source already holds, ["a","b"], not a
// list of one-field wrapper tables (SPEC-TABLES 2.13.1). Each field reads
// such a list through the generated reader with no kind mismatch, clamp or
// malformed report, saves it to the wire, loads it back and writes the same
// list as text.
func TestViewrowsTextListsArePlain(t *testing.T) {
	t.Parallel()

	cases := []textListCase{
		{"LineList", "items", textListOps((*wire.LineListBuilder).Init, (*wire.LineListBuilder).GetRoot, func(b *wire.LineListBuilder) *wire.TableArena { return &b.Arena }, wire.LineListFromJson, wire.LineListMeasure, wire.LineListSave, wire.LineListLoadMeasureReason, wire.LineListLoad, wire.LineListToJsonMeasure, wire.LineListToJson)},
		{"CostCheck", "orphans", textListOps((*wire.CostCheckBuilder).Init, (*wire.CostCheckBuilder).GetRoot, func(b *wire.CostCheckBuilder) *wire.TableArena { return &b.Arena }, wire.CostCheckFromJson, wire.CostCheckMeasure, wire.CostCheckSave, wire.CostCheckLoadMeasureReason, wire.CostCheckLoad, wire.CostCheckToJsonMeasure, wire.CostCheckToJson)},
		{"PriorityCards", "cards", textListOps((*wire.PriorityCardsBuilder).Init, (*wire.PriorityCardsBuilder).GetRoot, func(b *wire.PriorityCardsBuilder) *wire.TableArena { return &b.Arena }, wire.PriorityCardsFromJson, wire.PriorityCardsMeasure, wire.PriorityCardsSave, wire.PriorityCardsLoadMeasureReason, wire.PriorityCardsLoad, wire.PriorityCardsToJsonMeasure, wire.PriorityCardsToJson)},
		{"StreamTransition", "owners", textListOps((*wire.StreamTransitionBuilder).Init, (*wire.StreamTransitionBuilder).GetRoot, func(b *wire.StreamTransitionBuilder) *wire.TableArena { return &b.Arena }, wire.StreamTransitionFromJson, wire.StreamTransitionMeasure, wire.StreamTransitionSave, wire.StreamTransitionLoadMeasureReason, wire.StreamTransitionLoad, wire.StreamTransitionToJsonMeasure, wire.StreamTransitionToJson)},
		{"DecisionDelivery", "reports", textListOps((*wire.DecisionDeliveryBuilder).Init, (*wire.DecisionDeliveryBuilder).GetRoot, func(b *wire.DecisionDeliveryBuilder) *wire.TableArena { return &b.Arena }, wire.DecisionDeliveryFromJson, wire.DecisionDeliveryMeasure, wire.DecisionDeliverySave, wire.DecisionDeliveryLoadMeasureReason, wire.DecisionDeliveryLoad, wire.DecisionDeliveryToJsonMeasure, wire.DecisionDeliveryToJson)},
		{"Note", "primaries", textListOps((*wire.NoteBuilder).Init, (*wire.NoteBuilder).GetRoot, func(b *wire.NoteBuilder) *wire.TableArena { return &b.Arena }, wire.NoteFromJson, wire.NoteMeasure, wire.NoteSave, wire.NoteLoadMeasureReason, wire.NoteLoad, wire.NoteToJsonMeasure, wire.NoteToJson)},
		{"Note", "decisions", textListOps((*wire.NoteBuilder).Init, (*wire.NoteBuilder).GetRoot, func(b *wire.NoteBuilder) *wire.TableArena { return &b.Arena }, wire.NoteFromJson, wire.NoteMeasure, wire.NoteSave, wire.NoteLoadMeasureReason, wire.NoteLoad, wire.NoteToJsonMeasure, wire.NoteToJson)},
		{"Note", "suspects", textListOps((*wire.NoteBuilder).Init, (*wire.NoteBuilder).GetRoot, func(b *wire.NoteBuilder) *wire.TableArena { return &b.Arena }, wire.NoteFromJson, wire.NoteMeasure, wire.NoteSave, wire.NoteLoadMeasureReason, wire.NoteLoad, wire.NoteToJsonMeasure, wire.NoteToJson)},
		{"Note", "needs", textListOps((*wire.NoteBuilder).Init, (*wire.NoteBuilder).GetRoot, func(b *wire.NoteBuilder) *wire.TableArena { return &b.Arena }, wire.NoteFromJson, wire.NoteMeasure, wire.NoteSave, wire.NoteLoadMeasureReason, wire.NoteLoad, wire.NoteToJsonMeasure, wire.NoteToJson)},
		{"WhereRecord", "blockers", textListOps((*wire.WhereRecordBuilder).Init, (*wire.WhereRecordBuilder).GetRoot, func(b *wire.WhereRecordBuilder) *wire.TableArena { return &b.Arena }, wire.WhereRecordFromJson, wire.WhereRecordMeasure, wire.WhereRecordSave, wire.WhereRecordLoadMeasureReason, wire.WhereRecordLoad, wire.WhereRecordToJsonMeasure, wire.WhereRecordToJson)},
		{"WhereRecord", "archived_primaries", textListOps((*wire.WhereRecordBuilder).Init, (*wire.WhereRecordBuilder).GetRoot, func(b *wire.WhereRecordBuilder) *wire.TableArena { return &b.Arena }, wire.WhereRecordFromJson, wire.WhereRecordMeasure, wire.WhereRecordSave, wire.WhereRecordLoadMeasureReason, wire.WhereRecordLoad, wire.WhereRecordToJsonMeasure, wire.WhereRecordToJson)},
		{"FriendReport", "running", textListOps((*wire.FriendReportBuilder).Init, (*wire.FriendReportBuilder).GetRoot, func(b *wire.FriendReportBuilder) *wire.TableArena { return &b.Arena }, wire.FriendReportFromJson, wire.FriendReportMeasure, wire.FriendReportSave, wire.FriendReportLoadMeasureReason, wire.FriendReportLoad, wire.FriendReportToJsonMeasure, wire.FriendReportToJson)},
		{"Beat", "live", textListOps((*wire.BeatBuilder).Init, (*wire.BeatBuilder).GetRoot, func(b *wire.BeatBuilder) *wire.TableArena { return &b.Arena }, wire.BeatFromJson, wire.BeatMeasure, wire.BeatSave, wire.BeatLoadMeasureReason, wire.BeatLoad, wire.BeatToJsonMeasure, wire.BeatToJson)},
		{"Beat", "facts", textListOps((*wire.BeatBuilder).Init, (*wire.BeatBuilder).GetRoot, func(b *wire.BeatBuilder) *wire.TableArena { return &b.Arena }, wire.BeatFromJson, wire.BeatMeasure, wire.BeatSave, wire.BeatLoadMeasureReason, wire.BeatLoad, wire.BeatToJsonMeasure, wire.BeatToJson)},
		{"CheckRecord", "streams", textListOps((*wire.CheckRecordBuilder).Init, (*wire.CheckRecordBuilder).GetRoot, func(b *wire.CheckRecordBuilder) *wire.TableArena { return &b.Arena }, wire.CheckRecordFromJson, wire.CheckRecordMeasure, wire.CheckRecordSave, wire.CheckRecordLoadMeasureReason, wire.CheckRecordLoad, wire.CheckRecordToJsonMeasure, wire.CheckRecordToJson)},
		{"CheckRecord", "findings", textListOps((*wire.CheckRecordBuilder).Init, (*wire.CheckRecordBuilder).GetRoot, func(b *wire.CheckRecordBuilder) *wire.TableArena { return &b.Arena }, wire.CheckRecordFromJson, wire.CheckRecordMeasure, wire.CheckRecordSave, wire.CheckRecordLoadMeasureReason, wire.CheckRecordLoad, wire.CheckRecordToJsonMeasure, wire.CheckRecordToJson)},
		{"ResetResult", "said", textListOps((*wire.ResetResultBuilder).Init, (*wire.ResetResultBuilder).GetRoot, func(b *wire.ResetResultBuilder) *wire.TableArena { return &b.Arena }, wire.ResetResultFromJson, wire.ResetResultMeasure, wire.ResetResultSave, wire.ResetResultLoadMeasureReason, wire.ResetResultLoad, wire.ResetResultToJsonMeasure, wire.ResetResultToJson)},
		{"StatsRecord", "archives", textListOps((*wire.StatsRecordBuilder).Init, (*wire.StatsRecordBuilder).GetRoot, func(b *wire.StatsRecordBuilder) *wire.TableArena { return &b.Arena }, wire.StatsRecordFromJson, wire.StatsRecordMeasure, wire.StatsRecordSave, wire.StatsRecordLoadMeasureReason, wire.StatsRecordLoad, wire.StatsRecordToJsonMeasure, wire.StatsRecordToJson)},
		{"StatsArchive", "kinds", textListOps((*wire.StatsArchiveBuilder).Init, (*wire.StatsArchiveBuilder).GetRoot, func(b *wire.StatsArchiveBuilder) *wire.TableArena { return &b.Arena }, wire.StatsArchiveFromJson, wire.StatsArchiveMeasure, wire.StatsArchiveSave, wire.StatsArchiveLoadMeasureReason, wire.StatsArchiveLoad, wire.StatsArchiveToJsonMeasure, wire.StatsArchiveToJson)},
		{"TidyResult", "said", textListOps((*wire.TidyResultBuilder).Init, (*wire.TidyResultBuilder).GetRoot, func(b *wire.TidyResultBuilder) *wire.TableArena { return &b.Arena }, wire.TidyResultFromJson, wire.TidyResultMeasure, wire.TidyResultSave, wire.TidyResultLoadMeasureReason, wire.TidyResultLoad, wire.TidyResultToJsonMeasure, wire.TidyResultToJson)},
		{"LogIndexEntry", "ids", textListOps((*wire.LogIndexEntryBuilder).Init, (*wire.LogIndexEntryBuilder).GetRoot, func(b *wire.LogIndexEntryBuilder) *wire.TableArena { return &b.Arena }, wire.LogIndexEntryFromJson, wire.LogIndexEntryMeasure, wire.LogIndexEntrySave, wire.LogIndexEntryLoadMeasureReason, wire.LogIndexEntryLoad, wire.LogIndexEntryToJsonMeasure, wire.LogIndexEntryToJson)},
		{"Heartbeat", "fresh", textListOps((*wire.HeartbeatBuilder).Init, (*wire.HeartbeatBuilder).GetRoot, func(b *wire.HeartbeatBuilder) *wire.TableArena { return &b.Arena }, wire.HeartbeatFromJson, wire.HeartbeatMeasure, wire.HeartbeatSave, wire.HeartbeatLoadMeasureReason, wire.HeartbeatLoad, wire.HeartbeatToJsonMeasure, wire.HeartbeatToJson)},
		{"FieldGuard", "one_of", textListOps((*wire.FieldGuardBuilder).Init, (*wire.FieldGuardBuilder).GetRoot, func(b *wire.FieldGuardBuilder) *wire.TableArena { return &b.Arena }, wire.FieldGuardFromJson, wire.FieldGuardMeasure, wire.FieldGuardSave, wire.FieldGuardLoadMeasureReason, wire.FieldGuardLoad, wire.FieldGuardToJsonMeasure, wire.FieldGuardToJson)},
		{"BatchMemberEntry", "unset", textListOps((*wire.BatchMemberEntryBuilder).Init, (*wire.BatchMemberEntryBuilder).GetRoot, func(b *wire.BatchMemberEntryBuilder) *wire.TableArena { return &b.Arena }, wire.BatchMemberEntryFromJson, wire.BatchMemberEntryMeasure, wire.BatchMemberEntrySave, wire.BatchMemberEntryLoadMeasureReason, wire.BatchMemberEntryLoad, wire.BatchMemberEntryToJsonMeasure, wire.BatchMemberEntryToJson)},
		{"BatchManifest", "prop_absent", textListOps((*wire.BatchManifestBuilder).Init, (*wire.BatchManifestBuilder).GetRoot, func(b *wire.BatchManifestBuilder) *wire.TableArena { return &b.Arena }, wire.BatchManifestFromJson, wire.BatchManifestMeasure, wire.BatchManifestSave, wire.BatchManifestLoadMeasureReason, wire.BatchManifestLoad, wire.BatchManifestToJsonMeasure, wire.BatchManifestToJson)},
		{"LogLine", "cards", textListOps((*wire.LogLineBuilder).Init, (*wire.LogLineBuilder).GetRoot, func(b *wire.LogLineBuilder) *wire.TableArena { return &b.Arena }, wire.LogLineFromJson, wire.LogLineMeasure, wire.LogLineSave, wire.LogLineLoadMeasureReason, wire.LogLineLoad, wire.LogLineToJsonMeasure, wire.LogLineToJson)},
		{"LogLine", "answers", textListOps((*wire.LogLineBuilder).Init, (*wire.LogLineBuilder).GetRoot, func(b *wire.LogLineBuilder) *wire.TableArena { return &b.Arena }, wire.LogLineFromJson, wire.LogLineMeasure, wire.LogLineSave, wire.LogLineLoadMeasureReason, wire.LogLineLoad, wire.LogLineToJsonMeasure, wire.LogLineToJson)},
		{"HeldRecord", "closes", textListOps((*wire.HeldRecordBuilder).Init, (*wire.HeldRecordBuilder).GetRoot, func(b *wire.HeldRecordBuilder) *wire.TableArena { return &b.Arena }, wire.HeldRecordFromJson, wire.HeldRecordMeasure, wire.HeldRecordSave, wire.HeldRecordLoadMeasureReason, wire.HeldRecordLoad, wire.HeldRecordToJsonMeasure, wire.HeldRecordToJson)},
		{"HeldRecord", "streams", textListOps((*wire.HeldRecordBuilder).Init, (*wire.HeldRecordBuilder).GetRoot, func(b *wire.HeldRecordBuilder) *wire.TableArena { return &b.Arena }, wire.HeldRecordFromJson, wire.HeldRecordMeasure, wire.HeldRecordSave, wire.HeldRecordLoadMeasureReason, wire.HeldRecordLoad, wire.HeldRecordToJsonMeasure, wire.HeldRecordToJson)},
		{"HeldRecord", "close_timers", textListOps((*wire.HeldRecordBuilder).Init, (*wire.HeldRecordBuilder).GetRoot, func(b *wire.HeldRecordBuilder) *wire.TableArena { return &b.Arena }, wire.HeldRecordFromJson, wire.HeldRecordMeasure, wire.HeldRecordSave, wire.HeldRecordLoadMeasureReason, wire.HeldRecordLoad, wire.HeldRecordToJsonMeasure, wire.HeldRecordToJson)},
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
