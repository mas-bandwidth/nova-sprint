package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An unknown verb names the nearest verb (by edit distance) and a synonym
// hint for a natural word, a refusal under --json is one JSON object on stdout
// with nothing else, and a boolean flag before the verb is read from the
// verb's flag set instead of being taken as value-taking and swallowing the
// verb (docs/CLI.md; the JSON refusal shape of pkg/tool: ok false, the verb,
// the problem, the remedy).
func TestUnknownVerbSuggestsNearestAndJSONRefusalsAndGlobalBoolFlags(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")

	code, out, errs := ta.do("adds")
	assert.Equal(t, 2, code)
	assert.Empty(t, out)
	assert.Contains(t, errs, "unknown verb adds")
	assert.Contains(t, errs, "did you mean add?", errs)

	code, _, errs = ta.do("friends")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "unknown verb friends")
	assert.Contains(t, errs, "friend", errs)

	code, out, errs = ta.do("adds --json")
	assert.Equal(t, 2, code)
	assert.Empty(t, errs, "%s", errs)
	assert.Equal(t, 1, strings.Count(out, "\n"), "one JSON object: %q", out)
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got), "not JSON: %q", out)
	assert.Equal(t, false, got["ok"])
	assert.Equal(t, "refused", got["status"])
	assert.Contains(t, got["reason"].(string), "did you mean add?")

	code, out, errs = ta.do("--all where")
	require.Equal(t, 0, code, "--all where: %s", errs)
	assert.Contains(t, out, "SPRINT TABLE")
	assert.NotContains(t, errs, "unknown verb")
}
