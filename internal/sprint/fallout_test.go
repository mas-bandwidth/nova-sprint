package sprint_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// Fallout is the card's own change in a file just outside its PATHS
// (tla/Fallout.tla, Land; docs/SPEC-SPRINT.md section 7). A rename or a deletion is
// followed by its stale references: a hunk that only replaces the old name with the new
// one, or drops a line whose only reference is a removed name, is in scope; a hunk that
// also changes something of its own is not.
func TestAHunkThatOnlyFollowsTheCardsRenameIsInScope(t *testing.T) {
	t.Parallel()
	falloutHunks(t)
}

// TestFallout runs the same table under the group's name, so the group's one -run Fallout
// command reaches every fallout test.
func TestFallout(t *testing.T) {
	t.Parallel()
	t.Run("TestAHunkThatOnlyFollowsTheCardsRenameIsInScope", func(t *testing.T) {
		t.Parallel()
		falloutHunks(t)
	})
}

func falloutHunks(t *testing.T) {
	t.Helper()
	const renameGo = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,3 +1,3 @@
 package a
 
-func OldName() {}
+func NewName() {}
`
	const goneGo = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,3 +1,2 @@
 package a
 
-func Gone() {}
`
	const renamedPath = `diff --git a/old.go b/new.go
similarity index 100%
rename from old.go
rename to new.go
diff --git a/docs/ref.md b/docs/ref.md
--- a/docs/ref.md
+++ b/docs/ref.md
@@ -1,2 +1,2 @@
 # Reference
-See old.go for the call.
+See new.go for the call.
`
	for _, tc := range []struct {
		name                      string
		paths                     []string
		diff                      string
		amended, fallout, refused []string
	}{
		{"a Go rename followed in another package and a README outside PATHS", []string{"a.go"}, renameGo + `diff --git a/other/use.go b/other/use.go
--- a/other/use.go
+++ b/other/use.go
@@ -1,2 +1,2 @@
 package other
-// OldName does the work.
+// NewName does the work.
diff --git a/guide/README.md b/guide/README.md
--- a/guide/README.md
+++ b/guide/README.md
@@ -1,2 +1,2 @@
 # Readme
-Use OldName here.
+Use NewName here.
`, nil, []string{"other/use.go", "guide/README.md"}, nil},
		{"the same README hunk that also changes a sentence", []string{"a.go"}, renameGo + `diff --git a/guide/README.md b/guide/README.md
--- a/guide/README.md
+++ b/guide/README.md
@@ -1,2 +1,2 @@
 # Readme
-Use OldName here.
+Use NewName here, and the docs are clearer.
`, nil, nil, []string{"guide/README.md"}},
		{"a citation of a deleted function removed", []string{"a.go"}, goneGo + `diff --git a/other/use.go b/other/use.go
--- a/other/use.go
+++ b/other/use.go
@@ -1,2 +1 @@
 package other
-// Gone is called here.
`, nil, []string{"other/use.go"}, nil},
		{"a git file rename followed in docs", []string{"old.go"}, renamedPath,
			nil, []string{"docs/ref.md"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			amended, fallout, refused := sprint.LandScopeFallout(tc.paths, tc.diff, nil)
			assert.Equal(t, tc.amended, amended, "amended")
			assert.Equal(t, tc.fallout, fallout, "fallout")
			assert.Equal(t, tc.refused, refused, "refused")
		})
	}
}

// A HOLD whose PATHS-PROPOSED files carry only the card's own fallout in its head is
// widened in place by rule: the same id, no twin, no coordinator step (tla/Fallout.tla,
// Widen; docs/SPEC-SPRINT.md section 7, the fallout sweep).
func TestAPathsProposedOfPureFalloutWidensByRule(t *testing.T) {
	t.Parallel()
	r, dir := newPathsRig(t)
	r.hold("p-a", "HOLD: the fix needs internal/a/b.go, outside PATHS\nPATHS-PROPOSED: internal/a/b.go")
	pr := r.snap().Work.Card("p-a")
	require.NotNil(t, pr)
	wc := r.snap().Fleet.Card(pr.F("work"))
	require.NotNil(t, wc)
	r.must(store.Step{Verb: "fixture diff", Load: []string{sprint.Fleet}, Plan: func(s *sprint.Snapshot) sprint.Plan {
		c := s.Fleet.Card(wc.ID)
		return sprint.Plan{Units: []sprint.Unit{{Key: c.ID, Changes: []sprint.Change{{Table: sprint.Fleet,
			Entry: ntable.BatchMemberEntry{ID: c.ID, Expect: &ntable.MemberExpect{Revision: fmt.Sprint(c.Rev), Place: &ntable.PlaceExpect{Row: c.Row, Col: c.Col}},
				Set: map[string]string{"diff": `diff --git a/internal/a/a.go b/internal/a/a.go
--- a/internal/a/a.go
+++ b/internal/a/a.go
@@ -1,3 +1,3 @@
 package a
 
-func OldName() {}
+func NewName() {}
diff --git a/internal/a/b.go b/internal/a/b.go
--- a/internal/a/b.go
+++ b/internal/a/b.go
@@ -1,2 +1,2 @@
 package a
-// OldName is the one.
+// NewName is the one.
`}}}}}}}
	}})
	r.tick()

	s := r.snap()
	old := s.Work.Card("p-a")
	require.NotNil(t, old)
	assert.True(t, old.Placed(), "the same card stays on the table")
	assert.Nil(t, s.Work.Card("p-a-t"), "no twin: the card is widened in place")
	assert.Contains(t, old.F("brief"), "PATHS: internal/a/a.go,internal/a/b.go", "PATHS widened to the proposal")
	assert.Contains(t, old.F("brief"), "CARRY: p-a attempt 1 head="+pathsHead, "the next attempt starts at the held head")
	assert.NoDirExists(t, dir+"/p-a", "no brief file: no coordinator step")

	var said []string
	for _, l := range r.logNotes() {
		if l.Kind == sprint.Decided && strings.Contains(l.What, sprint.RuleSaid(sprint.RulePaths, "")) {
			said = append(said, l.What)
		}
	}
	require.Len(t, said, 1, "one answer, logged")
	assert.Contains(t, said[0], "internal/a/b.go")
}
