package sprint

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/mas-bandwidth/nova-tools/pkg/gitrun"
	"github.com/mas-bandwidth/nova-tools/pkg/tlc"
)

// Which runner the lander fingerprints a tree's run records with (docs/SPEC-SPRINT.md
// section 7, "The run records"). The runner's result files are inputs of every case's
// fingerprint, so a record is current only under the runner that wrote it, and a tree's
// records have one authority for their runner, the tree itself:
//
//  1. a tree that holds the runner (pkg/tlc, as nova-sprint does) is fingerprinted with
//     those files;
//  2. else a tree whose CI workflows pin tlacheck to a nova-tools commit (rocketnet's
//     .github/workflows/tlc.yml clones nova-tools and checks it out at a full commit; the
//     shapes of a pin, and its refusals, are in land_runner_pin.go) is fingerprinted with
//     that revision's pkg/tlc, read from nova-tools;
//  3. else the runner this binary was built with: nova-tools' pkg/tlc at the version
//     nova-sprint's go.mod names, which is nova-sprint's own tree's runner (it holds no
//     pkg/tlc since 2026-10-18, and runs go tool tlacheck at that version).
//
// The embedded copy is only the fallback: two copies of the runner that disagree must not
// make the lander refuse records the tree's own pinned runner wrote (Stella, on
// rocketnet-cookie-key-rotates-on-its-period, 2026-10-15).

// RunnerSource reads the runner's result files (tlc.ResultFiles, by path under the
// checkout root as tlc.RunnerFiles returns them) at a nova-tools revision.
type RunnerSource func(rev string) (map[string][]byte, error)

// runnerRepo is where a pinned runner revision is read from. The pin names a revision
// only; the repository is never taken from the tree.
const runnerRepo = "https://github.com/mas-bandwidth/nova-tools.git"

// runnerPin is a tree's pin of its TLC runner: the full nova-tools commit and where it is.
type runnerPin struct {
	File string // path under the tree root, slash-separated
	Line int    // 1-based
	Rev  string // 40 hex digits
}

// landRunner is the runner a tree's records are fingerprinted with, and how the lander
// names it in a refusal.
type landRunner struct {
	files map[string][]byte
	name  string
}

// runnerDigest names a runner by its result files: tlc.RunnerDigest, the one
// tlacheck version prints as runner=, so two runners a refusal names can be told apart even
// when a revision is unknown.
func runnerDigest(files map[string][]byte) string { return tlc.RunnerDigest(files) }

// builtRunner is the runner this binary was built with, named by the nova-sprint revision
// it was built from ("devel" when the build records none) and its digest.
func builtRunner() (landRunner, error) {
	files, err := tlc.RunnerFiles()
	if err != nil {
		return landRunner{}, err
	}
	rev := "devel"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				rev = s.Value
			}
		}
	}
	return landRunner{files: files, name: fmt.Sprintf("the runner built into nova-sprint %s (runner digest %s)", rev, runnerDigest(files))}, nil
}

// gitRunnerSource reads a nova-tools revision's runner files from a bare cache repository
// at cache, fetching the one commit from runnerRepo when the cache lacks it.
func gitRunnerSource(cache string) RunnerSource {
	return func(rev string) (map[string][]byte, error) {
		ctx := context.Background()
		run := func(args ...string) ([]byte, error) {
			res, err := gitrun.Run(ctx, gitrun.Options{C: cache, OwnRepo: true}, args...)
			if err != nil {
				return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(res.Stderr)))
			}
			return res.Stdout, nil
		}
		if _, err := os.Stat(filepath.Join(cache, "HEAD")); err != nil {
			if err := os.MkdirAll(cache, 0o755); err != nil {
				return nil, err
			}
			if _, err := run("init", "-q", "--bare"); err != nil {
				return nil, err
			}
		}
		if _, err := run("cat-file", "-e", rev+"^{commit}"); err != nil {
			if _, err := run("fetch", "-q", "--no-tags", "--depth=1", runnerRepo, rev); err != nil {
				return nil, err
			}
		}
		out := map[string][]byte{}
		for _, name := range tlc.ResultFiles {
			p := tlc.RunnerDir + "/" + name
			raw, err := run("show", rev+":"+p)
			if err != nil {
				return nil, err
			}
			out[p] = raw
		}
		return out, nil
	}
}

// pinnedRunnerFiles is the lander's RunnerSource: nova-tools revisions cached under the
// user's cache directory, beside land's clones.
func pinnedRunnerFiles(rev string) (map[string][]byte, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	return gitRunnerSource(filepath.Join(cache, "nova-sprint", "runner", "nova-tools.git"))(rev)
}
