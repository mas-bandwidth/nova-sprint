package policy

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCatalogDefault: the built-in names no bench model (the operator does),
// and a version's bench is the tick's, read through BenchModel.
func TestCatalogDefault(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	a, fs := testEngine().Answer(Versions{}, w.snap(), w.ro, FullQuestions(w.snap(), nil))
	require.Empty(t, fs)
	assert.Empty(t, a.Catalog.Bench, "the built-in ships no bench model")
	assert.Empty(t, a.Catalog.BenchModel("pro"))
	body, _ := override("catalog", "def catalog():\n    return {\"bench\": {\"pro\": \"inception/mercury-3\"}}")
	v := &store.PolicyVersion{Version: 1, Kind: "script", Body: body}
	a, fs = testEngine().Answer(Versions{Active: v}, w.snap(), w.ro, FullQuestions(w.snap(), nil))
	require.Empty(t, fs)
	assert.Equal(t, map[string]string{"pro": "inception/mercury-3"}, a.Catalog.Bench)
	assert.Equal(t, "inception/mercury-3", a.Catalog.BenchModel("pro"), "a bench's one model for a tier")
}

// TestCatalogOlderKeys: a version written with the older catalog's models
// and harnesses keys is still accepted; only bench is read.
func TestCatalogOlderKeys(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	body, _ := override("catalog", "def catalog():\n    return {\"models\": {\"deepseek\": [{\"name\": \"deepseek-flash\", \"tier\": \"flash\"}]}, \"harnesses\": [\"opencode\"], \"bench\": {\"flash\": \"deepseek/deepseek-flash\"}}")
	w.set("older.star", body)
}

// TestCatalogBenchRefused: a tier not in TIERS, a model not in
// provider/model form, a bench that is not a dict and a missing bench are
// refused at set (defect 57: the deal names a model a harness accepts).
func TestCatalogBenchRefused(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	for _, c := range []struct{ answer, want string }{
		{`{"bench": {"pro": "mercury-3"}}`, `bench: pro's model "mercury-3" is not provider/model`},
		{`{"bench": {"ultra": "deepseek/deepseek-flash"}}`, `bench: tier "ultra" is not one of flash pro heavy frontier`},
		{`{"bench": ["deepseek/deepseek-flash"]}`, `bench is a list, not a dict of tier to model`},
		{`{}`, `key bench is missing`},
	} {
		body, line := override("catalog", "def catalog():\n    return "+c.answer)
		path := w.file("bench.star", body)
		_, err := Set(w.ctx(), w.st, w.ro, testEngine(), w.caller(), path, "test")
		assert.Equal(t, fmt.Sprintf("SPRINT REFUSED policy set: POLICY REFUSED bench.star line %d catalog(): %s: nova-sprint policy check %s", line, c.want, path), refusal(t, err))
	}
}
