package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// TestBriefFileOverTheBoundIsRefused checks that brief <id> --brief-file exits 2
// with the proper message when the file is over the bound, same as brief --dir.
func TestBriefFileOverTheBoundIsRefused(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)

	// Create a file with exactly MaxBriefBytes + 1 bytes (16385 bytes)
	overFile := filepath.Join(t.TempDir(), "over.md")
	briefText := passingBrief("handle the empty case") + "\n"
	for len(briefText) < store.MaxBriefBytes+1 {
		briefText += "padding words "
	}
	briefText = briefText[:store.MaxBriefBytes+1]
	require.NoError(t, os.WriteFile(overFile, []byte(briefText), 0o600))

	// Test --brief-file with over-bound file
	code, out, errs := ta.do("brief s1-1 --brief-file " + overFile)
	require.Equal(t, 2, code, "brief --brief-file should exit 2, out=%q errs=%q", out, errs)
	require.Contains(t, errs, "16385 bytes", "should mention actual size")
	require.Contains(t, errs, "16384 bytes", "should mention bound")
	require.Contains(t, errs, "shorten it", "should suggest remedy")

	// Test --brief with over-bound text (same size)
	code, out, errs = ta.do("brief s1-1 --brief '" + briefText + "'")
	require.Equal(t, 2, code, "brief --brief should exit 2, out=%q errs=%q", out, errs)
	require.Contains(t, errs, "16385 bytes", "should mention actual size")
	require.Contains(t, errs, "16384 bytes", "should mention bound")
	require.Contains(t, errs, "shorten it", "should suggest remedy")

	// Verify that a brief of exactly MaxBriefBytes is still taken
	exactFile := filepath.Join(t.TempDir(), "exact.md")
	exactText := briefText[:store.MaxBriefBytes]
	require.NoError(t, os.WriteFile(exactFile, []byte(exactText), 0o600))
	code, out, errs = ta.do("brief s1-1 --brief-file " + exactFile)
	require.Equal(t, 0, code, "brief --brief-file with exact bound should succeed, out=%q errs=%q", out, errs)
}
