package store

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/donewire"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRowCodecForms pins the one store row reader (S1, S2, S4). A stored value
// is read in each form through pkg/schemaread: the fixed binary image, its
// padded base64, the schema JSON text, and today's legacy JSON. A damaged
// value is refused by name. Reads are counted per record and per form, the
// store's CheckReport carries the counts, and the check report's JSON prints
// them as row_forms, which is what `nova-sprint check --json` marshals.
func TestRowCodecForms(t *testing.T) {
	t.Parallel()

	// The test clock starts at 1000.0 seconds, never 0.
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "row-"}, Actor: "tester",
		Now:   func() time.Time { return now },
		Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(t.Context()))

	want := rowCodecSample()
	done := NewRowCodec(rowCodecDoneFns())
	note := NewRowCodec(rowCodecNoteFns())
	done.Register(st)
	note.Register(st)

	bin, err := done.Write(want, schemaread.FormBinary)
	require.NoError(t, err)
	require.NotEmpty(t, bin)
	assert.Equal(t, byte(3), bin[0], "the fixed image opens with form byte 3")

	b64, err := done.Write(want, schemaread.FormBase64)
	require.NoError(t, err)
	assert.Equal(t, base64.StdEncoding.EncodeToString(bin), string(b64), "text carries padded standard base64 of the wire")

	js, err := done.Write(want, schemaread.FormJSON)
	require.NoError(t, err)
	assert.Equal(t, byte('\n'), js[len(js)-1], "the schema JSON text ends with one newline")

	leg, err := json.Marshal(struct {
		Epoch  uint64 `json:"epoch"`
		Cursor string `json:"cursor"`
		Pages  uint32 `json:"pages"`
	}{want.Epoch, string(want.Cursor[:want.CursorLength]), want.Pages})
	require.NoError(t, err)
	assert.False(t, bytesHasNewline(leg), "today's legacy JSON has no trailing newline")

	for _, tc := range []struct {
		name string
		in   []byte
		form schemaread.Form
	}{
		{"fixed binary", bin, schemaread.FormBinary},
		{"padded base64", b64, schemaread.FormBase64},
		{"schema JSON", js, schemaread.FormJSON},
		{"legacy JSON", leg, schemaread.FormLegacy},
	} {
		got, form, err := done.Read(tc.in)
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.form, form, tc.name)
		assert.Equal(t, want.Epoch, got.Epoch, tc.name)
		assert.Equal(t, want.Pages, got.Pages, tc.name)
		assert.Equal(t, string(want.Cursor[:want.CursorLength]), string(got.Cursor[:got.CursorLength]), tc.name)
	}

	damaged := append([]byte(nil), bin...)
	damaged[donewire.TableFixedHashAt] ^= 0xff
	_, form, err := done.Read(damaged)
	assert.Equal(t, schemaread.FormBinary, form, "a damaged fixed image is still the binary form")
	var ref *schemaread.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Equal(t, "DONERECORD REFUSED: layout_newer; run: nova-sprint check", err.Error())

	_, _, err = done.Read(nil)
	assert.EqualError(t, err, "DONERECORD REFUSED: unknown_form; run: nova-sprint check")

	noted, err := note.Write(&rowNote{Text: "one"}, schemaread.FormBinary)
	require.NoError(t, err)
	gotNote, form, err := note.Read(noted)
	require.NoError(t, err)
	assert.Equal(t, schemaread.FormBinary, form)
	assert.Equal(t, "one", gotNote.Text)

	assert.Equal(t, map[string]uint64{
		"binary": 2, "base64": 1, "json": 1, "legacy": 1,
		"unknown_fields": 0, "refused_layout_newer": 1, "refused_unknown_form": 1,
	}, done.Counts().Snapshot())
	assert.Equal(t, map[string]uint64{
		"binary": 1, "base64": 0, "json": 0, "legacy": 0, "unknown_fields": 0,
	}, note.Counts().Snapshot())

	rep, _, err := st.Check(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, done.Counts().Snapshot(), rep.RowForms["DONERECORD"])
	assert.Equal(t, note.Counts().Snapshot(), rep.RowForms["NOTE"])

	// cmdCheck prints json.Marshal of this report. The field name is row_forms.
	b, err := json.Marshal(rep)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(b, &decoded))
	forms, ok := decoded["row_forms"].(map[string]any)
	require.True(t, ok, "check JSON prints row_forms: %s", b)
	assert.Contains(t, forms, "DONERECORD")
	assert.Contains(t, forms, "NOTE")
}

func bytesHasNewline(b []byte) bool {
	for _, c := range b {
		if c == '\n' {
			return true
		}
	}
	return false
}

func rowCodecSample() *donewire.DoneRecord {
	var v donewire.DoneRecord
	donewire.DoneRecordReset(&v)
	v.Epoch = 7
	v.Pages = 1
	v.CursorLength = int32(copy(v.Cursor[:], "cursor-7"))
	return &v
}

func rowCodecDoneFns() RowFns[donewire.DoneRecord] {
	return RowFns[donewire.DoneRecord]{
		Name:  "DONERECORD",
		Reset: func(v *donewire.DoneRecord) { donewire.DoneRecordReset(v) },
		Measure: func(wire []byte) error {
			if int64(len(wire)) < donewire.DoneRecordFixedMeasure(1) {
				return errors.New("wire_damaged")
			}
			return nil
		},
		LoadWire:   rowCodecLoadDone,
		LoadJSON:   rowCodecLoadDoneJSON,
		Legacy:     LegacyObject,
		LoadLegacy: rowCodecLoadDoneLegacy,
		SaveWire: func(v *donewire.DoneRecord) []byte {
			n := donewire.DoneRecordFixedMeasure(1)
			buf := make([]byte, n)
			if donewire.DoneRecordFixedSave([]donewire.DoneRecord{*v}, buf) != n {
				return nil
			}
			return buf
		},
		SaveJSON: func(v *donewire.DoneRecord) []byte {
			n := donewire.DoneRecordToJsonMeasure(v)
			if n < 0 {
				return nil
			}
			buf := make([]byte, n)
			if donewire.DoneRecordToJson(v, buf) != n {
				return nil
			}
			return buf
		},
	}
}

func rowCodecLoadDone(v *donewire.DoneRecord, wire []byte) RowReport {
	out := make([]donewire.DoneRecord, 1)
	plan := make([]donewire.TableFixedEntry, 2*len(donewire.DoneRecordFixedPlan.Entries))
	var rep donewire.TableReport
	n := donewire.DoneRecordFixedLoad(out, wire, plan, &rep)
	if n == 1 {
		*v = out[0]
	}
	return rowCodecReport(rep, n)
}

func rowCodecLoadDoneJSON(v *donewire.DoneRecord, text []byte) RowReport {
	var rep donewire.TableReport
	ok := donewire.DoneRecordFromJson(v, text, &rep)
	rr := rowCodecReport(rep, 1)
	if !ok && rr.Reason == "" && !rr.Malformed {
		rr.Malformed = true
	}
	return rr
}

func rowCodecLoadDoneLegacy(v *donewire.DoneRecord, in []byte) RowReport {
	var leg struct {
		Epoch  uint64 `json:"epoch"`
		Cursor string `json:"cursor"`
		Pages  uint32 `json:"pages"`
	}
	if err := json.Unmarshal(in, &leg); err != nil {
		return RowReport{Malformed: true, Reason: "legacy_json_unreadable"}
	}
	donewire.DoneRecordReset(v)
	if len(leg.Cursor) > len(v.Cursor) {
		return RowReport{Clamped: 1}
	}
	v.Epoch = leg.Epoch
	v.Pages = leg.Pages
	v.CursorLength = int32(copy(v.Cursor[:], leg.Cursor))
	return RowReport{}
}

func rowCodecReport(rep donewire.TableReport, n int64) RowReport {
	rr := RowReport{
		Unknown: int(rep.Unknown), KindMismatch: int(rep.KindMismatch), Clamped: int(rep.Clamped),
		Malformed: rep.Malformed, Duplicate: int(rep.Duplicate), Reason: rep.Reason,
	}
	if n < 0 && rr.Reason == "" && !rr.Malformed {
		rr.Malformed = true
	}
	if n >= 0 && n != 1 && rr.Reason == "" {
		rr.Malformed = true
		rr.Reason = "record_count"
	}
	return rr
}

// rowNote is a second record, so the counts are per record and not one pool.
type rowNote struct {
	Text string `json:"text"`
}

func rowCodecNoteFns() RowFns[rowNote] {
	return RowFns[rowNote]{
		Name: "NOTE",
		Measure: func(wire []byte) error {
			if len(wire) < 1 || wire[0] != 3 {
				return errors.New("wire_damaged")
			}
			return nil
		},
		LoadWire: func(v *rowNote, wire []byte) RowReport {
			if len(wire) < 1 || wire[0] != 3 {
				return RowReport{Malformed: true}
			}
			v.Text = string(wire[1:])
			return RowReport{}
		},
		LoadJSON: func(v *rowNote, text []byte) RowReport {
			if err := json.Unmarshal(text, v); err != nil {
				return RowReport{Malformed: true}
			}
			return RowReport{}
		},
		Legacy:     LegacyObject,
		LoadLegacy: func(v *rowNote, in []byte) RowReport { return DecodeLegacy(v, in) },
		SaveWire: func(v *rowNote) []byte {
			return append([]byte{3}, v.Text...)
		},
		SaveJSON: func(v *rowNote) []byte {
			b, err := json.Marshal(v)
			if err != nil {
				return nil
			}
			return append(b, '\n')
		},
	}
}
