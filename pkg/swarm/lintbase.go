package swarm

import (
	"context"
	"fmt"

	"os/exec"
	"path"
	"regexp"

	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/gitrun"
)

// THE BASE CHECKS: FIVE RULES A CODING CARD IS HELD TO BEFORE IT IS DEALT (#2636, #3083).
//
// The first four are each one class of the 2026-09-22 sprint's failed cards (a friend's
// reports/failed-cards-2026-09-22.md), and each needs evidence the card text alone
// does not hold, so they run only under `nova-swarm lint --base-check`:
//
//	paths-at-base  every PATHS entry resolves at the card's base-sha, or is a new
//	               `_test` file, or (on a repair card) at its PR-HEAD. Class 9: card-nx-f19 named example.com/decide/entry.go,
//	               a placeholder under a foreign root that does not exist at its base, and ABSTAINed out-of-scope.
//	no-push-steps  no STEP runs `git push` or `gh`. Class 8: 39 nx-r and holdfix
//	               repair cards asked the wall, which holds no credential by design,
//	               to push and to post the REPAIR comment. The harvest pushes.
//	leg-in-fleet   every LEG/LEGS entry is a leg the fleet's leg table carries. The
//	               seven nx-r cards cut `LEG: lisp` were held: no bench carries
//	               `lisp`, the fleet's leg is `sbcl`.
//	deadline-p95   DEADLINE is at or above the measured p95 wall of the card's KIND.
//	               Class 11: 27 cards died at 1526 s against a 1500 s DEADLINE.
//	donewhen-test-name  DONE-WHEN names a test runner and a literal test that is
//	               absent at base-sha, so it can be red there (#3083, Jev's insertion
//	               2). 321 of 543 post-tune ledger rows read `donewhen missing`: the
//	               card never named a control a test could fail. "applied cleanly" and
//	               "make preflight" are English outcomes, not controls.
//
// NO EVIDENCE IS NOT NEGATIVE EVIDENCE. A check whose evidence was not handed over
// (no repository, a base-sha the repository does not hold, no leg table, no p95
// row for the kind) does not pass: it draws its finding with MISSING in the
// excerpt, naming what to hand over. `--base-check` is the ask for these checks,
// and a check that could not run is not a check that passed.

// CardBaseRemedies is what each base token wants, in the table shape of
// CardHeaderRemedies, so `nova-swarm lint --rules` prints them beside the rest. The brief
// tokens nova-sprint add holds a card brief to at its BASE tip (lintpaths.go) are beside
// them in BriefBaseRemedies; add runs paths-at-base and donewhen-test-name there too.
var CardBaseRemedies = map[string]string{
	"paths-at-base":      "every PATHS entry names a file, directory or glob that exists at the card's base-sha (or is a new `_test` file); cut the card from the tree at that sha, not from the issue's words, and hand the lint a repository holding the sha with `--repo <dir>`",
	"no-push-steps":      "a card ends at a local commit: no STEP runs `git push` or `gh`, because the wall holds no credential and the member pushes the card's commit at its finish; say `no gh, no push` in RULES, never as a STEP command",
	"leg-in-fleet":       "LEG: (or each LEGS: entry) is a leg the fleet's leg table carries -- `sbcl`, not `lisp` -- and the table is handed over with `--legs <file>`",
	"donewhen-test-name": "DONE-WHEN: names the runner and a literal test that does not exist at the card's base-sha -- `go test ./<pkg> -run <TestName>`, `pytest <file>::<test_name>` (or `-k <test_name>`), `cargo test <name>` -- so the test can be red on base and green at head; an English outcome (\"applied cleanly\", \"make preflight\") or a runner with no test named is not a control, and the repository holding base-sha is handed over with `--repo <dir>`",
	"deadline-p95":       "DEADLINE: is at or above the measured p95 wall of the card's KIND, in seconds (or `finish within <n> minutes`), and the p95 table is handed over with `--p95 <file>`; a kind with no row needs a `*` row or a measurement first",
}

// LintCardBase returns the base-check findings for one card, in the order the
// tokens are listed above.

// 1. paths-at-base.

// A REPAIR CARD'S PATHS ARE THE PR'S OWN FILES. A card that carries
// `PR-HEAD:` works on the PR's branch, and a file the PR adds is absent at
// base by construction; such an entry resolves at PR-HEAD instead. A
// PR-HEAD the repository does not hold is MISSING, never a pass.

// 2. no-push-steps.

// 3. leg-in-fleet.

// 4. deadline-p95.

// 5. donewhen-test-name.

// doneWhenValue is the card's DONE-WHEN: the header line when the header carries one,
// else the first `DONE-WHEN:` line anywhere in the card (a bullet or bold key allowed),
// which is where a card cut from an issue body carries it.

// doneTest is one literal test a DONE-WHEN names, and the runner that names it.
type doneTest struct {
	runner string // go, pytest, cargo
	name   string
	// scope is the command's own targets, as written: go packages (`./pkg/decide`,
	// `./x/...`, `<module>/x`), pytest files or directories (`tests/test_x.py`, `tests`).
	// The lookup searches only there, so a same-named test in another package or file
	// does not count as present. Empty is the whole repository (no target named).
	scope []string
}

// doneWhenTests is every literal test the DONE-WHEN names, or nil and why not. A go
// `-run` pattern is split on `|`, stripped of `^`/`$` anchors and of a `/subtest`,
// and each piece must then be a literal `Test...` name: a regex that is no name
// (`TestDecide.*`) names no test a lookup can find.

// testDefinedAt says whether the tree at sha defines the test, by its runner's
// definition shape: `func Name(` in a `_test.go` file, `def name(` in a Python file,
// `fn name(` in a Rust file. `git grep` exits 1 on no match, which is an answer
// (absent), not an error.
func testDefinedAt(repo, sha string, tn doneTest) (bool, error) {
	// `git grep` has no separator that keeps a tree-ish an operand on every git the benches
	// carry (2.43 reads `--end-of-options` there as a revision), so the tree-ish is held to
	// the shape git itself printed, never a value a card wrote: the 40 hex digits of
	// rev-parse, which cannot start with `-`. The class test allows this one site for that.
	if !fullHexSHA(sha) {
		return false, fmt.Errorf("the tree %q is not a full commit sha", sha)
	}
	var pat string
	var specs []string
	switch tn.runner {
	case "go":
		pat, specs = `^func[ \t]+`+tn.name+`[ \t]*\(`, goTestPathspecs(repo, sha, tn.scope)
	case "pytest":
		pat, specs = `^[ \t]*(async[ \t]+)?def[ \t]+`+tn.name+`[ \t]*\(`, pytestPathspecs(repo, sha, tn.scope)
	default:
		pat, specs = `fn[ \t]+`+tn.name+`[ \t]*[(<]`, []string{"*.rs"}
	}
	args := append([]string{"grep", "-q", "-E", "-e", pat, sha, "--"}, specs...)
	res, err := gitrun.Run(context.Background(), gitrun.Options{C: repo}, args...)
	if err == nil {
		return true, nil
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 && strings.TrimSpace(string(res.Stderr)) == "" {
		return false, nil
	}
	if msg := strings.TrimSpace(string(res.Stderr)); msg != "" {
		return false, fmt.Errorf("%v: %s", err, msg)
	}
	return false, err
}

// goTestTargets is the package arguments of one `go test` command: `.`, `./x`,
// `./x/...`, `../x`, or an import path (`github.com/org/repo/x`). A flag value
// (`-count 1`, `-timeout 60s`) is no package and is left out.

// pytestValueOptions is the pytest options (core, pytest-cov, -xdist, -timeout,
// -html, -randomly, -rerunfailures, -repeat, hypothesis, -asyncio, -django) whose
// required value is the next word when no `=` joins it (options whose value is
// optional, like --cov, are left out): `--rootdir tests` names a directory
// to root at, not a directory of tests to run.
var pytestValueOptions = map[string]bool{}

func init() {
	for _, o := range []string{
		"-k", "-m", "-p", "-c", "-o", "-r", "-W", "-n", "--rootdir", "--confcutdir",
		"--basetemp", "--config-file", "--inifile", "--ignore", "--ignore-glob", "--deselect",
		"--override-ini", "--pythonwarnings", "--maxfail", "--tb", "--durations",
		"--durations-min", "--capture", "--import-mode", "--junitxml", "--junit-xml",
		"--junit-prefix", "--log-level", "--log-file", "--log-file-level", "--log-format",
		"--log-date-format", "--log-cli-level", "--log-cli-format", "--log-cli-date-format",
		"--show-capture", "--doctest-glob", "--color", "--code-highlight", "--pdbcls",
		"--last-failed-no-failures", "--lfnf", "--cov-report", "--cov-config",
		"--cov-fail-under", "--numprocesses", "--maxprocesses", "--dist", "--timeout",
		"--timeout-method", "--html", "--randomly-seed", "--reruns", "--reruns-delay",
		"--count", "--hypothesis-profile", "--hypothesis-seed", "--asyncio-mode", "--ds",
	} {
		pytestValueOptions[o] = true
	}
}

// pytestTakesValue says whether a pytest option word is followed by its value: a
// short value flag (`-k expr`), or a long option from pytestValueOptions written
// without `=` (`--rootdir tests`). `--opt=value` and attached short values
// (`-ktest_x`) carry their value in the same word.

// pytestTargets is the file and directory arguments of one pytest command: every
// word that is no flag, no flag's value (`-k x`, `--rootdir tests`) and no node id.

// an option whose value is the next word, never a target

// goTestPathspecs is the `*_test.go` pathspecs of the named packages at sha: a
// package is its own directory, `/...` adds every directory under it, and an import
// path is read against the module line of go.mod at sha. No package that resolves
// in this repository means the whole repository (the command's cwd is unknown).
func goTestPathspecs(repo, sha string, scope []string) []string {
	module := ""
	if len(scope) > 0 {
		if gm, err := baseGit(repo, "show", "--end-of-options", sha+":go.mod"); err == nil {
			for _, l := range strings.Split(gm, "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(l), "module "); ok {
					module = strings.Trim(strings.TrimSpace(v), `"`)
					break
				}
			}
		}
	}
	var specs []string
	for _, t := range scope {
		recursive := t == "..." || strings.HasSuffix(t, "/...")
		t = strings.TrimSuffix(strings.TrimSuffix(t, "..."), "/")
		switch {
		case strings.HasPrefix(t, "../"):
			continue
		case t == "." || t == "":
			t = ""
		case strings.HasPrefix(t, "./"):
			t = strings.TrimPrefix(t, "./")
		case module != "" && t == module:
			t = ""
		case module != "" && strings.HasPrefix(t, module+"/"):
			t = strings.TrimPrefix(t, module+"/")
		default:
			continue
		}
		specs = append(specs, globSpec(t, recursive, "*_test.go"))
	}
	if len(specs) == 0 {
		return []string{"*_test.go"}
	}
	return specs
}

// pytestPathspecs is the pathspecs of the named pytest targets at sha: a node id's
// or argument's `.py` file as written (absent at base is an answer: no test there),
// a directory as every `*.py` under it. No target means the whole repository.
func pytestPathspecs(repo, sha string, scope []string) []string {
	var specs []string
	for _, t := range scope {
		t = strings.TrimSuffix(strings.TrimPrefix(t, "./"), "/")
		if strings.HasSuffix(t, ".py") {
			specs = append(specs, ":(literal)"+t)
			continue
		}
		if typ, err := baseGit(repo, "cat-file", "-t", "--end-of-options", sha+":"+t); err == nil && strings.TrimSpace(typ) == "tree" {
			specs = append(specs, globSpec(t, true, "*.py"))
		}
	}
	if len(specs) == 0 {
		return []string{"*.py"}
	}
	return specs
}

// globSpec is a `:(glob)` pathspec for files named like base in dir, and in every
// directory under it when recursive; dir "" is the repository root.
func globSpec(dir string, recursive bool, base string) string {
	p := ":(glob)"
	if dir != "" {
		p += dir + "/"
	}
	if recursive {
		p += "**/"
	}
	return p + base
}

// oneLineCap is v with its length capped at n bytes, for an excerpt.

// contractSha is the `sha=<hex>` on the contract line, or "".

var hexRE = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

func short12(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// baseGit runs one git command in repo and returns its trimmed stdout.
func baseGit(repo string, args ...string) (string, error) {
	return gitrun.Output(context.Background(), gitrun.Options{C: repo}, args...)
}

// pathsMissingAt lists the tree at sha once, and returns every entry that names
// nothing in it. An entry matches a file, or a directory (a prefix of a file), or,
// as a glob, any file hygiene.MatchGlob matches -- the matcher the gate uses. A
// NEW `_test` file is the one entry allowed to be absent: a card that writes the
// red test first creates it.

// newTestFile is a literal path whose base name is a test file: `x_test.go`,
// `x_test.py`, `test_x.py`. A glob is never one.
func newTestFile(p string) bool {
	if strings.ContainsAny(p, "*?[") {
		return false
	}
	b := path.Base(p)
	return strings.Contains(b, "_test.") || strings.HasPrefix(b, "test_")
}

// pushStepLines is every line inside a STEP that runs `git push` or `gh`. A step
// runs from its STEP line to the next STEP line, the next markdown heading, or a
// line that opens the `RULES` paragraph -- which is where a card says what it may
// NOT run, in words the check must not read as a command.

// deadlineSeconds reads `2700`, `2700s`, `45m` or `finish within 45 minutes`.
