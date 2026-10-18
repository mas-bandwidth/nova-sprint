package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// logCountMem is the in-memory store with its log reads and multi-key reads counted.
type logCountMem struct {
	*Mem
	since, gets int
}

func (c *logCountMem) LogSince(ctx context.Context, after string, max int) ([]sprint.Line, []string, error) {
	c.since++
	return c.Mem.LogSince(ctx, after, max)
}

func (c *logCountMem) GetKeys(ctx context.Context, names []string) ([]string, []bool, error) {
	c.gets++
	return c.Mem.GetKeys(ctx, names)
}

// appendReadLine appends a log line that completes a read card on row.
func appendReadLine(m *Mem, epoch uint64, card, row string, at time.Time) {
	l := sprint.Line{At: at, Epoch: epoch, Table: sprint.Fleet, Removed: true, From: row + ":reading", Card: card, Set: map[string]string{"verdict": "ok"}}
	m.mu.Lock()
	m.seq++
	lg := m.log()
	lg.lines = append(lg.lines, memLine{fmt.Sprintf("%d-0", m.seq), l})
	m.mu.Unlock()
}

// The faults of 2026-10-17: every tick decoded the done record whole (every page through the
// row codec) to find no new line, and asked the log again for the lines the card log index
// had just read. An idle tick now reads the log once (the index's read) and the record's
// pages once (one multi-key read, so a torn record is seen) and decodes nothing, while every
// page read still counts as a read of its form; a tick with a new line decodes no page it
// decoded before and reads the log once; and the record kept is the one the log gives.
//
// Not parallel: the pages the index shares are one slot in the process (logPagesShared), and
// the counts here are exact.
func TestKeepDoneReadsTheLogOnceAndDecodesNothingIdle(t *testing.T) {
	t0 := time.Unix(1000, 0).UTC()
	h := newHarness(t)
	m := NewMem()
	h.m, h.st.B = m, m
	require.NoError(t, h.st.Init(h.ctx))
	pinned, err := h.st.pin(h.ctx)
	require.NoError(t, err)
	pm := pinned.B.(*Mem)
	for i := range 200 { // three pages of the record (95 + 95 + 10)
		appendReadLine(pm, pinned.epoch, fmt.Sprintf("card-once-%03d.r1.emma", i), fmt.Sprintf("member-%d", i%4), t0.Add(time.Duration(i+1)*time.Second))
	}
	c := &logCountMem{Mem: pm}
	pinned.B = c
	kv, err := pinned.kv()
	require.NoError(t, err)
	// keepWhere's order: the card log index, then the done record read with the where record
	tick := func() {
		t.Helper()
		require.NoError(t, pinned.keepLogIndex(h.ctx))
		raw, ok, err := kv.GetKey(h.ctx, keyDoneRecord)
		require.NoError(t, err)
		require.NoError(t, pinned.keepDone(h.ctx, kv, raw, ok))
	}
	for range 4 {
		tick()
	}
	_, tail, err := pm.LogSince(h.ctx, "", 1000)
	require.NoError(t, err)
	r, err := pinned.readDoneRecord(h.ctx, kv)
	require.NoError(t, err)
	require.Equal(t, tail[len(tail)-1], r.Cursor, "the record caught up")
	require.Len(t, r.Done, 200)

	// the codec's own counter counts decodes; the reads Check reports count every page read
	decodes := func() uint64 { return pinned.doneRows().Counts().Snapshot()["binary"] }
	reads := func() uint64 { return pinned.doneReadCounts()["binary"] }
	for i := range 3 {
		c.since, c.gets = 0, 0
		before, readBefore := decodes(), reads()
		tick()
		require.Equal(t, 1, c.since, "idle tick %d: %d log reads, want 1 (the index's; the fault: the done record read the log again)", i, c.since)
		require.Equal(t, 1, c.gets, "idle tick %d: %d reads of the follow-on pages, want 1", i, c.gets)
		require.Equal(t, before, decodes(), "idle tick %d decoded %d pages, want 0 (the fault: every page every tick)", i, decodes()-before)
		require.Equal(t, readBefore+3, reads(), "idle tick %d: %d page reads counted, want 3 (a memo's page is a read)", i, reads()-readBefore)
	}

	// a new line: one log read, the pages decoded before not decoded again, the record moved
	appendReadLine(pm, pinned.epoch, "card-once-new.r1.emma", "member-9", t0.Add(time.Hour))
	c.since, c.gets = 0, 0
	before := decodes()
	tick()
	require.Equal(t, 1, c.since, "a tick with a new line: %d log reads, want 1", c.since)
	require.Equal(t, 1, c.gets, "a tick with a new line reads the follow-on pages once")
	require.Equal(t, before, decodes(), "a tick with a new line decoded %d pages it had decoded before", decodes()-before)
	got, err := pinned.readDoneRecord(h.ctx, kv)
	require.NoError(t, err)
	require.Len(t, got.Done, 201)
	require.Equal(t, "card-once-new.r1.emma", got.Done[200].Card)

	// the record kept is the one a fresh read of the store gives (no memo: another process)
	fresh := DoneRecord{Epoch: pinned.epoch}
	require.NoError(t, func() error { _, err := pinned.doneAfter(h.ctx, &fresh, 0); return err }())
	require.Equal(t, fresh.Cursor, got.Cursor)
	require.Equal(t, sprint.CountDone(fresh.Done, time.Time{}), sprint.CountDone(got.Done, time.Time{}))
}

// A page from the memo is the caller's own: appending to it or changing an entry reaches
// no later read.
func TestDonePageOfGivesCopies(t *testing.T) {
	t.Parallel()
	st := &Store{}
	rec := DoneRecord{Epoch: 3, Cursor: "9-0", Done: []sprint.Completion{{Row: "r", Kind: sprint.DoneKindRead, Card: "copies.r1.emma", At: time.Unix(1000, 0).UTC()}}}
	pages, _, err := encodeDonePages(rec)
	require.NoError(t, err)
	a, _, why := st.donePageOf(pages[0])
	require.Empty(t, why)
	a.Done[0].Card = "spoiled"
	_ = append(a.Done[:1], sprint.Completion{Card: "appended"})
	b, n, why := st.donePageOf(pages[0])
	require.Empty(t, why)
	require.EqualValues(t, 1, n)
	require.Equal(t, rec.Done, b.Done)
}

// The pages the index shares are taken only by the store that put them, once; another
// store's take leaves them.
func TestLogPagesTakenOnceByTheirStore(t *testing.T) {
	a, b := &Store{B: NewMem(), epoch: 1}, &Store{B: NewMem(), epoch: 1}
	pages := map[string]logPageRead{"": {ids: []string{"1-0"}}}
	a.shareLogPages(pages)
	require.Nil(t, b.takeLogPages(), "another backend took the pages")
	other := &Store{B: a.B, epoch: 2}
	require.Nil(t, other.takeLogPages(), "another epoch took the pages")
	require.Equal(t, pages, a.takeLogPages())
	require.Nil(t, a.takeLogPages(), "the pages were taken twice")
	require.False(t, sameBackend(map[string]int{}, map[string]int{}), "a type that does not compare")
}

// A record torn by a write cut between its pages: it grew from two pages to three, page 0
// landed with the new cursor and three pages, and the process stopped before the follow-on
// pages. On a quiet log the next tick rebuilds it from the log (cold read of PR #486, 2026-10-17:
// the page-0 early exit left it refusing missing_page until a new line, for ever on a stopped
// sprint; tla/DoneRecord.tla TornWrite, TornRepaired).
func TestKeepDoneRepairsATornRecordOnAQuietLog(t *testing.T) {
	t0 := time.Unix(1000, 0).UTC()
	h := newHarness(t)
	m := NewMem()
	h.m, h.st.B = m, m
	require.NoError(t, h.st.Init(h.ctx))
	pinned, err := h.st.pin(h.ctx)
	require.NoError(t, err)
	pm := pinned.B.(*Mem)
	kv, err := pinned.kv()
	require.NoError(t, err)
	tick := func() {
		t.Helper()
		require.NoError(t, pinned.keepLogIndex(h.ctx))
		raw, ok, err := kv.GetKey(h.ctx, keyDoneRecord)
		require.NoError(t, err)
		require.NoError(t, pinned.keepDone(h.ctx, kv, raw, ok))
	}
	for i := range 150 { // two pages
		appendReadLine(pm, pinned.epoch, fmt.Sprintf("card-torn-%03d.r1.emma", i), "member-1", t0.Add(time.Duration(i+1)*time.Second))
	}
	for range 4 {
		tick()
	}
	_, _, why, err := pinned.readDoneFrom(h.ctx, kv, func() string { r, _, _ := kv.GetKey(h.ctx, keyDoneRecord); return r }(), true)
	require.NoError(t, err)
	require.Empty(t, why)
	// sixty more lines: the record the next write would make is three pages
	for i := range 60 {
		appendReadLine(pm, pinned.epoch, fmt.Sprintf("card-torn-more-%03d.r1.emma", i), "member-2", t0.Add(time.Hour+time.Duration(i)*time.Second))
	}
	_, ids, err := pm.LogSince(h.ctx, "", 1000)
	require.NoError(t, err)
	whole := DoneRecord{Epoch: pinned.epoch}
	_, err = pinned.doneAfter(h.ctx, &whole, 0)
	require.NoError(t, err)
	require.Equal(t, ids[len(ids)-1], whole.Cursor)
	pages, _, err := encodeDonePages(whole)
	require.NoError(t, err)
	require.Len(t, pages, 3)
	// the cut write: page 0 only (writeDonePages writes page 0 first)
	require.NoError(t, kv.SetKey(h.ctx, keyDoneRecord, pages[0]))
	raw, _, err := kv.GetKey(h.ctx, keyDoneRecord)
	require.NoError(t, err)
	_, _, why, err = pinned.readDoneFrom(h.ctx, kv, raw, true)
	require.NoError(t, err)
	require.Equal(t, "missing_page", why, "the torn record")
	// a quiet log: the cursor on page 0 is the log's tail, and the next tick repairs it
	tick()
	got, err := pinned.readDoneRecord(h.ctx, kv)
	require.NoError(t, err)
	require.Equal(t, whole.Cursor, got.Cursor, "the torn record was not rebuilt on a quiet log")
	require.Len(t, got.Done, 210)
	require.Equal(t, sprint.CountDone(whole.Done, time.Time{}), sprint.CountDone(got.Done, time.Time{}))
}
