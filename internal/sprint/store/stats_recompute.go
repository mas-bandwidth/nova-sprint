package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// RecomputeStats recomputes the per-member and per-route end-class counters from the
// stored take ends (the primaries' cost records, CardCostOf), classified by
// sprint.ClassifyEnd: ok, failed work, no result, provider, coordination, environment
// and harness. It changes no card: it says what the counters are under the new classes,
// and with dryRun false it writes the counters to the recompute record (keyStatsRecompute),
// read back by RecomputeStatsRecord; dry-run prints the same figures and writes nothing
// (dry-run first: `stats tidy --recompute --dry-run`).
func (st *Store) RecomputeStats(ctx context.Context, asJSON, dryRun bool, stdout, stderr io.Writer) error {
	const verb = "stats tidy --recompute"
	s, err := st.Load(ctx, []string{sprint.Work, sprint.Fleet}, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", verb, err)
	}
	members, routes := map[string]*endCounts{}, map[string]*endCounts{}
	count := func(m map[string]*endCounts, name string, class sprint.EndClass) {
		x := m[name]
		if x == nil {
			x = &endCounts{}
			m[name] = x
		}
		x.add(class)
	}
	for _, p := range s.Work.Cards() {
		if p == nil || !p.Placed() || sprint.IsSentinel(p) {
			continue
		}
		for _, c := range sprint.CardCostOf(p).Consumers {
			if c.Kind != "work" {
				continue
			}
			class := sprint.ClassifyEnd(c.End)
			who := c.Who
			if who == "" {
				who = "-"
			}
			count(members, who, class)
			route := c.Route
			if route == "" {
				route = "-"
			}
			count(routes, route, class)
		}
	}
	rec := RecomputeRecord{At: st.now().UTC(), Members: map[string]RecomputeCounts{}, Routes: map[string]RecomputeCounts{}}
	for name, e := range members {
		rec.Members[name] = e.counts()
	}
	for name, e := range routes {
		rec.Routes[name] = e.counts()
	}
	for _, name := range slices.Sorted(maps.Keys(members)) {
		if !asJSON {
			fmt.Fprintf(stdout, "MEMBER %s %s\n", name, members[name].line())
		}
	}
	for _, name := range slices.Sorted(maps.Keys(routes)) {
		if !asJSON {
			fmt.Fprintf(stdout, "ROUTE %s %s\n", name, routes[name].line())
		}
	}
	if asJSON {
		b, _ := json.Marshal(rec)
		fmt.Fprintln(stdout, string(b))
	}
	if dryRun {
		fmt.Fprintf(stdout, "RECOMPUTE DRY-RUN would write counters for members=%d routes=%d (nothing written)\n", len(members), len(routes))
		return nil
	}
	if err := st.putJSON(ctx, keyStatsRecompute, rec); err != nil {
		return fmt.Errorf("%s: %w", verb, err)
	}
	fmt.Fprintf(stdout, "RECOMPUTE OK wrote counters for members=%d routes=%d to %s\n", len(members), len(routes), keyStatsRecompute)
	return nil
}

// keyStatsRecompute is the recompute record: the end-class counters recomputed from the
// stored take ends, written by stats tidy --recompute and read back by
// RecomputeStatsRecord.
const keyStatsRecompute = "stats:recompute"

// RecomputeStatsRecord reads the recompute record back (stats tidy --recompute). It is
// the reader of the record the recompute verb writes: the store's own test reads it back
// to prove the write is stored and readable. ok is false when no recompute has been
// written.
func (st *Store) RecomputeStatsRecord(ctx context.Context) (rec RecomputeRecord, ok bool, err error) {
	kv, err := st.kv()
	if err != nil {
		return RecomputeRecord{}, false, err
	}
	raw, ok, err := kv.GetKey(ctx, keyStatsRecompute)
	if err != nil || !ok {
		return RecomputeRecord{}, false, err
	}
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return RecomputeRecord{}, false, fmt.Errorf("the machine's %s record is unreadable: %w", keyStatsRecompute, err)
	}
	return rec, true, nil
}

// RecomputeRecord is the recomputed counters, by member and by route, as stored.
type RecomputeRecord struct {
	At      time.Time                  `json:"at"`
	Members map[string]RecomputeCounts `json:"members,omitempty"`
	Routes  map[string]RecomputeCounts `json:"routes,omitempty"`
}

// RecomputeCounts is one member's or route's take ends, by class, with the ok% the
// classes give: ok over ok + failed work + no result.
type RecomputeCounts struct {
	OK           int    `json:"ok"`
	Failed       int    `json:"failed"`
	NoResult     int    `json:"no_result"`
	Provider     int    `json:"provider"`
	Coordination int    `json:"coordination"`
	Environment  int    `json:"environment"`
	Harness      int    `json:"harness"`
	OkPct        string `json:"okpct"`
}

// endCounts is one member's or route's take ends, by class, while RecomputeStats counts.
type endCounts struct {
	ok, failed, noResult, provider, coordination, environment, harness int
}

func (e *endCounts) add(class sprint.EndClass) {
	switch class {
	case sprint.EndClassOK:
		e.ok++
	case sprint.EndClassFailedWork:
		e.failed++
	case sprint.EndClassNoResult:
		e.noResult++
	case sprint.EndClassProvider:
		e.provider++
	case sprint.EndClassCoordination, sprint.EndClassClear:
		e.coordination++
	case sprint.EndClassEnvironment:
		e.environment++
	case sprint.EndClassHarness:
		e.harness++
	}
}

func (e *endCounts) counts() RecomputeCounts {
	return RecomputeCounts{OK: e.ok, Failed: e.failed, NoResult: e.noResult, Provider: e.provider,
		Coordination: e.coordination, Environment: e.environment, Harness: e.harness, OkPct: e.okPct()}
}

func (e *endCounts) line() string {
	return fmt.Sprintf("ok=%d failed=%d no_result=%d provider=%d coordination=%d environment=%d harness=%d okpct=%s",
		e.ok, e.failed, e.noResult, e.provider, e.coordination, e.environment, e.harness, e.okPct())
}

// okPct is the recomputed ok%: ok over ok + failed work + no result, the work ends
// alone; "-" when there are none.
func (e *endCounts) okPct() string {
	work := e.ok + e.failed + e.noResult
	if work == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(e.ok)/float64(work))
}
