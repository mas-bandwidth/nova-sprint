package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
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
// does not read.
func readWhere(raw string, ok bool) (WhereRecord, bool) {
	var r WhereRecord
	if !ok || json.Unmarshal([]byte(raw), &r) != nil {
		return WhereRecord{}, false
	}
	return r, true
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
	if r, ok := readWhere(vals[0], oks[0]); ok && r.Epoch == st.epoch && r.Rev == shapes[0].Revision && r.FleetRev == shapes[1].Revision && r.Stats == stamp && r.SeriesCounted && r.ArchivedCounted && r.DecisionHoldsCounted {
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
	return d, st.keepStoreRTT(ctx, kv, readStoreRTT(v, ok), t0, d)
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
	return st.keepStoreRTT(ctx, kv, readStoreRTT(v, ok), st.now(), d)
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

// readStoreRTT is the record a read returned; none, or one that does not
// parse, is an empty record.
func readStoreRTT(v string, ok bool) StoreRTTRecord {
	var r StoreRTTRecord
	if ok && json.Unmarshal([]byte(v), &r) != nil {
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
		// the stats reset's mark, in the same exchange: an unreadable stats record is none
		// here, as it is no tidy (the tick says it, statsRecordOf)
		var stats StatsRecord
		if !oks[4] || json.Unmarshal([]byte(vals[4]), &stats) != nil {
			stats = StatsRecord{}
		}
		f.Reset = stats.resetIn(st.epoch)
		for i, v := range []any{&f.Machine, &f.Heartbeat} {
			if oks[i] {
				if err := json.Unmarshal([]byte(vals[i]), v); err != nil {
					return f, err
				}
			}
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
		if r := readStoreRTT(vals[3], oks[3]); len(r.Samples) > 0 && st.now().Sub(time.UnixMilli(r.At)) <= StoreRTTWindow {
			f.HasStoreRTT, f.StoreRTTP50MS, f.StoreRTTP99MS = true, r.P50MS, r.P99MS
		}
		if r, ok := readStops(vals[5], oks[5]); ok && r.Epoch == st.epoch {
			f.Stops, f.HasStops = r, true
		}
		r, ok := readWhere(vals[2], oks[2])
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
	// a ready primary the server-default holds in place (sprint.TickDecisionDefaults) is
	// in the ready column: one more read, on this uncounted path alone; a read that fails
	// leaves the waiting column's holds, and where answers degraded, as with the stamps
	if t, rerr := st.workColumn(ctx, sprint.Ready); rerr == nil && t != nil {
		f.DecisionHolds = append(f.DecisionHolds, sprint.DefaultHeldDecisions(&sprint.Snapshot{Work: t})...)
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
	var stats StatsRecord
	if !had || json.Unmarshal([]byte(raw), &stats) != nil {
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
