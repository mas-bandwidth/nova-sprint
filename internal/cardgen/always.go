package cardgen

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/diffcheck"
)

// AlwaysInPathsRule is the one sentence the brief generator, the lander and the readers
// hold a card's scope to (docs/SPEC-SPRINT.md section 7, the lander's checks;
// docs/SPEC-CARD-CONTRACT.md): the files a change must touch to keep the tree green are
// inside every card's PATHS. The owner, 2026-10-06: "Can we stop this whole 'test
// outside of paths' thing. It's wasteful."
const AlwaysInPathsRule = "Files a change must touch to keep the tree green are always inside PATHS, whatever the brief names: every *_test.go, every file under a testdata/ directory, tla/RUNS.tsv, tla/CASES.tsv and tla/COVERAGE.tsv, internal/docs/catalog.go, and every AGENTS.md map; any other file outside PATHS is still out of scope."

// alwaysInPathsFiles are the single files AlwaysInPathsRule names.
var alwaysInPathsFiles = []string{"tla/RUNS.tsv", "tla/CASES.tsv", "internal/docs/catalog.go"}

// ledgerInPathsFiles are the shared ledgers every card may write and whose merge is the
// lander's (docs/SPEC-LANDING.md): the roadmap's data and pages, the class ledgers, the
// AGENTS.md maps, the keyed TLA+ tables and the tables lock. docs/roadmap-evidence/*.sexp
// is the evidence ledgers (EvidenceLedger), matched by glob, not named here.
var ledgerInPathsFiles = []string{
	"docs/fixes.sexp", "FIXES.md", "docs/roadmap.sexp", "ROADMAP.md",
	"tla/CASES.tsv", "tla/RUNS.tsv", "tla/COVERAGE.tsv", "internal/sprint/TABLES.lock", "internal/docs/catalog.go",
}

// EvidenceLedger says p is an evidence ledger the lander merges entry by entry: a
// docs/roadmap-evidence/fixed-<leg>.sexp file (docs/SPEC-LANDING.md).
func EvidenceLedger(p string) bool {
	return strings.HasPrefix(p, "docs/roadmap-evidence/") && strings.HasSuffix(p, ".sexp")
}

// CatalogLedgers reads a repository's own catalog at repoDir: a LEDGERS line in its
// AGENTS.md or nova.sexp, read at staging. It returns the paths those lines name, in file
// order and each once; nil when neither file names one, and then the built-in set stands.
func CatalogLedgers(repoDir string) []string {
	var out []string
	for _, name := range []string{"AGENTS.md", "nova.sexp"} {
		data, err := os.ReadFile(filepath.Join(repoDir, name))
		if err != nil {
			continue
		}
		for _, p := range catalogLedgerPaths(data) {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// catalogLedgerPaths is the paths a catalog file's LEDGERS lines name: a line is a LEDGERS
// line when its first token, once a trailing colon is dropped, is "LEDGERS"; the rest of its
// tokens are the paths, each stripped of surrounding quotes, commas and parentheses.
func catalogLedgerPaths(data []byte) []string {
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 1 || strings.TrimSuffix(f[0], ":") != "LEDGERS" {
			continue
		}
		for _, tok := range f[1:] {
			if tok = strings.Trim(tok, `"'(),;[]`); tok == "" {
				continue
			}
			if !slices.Contains(out, tok) {
				out = append(out, tok)
			}
		}
	}
	return out
}

// LedgerInPaths says p is a ledger path: one the lander merges (the shared ledgers and the
// evidence ledgers), and so always inside every card's PATHS (docs/SPEC-LANDING.md). p is a
// path relative to the repository root. catalog is the paths a repository's own catalog names
// as ledgers (CatalogLedgers), read at staging; absent (nil), the built-in set stands.
func LedgerInPaths(p string, catalog ...string) bool {
	p = strings.TrimPrefix(path.Clean(p), "./")
	switch {
	case p == "." || p == "" || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/"):
		return false
	case slices.Contains(ledgerInPathsFiles, p), EvidenceLedger(p), slices.Contains(catalog, p):
		return true
	}
	return diffcheck.Ledger(p) || diffcheck.AgentsMap(p)
}

// AlwaysInPaths says p is a file AlwaysInPathsRule puts inside every card's PATHS: a Go
// test file, a file under a testdata directory at any depth, one of the TLA+ ledgers,
// the docs catalog, an AGENTS.md map, or a shared or evidence ledger (LedgerInPaths).
// p is a path relative to the repository root.
func AlwaysInPaths(p string) bool {
	p = strings.TrimPrefix(path.Clean(p), "./")
	switch {
	case p == "." || p == "" || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/"):
		return false
	case strings.HasSuffix(p, "_test.go"), path.Base(p) == "AGENTS.md", slices.Contains(alwaysInPathsFiles, p), LedgerInPaths(p):
		return true
	}
	return slices.Contains(strings.Split(path.Dir(p), "/"), "testdata")
}
