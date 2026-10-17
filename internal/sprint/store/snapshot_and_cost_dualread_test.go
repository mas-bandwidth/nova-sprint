package store

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	snapwire "github.com/mas-bandwidth/nova-sprint/internal/sprint/store/snaprowswire"
	"github.com/mas-bandwidth/nova-tools/pkg/cardcost"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSnapshotAndCostDualRead pins the reader-first dual read of the snapshot,
// restore and cost records (Aspect 8; the owner's rules S1, S2 and S4): the twin's
// snapshot document and its dump's pages (Mem.Restore, MemTwin.Load, MemDump,
// MemRestore), the restore state and the fleet parts the live comparison reads
// (restore.go), and the card cost records (sprint.CardCostOf) each read alike from
// its table image, padded standard base64 of that image and its legacy form,
// while every writer still writes the legacy form; a malformed value of each form
// is refused by name and never read as absent, empty or zero; a clamped value is
// refused; and reads are counted per record and per form. It runs on the
// in-memory backend only (NewMem); its clock starts at 1000 seconds.
func TestSnapshotAndCostDualRead(t *testing.T) {
	t.Parallel()
	at := time.Unix(1000, 0).UTC()

	// ---- the twin's document -------------------------------------------------
	// A document of every field but the log's oprows records (snapOprowsUnread).
	legacyDoc := dualDoc(t, at)
	src := NewMem()
	require.NoError(t, src.Restore(legacyDoc), "today's legacy document restores")
	doc, err := src.Snapshot()
	require.NoError(t, err)
	assert.False(t, strings.HasPrefix(string(doc), "\x01"), "the writer still writes the legacy JSON (S4)")
	value, _, err := memSnapshotRows.Read(doc)
	require.NoError(t, err)

	before := memSnapshotRows.Counts().Snapshot()
	forms := dualForms(t, "MEMSNAPSHOT", func(form schemaread.Form) ([]byte, error) { return memSnapshotRows.Write(&value, form) })
	for name, in := range forms {
		m := NewMem()
		require.NoError(t, m.Restore(in), "Mem.Restore reads the document from %s", name)
		got, err := m.Snapshot()
		require.NoError(t, err)
		assert.Equal(t, string(doc), string(got), "the document reads alike from %s", name)
		c, err := MemTwin{Names: sprint.Names{Prefix: "dual-"}}.Load(in)
		require.NoError(t, err, "MemTwin.Load reads the document from %s", name)
		assert.Equal(t, 2, c.Keys, "the twin counts the document's tables from %s", name)
	}
	m := NewMem()
	require.NoError(t, m.Restore(doc), "the legacy document reads")
	after := memSnapshotRows.Counts().Snapshot()
	for _, f := range []string{"binary", "base64", "json"} {
		assert.EqualValues(t, 2, after[f]-before[f], "MEMSNAPSHOT counts each %s read (Restore and Load)", f)
	}
	assert.GreaterOrEqual(t, after["legacy"]-before["legacy"], uint64(1), "MEMSNAPSHOT counts the legacy read")

	// MemDump reads the document through the same reader.
	keys, err := MemDump(src, 21)
	require.NoError(t, err)
	require.NotEmpty(t, keys)

	// ---- the dump's pages ---------------------------------------------------
	pageBefore := pageCounts()
	// A backup holds the epoch's log only (InBackup), so the dump restores the
	// document without the older epoch's log, alike from every form.
	legacyRestored, err := MemRestore(keys)
	require.NoError(t, err)
	dumped, err := legacyRestored.Snapshot()
	require.NoError(t, err)
	for _, form := range []schemaread.Form{schemaread.FormBinary, schemaread.FormBase64, schemaread.FormJSON} {
		var converted []DumpKey
		for _, k := range keys {
			converted = append(converted, DumpKey{Key: k.Key, TTL: k.TTL, Payload: dualPage(t, k, form)})
		}
		restored, err := MemRestore(converted)
		require.NoError(t, err, "MemRestore reads every page from %s", form)
		got, err := restored.Snapshot()
		require.NoError(t, err)
		assert.Equal(t, string(dumped), string(got), "the dump restores alike from %s", form)
	}
	assert.Contains(t, string(dumped), `"t-old"`, "the legacy dump restores the document's tables")
	pageAfter := pageCounts()
	for _, name := range []string{"MEMSNAPSHOT", "VIEW", "RESIDUESNAP", "LOGSNAPSHOT", "EPOCHSNAPSHOT", "TABLESNAPSHOT", "MEMDUMPMEMBER"} {
		for _, f := range []string{"binary", "base64", "json"} {
			assert.Positive(t, pageAfter[name][f]-pageBefore[name][f], "%s counts its %s page reads", name, f)
		}
		assert.Positive(t, pageAfter[name]["legacy"]-pageBefore[name]["legacy"], "%s counts its legacy page reads", name)
	}

	// A log that holds an oprows record is refused by name in the table forms,
	// never read with the record absent.
	oprows := dualOprowsImage(t)
	err = NewMem().Restore(oprows)
	dualRefused(t, "MEMSNAPSHOT", snapOprowsUnread, err)
	_, err = MemTwin{}.Load([]byte(base64.StdEncoding.EncodeToString(oprows)))
	dualRefused(t, "MEMSNAPSHOT", snapOprowsUnread, err)

	// ---- the restore state ------------------------------------------------------
	state := SprintState{Parts: map[string]string{"epoch": `{"N":21}`, "coordinator": "rowan", "row fleet m1": `{"at":0}`}}
	stateLegacy, err := json.Marshal(state)
	require.NoError(t, err)
	gotState, _, err := sprintStateRows.Read(stateLegacy)
	require.NoError(t, err)
	assert.Equal(t, state, gotState, "the legacy state reads")
	stateBefore := sprintStateRows.Counts().Snapshot()
	for name, in := range dualForms(t, "SPRINTSTATE", func(form schemaread.Form) ([]byte, error) { return sprintStateRows.Write(&state, form) }) {
		gotState, _, err := sprintStateRows.Read(in)
		require.NoError(t, err, name)
		assert.Equal(t, state, gotState, "the state reads alike from %s", name)
	}
	stateAfter := sprintStateRows.Counts().Snapshot()
	for _, f := range []string{"binary", "base64", "json"} {
		assert.EqualValues(t, 1, stateAfter[f]-stateBefore[f], "SPRINTSTATE counts its %s read", f)
	}

	// The live comparison's fleet part: an unreadable value is compared as it is,
	// on purpose, and its refusal is counted (RESTOREPART).
	partBefore := restorePartRows.Counts().Snapshot()
	assert.Equal(t, "not json", liveForm("row "+sprint.Fleet+" m1", "not json"))
	assert.Equal(t, `{"texts":{}}`, liveForm("row "+sprint.Fleet+" m1", `{"at":3,"texts":{}}`))
	partAfter := restorePartRows.Counts().Snapshot()
	assert.Positive(t, partAfter["legacy"]-partBefore["legacy"], "RESTOREPART counts the legacy read")
	refusedDelta := uint64(0)
	for k, v := range partAfter {
		if strings.HasPrefix(k, "refused_") {
			refusedDelta += v - partBefore[k]
		}
	}
	assert.Positive(t, refusedDelta, "RESTOREPART counts the refusal it compares as it is")

	// ---- the card cost record --------------------------------------------------
	u := cardcost.ParseUsage("input=19541 cache_read=36336 cache_write=0 output=692 reasoning=70 requests=6 max_prompt=9861 model=opencode/deepseek-v4-pro cost=0.04219614")
	line := "kind=work card=a8 attempt=2 take=1 gen=3 who=friend.rowan on_route=pro-a on_model=opencode/deepseek-v4-pro on_tier=pro end=landed at=2026-10-17T01:02:03Z lane_cap=30m lane_overrun=4m " + u.String()
	legacyC, err := sprint.DecodeConsumer("a8#g3", line)
	require.NoError(t, err)
	require.Equal(t, 2, legacyC.Attempt)
	consumerImage := dualConsumerImage(t, legacyC)
	consumerBefore := sprint.ConsumerReader.Counts().Snapshot()
	for name, in := range map[string][]byte{"binary": consumerImage, "base64": []byte(base64.StdEncoding.EncodeToString(consumerImage)), "json": dualConsumerJSON(t, legacyC)} {
		c, err := sprint.DecodeConsumer("a8#g3", string(in))
		require.NoError(t, err, name)
		assert.Equal(t, legacyC, c, "the cost record reads alike from %s", name)
	}
	card := &sprint.Card{ID: "a8", Fields: map[string]string{
		sprint.FieldCostRecord + "a8#g1": line,
		sprint.FieldCostRecord + "a8#g2": string(consumerImage),
		sprint.FieldCostRecord + "a8#g3": base64.StdEncoding.EncodeToString(consumerImage),
		sprint.FieldCostRecord + "a8#g4": base64.StdEncoding.EncodeToString(consumerImage[:len(consumerImage)-3]),
	}}
	view := sprint.CardCostOf(card)
	assert.Len(t, view.Consumers, 3, "CardCostOf reads the legacy line, the image and its base64")
	require.Len(t, view.Refused, 1, "a damaged record is refused, never read as absent")
	assert.Contains(t, view.Refused[0], "a8#g4: CONSUMER REFUSED: ")
	assert.Contains(t, strings.Join(view.CostLines(), "\n"), "COST REFUSED a8#g4: CONSUMER REFUSED: ")
	consumerAfter := sprint.ConsumerReader.Counts().Snapshot()
	for _, f := range []string{"binary", "base64", "json", "legacy"} {
		assert.Positive(t, consumerAfter[f]-consumerBefore[f], "CONSUMER counts its %s reads", f)
	}
	// a legacy line whose count is not a number is refused, never read as zero
	_, err = sprint.DecodeConsumer("a8#g5", strings.Replace(line, "attempt=2", "attempt=two", 1))
	dualRefused(t, "CONSUMER", "", err)

	// ---- malformed values of every form, refused by name -----------------------
	type reader struct {
		name    string
		read    func([]byte) error
		good    map[string][]byte
		samples int
	}
	readers := []reader{
		{"MEMSNAPSHOT", func(in []byte) error { return NewMem().Restore(in) }, forms, 60},
		{"SPRINTSTATE", func(in []byte) error { _, _, err := sprintStateRows.Read(in); return err },
			dualForms(t, "SPRINTSTATE", func(form schemaread.Form) ([]byte, error) { return sprintStateRows.Write(&state, form) }), 60},
		{"CONSUMER", func(in []byte) error { _, err := sprint.DecodeConsumer("a8#g9", string(in)); return err },
			map[string][]byte{"binary": consumerImage, "base64": []byte(base64.StdEncoding.EncodeToString(consumerImage)), "json": dualConsumerJSON(t, legacyC), "legacy": []byte(line)}, 60},
	}
	for _, k := range keys {
		c := dualPageCodec(k.Key)
		if c == nil {
			continue
		}
		page := map[string][]byte{"legacy": k.Payload}
		for _, form := range []schemaread.Form{schemaread.FormBinary, schemaread.FormBase64, schemaread.FormJSON} {
			page[form.String()] = dualPage(t, k, form)
		}
		readers = append(readers, reader{c.name, c.read, page, 8})
	}
	forms["legacy"] = doc
	stateForms := readers[1].good
	stateForms["legacy"] = stateLegacy
	for _, r := range readers {
		for form, good := range r.good {
			for i, bad := range dualDamage(good, form == "legacy", r.samples) {
				err := r.read(bad)
				require.Error(t, err, "%s: damaged %s value %d is refused, never read as absent: %q", r.name, form, i, dualShort(bad))
				dualRefused(t, r.name, "", err)
			}
		}
	}

	// ---- a clamped value is refused ---------------------------------------------
	clamped := strings.Replace(string(forms["json"]), `"version": 1,`, `"version": 99999999999999999999999,`, 1)
	require.NotEqual(t, string(forms["json"]), clamped, "the schema JSON text holds the version")
	err = NewMem().Restore([]byte(clamped))
	dualRefused(t, "MEMSNAPSHOT", "", err)
	t.Logf("a version past int64: %v", err)
	assert.True(t, strings.Contains(err.Error(), schemaread.ReasonClamped) || strings.Contains(err.Error(), schemaread.ReasonMalformed), "a value past the field's width is refused: %v", err)
}

// dualDoc is a twin document of every field, in today's legacy JSON, with times
// from the test clock.
func dualDoc(t *testing.T, at time.Time) []byte {
	t.Helper()
	table := func(name, brief string) map[string]any {
		return map[string]any{
			"def": map[string]any{"EpochKey": "e", "EpochField": "n", "MemberPrefix": "w:", "Epoch": 21, "Revision": 7, "Name": name,
				"Columns": []any{map[string]any{"Name": "state", "Label": "State", "Projection": "count", "Fold": "sum", "Width": 8}}, "FooterLabel": "total", "Hidden": []any{"note"}, "HiddenTable": true,
				"Sort": "-name", "Rows": []any{map[string]any{"Key": "r1", "Label": "Row one", "Exclude": "s1-9", "Owner": "finish", "Hidden": true, "Texts": map[string]any{"state": "active"},
					"Cells": []any{map[string]any{"Key": "k:r1:state", "Bound": true, "Count": 2, "Unread": true, "UnreadWhy": "unread",
						"Members": []any{map[string]any{"Member": "s1-1", "Score": 0.5}}}}}},
				"Props": map[string]any{"stream": "s1"}},
			"epochs":  map[string]any{"0": map[string]any{"rows": []any{"r1"}, "hidden": map[string]any{"r2": true}, "texts": map[string]any{"r1": map[string]any{"state": "active"}}, "props": map[string]any{"stream": "s1"}}, "21": map[string]any{"rows": []any{"r3"}}},
			"wrote":   map[string]any{"0": true, "21": true},
			"rev":     2,
			"members": map[string]any{"s1-1": map[string]any{"epoch": 21, "placed": true, "row": "r1", "col": "state", "score": 0.5, "rev": 1, "fields": map[string]any{"brief": brief}}, "s1-2": map[string]any{"epoch": 3, "placed": false, "score": 0, "rev": 0}},
			"ops": map[string]any{"21:op1": map[string]any{"body": "{}", "receipt": map[string]any{"ID": "1-0", "Epoch": 21, "Before": 1, "After": 2, "Outcome": "changed", "Replay": false,
				"BatchDelta": map[string]any{"operation_id": "op1", "digest": "d1", "actor": "beta", "selected_count": 1, "guard_count": 0, "changed_count": 1,
					"members": []any{map[string]any{"id": "s1-1", "before_place": "", "after_place": "r1:state", "before_score": nil, "after_score": "0.5", "before_rev": "0", "after_rev": "1",
						"fields_set": map[string]any{"brief": "t"}, "fields_unset": []any{"old"},
						"fields": map[string]any{"brief": map[string]any{"before": nil, "after": "t", "after_bytes": 1, "after_sha1": "da4b9237bacccdf19c0760cab7aec4a8359010b0"}}}},
					"props": map[string]any{"stream": "s1"}}}}},
			"changes": []any{map[string]any{"epoch": 21, "before": 1, "after": 2, "verb": "apply", "ids": []any{"s1-1"}}},
		}
	}
	doc := map[string]any{"version": SnapshotVersion, "tables": map[string]any{"t-work": table("t-work", strings.Repeat("b", 3000)), "t-fleet": table("t-fleet", "short")},
		"dropped":   map[string]any{"t-old": map[string]any{"keys": map[string]any{"w:s1-1": true}, "table": table("t-old", "old")}},
		"views":     map[string]any{"sprint": map[string]any{"Name": "sprint", "Tables": []any{"t-work", "t-fleet"}, "Title": "The sprint", "Summary": "state", "State": "STOPPED"}},
		"epoch_set": true, "epoch": 21, "cleared": at.Add(4 * time.Nanosecond).Format(time.RFC3339Nano), "owed": true, "defaults_owed": 21,
		"logs": map[string]any{"21": map[string]any{"gen": 3, "done": map[string]any{"caller-1": "ok"}, "progress": map[string]any{"s1": at.Format(time.RFC3339Nano)},
			"aliases": map[string]any{"j12": "n12"}, "answered": map[string]any{"n12": "coordinator"}, "open": map[string]any{"s1/s1-1": "n12"}, "cursor": "1-0"}, "3": map[string]any{"gen": 1}},
		"kv": map[string]any{"coordinator": "rowan"}, "seq": 9}
	b, err := json.Marshal(doc)
	require.NoError(t, err)
	return b
}

// dualForms is a record's binary image, padded standard base64 of it and its schema
// JSON text, each written by its reader's writer.
func dualForms(t *testing.T, name string, write func(schemaread.Form) ([]byte, error)) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, form := range []schemaread.Form{schemaread.FormBinary, schemaread.FormBase64, schemaread.FormJSON} {
		b, err := write(form)
		require.NoError(t, err, "%s writes %s", name, form)
		require.NotEmpty(t, b, "%s writes %s", name, form)
		out[form.String()] = b
	}
	assert.Equal(t, base64.StdEncoding.EncodeToString(out["binary"]), string(out["base64"]), "%s: the base64 text is the image", name)
	assert.Equal(t, byte(1), out["binary"][0], "%s: the image leads with the variable form byte", name)
	return out
}

// dualCodec is one page's reader and writer, whatever its record.
type dualCodec struct {
	name  string
	read  func([]byte) error
	write func([]byte, schemaread.Form) ([]byte, error)
}

func dualCodecOf[T any](c *RowCodec[T]) *dualCodec {
	return &dualCodec{
		name: c.rowName(),
		read: func(in []byte) error { _, _, err := c.Read(in); return err },
		write: func(legacy []byte, form schemaread.Form) ([]byte, error) {
			v, f, err := c.Read(legacy)
			if err != nil {
				return nil, err
			}
			if f != schemaread.FormLegacy {
				return nil, fmt.Errorf("%s: the dump page is not legacy JSON (%s)", c.rowName(), f)
			}
			return c.Write(&v, form)
		},
	}
}

// dualPageCodec is the reader of a dump key's page; nil for the kv values, one
// JSON string each and no record of the snaprows area.
func dualPageCodec(key string) *dualCodec {
	n := sprint.Names{}
	switch {
	case key == n.Key("mem:meta"):
		return dualCodecOf(memSnapshotRows)
	case strings.HasPrefix(key, "view:"):
		return dualCodecOf(snapViewRows)
	case strings.HasPrefix(key, n.Key("mem:kv:")):
		return nil
	case strings.HasPrefix(key, n.Key("mem:dropped:")):
		return dualCodecOf(snapResidueRows)
	case strings.HasPrefix(key, n.Key("mem:log")):
		return dualCodecOf(snapLogRows)
	case strings.HasPrefix(key, "table:") && strings.HasSuffix(key, ":epoch"):
		return dualCodecOf(snapEpochRows)
	case strings.HasPrefix(key, "table:"):
		return dualCodecOf(snapTableRows)
	}
	return dualCodecOf(snapMemberRows)
}

// dualPage is a dump key's page written in form.
func dualPage(t *testing.T, k DumpKey, form schemaread.Form) []byte {
	t.Helper()
	c := dualPageCodec(k.Key)
	if c == nil {
		return k.Payload
	}
	b, err := c.write(k.Payload, form)
	require.NoError(t, err, "the page %s writes as %s", k.Key, form)
	require.NotEmpty(t, b, "the page %s writes as %s", k.Key, form)
	return b
}

func pageCounts() map[string]map[string]uint64 {
	out := map[string]map[string]uint64{}
	for _, c := range []rowSource{memSnapshotRows, snapViewRows, snapResidueRows, snapLogRows, snapEpochRows, snapTableRows, snapMemberRows} {
		out[c.rowName()] = c.rowSnapshot()
	}
	return out
}

// dualOprowsImage is a MemSnapshot image whose log holds an inbox note: an
// oprows record, held as a blob, that this reader does not decode.
func dualOprowsImage(t *testing.T) []byte {
	t.Helper()
	text := `{"version":1,"tables":[],"dropped":[],"views":[],"epoch_set":true,"epoch":21,"cleared":0,"owed":false,"defaults_owed":0,` +
		`"logs":[{"epoch":21,"log":{"fence":null,"gen":1,"done":[],"progress":[],"inbox":[{"id":"1-0","note":"AQID"}],"lines":[],"notes":[],` +
		`"aliases":[],"answered":[],"open":[],"cursor":"","queue":[]}}],"kv":[],"seq":1}`
	var b snapwire.MemSnapshotBuilder
	require.True(t, b.Init())
	var rep snapwire.TableReport
	require.True(t, snapwire.MemSnapshotFromJson(&b, []byte(text), &rep), "the image's schema JSON reads: %+v", rep)
	buf := make([]byte, snapwire.MemSnapshotMeasure(b.GetRoot(), &b.Arena))
	n := snapwire.MemSnapshotSave(b.GetRoot(), buf, &b.Arena)
	require.Positive(t, n)
	return buf[:n]
}

// dualConsumerJSON is the cost record as the Consumer table's schema JSON text,
// written by the generated writer.
func dualConsumerJSON(t *testing.T, c sprint.Consumer) []byte {
	t.Helper()
	img := dualConsumerImage(t, c)
	n, err := snapwire.ConsumerLoadMeasureReason(img)
	require.NoError(t, err)
	var rep snapwire.TableReport
	v := snapwire.ConsumerLoad(make([]byte, n), img, &rep)
	require.NotNil(t, v)
	buf := make([]byte, snapwire.ConsumerToJsonMeasure(v))
	return buf[:snapwire.ConsumerToJson(v, buf)]
}

// dualConsumerImage is the cost record as the Consumer table's image.
func dualConsumerImage(t *testing.T, c sprint.Consumer) []byte {
	t.Helper()
	raw, err := json.Marshal(c)
	require.NoError(t, err)
	text := strings.Replace(string(raw), `"long":`, `"long_context":`, 1)
	var b snapwire.ConsumerBuilder
	require.True(t, b.Init())
	var rep snapwire.TableReport
	require.True(t, snapwire.ConsumerFromJson(&b, []byte(text), &rep), "the cost record's schema JSON reads: %s %+v", text, rep)
	require.Zero(t, rep.Unknown, "every field of the cost record is a field of the table: %s", text)
	buf := make([]byte, snapwire.ConsumerMeasure(b.GetRoot(), &b.Arena))
	n := snapwire.ConsumerSave(b.GetRoot(), buf, &b.Arena)
	require.Positive(t, n)
	return buf[:n]
}

// dualDamage is a deterministic fuzz of malformed values of one form: every
// truncation (samples of them on a long value, the last byte always among them), an unknown form byte for an image, and
// for JSON text a closing brace removed and a list where an object is.
func dualDamage(good []byte, legacy bool, samples int) [][]byte {
	var out [][]byte
	if line := string(good); legacy && strings.HasPrefix(line, "kind=") {
		// the cost line reads any words, so its malformed values are its counts:
		// a count that is not a number, an empty count, and a cut kind word
		for _, k := range []string{"attempt=", "take=", "gen="} {
			out = append(out, []byte(strings.Replace(line, k, k+"x", 1)))
			out = append(out, []byte(strings.Replace(line, k, k+" ", 1)))
		}
		return append(out, []byte("kin"), []byte("kind"))
	}
	step := 1
	if len(good) > 2*samples {
		step = len(good)/samples + 1
	}
	cuts := []int{len(good) - 1}
	for n := 1; n < len(good)-1; n += step {
		cuts = append(cuts, n)
	}
	for _, n := range cuts {
		if n < 1 {
			continue
		}
		cut := good[:n]
		if t := strings.TrimSpace(string(cut)); t == "" || t == strings.TrimSpace(string(good)) {
			continue // blank, or the whole value without its trailing newline
		}
		out = append(out, append([]byte(nil), cut...))
	}
	out = append(out, []byte{})
	switch {
	case len(good) > 0 && good[0] == 1:
		bad := append([]byte(nil), good...)
		bad[0] = 2
		out = append(out, bad)
	case len(good) > 0 && good[0] == '{':
		out = append(out, []byte(strings.TrimSuffix(strings.TrimSpace(string(good)), "}")))
		out = append(out, []byte("["+string(good)+"]"))
	}
	return out
}

// dualRefused requires err to be a refusal by name: `<NAME> REFUSED: <reason>;
// run: <help>`, with reason when one is named.
func dualRefused(t *testing.T, name, reason string, err error) {
	t.Helper()
	require.Error(t, err, "%s refuses", name)
	var r *schemaread.Refusal
	require.True(t, errors.As(err, &r), "%s: the refusal is a *schemaread.Refusal: %v", name, err)
	assert.Equal(t, name, r.Name, "%s: the refusal names the record: %v", name, err)
	assert.Contains(t, err.Error(), name+" REFUSED: ")
	assert.Contains(t, err.Error(), "; run: ")
	assert.NotEmpty(t, r.Reason)
	if reason != "" {
		assert.Equal(t, reason, r.Reason, "%s: %v", name, err)
	}
}

func dualShort(b []byte) string {
	if len(b) > 240 {
		return string(b[:120]) + " ... " + string(b[len(b)-120:])
	}
	return string(b)
}
