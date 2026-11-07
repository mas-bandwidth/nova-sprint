package policy

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCatalogDefault: the built-in's catalog, and the same read from v1
// after up, naming its version.
func TestCatalogDefault(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	want := Catalog{Models: map[string][]Model{
		"anthropic": {{"sonnet", "flash"}, {"sonnet", "pro"}, {"opus", "heavy"}, {"opus", "frontier"}},
		"google":    {{"gemini", "pro"}, {"argon", "heavy"}, {"argon", "frontier"}},
	}, Harnesses: []string{"claude", "codex", "opencode"}}
	c, err := ReadCatalog(w.ctx(), w.st, 0)
	require.NoError(t, err)
	assert.Equal(t, want, c)
	w.up()
	c, err = ReadCatalog(w.ctx(), w.st, 1)
	require.NoError(t, err)
	want.Version = 1
	assert.Equal(t, want, c)
	assert.Empty(t, c.Unknown("opus", "claude"))
	a, fs := NewEngine().Answer(Versions{}, w.snap(), FullQuestions(w.snap(), nil))
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
	c, err := ReadCatalog(w.ctx(), w.st, v)
	require.NoError(t, err)
	assert.Equal(t, Catalog{Version: 2, Models: map[string][]Model{"anthropic": {{"opus", "heavy"}}}, Harnesses: []string{"claude"}}, c)
	assert.NotContains(t, c.Models, "google")
	assert.Equal(t, `model "gemini" is not in the catalog of policy v2 (models: opus)`, c.Unknown("gemini", "claude"))
	assert.Equal(t, `harness "codex" is not in the catalog of policy v2 (harnesses: claude)`, c.Unknown("opus", "codex"))
	_, err = ReadCatalog(w.ctx(), w.st, 9)
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
	_, err := Set(w.ctx(), w.st, NewEngine(), w.caller(), path, "test")
	assert.Equal(t, fmt.Sprintf("SPRINT REFUSED policy set: POLICY REFUSED bad.star line %d catalog(): anthropic model 1: tier is \"ultra\", not one of flash pro heavy frontier: nova-sprint policy check %s", line, path), refusal(t, err))
}
