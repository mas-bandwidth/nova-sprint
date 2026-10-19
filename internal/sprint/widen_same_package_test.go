package sprint_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A file outside PATHS that is another source file of a package a PATHS entry names is
// adjacent to the change (docs/SPEC-SPRINT.md section 8, the rules table's row widen): the
// widen rule edits the card in place rather than leaving it a judgment. The same directory
// only, and only a concrete PATHS file lends it: a directory entry already covers its files
// directly, and a glob must not make every package adjacent. On the twin store (store.Mem).
func TestASamePackageSourceFileIsAdjacentForWiden(t *testing.T) {
	t.Parallel()
	t.Run("another source file of the package is adjacent, in a HOLD and an E12 refusal", func(t *testing.T) {
		t.Parallel()
		paths := []string{"internal/x/a.go"}
		assert.Equal(t, sprint.AdjSamePackage, sprint.WidenAdjacent(paths, "internal/x/b.go", "", false))
		assert.Equal(t, sprint.AdjSamePackage, sprint.WidenAdjacent(paths, "internal/x/b.go", "", true))
	})
	t.Run("reversed witnesses: another package, a subdirectory, no source, a glob entry", func(t *testing.T) {
		t.Parallel()
		paths := []string{"internal/x/a.go"}
		assert.Equal(t, "", sprint.WidenAdjacent(paths, "internal/z/c.go", "", false), "another directory, another package")
		assert.Equal(t, "", sprint.WidenAdjacent(paths, "internal/x/sub/c.go", "", false), "a subdirectory of the PATHS file's directory is another package")
		assert.Equal(t, "", sprint.WidenAdjacent(paths, "internal/x/notes.txt", "", false), "not a .go source file")
		assert.Equal(t, "", sprint.WidenAdjacent([]string{"internal/q/*.go"}, "internal/q/d.go", "", false), "a glob lends no same-package adjacency")
	})
	t.Run("an E12 refusal naming a same-package source is widened in place by rule", func(t *testing.T) {
		t.Parallel()
		r := newConflictRig(t)
		r.widenCard()
		r.refusedE12("w-1", "w", "internal/x/b.go")
		r.tick()

		s := r.snap()
		assert.Nil(t, s.Work.Card("w-1b"), "no twin")
		pr := s.Work.Placed("w-1")
		require.NotNil(t, pr, "the same card, on the table")
		assert.Contains(t, pr.F("brief"), "\nPATHS: internal/x/a.go,internal/x/b.go\n", "PATHS widened by exactly the file")
		assert.Contains(t, pr.F("brief"), "\nSHARED: internal/x/a.go,internal/x/b.go\n", "SHARED widened too")
		assert.Contains(t, pr.F("brief"), "CARRY: w-1 attempt 1 head="+readHead)
		assert.Empty(t, r.openOnCard(sprint.NReturned, "w-1"), "the E12 refusal is answered")
		notes := r.widenNotes()
		require.Len(t, notes, 1, "the coordinator gets one note")
		assert.Contains(t, notes[0].What, "internal/x/b.go ("+sprint.AdjSamePackage+")")
		r.clean("a same-package source widened in place")
	})
	t.Run("same-package widenings stop at MaxWidens: the next is the brief's bound", func(t *testing.T) {
		t.Parallel()
		r := newConflictRig(t)
		r.widenCard()
		r.m.SetMaxAttempts("8") // the widen cap, not the attempt cap, is the bound under test
		for i, file := range []string{"internal/x/b.go", "internal/x/c.go", "internal/x/d.go"} {
			r.refusedE12("w-1", "w", file)
			r.tick()
			pr := r.snap().Work.Card("w-1")
			require.Contains(t, pr.F("brief"), file, "widening %d", i+1)
			require.Equal(t, strconv.Itoa(i+1), pr.F(sprint.FieldWidens), "widening %d is counted", i+1)
		}
		r.refusedE12("w-1", "w", "internal/x/e.go")
		r.tick()
		r.tick()

		pr := r.snap().Work.Card("w-1")
		assert.Equal(t, sprint.Review, pr.Col, "not widened again")
		assert.NotContains(t, pr.F("brief"), "internal/x/e.go")
		bound := r.openOnCard(sprint.NBriefWrong, "w-1")
		require.Len(t, bound, 1, "the brief is wrong: a mind's")
		assert.Contains(t, bound[0].Note.What, "widened 3 times")
		assert.Len(t, r.widenNotes(), sprint.MaxWidens)
	})
}
