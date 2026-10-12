package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// add admits a card cut on dev into a new stream with no mark, re-cutting it on the
// stream's branch sprint/<stream> and saying so on one NOTE quoting the landing flow
// (docs/SPEC-SPRINT.md section 7, the sprint branch): the --land-protected mark is no
// longer what admits a protected base. The mark still founds a promotion stream, and an
// existing stream is never re-marked.
func TestAddReCutsADevCardOntoTheStreamBranch(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	brief := func(id, base string) string {
		return passingBrief("RESULT: " + id + " sha=000000000000 tier: flash\nKIND: fix\nREPO: owner/repo\nBASE: " + base + "\n\nThe task. Add " + id + ".")
	}

	// a card cut on dev founds the stream and is admitted with no mark, re-cut on its branch
	out := ta.ok("add --stream on-dev d1 --one --brief '" + brief("d1", "dev") + "'")
	assert.Contains(t, out, "MOVED d1 -> ready", "the first card and the stream's branch are written together")
	assert.Contains(t, out, sprint.LandingFlow, "the NOTE quotes the landing flow")

	// --land-protected marks a new stream only: one that already holds a card is refused
	code, out, errs := ta.do("add --stream on-dev d2 --one --brief '" + brief("d2", "dev") + "' --land-protected owner/repo")
	assert.NotEqual(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out+errs, "--land-protected marks a new stream only")
}
