package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCardAllPrintsATableAndJSONIsOneObject pins the card listing (docs/SPEC-SPRINT.md
// section 11, the card verb): `card --all` and `card --stream` print a bounded table
// (id, state, tier, stream, attempts) with --max and MORE, `--json` is one object
// {cards:[...]}, and the line form stays as `--ndjson`.
func TestCardAllPrintsATableAndJSONIsOneObject(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("add --stream s1 --count 3")
	ta.ok("add --stream s2 --count 1 --one")

	text := ta.ok("card --all --max 0")
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	require.NotEmpty(t, lines)
	header := lines[0]
	for _, col := range []string{"id", "state", "tier", "stream", "attempts"} {
		assert.Contains(t, header, col, "the table names %s: %q", col, header)
	}
	assert.NotContains(t, text, "MORE", "--max 0 is the whole table")
	assert.Contains(t, text, "s1-1")
	assert.Contains(t, text, "s1-3")
	assert.Contains(t, text, "s2-1")
	assert.Regexp(t, `(?m)^s1-1\s+ready\s+flash\s+s1\s+0$`, text, "one row is id, state, tier, stream, attempts")

	bounded := ta.ok("card --all --max 1")
	assert.Contains(t, bounded, "s1-1")
	assert.NotContains(t, bounded, "s1-2", "--max 1 shows one row")
	assert.NotContains(t, bounded, "s2-1")
	assert.Regexp(t, `MORE kind=cards shown=1 total=\d+ run: nova-sprint card --all --max 0`, bounded)

	raw := ta.ok("card --all --json --max 0")
	assert.Equal(t, 1, strings.Count(raw, "\n"), "one object, not one object a line: %s", raw)
	var obj struct {
		Cards []struct {
			ID       string `json:"id"`
			State    string `json:"state"`
			Tier     string `json:"tier"`
			Stream   string `json:"stream"`
			Attempts int    `json:"attempts"`
			Column   string `json:"column"`
		} `json:"cards"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &obj), raw)
	require.GreaterOrEqual(t, len(obj.Cards), 4, "every card is in the one object")
	ids := map[string]bool{}
	for _, c := range obj.Cards {
		ids[c.ID] = true
		if c.ID == "s1-1" {
			assert.Equal(t, "ready", c.State)
			assert.Equal(t, "flash", c.Tier)
			assert.Equal(t, "s1", c.Stream)
			assert.Equal(t, 0, c.Attempts)
			assert.Equal(t, "ready", c.Column, "column stays the live table place")
		}
	}
	assert.True(t, ids["s1-1"] && ids["s2-1"])

	var cut struct {
		Cards []struct {
			ID string `json:"id"`
		} `json:"cards"`
		More *struct {
			Shown int `json:"shown"`
			Total int `json:"total"`
		} `json:"more"`
	}
	require.NoError(t, json.Unmarshal([]byte(ta.ok("card --all --json --max 1")), &cut))
	require.Len(t, cut.Cards, 1)
	require.NotNil(t, cut.More)
	assert.Equal(t, 1, cut.More.Shown)
	assert.Greater(t, cut.More.Total, 1)

	streamText := ta.ok("card --stream s2 --max 0")
	assert.Contains(t, streamText, "s2-1")
	assert.NotContains(t, streamText, "s1-1", "--stream keeps one stream")
	var streamObj struct {
		Cards []struct {
			ID     string `json:"id"`
			Stream string `json:"stream"`
		} `json:"cards"`
	}
	require.NoError(t, json.Unmarshal([]byte(ta.ok("card --stream s2 --json --max 0")), &streamObj))
	require.NotEmpty(t, streamObj.Cards)
	for _, c := range streamObj.Cards {
		assert.Equal(t, "s2", c.Stream)
	}
	assert.Regexp(t, `MORE kind=cards shown=1 total=\d+ run: nova-sprint card --stream s1 --max 0`, ta.ok("card --stream s1 --max 1"))

	nd := strings.Split(strings.TrimSpace(ta.ok("card --all --ndjson")), "\n")
	require.GreaterOrEqual(t, len(nd), 4, "--ndjson is one object a line")
	for _, line := range nd {
		var c cardLine
		require.NoError(t, json.Unmarshal([]byte(line), &c), line)
		assert.NotEmpty(t, c.ID)
		assert.NotContains(t, c.Fields, "brief", "the brief is its length, not its text")
	}
}
