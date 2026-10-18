package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/donewire"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
)

// The done record (docs/SPEC-SPRINT.md section 1, done and ok%; schemas/store/store.schema):
// every card a row completed, of every kind sprint.DoneKinds registers, read off the log
// lines that complete them (the record a completion leaves: a read card retires off its row
// with its verdict, so the row's cells forget it; the log does not). The tick keeps the
// record (keepDone, with the where record, STOPPED or RUNNING; tla/DoneRecord.tla: Tick,
// Take, StaleWrite): the completions of the lines after its cursor, at most logIndexPages
// pages a tick, compacted to one entry per (row, kind, card) and paged at 95 entries per
// fixed-table page (keyDoneRecord and keyDoneRecord:1 .. keyDoneRecord:<pages-1>). where
// counts from the record (DoneSince, DoneOf; tla/DoneRecord.tla: Shown, Count), at most the
// tick in flight behind, as its other records are. The record is the epoch's: one of another
// epoch is counted again (tla/DoneRecord.tla: Current, Readable, Clear, ClearStartsAgain).
// An older build that meets a newer layout (layout_newer) refuses the tick and writes nothing
// over the newer record (tla/DoneRecord.tla: OlderBuildNeverWritesNewerLayout).

const (
	keyDoneRecord    = "donerecord" // STRING, page 0 of the done record (fixed table, or legacy JSON); "done" is the operations' hash
	doneFormFixed    = "fixed"      // the stored form's name: the fixed table (form byte 3)
	doneFormJSON     = "json"       // the legacy JSON form (first byte '{')
	doneFixedMaxDone = 95           // the done list's declared per-page max (schemas/store/store.schema)
)

// doneFixedPlanRoom is the plan scratch DoneRecordFixedLoad is handed: the identity plan's
// entries and room for any lineage plan (it refuses plan_too_large past it).
var doneFixedPlanRoom = 2 * len(donewire.DoneRecordFixedPlan.Entries)

// DoneRecord is the done record: the epoch, the stream id of the last log line read, and the
// compacted completions of the lines read across all pages.
type DoneRecord struct {
	Epoch  uint64              `json:"epoch"`
	Cursor string              `json:"cursor,omitempty"`
	Done   []sprint.Completion `json:"done,omitempty"`
}

// donePageKey is the KV key name of page p of the done record (keyDoneRecord for page 0,
// "donerecord:<p>" for follow-on pages p >= 1).
func donePageKey(p int) string {
	if p <= 0 {
		return keyDoneRecord
	}
	return fmt.Sprintf("%s:%d", keyDoneRecord, p)
}

// doneRefusedNewer says a refusal names a layout newer than this build's
// (tla/DoneRecord.tla: Readable, OlderBuildNeverWritesNewerLayout).
func doneRefusedNewer(why string) bool {
	return why == "layout_newer" || why == "layout_unsupported"
}

// compactDone compacts cs to one entry per (row, kind, card), keeping the latest completion
// timestamp so sprint.CountDone(compactDone(cs), since) equals sprint.CountDone(cs, since)
// at every stats-reset mark (tla/DoneRecord.tla: Count, NoDoubleCount, CaughtUpIsTruth,
// ResetShowsZero).
func compactDone(cs []sprint.Completion) []sprint.Completion {
	if len(cs) == 0 {
		return nil
	}
	idx := make(map[[3]string]int, len(cs))
	out := make([]sprint.Completion, 0, len(cs))
	for _, c := range cs {
		k := [3]string{c.Row, c.Kind, c.Card}
		if i, ok := idx[k]; ok {
			if c.At.After(out[i].At) {
				out[i].At = c.At
			}
			continue
		}
		idx[k] = len(out)
		out = append(out, c)
	}
	return out
}

// doneTimeOf is a completion's time of its stored Unix nanoseconds; 0 is the zero time.
func doneTimeOf(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

// doneText puts s into a fixed text field of capacity len(dst), or says why it does not fit:
// past the bound, or not the schema's string text (UTF-8, no NUL).
func doneText(dst []byte, s string) (int32, string) {
	if len(s) > len(dst) {
		return 0, "text_over_bound"
	}
	if !utf8.ValidString(s) || strings.IndexByte(s, 0) >= 0 {
		return 0, "text_ill_formed"
	}
	return int32(copy(dst, s)), ""
}

// donePage is one page decoded by the done row reader (S1, S2).
type donePage struct {
	rec   DoneRecord
	pages uint32
}

// doneWireForms lets the generated fixed loader preserve refusal reasons for every form byte
// instead of having the row reader reject one first (S2).
func doneWireForms() []byte {
	forms := make([]byte, 256)
	for i := range forms {
		forms[i] = byte(i)
	}
	return forms
}

// doneRowFns adapts the fixed table and today's legacy JSON reader to the store row codec (S1, S2).
func doneRowFns() RowFns[donePage] {
	return RowFns[donePage]{
		Name: "DONERECORD", Forms: doneWireForms(),
		Reset:    func(v *donePage) { *v = donePage{} },
		LoadWire: doneLoadWire, LoadJSON: doneLoadJSON,
		Legacy:     func(in []byte) bool { return len(in) > 0 && in[0] == '{' },
		LoadLegacy: doneLoadLegacy,
	}
}

// doneRows makes and registers the one done reader on this Store. S4: its counts are the evidence
// exported by Check and shared with pinned copies.
func (st *Store) doneRows() *RowCodec[donePage] {
	if st == nil {
		return nil
	}
	lazyMu.Lock()
	defer lazyMu.Unlock()
	c := st.doneRowsLocked()
	c.Register(st)
	return c
}

// doneRowsLocked is doneRows while lazyMu is held (S4).
func (st *Store) doneRowsLocked() *RowCodec[donePage] {
	if st.doneRowCodec == nil {
		st.doneRowCodec = NewRowCodec(doneRowFns())
	}
	return st.doneRowCodec
}

// donePageFromWire maps the generated fixed record into the page read by the store (S1).
func donePageFromWire(w donewire.DoneRecord) donePage {
	r := DoneRecord{Epoch: w.Epoch, Cursor: string(w.Cursor[:w.CursorLength])}
	if w.DoneCount > 0 {
		r.Done = make([]sprint.Completion, w.DoneCount)
	}
	for i := range r.Done {
		e := &w.Done[i]
		r.Done[i] = sprint.Completion{
			Kind: string(e.Kind[:e.KindLength]), Card: string(e.Card[:e.CardLength]),
			Row: string(e.Row[:e.RowLength]), At: doneTimeOf(e.At),
		}
	}
	pages := w.Pages
	if pages == 0 {
		pages = 1
	}
	return donePage{rec: r, pages: pages}
}

// doneWireReport preserves the fixed decoder's refusal precedence and reasons (S2).
func doneWireReport(rep donewire.TableReport, n int64) RowReport {
	switch {
	case n < 0 && rep.Reason != "":
		return RowReport{Reason: rep.Reason}
	case n < 0 || rep.Malformed:
		return RowReport{Malformed: true, Reason: "malformed"}
	case n != 1:
		return RowReport{Malformed: true, Reason: "record_count"}
	case rep.Clamped != 0:
		return RowReport{Clamped: int(rep.Clamped), Reason: "clamped"}
	case rep.Unknown != 0 || rep.KindMismatch != 0:
		return RowReport{Unknown: int(rep.Unknown), KindMismatch: int(rep.KindMismatch), Reason: "unknown"}
	default:
		return RowReport{}
	}
}

// doneLoadWire adapts the generated fixed table while preserving the prior decoder's refusal reasons (S1, S2).
func doneLoadWire(v *donePage, wire []byte) RowReport {
	out := make([]donewire.DoneRecord, 1)
	plan := make([]donewire.TableFixedEntry, doneFixedPlanRoom)
	var rep donewire.TableReport
	n := donewire.DoneRecordFixedLoad(out, wire, plan, &rep)
	rr := doneWireReport(rep, n)
	if rr.Reason == "" {
		*v = donePageFromWire(out[0])
	}
	return rr
}

// doneLoadJSON adapts the generated schema JSON form to the same page value (S2).
func doneLoadJSON(v *donePage, text []byte) RowReport {
	var w donewire.DoneRecord
	var rep donewire.TableReport
	if !donewire.DoneRecordFromJson(&w, text, &rep) {
		rr := RowReport{Unknown: int(rep.Unknown), KindMismatch: int(rep.KindMismatch), Clamped: int(rep.Clamped),
			Malformed: rep.Malformed, Duplicate: int(rep.Duplicate), Reason: rep.Reason}
		if rr.Reason == "" && !rr.Malformed && rr.Duplicate == 0 && rr.Clamped == 0 && rr.Unknown == 0 && rr.KindMismatch == 0 {
			rr.Malformed = true
		}
		return rr
	}
	*v = donePageFromWire(w)
	return RowReport{}
}

// doneLoadLegacy keeps today's JSON page decoder and compaction behavior (S2, S4).
func doneLoadLegacy(v *donePage, raw []byte) RowReport {
	if err := json.Unmarshal(raw, &v.rec); err != nil {
		return RowReport{Malformed: true, Reason: "legacy_json_unreadable"}
	}
	v.rec.Done = compactDone(v.rec.Done)
	v.pages = 1
	return RowReport{}
}

// decodeDonePage reads one stored page through the registered row codec (S1, S2, S4), returning
// its record, the declared page count, or the original refusal reason. Empty remains the early return.
func (st *Store) decodeDonePage(raw string) (DoneRecord, uint32, string) {
	if raw == "" {
		return DoneRecord{}, 0, "empty"
	}
	page, _, err := st.doneRows().Read([]byte(raw))
	if err != nil {
		var refusal *schemaread.Refusal
		if errors.As(err, &refusal) {
			return DoneRecord{}, 0, refusal.Reason
		}
		return DoneRecord{}, 0, "malformed"
	}
	return page.rec, page.pages, ""
}

// donePages is the fixed-table pages of the done record as decoded, by their stored text,
// kept in process (donePageOf). The fault of 2026-10-17: every tick, RUNNING or STOPPED,
// decoded the whole record (every page through the row codec) to find, most ticks, no new
// line after its cursor. A page's decode depends on its text alone, so a page kept is the
// page a decode gives. At most doneMemoMax pages are kept; past that the memo starts empty.
var donePages struct {
	mu    sync.Mutex
	pages map[string]donePageMemo
}

// donePageMemo is one page decoded: its record and declared page count.
type donePageMemo struct {
	rec   DoneRecord
	pages uint32
}

// doneMemoMax bounds donePages: a done record of 95 entries a page and some.
const doneMemoMax = 512

// donePageOf is decodeDonePage, from donePages when the page was decoded before. Only a
// fixed-table page that reads is kept: a legacy page is rewritten at its first read, and a
// refusal is decoded (and counted) each time. The record given is a copy.
func (st *Store) donePageOf(raw string) (DoneRecord, uint32, string) {
	if raw == "" || raw[0] == '{' {
		return st.decodeDonePage(raw)
	}
	donePages.mu.Lock()
	m, ok := donePages.pages[raw]
	donePages.mu.Unlock()
	if !ok {
		r, pages, why := st.decodeDonePage(raw)
		if why != "" {
			return r, pages, why
		}
		m = donePageMemo{rec: r, pages: pages}
		donePages.mu.Lock()
		if donePages.pages == nil || len(donePages.pages) >= doneMemoMax {
			donePages.pages = map[string]donePageMemo{}
		}
		donePages.pages[raw] = m
		donePages.mu.Unlock()
	}
	r := m.rec
	r.Done = slices.Clone(r.Done)
	return r, m.pages, ""
}

// doneFixedPageOf fills w as one fixed-table page of (epoch, cursor, pages, chunk), or returns
// the reason the fixed form cannot hold it.
func doneFixedPageOf(epoch uint64, cursor string, pages uint32, chunk []sprint.Completion, w *donewire.DoneRecord) string {
	donewire.DoneRecordReset(w)
	if len(chunk) > len(w.Done) {
		return "over_max_count"
	}
	w.Epoch = epoch
	w.Pages = pages
	var why string
	if w.CursorLength, why = doneText(w.Cursor[:], cursor); why != "" {
		return why
	}
	w.DoneCount = int32(len(chunk))
	for i, c := range chunk {
		e := &w.Done[i]
		for _, f := range []struct {
			n   *int32
			dst []byte
			s   string
		}{
			{&e.KindLength, e.Kind[:], c.Kind},
			{&e.CardLength, e.Card[:], c.Card},
			{&e.RowLength, e.Row[:], c.Row},
		} {
			if *f.n, why = doneText(f.dst, f.s); why != "" {
				return why
			}
		}
		if !c.At.IsZero() {
			if n := c.At.UnixNano(); n == 0 || !time.Unix(0, n).Equal(c.At) {
				return "time_out_of_range"
			} else {
				e.At = n
			}
		}
	}
	return ""
}

// encodeDonePages compacts r.Done and encodes r into fixed-table pages of at most
// doneFixedMaxDone (95) completions each.
func encodeDonePages(r DoneRecord) ([]string, string, error) {
	done := compactDone(r.Done)
	pages := max(1, (len(done)+doneFixedMaxDone-1)/doneFixedMaxDone)
	out := make([]string, pages)
	size := donewire.DoneRecordFixedMeasure(1)
	for p := range pages {
		lo := min(p*doneFixedMaxDone, len(done))
		hi := min((p+1)*doneFixedMaxDone, len(done))
		var w donewire.DoneRecord
		if why := doneFixedPageOf(r.Epoch, r.Cursor, uint32(pages), done[lo:hi], &w); why != "" {
			return nil, why, fmt.Errorf("done record page %d refused: %s", p, why)
		}
		b := make([]byte, size)
		if donewire.DoneRecordFixedSave([]donewire.DoneRecord{w}, b) != size {
			return nil, "save_refused", fmt.Errorf("done record page %d: fixed save refused", p)
		}
		out[p] = string(b)
	}
	return out, doneFormFixed, nil
}

// writeDonePages writes all fixed-table pages of r to kv and removes any surplus pages left by
// a wider previous record.
func (st *Store) writeDonePages(ctx context.Context, kv KV, r DoneRecord, prevPages uint32) error {
	pages, _, err := encodeDonePages(r)
	if err != nil {
		return err
	}
	for p, raw := range pages {
		if err := kv.SetKey(ctx, donePageKey(p), raw); err != nil {
			return err
		}
	}
	if int(prevPages) > len(pages) && st.B != nil {
		var extra []string
		for p := len(pages); p < int(prevPages); p++ {
			extra = append(extra, st.Names.Key(donePageKey(p)))
		}
		if _, err := st.B.DeleteKeys(ctx, extra); err != nil {
			return err
		}
	}
	return nil
}

// readDoneFrom reads the epoch's done record from page 0 (raw, ok) and any follow-on pages in
// kv (tla/DoneRecord.tla: Current, Readable). Legacy JSON (first byte '{') is read once and
// rewritten into the fixed paged form.
func (st *Store) readDoneFrom(ctx context.Context, kv KV, raw string, ok bool) (DoneRecord, uint32, string, error) {
	if !ok {
		return DoneRecord{Epoch: st.epoch}, 0, "", nil
	}
	r, pages, why := st.donePageOf(raw)
	if why != "" {
		return DoneRecord{Epoch: st.epoch}, 0, why, nil
	}
	if r.Epoch != st.epoch {
		return DoneRecord{Epoch: st.epoch}, pages, "epoch", nil
	}
	if raw[0] == '{' {
		if kv != nil {
			if err := st.writeDonePages(ctx, kv, r, 1); err != nil {
				return DoneRecord{Epoch: st.epoch}, 1, "", err
			}
		}
		return r, uint32(max(1, (len(r.Done)+doneFixedMaxDone-1)/doneFixedMaxDone)), "", nil
	}
	if pages > 1 && kv != nil {
		names := make([]string, pages-1)
		for p := uint32(1); p < pages; p++ {
			names[p-1] = donePageKey(int(p))
		}
		vals, oks, err := getKeys(ctx, kv, names)
		if err != nil {
			return DoneRecord{Epoch: st.epoch}, pages, "", err
		}
		for i, pok := range oks {
			if !pok {
				return DoneRecord{Epoch: st.epoch}, pages, "missing_page", nil
			}
			pr, _, pwhy := st.donePageOf(vals[i])
			if pwhy != "" {
				return DoneRecord{Epoch: st.epoch}, pages, pwhy, nil
			}
			if pr.Epoch != st.epoch {
				return DoneRecord{Epoch: st.epoch}, pages, "epoch", nil
			}
			r.Done = append(r.Done, pr.Done...)
		}
	}
	r.Done = compactDone(r.Done)
	return r, pages, "", nil
}

// readDoneRecord is the epoch's done record as stored across all pages; empty when there is
// none, it does not read, or it is another epoch's (tla/DoneRecord.tla: Current, Readable).
func (st *Store) readDoneRecord(ctx context.Context, kv KV) (DoneRecord, error) {
	raw, ok, err := kv.GetKey(ctx, keyDoneRecord)
	if err != nil {
		return DoneRecord{Epoch: st.epoch}, err
	}
	r, _, _, err := st.readDoneFrom(ctx, kv, raw, ok)
	return r, err
}

// donePageKeys returns the follow-on page keys ("donerecord:1" .. "donerecord:<pages-1>")
// named by page 0 of the done record for Teardown.
func (st *Store) donePageKeys(ctx context.Context) []string {
	kv, err := st.kv()
	if err != nil {
		return nil
	}
	raw, ok, err := kv.GetKey(ctx, keyDoneRecord)
	if err != nil || !ok {
		return nil
	}
	_, pages, why := st.decodeDonePage(raw)
	if why != "" || pages <= 1 {
		return nil
	}
	out := make([]string, pages-1)
	for p := uint32(1); p < pages; p++ {
		out[p-1] = donePageKey(int(p))
	}
	return out
}

// doneAfter adds to r the completions of the log's lines after its cursor, at most pages
// pages of logPage lines (every page when pages is 0), compacts r.Done to one entry per
// (row, kind, card), and moves its cursor; more says lines were left
// (tla/DoneRecord.tla: Take, Complete).
func (st *Store) doneAfter(ctx context.Context, r *DoneRecord, pages int) (more bool, err error) {
	return st.doneAfterRead(ctx, r, pages, nil)
}

// doneAfterRead is doneAfter with the log pages the tick's card log index read (logPagesRead,
// keepLogIndex) taken from read instead of the store, each by the cursor it was read after.
func (st *Store) doneAfterRead(ctx context.Context, r *DoneRecord, pages int, read map[string]logPageRead) (more bool, err error) {
	for n := 0; pages == 0 || n < pages; n++ {
		lines, ids, err := st.logPageAfter(ctx, r.Cursor, read)
		if err != nil {
			return false, err
		}
		for _, l := range lines {
			if l.Epoch != 0 && l.Epoch != st.epoch {
				continue
			}
			r.Done = append(r.Done, sprint.CompletionsOf(l)...)
		}
		r.Done = compactDone(r.Done)
		if len(ids) > 0 {
			r.Cursor = ids[len(ids)-1]
		}
		if len(ids) < logPage {
			return false, nil
		}
	}
	return true, nil
}

// keepDone is the tick's done record, as stored (raw, ok: read with the where record, one
// exchange): the lines after its cursor read, at most logIndexPages pages, compacted and
// written across fixed-table pages when it moved or was legacy JSON. An older build meeting a
// newer layout (layout_newer) refuses by name and writes nothing over it
// (tla/DoneRecord.tla: Tick, StaleWrite, OlderBuildNeverWritesNewerLayout).
func (st *Store) keepDone(ctx context.Context, kv KV, raw string, ok bool) error {
	read := st.takeLogPages()
	// page 0 alone first: its cursor says whether the log has lines past it, and with none
	// (most ticks) the record is not read further, as the tick writes nothing then
	// (tla/DoneRecord.tla: Tick is enabled only while Current.cur < Len(log)). The fault of
	// 2026-10-17: every tick decoded every page to find no new line.
	if ok && raw != "" && raw[0] != '{' {
		if head, _, why := st.donePageOf(raw); why == "" && head.Epoch == st.epoch {
			_, ids, err := st.logPageAfter(ctx, head.Cursor, read)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				return nil
			}
		}
	}
	r, prevPages, why, err := st.readDoneFrom(ctx, kv, raw, ok)
	if err != nil {
		return err
	}
	if doneRefusedNewer(why) {
		return fmt.Errorf("%s: the stored done record is a newer build's layout; this build reads the log whole and writes nothing over it", why)
	}
	was := r.Cursor
	if _, err := st.doneAfterRead(ctx, &r, logIndexPages, read); err != nil {
		return err
	}
	if r.Cursor == was {
		return nil
	}
	return st.writeDonePages(ctx, kv, r, prevPages)
}

// DoneSince is each fleet row's done by kind: the completions after since (a stats reset's
// mark; every completion of the epoch when since is zero), from the done record the tick keeps,
// as where takes its other records: at most the tick in flight behind, and the log never read
// by where once a tick has kept it (where_landed_series_test: where reads no log line). With no
// readable record of the epoch yet (before the first tick of a build that keeps it, after a
// clear, or while an older build meets layout_newer), the epoch's log is read whole, as where
// reads the cards before the where record (tla/DoneRecord.tla: Shown, Count, NoDoubleCount,
// CaughtUpIsTruth, ResetShowsZero, ClearStartsAgain).
func (st *Store) DoneSince(ctx context.Context, since time.Time) (sprint.DoneCounts, error) {
	st, err := st.pin(ctx)
	if err != nil {
		return nil, err
	}
	r := DoneRecord{Epoch: st.epoch}
	if kv, err := st.kv(); err == nil {
		if r, err = st.readDoneRecord(ctx, kv); err != nil {
			return nil, err
		}
	}
	if r.Cursor == "" {
		if _, err := st.doneAfter(ctx, &r, 0); err != nil {
			return nil, err
		}
	}
	return sprint.CountDone(r.Done, since), nil
}

// DoneOf is each fleet row's done by kind after since, from the done record where's facts read
// with the where record (WhereFacts, one exchange), or, with none of the epoch yet, DoneSince
// (tla/DoneRecord.tla: Shown).
func (st *Store) DoneOf(ctx context.Context, f WhereFacts, since time.Time) (sprint.DoneCounts, error) {
	if f.Done.Cursor != "" {
		return sprint.CountDone(f.Done.Done, since), nil
	}
	return st.DoneSince(ctx, since)
}

// logPageRead is one page of the log as LogSince gave it.
type logPageRead struct {
	lines []sprint.Line
	ids   []string
}

// logPagesShared is the log pages the tick's card log index last read (keepLogIndex), by the
// cursor each was read after, for the done record that reads the same lines next in the same
// tick (keepWhere: keepLogIndex, then keepDone). The fault of 2026-10-17: the two read and
// decoded the same new lines twice, and an idle tick asked the log twice for nothing. A page
// is the store's answer at the moment the index read it: the done record that takes it reads
// the log as it was a moment earlier, as a tick that ran a moment earlier would
// (tla/DoneRecord.tla: Take; a line written since is taken by the next tick). It is taken
// once, by the store (its backend and epoch) that put it.
var logPagesShared struct {
	mu    sync.Mutex
	b     any
	epoch uint64
	pages map[string]logPageRead
}

// shareLogPages puts the pages the card log index read for keepDone to take.
func (st *Store) shareLogPages(pages map[string]logPageRead) {
	logPagesShared.mu.Lock()
	defer logPagesShared.mu.Unlock()
	logPagesShared.b, logPagesShared.epoch, logPagesShared.pages = st.B, st.epoch, pages
}

// takeLogPages is the pages shareLogPages put, when this store put them (nil otherwise, and
// another store's are left as they are); pages taken are left for no other take.
func (st *Store) takeLogPages() map[string]logPageRead {
	logPagesShared.mu.Lock()
	defer logPagesShared.mu.Unlock()
	if logPagesShared.pages == nil || logPagesShared.epoch != st.epoch || !sameBackend(logPagesShared.b, st.B) {
		return nil
	}
	pages := logPagesShared.pages
	logPagesShared.b, logPagesShared.pages = nil, nil
	return pages
}

// sameBackend says a and b are the same backend value; false for a type that does not compare.
func sameBackend(a, b any) bool {
	if a == nil || b == nil || reflect.TypeOf(a) != reflect.TypeOf(b) || !reflect.TypeOf(a).Comparable() {
		return false
	}
	return a == b
}

// logPageAfter is LogSince of one page after the cursor: from read when it holds that page,
// else from the store.
func (st *Store) logPageAfter(ctx context.Context, after string, read map[string]logPageRead) ([]sprint.Line, []string, error) {
	if p, ok := read[after]; ok {
		return p.lines, p.ids, nil
	}
	return st.B.LogSince(ctx, after, logPage)
}
