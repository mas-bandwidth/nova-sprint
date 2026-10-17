package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/viewrowswire"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
)

// The where record (docs/SPEC-SPRINT.md section 1): what where shows that the
// work table's count cells do not carry, the held cards and the landings of
// the last hour, counted by the tick from the sprint it reads anyway and kept
// in one key, so where reads no card (the owner's requirement of 2026-09-30,
// one read a tick, and where under 1 s at 3,000 cards: at 2,843 it read every
// waiting and landed card's record, 5 to 7 s on the live store, 2026-10-02).
//
// Held is not a count a step can keep by adding one: a card is held when it
// waits, through a need or its place in line, on a sentinel not released or a
// card admitted held, so one release or one need frees or holds a chain. The
// tick counts it whole (sprint.HeldBack) from its twin, which holds every card,
// whenever the work table or the fleet table has moved since the record was counted.

// keyWhere is the where record's key, under the deployment's prefix.
const keyWhere = "where" // STRING, the where record (JSON)

// WhereRecord is the where record: the epoch and the work table's revision it
// was counted at, the held cards there (sprint.HeldBack), and the landings
// LandingRate may count from then on (sprint.RecentLandings), in Unix seconds,
// oldest first.
type WhereRecord struct {
	DecisionHolds        []sprint.Note `json:"decision_holds,omitempty"`
	DecisionHoldsCounted bool          `json:"decision_holds_counted"`
	Epoch                uint64        `json:"epoch"`
	Rev                  uint64        `json:"rev"`
	Held                 int           `json:"held"`
	// HeldStreams is Held counted per stream (sprint.HeldBackByStream):
	// where --json's stream clocks carry it as held_cards beside each
	// stream's stored state (a-waiting-stream-is-never-shown-held).
	HeldStreams map[string]int        `json:"held_streams,omitempty"`
	Landings    []int64               `json:"landings,omitempty"`
	Critical    []sprint.CriticalCard `json:"critical,omitempty"` // the five heaviest (weight.go)
	Blockers    []string              `json:"blockers,omitempty"` // the top blockers tracing why waiting cards are not moving (sprint.IdleBlockers)
	// Sentinels is each stream's sentinel primaries, all and landed, counted here
	// from the cards: a sentinel is a stop the coordinator releases, not work, so
	// where's x/y, the ETA and the landing rate leave them out (docs/SPEC-SPRINT.md
	// sections 1 and 16). The work table's cells still count them.
	Sentinels map[string]SentinelCounts `json:"sentinels,omitempty"`
	// Tiers counts every card by its brief's tier, and Streams carries each stream's
	// tiers, dollars per landed card and spend by tier (sprint.TierCosts, cost_view.go):
	// counted here from the cards the tick reads, never by where from per-card reads.
	Tiers   map[string]int              `json:"tiers,omitempty"`
	Streams map[string]sprint.TierCosts `json:"streams,omitempty"`
	// CostCheck is the streams' sums against every card's own total, counted with Streams
	// before a stats reset's mark is taken off (sprint.CostCheckOf): where prints its line.
	CostCheck *sprint.CostCheck `json:"cost_check,omitempty"`
	// StageTimes is the median and p90 of each stage over the cards landed in the last day
	// (sprint.CycleTimes, docs/SPEC-SPRINT.md, cycle-time-breakdownb.w1), as of the count.
	StageTimes sprint.StageTimes `json:"stage_times,omitzero"`
	// DealtFleet is each friend's count of the fleet's cards on her row
	// (sprint.FriendsDealtFleet), where --json's dealt_fleet.
	DealtFleet map[string]int `json:"dealt_fleet,omitempty"`
	// ReadsWaiting is the reads wanted now and not asked over the primaries in review
	// (sprint.ReadsWaiting), Priorities every open primary whose level is not normal, by
	// level (sprint.PriorityCounts), and StreamPriorities each stream's default level that
	// is not normal (sprint.StreamPriorities), as of the count.
	ReadsWaiting     int                 `json:"reads_waiting,omitempty"`
	Priorities       map[string][]string `json:"priorities,omitempty"`
	StreamPriorities map[string]string   `json:"stream_priorities,omitempty"`
	// ReadsWindow is the ok and broken verdicts over the last 30 minutes of running time
	// (sprint.ReadsWindowOf), as of the count.
	ReadsWindow sprint.ReadsWindowView `json:"reads_window,omitzero"`
	// FleetRev is the fleet table's revision at the count: a take moves the fleet alone.
	// RowCards is each fleet row's cards by level and its reads, ReadCards the epoch's read
	// cards (sprint.RowCardCounts), the dashboard's rows and read_cards.
	FleetRev  uint64                    `json:"fleet_rev,omitempty"`
	RowCards  map[string]map[string]int `json:"row_cards,omitempty"`
	ReadCards sprint.ReadCardCounts     `json:"read_cards"`
	FixStates map[string]map[string]int `json:"fix_states,omitempty"`
	// RowWorking is each fleet row's working cards as <card>@<gen> (sprint.RowWorkingKeys):
	// where counts them against the rows' beats at each read (sprint.LiveCountKeys), so the
	// dashboard's working cell is the live runs, never a take whose run is gone (v1.2.6).
	RowWorking map[string][]string `json:"row_working,omitempty"`
	// Stats is the stats record the spend and per landed were counted from (statsStamp: the
	// last tidy of the streams and the reset's mark); a record whose stamp is not the stats
	// record's is counted again and never taken, though no table moved.
	Stats string `json:"stats,omitempty"`
	// Series is the landings of the last day (sprint.SeriesLandings, sprint.SeriesWindow),
	// which where --json's landedSeries buckets at its own time, and SeriesCounted says the
	// tick counted them: a record from before the count has none, and where reads the log
	// for the series as before (2026-10-10: that read was 82% of where's 12.7 s on the live
	// store, 695,000 log lines decoded a call).
	Series        []sprint.Landing `json:"series,omitempty"`
	SeriesCounted bool             `json:"series_counted,omitempty"`
	// Archived work is in no total (the owner, 2026-10-14; sprint/archived_totals.go):
	// ArchivedPrimaries is the archived streams' cards, whose completions where leaves out of
	// the rows' done; ArchivedFleet their work cards in the fleet rows' ok and failed cells,
	// which where takes off the rows' ok%; LiveStageTimes the stage times without them.
	// ArchivedCounted says this record counted them: one from before is counted again once.
	ArchivedPrimaries []string                   `json:"archived_primaries,omitempty"`
	ArchivedFleet     []sprint.ArchivedFleetCard `json:"archived_fleet,omitempty"`
	LiveStageTimes    sprint.StageTimes          `json:"live_stage_times,omitzero"`
	ArchivedCounted   bool                       `json:"archived_counted,omitempty"`
}

// SentinelCounts is one stream's sentinel primaries: All, every sentinel the
// stream holds, and Landed, those released into landed (docs/SPEC-SPRINT.md
// section 16). where subtracts them from the stream's cells.
type SentinelCounts struct {
	All    int `json:"all,omitempty"`
	Landed int `json:"landed,omitempty"`
	// ByCol is the sentinels by the column they are in, so where takes them off each of the
	// work table's cells (the owner, 2026-10-14: sentinels are counted nowhere); nil in a
	// record from before, when where takes All-Landed off waiting and Landed off landed.
	ByCol map[string]int `json:"by_col,omitempty"`
}

// sentinelCounts is each stream's sentinels among the snapshot's work cards, in
// place (waiting, ready, working, review, merging, landed), all and landed:
// carried in the where record so where counts no sentinel (docs/SPEC-SPRINT.md
// sections 1 and 16). The work table's cells count them with the work.
func sentinelCounts(s *sprint.Snapshot) map[string]SentinelCounts {
	if s == nil || s.Work == nil {
		return nil
	}
	out := map[string]SentinelCounts{}
	for _, c := range s.Work.Column(sprint.States...) {
		if !sprint.IsSentinel(c) {
			continue
		}
		n := out[c.Row]
		n.All++
		if c.Col == sprint.Landed {
			n.Landed++
		}
		if n.ByCol == nil {
			n.ByCol = map[string]int{}
		}
		n.ByCol[c.Col]++
		out[c.Row] = n
	}
	return out
}

// whereOf is the where record of a snapshot holding every card of the work
// table, at now, for the machine's STOPPED spans.
func whereOf(s *sprint.Snapshot, m Machine, now time.Time) WhereRecord {
	var landed []time.Time
	for _, c := range s.Work.Column(sprint.Landed) {
		// a released sentinel is not work: its stamp is no landing the rate
		// counts (docs/SPEC-SPRINT.md sections 1 and 16)
		if sprint.IsSentinel(c) {
			continue
		}
		if at, err := time.Parse(time.RFC3339, c.F("landed")); err == nil {
			landed = append(landed, at)
		}
	}
	r := WhereRecord{DecisionHolds: sprint.DefaultHeldDecisions(s), DecisionHoldsCounted: true, Epoch: s.Epoch, Rev: s.Work.Revision, Held: sprint.HeldBack(s), HeldStreams: sprint.HeldBackByStream(s), Sentinels: sentinelCounts(s), Critical: sprint.Critical(s, 5),
		Tiers: sprint.TierCounts(s), Streams: sprint.StreamTierCosts(s), StageTimes: sprint.CycleTimes(s, now), DealtFleet: sprint.FriendsDealtFleet(s),
		ReadsWaiting: sprint.ReadsWaiting(s), Priorities: sprint.PriorityCounts(s), StreamPriorities: sprint.StreamPriorities(s),
		ReadsWindow: sprint.ReadsWindowOf(s, m.StoppedBetween)}
	r.Blockers = sprint.IdleBlockers(s, 3)
	if s.Fleet != nil {
		r.FleetRev = s.Fleet.Revision
	}
	check := sprint.CostCheckOf(s, r.Streams)
	r.CostCheck = &check
	r.RowCards, r.ReadCards = sprint.RowCardCounts(s)
	r.FixStates = sprint.FixStateCounts(s)
	r.Series, r.SeriesCounted = sprint.SeriesLandings(s, now.Add(-sprint.SeriesWindow)), true
	r.RowWorking = sprint.RowWorkingKeys(s)
	for _, at := range sprint.RecentLandings(landed, m.Spans, m.FirstStart(s.Cleared), now) {
		r.Landings = append(r.Landings, at.Unix())
	}
	return r
}

// streamsSince is the streams' figures (sprint.StreamTierCosts over snap) as where shows
// them: the spend since the stats reset's mark (sprint.TierCostsSince: the total, the work,
// the reads, the decide calls and each tier, less the mark's; a stream the mark does not
// know reads as before), the sprint's unattributed spend counted from the mark on every
// stream's record (the figure is the sprint's, the same on each, so a stream the mark does
// not know too) and per landed since the last tidy of the streams or the reset, the later
// (sprint.PerLandedSince, over the landed cards' own cost counted before the mark).
func (st *Store) streamsSince(snap *sprint.Snapshot, streams map[string]sprint.TierCosts, stats StatsRecord) map[string]sprint.TierCosts {
	landedUSD := map[string]string{}
	for stream, tc := range streams {
		landedUSD[stream] = tc.LandedUSD
	}
	if m := stats.resetIn(st.epoch); m != nil {
		for stream, tc := range streams {
			// docs/SPEC-SPRINT.md section 11, Statistics, stats reset: the mark holds the
			// sprint's unattributed spend, and it counts from the mark
			tc.Unreconciled = sprint.MoneyLess(tc.Unreconciled, m.Unreconciled)
			if base, ok := m.Streams[stream]; ok {
				tc = sprint.TierCostsSince(tc, base)
			}
			streams[stream] = tc
		}
	}
	for stream, b := range stats.basesIn(st.epoch) {
		tc, ok := streams[stream]
		if !ok {
			continue
		}
		tc.PerLanded = sprint.PerLandedSince(landedUSD[stream], snap.Work.Count(stream, sprint.Landed), b)
		streams[stream] = tc
	}
	return streams
}

// costExtras is the tick's extras (tickExtras) and every primary off the work table whose
// cost its stream counts (sprint.CostOffIDs): what the cost view is counted from, by the
// where record's count and a stats reset's mark alike, so the two count the same cards. A
// card off the table is read by id once and kept by the twin after.
func costExtras(s *sprint.Snapshot) map[string][]string {
	out := tickExtras(s)
	if off := sprint.CostOffIDs(s); len(off) > 0 {
		out[sprint.Work] = append(slices.Clone(out[sprint.Work]), off...)
	}
	return out
}

// readWhere is the where record as stored; false when there is none or it
// does not read. It reads in any of the record's forms (S2) through c, the
// Store's reader, or the loose reader with none. A record that does not read
// is no record, on purpose: keepWhere counts it again and writes it over, and
// WhereFacts counts the cards as it does before the first record. The refusal
// is counted on the reader by its name (S4).
func readWhere(raw string, ok bool, c ...*RowCodec[WhereRecord]) (WhereRecord, bool) {
	r, ok, err := viewRead(viewCodec(viewRowsLoose.where, c), raw, ok)
	if err != nil {
		return WhereRecord{}, false
	}
	return r, ok
}

// keepWhere is the tick's count of the where record: nothing when the record
// is of this epoch at the work and fleet tables' revisions (an idle tick reads the
// shapes and the record, two exchanges); else the sprint from the twin, brought
// up to date, and the record written. A twin held by another step of this
// process, or a clear under the read, leaves it to the next tick.
//
// First, every tick, the card log index takes the log's new lines
// (keepLogIndex, load.go): the cursor read and the lines after it, two
// exchanges, and their entries written in one more when there are any.
func (st *Store) keepWhere(ctx context.Context, m Machine) error {
	kv, err := st.kv()
	if err != nil {
		return nil // a store that keeps no records: where counts the cards
	}
	if err := st.keepLogIndex(ctx); err != nil {
		return fmt.Errorf("the card log index: %w", err)
	}
	shapes, err := st.B.Shapes(ctx, []string{st.Names.Table(sprint.Work), st.Names.Table(sprint.Fleet)})
	if err != nil {
		return err
	}
	// the where record, the stats record (stats tidy's streams' bases) and the done record in
	// one exchange
	vals, oks, err := getKeys(ctx, kv, []string{keyWhere, keyStats, keyDoneRecord})
	if err != nil {
		return err
	}
	// the done record takes the log's new lines' completions (done.go): a write only when
	// there were any
	if err := st.keepDone(ctx, kv, vals[2], oks[2]); err != nil {
		return fmt.Errorf("the done record: %w", err)
	}
	stats := st.statsRecordOf(vals[1], oks[1]) // permissive: an unreadable one is no tidy
	stamp := stats.statsStamp(st.epoch)
	// a record from before the series, or the archived streams' figures, were counted is
	// counted again once, tables still or not
	if r, ok := readWhere(vals[0], oks[0], st.viewRows().where); ok && r.Epoch == st.epoch && r.Rev == shapes[0].Revision && r.FleetRev == shapes[1].Revision && r.Stats == stamp && r.SeriesCounted && r.ArchivedCounted && r.DecisionHoldsCounted {
		return nil
	}
	tw := st.twin()
	if !tw.mu.TryLock() {
		return nil
	}
	snap, _, err := st.twinRead(withBudget(ctx), tw, All, costExtras, nil)
	tw.mu.Unlock()
	if errors.Is(err, errCleared) {
		return nil
	}
	if err != nil {
		return err
	}
	now := st.now()
	r := whereOf(snap, m, now)
	r.Stats = stamp
	// the archived streams, off the work table's shape (the twin carries no row hide): what
	// where takes off the rows' done and ok% and the stage times (WhereRecord.ArchivedCounted)
	archived := ArchivedOfShape(shapes[0])
	r.ArchivedPrimaries, r.ArchivedFleet = sprint.ArchivedPrimaries(snap, archived), sprint.ArchivedFleetCards(snap, archived)
	r.LiveStageTimes, r.ArchivedCounted = sprint.CycleTimesExcept(snap, now, archived), true
	r.Streams = st.streamsSince(snap, r.Streams, stats)
	// the work table's cost cells, from the figures just counted (the tile's), the tick the
	// figure changes; a write that alone moved the table since the count (its revision one
	// past the count's) is the record's revision, so the record is not counted again for it
	costs := map[string]string{}
	for stream, tc := range r.Streams {
		costs[stream] = tc.TotalCost
	}
	wrote, err := st.showStreamCosts(ctx, shapes[0], costs)
	if err != nil {
		return fmt.Errorf("the work table's cost cells: %w", err)
	}
	if wrote && shapes[0].Revision == r.Rev {
		if after, err := st.B.Shapes(ctx, []string{st.Names.Table(sprint.Work)}); err == nil && len(after) == 1 && after[0].Revision == r.Rev+1 {
			r.Rev = after[0].Revision
		}
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return kv.SetKey(ctx, keyWhere, string(b))
}

// The store round trip (store-latency-row-r.w2, docs/SPEC-SPRINT.md section
// 14): the server times one round trip to the store every StoreRTTEvery by the
// injected clock and keeps the samples of the last StoreRTTWindow, with their
// p50 and p99, in one key, so where, another process, reads them in the
// exchange it makes anyway.

// StoreRTTEvery is how often the server times a store round trip, and
// StoreRTTWindow how long a sample is kept; a record whose last sample is older
// than the window is a server that stopped measuring, and where shows none.
const (
	StoreRTTEvery  = 10 * time.Second
	StoreRTTWindow = time.Minute
)

// keyStoreRTT is the store round trip record's key, under the deployment's prefix.
const keyStoreRTT = "store_rtt" // STRING, the store round trip record (JSON)

// StoreRTTRecord is the store round trip record: the samples of the last
// StoreRTTWindow (when each round trip began, Unix milliseconds, and how long it
// took, microseconds, so a round trip under a millisecond is not zero) and their
// p50 and p99 in milliseconds.
type StoreRTTRecord struct {
	At      int64      `json:"at"` // the last sample's start, Unix milliseconds
	Samples [][2]int64 `json:"samples"`
	P50MS   float64    `json:"store_rtt_p50_ms"`
	P99MS   float64    `json:"store_rtt_p99_ms"`
}

// MeasureStoreRTT times one round trip to the store by the injected clock (the
// read of the record itself) and records it from the time it began
// (RecordStoreRTT); it returns the round trip. A store without the KV records
// measures nothing.
func (st *Store) MeasureStoreRTT(ctx context.Context) (time.Duration, error) {
	kv, err := st.kv()
	if err != nil {
		return 0, err
	}
	t0 := st.now()
	v, ok, err := kv.GetKey(ctx, keyStoreRTT)
	d := st.now().Sub(t0)
	if err != nil {
		return d, err
	}
	return d, st.keepStoreRTT(ctx, kv, storeRTTOf(st.viewRows().rtt, v, ok), t0, d)
}

// RecordStoreRTT records one store round trip d that began at the store's now.
func (st *Store) RecordStoreRTT(ctx context.Context, d time.Duration) error {
	kv, err := st.kv()
	if err != nil {
		return err
	}
	v, ok, err := kv.GetKey(ctx, keyStoreRTT)
	if err != nil {
		return err
	}
	return st.keepStoreRTT(ctx, kv, storeRTTOf(st.viewRows().rtt, v, ok), st.now(), d)
}

// keepStoreRTT adds the sample (at, d) to r, drops the samples older than
// StoreRTTWindow before at, and writes r with its p50 and p99. The server is
// the one writer of the record.
func (st *Store) keepStoreRTT(ctx context.Context, kv KV, r StoreRTTRecord, at time.Time, d time.Duration) error {
	cut := at.Add(-StoreRTTWindow).UnixMilli()
	kept := [][2]int64{}
	for _, s := range r.Samples {
		if s[0] >= cut {
			kept = append(kept, s)
		}
	}
	kept = append(kept, [2]int64{at.UnixMilli(), d.Microseconds()})
	r = StoreRTTRecord{At: at.UnixMilli(), Samples: kept}
	r.P50MS, r.P99MS = rttQuantile(kept, 0.50), rttQuantile(kept, 0.99)
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return kv.SetKey(ctx, keyStoreRTT, string(b))
}

// storeRTTOf reads the store round trip record through c, in any of its forms (S2). One
// that does not read is an empty record, on purpose: the server is its one writer and
// writes it over with the next sample, and where shows no round trip until then. The
// refusal is counted on c by its name (S4).
func storeRTTOf(c *RowCodec[StoreRTTRecord], v string, ok bool) StoreRTTRecord {
	r, _, err := viewRead(c, v, ok)
	if err != nil {
		return StoreRTTRecord{}
	}
	return r
}

// rttQuantile is the nearest-rank q quantile of the samples' round trips, in
// milliseconds to the microsecond: the sorted sample at index floor(q*n), the
// last at most.
func rttQuantile(samples [][2]int64, q float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	us := make([]int64, len(samples))
	for i, s := range samples {
		us[i] = s[1]
	}
	slices.Sort(us)
	return float64(us[min(len(us)-1, int(q*float64(len(us))))]) / 1000
}

// WhereFacts is what where shows beside the tables' counts: the machine's
// records (Records: the store keeps them), the held cards, the landing
// stamps for the rate, and the store round trip the server measured.
type WhereFacts struct {
	DecisionHolds []sprint.Note
	Records       bool
	Machine       Machine
	Heartbeat     Heartbeat
	Held          int
	// HeldStreams is the record's per-stream held count (WhereRecord.HeldStreams).
	HeldStreams map[string]int
	Landed      []time.Time
	Critical    []sprint.CriticalCard // the five heaviest, from the record (weight.go)
	// Sentinels is the record's per-stream sentinel counts (WhereRecord.Sentinels),
	// so where counts no sentinel in x/y, the ETA and the landing rate; nil without
	// the record.
	Sentinels map[string]SentinelCounts
	// Tiers and Streams are the record's counts and costs by tier (cost_view.go); nil
	// without the record.
	Tiers   map[string]int
	Streams map[string]sprint.TierCosts
	// CostCheck is the record's (WhereRecord.CostCheck); nil without the record.
	CostCheck *sprint.CostCheck
	// StageTimes is the record's stage times (sprint.CycleTimes); empty without the record.
	StageTimes sprint.StageTimes
	// DealtFleet is the record's count of the fleet's cards on each friend's row
	// (sprint.FriendsDealtFleet); nil without the record.
	DealtFleet map[string]int
	// RowCards and ReadCards are the record's (sprint.RowCardCounts); nil and zero without it.
	RowCards  map[string]map[string]int
	ReadCards sprint.ReadCardCounts
	FixStates map[string]map[string]int
	// Blockers is the record's top blockers (WhereRecord.Blockers); nil without the record.
	Blockers []string
	// RowWorking is the record's working cards by fleet row (sprint.RowWorkingKeys), counted
	// at the fleet table's revision FleetRev; nil without the record, when where shows the
	// table's counts (as it does when the fleet moved since the count).
	RowWorking map[string][]string
	FleetRev   uint64
	// ReadsWaiting, Priorities and StreamPriorities are the record's (WhereRecord); zero
	// without the record.
	ReadsWaiting     int
	Priorities       map[string][]string
	StreamPriorities map[string]string
	// ReadsWindow is the record's verdicts window (sprint.ReadsWindowOf); zero without it.
	ReadsWindow sprint.ReadsWindowView
	// Reset is the stats reset's mark in the pinned epoch (stats_reset.go); nil with none.
	// The where record's spend is counted from it already (keepWhere); where counts the
	// fleet's done cells and the landed per card from it.
	Reset *sprint.ResetMark
	// HasStoreRTT is set when the server's store round trip record has a sample
	// within StoreRTTWindow; StoreRTTP50MS and StoreRTTP99MS are its p50 and p99.
	HasStoreRTT   bool
	StoreRTTP50MS float64
	StoreRTTP99MS float64
	// Stops is the stops record the tick last wrote (stops.go), when HasStops: every
	// automatic stop that holds and what waits on the seat, as of its At.
	Stops    StopsRecord
	HasStops bool
	// Series is the record's landings for the landed series (WhereRecord.Series) when
	// SeriesCounted: a record of the pinned epoch counted them. Without it where --json's
	// writer reads the log for the series (sprint.LandedSeriesFrom).
	Series        []sprint.Landing
	SeriesCounted bool
	// Done is the epoch's done record (done.go), read in the same exchange; its Cursor is
	// empty when there is none of the epoch yet (DoneOf then reads the log).
	Done DoneRecord
	// ArchivedPrimaries, ArchivedFleet and LiveStageTimes are the record's (WhereRecord) when
	// ArchivedCounted: what where takes off the rows' done and ok% and the stage times.
	ArchivedPrimaries []string
	ArchivedFleet     []sprint.ArchivedFleetCard
	LiveStageTimes    sprint.StageTimes
	ArchivedCounted   bool
}

// StoreLine is where's store line, "store: rtt p50=<ms>ms p99=<ms>ms", or
// empty when the server has measured no round trip within the window.
func (f WhereFacts) StoreLine() string {
	if !f.HasStoreRTT {
		return ""
	}
	return fmt.Sprintf("store: rtt p50=%gms p99=%gms", f.StoreRTTP50MS, f.StoreRTTP99MS)
}

// WhereFacts reads the machine's records and the where record in one exchange.
// The record is taken when it is of the pinned epoch and either counted at
// workRev, the work table's revision the caller read, or kept by the RUNNING
// machine's loop: its last tick recent (MachineSilence) and not failed, and the
// record counted at or after the revision that tick saw (the heartbeat's), so
// the record is at most the tick in flight behind. A loop that ticks and does
// not keep the record (a binary from before it, a tick whose count was passed
// over) has its heartbeat move past the record, and the record is not taken.
// Otherwise (no record of this epoch: before the first tick, or after a clear;
// a STOPPED machine whose table a verb moved since its last tick; no loop
// keeping it) the held cards and the stamps are read from the cards (HeldBack,
// LandedAt), as before the record; a read of the stamps that fails leaves none,
// and the rate is the whole sprint's average, never a failed view.
func (st *Store) WhereFacts(ctx context.Context, workRev uint64) (WhereFacts, error) {
	var f WhereFacts
	if kv, err := st.kv(); err == nil {
		vals, oks, err := getKeys(ctx, kv, []string{keyMachine, keyHeartbeat, keyWhere, keyStoreRTT, keyStats, keyStops, keyDoneRecord})
		if err != nil {
			return f, err
		}
		f.Records = true
		f.Done, _, _, _ = st.readDoneFrom(ctx, kv, vals[6], oks[6])
		rows := st.viewRows()
		// the stats reset's mark, in the same exchange: an unreadable stats record is none
		// here, as it is no tidy (the tick says it, statsRecordOf); its refusal is counted
		// on the stats reader by name (S2, S4)
		stats, _, serr := viewRead(rows.stats, vals[4], oks[4])
		if serr != nil {
			stats = StatsRecord{}
		}
		f.Reset = stats.resetIn(st.epoch)
		// the machine's records: one that does not read is the named refusal (S2)
		if f.Machine, _, err = viewRead(rows.machine, vals[0], oks[0]); err != nil {
			return f, err
		}
		if f.Heartbeat, _, err = viewRead(rows.heartbeat, vals[1], oks[1]); err != nil {
			return f, err
		}
		if !f.Machine.Running() {
			// READY is read off the record at the epoch these facts are of (Machine.Ready;
			// docs/SPEC-SPRINT.md section 14, "READY")
			es := EpochState{N: st.epoch, Cleared: st.cleared}
			if !st.pinned {
				if es, err = st.EpochNow(ctx); err != nil {
					return f, err
				}
			}
			f.Machine = f.Machine.readyAt(es)
		}
		if r := storeRTTOf(rows.rtt, vals[3], oks[3]); len(r.Samples) > 0 && st.now().Sub(time.UnixMilli(r.At)) <= StoreRTTWindow {
			f.HasStoreRTT, f.StoreRTTP50MS, f.StoreRTTP99MS = true, r.P50MS, r.P99MS
		}
		if r, ok := stopsOfRaw(rows.stops, vals[5], oks[5]); ok && r.Epoch == st.epoch {
			f.Stops, f.HasStops = r, true
		}
		r, ok := readWhere(vals[2], oks[2], rows.where)
		// the landed series' landings are taken from any record of the pinned epoch that
		// counted them, as of the tick's last count (WhereRecord.Series): the series is a
		// day of landings in 10-minute buckets, never the cards' counts
		if ok && r.Epoch == st.epoch && r.SeriesCounted {
			f.Series, f.SeriesCounted = r.Series, true
		}
		// a record counted from another stats record (a reset or a tidy since) is not taken
		if ok && r.DecisionHoldsCounted && r.Epoch == st.epoch && r.Stats == stats.statsStamp(st.epoch) && (r.Rev == workRev || st.keptBy(r, f.Machine, f.Heartbeat)) {
			f.Held, f.HeldStreams, f.Critical, f.Tiers, f.Streams, f.StageTimes, f.DealtFleet = r.Held, r.HeldStreams, r.Critical, r.Tiers, r.Streams, r.StageTimes, r.DealtFleet
			f.Sentinels = r.Sentinels
			f.DecisionHolds = r.DecisionHolds
			f.CostCheck = r.CostCheck
			f.ReadsWaiting, f.Priorities, f.StreamPriorities = r.ReadsWaiting, r.Priorities, r.StreamPriorities
			f.ReadsWindow = r.ReadsWindow
			f.RowCards, f.ReadCards = r.RowCards, r.ReadCards
			f.FixStates = r.FixStates
			f.Blockers = r.Blockers
			f.RowWorking, f.FleetRev = r.RowWorking, r.FleetRev
			f.ArchivedPrimaries, f.ArchivedFleet, f.LiveStageTimes, f.ArchivedCounted = r.ArchivedPrimaries, r.ArchivedFleet, r.LiveStageTimes, r.ArchivedCounted
			for _, s := range r.Landings {
				f.Landed = append(f.Landed, time.Unix(s, 0).UTC())
			}
			return f, nil
		}
	}
	var err error
	// the held cards, total and per stream, from one read of the waiting
	// column (sprint.HeldBack, sprint.HeldBackByStream).
	if t, herr := st.workColumn(ctx, sprint.Waiting); herr != nil {
		return f, herr
	} else if t != nil {
		snap := &sprint.Snapshot{Work: t}
		f.Held, f.HeldStreams = sprint.HeldBack(snap), sprint.HeldBackByStream(snap)
		f.DecisionHolds = sprint.DefaultHeldDecisions(snap)
	}
	// the streams' spend from the cards, as the tick counts it (keepWhere), so the cost
	// column and the tile show the cards as they are when no record of them is there yet
	if f.Records {
		if err := st.whereCosts(ctx, &f); err != nil {
			return f, err
		}
	}
	if f.Landed, err = st.LandedAt(ctx); err != nil {
		f.Landed = nil // the whole sprint's average (sprint.LandingRate)
	}
	return f, nil
}

// whereCosts counts the streams' figures and the cost check from the cards (the read the
// tick's count makes: every table and the cards off the table its streams name), into f,
// for a where that found no record to take; a clear under the read leaves them out.
func (st *Store) whereCosts(ctx context.Context, f *WhereFacts) error {
	kv, err := st.kv()
	if err != nil {
		return nil
	}
	s, err := st.Load(ctx, All, costExtras)
	if errors.Is(err, errCleared) {
		return nil
	}
	if err != nil {
		return err
	}
	raw, had, err := kv.GetKey(ctx, keyStats)
	if err != nil {
		return err
	}
	// an unreadable stats record is none here, as it is no tidy (statsRecordOf); its refusal
	// is counted on the stats reader by name (S2, S4)
	stats, _, err := viewRead(st.viewRows().stats, raw, had)
	if err != nil {
		stats = StatsRecord{}
	}
	streams := sprint.StreamTierCosts(s)
	check := sprint.CostCheckOf(s, streams)
	f.Streams, f.CostCheck = st.streamsSince(s, streams, stats), &check
	f.Sentinels = sentinelCounts(s)
	f.Blockers = sprint.IdleBlockers(s, 3)
	return nil
}

// keptBy says the RUNNING machine's loop keeps the record: it ticks (its last
// tick within MachineSilence, not failed) and the record is counted at or
// after the work table's revision its last tick saw.
func (st *Store) keptBy(r WhereRecord, m Machine, hb Heartbeat) bool {
	return !st.ByHand && m.Running() && hb.Error == "" && st.now().Sub(hb.Alive()) <= MachineSilence && r.Rev >= hb.Revisions[0]
}

// ---------------------------------------------------------------------------
// The view rows' strict read (S1, S2, S4; schemas/viewrows/viewrows.schema).
//
// The where, store round trip, stats, machine and heartbeat records (this file), the stops
// record (stops.go), the check record (check_record.go) and the stuck record (stuck.go) are
// variable tables of viewrowswire (S1: each holds text or a list no path bounds). Every read
// of them goes through its RowCodec (rowcodec.go), which takes the table's binary wire (form
// byte 1), padded standard base64 of that wire, the schema JSON text and the legacy JSON
// every writer still writes, and reads each to one equal value (S2). A damaged value is
// `<RECORD> REFUSED: <reason>; run: nova-sprint check`, and every read is counted on its
// record and its form (S4: the evidence a later card reads before the writers switch).
// The Go value and the schema text are mapped by the generated descriptors
// (viewrowswire.UnitView): a Go field is the schema field with its legacy JSON key, a map is a
// list of pairs in key order, a list of texts is a list of one-text tables, a time is Unix
// nanoseconds UTC with 0 for the zero time, and a Go array is the fixed table of as many
// fields, in order (the schema's own header).

// viewWireForm is the variable table's form byte (S1, S2).
const viewWireForm byte = 1

// viewOps is one record's generated variable-table functions (S1, S2).
type viewOps[W any] struct {
	table       string // the schema table's name, its descriptor in viewrowswire.UnitView
	fromJSON    func([]byte, *viewrowswire.TableReport) (*W, *viewrowswire.TableArena, bool)
	measure     func(*W, ...viewrowswire.TableWriteContext) int64
	save        func(*W, []byte, ...viewrowswire.TableWriteContext) int64
	loadMeasure func([]byte) (int64, error)
	load        func(region, wire []byte, rep *viewrowswire.TableReport) *W
	jsonMeasure func(*W, ...viewrowswire.TableAllocator) int64
	toJSON      func(*W, []byte, ...viewrowswire.TableAllocator) int64
}

// viewBuilder adapts a generated builder to viewOps.fromJSON.
func viewBuilder[B any, W any](init func(*B, ...viewrowswire.TableAllocator) bool, root func(*B) *W, arena func(*B) *viewrowswire.TableArena, from func(*B, []byte, *viewrowswire.TableReport) bool) func([]byte, *viewrowswire.TableReport) (*W, *viewrowswire.TableArena, bool) {
	return func(text []byte, rep *viewrowswire.TableReport) (*W, *viewrowswire.TableArena, bool) {
		b := new(B)
		if !init(b) {
			return nil, nil, false
		}
		ok := from(b, text, rep)
		return root(b), arena(b), ok
	}
}

// viewReport is a generated table report as the row codec decides on it; a load that
// returned nothing is malformed when it names nothing else (S2).
func viewReport(rep viewrowswire.TableReport, ok bool) RowReport {
	return RowReport{Unknown: int(rep.Unknown), KindMismatch: int(rep.KindMismatch), Clamped: int(rep.Clamped),
		Malformed: rep.Malformed || !ok, Duplicate: int(rep.Duplicate), Reason: rep.Reason}
}

// viewRefused says the strict read refuses r (rowcodec.go RowReport.schema, S2).
func viewRefused(r RowReport) bool {
	return r.Reason != "" || r.Malformed || r.Duplicate > 0 || r.KindMismatch > 0 || r.Clamped > 0
}

// viewMalformed is the refusal of a wire or text the generated functions could not size or
// write (S2).
var viewMalformed = RowReport{Malformed: true, Reason: schemaread.ReasonMalformed}

// text is the schema JSON text of one wire, or the refusal its load reports (S2).
func (o viewOps[W]) text(wire []byte) ([]byte, RowReport) {
	n, err := o.loadMeasure(wire)
	if err != nil {
		return nil, RowReport{Reason: err.Error()}
	}
	var rep viewrowswire.TableReport
	out := o.load(make([]byte, n), wire, &rep)
	if rr := viewReport(rep, out != nil); viewRefused(rr) {
		return nil, rr
	}
	size := o.jsonMeasure(out)
	if size < 0 {
		return nil, viewMalformed
	}
	buf := make([]byte, size)
	m := o.toJSON(out, buf)
	if m < 0 {
		return nil, viewMalformed
	}
	return buf[:m], viewReport(rep, true)
}

// wire is the binary wire of one schema JSON text, or the refusal its reader reports (S2).
func (o viewOps[W]) wire(text []byte) ([]byte, RowReport) {
	var rep viewrowswire.TableReport
	root, arena, ok := o.fromJSON(text, &rep)
	if rr := viewReport(rep, ok && root != nil); viewRefused(rr) {
		return nil, rr
	}
	size := o.measure(root, arena)
	if size < 0 {
		return nil, viewMalformed
	}
	buf := make([]byte, size)
	n := o.save(root, buf, arena)
	if n < 0 {
		return nil, viewMalformed
	}
	return buf[:n], viewReport(rep, true)
}

// viewRowFns is one record's row functions: the generated variable table for the binary wire,
// its base64 and the schema JSON text, and encoding/json for the legacy text every writer
// writes today (S1, S2, S4). Legacy text is told from the schema text as rowcodec.go's
// LegacyObject tells it: the generated writer ends its text with a newline.
func viewRowFns[T any, W any](name string, o viewOps[W]) RowFns[T] {
	info := viewTable(o.table)
	load := func(v *T, wire []byte) RowReport {
		text, rr := o.text(wire)
		if viewRefused(rr) {
			return rr
		}
		if err := viewFromText(text, info, v); err != nil {
			return viewMapReport(err)
		}
		return rr
	}
	save := func(v *T) []byte {
		text, err := viewToText(v, info)
		if err != nil {
			return nil
		}
		wire, rr := o.wire(text)
		if viewRefused(rr) {
			return nil
		}
		return wire
	}
	return RowFns[T]{
		Name: name, Form: viewWireForm,
		Reset:    func(v *T) { var zero T; *v = zero },
		LoadWire: load,
		LoadJSON: func(v *T, text []byte) RowReport {
			wire, rr := o.wire(text)
			if viewRefused(rr) {
				return rr
			}
			lr := load(v, wire)
			if !viewRefused(lr) {
				lr.Unknown = rr.Unknown // the wire written from the text holds no unknown field
			}
			return lr
		},
		Legacy:     LegacyObject,
		LoadLegacy: DecodeLegacy[T],
		SaveWire:   save,
		SaveJSON: func(v *T) []byte {
			text, rr := o.text(save(v))
			if viewRefused(rr) {
				return nil
			}
			return text
		},
	}
}

// viewRowCodecs is one Store's view-row readers (S4). A Store and every copy pinned from it
// share them, as they share the done reader (stats.go clone), so the counts are one per
// sprint store and not one per pin.
type viewRowCodecs struct {
	where     *RowCodec[WhereRecord]
	rtt       *RowCodec[StoreRTTRecord]
	stats     *RowCodec[StatsRecord]
	machine   *RowCodec[Machine]
	heartbeat *RowCodec[Heartbeat]
	stops     *RowCodec[StopsRecord]
	check     *RowCodec[CheckRecord]
	stuck     *RowCodec[Stuck]
}

// The generated functions of each view row read here (S1).
var (
	whereOps = viewOps[viewrowswire.WhereRecord]{"WhereRecord",
		viewBuilder((*viewrowswire.WhereRecordBuilder).Init, (*viewrowswire.WhereRecordBuilder).GetRoot, func(b *viewrowswire.WhereRecordBuilder) *viewrowswire.TableArena { return &b.Arena }, viewrowswire.WhereRecordFromJson),
		viewrowswire.WhereRecordMeasure, viewrowswire.WhereRecordSave, viewrowswire.WhereRecordLoadMeasureReason, viewrowswire.WhereRecordLoad, viewrowswire.WhereRecordToJsonMeasure, viewrowswire.WhereRecordToJson}
	rttOps = viewOps[viewrowswire.StoreRTTRecord]{"StoreRTTRecord",
		viewBuilder((*viewrowswire.StoreRTTRecordBuilder).Init, (*viewrowswire.StoreRTTRecordBuilder).GetRoot, func(b *viewrowswire.StoreRTTRecordBuilder) *viewrowswire.TableArena { return &b.Arena }, viewrowswire.StoreRTTRecordFromJson),
		viewrowswire.StoreRTTRecordMeasure, viewrowswire.StoreRTTRecordSave, viewrowswire.StoreRTTRecordLoadMeasureReason, viewrowswire.StoreRTTRecordLoad, viewrowswire.StoreRTTRecordToJsonMeasure, viewrowswire.StoreRTTRecordToJson}
	statsOps = viewOps[viewrowswire.StatsRecord]{"StatsRecord",
		viewBuilder((*viewrowswire.StatsRecordBuilder).Init, (*viewrowswire.StatsRecordBuilder).GetRoot, func(b *viewrowswire.StatsRecordBuilder) *viewrowswire.TableArena { return &b.Arena }, viewrowswire.StatsRecordFromJson),
		viewrowswire.StatsRecordMeasure, viewrowswire.StatsRecordSave, viewrowswire.StatsRecordLoadMeasureReason, viewrowswire.StatsRecordLoad, viewrowswire.StatsRecordToJsonMeasure, viewrowswire.StatsRecordToJson}
	machineOps = viewOps[viewrowswire.Machine]{"Machine",
		viewBuilder((*viewrowswire.MachineBuilder).Init, (*viewrowswire.MachineBuilder).GetRoot, func(b *viewrowswire.MachineBuilder) *viewrowswire.TableArena { return &b.Arena }, viewrowswire.MachineFromJson),
		viewrowswire.MachineMeasure, viewrowswire.MachineSave, viewrowswire.MachineLoadMeasureReason, viewrowswire.MachineLoad, viewrowswire.MachineToJsonMeasure, viewrowswire.MachineToJson}
	heartbeatOps = viewOps[viewrowswire.Heartbeat]{"Heartbeat",
		viewBuilder((*viewrowswire.HeartbeatBuilder).Init, (*viewrowswire.HeartbeatBuilder).GetRoot, func(b *viewrowswire.HeartbeatBuilder) *viewrowswire.TableArena { return &b.Arena }, viewrowswire.HeartbeatFromJson),
		viewrowswire.HeartbeatMeasure, viewrowswire.HeartbeatSave, viewrowswire.HeartbeatLoadMeasureReason, viewrowswire.HeartbeatLoad, viewrowswire.HeartbeatToJsonMeasure, viewrowswire.HeartbeatToJson}
)

// newViewRowCodecs makes one set of the view-row readers (S1, S2, S4).
func newViewRowCodecs() *viewRowCodecs {
	return &viewRowCodecs{
		where:     NewRowCodec(viewRowFns[WhereRecord]("WHERERECORD", whereOps)),
		rtt:       NewRowCodec(viewRowFns[StoreRTTRecord]("STORERTTRECORD", rttOps)),
		stats:     NewRowCodec(viewRowFns[StatsRecord]("STATSRECORD", statsOps)),
		machine:   NewRowCodec(viewRowFns[Machine]("MACHINE", machineOps)),
		heartbeat: NewRowCodec(viewRowFns[Heartbeat]("HEARTBEAT", heartbeatOps)),
		stops:     NewRowCodec(viewRowFns[StopsRecord]("STOPSRECORD", stopsOps)),
		check:     NewRowCodec(viewRowFns[CheckRecord]("CHECKRECORD", checkOps)),
		stuck:     NewRowCodec(viewRowFns[Stuck]("STUCK", stuckOps)),
	}
}

// register counts every view-row reader on st's CheckReport (S4).
func (b *viewRowCodecs) register(st *Store) {
	b.where.Register(st)
	b.rtt.Register(st)
	b.stats.Register(st)
	b.machine.Register(st)
	b.heartbeat.Register(st)
	b.stops.Register(st)
	b.check.Register(st)
	b.stuck.Register(st)
}

var (
	viewRowsMu sync.Mutex
	// viewRowsBy is each store's readers, keyed by the done reader the store and its pinned
	// copies share (stats.go clone), so a pin reads with its store's counters (S4).
	viewRowsBy = map[*RowCodec[donePage]]*viewRowCodecs{}
	// viewRowsLoose is the readers of the reads made with no Store (readWhere,
	// CheckRecordOf, stuckOf): counted, on no store's report (S4).
	viewRowsLoose = newViewRowCodecs()
)

// viewRows is this Store's view-row readers, registered on it (S4); a nil Store reads with
// the loose readers.
func (st *Store) viewRows() *viewRowCodecs {
	if st == nil {
		return viewRowsLoose
	}
	key := st.doneRows()
	viewRowsMu.Lock()
	b := viewRowsBy[key]
	if b == nil {
		b = newViewRowCodecs()
		viewRowsBy[key] = b
	}
	viewRowsMu.Unlock()
	b.register(st)
	return b
}

// viewCodec is the one reader of c, or loose with none (S4).
func viewCodec[T any](loose *RowCodec[T], c []*RowCodec[T]) *RowCodec[T] {
	if len(c) > 0 && c[0] != nil {
		return c[0]
	}
	return loose
}

// viewRead reads one stored value through c: absent is no record and no error; a value that
// does not read is the named refusal, counted on c (S2, S4).
func viewRead[T any](c *RowCodec[T], raw string, ok bool) (T, bool, error) {
	var zero T
	if !ok {
		return zero, false, nil
	}
	v, _, err := c.Read([]byte(raw))
	if err != nil {
		return zero, false, err
	}
	return v, true, nil
}

// ---------------------------------------------------------------------------
// The Go record and the schema text, mapped by the generated descriptors (S2).

var (
	viewTablesOnce sync.Once
	viewTables     map[string]*viewrowswire.TableTypeInfo
	viewTimeType   = reflect.TypeOf(time.Time{})
)

// viewTable is the generated descriptor of one schema table; nil for none.
func viewTable(name string) *viewrowswire.TableTypeInfo {
	viewTablesOnce.Do(func() {
		viewTables = map[string]*viewrowswire.TableTypeInfo{}
		for _, t := range viewrowswire.UnitView().Tables {
			viewTables[t.Name] = t.Type
		}
	})
	return viewTables[name]
}

// viewMapError is a value the Go record and the schema text cannot both hold: its reason is
// the refusal's (S2).
type viewMapError struct {
	reason, at string
}

func (e *viewMapError) Error() string { return e.reason + ": " + e.at }

func viewMapErr(reason, format string, args ...any) error {
	return &viewMapError{reason: reason, at: fmt.Sprintf(format, args...)}
}

// viewMapReport is the refusal of a mapping error: its own reason, else malformed (S2).
func viewMapReport(err error) RowReport {
	var me *viewMapError
	if errors.As(err, &me) && me.reason == schemaread.ReasonClamped {
		return RowReport{Clamped: 1, Reason: schemaread.ReasonClamped}
	}
	return viewMalformed
}

// viewGoField is one Go field: where it is in its struct.
type viewGoField struct {
	index []int
}

// viewGoFieldsOf is viewGoFields by type, made once per type.
var viewGoFieldsOf sync.Map // reflect.Type -> map[string]viewGoField

// viewGoFields is a Go struct's exported fields by their legacy JSON key, embedded structs
// flattened as encoding/json flattens them.
func viewGoFields(t reflect.Type) map[string]viewGoField {
	if v, ok := viewGoFieldsOf.Load(t); ok {
		return v.(map[string]viewGoField)
	}
	out := map[string]viewGoField{}
	var walk func(t reflect.Type, at []int)
	walk = func(t reflect.Type, at []int) {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			idx := append(slices.Clone(at), i)
			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name, _, _ := strings.Cut(tag, ",")
			if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
				walk(f.Type, idx)
				continue
			}
			if !f.IsExported() {
				continue
			}
			if name == "" {
				name = f.Name
			}
			out[name] = viewGoField{index: idx}
		}
	}
	walk(t, nil)
	viewGoFieldsOf.Store(t, out)
	return out
}

// viewToText is the schema JSON text of the Go record *v (S2).
func viewToText[T any](v *T, info *viewrowswire.TableTypeInfo) ([]byte, error) {
	if info == nil {
		return nil, viewMapErr(schemaread.ReasonMalformed, "no table")
	}
	obj, err := viewObjTo(reflect.ValueOf(v).Elem(), info)
	if err != nil {
		return nil, err
	}
	return json.Marshal(obj)
}

// viewObjTo is one Go struct as the schema table's object; every Go field has its schema
// field and every schema field its Go field, so nothing is dropped unseen.
func viewObjTo(gv reflect.Value, info *viewrowswire.TableTypeInfo) (map[string]any, error) {
	fields := viewGoFields(gv.Type())
	if len(fields) != len(info.Fields) {
		return nil, viewMapErr(schemaread.ReasonMalformed, "%s: %d Go fields, %d schema fields", info.Name, len(fields), len(info.Fields))
	}
	out := make(map[string]any, len(info.Fields))
	for i := range info.Fields {
		f := &info.Fields[i]
		gf, ok := fields[f.Json]
		if !ok {
			return nil, viewMapErr(schemaread.ReasonMalformed, "%s.%s: no Go field", info.Name, f.Json)
		}
		x, present, err := viewValueTo(gv.FieldByIndex(gf.index), f.TypeName, f.IsArray)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", info.Name, f.Json, err)
		}
		if present {
			out[f.Json] = x
		}
	}
	return out, nil
}

// viewValueTo is one Go value as the schema value of type typ (a list of typ when list);
// present is false for a nil pointer and an empty map or slice, which the text leaves out.
func viewValueTo(gv reflect.Value, typ string, list bool) (any, bool, error) {
	if list {
		switch gv.Kind() {
		case reflect.Map:
			if gv.Len() == 0 {
				return nil, false, nil
			}
			pair := viewTable(typ)
			if pair == nil || len(pair.Fields) != 2 || gv.Type().Key().Kind() != reflect.String {
				return nil, false, viewMapErr(schemaread.ReasonMalformed, "a map into %s", typ)
			}
			keys := gv.MapKeys()
			slices.SortFunc(keys, func(a, b reflect.Value) int { return strings.Compare(a.String(), b.String()) })
			arr := make([]any, 0, len(keys))
			for _, k := range keys {
				vf := &pair.Fields[1]
				x, present, err := viewValueTo(gv.MapIndex(k), vf.TypeName, vf.IsArray)
				if err != nil {
					return nil, false, err
				}
				e := map[string]any{pair.Fields[0].Json: k.String()}
				if present {
					e[vf.Json] = x
				}
				arr = append(arr, e)
			}
			return arr, true, nil
		case reflect.Slice, reflect.Array:
			if gv.Kind() == reflect.Slice && gv.Len() == 0 {
				return nil, false, nil
			}
			arr := make([]any, gv.Len())
			for i := range arr {
				x, present, err := viewValueTo(gv.Index(i), typ, false)
				if err != nil {
					return nil, false, err
				}
				if !present {
					return nil, false, viewMapErr(schemaread.ReasonMalformed, "an absent element of %s", typ)
				}
				arr[i] = x
			}
			return arr, true, nil
		}
		return nil, false, viewMapErr(schemaread.ReasonMalformed, "a Go %s into a list of %s", gv.Type(), typ)
	}
	switch typ {
	case "int8", "int16", "int32", "int64":
		switch {
		case gv.Type() == viewTimeType:
			return json.Number(strconv.FormatInt(viewNanos(gv.Interface().(time.Time)), 10)), true, nil
		case gv.CanInt():
			return json.Number(strconv.FormatInt(gv.Int(), 10)), true, nil
		case gv.CanUint() && gv.Uint() <= math.MaxInt64:
			return json.Number(strconv.FormatInt(int64(gv.Uint()), 10)), true, nil
		}
	case "uint8", "uint16", "uint32", "uint64":
		switch {
		case gv.CanUint():
			return json.Number(strconv.FormatUint(gv.Uint(), 10)), true, nil
		case gv.CanInt() && gv.Int() >= 0:
			return json.Number(strconv.FormatUint(uint64(gv.Int()), 10)), true, nil
		}
	case "float32", "float64":
		if gv.CanFloat() && !math.IsNaN(gv.Float()) && !math.IsInf(gv.Float(), 0) {
			return json.Number(strconv.FormatFloat(gv.Float(), 'g', -1, 64)), true, nil
		}
	case "bool":
		if gv.Kind() == reflect.Bool {
			return gv.Bool(), true, nil
		}
	case "string":
		if gv.Kind() == reflect.String {
			return gv.String(), true, nil
		}
	default:
		info := viewTable(typ)
		switch {
		case info == nil:
		case gv.Kind() == reflect.Pointer:
			if gv.IsNil() {
				return nil, false, nil
			}
			return viewValueTo(gv.Elem(), typ, false)
		case gv.Kind() == reflect.Struct && gv.Type() != viewTimeType:
			obj, err := viewObjTo(gv, info)
			return obj, err == nil, err
		case gv.Kind() == reflect.Array && gv.Len() == len(info.Fields):
			obj := map[string]any{}
			for i := range info.Fields {
				f := &info.Fields[i]
				x, present, err := viewValueTo(gv.Index(i), f.TypeName, f.IsArray)
				if err != nil {
					return nil, false, err
				}
				if present {
					obj[f.Json] = x
				}
			}
			return obj, true, nil
		case len(info.Fields) == 1:
			f := &info.Fields[0]
			x, present, err := viewValueTo(gv, f.TypeName, f.IsArray)
			if err != nil {
				return nil, false, err
			}
			obj := map[string]any{}
			if present {
				obj[f.Json] = x
			}
			return obj, true, nil
		}
	}
	return nil, false, viewMapErr(schemaread.ReasonMalformed, "a Go %s into %s", gv.Type(), typ)
}

// viewNanos is a stored time as Unix nanoseconds UTC, 0 for the zero time (the schema's
// header).
func viewNanos(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

// viewTime is viewNanos read back: 0 is the zero time.
func viewTime(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

// viewFromText reads one schema JSON text, as the generated writer wrote it, into *v (S2).
func viewFromText[T any](text []byte, info *viewrowswire.TableTypeInfo, v *T) error {
	if info == nil {
		return viewMapErr(schemaread.ReasonMalformed, "no table")
	}
	dec := json.NewDecoder(bytes.NewReader(text))
	dec.UseNumber()
	var x any
	if err := dec.Decode(&x); err != nil {
		return viewMapErr(schemaread.ReasonMalformed, "%s: %v", info.Name, err)
	}
	obj, ok := x.(map[string]any)
	if !ok {
		return viewMapErr(schemaread.ReasonMalformed, "%s: not an object", info.Name)
	}
	return viewObjFrom(obj, reflect.ValueOf(v).Elem(), info)
}

// viewObjFrom fills one Go struct from the schema table's object. An empty list or map is
// absent from the text and reads as nil: the variable table holds no presence for it.
func viewObjFrom(obj map[string]any, gv reflect.Value, info *viewrowswire.TableTypeInfo) error {
	fields := viewGoFields(gv.Type())
	if len(fields) != len(info.Fields) {
		return viewMapErr(schemaread.ReasonMalformed, "%s: %d Go fields, %d schema fields", info.Name, len(fields), len(info.Fields))
	}
	for i := range info.Fields {
		f := &info.Fields[i]
		gf, ok := fields[f.Json]
		if !ok {
			return viewMapErr(schemaread.ReasonMalformed, "%s.%s: no Go field", info.Name, f.Json)
		}
		fv := gv.FieldByIndex(gf.index)
		if x, has := obj[f.Json]; has && x != nil {
			if err := viewValueFrom(x, fv, f.TypeName, f.IsArray); err != nil {
				return fmt.Errorf("%s.%s: %w", info.Name, f.Json, err)
			}
		}
	}
	return nil
}

// viewValueFrom sets one Go value from the schema value x of type typ (a list of typ when
// list). A number past the Go value's range, or a list past a Go array's length, is clamped;
// a key twice in a map's pairs is malformed, never last-wins (rowcodec.go RowReport).
func viewValueFrom(x any, gv reflect.Value, typ string, list bool) error {
	if list {
		arr, ok := x.([]any)
		if !ok {
			return viewMapErr(schemaread.ReasonMalformed, "not a list of %s", typ)
		}
		switch gv.Kind() {
		case reflect.Map:
			pair := viewTable(typ)
			if pair == nil || len(pair.Fields) != 2 || gv.Type().Key().Kind() != reflect.String {
				return viewMapErr(schemaread.ReasonMalformed, "%s into a map", typ)
			}
			if len(arr) == 0 {
				return nil
			}
			m := reflect.MakeMapWithSize(gv.Type(), len(arr))
			for _, e := range arr {
				eo, ok := e.(map[string]any)
				if !ok {
					return viewMapErr(schemaread.ReasonMalformed, "a %s that is not an object", typ)
				}
				var key string
				if k, has := eo[pair.Fields[0].Json]; has && k != nil {
					if key, ok = k.(string); !ok {
						return viewMapErr(schemaread.ReasonMalformed, "a %s key that is not text", typ)
					}
				}
				kv := reflect.ValueOf(key).Convert(gv.Type().Key())
				if m.MapIndex(kv).IsValid() {
					return viewMapErr(schemaread.ReasonMalformed, "the %s key %q twice", typ, key)
				}
				val := reflect.New(gv.Type().Elem()).Elem()
				vf := &pair.Fields[1]
				if y, has := eo[vf.Json]; has && y != nil {
					if err := viewValueFrom(y, val, vf.TypeName, vf.IsArray); err != nil {
						return err
					}
				}
				m.SetMapIndex(kv, val)
			}
			gv.Set(m)
			return nil
		case reflect.Slice:
			if len(arr) == 0 {
				return nil
			}
			s := reflect.MakeSlice(gv.Type(), len(arr), len(arr))
			for i, e := range arr {
				if err := viewValueFrom(e, s.Index(i), typ, false); err != nil {
					return err
				}
			}
			gv.Set(s)
			return nil
		case reflect.Array:
			if len(arr) != gv.Len() {
				return viewMapErr(schemaread.ReasonClamped, "%d of %s into %d", len(arr), typ, gv.Len())
			}
			for i, e := range arr {
				if err := viewValueFrom(e, gv.Index(i), typ, false); err != nil {
					return err
				}
			}
			return nil
		}
		return viewMapErr(schemaread.ReasonMalformed, "a list of %s into a Go %s", typ, gv.Type())
	}
	switch typ {
	case "int8", "int16", "int32", "int64":
		n, ok := x.(json.Number)
		if !ok {
			break
		}
		i, err := n.Int64()
		if err != nil {
			return viewMapErr(schemaread.ReasonClamped, "%s %s", typ, n)
		}
		switch {
		case gv.Type() == viewTimeType:
			gv.Set(reflect.ValueOf(viewTime(i)))
			return nil
		case gv.CanInt():
			if gv.OverflowInt(i) {
				return viewMapErr(schemaread.ReasonClamped, "%d into a Go %s", i, gv.Type())
			}
			gv.SetInt(i)
			return nil
		case gv.CanUint():
			if i < 0 || gv.OverflowUint(uint64(i)) {
				return viewMapErr(schemaread.ReasonClamped, "%d into a Go %s", i, gv.Type())
			}
			gv.SetUint(uint64(i))
			return nil
		}
	case "uint8", "uint16", "uint32", "uint64":
		n, ok := x.(json.Number)
		if !ok {
			break
		}
		u, err := strconv.ParseUint(n.String(), 10, 64)
		if err != nil {
			return viewMapErr(schemaread.ReasonClamped, "%s %s", typ, n)
		}
		switch {
		case gv.CanUint():
			if gv.OverflowUint(u) {
				return viewMapErr(schemaread.ReasonClamped, "%d into a Go %s", u, gv.Type())
			}
			gv.SetUint(u)
			return nil
		case gv.CanInt():
			if u > math.MaxInt64 || gv.OverflowInt(int64(u)) {
				return viewMapErr(schemaread.ReasonClamped, "%d into a Go %s", u, gv.Type())
			}
			gv.SetInt(int64(u))
			return nil
		}
	case "float32", "float64":
		n, ok := x.(json.Number)
		if !ok || !gv.CanFloat() {
			break
		}
		f, err := n.Float64()
		if err != nil {
			return viewMapErr(schemaread.ReasonClamped, "%s %s", typ, n)
		}
		gv.SetFloat(f)
		return nil
	case "bool":
		if b, ok := x.(bool); ok && gv.Kind() == reflect.Bool {
			gv.SetBool(b)
			return nil
		}
	case "string":
		if s, ok := x.(string); ok && gv.Kind() == reflect.String {
			gv.SetString(s)
			return nil
		}
	default:
		info := viewTable(typ)
		switch {
		case info == nil:
		case gv.Kind() == reflect.Pointer:
			p := reflect.New(gv.Type().Elem())
			if err := viewValueFrom(x, p.Elem(), typ, false); err != nil {
				return err
			}
			gv.Set(p)
			return nil
		case gv.Kind() == reflect.Struct && gv.Type() != viewTimeType:
			obj, ok := x.(map[string]any)
			if !ok {
				break
			}
			return viewObjFrom(obj, gv, info)
		case gv.Kind() == reflect.Array && gv.Len() == len(info.Fields):
			obj, ok := x.(map[string]any)
			if !ok {
				break
			}
			for i := range info.Fields {
				f := &info.Fields[i]
				if y, has := obj[f.Json]; has && y != nil {
					if err := viewValueFrom(y, gv.Index(i), f.TypeName, f.IsArray); err != nil {
						return err
					}
				}
			}
			return nil
		case len(info.Fields) == 1:
			obj, ok := x.(map[string]any)
			if !ok {
				break
			}
			f := &info.Fields[0]
			if y, has := obj[f.Json]; has && y != nil {
				return viewValueFrom(y, gv, f.TypeName, f.IsArray)
			}
			return nil
		}
	}
	return viewMapErr(schemaread.ReasonMalformed, "%s into a Go %s", typ, gv.Type())
}
