package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLanderAcceptsNewFileUnderDirectoryPaths is the lander's reading of a directory
// PATHS entry (diffcheck.Covers; docs/SPEC-SPRINT.md section 7, the lander's checks):
// a directory entry (trailing /) covers every file under it, so a card whose brief
// names pkg/thing/ and whose head adds a new file there lands. One definition of what
// an entry covers is shared by the E12 check and the add/brief lint; before it,
// diffcheck.Outside matched only globs, so a directory entry covered nothing and the
// head was refused E12 although the directory that named it stood at the base.
func TestLanderAcceptsNewFileUnderDirectoryPaths(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	// the base holds the directory the card names, as paths-at-base reads it
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.files("the directory the card names", map[string]string{"pkg/thing/keep.txt": "keep\n"})
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	// the brief names the directory, a trailing / and all
	brief := writeNeedsBrief(t, t.TempDir(), "a", "RESULT: a tier: flash\nPATHS: pkg/thing/\nAdd pkg/thing/new.go.", "")
	r.ok("add --stream s1 a --one --brief-file " + brief)
	head := r.card("a", map[string]string{"pkg/thing/new.go": "package thing\n"})
	r.queued(map[string]string{"a": head}, "a")
	code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
	assert.Equal(t, 0, code, out+errs)
	assert.NotContains(t, errs, "(E12)")
	assert.Equal(t, map[string]string{"a": "landed/merged"}, r.places("a"))
	assert.Equal(t, "package thing", r.git(r.remote, "show", "main:pkg/thing/new.go"))
	r.clean()
}
