package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/donewire"
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

// doneCodec is the done record's row reader (a8-store-row-codec), built once for per-form
// counting. Every stored page read passes through it so the read is counted once under its
// form (S2, S4). Registration on the store's CheckReport needs a store-construction site
// outside this file, which the retire card provides as a proposed diff: until one registers
// it, row_forms shows no DONERECORD counts.
var (
	doneCodecOnce sync.Once
	doneCodec     *RowCodec[donewire.DoneRecord]
)

func getDoneCodec() *RowCodec[donewire.DoneRecord] {
	doneCodecOnce.Do(func() {
		doneCodec = NewRowCodec(RowFns[donewire.DoneRecord]{
			Name:  "DONERECORD",
			Reset: func(v *donewire.DoneRecord) { donewire.DoneRecordReset(v) },
			Measure: func(wire []byte) error {
				if int64(len(wire)) < donewire.DoneRecordFixedMeasure(1) {
					return errors.New("wire_damaged")
				}
				return nil
			},
			LoadWire:   doneLoadWire,
			LoadJSON:   doneLoadJSON,
			Legacy:     LegacyObject,
			LoadLegacy: doneLoadLegacy,
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
		})
	})
	return doneCodec
}

// doneReport maps one generated fixed-table report and load count to the strict read's
// RowReport (the same mapping the fixed load returns today).
func doneReport(rep donewire.TableReport, n int64) RowReport {
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

func doneLoadWire(v *donewire.DoneRecord, wire []byte) RowReport {
	out := make([]donewire.DoneRecord, 1)
	plan := make([]donewire.TableFixedEntry, 2*len(donewire.DoneRecordFixedPlan.Entries))
	var rep donewire.TableReport
	n := donewire.DoneRecordFixedLoad(out, wire, plan, &rep)
	if n == 1 {
		*v = out[0]
	}
	return doneReport(rep, n)
}

func doneLoadJSON(v *donewire.DoneRecord, text []byte) RowReport {
	var rep donewire.TableReport
	ok := donewire.DoneRecordFromJson(v, text, &rep)
	rr := doneReport(rep, 1)
	if !ok && rr.Reason == "" && !rr.Malformed {
		rr.Malformed = true
	}
	return rr
}

func doneLoadLegacy(v *donewire.DoneRecord, in []byte) RowReport {
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

// decodeDonePage decodes one stored page of the done record (fixed form, or legacy JSON when
// raw[0] == '{'), returning the page's record, the total page count declared on the page, or
// the refusal reason that read nothing.
//
// The legacy JSON branch and the read-time rewrite below are the piece of Aspect 8 the card
// a8-store-done-retire-legacy-json retires. They are not yet removed: retirement is gated on
// the per-form counter (rowcodec's row_forms, nova-sprint check --json) showing zero legacy
// reads of the done record over one adopt cycle, which is deployment evidence no test can
// produce, so this card stops not-done with the branch in place. The next attempt wires the
// done read through RowCodec and registers it (a store-construction file outside this PATHS
// list), then, once a deployed cycle shows zero legacy reads, deletes this branch and the
// rewrite and names a legacy page a refusal.
func decodeDonePage(raw string) (DoneRecord, uint32, string) {
	if raw == "" {
		return DoneRecord{}, 0, "empty"
	}
	getDoneCodec().Read([]byte(raw))
	if raw[0] == '{' {
		var r DoneRecord
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return DoneRecord{}, 0, "legacy_json_unreadable"
		}
		r.Done = compactDone(r.Done)
		return r, 1, ""
	}
	out := make([]donewire.DoneRecord, 1)
	plan := make([]donewire.TableFixedEntry, doneFixedPlanRoom)
	var rep donewire.TableReport
	n := donewire.DoneRecordFixedLoad(out, []byte(raw), plan, &rep)
	switch {
	case n < 0 && rep.Reason != "":
		return DoneRecord{}, 0, rep.Reason
	case n < 0 || rep.Malformed:
		return DoneRecord{}, 0, "malformed"
	case n != 1:
		return DoneRecord{}, 0, "record_count"
	case rep.Clamped != 0:
		return DoneRecord{}, 0, "clamped"
	case rep.Unknown != 0 || rep.KindMismatch != 0:
		return DoneRecord{}, 0, "unknown"
	}
	w := &out[0]
	r := DoneRecord{Epoch: w.Epoch, Cursor: string(w.Cursor[:w.CursorLength])}
	if w.DoneCount > 0 {
		r.Done = make([]sprint.Completion, w.DoneCount)
	}
	for i := range r.Done {
		e := &w.Done[i]
		r.Done[i] = sprint.Completion{
			Kind: string(e.Kind[:e.KindLength]),
			Card: string(e.Card[:e.CardLength]),
			Row:  string(e.Row[:e.RowLength]),
			At:   doneTimeOf(e.At),
		}
	}
	pages := w.Pages
	if pages == 0 {
		pages = 1
	}
	return r, pages, ""
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
	r, pages, why := decodeDonePage(raw)
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
			pr, _, pwhy := decodeDonePage(vals[i])
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
	_, pages, why := decodeDonePage(raw)
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
