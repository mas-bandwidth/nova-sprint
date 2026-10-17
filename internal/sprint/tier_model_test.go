package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// A card that writes a new TLA+ model (its NEW names a .tla module) is tiered heavy at
// add (the owner, 2026-10-17: "I think models at heavy is fine, and touching models can be
// pro now"; docs/SPEC-SPRINT.md, the card decides its model): one that names no tier is
// admitted with tier: heavy on its line 1, one that names heavy or frontier is admitted as
// written, one that names a lower tier is refused with the reason, and one that only edits
// what tla/ holds (models, .cfg files, the run records) keeps its own tier, pro included.
func TestACardThatWritesAModelIsTieredFrontier(t *testing.T) {
	t.Parallel()
	brief := func(line1, paths, news string) string {
		b := line1 + "\nREPO: mas-bandwidth/nova-tools\nPATHS: " + paths + "\n"
		if news != "" {
			b += "NEW: " + news + "\n"
		}
		return b + "TEST: ./internal/x TestX\n\nThe task."
	}
	w := newWorld(t)
	p := Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{
		{ID: "m1", Brief: brief("m1: a lease model", "tla/Lease.tla,tla/MCLease.cfg,internal/x/*.go", "tla/Lease.tla,tla/MCLease.cfg")},
		{ID: "m2", Brief: brief("m2: a friend model tier: frontier", "internal/friend/tla/LaneEnd.tla", "internal/friend/tla/LaneEnd.tla")},
		{ID: "m3", Brief: brief("m3: a model at heavy tier: heavy", "tla/Bar.tla", "tla/Bar.tla")},
		{ID: "e1", Brief: brief("e1: edit a model tier: pro", "tla/Foo.tla,tla/Foo.cfg,tla/COVERAGE.tsv,tla/RUNS.tsv", "")},
		{ID: "e2", Brief: brief("e2: every model", "tla/**", "tla/MCFoo2.cfg")},
		{ID: "r1", Brief: brief("r1: a run record", "tla/RUNS.tsv,tla/CASES.tsv,internal/x/*.go", "")},
		{ID: "g1", Brief: brief("g1: go only tier: pro", "internal/x/*.go", "")},
	}})
	require.Empty(t, p.Refused)
	w.do(p)
	for id, want := range map[string]string{"m1": cardhdr.RouteHeavy, "m2": cardhdr.RouteFrontier, "m3": cardhdr.RouteHeavy, "e1": cardhdr.RoutePro, "e2": cardhdr.RouteFlash, "r1": cardhdr.RouteFlash, "g1": cardhdr.RoutePro} {
		c := w.s.Primary(id)
		require.NotNil(t, c, id)
		assert.Equal(t, want, CardTier(c), "%s is dealt on %s", id, want)
		assert.Equal(t, want, TierWord(c), "%s is counted on %s", id, want)
	}
	m1 := w.s.Primary("m1").F("brief")
	assert.True(t, strings.HasPrefix(m1, "m1: a lease model tier: heavy\n"), "add writes the tier on line 1: %q", m1)
	assert.Equal(t, brief("m2: a friend model tier: frontier", "internal/friend/tla/LaneEnd.tla", "internal/friend/tla/LaneEnd.tla"), w.s.Primary("m2").F("brief"), "a frontier brief is admitted as written")
	assert.Equal(t, brief("m3: a model at heavy tier: heavy", "tla/Bar.tla", "tla/Bar.tla"), w.s.Primary("m3").F("brief"), "heavy card with NEW tla/Bar.tla passes as written")
	assert.Equal(t, brief("e1: edit a model tier: pro", "tla/Foo.tla,tla/Foo.cfg,tla/COVERAGE.tsv,tla/RUNS.tsv", ""), w.s.Primary("e1").F("brief"), "pro card editing tla/Foo.tla passes as written")
	assert.Equal(t, brief("r1: a run record", "tla/RUNS.tsv,tla/CASES.tsv,internal/x/*.go", ""), w.s.Primary("r1").F("brief"), "run records alone are no model")
	moved := map[string]string{}
	for _, u := range p.Units {
		moved[u.Key] = u.Moved
	}
	assert.Contains(t, moved["m1"], "tiered heavy: NEW names a new TLA+ model (tla/Lease.tla)")
	assert.NotContains(t, moved["m2"], "tiered heavy")
	assert.NotContains(t, moved["e2"], "tiered heavy", "a new .cfg is no new model")

	for _, tier := range []string{cardhdr.RouteFlash, cardhdr.RoutePro} {
		p := Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "low-" + tier, Brief: brief("low: a model tier: "+tier, "tla/Bar.tla", "tla/Bar.tla")}}})
		require.Len(t, p.Refused, 1, "%s card with NEW tla/Bar.tla is refused", tier)
		why := p.Refused[0].Why
		assert.Contains(t, why, "NEW names a new TLA+ model (tla/Bar.tla)", tier)
		assert.Contains(t, why, "line 1 names tier "+tier, tier)
		assert.Contains(t, why, "write tier: heavy on line 1", tier)
		assert.Empty(t, p.Units, "nothing admitted on %s", tier)
	}
	assert.Empty(t, Check(w.s, nil))
}

// ModelPaths reads which PATHS entries can name a model: a .tla module anywhere, an MC
// config or the directory itself under a tla/ directory, and a glob that matches either;
// never a run record, a Go file or a doc.
func TestModelPathsNameModulesAndConfigsNotRunRecords(t *testing.T) {
	t.Parallel()
	for entry, want := range map[string]bool{
		"tla/Lease.tla":                       true,
		"tla/MCLease.cfg":                     true,
		"tla/MC*.cfg":                         true,
		"tla/*.tla":                           true,
		"tla/**":                              true,
		"tla/":                                true,
		"tla":                                 true,
		"internal/friend/tla/LaneEnd.tla":     true,
		"internal/friend/tla/*":               true,
		"**/*.tla":                            true,
		"security/**":                         false,
		"**":                                  false,
		"internal/x/*":                        false,
		"tla/RUNS.tsv":                        false,
		"tla/CASES.tsv":                       false,
		"tla/*.tsv":                           false,
		"tla/README.md":                       false,
		"internal/tlaplus/*.go":               false,
		"internal/x/config.cfg":               false,
		"docs/SPEC-SPRINT.md":                 false,
		"cmd/nova-tla/*.go":                   false,
		"internal/friend/tla/LaneEnd_test.go": false,
	} {
		assert.Equal(t, want, len(ModelPaths([]string{entry})) == 1, entry)
	}
}
