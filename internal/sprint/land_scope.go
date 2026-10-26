package sprint

import (
	"slices"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
	"github.com/mas-bandwidth/nova-tools/pkg/diffcheck"
)

// LandScope is E12 as the lander reads one merge's diff (docs/SPEC-SPRINT.md section 7,
// the lander's checks): the files the diff changes outside the card's PATHS
// (diffcheck.Outside), less those inside every card's PATHS by rule (OutsideByRule),
// split into the scope amendments of the same change and the files refused
// (ScopeAmended). trackedBefore is diffcheck.Outside's. registries is the repository's
// own shared registry (.nova/always-allowed), read at the base, whose entries the rule
// holds inside every card's PATHS beside the built-in set (cardgen.SharedRegistries;
// docs/SPEC-LANDING.md).
func LandScope(paths []string, diff string, trackedBefore []string, registries ...string) (amended, refused []string) {
	// A card with no MERGE-OF line (docs/SPEC-SPRINT.md section 7, the lander's
	// checks) has no exempt files: the one loop lives in LandScopeMergeOf, so a
	// forward-merge card and an ordinary one cannot drift apart.
	return LandScopeMergeOf(paths, diff, trackedBefore, nil, registries...)
}

// OutsideByRule is outside, files a diff changes outside a card's PATHS, without those
// cardgen.AlwaysInPathsRule puts inside every card's PATHS: a test, a testdata file, a
// TLA+ ledger, the docs catalog, an AGENTS.md map or a shared registry file
// (cardgen.AlwaysInPaths, the repository's registry read at the base). files is the
// diff's (diffcheck.Parse); a rename is inside by rule only when both its sides are, so
// a source file moved to a test's name is still refused.
func OutsideByRule(files []diffcheck.File, outside []string, registries ...string) []string {
	var out []string
	for _, p := range outside {
		byRule := slices.ContainsFunc(files, func(f diffcheck.File) bool {
			return f.New == p && cardgen.AlwaysInPaths(f.Old, registries...) && cardgen.AlwaysInPaths(f.New, registries...)
		})
		if !byRule {
			out = append(out, p)
		}
	}
	return out
}
