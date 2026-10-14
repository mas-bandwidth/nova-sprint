package main

import (
	"maps"
	"math/big"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/cardcost"
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
// the cost check. stream_costs and read_spend are built from liveStreamCosts already.
func liveTotals(v *whereView, streams map[string]sprint.TierCosts) {
	gone := v.Archived
	if gone == nil || v.Done {
		return
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
