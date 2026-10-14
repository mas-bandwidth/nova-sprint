package main

import (
	"maps"
	"math/big"
	"slices"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/cardcost"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// Archived work is in no total (the owner, 2026-10-14: "archived cards and work streams
// should not show up in totals, dashboard, costs, anywhere"). where --json counts the
// streams on the table only: the archived streams (stream archive) are counted under
// `archived` (their names, cards, landed and cost) and nowhere else. Each helper here
// takes a figure the tick's where record counted over every stream of the epoch and
// leaves the archived streams out. A sprint done is the exception the headline already
// makes (sprintDone): with every stream archived its figures are the epoch's, so
// liveTotals leaves a done view as it is.

// liveStreamCosts is the streams' costs without the archived streams'; nil stays nil.
func liveStreamCosts(streams map[string]sprint.TierCosts, gone *archivedView) map[string]sprint.TierCosts {
	if streams == nil || gone == nil {
		return streams
	}
	out := make(map[string]sprint.TierCosts, len(streams))
	for k, tc := range streams {
		if !gone.has(k) {
			out[k] = tc
		}
	}
	return out
}

// liveTiers is the cards by tier without the archived streams' (each stream's own tier
// counts, sprint.TierCosts.Tiers, taken off the epoch's); a tier left at none is dropped.
func liveTiers(tiers map[string]int, streams map[string]sprint.TierCosts, gone *archivedView) map[string]int {
	if tiers == nil || gone == nil {
		return tiers
	}
	out := maps.Clone(tiers)
	for _, s := range gone.Streams {
		for t, n := range streams[s].Tiers {
			out[t] -= n
		}
	}
	maps.DeleteFunc(out, func(_ string, n int) bool { return n <= 0 })
	if len(out) == 0 {
		return nil
	}
	return out
}

// liveCostCheck is the cost check over the streams on the table: the archived streams'
// exact spend (TotalUSD) taken off both sums, so the gap and ok are the check's own and
// neither figure carries an archived dollar. A sum that does not read is left as it is.
func liveCostCheck(k *sprint.CostCheck, streams map[string]sprint.TierCosts, gone *archivedView) *sprint.CostCheck {
	if k == nil || gone == nil {
		return k
	}
	var off []string
	for _, s := range gone.Streams {
		if v := streams[s].TotalUSD; v != "" {
			off = append(off, v)
		}
	}
	if len(off) == 0 {
		return k
	}
	sum, ok := cardcost.Sum(off...)
	if !ok {
		return k
	}
	goneUSD := mustRat(sum)
	if goneUSD == nil {
		return k
	}
	out := *k
	out.Orphans = slices.Clone(k.Orphans)
	for _, p := range []*string{&out.Streams, &out.Cards} {
		r := mustRat(*p)
		if r == nil {
			continue
		}
		r.Sub(r, goneUSD)
		if r.Sign() < 0 {
			r.SetInt64(0)
		}
		*p = cardcost.Text(r)
	}
	return &out
}

// mustRat is a decimal the cost view wrote, exactly, or nil when it does not read.
func mustRat(s string) *big.Rat {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil
	}
	return r
}

// liveTotals takes the archived streams out of every per-stream list and total of the
// view that the where record counted over the epoch: the stream clocks and the stalled
// streams, the stage times by stream, the streams' default levels, the cards by tier and
// the cost check. stream_costs and read_spend are built from liveStreamCosts already, and
// the rows' done and ok% and the landed series leave them out where they are counted
// (sprint.LiveCompletions, lessArchivedDone, liveLandings).
func liveTotals(v *whereView, facts store.WhereFacts) {
	streams := facts.Streams
	gone := v.Archived
	if gone == nil || v.Done {
		return
	}
	if facts.ArchivedCounted {
		// the stage times the tick counted without the archived streams, overall and by
		// stream (sprint.CycleTimesExcept); a record from before is filtered by stream below
		v.StageTimes = nil
		if len(facts.LiveStageTimes.All) > 0 {
			st := facts.LiveStageTimes
			v.StageTimes = &st
		}
	}
	v.Streams = slices.DeleteFunc(slices.Clone(v.Streams), func(c sprint.StreamClock) bool { return gone.has(c.Stream) })
	if v.Streams == nil {
		v.Streams = []sprint.StreamClock{}
	}
	v.Stalled = slices.DeleteFunc(v.Stalled, gone.has)
	if len(v.Stalled) == 0 {
		v.Stalled = nil
	}
	if v.StageTimes != nil && v.StageTimes.Streams != nil {
		st := *v.StageTimes
		st.Streams = maps.Clone(st.Streams)
		maps.DeleteFunc(st.Streams, func(k string, _ map[string]sprint.StageStat) bool { return gone.has(k) })
		v.StageTimes = &st
	}
	if v.StreamPriorities != nil {
		v.StreamPriorities = maps.Clone(v.StreamPriorities)
		maps.DeleteFunc(v.StreamPriorities, func(k, _ string) bool { return gone.has(k) })
		if len(v.StreamPriorities) == 0 {
			v.StreamPriorities = nil
		}
	}
	v.Tiers = liveTiers(v.Tiers, streams, gone)
	v.CostCheck = liveCostCheck(v.CostCheck, streams, gone)
}

// liveLandings is the landings of the streams on the table: an archived stream's left out,
// but for a sprint done, whose series is the epoch's. A landing with no stream (a record
// from before it was carried) is kept.
func liveLandings(ls []sprint.Landing, gone *archivedView, done bool) []sprint.Landing {
	if gone == nil || done {
		return ls
	}
	return slices.DeleteFunc(slices.Clone(ls), func(l sprint.Landing) bool { return l.Stream != "" && gone.has(l.Stream) })
}

// lessArchivedDone is the fleet table with each row's ok and failed cells less its archived
// streams' work cards there (sprint.ArchivedFleetCards) finished after since (every one when
// since is zero), never below zero: the cells as FleetSince left them count from the stats
// reset's mark, so only the archived cards since it are in them. The rows' done (ok+failed)
// and ok% formulas then count the streams on the table.
func lessArchivedDone(t ntable.Table, cards []sprint.ArchivedFleetCard, since time.Time) ntable.Table {
	if len(cards) == 0 {
		return t
	}
	less := map[string]map[string]int64{}
	for _, c := range cards {
		if !since.IsZero() && (c.At == 0 || !time.Unix(c.At, 0).After(since)) {
			continue
		}
		if less[c.Row] == nil {
			less[c.Row] = map[string]int64{}
		}
		less[c.Row][c.Col]++
	}
	if len(less) == 0 {
		return t
	}
	cols := map[string]int{sprint.DoneOK: t.Column(sprint.DoneOK), sprint.DoneFailed: t.Column(sprint.DoneFailed)}
	rows := make([]ntable.Row, len(t.Rows))
	for i, r := range t.Rows {
		if by := less[r.Key]; by != nil {
			r.Cells = slices.Clone(r.Cells)
			for col, n := range by {
				if j := cols[col]; j >= 0 && j < len(r.Cells) && !r.Cells[j].Unread {
					r.Cells[j].Count = max(0, r.Cells[j].Count-n)
				}
			}
		}
		rows[i] = r
	}
	t.Rows = rows
	return t
}
