package ci

import (
	"os"
	"path/filepath"
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
}
