package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorkExampleOfflineFixture: card work-example-offline-fixture.
// example --dir writes tree files plus a recorded GraphQL replay fixture;
// verify --against proves the trees with no gh and no network, and import
// reads the fixture with no login.
func TestWorkExampleOfflineFixture(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "example")
	res := workMain(unreachable(t)).Run("example", "--dir", dir)
	require.Equal(t, 0, res.Code, "%s%s", res.Stdout, res.Stderr)
	for _, name := range []string{"tree.lisp", "tree-again.lisp", filepath.Join("calls", "call-01.json"), filepath.Join("calls", "call-02.json")} {
		_, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err, name)
	}
	ver := workMain(unreachable(t)).Run("verify", "--tree", filepath.Join(dir, "tree.lisp"), "--against", filepath.Join(dir, "tree-again.lisp"))
	require.Equal(t, 0, ver.Code, "%s%s", ver.Stdout, ver.Stderr)

	imp := workMain(unreachable(t)).Run("import", "--org", "acme", "--repo", "acme/widgets", "--fixture", filepath.Join(dir, "calls"), "--out", filepath.Join(t.TempDir(), "out.lisp"))
	require.Equal(t, 0, imp.Code, "%s%s", imp.Stdout, imp.Stderr)

	assert.Contains(t, workTool(unreachable(t)).Banner(), "nova-work example --dir")
}
