package ci

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReadmeSaysWhatNovaSprintIs pins the one sentence pair both READMEs
// (nova-sprint and nova-tools) carry: nova-sprint is the opinionated system,
// nova-tools are the general tools that support it. The nova-tools side
// lives in another repository and is held as a proposed diff in docs/.
func TestReadmeSaysWhatNovaSprintIs(t *testing.T) {
	root := filepath.Join("..", "..")
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, rel))
		require.NoError(t, err)
		return strings.Join(strings.Fields(string(b)), " ")
	}

	readme := read("README.md")
	require.Contains(t, readme, "nova-sprint is the opinionated system")
	require.Contains(t, readme, "nova-tools are the general tools that support it")
	require.NotContains(t, readme, "unopinionated")

	diff := read("docs/NOVA-TOOLS-README.diff")
	require.Contains(t, diff, "nova-sprint is the opinionated system")
	require.Contains(t, diff, "nova-tools are the general tools that support it")

	// Verify that the proposed diff contains a well-formed hunk whose line
	// counts match the @@ header.
	diffRaw, err := os.ReadFile(filepath.Join(root, "docs/NOVA-TOOLS-README.diff"))
	require.NoError(t, err)

	lines := strings.Split(string(diffRaw), "\n")
	var foundHunk bool
	var expectedOld, expectedNew int
	var actualOld, actualNew int

	for _, line := range lines {
		if strings.HasPrefix(line, "@@ ") {
			require.False(t, foundHunk, "diff has single hunk")
			foundHunk = true
			parts := strings.Split(line, "@@")
			require.GreaterOrEqual(t, len(parts), 3, "well-formed hunk header")
			ranges := strings.Fields(parts[1])
			require.Len(t, ranges, 2, "hunk header specifies old and new ranges")

			oldSpec := strings.TrimPrefix(ranges[0], "-")
			oldParts := strings.Split(oldSpec, ",")
			if len(oldParts) == 2 {
				expectedOld, err = strconv.Atoi(oldParts[1])
				require.NoError(t, err)
			} else {
				expectedOld = 1
			}

			newSpec := strings.TrimPrefix(ranges[1], "+")
			newParts := strings.Split(newSpec, ",")
			if len(newParts) == 2 {
				expectedNew, err = strconv.Atoi(newParts[1])
				require.NoError(t, err)
			} else {
				expectedNew = 1
			}
			continue
		}

		if foundHunk {
			if len(line) == 0 {
				continue
			}
			switch line[0] {
			case ' ':
				actualOld++
				actualNew++
			case '-':
				actualOld++
			case '+':
				actualNew++
			}
		}
	}

	require.True(t, foundHunk, "found @@ hunk header")
	require.Equal(t, expectedOld, actualOld, "old line count matches @@ header")
	require.Equal(t, expectedNew, actualNew, "new line count matches @@ header")
}
