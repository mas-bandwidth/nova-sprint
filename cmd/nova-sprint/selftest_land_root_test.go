package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The selftest's inner apps keep the clones of their landings under the fresh
// directory the selftest makes, not under the caller's cache: the selftest
// removes that directory on success, so a clone left in the cache would have
// its .git pointing into a directory that is gone.
func TestTheSelftestKeepsLandClonesUnderItsDir(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	root, err := selftestApp(d, nil).landRoot()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(d, "land"), root)
	rel, err := filepath.Rel(d, root)
	require.NoError(t, err)
	assert.False(t, rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)), "the selftest's land root %s is under its fresh directory %s", root, d)
}
