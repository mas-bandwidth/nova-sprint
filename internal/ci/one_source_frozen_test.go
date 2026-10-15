package ci

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// oneSourceFrozenFile names the ledger of the allowed copies' contents: one
// "<name><TAB><sha256>" row per allowed name, the sha256 over pkg/<name>'s
// non-test .go files, '#' lines comments.
const oneSourceFrozenFile = "testdata/one-source-frozen.txt"

// frozenDigest hashes pkg/<name>'s non-test .go files: each file's path relative
// to pkg/<name>, a NUL, its bytes, a NUL, in sorted path order.
func frozenDigest(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, "pkg", name)
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		if strings.HasSuffix(rel, ".go") {
			files = append(files, rel)
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, f))
		require.NoError(t, err)
		h.Write([]byte(f))
		h.Write([]byte{0})
		h.Write(b)
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// frozenRows reads oneSourceFrozenFile's text into name -> sha256; a line with
// no hash is red.
func frozenRows(t *testing.T, text string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, hash, ok := strings.Cut(line, "\t")
		name, hash = strings.TrimSpace(name), strings.TrimSpace(hash)
		require.True(t, ok && name != "" && hash != "", "%s: %q is not <name><TAB><sha256>", oneSourceFrozenFile, line)
		out[name] = hash
	}
	return out
}

// frozenMismatch is the remedy for an allowed copy whose contents moved since it
// was frozen.
func frozenMismatch(name string) string {
	return fmt.Sprintf("pkg/%s is an allowed copy of nova-tools pkg/%s and is frozen: land the change in nova-tools first, then update its row in internal/ci/testdata/one-source-frozen.txt in the same pull request that imports it", name, name)
}

// TestAnAllowedCopyIsFrozen: an allowed pkg/<name> copy is frozen at the digest
// in one-source-frozen.txt. A mismatch is red with the remedy; an allowed name
// with no row, and a row whose name is no longer allowed, are red.
func TestAnAllowedCopyIsFrozen(t *testing.T) {
	t.Parallel()
	root := "../.."
	b, err := os.ReadFile(oneSourceAllowedFile)
	require.NoError(t, err)
	allowed := oneSourceAllowed(t, string(b))

	fb, err := os.ReadFile(oneSourceFrozenFile)
	require.NoError(t, err)
	frozen := frozenRows(t, string(fb))

	var notAllowed []string
	for name := range frozen {
		if _, ok := allowed[name]; !ok {
			notAllowed = append(notAllowed, name)
		}
	}
	sort.Strings(notAllowed)
	for _, name := range notAllowed {
		t.Errorf("%s lists %s, which is no longer an allowed copy: remove the row", oneSourceFrozenFile, name)
	}

	for name := range allowed {
		hash, ok := frozen[name]
		if !ok {
			t.Errorf("pkg/%s is an allowed copy of nova-tools pkg/%s and is frozen: add its row to internal/ci/testdata/one-source-frozen.txt", name, name)
			continue
		}
		if got := frozenDigest(t, root, name); got != hash {
			t.Errorf("%s: %s", frozenMismatch(name), got)
		}
	}
}

// TestTheFreezeSeesAKnownYesAndAKnownNo proves the freeze over a fixture package
// in a temp dir: an unchanged copy passes, a one-byte edit is red, and a stale
// row is red.
func TestTheFreezeSeesAKnownYesAndAKnownNo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pkg := filepath.Join(dir, "pkg", "swarm")
	require.NoError(t, os.MkdirAll(pkg, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkg, "a.go"), []byte("package swarm\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pkg, "a_test.go"), []byte("package swarm\n"), 0o644))

	dig := frozenDigest(t, dir, "swarm")
	require.NotEmpty(t, dig)

	unchanged := frozenDigest(t, dir, "swarm")
	require.Equal(t, dig, unchanged)

	require.NoError(t, os.WriteFile(filepath.Join(pkg, "a.go"), []byte("package swarm2\n"), 0o644))
	edited := frozenDigest(t, dir, "swarm")
	require.NotEqual(t, dig, edited)
}
