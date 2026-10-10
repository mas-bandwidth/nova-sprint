package main

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/subproc"
)

// release is the nova-tools release a checkout is at: the tag NOVA-TOOLS-VERSION
// records (docs/SPEC-SPRINT.md, "The nova-tools it runs on") and its commit.
type release struct {
	tag, commit string
}

// checkoutRelease is the release tag on the nova-tools checkout's HEAD, the highest
// when it carries more than one. A checkout at no release tag is refused: the seed
// records which nova-tools the tree came from, and a commit with no tag names no
// version anybody can install. A dirty checkout (uncommitted or untracked changes)
// is refused too: what it holds is not what the tag published.
func checkoutRelease(from string) (release, error) {
	commit, err := gitOut(from, "rev-parse", "HEAD")
	if err != nil {
		return release{}, err
	}
	dirty, err := gitOut(from, "status", "--porcelain")
	if err != nil {
		return release{}, err
	}
	if dirty != "" {
		return release{}, fmt.Errorf("%s has uncommitted changes (git -C %s status); seed from a clean checkout at a release tag so %s records what was published", from, from, sprint.NovaToolsVersionFile)
	}
	tags, err := gitOut(from, "tag", "--points-at", "HEAD")
	if err != nil {
		return release{}, err
	}
	return pickRelease(commit, strings.Fields(tags), from)
}

// pickRelease is the highest canonical SemVer tag without build metadata. A
// prerelease is useful for a private exact-SHA canary; checkoutRelease still
// requires a clean tree whose HEAD is exactly the tagged commit.
func pickRelease(commit string, tags []string, from string) (release, error) {
	var best string
	for _, t := range tags {
		if !semver.IsValid(t) || semver.Canonical(t) != t || semver.Build(t) != "" {
			continue
		}
		if best == "" || semver.Compare(t, best) > 0 {
			best = t
		}
	}
	if best == "" {
		short := commit
		if len(short) > 12 {
			short = short[:12]
		}
		return release{}, fmt.Errorf("%s is at %s, which carries no release tag vX.Y.Z; seed from a nova-tools release (git -C %s checkout <tag>) so %s records it", from, short, from, sprint.NovaToolsVersionFile)
	}
	return release{tag: best, commit: commit}, nil
}

func gitOut(dir string, args ...string) (string, error) {
	cmd, cancel := subproc.Command(context.Background(), subproc.Git, "git", append([]string{"-C", dir}, args...)...)
	defer cancel()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s in %s: %w", strings.Join(args, " "), dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}
