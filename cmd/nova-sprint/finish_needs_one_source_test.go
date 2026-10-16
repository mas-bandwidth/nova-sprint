package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/ntable"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// finishWorld is stream a with "work" taken by m1 and working, and "dep" ready behind it,
// not landed: a working card ready to gain a need (the shape of the fleet's stuck
// finishes of 2026-10-15). Nothing ticks after the need is written, so the card is
// still working when its finish comes.
func finishWorld(t *testing.T) *testApp {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --one --stream a work --brief-file " + writeBrief(t, "work"))
	ta.ok("add --one --stream a dep --brief-file " + writeBrief(t, "dep"))
	ta.deal(1)
	ta.ok("take --as m1 work.w1@1")
	require.Equal(t, sprint.Working, ta.primary("work").Col, "work is working")
	return ta
}

// TestFinishAgreesWithTheNeedsRecord: the needs field is the card's one record of what
// it waits for, and the finish ends where it says, as the lifecycle judges
// (tla/SprintTables.tla FinishAgreesWithLifecycle, WaitsAfterWithinNeeds,
// FinishRefusalsBounded).
func TestFinishAgreesWithTheNeedsRecord(t *testing.T) {
	t.Parallel()

	// (a) the go-table case: a need gained while working, then needs set --clear: the
	// finish goes to review on the first try and the marker is gone
	t.Run("a need gained then cleared finishes to review", func(t *testing.T) {
		t.Parallel()
		ta := finishWorld(t)
		out := ta.ok("needs set work --needs dep")
		assert.Contains(t, out, "working, waits for dep after this attempt")
		assert.Equal(t, "dep", ta.primary("work").F(sprint.FieldWaitsAfter))

		out = ta.ok("needs set work --clear")
		assert.NotContains(t, out, "waits for", "--clear prints no wait")
		c := ta.primary("work")
		assert.Equal(t, "", c.F("needs"))
		assert.Equal(t, "", c.F(sprint.FieldWaitsAfter), "the marker left with the need")
		ta.clean()

		code, out, errs := ta.do("finish --as m1 work.w1@1 --head " + strings.Repeat("a", 40) + " --report done")
		require.Equal(t, 0, code, out+errs)
		assert.Contains(t, out, "work working -> review")
		assert.Equal(t, sprint.Review, ta.primary("work").Col)
	})

	// (b) the b3 case: a DEPENDS-ON rewrite gives the need, a second brief empties it
	t.Run("a depends-on rewrite emptied finishes to review", func(t *testing.T) {
		t.Parallel()
		ta := finishWorld(t)
		dir := t.TempDir()
		out := ta.ok("brief work --replace-in-flight --brief-file " + writeHeaderBrief(t, dir, "work", "dep", "internal/work.go"))
		assert.Contains(t, out, "waits for dep after this attempt")
		assert.Equal(t, "dep", ta.primary("work").F(sprint.FieldWaitsAfter))
		ta.ok("brief work --replace-in-flight --needs-from-brief --brief-file " + writeHeaderBrief(t, dir, "work", "-", "internal/work.go"))
		c := ta.primary("work")
		assert.Equal(t, "", c.F("needs"), "the text took the needs whole")
		assert.Equal(t, "", c.F(sprint.FieldWaitsAfter), "the marker stays inside the needs")

		code, out, errs := ta.do("finish --as m1 work.w1@1 --head " + strings.Repeat("b", 40) + " --report done")
		require.Equal(t, 0, code, out+errs)
		assert.Contains(t, out, "work working -> ready", "the brief generation moved: the attempt goes round again, never to waiting")
		assert.Equal(t, sprint.Ready, ta.primary("work").Col)
	})

	// (c) a named need still open: the finish goes to waiting, as before
	t.Run("an open need finishes to waiting", func(t *testing.T) {
		t.Parallel()
		ta := finishWorld(t)
		ta.ok("needs set work --needs dep")
		code, out, errs := ta.do("finish --as m1 work.w1@1 --head " + strings.Repeat("c", 40) + " --report done")
		require.Equal(t, 0, code, out+errs)
		assert.Contains(t, out, "working -> waiting")
		c := ta.primary("work")
		assert.Equal(t, sprint.Waiting, c.Col)
		assert.Equal(t, "dep", c.F("needs"), "it waits for what its needs name")
	})

	// (d) the stale record already stored: check names it, the finish goes to review
	t.Run("a stale record is named by check and finishes to review", func(t *testing.T) {
		t.Parallel()
		ta := finishWorld(t)
		ta.seed(nil, nil, []ntable.BatchMemberEntry{edit("work", map[string]string{sprint.FieldWaitsAfter: "dep"})})
		code, out, errs := ta.do("check")
		assert.Equal(t, 1, code, out+errs)
		assert.Contains(t, out+errs, "work is working and its waits_after names dep, which its needs do not")

		code, out, errs = ta.do("finish --as m1 work.w1@1 --head " + strings.Repeat("d", 40) + " --report done")
		require.Equal(t, 0, code, out+errs)
		assert.Contains(t, out, "work working -> review")
		assert.Equal(t, "", ta.primary("work").F(sprint.FieldWaitsAfter))
	})

	// (e) a finish the lifecycle refuses is counted: past the bound, one judgment
	// naming card, attempt and head; a seventh raises none
	t.Run("a finish refused past the bound raises one judgment", func(t *testing.T) {
		t.Parallel()
		ta := finishWorld(t)
		// a server-default hold on the working primary: the lifecycle refuses any move of it
		ta.seed(nil, nil, []ntable.BatchMemberEntry{edit("work", map[string]string{
			sprint.FieldHeld: "2026-10-15T00:00:00Z", "held_by": sprint.ServerDefaultActor, "held_reason": "test", "decision_held_from": sprint.Review})})
		head := strings.Repeat("e", 40)
		raised := 0
		for i := 1; i <= 7; i++ {
			code, out, errs := ta.do("finish --as m1 work.w1@1 --head " + head + " --report done")
			require.Equal(t, 1, code, "finish %d: %s%s", i, out, errs)
			assert.Contains(t, errs, "release the server-default hold", "finish %d", i)
			if strings.Contains(out+errs, "JUDGMENT raised") {
				raised++
				assert.Equal(t, 6, i, "raised at the refusal that passes the bound")
				assert.Contains(t, out+errs, sprint.NFinishRefused)
			}
		}
		assert.Equal(t, 1, raised, "one judgment for the attempt")
		n := 0
		for _, g := range ta.inboxGroups() {
			if g.Type == sprint.NFinishRefused {
				n++
			}
		}
		assert.Equal(t, 1, n, "one group in the inbox")
		inbox := ta.ok("inbox")
		assert.Contains(t, inbox, head, "the head offered")
		assert.Contains(t, inbox, "work.w1")
		assert.Contains(t, inbox, "attempt 1")
	})
}
