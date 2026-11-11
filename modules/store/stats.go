package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// StatsCursor is the cursor `nova-sprint stats reset` sets to the seq the
// coordinator read: the board's Fleet and Friends done, ok, failed and ok%
// count only the consumers done after it. Unset, they count the epoch's.
const StatsCursor = "stats"

// StatsMark is the board's stats mark: the cursor's seq, the time of the
// event that set it (zero when none in the log did), and each consumer an
// accepted event after it moved to done, with that event's seq.
type StatsMark struct {
	Seq  int64
	At   time.Time
	Done map[string]int64
}

// DoneIn is the consumers an accepted event moved to done: their after-image
// is done and their before-image is not.
func DoneIn(e Event) []string {
	if !e.Accepted || e.After == nil {
		return nil
	}
	var out []string
	for id, c := range e.After.Cards {
		if (c.Kind != "work" && c.Kind != "read") || c.State != "done" {
			continue
		}
		if b := e.Before; b != nil && b.Cards[id].State == "done" {
			continue
		}
		out = append(out, id)
	}
	return out
}

// Add folds e into m: the time when e set the mark, and the consumers it
// moved to done.
func (m *StatsMark) Add(e Event) {
	if !e.Accepted || e.After == nil {
		return
	}
	if v, ok := e.After.Cursors[StatsCursor]; ok && v == m.Seq && m.At.IsZero() {
		m.At = e.At
	}
	for _, id := range DoneIn(e) {
		m.Done[id] = e.Seq
	}
}

// StatsSince reads the mark at seq mark from the events after it.
func (f *Fake) StatsSince(ctx context.Context, mark int64) (StatsMark, error) {
	m := StatsMark{Seq: mark, Done: map[string]int64{}}
	for after := mark; ; {
		evs, err := f.Events(ctx, after, MaxEvents)
		if err != nil {
			return StatsMark{}, err
		}
		for _, e := range evs {
			after = e.Seq
			m.Add(e)
		}
		if len(evs) < MaxEvents {
			return m, nil
		}
	}
}

// StatsSince is Fake's, read in SQL on the events' images.
func (p *PG) StatsSince(ctx context.Context, mark int64) (StatsMark, error) {
	m := StatsMark{Seq: mark, Done: map[string]int64{}}
	err := p.pool.QueryRow(ctx, `SELECT at FROM sprint.events
		WHERE accepted AND seq > $1 AND after IS NOT NULL AND after->'cursors'->$2::text = to_jsonb($1::bigint)
		ORDER BY seq LIMIT 1`, mark, StatsCursor).Scan(&m.At)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return StatsMark{}, err
	}
	rows, err := p.pool.Query(ctx, `SELECT a.id, max(e.seq) FROM sprint.events e, jsonb_each(e.after->'cards') a(id, c)
		WHERE e.accepted AND e.seq > $1 AND e.after IS NOT NULL
		AND a.c->>'kind' IN ('work', 'read') AND a.c->>'state' = 'done'
		AND coalesce(e.before->'cards'->a.id->>'state', '') <> 'done'
		GROUP BY a.id`, mark)
	if err != nil {
		return StatsMark{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var seq int64
		if err := rows.Scan(&id, &seq); err != nil {
			return StatsMark{}, err
		}
		m.Done[id] = seq
	}
	return m, rows.Err()
}
