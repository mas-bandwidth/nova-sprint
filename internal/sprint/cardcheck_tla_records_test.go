package sprint

import (
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckCardsRefusesATlaEditWithoutARecordsRefresh pins the same typed
// PATHS/NEW and STEP contract that admission enforces: a restored or legacy
// card cannot pass the pre-start check without its run ledger refresh.
func TestCheckCardsRefusesATlaEditWithoutARecordsRefresh(t *testing.T) {
	t.Parallel()
	const step = "\nSTEP 1. Run the changed groups, then: tlacheck merge --root . --keep tla/RUNS.tsv --out tla/RUNS.tsv runs/*/RUNS.tsv\n"
	brief := func(paths, extra string) string {
		return checkBrief("b1", "sprint/s2", paths) + extra
	}
	check := func(t *testing.T, text string) []CardFinding {
		t.Helper()
		s := cardCheckFixture()
		b1 := *s.Work.Card("b1")
		b1.Fields = maps.Clone(b1.Fields)
		b1.Fields["brief"] = text
		s.Work.Put(&b1)
		return CheckCards(s, cardCheckIn())
	}
	refused := func(t *testing.T, text string, says ...string) {
		t.Helper()
		got := check(t, text)
		require.Len(t, got, 1, "%+v", got)
		assert.Equal(t, "tla-records", got[0].Kind)
		assert.Equal(t, "b1", got[0].Subject)
		for _, s := range says {
			assert.Contains(t, got[0].Line(), s)
		}
	}

	t.Run("missing ledger", func(t *testing.T) {
		refused(t, brief("tla/Land.tla", step), "tla/Land.tla", "does not cover tla/RUNS.tsv")
	})
	t.Run("missing typed merge step", func(t *testing.T) {
		refused(t, brief("tla/Land.tla tla/RUNS.tsv", "\nThe notes say tlacheck merge --keep.\n"), "names no STEP", "tlacheck merge --keep")
	})
	t.Run("model introduced through NEW", func(t *testing.T) {
		text := strings.Replace(brief("internal/b.go", step), "\n\nTHE TASK.", "\nNEW: tla/New.tla\n\nTHE TASK.", 1)
		refused(t, text, "tla/New.tla", "does not cover tla/RUNS.tsv")
	})

	t.Run("model ledger and typed step pass", func(t *testing.T) {
		assert.Empty(t, check(t, brief("tla/Land.tla tla/RUNS.tsv", step)))
	})
	t.Run("prose-only model read passes", func(t *testing.T) {
		assert.Empty(t, check(t, brief("internal/b.go", "\nSTEP 1. Read tla/Land.tla and update the Go file.\n")))
	})
	t.Run("TLA documentation and config pass", func(t *testing.T) {
		assert.Empty(t, check(t, brief("tla/README.md tla/MCLand.cfg", "\nSTEP 1. Update the documentation and config.\n")))
	})
}
