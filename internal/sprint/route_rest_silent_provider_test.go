package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Replay the supplied storm evidence at synthetic times, without a provider call.
// tla/RouteRest.tla SilentProviderRests and MCRouteRest SilentSpec: the third
// silent take rests the route; output-bearing noresult never contributes.
func TestASilentProviderTakeCountsTowardItsRouteRest(t *testing.T) {
	t.Parallel()
	const silent = "input=0 cache_read=0 cache_write=0 output=0 reasoning=0 requests=1"
	const direct = "pro-deepseek-v4-direct"
	const control = "pro-deepseek-openrouter"
	ids := []string{
		"delta-tests-build-every-test-file-in-assert-and-ndebug-modes.w1",
		"replicant-interpolation-snaps-enums-and-flags.w2",
		"serialize-cpp-fuzz-block-decoder-and-model-inputs.w2",
	}
	t0 := time.Date(2026, 10, 15, 0, 25, 0, 0, time.UTC)
	for _, scenario := range []struct {
		members int
		report  string
	}{
		{1, "idle-reap: no RESULT.md shape"},
		{3, "idle-reap: no RESULT.md shape"},
		{1, "no result: idle-reap: no RESULT.md shape"},
		{3, "no result: idle-reap: no RESULT.md shape"},
	} {
		t.Run(itoa(scenario.members)+" members "+scenario.report, func(t *testing.T) {
			f := NewTable(Fleet)
			f.SetRows([]string{"m1", "m2", "m3"})
			routes := []Route{{Name: direct, Provider: "deepseek", Enabled: true}, {Name: control, Provider: "openrouter", Enabled: true}}
			put := func(id, route, member, usage string, minute int) {
				take := ProviderTake{Route: route, Member: member, Finished: stamp(t0.Add(time.Duration(minute) * time.Minute)), Usage: usage, Error: scenario.report}
				f.Put(&Card{ID: id, Row: member, Col: Withdrawn, Fields: map[string]string{FieldProviderTake + "1": take.String()}})
			}
			put("openrouter-control.w1", control, "m1", "output=9874 reasoning=33307 requests=57", 1)
			require.False(t, routeEnds(f)[control][0].transient, "output-bearing control must never count")
			for i, id := range ids {
				put(id, direct, "m"+itoa(i%scenario.members+1), silent, i+2)
				now := t0.Add(time.Duration(i+2) * time.Minute)
				due := RestsDue(&Snapshot{Now: now, Fleet: f, Routes: routes})
				if i < 2 {
					require.Empty(t, due, "the existing 3-in-10 threshold must hold")
					continue
				}
				require.Equal(t, []RouteRest{{Route: direct, At: now, Until: now.Add(RouteRestFor), Cards: ids, Cause: RestProvider}}, due)
				f.SetProps(map[string]string{PropRule3Rest("deepseek"): direct + " " + due[0].value()})
				assert.Empty(t, RestsDue(&Snapshot{Now: now, Fleet: f, Routes: routes}), "a resting route is not rested again")
			}
		})
	}

	// Each counter must explicitly report zero, with exactly one request. Unknown
	// usage, output/reasoning and cached input are not silence.
	for _, usage := range []string{"", "-", "requests=1", "input=0 output=0 requests=1",
		strings.Replace(silent, "requests=1", "requests=0", 1),
		strings.Replace(silent, "requests=1", "requests=2", 1),
		strings.Replace(silent, "input=0", "input=1", 1),
		strings.Replace(silent, "cache_read=0", "cache_read=1", 1),
		strings.Replace(silent, "cache_write=0", "cache_write=1", 1),
		strings.Replace(silent, "output=0", "output=1", 1),
		strings.Replace(silent, "reasoning=0", "reasoning=1", 1)} {
		t.Run("not silent "+usage, func(t *testing.T) {
			f := NewTable(Fleet)
			f.SetRows([]string{"m1"})
			for i, id := range ids {
				take := ProviderTake{Route: direct, Member: "m1", Finished: stamp(t0.Add(time.Duration(i) * time.Minute)), Usage: usage, Error: "no result: idle-reap: no RESULT.md shape"}
				f.Put(&Card{ID: id, Row: "m1", Col: Withdrawn, Fields: map[string]string{FieldProviderTake + "1": take.String()}})
			}
			for _, end := range routeEnds(f)[direct] {
				assert.False(t, end.transient, "unknown/nonzero usage must never count as a provider timeout")
			}
			for _, rest := range RestsDue(&Snapshot{Now: t0.Add(5 * time.Minute), Fleet: f, Routes: []Route{{Name: direct, Enabled: true}}}) {
				assert.NotEqual(t, RestProvider, rest.Cause, "the existing no-model-call attribution is separate")
			}
		})
	}
}
