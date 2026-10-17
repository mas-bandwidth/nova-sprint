package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// doneRowFns adapts the fixed table to the store row codec (S1, S2). The legacy
// JSON form stays recognised and counted, and is refused by name (S4).
func doneRowFns() RowFns[donePage] {
	return RowFns[donePage]{
		Name: "DONERECORD", Forms: doneWireForms(),
		Reset:    func(v *donePage) { *v = donePage{} },
		LoadWire: doneLoadWire, LoadJSON: doneLoadJSON,
		Legacy: func(in []byte) bool { return len(in) > 0 && in[0] == '{' },
		LoadLegacy: func(*donePage, []byte) RowReport {
			return RowReport{Malformed: true, Reason: "legacy_json_retired"}
		},
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

// doneWireReport preserves the fixed decoder's refusal precedence and reasons (S2). The
// generated decoder names a malformed header, a reserved byte and a bound overrun more
// finely than the store's vocabulary, so they map to the store's reasons; a layout the
// lineage does not hold maps to layout_newer, the name an older build refuses it by.
func doneWireReport(rep donewire.TableReport, n int64) RowReport {
	switch rep.Reason {
	case "truncated", "reserved_not_zero":
		return RowReport{Malformed: true, Reason: "malformed"}
	case "count_over_bound", "length_over_bound":
		return RowReport{Clamped: 1, Reason: "clamped"}
	case "layout_unknown":
		return RowReport{Reason: "layout_newer"}
	}
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
// kv (tla/DoneRecord.tla: Current, Readable). A legacy JSON page is refused by name.
func (st *Store) readDoneFrom(ctx context.Context, kv KV, raw string, ok bool) (DoneRecord, uint32, string, error) {
	if !ok {
		return DoneRecord{Epoch: st.epoch}, 0, "", nil
	}
	r, pages, why := st.decodeDonePage(raw)
	if why != "" {
		return DoneRecord{Epoch: st.epoch}, 0, why, nil
	}
	if r.Epoch != st.epoch {
		return DoneRecord{Epoch: st.epoch}, pages, "epoch", nil
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
			pr, _, pwhy := st.decodeDonePage(vals[i])
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
	for n := 0; pages == 0 || n < pages; n++ {
		lines, ids, err := st.B.LogSince(ctx, r.Cursor, logPage)
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
	r, prevPages, why, err := st.readDoneFrom(ctx, kv, raw, ok)
	if err != nil {
		return err
	}
	if doneRefusedNewer(why) {
		return fmt.Errorf("%s: the stored done record is a newer build's layout; this build reads the log whole and writes nothing over it", why)
	}
	was := r.Cursor
	if _, err := st.doneAfter(ctx, &r, logIndexPages); err != nil {
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
