package policy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCatalogDefault: the built-in's catalog names no provider, model or
// bench model (they are the sprint's own), read from v1 after up the same,
// naming its version; a catalog the coordinator sets is read and answered.
func TestCatalogDefault(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	want := Catalog{Models: map[string][]Model{}, Harnesses: []string{"claude", "codex", "opencode", "api"}, Bench: map[string]string{}}
	c, err := ReadCatalog(w.ctx(), w.st, testEngine(), 0)
	require.NoError(t, err)
	assert.Equal(t, want, c)
	w.up()
	c, err = ReadCatalog(w.ctx(), w.st, testEngine(), 1)
	require.NoError(t, err)
	want.Version = 1
	assert.Equal(t, want, c)
	assert.Empty(t, c.ForTier("flash"))
	assert.Empty(t, c.BenchModel("flash"), "no bench model: no bench deal until the sprint names one")
	assert.Equal(t, `model "a-flash" is not in the catalog of policy v1 (models: )`, c.Unknown("a-flash", "api"))
	a, fs := testEngine().Answer(Versions{}, w.snap(), w.ro, FullQuestions(w.snap(), nil))
	require.Empty(t, fs)
	assert.Equal(t, want.Models, a.Catalog.Models, "the tick validates catalog like every answer")
	assert.Equal(t, want.Bench, a.Catalog.Bench)

	v := w.set("filled.star", withTestCatalog(defaultBody))
	c, err = ReadCatalog(w.ctx(), w.st, testEngine(), v)
	require.NoError(t, err)
	assert.Empty(t, c.Unknown("a-heavy", "claude"))
	assert.Empty(t, c.Unknown("z-pro", "api"))
	assert.Equal(t, `model "gemini" is not in the catalog of policy v2 (models: a-flash a-heavy z-flash z-pro)`, c.Unknown("gemini", "api"))
	// A worker's candidates for a tier: provider/model, providers in name order.
	assert.Equal(t, []string{"acme/a-flash", "zeta/z-flash"}, c.ForTier("flash"))
	assert.Equal(t, []string{"zeta/z-pro"}, c.ForTier("pro"))
	assert.Empty(t, c.ForTier("none"))
	assert.Equal(t, "zeta/z-pro", c.BenchModel("pro"), "a bench's one model for a tier")
}

// withTestCatalog is body with a filled catalog of made-up providers and
// models: acme serves flash and heavy, zeta flash and pro; the bench runs
// acme's flash and zeta's pro.
func withTestCatalog(body string) string {
	def := `def catalog():
    return {
        "models": {
            "acme": [{"name": "a-flash", "tier": "flash"}, {"name": "a-heavy", "tier": "heavy"}],
            "zeta": [{"name": "z-flash", "tier": "flash"}, {"name": "z-pro", "tier": "pro"}],
        },
        "harnesses": ["claude", "codex", "opencode", "api"],
        "bench": {"flash": "acme/a-flash", "pro": "zeta/z-pro"},
    }`
	start := strings.Index(body, "def catalog():")
	end := strings.Index(body[start:], "\n    }\n") + start + len("\n    }\n")
	return body[:start] + def + "\n" + body[end:]
}

// TestDefaultPolicyNamesNoVendorOrModel: the built-in policy names nothing
// of ours or anyone's: no provider, no model, no org.
func TestDefaultPolicyNamesNoVendorOrModel(t *testing.T) {
	t.Parallel()
	body := strings.ToLower(defaultBody)
	for _, name := range []string{"anthropic", "openai", "google", "deepseek", "inception", "paperinstruments", "haiku", "sonnet", "opus", "fable", "luna", "terra", "argon", "astra", "mercury", "ultramarine", "gemini", "gpt", "mas-bandwidth", "the human's"} {
		assert.NotContains(t, body, name)
	}
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

// TestCatalogBenchRefused: a bench model that is not one of its tier's
// models, or a tier not in TIERS, is refused at set, naming the tier's
// models (defect 57: the deal names a model a harness accepts).
func TestCatalogBenchRefused(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	for _, c := range []struct{ bench, want string }{
		{`{"pro": "deepseek/deepseek-chat"}`, `bench: pro's model "deepseek/deepseek-chat" is not one of its models (deepseek/deepseek-flash)`},
		{`{"ultra": "deepseek/deepseek-flash"}`, `bench: tier "ultra" is not one of flash pro heavy frontier`},
		{`["deepseek/deepseek-flash"]`, `bench is a list, not a dict of tier to model`},
	} {
		body, line := override("catalog", "def catalog():\n    return {\"models\": {\"deepseek\": [{\"name\": \"deepseek-flash\", \"tier\": \"pro\"}]}, \"harnesses\": [\"opencode\"], \"bench\": "+c.bench+"}")
		path := w.file("bench.star", body)
		_, err := Set(w.ctx(), w.st, w.ro, testEngine(), w.caller(), path, "test")
		assert.Equal(t, fmt.Sprintf("SPRINT REFUSED policy set: POLICY REFUSED bench.star line %d catalog(): %s: nova-sprint policy check %s", line, c.want, path), refusal(t, err))
	}
}
