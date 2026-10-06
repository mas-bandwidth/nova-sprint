package ci

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// novaToolsReadmeDiff is the proposed nova-tools README change this card
// carries: nova-tools is its own repository (mas-bandwidth/nova-tools), so its
// side of the pair travels as a proposed diff held here for whoever owns that
// repository to apply, never committed as a docs/ .diff scratch file (the
// lander refuses one outside PATHS, E12).
const novaToolsReadmeDiff = `diff --git a/README.md b/README.md
index 4310181c3..35d6c9821 100644
--- a/README.md
+++ b/README.md
@@ -9,6 +9,9 @@ and harnesses work together efficiently. Exchange messages, organize work,
 find useful records, and check repeatable tasks—leaving more time and tokens
 for the work that needs thought.
 
+nova-sprint is the opinionated system; nova-tools are the general tools that support it.
+You do not need [nova-sprint](https://github.com/mas-bandwidth/nova-sprint) to use any of them.
+
 Choose the tool for the problem you have. Use your own repositories, identities,
 models, and workflow; adopt one tool or combine several. Humans are welcome to
 use and contribute too!
`

// TestReadmeSaysWhatNovaSprintIs pins the one sentence pair both READMEs
// (nova-sprint and nova-tools) carry: nova-sprint is the opinionated system,
// nova-tools are the general tools that support it. The nova-tools side lives
// in another repository and is held as the proposed diff above.
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

	require.Contains(t, novaToolsReadmeDiff, "nova-sprint is the opinionated system")
	require.Contains(t, novaToolsReadmeDiff, "nova-tools are the general tools that support it")

	// Verify that the proposed diff contains a well-formed hunk whose line
	// counts match the @@ header.
	atoi := func(s string) int {
		n, err := strconv.Atoi(s)
		require.NoError(t, err)
		return n
	}
	lines := strings.Split(novaToolsReadmeDiff, "\n")
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

			oldParts := strings.Split(strings.TrimPrefix(ranges[0], "-"), ",")
			if len(oldParts) == 2 {
				expectedOld = atoi(oldParts[1])
			} else {
				expectedOld = 1
			}

			newParts := strings.Split(strings.TrimPrefix(ranges[1], "+"), ",")
			if len(newParts) == 2 {
				expectedNew = atoi(newParts[1])
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
