package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProcessorDocNamesEachMetaphorAndItsMechanism pins docs/PROCESSOR.md's
// mapping table against its own rule: a MECHANISM row names the file or card
// that implements it, and a METAPHOR row names no code path. A row that marks
// a metaphor as a mechanism, or a mechanism as only a metaphor, fails here.
func TestProcessorDocNamesEachMetaphorAndItsMechanism(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../docs/PROCESSOR.md")
	require.NoError(t, err)

	in, rows := false, 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "## ") {
			in = line == "## Mapping"
			continue
		}
		if !in || !strings.HasPrefix(line, "| ") {
			continue
		}
		if strings.HasPrefix(line, "| concept ") || strings.HasPrefix(line, "| --- ") {
			continue
		}
		cells := strings.Split(line, "|")
		require.Len(t, cells, 5, "a mapping row is not three cells: %q", line)
		mark := strings.TrimSpace(cells[2])
		names := strings.TrimSpace(cells[3])
		switch mark {
		case "MECHANISM":
			assert.Contains(t, names, "`", "a MECHANISM row names no file or card: %q", line)
		case "METAPHOR":
			assert.Equal(t, "none", names, "a METAPHOR row names a code path: %q", line)
		default:
			t.Errorf("a mapping row marks neither MECHANISM nor METAPHOR: %q", line)
		}
		rows++
	}
	assert.NotEmpty(t, rows, "docs/PROCESSOR.md section Mapping holds no row")
}
