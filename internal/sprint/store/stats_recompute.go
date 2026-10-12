package store

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// RecomputeResult is the stored fleet projection before and after rebuilding
// it from typed end events. Unknown older ends remain visible as unknown.
type RecomputeResult struct {
	Epoch uint64 `json:"epoch"`
	DryRun bool `json:"dry_run"`
	Changed bool `json:"changed"`
	Rows map[string]sprint.EndCounts `json:"rows"`
	Unknown int `json:"unknown"`
}

// RecomputeStats rebuilds only the end counters. A dry run reads the same log
// and stats record, then returns without writing either one.
func (st *Store) RecomputeStats(ctx context.Context, dry bool) (RecomputeResult, error) {
	st, err := st.pin(ctx)
	if err != nil { return RecomputeResult{}, err }
	rows, cursor, err := st.endCountsSince(ctx, nil, "")
	if err != nil { return RecomputeResult{}, err }
	res := RecomputeResult{Epoch: st.epoch, DryRun: dry, Rows: rows}
	for _, row := range rows { res.Unknown += row.Unknown }
	previous, err := st.StatsTidied(ctx)
	if err != nil { return res, err }
	res.Changed = previous.EndsEpoch != st.epoch || !reflect.DeepEqual(previous.Ends, rows) || previous.EndsCursor != cursor
	if dry || !res.Changed { return res, nil }
	kv, err := st.kv()
	if err != nil { return res, err }
	_, err = st.updateStats(ctx, kv, func(rec *StatsRecord) error {
		if rec.Tidying != nil && rec.Tidying.Fresh(st.now()) {
			return fmt.Errorf("stats tidy is in flight; retry recompute after it completes")
		}
		rec.EndsEpoch, rec.EndsAt, rec.Ends, rec.EndsCursor = st.epoch, st.now().UTC().Truncate(time.Nanosecond), rows, cursor
		return nil
	})
	return res, err
}

// endCountsSince folds log lines after cursor. It uses only stored typed move
// fields; a legacy end without a typed cause remains in the unknown bucket.
func (st *Store) endCountsSince(ctx context.Context, base map[string]sprint.EndCounts, cursor string) (map[string]sprint.EndCounts, string, error) {
	rows := maps.Clone(base)
	if rows == nil { rows = map[string]sprint.EndCounts{} }
	for {
		lines, ids, err := st.B.LogSince(ctx, cursor, logPage)
		if err != nil { return nil, "", err }
		for _, line := range lines { sprint.AddEndLine(rows, line) }
		if len(ids) == 0 { return rows, cursor, nil }
		cursor = ids[len(ids)-1]
		if len(ids) < logPage { return rows, cursor, nil }
	}
}

// CurrentEndCounts includes committed ends after the last recompute or tick.
// It does not write; callers may use it for both text and JSON projections.
func (st *Store) CurrentEndCounts(ctx context.Context, rec StatsRecord) (map[string]sprint.EndCounts, error) {
	if rec.EndsEpoch != st.epoch { rec.Ends, rec.EndsCursor = nil, "" }
	rows, _, err := st.endCountsSince(ctx, rec.Ends, rec.EndsCursor)
	return rows, err
}

// keepEndCounts advances the durable projection on the tick. A compare-and-set
// retry rereads the cursor, so two ticks cannot count the same end twice.
func (st *Store) keepEndCounts(ctx context.Context, kv KV) error {
	_, err := st.updateStats(ctx, kv, func(rec *StatsRecord) error {
		if rec.EndsEpoch != st.epoch { rec.Ends, rec.EndsCursor = nil, "" }
		rows, cursor, err := st.endCountsSince(ctx, rec.Ends, rec.EndsCursor)
		if err != nil { return err }
		rec.EndsEpoch, rec.Ends, rec.EndsCursor = st.epoch, rows, cursor
		rec.EndsAt = st.now().UTC()
		return nil
	})
	return err
}
