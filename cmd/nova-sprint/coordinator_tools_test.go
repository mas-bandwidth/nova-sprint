package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryCoordinatorStopgapNamesTheVerbThatReplacesIt pins
// docs/COORDINATOR-TOOLS.md's register: every row names the nova verb that
// replaces its tool, or the card that will write it (its own rule, section
// "The register"). A row with neither is a tool the page leaves no way out of.
func TestEveryCoordinatorStopgapNamesTheVerbThatReplacesIt(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../docs/COORDINATOR-TOOLS.md")
	require.NoError(t, err)

	in, rows := false, 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "## ") {
			in = line == "## The tools in use"
			continue
		}
		if !in || !strings.HasPrefix(line, "| ") {
			continue
		}
		if strings.HasPrefix(line, "| Tool ") || strings.HasPrefix(line, "| --- ") {
			continue
		}
		cells := strings.Split(line, "|")
		require.Len(t, cells, 5, "a tool row is not three cells: %q", line)
		replaced := strings.TrimSpace(cells[3])
		assert.True(t,
			strings.Contains(replaced, "`") || strings.Contains(replaced, "card:"),
			"a tool row names no verb and no card: %q", line)
		rows++
	}
	assert.NotEmpty(t, rows, "docs/COORDINATOR-TOOLS.md section The tools in use holds no row")
}
