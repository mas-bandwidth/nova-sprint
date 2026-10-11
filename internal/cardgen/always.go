package cardgen

import (
	"path"
	"slices"
	"strings"
)

// AlwaysInPathsRule is the one sentence the brief generator, the lander and the readers
// hold a card's scope to (docs/SPEC-SPRINT.md section 7, the lander's checks;
// docs/SPEC-CARD-CONTRACT.md): the files a change must touch to keep the tree green are
// inside every card's PATHS. The owner, 2026-10-06: "Can we stop this whole 'test
// outside of paths' thing. It's wasteful."
const AlwaysInPathsRule = "Files a change must touch to keep the tree green are always inside PATHS, whatever the brief names: every *_test.go, every file under a testdata/ directory, tla/RUNS.tsv and tla/CASES.tsv, internal/docs/catalog.go, and every AGENTS.md map; any other file outside PATHS is still out of scope."

// alwaysInPathsFiles are the single files AlwaysInPathsRule names.
var alwaysInPathsFiles = []string{"tla/RUNS.tsv", "tla/CASES.tsv", "internal/docs/catalog.go"}

// LedgerInPathsRule names the generated ledgers the lander merges (docs/SPEC-LANDING.md,
// the ledger set): the roadmap data and the pages generated from it, and the per-card
// evidence ledgers under docs/roadmap-evidence. They are inside every card's PATHS, and a
// repository's own AGENTS.md or nova.sexp may name more with a LEDGERS line.
const LedgerInPathsRule = "The generated ledgers the lander merges are inside every card's PATHS: docs/fixes.sexp, docs/roadmap.sexp, FIXES.md, ROADMAP.md and every .sexp under docs/roadmap-evidence/, plus any path a repository's AGENTS.md or nova.sexp names on a LEDGERS line."

// ledgerFiles are the single data and page ledgers LedgerInPathsRule names.
var ledgerFiles = []string{"docs/fixes.sexp", "docs/roadmap.sexp", "FIXES.md", "ROADMAP.md"}

// LedgerInPaths says p is a generated ledger the lander merges (LedgerInPathsRule): one of
// the named files, or a .sexp under docs/roadmap-evidence. p is a path relative to the
// repository root. A repository's own LEDGERS line is read at staging, not here.
func LedgerInPaths(p string) bool {
	p = strings.TrimPrefix(path.Clean(p), "./")
	switch {
	case p == "." || p == "" || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/"):
		return false
	case slices.Contains(ledgerFiles, p):
		return true
	}
	return path.Dir(p) == "docs/roadmap-evidence" && strings.HasSuffix(p, ".sexp")
}

// AlwaysInPaths says p is a file AlwaysInPathsRule puts inside every card's PATHS: a Go
// test file, a file under a testdata directory at any depth, one of the TLA+ ledgers,
// the docs catalog, or an AGENTS.md map. p is a path relative to the repository root.
func AlwaysInPaths(p string) bool {
	p = strings.TrimPrefix(path.Clean(p), "./")
	switch {
	case p == "." || p == "" || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/"):
		return false
	case strings.HasSuffix(p, "_test.go"), path.Base(p) == "AGENTS.md", slices.Contains(alwaysInPathsFiles, p):
		return true
	case LedgerInPaths(p):
		return true
	}
	return slices.Contains(strings.Split(path.Dir(p), "/"), "testdata")
}
