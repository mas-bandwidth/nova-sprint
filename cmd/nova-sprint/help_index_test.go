package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The bare help is a short index (docs/CLI.md, docs/STANDARD.md section 2): an
// AI caller spends ~16,000 tokens before choosing a verb when `nova-sprint help`
// prints the whole manual, so bare help is at most 70 lines, the coordinator's
// day before the verb groups, and every moved section is reachable as a topic.
// `help --all` still prints the whole manual, byte for byte, so scripts and
// tests that read it keep working. An unknown topic is refused (exit 2) naming
// the topics, and `help --json` is the same index as one JSON object.
func TestBareHelpIsAShortIndexAndTheGuideIsATopic(t *testing.T) {
	t.Parallel()
	a := newApp(func(string) string { return "" })
	do := func(args ...string) (int, string, string) {
		var out, errb bytes.Buffer
		code := runInto(a, args, &out, &errb)
		return code, out.String(), errb.String()
	}

	code, out, errs := do("help")
	require.Equal(t, 0, code, "help: %s", errs)
	require.Empty(t, errs)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	assert.LessOrEqual(t, len(lines), 70, "bare help is a short index, not the manual: %d lines", len(lines))
	day := strings.Index(out, "the coordinator's day, in five lines")
	groups := strings.Index(out, "\nverbs:")
	require.GreaterOrEqual(t, day, 0, "the index names the coordinator's day")
	require.GreaterOrEqual(t, groups, 0, "the index names the verb groups")
	assert.Less(t, day, groups, "the five-line day comes before the verb groups")
	for _, l := range dayLines {
		assert.Contains(t, out, l, "the index carries the day line %q", l)
	}
	assert.Contains(t, out, "nova-sprint play", "the index names the standalone verbs")

	// help --all is the old bare help, byte for byte.
	code, all, errs := do("help", "--all")
	require.Equal(t, 0, code, "help --all: %s", errs)
	assert.Equal(t, banner(), all, "help --all is the old bare help, byte for byte")

	// help guide carries the walkthrough and the day.
	code, guide, errs := do("help", "guide")
	require.Equal(t, 0, code, "help guide: %s", errs)
	assert.Contains(t, guide, "trying it without a Redis", "the guide carries the walkthrough")
	assert.Contains(t, guide, "nova-sprint init --readers reader-m2 --members m1,m2")
	for _, l := range dayLines {
		assert.Contains(t, guide, l, "the guide carries the day line %q", l)
	}

	// Every moved section is reachable as a topic by its own name: land and
	// twin are verbs and moved sections at once, so their -h carries the
	// section as `help hold` carries holdWords().
	code, land, errs := do("help", "land")
	require.Equal(t, 0, code, "help land: %s", errs)
	assert.Contains(t, land, "Landing, the coordinator's", "help land reaches landWords()")
	code, twin, errs := do("help", "twin")
	require.Equal(t, 0, code, "help twin: %s", errs)
	assert.Contains(t, twin, "trying it without a Redis", "help twin reaches twinWords()")
	code, hold, errs := do("help", "hold")
	require.Equal(t, 0, code, "help hold: %s", errs)
	assert.Contains(t, hold, "the hold family, one screen", "help hold reaches holdWords()")

	// An unknown topic is refused, naming the topics.
	code, _, errs = do("help", "nosuchtopic")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "nosuchtopic")
	assert.Contains(t, errs, "topics are")

	// help --json is the same index as one JSON object, never a refusal.
	code, raw, errs := do("help", "--json")
	require.Equal(t, 0, code, "help --json: %s", errs)
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &got), "one JSON object: %q", raw)
	assert.Contains(t, got, "topics")
	assert.Contains(t, got, "day")
	assert.Equal(t, 0, strings.Count(strings.TrimSpace(raw), "\n"), "one line: %q", raw)

	// --help --json is the same object.
	code, raw2, errs := do("--help", "--json")
	require.Equal(t, 0, code, "--help --json: %s", errs)
	assert.Equal(t, raw, raw2, "--help --json is the same object")
}
