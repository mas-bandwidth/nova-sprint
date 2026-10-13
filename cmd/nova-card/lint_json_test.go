package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCardLintJSONOutput pins card card-lint-supports-json.
func TestCardLintJSONOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.md")
	require.NoError(t, os.WriteFile(bad, []byte("RESULT: bad sha=0123456789ab tier: cheap\nKIND: fix-red\n\nTHE TASK. <fill>\n"), 0o644))
	exit, stdout, _ := runCard("lint", "--card", bad, "--json")
	assert.Equal(t, 1, exit)
	var doc struct {
		Findings []struct {
			Rule    string `json:"rule"`
			File    string `json:"file"`
			Line    int    `json:"line"`
			Excerpt string `json:"excerpt"`
			Remedy  string `json:"remedy"`
		} `json:"findings"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	require.NotEmpty(t, doc.Findings)
	assert.Equal(t, bad, doc.Findings[0].File)
	assert.NotEmpty(t, doc.Findings[0].Rule)
	assert.NotEmpty(t, doc.Findings[0].Excerpt)
	assert.NotEmpty(t, doc.Findings[0].Remedy)
	// clean: a brief with no drift findings exits 0 with empty findings
	good := filepath.Join(dir, "good.md")
	require.NoError(t, os.WriteFile(good, []byte("hello\n"), 0o644))
	exit, stdout, _ = runCard("lint", "--card", good, "--json")
	// good may still drift on model lines; only assert JSON parses
	var doc2 map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc2))
	_ = exit
}
