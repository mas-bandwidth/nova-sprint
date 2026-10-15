package sprint

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/tlc"
	"github.com/mas-bandwidth/nova-tools/pkg/gitrun"
)

// Which runner the lander fingerprints a tree's run records with (docs/SPEC-SPRINT.md
// section 7, "The run records"). The runner's result files are inputs of every case's
// fingerprint, so a record is current only under the runner that wrote it, and a tree's
// records have one authority for their runner, the tree itself:
//
//  1. a tree that holds the runner (pkg/tlc, as nova-sprint does) is fingerprinted with
//     those files;
//  2. else a tree that pins tlacheck to a nova-tools revision in a CI workflow (rocketnet's
//     .github/workflows/tlc.yml builds tlacheck from a nova-tools checkout at a full
//     commit) is fingerprinted with that revision's pkg/tlc, read from nova-tools;
//  3. else the runner this binary was built with.
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

// runnerPin is a tree's pin of tlacheck: the full nova-tools commit and where it is.
type runnerPin struct {
	File string // path under the tree root, slash-separated
	Line int    // 1-based
	Rev  string // 40 hex digits
}

// pinCheckoutRE is a checkout of a full commit (`git checkout --detach <sha>`, or
// `git -C dir checkout <sha>`); pinInstallRE is `go install|run .../nova-tools/tools/tlacheck@<sha>`.
var (
	pinCheckoutRE = regexp.MustCompile(`\bcheckout\s+(?:--detach\s+|-q\s+|--quiet\s+)*([0-9a-f]{40})\b`)
	pinInstallRE  = regexp.MustCompile(`nova-tools/tools/tlacheck@([0-9a-f]{40})\b`)
)

// treeRunnerPin is the tree's pin of its tlacheck runner, from its CI workflows: a
// workflow that names tlacheck and nova-tools and checks out a full commit, or installs
// tlacheck at one. ok is false when no workflow pins one. Two workflows pinning two
// revisions is an error naming both: the tree's records have one runner.
func treeRunnerPin(dir string) (pin runnerPin, ok bool, err error) {
	var files []string
	for _, pat := range []string{"*.yml", "*.yaml"} {
		m, _ := filepath.Glob(filepath.Join(dir, ".github", "workflows", pat))
		files = append(files, m...)
	}
	slices.Sort(files)
	var pins []runnerPin
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return runnerPin{}, false, err
		}
		if !bytes.Contains(raw, []byte("tlacheck")) || !bytes.Contains(raw, []byte("nova-tools")) {
			continue
		}
		rel, _ := filepath.Rel(dir, f)
		sc := bufio.NewScanner(bytes.NewReader(raw))
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for n := 1; sc.Scan(); n++ {
			l := sc.Text()
			m := pinInstallRE.FindStringSubmatch(l)
			if m == nil {
				m = pinCheckoutRE.FindStringSubmatch(l)
			}
			if m != nil {
				pins = append(pins, runnerPin{File: filepath.ToSlash(rel), Line: n, Rev: m[1]})
			}
		}
	}
	if len(pins) == 0 {
		return runnerPin{}, false, nil
	}
	for _, p := range pins[1:] {
		if p.Rev != pins[0].Rev {
			return pins[0], true, fmt.Errorf("the tree pins its TLC runner at two nova-tools revisions, %s (%s:%d) and %s (%s:%d); its records have one runner",
				pins[0].Rev, pins[0].File, pins[0].Line, p.Rev, p.File, p.Line)
		}
	}
	return pins[0], true, nil
}

// landRunner is the runner a tree's records are fingerprinted with, and how the lander
// names it in a refusal.
type landRunner struct {
	files map[string][]byte
	name  string
}

// runnerDigest is the first 12 hex digits of the SHA-256 of the runner's result files, so
// two runners a refusal names can be told apart even when a revision is unknown.
func runnerDigest(files map[string][]byte) string {
	h := sha256.New()
	for _, name := range tlc.ResultFiles {
		p := tlc.RunnerDir + "/" + name
		fmt.Fprintf(h, "%s\x00%d\x00", p, len(files[p]))
		h.Write(files[p])
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

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
