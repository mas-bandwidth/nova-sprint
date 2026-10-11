package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/roadmapdoc"
)

// testRoadmapSexp is a smallest roadmap that decodes: one group and one item, so a
// defer appends its items beside them.
const testRoadmapSexp = `(roadmap "v1"
 :title "test roadmap"
 :text "test"
 :groups ((group "sprint" :title "The sprint machine" :text "new verbs"))
 :items ((item "existing" :group "sprint" :title "Existing" :text "already here" :date "2026-10-10")))
`

// roadmapDir writes a checkout holding docs/roadmap.sexp and returns its root.
func roadmapDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docs", "roadmap.sexp"), []byte(testRoadmapSexp), 0o644))
	return dir
}

// deferApp is a sprint where x2 waits on x1 and x3 on x2, so x2 and x3 are the
// waiting cards and x1 is ready. Each brief names the repository, so the stream
// records it for the --repo form.
func deferApp(t *testing.T) *testApp {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("add --stream s1 x1 --one --brief-file " + writeBrief(t, "THE TASK. Do x1. It is the need.\nREPO: mas-bandwidth/nova-sprint"))
	ta.ok("add --stream s1 x2 --one --needs x1 --brief-file " + writeBrief(t, "THE TASK. Defer x2 to the roadmap. It waits on x1.\nREPO: mas-bandwidth/nova-sprint"))
	ta.ok("add --stream s1 x3 --one --needs x2 --brief-file " + writeBrief(t, "THE TASK. Defer x3 to the roadmap. It waits on x2.\nREPO: mas-bandwidth/nova-sprint"))
	return ta
}

// A defer writes the named waiting cards' whole briefs into the roadmap data and
// drops them from the store, and --expect checks the count. The stream form names
// every waiting card of the stream; a ready card is left alone.
func TestDeferWritesWaitingCardsToTheRoadmapAndDropsThem(t *testing.T) {
	t.Parallel()
	ta := deferApp(t)
	dir := roadmapDir(t)

	out := ta.ok("defer --stream s1 --repo-dir " + dir + " --reason 'deferred to the roadmap' --expect 2")
	assert.Contains(t, out, "DEFER OK")
	assert.Contains(t, out, "x2 waiting -> off the table")
	assert.Contains(t, out, "x3 waiting -> off the table")

	for _, id := range []string{"x2", "x3"} {
		assert.Contains(t, ta.ok("card --fields "+id), "outcome=dropped", "%s was not dropped", id)
		assert.Contains(t, ta.ok("card --fields "+id), "dropped_from=waiting", "%s was not dropped from waiting", id)
	}
	require.NotNil(t, ta.primary("x1"), "x1 was not waiting and should stay on the table")

	raw, err := os.ReadFile(filepath.Join(dir, "docs", "roadmap.sexp"))
	require.NoError(t, err)
	doc, err := roadmapdoc.Decode("docs/roadmap.sexp", raw)
	require.NoError(t, err, "the defer wrote a roadmap that does not decode")
	require.Len(t, doc.Items, 3, "the existing item plus x2 and x3")
	var titles []string
	for _, it := range doc.Items {
		titles = append(titles, it.Title)
	}
	assert.Contains(t, titles, "Defer x2 to the roadmap")
	assert.Contains(t, titles, "Defer x3 to the roadmap")
	for _, it := range doc.Items {
		if it.ID == "x2" || it.ID == "x3" {
			assert.Contains(t, it.Text, "THE TASK. Defer "+it.ID+" to the roadmap", "the whole brief is written: %q", it.Text)
			assert.Contains(t, it.Text, "It waits on", "the whole brief is written: %q", it.Text)
			assert.Equal(t, "after v1.4", it.Release)
			assert.Contains(t, it.Origin, "card moved out of the sprint")
			assert.Equal(t, "sprint", it.Group)
		}
	}
	ta.clean()
}

// --expect refuses a set that is not the count named, and nothing changes: the
// cards stay waiting and the roadmap is untouched.
func TestDeferExpectChecksTheCount(t *testing.T) {
	t.Parallel()
	ta := deferApp(t)
	dir := roadmapDir(t)

	code, out, errs := ta.do("defer --stream s1 --repo-dir " + dir + " --reason 'deferred to the roadmap' --expect 3")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "--expect 3")
	assert.NotContains(t, out, "DEFER OK")

	require.NotNil(t, ta.primary("x2"), "a refused defer must not drop x2")
	require.NotNil(t, ta.primary("x3"), "a refused defer must not drop x3")
	raw, err := os.ReadFile(filepath.Join(dir, "docs", "roadmap.sexp"))
	require.NoError(t, err)
	assert.Equal(t, testRoadmapSexp, string(raw), "a refused defer must not write the roadmap")
	ta.clean()
}

// A named card that is not waiting is refused: only a waiting card defers.
func TestDeferRefusesACardThatIsNotWaiting(t *testing.T) {
	t.Parallel()
	ta := deferApp(t)
	dir := roadmapDir(t)

	code, _, errs := ta.do("defer x1 --repo-dir " + dir + " --reason 'deferred to the roadmap'")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "x1")
	assert.Contains(t, errs, "not waiting")
	require.NotNil(t, ta.primary("x1"), "x1 must stay on the table")
	ta.clean()
}

// The repository form names every waiting card of the streams recording the
// repository, and --expect checks their count.
func TestDeferByRepository(t *testing.T) {
	t.Parallel()
	ta := deferApp(t)
	dir := roadmapDir(t)

	out := ta.ok("defer --repo mas-bandwidth/nova-sprint --repo-dir " + dir + " --reason 'deferred to the roadmap' --expect 2")
	assert.Contains(t, out, "DEFER OK")
	assert.Contains(t, out, "x2 waiting -> off the table")
	assert.Contains(t, out, "x3 waiting -> off the table")
	require.NotNil(t, ta.primary("x1"))
	ta.clean()
}
