package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The owner, 2026-10-11: "The dashboard has to reflect the truth." "Anytime when it doesn't is
// a bug." and "Done is every card completed." Friend stella's row read done 0 and ok% 0.0% while
// the log recorded her finishes ok and their reads ok: the readers' verdict (TickVerdicts)
// counted the readers table's reads alone, and her cards were read on read cards of the fleet
// table, retired off their rows with their verdicts, so her work waited in finished however
// the accept took it (Stella's finding). A row's done is now every card it completed, of every
// kind (sprint.DoneKinds): work and fix cards the readers' verdict moved to ok, and read cards
// completed with a verdict.

// inboxJobOf is the job of her inbox whose name begins with the card's id.
func inboxJobOf(t *testing.T, root, friend, id string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, friend+"-working", "inbox"))
	require.NoError(t, err)
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), id) {
			return e.Name()
		}
	}
	require.Failf(t, "no job", "%s's inbox holds no job of %s: %v", friend, id, entries)
	return ""
}

// rowCells is a row's cells of the named table as where --json carries them.
func rowCells(ta *testApp, table, row string) map[string]any {
	var w whereView
	ta.json("where", &w)
	return withoutRowCards(w.Tables[table][row])
}
