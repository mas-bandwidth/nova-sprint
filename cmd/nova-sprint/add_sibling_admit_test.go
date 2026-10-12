package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The add command holds a brief that names a sibling path (../<name>) to the sibling
// rule at admission (docs/SPEC-CARD-CONTRACT.md, the SIBLINGS header): a brief naming
// ../serialize without a SIBLINGS header is refused with the path quoted and nothing is
// written; with the header naming the repository it is admitted.
func TestAddRefusesASiblingPathWithoutASIBLINGSHeader(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead --owner ada")
	ta.ok("add --stream s0 --count 2 --actor lead")
	header := "RESULT: c sha=0123456789ab tier: pro\nPATHS: internal/x/*.go, internal/x/*_test.go\nTEST: internal/x TestY\n"
	write := func(name, text string) string {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, name+".md")
		require.NoError(t, os.WriteFile(path, []byte(passingBrief(text)), 0o600))
		return path
	}

	noHeader := header + "\nTHE TASK. The harness expects the C++ reference corpus at ../serialize next to the checkout."
	code, out, stderr := ta.do("add --stream s1 --one --actor lead --brief-file " + write("sib", noHeader))
	assert.Equal(t, 2, code, "%s%s", out, stderr)
	assert.Contains(t, stderr, "LINT DRIFT card=sib check=sibling-path line=5: ../serialize")
	assert.Contains(t, stderr, "nova-sprint add REFUSED: 1 brief finding(s)")
	assert.False(t, ta.placed("sib"), "nothing written")

	withHeader := header + "\nSIBLINGS: mas-bandwidth/serialize@abc\nTHE TASK. The harness expects the C++ reference corpus at ../serialize next to the checkout."
	assert.Contains(t, ta.ok("add --stream s1 --one --actor lead --brief-file "+write("sibok", withHeader)), "MOVED sibok -> ready")
}
