package main

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
)

// A generated ledger the lander merges is inside every card's PATHS by the one rule
// (docs/SPEC-LANDING.md, the ledger set): the data and page ledgers docs/fixes.sexp,
// FIXES.md, docs/roadmap.sexp and ROADMAP.md, and the per-card evidence ledgers
// docs/roadmap-evidence/*.sexp. cardgen.AlwaysInPaths is the one place the set lives.
func TestLedgersAreAlwaysInPaths(t *testing.T) {
	t.Parallel()
	for _, p := range []string{
		"docs/fixes.sexp",
		"FIXES.md",
		"docs/roadmap.sexp",
		"ROADMAP.md",
		"docs/roadmap-evidence/fixed-x.sexp",
		"docs/roadmap-evidence/fixed-leg.sexp",
	} {
		if !cardgen.AlwaysInPaths(p) {
			t.Errorf("cardgen.AlwaysInPaths(%q) = false, want true: a generated ledger the lander merges", p)
		}
	}
	// not every .sexp is a ledger: the set is the named files and the evidence directory
	if cardgen.AlwaysInPaths("docs/notes.sexp") {
		t.Errorf("cardgen.AlwaysInPaths(%q) = true, want false: no ledger", "docs/notes.sexp")
	}
}
