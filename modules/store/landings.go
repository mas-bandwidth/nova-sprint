package store

import (
	"context"
	"strings"
	"time"
)

// ImportedReason starts the reason of a producer the v1 import carried over
// as landed: it landed before this run, so it is no landing of the sprint.
const ImportedReason = "its producer landed in v1"

// LandedIn is how many producers an accepted event moved to landed (the
// import's excluded): the cards whose after-image is landed and whose
// before-image is not.
func LandedIn(e Event) int {
	if !e.Accepted || e.After == nil {
		return 0
	}
	n := 0
	for id, c := range e.After.Cards {
		if c.Kind != "producer" || c.State != "landed" || strings.HasPrefix(c.Reason, ImportedReason) {
			continue
		}
		if e.Before != nil && e.Before.Cards[id].State == "landed" {
			continue
		}
		n++
	}
	return n
}

// Landings returns the time of every landing in epoch up to and including
// event upToSeq, oldest first: the dashboard's rate, independent of how long
// it has been running.
func (f *Fake) Landings(ctx context.Context, epoch int, upToSeq int64) ([]time.Time, error) {
	var out []time.Time
	for after := int64(0); ; {
		evs, err := f.Events(ctx, after, MaxEvents)
		if err != nil {
			return nil, err
		}
		for _, e := range evs {
			after = e.Seq
			if e.Seq > upToSeq || e.Epoch != epoch {
				continue
			}
			for i := LandedIn(e); i > 0; i-- {
				out = append(out, e.At)
			}
		}
		if len(evs) < MaxEvents {
			return out, nil
		}
	}
}

// Landings is Fake's, read in SQL on the events' images.
func (p *PG) Landings(ctx context.Context, epoch int, upToSeq int64) ([]time.Time, error) {
	rows, err := p.pool.Query(ctx, `SELECT e.at FROM sprint.events e, jsonb_each(e.after->'cards') a(id, c)
		WHERE e.accepted AND e.epoch = $1 AND e.seq <= $2 AND e.after IS NOT NULL
		AND a.c->>'kind' = 'producer' AND a.c->>'state' = 'landed'
		AND coalesce(e.before->'cards'->a.id->>'state', '') <> 'landed'
		AND a.c->>'reason' NOT LIKE $3
		ORDER BY e.at`, epoch, upToSeq, ImportedReason+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var at time.Time
		if err := rows.Scan(&at); err != nil {
			return nil, err
		}
		out = append(out, at)
	}
	return out, rows.Err()
}
