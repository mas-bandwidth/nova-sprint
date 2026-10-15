package store

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/donewire"
	donewirev2 "github.com/mas-bandwidth/nova-sprint/internal/sprint/store/donewire/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The done record as a FIXED table (schemas/store/done.schema, the tracer
// schema-tracer-done-record-fixed; docs/research/schema-tracer/done-record.md): the
// record's image is the schema's form-3 file of one record, a newer layout reads an
// older image, an older layout refuses a newer one by the schema's name (layout_newer),
// and hostile images are refused by name. Time is an argument (fixed instants), and
// nothing here touches a socket or a service.

var doneT0 = time.Date(2026, 10, 15, 12, 13, 36, 123456789, time.UTC)

// doneSample is a typical record: three completions, one a re-read of the same line (the
// TLA model's StaleWrite: the record holds it twice and CountDone counts it once).
func doneSample() DoneRecord {
	return DoneRecord{Epoch: 21, Cursor: "1760530416123-0", Done: []sprint.Completion{
		{Kind: sprint.DoneKindWork, Card: "s1-1.w1", Row: "m1", At: doneT0},
		{Kind: sprint.DoneKindRead, Card: "s1-1.r1.reader-a", Row: "m2", At: doneT0.Add(time.Minute)},
		{Kind: sprint.DoneKindRead, Card: "s1-1.r1.reader-a", Row: "m2", At: doneT0.Add(time.Minute)},
	}}
}

// doneEdge is a record at every declared bound: the most completions the fixed form holds,
// every text at its full length, and a zero time.
func doneEdge() DoneRecord {
	word := strings.Repeat("w", sprint.MaxIDLen)
	r := DoneRecord{Epoch: ^uint64(0), Cursor: "18446744073709551615-18446744073709551615"}
	for i := range doneFixedMaxDone {
		c := sprint.Completion{Kind: strings.Repeat("k", 16), Card: word + "." + word + "." + word + "." + word,
			Row: sprint.FriendRow(word), At: doneT0.Add(time.Duration(i))}
		if i == 0 {
			c.At = time.Time{}
		}
		r.Done = append(r.Done, c)
	}
	return r
}

// sameDone says two records are the same record: times compared as instants (the fixed form
// keeps the instant, UTC; the legacy JSON kept the writer's zone too).
func sameDone(t *testing.T, want, got DoneRecord) {
	t.Helper()
	require.Equal(t, want.Epoch, got.Epoch)
	require.Equal(t, want.Cursor, got.Cursor)
	require.Len(t, got.Done, len(want.Done))
	for i := range want.Done {
		w, g := want.Done[i], got.Done[i]
		require.True(t, w.At.Equal(g.At), "completion %d at %v, read %v", i, w.At, g.At)
		w.At, g.At = time.Time{}, time.Time{}
		require.Equal(t, w, g, "completion %d", i)
	}
}

// fixedImage is the record's stored form, which must be the fixed form.
func fixedImage(t *testing.T, r DoneRecord) []byte {
	t.Helper()
	raw, form, err := encodeDoneRecord(r)
	require.NoError(t, err)
	require.Equal(t, doneFormFixed, form, "the record keeps the fixed form")
	return []byte(raw)
}

func TestDoneRecordFixedRoundTrip(t *testing.T) {
	t.Parallel()
	for name, r := range map[string]DoneRecord{"typical": doneSample(), "edge": doneEdge(), "empty": {Epoch: 1}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := fixedImage(t, r)
			require.Equal(t, donewire.TableFixedForm, b[0], "the image opens with form byte 3")
			require.EqualValues(t, donewire.DoneRecordFixedMeasure(1), len(b), "one record, the same size whatever it holds")
			got, why := decodeDoneRecord(string(b))
			require.Empty(t, why)
			sameDone(t, r, got)
			// the TLA model's counting (CountDone dedupe) reads the same off the decoded record
			assert.Equal(t, sprint.CountDone(r.Done, time.Time{}), sprint.CountDone(got.Done, time.Time{}))
			assert.Equal(t, sprint.CountDone(r.Done, doneT0), sprint.CountDone(got.Done, doneT0))
		})
	}
}

// A v2 layout (one field appended: donewire/v2) reads a v1 image: every v1 field lands, the
// appended field takes its declared default, and the report is silent.
func TestDoneRecordV2ReadsAV1Image(t *testing.T) {
	t.Parallel()
	r := doneSample()
	b := fixedImage(t, r)
	out := make([]donewirev2.DoneRecord, 1)
	plan := make([]donewirev2.TableFixedEntry, 2*len(donewirev2.DoneRecordFixedPlan.Entries))
	var rep donewirev2.TableReport
	n := donewirev2.DoneRecordFixedLoad(out, b, plan, &rep)
	require.EqualValues(t, 1, n, "v2 reads the v1 image: %+v", rep)
	require.Equal(t, donewirev2.TableReport{}, rep, "a v1 image reads silently under v2")
	v2 := &out[0]
	assert.Equal(t, r.Epoch, v2.Epoch)
	assert.Equal(t, r.Cursor, string(v2.Cursor[:v2.CursorLength]))
	require.EqualValues(t, len(r.Done), v2.DoneCount)
	for i, c := range r.Done {
		e := &v2.Done[i]
		assert.Equal(t, c.Kind, string(e.Kind[:e.KindLength]))
		assert.Equal(t, c.Card, string(e.Card[:e.CardLength]))
		assert.Equal(t, c.Row, string(e.Row[:e.RowLength]))
		assert.Equal(t, c.At.UnixNano(), e.At)
	}
	assert.Zero(t, v2.Wrote, "the appended field takes its declared default")
}

// v2Image is a v2 writer's image of the sample record.
func v2Image(t *testing.T) []byte {
	t.Helper()
	var v donewirev2.DoneRecord
	donewirev2.DoneRecordReset(&v)
	v.Epoch = 21
	v.CursorLength = int32(copy(v.Cursor[:], "1760530416123-0"))
	v.Wrote = doneT0.UnixNano()
	b := make([]byte, donewirev2.DoneRecordFixedMeasure(1))
	require.EqualValues(t, len(b), donewirev2.DoneRecordFixedSave([]donewirev2.DoneRecord{v}, b))
	return b
}

// doneKV is a fake KV: the done record's key and the writes made to it.
type doneKV struct {
	vals   map[string]string
	writes int
}

func (k *doneKV) GetKey(_ context.Context, name string) (string, bool, error) {
	v, ok := k.vals[name]
	return v, ok, nil
}

func (k *doneKV) SetKey(_ context.Context, name, value string) error {
	k.vals[name] = value
	k.writes++
	return nil
}

func (k *doneKV) SetKeyShowing(ctx context.Context, name, value, _, _ string) error {
	return k.SetKey(ctx, name, value)
}

func (k *doneKV) ShowState(context.Context, string, string) error { return nil }

// v1 (this build) refuses a v2 image by the schema's name, reads nothing, and the tick
// refuses loudly rather than write its older layout over the newer record.
func TestDoneRecordV1RefusesAV2Image(t *testing.T) {
	t.Parallel()
	b := v2Image(t)
	got, why := decodeDoneRecord(string(b))
	require.Equal(t, "layout_newer", why)
	require.Equal(t, DoneRecord{}, got, "a refusal reads nothing")
	st := &Store{epoch: 21}
	assert.Equal(t, DoneRecord{Epoch: 21}, st.doneRecordOf(string(b), true), "where reads the log whole instead")
	kv := &doneKV{vals: map[string]string{keyDoneRecord: string(b)}}
	err := st.keepDone(context.Background(), kv, string(b), true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "layout_newer")
	assert.Zero(t, kv.writes, "the older build never writes over a newer record")
	assert.Equal(t, string(b), kv.vals[keyDoneRecord])
}

// put32 writes v little-endian at b[at:].
func put32(b []byte, at int, v uint32) { binary.LittleEndian.PutUint32(b[at:], v) }

func TestDoneRecordFixedRefusesHostileImagesByName(t *testing.T) {
	t.Parallel()
	good := fixedImage(t, doneSample())
	layoutAt := donewire.TableFixedHeaderBytes + 4
	recordAt := layoutAt + len(donewire.DoneRecordFixedLayout)
	bodyAt := recordAt + 8
	mut := func(f func(b []byte) []byte) string {
		b := append([]byte(nil), good...)
		return string(f(b))
	}
	cases := []struct {
		name, image, why string
	}{
		{"empty", "", "empty"},
		{"truncated record", mut(func(b []byte) []byte { return b[:len(b)-1] }), "malformed"},
		{"truncated header", mut(func(b []byte) []byte { return b[:10] }), "malformed"},
		{"truncated layout", mut(func(b []byte) []byte { return b[:layoutAt+20] }), "layout_malformed"},
		{"no record", mut(func(b []byte) []byte { return b[:recordAt] }), "record_count"},
		{"two records", mut(func(b []byte) []byte { return append(b, b[recordAt:]...) }), "batch_too_large"},
		// a hash this build has no layout for reads as newer: the reader cannot tell a
		// corrupt hash from a newer writer's (the finding's schema improvements)
		{"bad file hash", mut(func(b []byte) []byte { b[donewire.TableFixedHashAt] ^= 0xff; return b }), "layout_newer"},
		{"bad layout byte", mut(func(b []byte) []byte { b[layoutAt+8] ^= 0xff; return b }), "layout_malformed"},
		{"bad record hash", mut(func(b []byte) []byte { b[recordAt] ^= 0xff; return b }), "no_layout"},
		{"reserved byte", mut(func(b []byte) []byte { b[3] = 1; return b }), "malformed"},
		{"variable form", mut(func(b []byte) []byte { b[0] = 1; return b }), "previous_form"},
		{"message form", mut(func(b []byte) []byte { b[0] = 2; return b }), "message_form_as_file"},
		{"unknown form", mut(func(b []byte) []byte { b[0] = 9; return b }), "newer_form"},
		// the done list's count past its declared max (95): the runtime clamps and counts it,
		// and the store refuses a record a clamp would cut short
		{"over-max count", mut(func(b []byte) []byte { put32(b, bodyAt+53, 200); return b }), "clamped"},
		// a text length past its bound: the runtime clamps it to the bound and checks the
		// clamped units as text first, so zero slack refuses as text_ill_formed (the spec
		// calls a length past the bound malformed: the finding's schema improvements)
		{"text over bound", mut(func(b []byte) []byte { put32(b, bodyAt+8, 99); return b }), "text_ill_formed"},
		{"text over bound, text slack", mut(func(b []byte) []byte {
			put32(b, bodyAt+8, 99)
			copy(b[bodyAt+12:bodyAt+12+41], strings.Repeat("9", 41))
			return b
		}), "clamped"},
		{"text ill formed", mut(func(b []byte) []byte { b[bodyAt+8+4] = 0xff; return b }), "text_ill_formed"},
		{"legacy json unreadable", `{"epoch":`, "legacy_json_unreadable"},
	}
	for _, c := range cases {
		got, why := decodeDoneRecord(c.image)
		assert.Equal(t, c.why, why, c.name)
		assert.Equal(t, DoneRecord{}, got, "%s: a refusal reads nothing", c.name)
	}
}

// A record the fixed form cannot hold keeps the legacy JSON form, by name, and still reads.
func TestDoneRecordPastTheFixedBoundsKeepsTheLegacyForm(t *testing.T) {
	t.Parallel()
	many := doneSample()
	for len(many.Done) <= doneFixedMaxDone {
		many.Done = append(many.Done, many.Done[0])
	}
	long := doneSample()
	long.Done[0].Card = strings.Repeat("c", 516)
	nul := doneSample()
	nul.Done[0].Row = "m\x001"
	far := doneSample()
	far.Done[0].At = time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC)
	for name, c := range map[string]struct {
		r   DoneRecord
		why string
	}{"over max count": {many, "over_max_count"}, "text over bound": {long, "text_over_bound"},
		"text ill formed": {nul, "text_ill_formed"}, "time out of range": {far, "time_out_of_range"}} {
		raw, form, err := encodeDoneRecord(c.r)
		require.NoError(t, err)
		assert.Equal(t, "json:"+c.why, form, name)
		require.Equal(t, byte('{'), raw[0], name)
		got, why := decodeDoneRecord(raw)
		require.Empty(t, why, name)
		require.Equal(t, len(c.r.Done), len(got.Done), name)
	}
}

// Today's stored form, JSON as the build before this one wrote it, reads for one release:
// the old form is told by its first byte, '{', which no fixed image (form byte 3) has.
func TestDoneRecordLegacyJSONStillReads(t *testing.T) {
	t.Parallel()
	r := doneSample()
	b, err := json.Marshal(r)
	require.NoError(t, err)
	got, why := decodeDoneRecord(string(b))
	require.Empty(t, why)
	sameDone(t, r, got)
}

// The tick keeps the record in the fixed form, and the model's invariants hold through it
// (tla/DoneRecord.tla), the machine STOPPED so the tick deals nothing: caught up, where shows
// the truth (CaughtUpIsTruth); a stale write of an older image (StaleWrite) or a retried tick
// makes the next tick read lines again, and nothing counts twice (NoDoubleCount).
func TestDoneRecordFixedKeepsTheModel(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(3)
	h.machine()
	pinned, err := h.st.pin(h.ctx)
	require.NoError(t, err)
	kv, err := pinned.kv()
	require.NoError(t, err)
	stale, ok, err := kv.GetKey(h.ctx, keyDoneRecord)
	require.NoError(t, err)
	require.True(t, ok, "the tick kept a done record")
	require.Equal(t, donewire.TableFixedForm, stale[0], "kept in the fixed form")
	h.through("s1-1", "s1-2", "s1-3")
	h.machine()
	truth, err := pinned.DoneSince(h.ctx, time.Time{})
	require.NoError(t, err)
	total := func(d sprint.DoneCounts) int {
		n := 0
		for row := range d {
			n += d.Total(row)
		}
		return n
	}
	require.Equal(t, 9, total(truth), fmt.Sprintf("three work cards and two reads of each: %v", truth))
	caught, _, err := kv.GetKey(h.ctx, keyDoneRecord)
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(h.ctx, keyDoneRecord, stale)) // StaleWrite: the older record back
	h.machine()
	again, err := pinned.DoneSince(h.ctx, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, truth, again, "the stale record catches up to the truth")
	// a retried tick: the caught-up entries under the older cursor, so every line is read again
	old, why := decodeDoneRecord(stale)
	require.Empty(t, why)
	r, why := decodeDoneRecord(caught)
	require.Empty(t, why)
	r.Cursor = old.Cursor
	retried, form, err := encodeDoneRecord(r)
	require.NoError(t, err)
	require.Equal(t, doneFormFixed, form)
	require.NoError(t, kv.SetKey(h.ctx, keyDoneRecord, retried))
	h.machine()
	again, err = pinned.DoneSince(h.ctx, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, truth, again, "lines read again count once")
	raw, _, err := kv.GetKey(h.ctx, keyDoneRecord)
	require.NoError(t, err)
	r, why = decodeDoneRecord(raw)
	require.Empty(t, why)
	assert.Len(t, r.Done, 2*len(old.Done)+2*9, "the record holds the re-read lines twice, as the model's does")
}

// doneOf is a record of n completions shaped like today's (a read card id, a member row).
func doneOf(n int) DoneRecord {
	r := DoneRecord{Epoch: 21, Cursor: "1760530416123-0"}
	for i := range n {
		r.Done = append(r.Done, sprint.Completion{Kind: sprint.DoneKindRead,
			Card: fmt.Sprintf("oracle-rows-identical-on-x86-64-and-arm64.r%d.flash-mercury", i), Row: "hetzner2", At: doneT0.Add(time.Duration(i))})
	}
	return r
}

// The two forms' bytes and decode cost: go test -run '^$' -bench DoneRecord ./internal/sprint/store/
func BenchmarkDoneRecordDecode(b *testing.B) {
	for _, n := range []int{3, doneFixedMaxDone} {
		r := doneOf(n)
		fixed, form, err := encodeDoneRecord(r)
		require.NoError(b, err)
		require.Equal(b, doneFormFixed, form)
		legacy, err := json.Marshal(r)
		require.NoError(b, err)
		for name, raw := range map[string]string{"fixed": fixed, "json": string(legacy)} {
			b.Run(fmt.Sprintf("%s/%d", name, n), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, why := decodeDoneRecord(raw); why != "" {
						b.Fatal(why)
					}
				}
			})
		}
	}
}

func BenchmarkDoneRecordEncode(b *testing.B) {
	for _, n := range []int{3, doneFixedMaxDone} {
		r := doneOf(n)
		b.Run(fmt.Sprintf("fixed/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _, _ = encodeDoneRecord(r)
			}
		})
		b.Run(fmt.Sprintf("json/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = json.Marshal(r)
			}
		})
	}
}
