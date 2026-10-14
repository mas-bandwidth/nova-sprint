package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An unknown verb names the nearest verb (by edit distance) or a synonym of the
// natural word typed, then the verb groups' names instead of every verb on one
// line; a usage refusal under --json, the dispatcher's and a verb's own, is one
// JSON object on stdout with an empty stderr; and a boolean flag before the verb
// is read from the matched verb's flag set, so it never swallows the verb
// (docs/CLI.md; the JSON refusal shape of pkg/tool: ok false, the verb, the
// problem, the remedy).
func TestUnknownVerbSuggestsNearestAndJSONRefusalsAndGlobalBoolFlags(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")

	code, out, errs := ta.do("adds")
	assert.Equal(t, 2, code)
	assert.Empty(t, out)
	assert.Contains(t, errs, "unknown verb adds")
	assert.Contains(t, errs, "did you mean add?", errs)
	assert.NotContains(t, errs, "available:", "every verb on one line: %s", errs)
	assert.Contains(t, errs, "the verb groups are fleet, friend, reader, goal, stream, lane, merge-window", errs)
	assert.Contains(t, errs, "run: nova-sprint help", errs)

	code, _, errs = ta.do("friends")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "unknown verb friends")
	assert.Contains(t, errs, "friends means friend", errs)

	code, out, errs = ta.do("adds --json")
	assert.Equal(t, 2, code)
	assert.Empty(t, errs, "%s", errs)
	assert.Equal(t, 1, strings.Count(out, "\n"), "one JSON object: %q", out)
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got), "not JSON: %q", out)
	assert.Equal(t, false, got["ok"])
	assert.Equal(t, "refused", got["status"])
	assert.Equal(t, 2.0, got["exit"])
	assert.Contains(t, got["reason"].(string), "did you mean add?")
	assert.Equal(t, "nova-sprint help", got["remedy"])

	// a verb's own usage refusal under --json is the same object, not text on stderr
	code, out, errs = ta.do("backup --json")
	assert.Equal(t, 2, code)
	assert.Empty(t, errs, "%s", errs)
	var verbGot map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &verbGot), "not JSON: %q", out)
	assert.Equal(t, false, verbGot["ok"])
	assert.Equal(t, "refused", verbGot["status"])
	assert.Equal(t, "backup", verbGot["verb"])
	assert.NotEmpty(t, verbGot["reason"])
	assert.NotEmpty(t, verbGot["remedy"])

	code, out, errs = ta.do("--all where")
	require.Equal(t, 0, code, "--all where: %s", errs)
	assert.Contains(t, out, "SPRINT TABLE")
	assert.NotContains(t, errs, "unknown verb")

	// a flag the matched verb gives a value still takes it before the verb
	code, _, errs = ta.do("--check nonce friend beat friend-a")
	assert.NotContains(t, errs, "unknown verb", "%s", errs)
}
