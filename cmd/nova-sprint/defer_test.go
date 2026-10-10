package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/roadmapdoc"
)

// deferRoadmap is a small roadmap with one group and one item, its comment and its wrapping
// the kind a living roadmap has.
const deferRoadmap = `; a roadmap of its own
(roadmap "v1"
 :title "A roadmap"
 :text "The page."
 :groups
 ((group "sprint" :title "Sprint" :text "The sprint."))
 :items
 (
  ; the sprint
  (item "seed" :group "sprint" :title "Seed" :text "A seed
    item." :date "2026-10-01")))
`

// deferTable is a sprint with one ready card (x1) and two waiting on it (b, c) in stream
// s2, and a roadmap file beside it.
func deferTable(t *testing.T) (*testApp, string) {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	ta.ok("add --one --stream s1 x1 --brief-file " + writeHeaderBrief(t, dir, "x1", "-", "internal/x1.go"))
	ta.ok("add --one --stream s2 b --brief-file " + writeHeaderBrief(t, dir, "b", "x1", "internal/b.go"))
	ta.ok("add --one --stream s2 c --brief-file " + writeHeaderBrief(t, dir, "c", "x1", "internal/c.go"))
	file := filepath.Join(t.TempDir(), "roadmap.sexp")
	require.NoError(t, os.WriteFile(file, []byte(deferRoadmap), 0o644))
	return ta, file
}

func deferDoc(t *testing.T, file string) *roadmapdoc.Doc {
	t.Helper()
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	doc, err := roadmapdoc.Decode(file, data)
	require.NoError(t, err, "the roadmap after the call:\n%s", data)
	return doc
}

// defer writes each named waiting card into the roadmap with its whole brief and drops it
// from the store; the rest of the file is as it was.
func TestDeferWritesTheWholeBriefsToTheRoadmapAndDropsTheCards(t *testing.T) {
	t.Parallel()
	ta, file := deferTable(t)
	briefB, briefC := ta.primary("b").F("brief"), ta.primary("c").F("brief")
	require.NotEmpty(t, briefB)
	require.NotEqual(t, briefB, briefC)

	out := ta.ok("defer b c --into sprint --release v1.3 --expect 2 --file " + file)
	assert.Contains(t, out, "b", "defer printed no move for b: %s", out)

	doc := deferDoc(t, file)
	require.Len(t, doc.Items, 3)
	assert.Equal(t, "seed", doc.Items[0].ID)
	for i, want := range []struct{ id, brief string }{{"b", briefB}, {"c", briefC}} {
		it := doc.Items[i+1]
		assert.Equal(t, want.id, it.ID)
		assert.Equal(t, "sprint", it.Group)
		assert.Equal(t, "v1.3", it.Release)
		assert.Equal(t, want.brief, it.Text, "the item for %s is not its whole brief", want.id)
		assert.Contains(t, ta.ok("card --fields "+want.id), "outcome=dropped", "%s is still on the table", want.id)
	}
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(data), deferRoadmap[:strings.LastIndex(deferRoadmap, ")))")+1]), "the file before the new items changed:\n%s", data)
	// the card that was not named stays where it was
	assert.Contains(t, ta.ok("card --fields x1"), "place=s1:ready")
}

// defer takes the waiting cards of a stream; --expect checks the count, and a call that
// is refused writes nothing.
func TestDeferTakesAStreamAndChecksTheCount(t *testing.T) {
	t.Parallel()
	ta, file := deferTable(t)

	code, out, errs := ta.do("defer --stream s2 --into sprint --expect 3 --file " + file)
	require.NotEqual(t, 0, code, "a wrong --expect: %s%s", out, errs)
	assert.Contains(t, errs, "selects 2", "a wrong --expect: %s", errs)
	assert.Equal(t, deferRoadmap, readFile(t, file), "a refused defer wrote the roadmap")
	assert.Contains(t, ta.ok("card --fields b"), "place=s2:waiting")

	out = ta.ok("defer --stream s2 --into sprint --dry-run --file " + file)
	assert.Contains(t, out, "2 waiting card(s)", "the dry run: %s", out)
	assert.Equal(t, deferRoadmap, readFile(t, file), "a dry run wrote the roadmap")
	assert.Contains(t, ta.ok("card --fields c"), "place=s2:waiting", "a dry run dropped c")

	ta.ok("defer --stream s2 --into sprint --expect 2 --file " + file)
	assert.Len(t, deferDoc(t, file).Items, 3)
	assert.Contains(t, ta.ok("card --fields b"), "outcome=dropped")
	assert.Contains(t, ta.ok("card --fields c"), "outcome=dropped")
}

// Only a waiting card is deferred, and a call with one that is not refuses the whole call.
func TestDeferRefusesACardThatIsNotWaitingAndWritesNothing(t *testing.T) {
	t.Parallel()
	ta, file := deferTable(t)
	for _, line := range []string{
		"defer b x1 --into sprint --file " + file, // x1 is ready
		"defer b ghost --into sprint --file " + file,
		"defer b --into nowhere --file " + file,
		"defer b --file " + file,
		"defer --file " + file + " --into sprint",
		"defer --repo owner/name --into sprint --file " + file, // a repository wants --expect
	} {
		code, out, errs := ta.do(line)
		require.NotEqual(t, 0, code, "%s: %s%s", line, out, errs)
		assert.Contains(t, errs, "REFUSED", line)
		assert.Equal(t, deferRoadmap, readFile(t, file), "%s wrote the roadmap", line)
		assert.Contains(t, ta.ok("card --fields b"), "place=s2:waiting", "%s moved b", line)
	}
}

// A card another waiting card still needs is refused by the drop; the roadmap then holds
// no item for it, and the cards that did leave have theirs.
func TestDeferLeavesNoItemForACardThatStaysOnTheTable(t *testing.T) {
	t.Parallel()
	ta, file := deferTable(t)
	ta.ok("add --one --stream s2 d --brief-file " + writeHeaderBrief(t, t.TempDir(), "d", "c", "internal/d.go"))

	code, out, errs := ta.do("defer c --into sprint --file " + file)
	require.NotEqual(t, 0, code, "defer of a needed card: %s%s", out, errs)
	assert.Equal(t, deferRoadmap, readFile(t, file), "an item stands for a card that is still on the table")
	assert.Contains(t, ta.ok("card --fields c"), "place=s2:waiting")

	// the drop is all or none: c is refused (d, not named, still needs it), so b stays
	// too, and the roadmap is as it was
	code, out, errs = ta.do("defer c b --into sprint --file " + file)
	require.NotEqual(t, 0, code, "one card was refused: %s%s", out, errs)
	assert.Equal(t, deferRoadmap, readFile(t, file), "the roadmap changed for cards that did not leave\n%s%s", out, errs)
	assert.Contains(t, ta.ok("card --fields b"), "place=s2:waiting")

	// naming the dependant with it takes both: the drop sees the need is inside the call
	ta.ok("defer c d --into sprint --file " + file)
	ids := []string{}
	for _, it := range deferDoc(t, file).Items {
		ids = append(ids, it.ID)
	}
	assert.Equal(t, []string{"seed", "c", "d"}, ids)
	assert.Contains(t, ta.ok("card --fields d"), "outcome=dropped")
}

// The brief is carried exactly: quotes, backslashes, line ends and the blank lines of a
// brief, and the comments and wrapping of the file around it.
func TestInsertRoadmapItemsCarriesABriefExactly(t *testing.T) {
	t.Parallel()
	brief := "RESULT: x sha=000\nTHE TASK. Say \"hi\" \\ and \\\" and a\n\n  indented line;\t(not a comment)\n"
	got, err := insertRoadmapItems("r.sexp", []byte(deferRoadmap), []deferItem{{ID: "x", Group: "sprint", Title: "X", Text: brief, Why: "w", Date: "2026-10-10", Origin: "o"}})
	require.NoError(t, err)
	doc, err := roadmapdoc.Decode("r.sexp", got)
	require.NoError(t, err)
	require.Len(t, doc.Items, 2)
	assert.Equal(t, brief, doc.Items[1].Text)
	assert.Contains(t, string(got), "; the sprint\n", "a comment of the file was lost")

	_, err = insertRoadmapItems("r.sexp", []byte(deferRoadmap), []deferItem{{ID: "seed", Group: "sprint", Title: "X", Text: "t", Why: "w", Date: "2026-10-10", Origin: "o"}})
	require.Error(t, err, "an id already in use was added")
	_, err = insertRoadmapItems("r.sexp", []byte(deferRoadmap), []deferItem{{ID: "y", Group: "nope", Title: "X", Text: "t", Why: "w", Date: "2026-10-10", Origin: "o"}})
	require.Error(t, err, "a group that is not there was used")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}
