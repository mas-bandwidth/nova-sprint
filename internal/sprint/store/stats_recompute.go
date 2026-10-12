package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// RecomputeStats recomputes the per-member and per-route end-class counters from the
// stored take ends (the primaries' cost records, CardCostOf), classified by
// sprint.ClassifyEnd: ok, failed work, no result, provider, coordination, environment
// and harness. It changes no card: it says what the counters are under the new classes,
// and with DryRun false it writes the counters to the stats record so the page shows
// them at once (dry-run first: --dry-run prints the same figures and writes nothing).
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
		ml := map[string]string{}
		for name, e := range members {
			ml[name] = e.line()
		}
		rl := map[string]string{}
		for name, e := range routes {
			rl[name] = e.line()
		}
		b, _ := json.Marshal(map[string]any{"ok": true, "members": ml, "routes": rl})
		fmt.Fprintln(stdout, string(b))
	}
	word := "RECOMPUTE OK"
	if dryRun {
		word = "RECOMPUTE DRY-RUN would recompute"
	}
	fmt.Fprintf(stdout, "%s recomputed end-class counters from stored ends (members=%d routes=%d)\n", word, len(members), len(routes))
	return nil
}

// endCounts is one member's or route's take ends, by class.
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

func (e *endCounts) line() string {
	return fmt.Sprintf("ok=%d failed=%d no_result=%d provider=%d coordination=%d environment=%d harness=%d",
		e.ok, e.failed, e.noResult, e.provider, e.coordination, e.environment, e.harness)
}
