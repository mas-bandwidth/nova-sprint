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
// (ScopeAmended). trackedBefore is diffcheck.Outside's. Fallout hunks are accepted too
// (LandScopeFallout); this older reader keeps its two lists for the callers that know no
// fallout, folding the fallout into amended.
func LandScope(paths []string, diff string, trackedBefore []string) (amended, refused []string) {
	amended, fallout, refused := LandScopeFallout(paths, diff, trackedBefore)
	return append(amended, fallout...), refused
}

// LandScopeFallout is E12 with the fallout rule (tla/Fallout.tla, Land): outside the
// card's PATHS, a file whose every hunk only follows the card's own rename or deletion
// is fallout, accepted as the scope amendment is; a file with any other hunk keeps
// today's refusal. amended is the test, fixture or doc of the same change; fallout is
// the card's own change through a file it did not name (docs/SPEC-SPRINT.md section 7,
// the fallout sweep).
func LandScopeFallout(paths []string, diff string, trackedBefore []string) (amended, fallout, refused []string) {
	files := diffcheck.Parse(diff)
	var changed []string
	blockedRename := map[string]bool{}
	for _, f := range files {
		changed = append(changed, f.New)
		if f.Old != f.New && !cardgen.AlwaysInPaths(f.Old) && cardgen.AlwaysInPaths(f.New) {
			blockedRename[f.New] = true
		}
	}
	renames, removed := CardRenames(diff, paths)
	outside := OutsideByRule(files, diffcheck.Outside(paths, diff, trackedBefore))
	falloutSet := falloutFiles(files, outside, renames, removed)
	var ordinary, renamed []string
	for _, p := range outside {
		if blockedRename[p] {
			renamed = append(renamed, p)
			continue
		}
		if slices.Contains(falloutSet, p) {
			fallout = append(fallout, p)
			continue
		}
		ordinary = append(ordinary, p)
	}
	// SPEC-SPRINT section 7: a rename cannot borrow another file's test amendment
	// to move source that the card did not own into an always-allowed category.
	amended, refused = ScopeAmended(changed, ordinary)
	return amended, fallout, append(refused, renamed...)
}

// OutsideByRule is outside, files a diff changes outside a card's PATHS, without those
// cardgen.AlwaysInPathsRule puts inside every card's PATHS: a test, a testdata file, a
// TLA+ ledger, the docs catalog or an AGENTS.md map (cardgen.AlwaysInPaths). files is
// the diff's (diffcheck.Parse); a rename is inside by rule only when both its sides are,
// so a source file moved to a test's name is still refused.
func OutsideByRule(files []diffcheck.File, outside []string) []string {
	var out []string
	for _, p := range outside {
		byRule := slices.ContainsFunc(files, func(f diffcheck.File) bool {
			return f.New == p && cardgen.AlwaysInPaths(f.Old) && cardgen.AlwaysInPaths(f.New)
		})
		if !byRule {
			out = append(out, p)
		}
	}
	return out
}
