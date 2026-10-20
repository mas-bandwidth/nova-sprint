package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The lander's E12 accepts a file outside the card's PATHS whose every hunk only follows
// the card's own rename, and the batch's LAND line names it as fallout beside the scope
// amendments (tla/Fallout.tla, Land; docs/SPEC-SPRINT.md section 7, the fallout sweep).
func TestTheLandLineNamesFallout(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	require.NoError(t, os.MkdirAll(filepath.Join(r.worker, "docs"), 0o755))
	r.commit("a.go", "package a\n\nfunc OldName() {}\n", "a.go at the base")
	r.commit("docs/ref.md", "# Reference\n\nCall OldName here.\n", "the doc at the base")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	brief := writeNeedsBrief(t, t.TempDir(), "a", "RESULT: a tier: flash\nPATHS: a.go", "")
	r.ok("add --stream s1 a --one --brief-file " + brief)
	head := r.card("a", map[string]string{
		"a.go":        "package a\n\nfunc NewName() {}\n",
		"docs/ref.md": "# Reference\n\nCall NewName here.\n",
	})
	r.queued(map[string]string{"a": head}, "a")
	code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
	require.Equal(t, 0, code, out+errs)
	assert.NotContains(t, errs, "(E12)")
	assert.Contains(t, out, "fallout=a:docs/ref.md", "the fallout file is named on the LAND line")
	assert.NotContains(t, out, "scope=a:docs/ref.md", "fallout is its own list, not a scope amendment")
	assert.Equal(t, map[string]string{"a": "landed/merged"}, r.places("a"))
	r.clean()
}
