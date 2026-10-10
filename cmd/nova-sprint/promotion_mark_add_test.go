package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// add --land-protected founds a new stream and marks it for the protected branches
// of its repositories in the one step (docs/SPEC-SPRINT.md section 7,
// protected-bases-pb-b.w2; the fix promotion-mark-at-add, the seat ledger bug 14): a
// card cut on main or dev is admitted with no sentinel and no follow-up stream set,
// the mark and the first card are one atomic add, a mark that does not name the
// card's repository refuses it, and an existing stream is never re-marked.
func TestAddMarksNewStreamForProtectedBaseWithFirstCard(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	mark := "owner/repo"
	brief := func(id, base, repo string) string {
		return passingBrief("RESULT: " + id + " sha=000000000000 tier: flash\nKIND: fix\nREPO: " + repo + "\nBASE: " + base + "\n\nThe task. Add " + id + ".")
	}

	// a card cut on main founds the stream and is admitted by the mark it carries
	assert.Contains(t, ta.ok("add --stream on-main p1 --one --brief '"+brief("p1", "main", mark)+"' --land-protected "+mark), "MOVED p1 -> ready")
	assert.Contains(t, ta.ok("where --json"), `"Promotion":"`+mark+`"`, "the first card and stream mark are admitted together")
	assert.Contains(t, ta.ok("where"), "promotion: on-main (owner/repo)", "where shows the marked stream")

	// a card cut on dev founds a second stream the same way: the mark is what admits
	// it, so no sentinel and no follow-up stream set are needed
	assert.Contains(t, ta.ok("add --stream on-dev d1 --one --brief '"+brief("d1", "dev", mark)+"' --land-protected "+mark), "MOVED d1 -> ready")
	assert.Contains(t, ta.ok("where --json"), `"Promotion":"`+mark+`"`, "the dev stream is marked too")

	// a mark that names another repository does not admit the card: nothing written
	code, out, errs := ta.do("add --stream on-other o1 --one --brief '" + brief("o1", "main", "elsewhere/thing") + "' --land-protected " + mark)
	assert.NotEqual(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out+errs, "--land-protected "+mark+" does not mark repository elsewhere/thing")
	assert.False(t, ta.placed("o1"), "nothing written")

	// --land-protected marks a new stream only: one that already holds a card is refused
	code, out, errs = ta.do("add --stream on-main p2 --one --brief '" + brief("p2", "main", mark) + "' --land-protected " + mark)
	assert.NotEqual(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out+errs, "--land-protected marks a new stream only")
}
