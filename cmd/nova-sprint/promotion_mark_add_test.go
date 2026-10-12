package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// add --land-protected is refused, exit 2, nothing written, quoting the flow: the mark is no
// landing permission, so a stream is never marked to land on a protected branch
// (docs/SPEC-SPRINT.md section 7, the protected branches).
func TestAddRefusesLandProtected(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	before := ta.applies()
	for _, line := range []string{
		"add --stream s1 p1 --one --land-protected owner/repo",
		"add --stream s1 p1 --one --land-protected any",
	} {
		code, out, errs := ta.do(line)
		assert.Equal(t, 2, code, "%s: %s%s", line, out, errs)
		assert.Contains(t, out+errs, sprint.LandingFlow, line)
		assert.Contains(t, out+errs, "nova-sprint stream set s1 --base sprint/s1", line)
		assert.False(t, ta.placed("p1"), "nothing written")
		assert.Equal(t, before, ta.applies(), "no store write")
	}
}
