package sprint

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
	"github.com/mas-bandwidth/nova-tools/pkg/diffcheck"
)

// A card can declare that it is a forward-merge of a named branch with the header
// line `MERGE-OF: <branch>` (docs/SPEC-SPRINT.md section 2, the card header). For
// such a card the lander resolves the merged parent and exempts from E12 every
// changed file that arrived unchanged through the merge; every other file still
// needs PATHS exactly as today (docs/SPEC-SPRINT.md section 7, the lander's checks).
// The model is tla/Land.tla: merged[c] is a card's merged set, a "paths" refusal
// draws its files from Files \ (paths[c] \cup merged[c]), and
// NoPathsRefusalNamesMerged holds (Broken "nomergeexempt" is its witness).

// MergeOfResult is one forward-merge card's exemption: the merged parent M, the
// files exempt (arrived unchanged through the merge) and the rest, the files the
// scope check still sees.
type MergeOfResult struct {
	// Parent is M, `git merge-base <after> origin/<branch>`, the merge's parent in
	// the merged branch; empty when no exemption applies (the branch could not be
	// resolved, or M is already an ancestor of before and the merge brings nothing
	// new), and then Exempt is empty and never silently.
	Parent string
	// Exempt is every file the merge's diff changes that arrived unchanged through
	// the merge (rule 2).
	Exempt []string
	// Rest is every other file the merge's diff changes.
	Rest []string
}

// MergeOf computes a forward-merge card's exemption (docs/SPEC-SPRINT.md section 7,
// the lander's checks). dir is the clone and git runs git there and returns its
// trimmed stdout, as RepairMerge's does; before is the batch branch's tip before
// the merge, after the merged head, branch the name the card's MERGE-OF line names.
//
// Rule 1: the merged parent M is `git merge-base <after> origin/<branch>`,
// fetching the branch the way the lander fetches the base when it is not present.
// When the branch cannot be resolved, or M is already an ancestor of before (the
// merge brings nothing new), there is no exemption and the error says why
// (merge-of unresolved); never exempt silently.
//
// Rule 2: a file the merge's diff changes is exempt when its blob at after equals
// its blob at M (absent at both counts as equal), or equals its blob in T, the
// tree `git merge-tree --write-tree before M` writes; a file merge-tree reports as
// conflicted is never exempt by T.
//
// Rule 3: a rename is exempt only when both its old and new paths are exempt by
// rule 2.
func MergeOf(dir string, git func(args ...string) (string, error), before, after, branch string) (*MergeOfResult, error) {
	ref := "refs/remotes/origin/" + branch
	if _, err := git("rev-parse", "--verify", ref); err != nil {
		if _, ferr := git("fetch", "--no-tags", "origin", "+refs/heads/"+branch+":"+ref); ferr != nil {
			return nil, fmt.Errorf("merge-of unresolved: branch %s could not be resolved: %v", branch, ferr)
		}
	}
	m, err := git("merge-base", after, ref)
	if err != nil || m == "" {
		return nil, fmt.Errorf("merge-of unresolved: the merge-base with %s could not be resolved: %v", branch, err)
	}
	// M already an ancestor of before: the merge brings nothing new, no exemption,
	// and the refusal says why.
	if _, err := git("merge-base", "--is-ancestor", m, before); err == nil {
		return nil, fmt.Errorf("merge-of unresolved: %s is already an ancestor of before; the merge brings nothing new", branch)
	}
	treeOut, treeErr := git("merge-tree", "--write-tree", before, m)
	conflicted := mergeTreeConflicted(treeOut)
	line := strings.SplitN(treeOut, "\n", 2)
	tree := strings.TrimSpace(line[0])
	// A content conflict is merge-tree's status 1, still emitting the tree OID and
	// the unmerged stage rows; only a command/parse failure (no tree, no stages) is
	// unresolved. Conflicted files are never exempt by T (rule 2).
	if treeErr != nil && len(conflicted) == 0 {
		return nil, fmt.Errorf("merge-of unresolved: merge-tree with %s failed: %v", branch, treeErr)
	}
	diff, err := git("diff", "-M", "--no-color", before, after)
	if err != nil {
		return nil, fmt.Errorf("merge-of unresolved: the merge's diff could not be read: %v", err)
	}
	granted := map[string]bool{}
	var files []diffcheck.File
	for _, f := range diffcheck.Parse(diff) {
		files = append(files, f)
		granted[f.New] = mergeOfGranted(git, after, m, tree, conflicted, f.New)
		if f.Old != f.New && f.Old != "" {
			granted[f.Old] = mergeOfGranted(git, after, m, tree, conflicted, f.Old)
		}
	}
	res := &MergeOfResult{Parent: m}
	for _, f := range files {
		exempt := granted[f.New]
		if f.Old != f.New && f.Old != "" {
			exempt = granted[f.Old] && granted[f.New]
		}
		if exempt {
			res.Exempt = append(res.Exempt, f.New)
		} else {
			res.Rest = append(res.Rest, f.New)
		}
	}
	return res, nil
}

// mergeOfGranted is rule 2 for one path: its blob at after equals its blob at M
// (absent at both counts as equal), or equals its blob in T when merge-tree did
// not report it conflicted.
func mergeOfGranted(git func(args ...string) (string, error), after, m, tree string, conflicted map[string]bool, p string) bool {
	afterBlob, aerr := git("rev-parse", after+":"+p)
	mBlob, merr := git("rev-parse", m+":"+p)
	if aerr == nil && merr == nil && afterBlob == mBlob {
		return true
	}
	if aerr != nil && merr != nil {
		return true // absent at both
	}
	if conflicted[p] {
		return false // never exempt by T
	}
	tBlob, terr := git("rev-parse", tree+":"+p)
	return (aerr == nil && terr == nil && afterBlob == tBlob) || (aerr != nil && terr != nil)
}

// mergeTreeConflicted is the paths `git merge-tree --write-tree` reports as
// conflicted: the unmerged stage rows (mode blob stage path) it writes.
var mergeTreeStageRE = regexp.MustCompile(`^[0-7]+\s+[0-9a-f]{40}\s+[123]\s+(.*)$`)

func mergeTreeConflicted(out string) map[string]bool {
	conflicted := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		if m := mergeTreeStageRE.FindStringSubmatch(l); m != nil {
			conflicted[strings.TrimSpace(m[1])] = true
		}
	}
	return conflicted
}

// LandScopeMergeOf is E12 as the lander reads a forward-merge card's diff
// (docs/SPEC-SPRINT.md section 7, the lander's checks): exactly LandScope, with
// the files exempt by rule 2 removed before the scope amendment. An exempt file
// never counts as the card's own change and never lends a test, fixture or doc
// amendment to a hand edit in its directory (rule 4).
func LandScopeMergeOf(paths []string, diff string, trackedBefore, exempt []string, registries ...string) (amended, refused []string) {
	exemptSet := map[string]bool{}
	for _, e := range exempt {
		exemptSet[e] = true
	}
	files := diffcheck.Parse(diff)
	var changed []string
	blockedRename := map[string]bool{}
	for _, f := range files {
		if exemptSet[f.New] {
			continue
		}
		changed = append(changed, f.New)
		if f.Old != f.New && !exemptSet[f.Old] && !cardgen.AlwaysInPaths(f.Old, registries...) && cardgen.AlwaysInPaths(f.New, registries...) {
			blockedRename[f.New] = true
		}
	}
	var ordinary, renamed []string
	for _, p := range OutsideByRule(files, diffcheck.Outside(paths, diff, trackedBefore), registries...) {
		if exemptSet[p] {
			continue
		}
		if blockedRename[p] {
			renamed = append(renamed, p)
		} else {
			ordinary = append(ordinary, p)
		}
	}
	amended, refused = ScopeAmended(changed, ordinary)
	return amended, append(refused, renamed...)
}
