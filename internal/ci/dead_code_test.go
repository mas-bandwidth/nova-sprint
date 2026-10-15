//go:build functional

package ci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The dead code rule, ported from nova-tools' internal/ci/dead_code_class_test.go:
// every package's count of functions unreachable from the module's production
// mains is held to a shrink-only ledger.
//
// Measured with `deadcode -test` (golang.org/x/tools/cmd/deadcode, the go.mod
// tool) over every main under ./cmd/... and ./tools/..., under GOOS linux, darwin
// and windows. A function counts as dead for a GOOS only when it is unreachable
// under every tag set the module uses (deadCodeTagSets): a helper reached only by
// functional tests is live. The per-package count is the union over the GOOSes.
// A root that does not build for a GOOS (cmd/nova-sprint on windows) is skipped
// for that GOOS with a printed line naming it, and the packages that root reaches
// are not judged on that GOOS: without their root they would all read as dead.
//
// Every ledger row is `<package> <count>`, '#' lines are comments. The ledger
// only shrinks: NOVA_CI_UPDATE=1 lowers counts and drops rows at zero, and it
// refuses to raise a count or add a row.
const (
	deadCodeLedgerPath    = "testdata/dead_code_allowlist.txt"
	deadCodeUpdateEnv     = "NOVA_CI_UPDATE"
	deadCodeUpdateCommand = "go test -tags functional -run '^TestDeadCode$' ./internal/ci/"
	deadCodeRemedy        = "delete the unreachable function(s) on their evidence or wire them into a production main; the dead code ledger only shrinks"
	deadCodeModule        = "github.com/mas-bandwidth/nova-sprint/"
)

// deadCodeGOOS are the operating systems measured; deadCodeTagSets are the build
// tag sets the module's files use ("" is no tags).
var (
	deadCodeGOOS    = []string{"linux", "darwin", "windows"}
	deadCodeTagSets = []string{"", "functional", "functional,slow", "slow", "swarmtest", "perf"}
)

type deadcodePackage struct {
	Path  string `json:"Path"`
	Funcs []struct {
		Name      string `json:"Name"`
		Generated bool   `json:"Generated"`
		Marker    bool   `json:"Marker"`
	} `json:"Funcs"`
}

// deadCodeEnv is the environment for a go or deadcode run for one GOOS.
func deadCodeEnv(goos string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GOOS=") || strings.HasPrefix(kv, "GOARCH=") {
			continue
		}
		env = append(env, kv)
	}
	if goos != "" {
		env = append(env, "GOOS="+goos)
	}
	return env
}

// deadcodeBinary builds the deadcode tool for the host and returns its path: `go
// tool deadcode` under a foreign GOOS would build a binary the host cannot run.
func deadcodeBinary(t *testing.T, ctx context.Context, root string) string {
	t.Helper()
	build := exec.CommandContext(ctx, "go", "tool", "deadcode", "-h")
	build.Dir = root
	build.Env = deadCodeEnv("")
	build.WaitDelay = 5 * time.Second
	_ = build.Run() // -h exits non-zero; the run compiles the tool into the cache
	cmd := exec.CommandContext(ctx, "go", "tool", "-n", "deadcode")
	cmd.Dir = root
	cmd.Env = deadCodeEnv("")
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.Output()
	require.NoError(t, err, "resolving deadcode binary via go tool -n deadcode")
	bin := strings.TrimSpace(string(out))
	require.FileExists(t, bin, "deadcode binary must exist")
	return bin
}

// deadCodeRoot is one production main: its import path and directory.
type deadCodeRoot struct {
	importPath, dir string
}

// deadCodeRoots lists every main package under ./cmd/... and ./tools/... for goos.
func deadCodeRoots(ctx context.Context, root, goos string) ([]deadCodeRoot, error) {
	list := exec.CommandContext(ctx, "go", "list", "-e", "-f", "{{if eq .Name \"main\"}}{{.ImportPath}} {{.Dir}}{{end}}", "./cmd/...", "./tools/...")
	list.Dir = root
	list.Env = deadCodeEnv(goos)
	list.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	list.Stdout, list.Stderr = &stdout, &stderr
	if err := list.Run(); err != nil {
		return nil, fmt.Errorf("listing mains for GOOS=%s: %w\nstderr: %s", goos, err, stderr.String())
	}
	var roots []deadCodeRoot
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		path, dir, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok {
			roots = append(roots, deadCodeRoot{importPath: path, dir: dir})
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("no mains under ./cmd/... or ./tools/... for GOOS=%s", goos)
	}
	return roots, nil
}

// unbuiltRoots names the roots whose own directory holds a file named in a
// failed deadcode run's diagnostics: those roots do not build for the GOOS.
func unbuiltRoots(roots []deadCodeRoot, stderr string) []deadCodeRoot {
	var out []deadCodeRoot
	for _, r := range roots {
		for _, line := range strings.Split(stderr, "\n") {
			file, _, ok := strings.Cut(line, ":")
			if ok && filepath.Dir(file) == r.dir {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// deadFuncs runs deadcode -test for one GOOS and tag set and returns the set of
// "<package> <func>" keys it reports, skipping roots that do not build.
func deadFuncs(t *testing.T, ctx context.Context, bin, root, goos, tags string, roots []deadCodeRoot) (map[string]bool, []deadCodeRoot, error) {
	t.Helper()
	for {
		args := []string{"-test", "-json", "-tags", tags}
		for _, r := range roots {
			args = append(args, r.importPath)
		}
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Dir = root
		cmd.Env = deadCodeEnv(goos)
		cmd.WaitDelay = 5 * time.Second
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			skip := unbuiltRoots(roots, stderr.String())
			if len(skip) == 0 || len(skip) == len(roots) {
				return nil, nil, fmt.Errorf("running deadcode for GOOS=%s tags=%q: %w\nstderr: %s", goos, tags, err, stderr.String())
			}
			drop := map[string]bool{}
			for _, r := range skip {
				fmt.Printf("dead code: skipping root %s for GOOS=%s tags=%q: it does not build there\n", r.importPath, goos, tags)
				drop[r.importPath] = true
			}
			var kept []deadCodeRoot
			for _, r := range roots {
				if !drop[r.importPath] {
					kept = append(kept, r)
				}
			}
			roots = kept
			continue
		}
		var pkgs []deadcodePackage
		if err := json.Unmarshal(stdout.Bytes(), &pkgs); err != nil {
			return nil, nil, fmt.Errorf("parsing deadcode json for GOOS=%s tags=%q: %w", goos, tags, err)
		}
		dead := map[string]bool{}
		for _, p := range pkgs {
			for _, fn := range p.Funcs {
				if !fn.Marker && !fn.Generated {
					dead[strings.TrimPrefix(p.Path, deadCodeModule)+" "+fn.Name] = true
				}
			}
		}
		return dead, roots, nil
	}
}

// measureDeadCode returns, per package, the sorted names of functions dead under
// every tag set for at least one GOOS.
func measureDeadCode(t *testing.T, ctx context.Context, root string) map[string][]string {
	t.Helper()
	bin := deadcodeBinary(t, ctx, root)
	union := map[string]bool{}
	for _, goos := range deadCodeGOOS {
		roots, err := deadCodeRoots(ctx, root, goos)
		require.NoError(t, err)
		all := roots
		var every map[string]bool
		for _, tags := range deadCodeTagSets {
			dead, kept, err := deadFuncs(t, ctx, bin, root, goos, tags, roots)
			require.NoError(t, err)
			roots = kept
			every = intersectDead(every, dead)
		}
		unjudged, err := skippedReach(ctx, root, goos, all, roots)
		require.NoError(t, err)
		for k := range every {
			pkg, _, _ := strings.Cut(k, " ")
			if !unjudged[pkg] {
				union[k] = true
			}
		}
	}
	byPkg := map[string][]string{}
	for k := range union {
		pkg, name, _ := strings.Cut(k, " ")
		byPkg[pkg] = append(byPkg[pkg], name)
	}
	for _, names := range byPkg {
		sort.Strings(names)
	}
	return byPkg
}

// skippedReach returns the packages (module-relative) reached, tests included,
// by the roots in all that are missing from kept: a GOOS that skipped a root
// cannot judge them.
func skippedReach(ctx context.Context, root, goos string, all, kept []deadCodeRoot) (map[string]bool, error) {
	have := map[string]bool{}
	for _, r := range kept {
		have[r.importPath] = true
	}
	var skipped []string
	for _, r := range all {
		if !have[r.importPath] {
			skipped = append(skipped, r.importPath)
		}
	}
	out := map[string]bool{}
	if len(skipped) == 0 {
		return out, nil
	}
	tags := strings.Join(deadCodeTagSets, ",")
	tags = strings.Trim(strings.ReplaceAll(tags, ",,", ","), ",")
	list := exec.CommandContext(ctx, "go", append([]string{"list", "-e", "-deps", "-test", "-tags", tags, "-f", "{{.ImportPath}}"}, skipped...)...)
	list.Dir = root
	list.Env = deadCodeEnv(goos)
	list.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	list.Stdout, list.Stderr = &stdout, &stderr
	if err := list.Run(); err != nil {
		return nil, fmt.Errorf("listing what %v reaches for GOOS=%s: %w\nstderr: %s", skipped, goos, err, stderr.String())
	}
	for _, path := range strings.Fields(stdout.String()) {
		if p, ok := strings.CutPrefix(path, deadCodeModule); ok {
			p, _, _ = strings.Cut(p, " ") // test variants read "pkg [pkg.test]"
			out[p] = true
		}
	}
	fmt.Printf("dead code: GOOS=%s does not judge the %d packages reached by %s\n", goos, len(out), strings.Join(skipped, ", "))
	return out, nil
}

// intersectDead keeps the keys in both sets; a nil acc is the first set.
func intersectDead(acc, next map[string]bool) map[string]bool {
	if acc == nil {
		return next
	}
	out := map[string]bool{}
	for k := range acc {
		if next[k] {
			out[k] = true
		}
	}
	return out
}

// deadCodeLedger is the parsed ledger: its comment header and its rows.
type deadCodeLedger struct {
	header []string
	rows   map[string]int
}

func parseDeadCodeLedger(text string) (deadCodeLedger, error) {
	l := deadCodeLedger{rows: map[string]int{}}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			l.header = append(l.header, line)
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 {
			return l, fmt.Errorf("line %d: %q is not `<package> <count>`", i+1, line)
		}
		n, err := strconv.Atoi(f[1])
		if err != nil || n <= 0 {
			return l, fmt.Errorf("line %d: %q: count must be a positive integer", i+1, line)
		}
		if _, dup := l.rows[f[0]]; dup {
			return l, fmt.Errorf("line %d: package %s listed twice", i+1, f[0])
		}
		l.rows[f[0]] = n
	}
	return l, nil
}

func (l deadCodeLedger) text() string {
	var b strings.Builder
	for _, h := range l.header {
		b.WriteString(h + "\n")
	}
	pkgs := make([]string, 0, len(l.rows))
	for p := range l.rows {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	for _, p := range pkgs {
		fmt.Fprintf(&b, "%s %d\n", p, l.rows[p])
	}
	return b.String()
}

// checkDeadCodeLedger compares measured counts with the ledger. Without update,
// any difference is a problem. With update, lowered and stale rows are written
// into the returned ledger, while a raised count or a new package stays a problem.
func checkDeadCodeLedger(l deadCodeLedger, measured map[string][]string, update bool) ([]string, deadCodeLedger) {
	var problems []string
	next := deadCodeLedger{header: l.header, rows: map[string]int{}}
	sample := func(pkg string) string {
		names := measured[pkg]
		if len(names) > 5 {
			names = names[:5]
		}
		return strings.Join(names, ", ")
	}
	for pkg, names := range measured {
		listed, ok := l.rows[pkg]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("%s: %d unreachable functions not listed in the ledger (e.g. %s); the ledger refuses to grow;\n  remedy: %s", pkg, len(names), sample(pkg), deadCodeRemedy))
		case len(names) > listed:
			problems = append(problems, fmt.Sprintf("%s: %d unreachable functions, over the ledger count of %d (e.g. %s); the ledger refuses to raise a count;\n  remedy: %s", pkg, len(names), listed, sample(pkg), deadCodeRemedy))
			next.rows[pkg] = listed
		case len(names) < listed:
			if !update {
				problems = append(problems, fmt.Sprintf("%s: %d unreachable functions, below the ledger count of %d; lower the row: %s=1 %s", pkg, len(names), listed, deadCodeUpdateEnv, deadCodeUpdateCommand))
			}
			next.rows[pkg] = len(names)
		default:
			next.rows[pkg] = listed
		}
	}
	for pkg, listed := range l.rows {
		if len(measured[pkg]) == 0 {
			if !update {
				problems = append(problems, fmt.Sprintf("%s: 0 unreachable functions, listed at %d; drop the stale row: %s=1 %s", pkg, listed, deadCodeUpdateEnv, deadCodeUpdateCommand))
			}
		}
	}
	sort.Strings(problems)
	return problems, next
}

// TestDeadCode holds nova-sprint's unreachable functions to the shrink-only
// per-package ledger in testdata/dead_code_allowlist.txt.
func TestDeadCode(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 540*time.Second)
	defer cancel()

	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	raw, err := os.ReadFile(deadCodeLedgerPath)
	require.NoError(t, err, "reading the dead code ledger")
	ledger, err := parseDeadCodeLedger(string(raw))
	require.NoError(t, err, "parsing %s", deadCodeLedgerPath)

	measured := measureDeadCode(t, ctx, root)
	update := os.Getenv(deadCodeUpdateEnv) == "1"
	problems, next := checkDeadCodeLedger(ledger, measured, update)
	if update {
		if text := next.text(); text != string(raw) {
			require.NoError(t, os.WriteFile(deadCodeLedgerPath, []byte(text), 0o644))
		}
	}
	if len(problems) > 0 {
		assert.Failf(t, "dead code rule", "%s\n(ledger: %s)", strings.Join(problems, "\n"), deadCodeLedgerPath)
	}
}

// TestDeadCodeLedgerOnlyShrinks pins the ledger mechanics: a new package and a
// raised count are refused with or without update; a lowered count and a stale
// row are refused without update and written with it.
func TestDeadCodeLedgerOnlyShrinks(t *testing.T) {
	t.Parallel()
	l, err := parseDeadCodeLedger("# header\npkg/a 5\npkg/b 3\npkg/c 2\n")
	require.NoError(t, err)
	measured := map[string][]string{
		"pkg/a": {"f1", "f2", "f3", "f4", "f5", "f6"}, // over
		"pkg/b": {"g1"},                               // lowered
		"pkg/d": {"h1"},                               // unlisted
		// pkg/c is stale
	}

	problems, _ := checkDeadCodeLedger(l, measured, false)
	require.Len(t, problems, 4)
	joined := strings.Join(problems, "\n")
	assert.Contains(t, joined, "pkg/a: 6 unreachable functions, over the ledger count of 5")
	assert.Contains(t, joined, "pkg/b: 1 unreachable functions, below the ledger count of 3")
	assert.Contains(t, joined, "pkg/c: 0 unreachable functions, listed at 2")
	assert.Contains(t, joined, "pkg/d: 1 unreachable functions not listed")

	problems, next := checkDeadCodeLedger(l, measured, true)
	require.Len(t, problems, 2, "update still refuses the raise and the new row")
	assert.Equal(t, "# header\npkg/a 5\npkg/b 1\n", next.text(), "update lowers b, drops c, keeps a at 5, never adds d")

	_, err = parseDeadCodeLedger("pkg/a 1\npkg/a 2\n")
	assert.ErrorContains(t, err, "listed twice")
	_, err = parseDeadCodeLedger("pkg/a x\n")
	assert.ErrorContains(t, err, "positive integer")
}

// TestUnbuiltRootsNamesOnlyTheRootThatFails pins the skip: a diagnostic naming a
// file in a root's own directory skips that root and no other.
func TestUnbuiltRootsNamesOnlyTheRootThatFails(t *testing.T) {
	t.Parallel()
	roots := []deadCodeRoot{{"m/cmd/a", "/r/cmd/a"}, {"m/cmd/ab", "/r/cmd/ab"}, {"m/tools/c", "/r/tools/c"}}
	got := unbuiltRoots(roots, "/r/cmd/a/x.go:225:31: undefined: syscall.Stat_t\n/r/cmd/a/x_test.go:1:1: nope\n")
	assert.Equal(t, []deadCodeRoot{{"m/cmd/a", "/r/cmd/a"}}, got)
	assert.Empty(t, unbuiltRoots(roots, "/r/internal/z/z.go:1:1: broken\n"))
}
