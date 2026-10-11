package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The done record (docs/SPEC-SPRINT.md section 1, done and ok%): every card a row completed,
// of every kind sprint.DoneKinds registers, read off the log lines that complete them (the
// record a completion leaves: a read card retires off its row with its verdict, so the row's
// cells forget it; the log does not). The tick keeps the record (keepDone, with the where
// record, STOPPED or RUNNING): the completions of the lines after its cursor, at most
// logIndexPages pages a tick, so a record behind the log (after a clear, or the first tick of
// a build that keeps it) catches up over the next ticks. where counts from the record
// (DoneSince), at most the tick in flight behind, as its other records are. The record is the
// epoch's: one of another epoch is counted again.

const keyDoneRecord = "donerecord" // STRING, the done record (JSON); "done" is the operations' hash

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
	var r DoneRecord
	if !ok || json.Unmarshal([]byte(raw), &r) != nil || r.Epoch != st.epoch {
		return DoneRecord{Epoch: st.epoch}
	}
	return r
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
	r := st.doneRecordOf(raw, ok)
	was := r.Cursor
	if _, err := st.doneAfter(ctx, &r, logIndexPages); err != nil {
		return err
	}
	if r.Cursor == was {
		return nil
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return kv.SetKey(ctx, keyDoneRecord, string(b))
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
