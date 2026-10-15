package store

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/donewire"
	donewirev2 "github.com/mas-bandwidth/nova-sprint/internal/sprint/store/donewire/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDoneRecordFixedPaged pins C28 (schemas/store/store.schema, internal/sprint/store/done.go,
// internal/sprint/store/teardown.go, tla/DoneRecord.tla):
//  1. Compaction to one entry per (row, kind, card) keeps sprint.CountDone equal at every mark.
//  2. Every stored page is a form-3 fixed table holding at most 95 entries, and a 310-completion
//     record round-trips across 4 pages (95 + 95 + 95 + 25) and is deleted whole by Teardown.
//  3. Legacy JSON (first byte '{') is read once and rewritten into the fixed paged form.
//  4. An older build meeting a newer layout (donewire/v2) refuses by name (layout_newer) and
//     writes nothing over the newer record (OlderBuildNeverWritesNewerLayout), while DoneSince
//     reads the epoch's log whole; v2 reads a v1 page silently; hostile images refuse by name.
func TestDoneRecordFixedPaged(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

	h := newHarness(t)
	m := NewMem()
	h.m, h.st.B = m, m
	require.NoError(t, h.st.Init(h.ctx))
	pinned, err := h.st.pin(h.ctx)
	require.NoError(t, err)
	kv, err := pinned.kv()
	require.NoError(t, err)

	// 1. Append 310 distinct completions plus duplicate re-completions at later timestamps.
	var uncompacted []sprint.Completion
	appendRead := func(id, row string, at time.Time) {
		l := sprint.Line{
			At:      at,
			Epoch:   pinned.epoch,
			Table:   sprint.Fleet,
			Removed: true,
			From:    row + ":reading",
			Card:    id,
			Set:     map[string]string{"verdict": "ok"},
		}
		uncompacted = append(uncompacted, sprint.CompletionsOf(l)...)
		m.mu.Lock()
		m.seq++
		lg := m.log()
		lg.lines = append(lg.lines, memLine{fmt.Sprintf("%d-0", m.seq), l})
		m.mu.Unlock()
	}

	for i := range 310 {
		at := t0.Add(time.Duration(i+1) * time.Second)
		card := fmt.Sprintf("card-fixed-paged-%03d.r1.emma", i)
		row := fmt.Sprintf("member-%02d", i%5)
		appendRead(card, row, at)
		if i < 15 {
			// Duplicate completion of the same (row, kind, card) at a later timestamp:
			// compaction must keep one entry per (row, kind, card) and preserve CountDone
			// at every mark before, between, and after the duplicate timestamps.
			appendRead(card, row, at.Add(500*time.Second))
		}
	}
	require.Len(t, uncompacted, 325)

	// Catch the done record up across log pages.
	for range 8 {
		raw, ok, err := kv.GetKey(h.ctx, keyDoneRecord)
		require.NoError(t, err)
		require.NoError(t, pinned.keepDone(h.ctx, kv, raw, ok))
	}

	// 2. Verify page 0 and follow-on pages in KV: 310 compacted entries across 4 pages (95, 95, 95, 25).
	pageKeys := []string{keyDoneRecord, "donerecord:1", "donerecord:2", "donerecord:3"}
	wantPageCounts := []int32{95, 95, 95, 25}
	perPageBytes := int(donewire.DoneRecordFixedMeasure(1))
	t.Logf("per-page byte size at 95 entries: %d bytes", perPageBytes)
	var page0Raw string
	for i, pk := range pageKeys {
		raw, ok, err := kv.GetKey(h.ctx, pk)
		require.NoError(t, err)
		require.True(t, ok, "page %d (%s) must be written", i, pk)
		if i == 0 {
			page0Raw = raw
		}
		require.Equal(t, donewire.TableFixedForm, raw[0], "page %d (%s) must open with fixed form byte 3", i, pk)
		require.Len(t, raw, perPageBytes, "page %d (%s) fixed byte size", i, pk)

		out := make([]donewire.DoneRecord, 1)
		plan := make([]donewire.TableFixedEntry, 2*len(donewire.DoneRecordFixedPlan.Entries))
		var rep donewire.TableReport
		n := donewire.DoneRecordFixedLoad(out, []byte(raw), plan, &rep)
		require.EqualValues(t, 1, n, "page %d loads cleanly: %+v", i, rep)
		assert.LessOrEqual(t, out[0].DoneCount, int32(95), "every page holds at most 95 entries")
		assert.Equal(t, wantPageCounts[i], out[0].DoneCount, "page %d entry count", i)
		assert.EqualValues(t, 4, out[0].Pages, "page %d records total pages = 4", i)
	}
	_, extraOK, err := kv.GetKey(h.ctx, "donerecord:4")
	require.NoError(t, err)
	assert.False(t, extraOK, "no fifth page for 310 entries")

	// v2 layout reads a v1 page silently, with the appended field taking its zero default.
	v2Out := make([]donewirev2.DoneRecord, 1)
	v2Plan := make([]donewirev2.TableFixedEntry, 2*len(donewirev2.DoneRecordFixedPlan.Entries))
	var v2Rep donewirev2.TableReport
	require.EqualValues(t, 1, donewirev2.DoneRecordFixedLoad(v2Out, []byte(page0Raw), v2Plan, &v2Rep))
	assert.Equal(t, donewirev2.TableReport{}, v2Rep)
	assert.EqualValues(t, 95, v2Out[0].DoneCount)
	assert.EqualValues(t, 4, v2Out[0].Pages)
	assert.Zero(t, v2Out[0].Wrote)

	// Hostile / damaged fixed images refuse by name through decodeDoneRecord.
	layoutAt := donewire.TableFixedHeaderBytes + 4
	recordAt := layoutAt + len(donewire.DoneRecordFixedLayout)
	bodyAt := recordAt + 8
	mut := func(f func(b []byte) []byte) string {
		b := append([]byte(nil), page0Raw...)
		return string(f(b))
	}
	put32 := func(b []byte, at int, v uint32) { binary.LittleEndian.PutUint32(b[at:], v) }
	for _, tc := range []struct {
		name, image, why string
	}{
		{"empty", "", "empty"},
		{"truncated record", mut(func(b []byte) []byte { return b[:len(b)-1] }), "malformed"},
		{"truncated header", mut(func(b []byte) []byte { return b[:10] }), "malformed"},
		{"truncated layout", mut(func(b []byte) []byte { return b[:layoutAt+20] }), "layout_malformed"},
		{"no record", mut(func(b []byte) []byte { return b[:recordAt] }), "record_count"},
		{"two records", mut(func(b []byte) []byte { return append(b, b[recordAt:]...) }), "batch_too_large"},
		{"bad file hash", mut(func(b []byte) []byte { b[donewire.TableFixedHashAt] ^= 0xff; return b }), "layout_newer"},
		{"bad layout byte", mut(func(b []byte) []byte { b[layoutAt+8] ^= 0xff; return b }), "layout_malformed"},
		{"bad record hash", mut(func(b []byte) []byte { b[recordAt] ^= 0xff; return b }), "no_layout"},
		{"reserved byte", mut(func(b []byte) []byte { b[3] = 1; return b }), "malformed"},
		{"variable form", mut(func(b []byte) []byte { b[0] = 1; return b }), "previous_form"},
		{"message form", mut(func(b []byte) []byte { b[0] = 2; return b }), "message_form_as_file"},
		{"unknown form", mut(func(b []byte) []byte { b[0] = 9; return b }), "newer_form"},
		{"over-max count", mut(func(b []byte) []byte { put32(b, bodyAt+53, 200); return b }), "clamped"},
		{"text over bound, text slack", mut(func(b []byte) []byte {
			put32(b, bodyAt+8, 99)
			copy(b[bodyAt+12:bodyAt+12+41], strings.Repeat("9", 41))
			return b
		}), "clamped"},
		{"text ill formed", mut(func(b []byte) []byte { b[bodyAt+8+4] = 0xff; return b }), "text_ill_formed"},
		{"legacy json unreadable", `{"epoch":`, "legacy_json_unreadable"},
	} {
		dec, _, why := decodeDonePage(tc.image)
		assert.Equal(t, tc.why, why, tc.name)
		assert.Equal(t, DoneRecord{}, dec, tc.name)
	}

	// Round-trip read through readDoneRecord, WhereFacts, and DoneSince.
	got, err := pinned.readDoneRecord(h.ctx, kv)
	require.NoError(t, err)
	require.Len(t, got.Done, 310, "compacted to one entry per (row, kind, card)")
	for i, c := range got.Done {
		wantCard := fmt.Sprintf("card-fixed-paged-%03d.r1.emma", i)
		wantRow := fmt.Sprintf("member-%02d", i%5)
		wantAt := t0.Add(time.Duration(i+1) * time.Second)
		if i < 15 {
			wantAt = wantAt.Add(500 * time.Second)
		}
		assert.Equal(t, sprint.DoneKindRead, c.Kind)
		assert.Equal(t, wantCard, c.Card)
		assert.Equal(t, wantRow, c.Row)
		assert.True(t, wantAt.Equal(c.At), "entry %d timestamp: want %v, got %v", i, wantAt, c.At)
	}

	facts, err := pinned.WhereFacts(h.ctx, 0)
	require.NoError(t, err)
	require.Len(t, facts.Done.Done, 310, "WhereFacts reads all 310 paged completions")
	doneOfCounts, err := pinned.DoneOf(h.ctx, facts, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, sprint.CountDone(uncompacted, time.Time{}), doneOfCounts)

	// Compaction keeps CountDone equal at every mark (zero, before first, at first, between
	// initial and duplicate completion, at last duplicate, and after all completions).
	marks := []time.Time{
		{},
		t0,
		t0.Add(time.Second),
		t0.Add(10 * time.Second),
		t0.Add(15 * time.Second),
		t0.Add(150 * time.Second),
		t0.Add(310 * time.Second),
		t0.Add(505 * time.Second),
		t0.Add(515 * time.Second),
		t0.Add(600 * time.Second),
	}
	for _, mark := range marks {
		wantCounts := sprint.CountDone(uncompacted, mark)
		assert.Equal(t, wantCounts, sprint.CountDone(got.Done, mark), "CountDone equal at mark %v", mark)
		sinceCounts, err := pinned.DoneSince(h.ctx, mark)
		require.NoError(t, err)
		assert.Equal(t, wantCounts, sinceCounts, "DoneSince equal at mark %v", mark)
	}

	// 3. Legacy JSON (first byte '{') is read once and rewritten into fixed paged form.
	legacyBytes, err := json.Marshal(DoneRecord{
		Epoch:  pinned.epoch,
		Cursor: got.Cursor,
		Done:   uncompacted,
	})
	require.NoError(t, err)
	require.Equal(t, byte('{'), legacyBytes[0])
	require.NoError(t, kv.SetKey(h.ctx, keyDoneRecord, string(legacyBytes)))
	for _, pk := range pageKeys[1:] {
		_, _ = m.DeleteKeys(h.ctx, []string{pinned.Names.Key(pk)})
	}

	fromLegacy, err := pinned.readDoneRecord(h.ctx, kv)
	require.NoError(t, err)
	require.Len(t, fromLegacy.Done, 310, "legacy JSON read compacts 325 entries to 310")
	rewritten, ok, err := kv.GetKey(h.ctx, keyDoneRecord)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, donewire.TableFixedForm, rewritten[0], "legacy JSON is rewritten to fixed form on first read")
	for i, pk := range pageKeys {
		raw, ok, err := kv.GetKey(h.ctx, pk)
		require.NoError(t, err)
		require.True(t, ok, "rewritten page %d (%s) exists", i, pk)
		require.Equal(t, donewire.TableFixedForm, raw[0], "rewritten page %d (%s) is fixed form", i, pk)
	}

	// 4. An older layout meeting a newer record refuses by name (layout_newer) and writes nothing.
	var v2Rec donewirev2.DoneRecord
	donewirev2.DoneRecordReset(&v2Rec)
	v2Rec.Epoch = pinned.epoch
	v2Rec.CursorLength = int32(copy(v2Rec.Cursor[:], "1-0"))
	v2Rec.Pages = 1
	v2Rec.Wrote = t0.UnixNano()
	v2Bytes := make([]byte, donewirev2.DoneRecordFixedMeasure(1))
	require.EqualValues(t, len(v2Bytes), donewirev2.DoneRecordFixedSave([]donewirev2.DoneRecord{v2Rec}, v2Bytes))

	require.NoError(t, kv.SetKey(h.ctx, keyDoneRecord, string(v2Bytes)))
	err = pinned.keepDone(h.ctx, kv, string(v2Bytes), true)
	require.Error(t, err)
	assert.ErrorContains(t, err, "layout_newer")

	afterRefusal, ok, err := kv.GetKey(h.ctx, keyDoneRecord)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, string(v2Bytes), afterRefusal, "older build writes nothing over the newer layout")

	// While the stored record is a newer layout, DoneSince still answers the truth from the log.
	fallbackCounts, err := pinned.DoneSince(h.ctx, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, sprint.CountDone(uncompacted, time.Time{}), fallbackCounts)

	// Restore the 4-page fixed record and verify Teardown deletes every page.
	require.NoError(t, pinned.keepDone(h.ctx, kv, string(legacyBytes), true))
	duringKeys := m.Keys(h.st.Names)
	for _, pk := range pageKeys {
		require.True(t, slices.Contains(duringKeys, h.st.Names.Key(pk)), "key %s present before Teardown", pk)
	}
	_, err = h.st.Teardown(h.ctx)
	require.NoError(t, err)
	assert.Empty(t, m.Keys(h.st.Names), "Teardown deletes every page of the done record")
}
