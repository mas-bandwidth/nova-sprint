package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The findings a reader met on the v1.2.22 binary (the cold-reader run):
// an unknown verb names the nearest verb, not the whole table; a natural word
// names the verb it stands for; a refusal under --json is the one JSON object
// the other tools write, on stdout, and not a line of text on stderr; and a
// boolean flag before the verb is that verb's own, so `--all where` runs where.
func TestUnknownVerbSuggestsNearestAndJSONRefusalsAndGlobalBoolFlags(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)

	// `adds` is one edit from add: the refusal names add, one line, on stderr.
	code, out, errs := ta.do("adds")
	require.Equal(t, 2, code)
	assert.Empty(t, out)
	assert.Contains(t, errs, "unknown verb adds")
	assert.Contains(t, errs, "did you mean add?")
	assert.Equal(t, 1, strings.Count(errs, "\n"), "one line: %q", errs)

	// `friends` is the natural word for the friend group: the hint names friend.
	code, out, errs = ta.do("friends")
	require.Equal(t, 2, code)
	assert.Empty(t, out)
	assert.Contains(t, errs, "did you mean friend?")

	// The same refusal under --json is one JSON object on stdout, nothing on stderr.
	code, out, errs = ta.do("adds --json")
	require.Equal(t, 2, code)
	assert.Empty(t, errs)
	assert.Equal(t, 1, strings.Count(out, "\n"), "one JSON object: %q", out)
	var got struct {
		Result struct {
			Verb   string   `json:"verb"`
			Status string   `json:"status"`
			Exit   int      `json:"exit"`
			Remedy string   `json:"remedy"`
			Why    []string `json:"why"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got), "the refusal is JSON: %q", out)
	assert.Equal(t, "refused", got.Result.Status)
	assert.Equal(t, 2, got.Result.Exit)
	require.NotEmpty(t, got.Result.Why)
	assert.Contains(t, got.Result.Why[0], "unknown verb adds")

	// A boolean flag before the verb is the matched verb's own: `--all where`
	// is where --all, never an unknown verb --all.
	code, out, errs = ta.do("--all where")
	assert.NotContains(t, errs, "unknown verb", "where --all ran: exit %d\n%s%s", code, out, errs)
	assert.NotContains(t, out, "unknown verb", "where --all ran: exit %d\n%s%s", code, out, errs)
}
