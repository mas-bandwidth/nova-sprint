package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 2026-10-18 (the v1.7 audit, N4): a wave is compared with itself, so two
// probable duplicates in one brief dir admit only the first; the second is a
// follow-up, and so admitted, when its brief names the first in DEPENDS-ON.
func TestTwoDuplicatesInOneWaveAdmitTheFirst(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	task := "THE TASK. Compare the wave with itself so two probable duplicates admit only the first.\n\nSTEP 1. nothing.\n"
	head := "REPO: mas-bandwidth/nova-sprint\nPATHS: docs/fixes.sexp\n"
	brief := "1. a card\n" + head + "\n" + task
	write := func(name, text string) string {
		t.Helper()
		path := filepath.Join(dir, name+".md")
		require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
		return path
	}
	read := func(path string) dupCard {
		t.Helper()
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		return dupCardOf(strings.TrimSuffix(filepath.Base(path), ".md"), string(b))
	}
	aPath := write("a", brief)
	bPath := write("b", brief)
	held, _ := dupHeld([]dupCard{read(aPath), read(bPath)}, nil, nil)
	assert.NotContains(t, held, "a", "the first of two duplicates is admitted")
	f, ok := held["b"]
	require.True(t, ok, "the second of two duplicates is held")
	assert.Equal(t, "a", f.Of, "the held card's finding names the first")
	var out strings.Builder
	dupPrint(&out, held)
	assert.Contains(t, out.String(), "DUPLICATE b:")
	assert.Contains(t, out.String(), "probable duplicate of a:")

	// b names a in DEPENDS-ON: it is a follow-up, so both are admitted.
	write("b", "1. a card\nDEPENDS-ON: a\n"+head+"\n"+task)
	held, _ = dupHeld([]dupCard{read(aPath), read(bPath)}, nil, nil)
	assert.Empty(t, held, "a follow-up on the earlier card is admitted")
}
