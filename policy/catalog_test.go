package policy

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCatalogDefault: the built-in's catalog, and the same read from v1
// after up, naming its version.
func TestCatalogDefault(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	want := Catalog{Models: map[string][]Model{
		"anthropic": {{"haiku", "flash"}, {"sonnet", "pro"}, {"opus", "heavy"}, {"fable", "frontier"}},
		"openai":    {{"luna", "flash"}, {"sol", "heavy"}},
		"google":    {{"terra", "pro"}, {"argon", "frontier"}},
		"deepseek":  {{"deepseek-flash-4.1", "flash"}},
		"inception": {{"mercury-3", "pro"}},
		"unknown":   {{"astra", "frontier"}},
	}, Harnesses: []string{"claude", "codex", "opencode", "api"}}
	c, err := ReadCatalog(w.ctx(), w.st, testEngine(), 0)
	require.NoError(t, err)
	assert.Equal(t, want, c)
	w.up()
	c, err = ReadCatalog(w.ctx(), w.st, testEngine(), 1)
	require.NoError(t, err)
	want.Version = 1
	assert.Equal(t, want, c)
	assert.Empty(t, c.Unknown("opus", "claude"))
	assert.Empty(t, c.Unknown("mercury-3", "api"))
	assert.Equal(t, `model "gemini" is not in the catalog of policy v1 (models: haiku sonnet opus fable deepseek-flash-4.1 terra argon mercury-3 luna sol astra)`, c.Unknown("gemini", "api"))
	a, fs := testEngine().Answer(Versions{}, w.snap(), w.ro, FullQuestions(w.snap(), nil))
	require.Empty(t, fs)
	assert.Equal(t, want.Models, a.Catalog.Models, "the tick validates catalog like every answer")
}

// TestCatalogOmitsProvider: a version with no google provider; its models
// are unknown to the row verbs, naming the version.
func TestCatalogOmitsProvider(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	body, _ := override("catalog", "def catalog():\n    return {\"models\": {\"anthropic\": [{\"name\": \"opus\", \"tier\": \"heavy\"}]}, \"harnesses\": [\"claude\"]}")
	v := w.set("no-google.star", body)
	c, err := ReadCatalog(w.ctx(), w.st, testEngine(), v)
	require.NoError(t, err)
	assert.Equal(t, Catalog{Version: 2, Models: map[string][]Model{"anthropic": {{"opus", "heavy"}}}, Harnesses: []string{"claude"}}, c)
	assert.NotContains(t, c.Models, "google")
	assert.Equal(t, `model "terra" is not in the catalog of policy v2 (models: opus)`, c.Unknown("terra", "claude"))
	assert.Equal(t, `harness "codex" is not in the catalog of policy v2 (harnesses: claude)`, c.Unknown("opus", "codex"))
	_, err = ReadCatalog(w.ctx(), w.st, testEngine(), 9)
	assert.Error(t, err, "an unknown version is the store's ErrNotFound")
}

// TestCatalogRefused: a catalog out of domain is refused at set, naming the
// version's file, the question and the line.
func TestCatalogRefused(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	body, line := override("catalog", "def catalog():\n    return {\"models\": {\"anthropic\": [{\"name\": \"opus\", \"tier\": \"ultra\"}]}, \"harnesses\": [\"claude\"]}")
	path := w.file("bad.star", body)
	_, err := Set(w.ctx(), w.st, w.ro, testEngine(), w.caller(), path, "test")
	assert.Equal(t, fmt.Sprintf("SPRINT REFUSED policy set: POLICY REFUSED bad.star line %d catalog(): anthropic model 1: tier is \"ultra\", not one of flash pro heavy frontier: nova-sprint policy check %s", line, path), refusal(t, err))
}

// TestCatalogRefusals: a negative version, an unknown one and one of kind
// data are refused naming the list; a model under two providers is
// refused; a stored version with no catalog() has the built-in's.
func TestCatalogRefusals(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	_, err := ReadCatalog(w.ctx(), w.st, testEngine(), -1)
	assert.Equal(t, "SPRINT REFUSED policy catalog: policy version -1 does not exist: nova-sprint policy show --list", refusal(t, err))
	_, err = ReadCatalog(w.ctx(), w.st, testEngine(), 9)
	assert.Equal(t, "SPRINT REFUSED policy catalog: policy version 9 does not exist: nova-sprint policy show --list", refusal(t, err))
	w.apply(store.PutPolicy{Kind: "data", Name: "table", Body: "{}"})
	_, err = ReadCatalog(w.ctx(), w.st, testEngine(), 2)
	assert.Equal(t, "SPRINT REFUSED policy catalog: v2 is of kind data, which policy does not run: name a script version: nova-sprint policy show --list", refusal(t, err))
	twice, line := override("catalog", "def catalog():\n    return {\"models\": {\"a\": [{\"name\": \"opus\", \"tier\": \"heavy\"}], \"b\": [{\"name\": \"opus\", \"tier\": \"frontier\"}]}, \"harnesses\": [\"claude\"]}")
	path := w.file("twice.star", twice)
	_, err = Set(w.ctx(), w.st, w.ro, testEngine(), w.caller(), path, "test")
	assert.Equal(t, fmt.Sprintf("SPRINT REFUSED policy set: POLICY REFUSED twice.star line %d catalog(): model opus is listed under both a and b: nova-sprint policy check %s", line, path), refusal(t, err))
	older, _ := override("catalog", "")
	v := w.set("older.star", older)
	c, err := ReadCatalog(w.ctx(), w.st, testEngine(), v)
	require.NoError(t, err)
	want, err := ReadCatalog(w.ctx(), w.st, testEngine(), 0)
	require.NoError(t, err)
	want.Version = 0 // ruling 7: the catalog that answered, the built-in one
	assert.Equal(t, want, c, "a version from before catalog has the built-in's")
}
