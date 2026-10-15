package sprint

import (
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/pkg/member"
	"github.com/mas-bandwidth/nova-tools/pkg/typedrec"
	"regexp"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
	"github.com/mas-bandwidth/nova-tools/pkg/diffcheck"
)

// LandScope is E12 as the lander reads one merge's diff (docs/SPEC-SPRINT.md section 7,
// the lander's checks): the files the diff changes outside the card's PATHS
// (diffcheck.Outside), less those inside every card's PATHS by rule (OutsideByRule),
// split into the scope amendments of the same change and the files refused
// (ScopeAmended). trackedBefore is diffcheck.Outside's.
func LandScope(paths []string, diff string, trackedBefore []string) (amended, refused []string) {
	files := diffcheck.Parse(diff)
	var changed []string
	blockedRename := map[string]bool{}
	for _, f := range files {
		changed = append(changed, f.New)
		if f.Old != f.New && !cardgen.AlwaysInPaths(f.Old) && cardgen.AlwaysInPaths(f.New) {
			blockedRename[f.New] = true
		}
	}
	var ordinary, renamed []string
	for _, p := range OutsideByRule(files, diffcheck.Outside(paths, diff, trackedBefore)) {
		if blockedRename[p] {
			renamed = append(renamed, p)
		} else {
			ordinary = append(ordinary, p)
		}
	}
	// SPEC-SPRINT section 7: a rename cannot borrow another file's test amendment
	// to move source that the card did not own into an always-allowed category.
	amended, refused = ScopeAmended(changed, ordinary)
	return amended, append(refused, renamed...)
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

// finishScope validates the supplied take/head binding before using the lander's exact rule.
// Omitted evidence is the legacy/manual/friend compatibility boundary; land still checks it.
func finishScope(raw, head, card string, gen int, epoch uint64, paths []string) (string, error) {
	if raw == "" {
		return "", nil
	}
	var e member.FinishScopeEvidence
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		return "", fmt.Errorf("scope JSON: %w", err)
	}
	if e.Card != card || e.Gen != gen || e.Epoch != epoch || e.Head != head || !typedrec.IsFullSha(e.Head) || !typedrec.IsFullSha(e.Base) || e.Tracked == nil {
		return "", fmt.Errorf("scope evidence must bind this card, generation, epoch, full base and finish head, and tracked files")
	}
	for _, line := range strings.Split(e.Diff, "\n") {
		if strings.HasPrefix(line, "@@ ") && !scopeHunk.MatchString(line) {
			return "", fmt.Errorf("scope diff has an invalid hunk header")
		}
	}
	files := diffcheck.Parse(e.Diff)
	if e.Diff != "" && len(files) == 0 {
		return "", fmt.Errorf("scope diff has no file records")
	}
	for _, f := range files {
		if f.Old == "" || f.New == "" {
			return "", fmt.Errorf("scope diff has an invalid file record")
		}
	}
	_, outside := LandScope(paths, e.Diff, e.Tracked)
	if len(outside) == 0 {
		return "", nil
	}
	parts := []string{}
	for _, f := range outside {
		if entry := diffcheck.ExpectedEntry(paths, f); entry != "" {
			parts = append(parts, f+" (expected to match PATHS entry "+entry+")")
		} else {
			parts = append(parts, f)
		}
	}
	return "it changes files outside its PATHS (E12): " + strings.Join(parts, ", "), nil
}

var scopeHunk = regexp.MustCompile(`^@@ -[0-9]+(,[0-9]+)? \+[0-9]+(,[0-9]+)? @@`)
