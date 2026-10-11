package tlc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LoadCases reads tla/CASES.tsv under root and checks it against the tree: it
// must name every MC*.cfg exactly once and every module must exist.
func LoadCases(root string) ([]Case, error) {
	dir := filepath.Join(root, "tla")
	f, err := os.Open(filepath.Join(dir, CasesFile))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %v", filepath.Join(dir, CasesFile), err)
	}
	defer func() { _ = f.Close() }() // ignored: the plan is opened only for reading
	cases, err := ParseCases(f)
	if err != nil {
		return nil, err
	}
	cfgs, err := filepath.Glob(filepath.Join(dir, "MC*.cfg"))
	if err != nil {
		return nil, err
	}
	inTree := map[string]bool{}
	for _, p := range cfgs {
		inTree[filepath.Base(p)] = true
	}
	named := map[string]bool{}
	for _, c := range cases {
		if named[c.Config] {
			return nil, fmt.Errorf("%s must name every MC*.cfg exactly once: %s is named twice", CasesFile, c.Config)
		}
		named[c.Config] = true
	}
	var problems []string
	for p := range inTree {
		if !named[p] {
			problems = append(problems, p+" is under tla/ but not in "+CasesFile)
		}
	}
	for _, c := range cases {
		if !inTree[c.Config] {
			problems = append(problems, c.Config+" is in "+CasesFile+" but has no file")
		}
		if info, err := os.Stat(filepath.Join(dir, c.Module)); err != nil || !info.Mode().IsRegular() {
			problems = append(problems, c.Config+" names a module "+c.Module+" that has no file")
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s must name every MC*.cfg exactly once: %s", CasesFile, strings.Join(problems, "; "))
	}
	return cases, nil
}

// RequiredGroups returns the groups that hold a required case, sorted: the
// matrix a CI run derives.
func RequiredGroups(cases []Case) []string {
	set := map[string]bool{}
	for _, c := range cases {
		if c.Gate == "required" {
			set[c.Group] = true
		}
	}
	out := make([]string, 0, len(set))
	for g := range set {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// Select returns the cases one run covers: a group, or shard number shard of
// shards (every shards'th case from shard) of the whole plan or, with a group,
// of that group's cases. A group's shards of its own size are its cases one
// at a time: run --bench measures a group so, a load trough before each case.
func Select(cases []Case, group string, shards, shard int) ([]Case, error) {
	if shards < 1 || shard < 0 || shard >= shards {
		return nil, errors.New("use a positive shard count and a zero-based shard below it")
	}
	pool := cases
	if group != "" {
		pool = nil
		for _, c := range cases {
			if c.Group == group {
				pool = append(pool, c)
			}
		}
		if len(pool) == 0 {
			return nil, fmt.Errorf("unknown group: %s", group)
		}
		if shards > len(pool) {
			return nil, fmt.Errorf("shard count exceeds the %d cases of group %s", len(pool), group)
		}
	}
	if shards > len(pool) {
		return nil, errors.New("shard count exceeds the number of declared cases")
	}
	var chosen []Case
	for i := shard; i < len(pool); i += shards {
		chosen = append(chosen, pool[i])
	}
	return chosen, nil
}
