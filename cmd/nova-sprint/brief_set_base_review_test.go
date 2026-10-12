package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// setBaseWorld is a stream s1 holding a waiting card w, a working card x and a
// review card y, with an --ids-file naming all three. A card with an attempt
// dealt is edited in place (sprint.Brief, briefInPlace): its plan reads the
// attempt's work card and the read table, so the selector step must load them.
func setBaseWorld(t *testing.T) (*testApp, string) {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	ta.ok("add --stream s1 x --one --brief-file " + writeNeedsBrief(t, dir, "x", "the working work", ""))
	ta.ok("add --stream s1 y --one --brief-file " + writeNeedsBrief(t, dir, "y", "the review work", ""))
	ta.ok("add --stream s1 w --one --brief-file " + writeNeedsBrief(t, dir, "w", "the waiting work", "x"))
	ta.deal(2) // x and y are dealt; w waits on x
	ta.ok("take --as m1 x.w1@1 y.w1@1")
	ta.ok("finish --as m1 y.w1@1 --head " + landHead + " --report done")
	require.Equal(t, sprint.Waiting, ta.primary("w").Col, "w waits on x")
	require.Equal(t, sprint.Working, ta.primary("x").Col, "x is working")
	require.Equal(t, sprint.Review, ta.primary("y").Col, "y is in review")
	ids := filepath.Join(t.TempDir(), "ids")
	require.NoError(t, os.WriteFile(ids, []byte("w\nx\ny\n"), 0o644))
	return ta, ids
}

// brief --ids-file --set-base over a waiting, a working and a review card: the
// review and working cards' in-place edit reads the attempt's work card and the
// read table. The selector step used to load the work table alone, so the plan
// dereferenced a nil readers table and panicked before anything was written; now
// every selected card's BASE: is moved, and --dry-run lists the same three and
// writes nothing.
func TestBriefSetBaseByIDsFileMovesEverySelectedCardsBase(t *testing.T) {
	t.Parallel()
	ta, ids := setBaseWorld(t)

	applies := ta.applies()
	out := ta.dry("brief --ids-file " + ids + " --set-base release-1 --dry-run")
	assert.Contains(t, out, "BRIEF OK DRY-RUN selected=3", out)
	assert.Equal(t, applies, ta.applies(), "a dry run wrote")
	for _, id := range []string{"w", "x", "y"} {
		assert.NotContains(t, ta.primary(id).F("brief"), "BASE: release-1", "%s is untouched by a dry run", id)
	}

	out = ta.ok("brief --ids-file " + ids + " --set-base release-1")
	assert.Contains(t, out, "BRIEF OK moved=3 refused=0", out)
	for _, id := range []string{"w", "x", "y"} {
		assert.Contains(t, ta.primary(id).F("brief"), "\nBASE: release-1\n", "%s's BASE moved", id)
	}
	ta.clean()
}

// The same selection through --stream --set-base, dry and applied: a stream's
// cards are selected by their row, and a review card's BASE: moves with the rest.
func TestBriefSetBaseByStreamMovesEverySelectedCardsBase(t *testing.T) {
	t.Parallel()
	ta, _ := setBaseWorld(t)

	out := ta.ok("brief --stream s1 --set-base release-1 --dry-run")
	assert.Contains(t, out, "BRIEF OK DRY-RUN selected=3", out)
	for _, id := range []string{"w", "x", "y"} {
		assert.NotContains(t, ta.primary(id).F("brief"), "BASE: release-1", "%s is untouched by a dry run", id)
	}

	out = ta.ok("brief --stream s1 --set-base release-1")
	assert.Contains(t, out, "BRIEF OK moved=3 refused=0", out)
	for _, id := range []string{"w", "x", "y"} {
		assert.Contains(t, ta.primary(id).F("brief"), "\nBASE: release-1\n", "%s's BASE moved", id)
	}
	ta.clean()
}
