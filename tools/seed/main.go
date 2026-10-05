// Command seed makes nova-sprint's seed tree from a nova-tools checkout, by the
// rules in tools/seed/RECIPE: the sprint's packages are moved whole, the
// nova-tools packages they import are copied with their import specs rewritten
// to this module, and the sprint's docs and TLA+ models come with them.
//
// It writes a new directory and touches nothing else; the caller commits it.
// A re-seed is this command and a three-way merge:
//
//	git worktree add ../seed-tree <the last seed commit>
//	go run ./tools/seed -from ../nova-tools -keep . -out ../seed-new
//	rsync -a --delete --exclude=.git ../seed-new/ ../seed-tree/
//	(cd ../seed-tree && go mod tidy && git add -A && git commit)
//	git checkout -b <branch> main && git cherry-pick <that commit>
//
// The cherry-pick carries nova-tools' changes since the last seed onto main and
// keeps nova-sprint's own; where both touched a line, git stops and asks.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	from := flag.String("from", "", "the nova-tools checkout to seed from")
	keep := flag.String("keep", ".", "the nova-sprint checkout whose own files (keep rules) are carried over")
	out := flag.String("out", "", "the directory to write; it must not exist")
	recipe := flag.String("recipe", "", "the recipe file (default: tools/seed/RECIPE under -keep)")
	flag.Parse()
	if *from == "" || *out == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: seed -from <nova-tools checkout> -out <new dir> [-keep <nova-sprint checkout>] [-recipe <file>]")
		os.Exit(2)
	}
	if *recipe == "" {
		*recipe = filepath.Join(*keep, "tools", "seed", "RECIPE")
	}
	r, err := readRecipe(*recipe)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	rep, err := run(r, *from, *keep, *out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	fmt.Printf("moved %d packages under %d roots\n", len(rep.movedPkgs), len(r.move))
	fmt.Printf("copied %d packages:\n", len(rep.copied))
	for _, p := range rep.copied {
		fmt.Println("  " + p)
	}
	fmt.Printf("wrote %d files to %s\n", rep.files, *out)
}
