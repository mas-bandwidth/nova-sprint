package store

import (
	"cmp"
	"context"
	"strings"
	"time"
)

// ImportedReason starts the reason of a producer the v1 import carried over
// as landed: it landed before this run, so it is no landing of the sprint.
const ImportedReason = "its producer landed in v1"

// LandedReason starts the reason of every producer recorded landed: its
// merge is on main (LANDING.md, "Landed means on main"). A landed producer
// whose reason does not start with it was recorded before landed meant on
// main, and Migrate moves it back to merging (migrateLanded).
const LandedReason = "landed on main"

// OnBasePrefix starts the reason of a merging producer whose head is merged
// onto its stream's base, a branch that is not main: the base merge is
// recorded (OnBaseReason) and the producer lands when that merge is on main.
const OnBasePrefix = "on base "

// MigratedNote ends the reason migrateLanded gives a producer it moves back
// to merging; the ETA's landing times skip that producer's next landing, so
// it is never counted twice.
const MigratedNote = "recorded landed before landed meant on main"

// OnBaseReason is the reason that records a producer's base merge: merge
// is the commit its landing made or found on base ("-" when not known; the
// promotion then checks the producer's head).
func OnBaseReason(base, merge string) string {
	return OnBasePrefix + cmp.Or(merge, "-") + ": merged onto " + base + "; landed when that merge is on main"
}

// BaseMerge is the base merge a reason records, and whether it records one
// ("" with true when the merge is not known).
func BaseMerge(reason string) (string, bool) {
	rest, ok := strings.CutPrefix(reason, OnBasePrefix)
	if !ok {
		return "", false
	}
	merge, _, _ := strings.Cut(rest, ":")
	if merge == "-" {
		merge = ""
	}
	return merge, true
}

// LandedIn is how many producers an accepted event moved to landed (the
// import's excluded): the cards whose after-image is landed and whose
// before-image is not, nor a producer migrateLanded moved back to merging
// (its landing was counted when it was first recorded).
func LandedIn(e Event) int {
	if !e.Accepted || e.After == nil {
		return 0
	}
	n := 0
	for id, c := range e.After.Cards {
		if c.Kind != "producer" || c.State != "landed" || strings.HasPrefix(c.Reason, ImportedReason) {
			continue
		}
		if b := e.Before; b != nil && (b.Cards[id].State == "landed" || strings.HasSuffix(b.Cards[id].Reason, MigratedNote)) {
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
		AND coalesce(e.before->'cards'->a.id->>'reason', '') NOT LIKE $4
		AND a.c->>'reason' NOT LIKE $3
		ORDER BY e.at`, epoch, upToSeq, ImportedReason+"%", "%"+MigratedNote)
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
