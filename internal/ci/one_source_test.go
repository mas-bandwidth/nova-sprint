package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// novaToolsModule is the module nova-sprint depends on instead of carrying copies of it.
const novaToolsModule = "github.com/mas-bandwidth/nova-tools"

// oneSourceAllowedFile names the pkg/<name> copies still allowed while their sprint
// behaviour moves into nova-tools: "<name>\t<reason>" a line, '#' lines comments.
const oneSourceAllowedFile = "one_source_allowed.txt"

// oneSourceAllowed reads oneSourceAllowedFile's text into name -> reason; a line
// with no reason, or a name listed twice, is red.
func oneSourceAllowed(t *testing.T, text string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, line := range allowedLines(text) {
		name, reason, ok := strings.Cut(line, "\t")
		name, reason = strings.TrimSpace(name), strings.TrimSpace(reason)
		require.True(t, ok && name != "" && reason != "", "%s: %q is not <name><TAB><reason>", oneSourceAllowedFile, line)
		_, dup := out[name]
		require.False(t, dup, "%s lists %s twice", oneSourceAllowedFile, name)
		out[name] = reason
	}
	return out
}

// pkgDirs is the names of the directories directly under dir/pkg.
func pkgDirs(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "pkg"))
	require.NoError(t, err)
	out := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			out[e.Name()] = true
		}
	}
	return out
}

// novaToolsDir is the directory of the nova-tools version go.mod requires, read from
// the module cache: GOPROXY=off and -mod=readonly, so the scan never fetches.
func novaToolsDir(t *testing.T, root string) string {
	t.Helper()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedModule,
		Dir:  root,
		Env:  append(os.Environ(), "GOPROXY=off", "GOFLAGS=-mod=readonly"),
	}
	pkgs, err := packages.Load(cfg, novaToolsModule+"/pkg/log")
	require.NoError(t, err, "go.mod must require %s", novaToolsModule)
	require.Len(t, pkgs, 1)
	require.Empty(t, pkgs[0].Errors, "loading %s/pkg/log from the module cache", novaToolsModule)
	require.NotNil(t, pkgs[0].Module, "%s/pkg/log has no module: go.mod must require %s", novaToolsModule, novaToolsModule)
	require.Equal(t, novaToolsModule, pkgs[0].Module.Path)
	require.NotEmpty(t, pkgs[0].Module.Dir)
	return pkgs[0].Module.Dir
}

// copiedPackages is every name with both a nova-sprint pkg/<name> and a nova-tools
// pkg/<name>, sorted.
func copiedPackages(sprint, tools map[string]bool) []string {
	var out []string
	for name := range sprint {
		if tools[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// oneSourceProblems is the lint's verdict: a copy not allowed, and an allowance
// that names no copy any more.
func oneSourceProblems(copies []string, allowed map[string]string) []string {
	isCopy := map[string]bool{}
	var problems []string
	for _, name := range copies {
		isCopy[name] = true
		if _, ok := allowed[name]; !ok {
			problems = append(problems, "pkg/"+name+" is a copy of "+novaToolsModule+"/pkg/"+name+": delete it and import nova-tools")
		}
	}
	var stale []string
	for name := range allowed {
		if !isCopy[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	for _, name := range stale {
		problems = append(problems, oneSourceAllowedFile+" lists "+name+", which is no longer a copy: remove the line")
	}
	return problems
}

// TestNoPackageIsACopyOfANovaToolsPackage: nova-sprint depends on nova-tools and
// carries no copy of it. A nova-sprint pkg/<name> whose name nova-tools (at the
// required version) also has under pkg/ is red, unless one_source_allowed.txt
// lists it; a listed name that is no longer a copy is red until its line goes.
func TestNoPackageIsACopyOfANovaToolsPackage(t *testing.T) {
	t.Parallel()
	root := "../.."
	b, err := os.ReadFile(oneSourceAllowedFile)
	require.NoError(t, err)
	allowed := oneSourceAllowed(t, string(b))
	copies := copiedPackages(pkgDirs(t, root), pkgDirs(t, novaToolsDir(t, root)))
	problems := oneSourceProblems(copies, allowed)
	require.Empty(t, problems, "one true source: nova-sprint carries copies of nova-tools packages:\n%s", strings.Join(problems, "\n"))
}

// TestTheOneSourceScanSeesAKnownYesAndAKnownNo proves the verdict on cases whose
// answer is known, both ways.
func TestTheOneSourceScanSeesAKnownYesAndAKnownNo(t *testing.T) {
	t.Parallel()
	sprint := map[string]bool{"log": true, "swarm": true, "ratebudget": true}
	tools := map[string]bool{"log": true, "swarm": true, "friend": true}
	copies := copiedPackages(sprint, tools)
	require.Equal(t, []string{"log", "swarm"}, copies)
	require.Empty(t, oneSourceProblems(copies, map[string]string{"log": "r", "swarm": "r"}))
	got := oneSourceProblems(copies, map[string]string{"swarm": "r", "friend": "r"})
	require.Len(t, got, 2)
	require.Contains(t, got[0], "pkg/log is a copy")
	require.Contains(t, got[1], "lists friend, which is no longer a copy")
}

// TestTheOneSourceAllowanceOnlyShrinks: against the base a change is merged into
// (UNIT_RULE_BASE, as CI names it for the unit rule's allowance; unset, the local
// origin/v1.3/comms, origin/sprint/comfort, origin/dev or origin/main if git has one,
// else not checked), every name in one_source_allowed.txt is in the base's copy: a new
// allowance is red, a removed one is the merge it records. A base with no list yet
// passes (the allowance's first change). No fetch runs here.
func TestTheOneSourceAllowanceOnlyShrinks(t *testing.T) {
	t.Parallel()
	base := os.Getenv("UNIT_RULE_BASE")
	if base == "" {
		for _, ref := range []string{"refs/remotes/origin/v1.3/comms", "refs/remotes/origin/sprint/comfort", "refs/remotes/origin/dev", "refs/remotes/origin/main"} {
			if exec.Command("git", "rev-parse", "--verify", "--quiet", ref+"^{commit}").Run() == nil {
				base = ref
				break
			}
		}
	}
	if base == "" {
		t.Skip("UNIT_RULE_BASE names no base revision and git has no origin/v1.3/comms, origin/sprint/comfort, origin/dev or origin/main here; CI sets it to the pull request's base")
	}
	if out, err := exec.Command("git", "rev-parse", "--verify", "--quiet", base+"^{commit}").CombinedOutput(); err != nil {
		t.Fatalf("UNIT_RULE_BASE %s is no commit git can read here (%v: %s): the allowance's shrinking is unchecked", base, err, strings.TrimSpace(string(out)))
	}
	const path = "internal/ci/" + oneSourceAllowedFile
	if exec.Command("git", "cat-file", "-e", base+":"+path).Run() != nil {
		t.Logf("%s holds no %s: this change is the allowance's first", base, oneSourceAllowedFile)
		return
	}
	out, err := exec.Command("git", "show", base+":"+path).Output()
	require.NoError(t, err, "git show %s:%s", base, path)
	was := oneSourceAllowed(t, string(out))
	b, err := os.ReadFile(oneSourceAllowedFile)
	require.NoError(t, err)
	var added []string
	for name := range oneSourceAllowed(t, string(b)) {
		if _, ok := was[name]; !ok {
			added = append(added, name)
		}
	}
	sort.Strings(added)
	if len(added) > 0 {
		t.Errorf("%s allows %d copies that %s does not: the list only shrinks; port the behaviour into nova-tools and import it instead:\n%s",
			oneSourceAllowedFile, len(added), base, strings.Join(added, "\n"))
	}
}
