package store

import (
	"context"
	"fmt"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// keyArchive is the archive's record: the streams the coordinator brought back
// by hand (stream unarchive), which the tick does not archive again until each
// holds a card not landed and lands it.
const keyArchive = "archive" // STRING, the archive record (JSON)

// archiveRecord is the archive's record as stored.
type archiveRecord struct {
	Kept []string `json:"kept,omitempty"`
}

// ArchiveStreams hides the named streams' rows of the work and merge tables
// (stream archive, sprint.StreamArchive): no card moves, so every landed card,
// its cost and its landing stay in the tables, their folds and every record
// that reads them. Refused, nothing written, for a stream that is no row or
// holds a card not landed, all or none; the machine may be RUNNING.
func (st *Store) ArchiveStreams(ctx context.Context, names []string) ([]sprint.Refusal, error) {
	return st.archive(ctx, names, true)
}

// UnarchiveStreams draws the named archived streams' rows again (stream
// unarchive, sprint.StreamUnarchive), and the tick leaves them drawn until each
// holds a card not landed again and lands it.
func (st *Store) UnarchiveStreams(ctx context.Context, names []string) ([]sprint.Refusal, error) {
	return st.archive(ctx, names, false)
}

func (st *Store) archive(ctx context.Context, names []string, hide bool) ([]sprint.Refusal, error) {
	st, err := st.pin(ctx)
	if err != nil {
		return nil, err
	}
	s, err := st.Load(ctx, []string{sprint.Work, sprint.Merge}, nil)
	if err != nil {
		return nil, err
	}
	rule := sprint.StreamArchive
	if !hide {
		rule = sprint.StreamUnarchive
	}
	if refused := rule(s, names); len(refused) > 0 {
		return refused, nil
	}
	rec, err := st.archiveRecord(ctx)
	if err != nil {
		return nil, err
	}
	kept := slices.DeleteFunc(slices.Clone(rec.Kept), func(k string) bool { return slices.Contains(names, k) })
	if !hide {
		kept = append(kept, names...)
	}
	// each first writes what keeps the tick off the streams: archive hides the
	// rows, then takes them out of the kept record; unarchive puts them in it,
	// then draws the rows. A tick between the two writes finds them hidden, or
	// kept, and never names a stream the coordinator archived (with the record
	// first, a tick between the writes archives it and names it as its own)
	if hide {
		if err := st.hideStreams(ctx, names, true); err != nil {
			return nil, err
		}
		return nil, st.putArchive(ctx, kept)
	}
	if err := st.putArchive(ctx, kept); err != nil {
		return nil, err
	}
	return nil, st.hideStreams(ctx, names, false)
}

// hideStreams hides or draws the streams' rows of the work and merge tables.
func (st *Store) hideStreams(ctx context.Context, names []string, hide bool) error {
	if len(names) == 0 {
		return nil
	}
	for _, t := range []string{sprint.Work, sprint.Merge} {
		do := st.B.RowsShow
		if hide {
			do = st.B.RowsHide
		}
		if err := do(ctx, st.Names.Table(t), names); err != nil {
			return fmt.Errorf("the rows of %s: %w", st.Names.Table(t), err)
		}
	}
	return nil
}

func (st *Store) archiveRecord(ctx context.Context) (archiveRecord, error) {
	var rec archiveRecord
	if _, ok := st.B.(KV); !ok {
		return rec, nil
	}
	return rec, st.getJSON(ctx, keyArchive, &rec)
}

func (st *Store) putArchive(ctx context.Context, kept []string) error {
	if _, ok := st.B.(KV); !ok {
		return nil
	}
	slices.Sort(kept)
	return st.putJSON(ctx, keyArchive, archiveRecord{Kept: slices.Compact(kept)})
}

// ArchiveResult is what the tick's archive part did.
type ArchiveResult struct {
	Archived []string `json:"archived,omitempty"` // streams whose last card landed, hidden
	Shown    []string `json:"shown,omitempty"`    // archived streams that hold a card not landed again, drawn
}

// keepArchive is the tick's archive part, from the shapes of the work and
// merge tables alone (no card is read): an archived stream that holds a card
// not landed again (an add, a recut) is drawn again, whether the machine runs
// or not. Streams are archived only by the explicit stream archive command
// (docs/SPEC-SPRINT.md section 16; the owner, 2026-10-11: archiving is a
// manual request only).
func (st *Store) keepArchive(ctx context.Context, running bool) (ArchiveResult, error) {
	var res ArchiveResult
	st, err := st.pin(ctx)
	if err != nil {
		return res, err
	}
	shapes, err := st.shapes(ctx, []string{st.Names.Table(sprint.Work), st.Names.Table(sprint.Merge)})
	if err != nil {
		return res, err
	}
	work, merge := shapes[0], shapes[1]
	waits := map[string]bool{}
	for _, r := range merge.Rows {
		if cellCount(merge, r, sprint.Queued)+cellCount(merge, r, sprint.Stuck) > 0 {
			waits[r.Key] = true
		}
	}
	f, err := st.B.ReadFence(ctx)
	if err != nil {
		return res, err
	}
	if f.Queued > 0 {
		q, err := st.B.QueueRead(ctx)
		if err != nil {
			return res, err
		}
		for _, c := range q {
			switch {
			case c.Entry == nil:
			case c.Entry.Create != nil:
				waits[c.Entry.Create.Row] = true
			case c.Entry.Move != nil:
				waits[c.Entry.Move.Row] = true
			}
		}
	}
	rec, err := st.archiveRecord(ctx)
	if err != nil {
		return res, err
	}
	kept := slices.Clone(rec.Kept)
	for _, r := range work.Rows {
		landed, open := 0, 0
		for k, c := range work.Columns {
			if !c.HasSet() || k >= len(r.Cells) {
				continue
			}
			if c.Name == sprint.Landed {
				landed += int(r.Cells[k].Count)
			} else {
				open += int(r.Cells[k].Count)
			}
		}
		switch {
		case open > 0 || waits[r.Key]:
			// work again: draw the stream (show it on the table again)
			kept = slices.DeleteFunc(kept, func(k string) bool { return k == r.Key })
			if r.Hidden {
				res.Shown = append(res.Shown, r.Key)
			}
		case !running || r.Hidden || landed == 0 || slices.Contains(kept, r.Key):
		default:
			// tick does not archive: streams are archived only by stream archive
		}
	}
	if !slices.Equal(kept, rec.Kept) {
		if err := st.putArchive(ctx, kept); err != nil {
			return res, err
		}
	}
	if err := st.hideStreams(ctx, res.Shown, false); err != nil {
		return res, err
	}
	return res, nil
}

// cellCount is the count cell of the row's column, 0 when the table has none.
func cellCount(t ntable.Table, r ntable.Row, col string) int64 {
	if j := t.Column(col); j >= 0 && j < len(r.Cells) {
		return r.Cells[j].Count
	}
	return 0
}
