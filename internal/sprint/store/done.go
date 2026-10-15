package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/donewire"
)

// The done record (docs/SPEC-SPRINT.md section 1, done and ok%): every card a row completed,
// of every kind sprint.DoneKinds registers, read off the log lines that complete them (the
// record a completion leaves: a read card retires off its row with its verdict, so the row's
// cells forget it; the log does not). The tick keeps the record (keepDone, with the where
// record, STOPPED or RUNNING): the completions of the lines after its cursor, at most
// logIndexPages pages a tick, so a record behind the log (after a clear, or the first tick of
// a build that keeps it) catches up over the next ticks. where counts from the record
// (DoneSince), at most the tick in flight behind, as its other records are. The record is the
// epoch's: one of another epoch is counted again. tla/DoneRecord.tla models it (NoDoubleCount,
// CaughtUpIsTruth, ResetShowsZero, ClearStartsAgain).

const keyDoneRecord = "donerecord" // STRING, the done record (the fixed table, or legacy JSON); "done" is the operations' hash

// DoneRecord is the done record: the epoch, the stream id of the last log line read, and the
// completions of the lines read.
type DoneRecord struct {
	Epoch  uint64              `json:"epoch"`
	Cursor string              `json:"cursor,omitempty"`
	Done   []sprint.Completion `json:"done,omitempty"`
}

// readDoneRecord is the epoch's done record as stored; empty when there is none, it does not
// read, or it is another epoch's.
func (st *Store) readDoneRecord(ctx context.Context, kv KV) (DoneRecord, error) {
	raw, ok, err := kv.GetKey(ctx, keyDoneRecord)
	if err != nil {
		return DoneRecord{Epoch: st.epoch}, err
	}
	return st.doneRecordOf(raw, ok), nil
}

// doneRecordOf is the epoch's done record of a stored value; empty when there is none, it does
// not read, or it is another epoch's.
func (st *Store) doneRecordOf(raw string, ok bool) DoneRecord {
	if !ok {
		return DoneRecord{Epoch: st.epoch}
	}
	r, why := decodeDoneRecord(raw)
	if why != "" || r.Epoch != st.epoch {
		return DoneRecord{Epoch: st.epoch}
	}
	return r
}

// The stored forms (TRACER, schema-tracer-done-record-fixed; docs/research/schema-tracer/
// done-record.md). The record is the schema compiler's FIXED table DoneRecord
// (schemas/store/done.schema, generated into donewire): a form-3 file of one record, its
// first byte 3. A record the fixed form cannot hold (more completions than the declared max,
// a text past its bound or not UTF-8 without NUL, a time past int64 nanoseconds) keeps the
// legacy JSON form, whose first byte is '{'; JSON is read for one release whatever wrote it.
// A newer layout (layout_newer, layout_unsupported) is refused loudly: keepDone never writes
// this build's older layout over it.
const (
	doneFormFixed    = "fixed" // the stored form's name: the fixed table
	doneFormJSON     = "json"  // the legacy form, "json:<why>" when the fixed form refused the record
	doneFixedMaxDone = 95      // the done list's declared max (schemas/store/done.schema)
)

// doneFixedPlanRoom is the plan scratch DoneRecordFixedLoad is handed: the identity plan's
// entries and room for any lineage plan (it refuses plan_too_large past it).
var doneFixedPlanRoom = 2 * len(donewire.DoneRecordFixedPlan.Entries)

// doneRefusedNewer says a refusal names a layout newer than this build's.
func doneRefusedNewer(why string) bool { return why == "layout_newer" || why == "layout_unsupported" }

// decodeDoneRecord is a stored done record, or the name of the refusal that read nothing: the
// schema runtime's (report.Reason: layout_newer, layout_malformed, no_layout, previous_form,
// newer_form, message_form_as_file, batch_too_large, text_ill_formed, ...), or this store's
// where the runtime gives a flag or a counter and no name: empty, malformed (report.Malformed),
// record_count (not one record), clamped (a count or length past its bound, which the runtime
// clamps and counts), unknown (a field the writer had and this layout does not), and
// legacy_json_unreadable.
func decodeDoneRecord(raw string) (DoneRecord, string) {
	if raw == "" {
		return DoneRecord{}, "empty"
	}
	if raw[0] == '{' {
		var r DoneRecord
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return DoneRecord{}, "legacy_json_unreadable"
		}
		return r, ""
	}
	out := make([]donewire.DoneRecord, 1)
	plan := make([]donewire.TableFixedEntry, doneFixedPlanRoom)
	var rep donewire.TableReport
	n := donewire.DoneRecordFixedLoad(out, []byte(raw), plan, &rep)
	switch {
	case n < 0 && rep.Reason != "":
		return DoneRecord{}, rep.Reason
	case n < 0 || rep.Malformed:
		return DoneRecord{}, "malformed"
	case n != 1:
		return DoneRecord{}, "record_count"
	case rep.Clamped != 0:
		return DoneRecord{}, "clamped"
	case rep.Unknown != 0 || rep.KindMismatch != 0:
		return DoneRecord{}, "unknown"
	}
	w := &out[0]
	r := DoneRecord{Epoch: w.Epoch, Cursor: string(w.Cursor[:w.CursorLength])}
	if w.DoneCount > 0 {
		r.Done = make([]sprint.Completion, w.DoneCount)
	}
	for i := range r.Done {
		e := &w.Done[i]
		r.Done[i] = sprint.Completion{Kind: string(e.Kind[:e.KindLength]), Card: string(e.Card[:e.CardLength]),
			Row: string(e.Row[:e.RowLength]), At: doneTimeOf(e.At)}
	}
	return r, ""
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

// doneFixedOf is r as the fixed table's value, or why the fixed form cannot hold it.
func doneFixedOf(r DoneRecord, w *donewire.DoneRecord) string {
	donewire.DoneRecordReset(w)
	if len(r.Done) > len(w.Done) {
		return "over_max_count"
	}
	w.Epoch = r.Epoch
	var why string
	if w.CursorLength, why = doneText(w.Cursor[:], r.Cursor); why != "" {
		return why
	}
	w.DoneCount = int32(len(r.Done))
	for i, c := range r.Done {
		e := &w.Done[i]
		for _, f := range []struct {
			n   *int32
			dst []byte
			s   string
		}{{&e.KindLength, e.Kind[:], c.Kind}, {&e.CardLength, e.Card[:], c.Card}, {&e.RowLength, e.Row[:], c.Row}} {
			if *f.n, why = doneText(f.dst, f.s); why != "" {
				return why
			}
		}
		if !c.At.IsZero() {
			// UnixNano is undefined outside about 1678..2262; a time that does not
			// come back is one the form cannot hold
			if n := c.At.UnixNano(); n == 0 || !time.Unix(0, n).Equal(c.At) {
				return "time_out_of_range"
			} else {
				e.At = n
			}
		}
	}
	return ""
}

// encodeDoneRecord is the done record's stored form and the form's name: the fixed table, or
// the legacy JSON with the reason the fixed form refused the record ("json:<why>").
func encodeDoneRecord(r DoneRecord) (string, string, error) {
	var w donewire.DoneRecord
	why := doneFixedOf(r, &w)
	if why == "" {
		b := make([]byte, donewire.DoneRecordFixedMeasure(1))
		if donewire.DoneRecordFixedSave([]donewire.DoneRecord{w}, b) == int64(len(b)) {
			return string(b), doneFormFixed, nil
		}
		why = "save_refused"
	}
	b, err := json.Marshal(r)
	return string(b), doneFormJSON + ":" + why, err
}

// doneAfter adds to r the completions of the log's lines after its cursor, at most pages
// pages of logPage lines (every page when pages is 0), and moves its cursor; more says lines
// were left.
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
// exchange): the lines after its cursor read, at most logIndexPages pages, and the record
// written when it moved.
func (st *Store) keepDone(ctx context.Context, kv KV, raw string, ok bool) error {
	if ok {
		if _, why := decodeDoneRecord(raw); doneRefusedNewer(why) {
			return fmt.Errorf("%s: the stored done record is a newer build's layout; this build reads the log whole and writes nothing over it", why)
		}
	}
	r := st.doneRecordOf(raw, ok)
	was := r.Cursor
	if _, err := st.doneAfter(ctx, &r, logIndexPages); err != nil {
		return err
	}
	if r.Cursor == was {
		return nil
	}
	b, _, err := encodeDoneRecord(r)
	if err != nil {
		return err
	}
	return kv.SetKey(ctx, keyDoneRecord, b)
}

// DoneSince is each fleet row's done by kind: the completions after since (a stats reset's
// mark; every completion of the epoch when since is zero), from the done record the tick keeps,
// as where takes its other records: at most the tick in flight behind, and the log never read
// by where once a tick has kept it (where_landed_series_test: where reads no log line). With no
// record of the epoch yet (before the first tick of a build that keeps it, or after a clear),
// the epoch's log is read whole, as where reads the cards before the where record.
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
// with the where record (WhereFacts, one exchange), or, with none of the epoch yet, DoneSince.
func (st *Store) DoneOf(ctx context.Context, f WhereFacts, since time.Time) (sprint.DoneCounts, error) {
	if f.Done.Cursor != "" {
		return sprint.CountDone(f.Done.Done, since), nil
	}
	return st.DoneSince(ctx, since)
}
