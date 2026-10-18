package ci

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// How we write unit tests here (Glenn, 2026-10-12): "If you want to fix your flaky
// tests, don't use real time, or sockets or external service calls in unit tests."
// "That's a rule. That's how we write unit tests here."
//
// A unit test is any _test.go that builds without the functional tag. It never
// uses real time (a sleep, a timer, a wall-clock deadline, time.Now driving the
// outcome, require.Eventually's real polling), never a real socket (net.Listen,
// net.Dial, an httptest server, a unix socket, a connection deadline), never an
// external service (a redis-server, an HTTP call) and never runs a program
// (os/exec.Command, os/exec.CommandContext, or a listed helper that runs one).
// Those belong behind `//go:build functional`, or behind a fake: a fake clock, an
// in-memory transport (net.Pipe, httptest.NewRecorder, a handler RoundTripper), or
// a testing/synctest bubble, whose clock is fake.
//
// TestUnitTestsUseNoRealTimeSocketsOrServices reads every test file of the module
// and fails, naming file, line and call, on any unit-tier use of the forbidden
// calls. The one allowance is unit_rule_allowed.txt beside this file: the files that
// broke the rule when it was written, allowed until moved (v1.3), each line counting
// the uses per kind (time, socket, service, program) measured when the list was
// written. A
// listed file whose count fell below its line fails until the line is lowered, a
// listed file that keeps the rule fails until its line is removed, and an offender
// not listed fails it; TestTheUnitRuleAllowanceOnlyShrinks fails a line the merge
// base does not have or a count above the base's. So the list only shrinks, by file
// and by use, and a new offender is red at once.
//
// The scan is syntactic (go/ast, no type information), and these are its known gaps,
// out of scope here: a custom *http.Client.Do on a real transport is not an
// httptest server and is not seen; an import name shadowed by a local identifier (a
// parameter named `time`, a variable named `synctest`) is read as the package, so a
// fake clock could be refused or a false bubble excused. A dot-import of a forbidden
// package (`import . "time"`, then a bare Sleep) is seen; a dot-import of a
// package-wide ban is refused at its import. A local function of the same bare name
// in such a file would be refused with it.

// unitRuleAllowedFile names the files still allowed to break the rule, one slash
// path from the module root a line, '#' lines comments (unit_rule_allowed.txt).
const unitRuleAllowedFile = "unit_rule_allowed.txt"

// unitRuleAllowed reads unitRuleAllowedFile.
func unitRuleAllowed(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(unitRuleAllowedFile)
	require.NoError(t, err)
	return allowedLines(string(b))
}

// allowedLines is the paths of an allowlist's text: blank and '#' lines dropped.
func allowedLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// unitRuleAllowedCounts reads unitRuleAllowedFile as counted allowances: each line
// is <path> [<kind>:<n> ...], a kind absent from the line counting as 0. A line
// with no counts (the old shape) reads as no bound for its path.
func unitRuleAllowedCounts(t *testing.T) map[string]map[string]int {
	t.Helper()
	return parseAllowedCounts(strings.Join(unitRuleAllowed(t), "\n"))
}

// parseAllowedCounts parses the counted-allowance text of unitRuleAllowedFile.
func parseAllowedCounts(text string) map[string]map[string]int {
	out := map[string]map[string]int{}
	for _, line := range allowedLines(text) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		m := map[string]int{}
		for _, fld := range fields[1:] {
			i := strings.LastIndex(fld, ":")
			if i < 0 {
				continue
			}
			n, err := strconv.Atoi(fld[i+1:])
			if err != nil {
				continue
			}
			m[fld[:i]] = n
		}
		out[fields[0]] = m
	}
	return out
}

// unitRuleAllowanceDelta compares the current ledger to its merge base. The program
// kind may be measured once while migrating an old ledger that has no program counts;
// after that migration, missing program counts are zero and new paths are refused.
func unitRuleAllowanceDelta(was, now map[string]map[string]int, baseHasProgramKind bool) (added, over []string) {
	programMeasured := baseHasProgramKind
	for _, kinds := range was {
		if _, ok := kinds["program"]; ok {
			programMeasured = true
			break
		}
	}
	onlyProgram := func(kinds map[string]int) bool {
		for kind, n := range kinds {
			if kind != "program" || n == 0 {
				return false
			}
		}
		return len(kinds) > 0
	}
	for file, kinds := range now {
		if _, ok := was[file]; !ok && (programMeasured || !onlyProgram(kinds)) {
			added = append(added, file)
		}
	}
	sort.Strings(added)

	for file, kinds := range now {
		bkinds, ok := was[file]
		if !ok || len(bkinds) == 0 {
			continue
		}
		for kind, n := range kinds {
			baseN, counted := bkinds[kind]
			if kind == "program" && programMeasured && !counted {
				counted = true // a missing program count after migration is a measured zero
			}
			if counted && n > baseN {
				over = append(over, fmt.Sprintf("%s: %s uses %d > base %d", file, kind, n, baseN))
			}
		}
	}
	sort.Strings(over)
	return added, over
}

// unitRuleOverAllowance is the findings of a listed file above its counted allowance:
// one sorted "<file>: <kind> uses <n> > allowed <m>" line per file and kind above its
// count. A kind not on the file's line counts as 0.
func unitRuleOverAllowance(findings []unitRuleFinding, allowed map[string]map[string]int) []string {
	counts := map[string]map[string]int{}
	for _, f := range findings {
		if counts[f.File] == nil {
			counts[f.File] = map[string]int{}
		}
		counts[f.File][f.Kind]++
	}
	var out []string
	for file, kinds := range counts {
		for kind, n := range kinds {
			m := allowed[file][kind]
			if n > m {
				out = append(out, fmt.Sprintf("%s: %s uses %d > allowed %d", file, kind, n, m))
			}
		}
	}
	sort.Strings(out)
	return out
}

// unitRuleUnderAllowance is the counts of a listed file below its line: one sorted
// "<file>: <kind> uses <n> < allowed <m>" line per kind on the file's line above its
// count, so the line is lowered to the scan. A kind absent from the findings counts
// as 0; a file with no finding at all is left to the stale check.
func unitRuleUnderAllowance(findings []unitRuleFinding, allowed map[string]map[string]int) []string {
	counts := map[string]map[string]int{}
	for _, f := range findings {
		if counts[f.File] == nil {
			counts[f.File] = map[string]int{}
		}
		counts[f.File][f.Kind]++
	}
	var out []string
	for file, kinds := range allowed {
		if counts[file] == nil {
			continue
		}
		for kind, m := range kinds {
			if n := counts[file][kind]; n < m {
				out = append(out, fmt.Sprintf("%s: %s uses %d < allowed %d", file, kind, n, m))
			}
		}
	}
	sort.Strings(out)
	return out
}

// unitRuleFinding is one forbidden use: the file (slash path from the module
// root), its line, the kind (time, socket, service or program) and what was used.
type unitRuleFinding struct {
	File string
	Line int
	Kind string
	What string
}

func (f unitRuleFinding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", f.File, f.Line, f.Kind, f.What)
}

// unitRuleCalls is, per import path, each forbidden selector and its kind. A name
// is matched as a call or as a value (`st.Sleep = time.Sleep` is the same sleep).
var unitRuleCalls = map[string]map[string]string{
	"time": {
		"Sleep": "time", "After": "time", "Tick": "time", "NewTimer": "time",
		"NewTicker": "time", "AfterFunc": "time", "Now": "time", "Since": "time",
		"Until": "time",
	},
	"context": {
		"WithTimeout": "time", "WithDeadline": "time",
		"WithTimeoutCause": "time", "WithDeadlineCause": "time",
	},
	"net": {
		"Listen": "socket", "ListenTCP": "socket", "ListenUDP": "socket", "ListenUnix": "socket",
		"ListenUnixgram": "socket", "ListenPacket": "socket", "ListenIP": "socket",
		"ListenMulticastUDP": "socket", "FileListener": "socket",
		"Dial": "socket", "DialTimeout": "socket", "DialTCP": "socket", "DialUDP": "socket",
		"DialUnix": "socket", "DialIP": "socket", "Dialer": "socket", "ListenConfig": "socket",
	},
	"net/http/httptest": {
		"NewServer": "socket", "NewUnstartedServer": "socket", "NewTLSServer": "socket",
	},
	"net/http": {
		"ListenAndServe": "socket", "ListenAndServeTLS": "socket", "Serve": "socket",
		"ServeTLS": "socket", "Get": "service", "Post": "service", "Head": "service",
		"PostForm": "service", "DefaultClient": "service", "DefaultTransport": "service",
	},
	// testredis starts a real redis-server on a real port (its SocketPath and
	// Program only name a path)
	"github.com/mas-bandwidth/nova-tools/pkg/testredis": {
		"Start": "service", "StartServer": "service", "Far": "service", "FarLink": "service",
	},
}

// unitRuleImports is the packages whose every use is a forbidden one: delayproxy is
// a TCP proxy.
var unitRuleImports = map[string]string{
	"github.com/mas-bandwidth/nova-tools/pkg/delayproxy": "socket",
}

// unitRuleMethods is the method names forbidden on any receiver: testify's
// Eventually and Never poll on the wall clock, and a connection deadline is a
// wall-clock one.
var unitRuleMethods = map[string]string{
	"Eventually": "time", "Eventuallyf": "time", "EventuallyWithT": "time", "EventuallyWithTf": "time",
	"Never": "time", "Neverf": "time",
	"SetDeadline": "time", "SetReadDeadline": "time", "SetWriteDeadline": "time",
}

// unitRuleHelpers is this module's own test helpers that open a real socket: a
// call to one in a unit test is a socket however it is spelled at the call site.
var unitRuleHelpers = map[string]string{
	"shortSock": "socket",
}

// unitRuleProgramHelpers is this module's own test helpers that run a program: a
// call to one in a unit test is a program however it is spelled at the call site.
// The scan follows them one level (a call to the helper, not a helper it calls);
// landRig.git runs git through gitrun, which the syntactic scan cannot see.
var unitRuleProgramHelpers = map[string]bool{
	"git": true,
}

// unitRuleExecBinaries are the programs a test may run by os/exec that open a real
// socket or talk to a network.
var unitRuleExecBinaries = map[string]bool{
	"curl": true, "wget": true, "nc": true, "ncat": true, "netcat": true,
	"socat": true, "telnet": true, "ssh": true, "scp": true, "redis-cli": true,
}

// unitRuleExecProgram is the program named by an os/exec.Command or CommandContext
// call's first argument after the context, "" when it is no string literal.
func unitRuleExecProgram(method string, args []ast.Expr) string {
	progIdx := 0
	if method == "CommandContext" {
		progIdx = 1
	}
	if progIdx >= len(args) {
		return ""
	}
	lit, ok := args[progIdx].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return s
}

// unitRuleCalleeName is a call's bare name: `git` or `r.git` both name git.
func unitRuleCalleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// unitRuleExecSocket is an os/exec.Command or CommandContext call that opens a real
// socket: its program is a known networked binary, or a string-literal argument opens
// /dev/tcp or /dev/udp (bash). It returns the finding's What and whether it is one.
func unitRuleExecSocket(method string, args []ast.Expr) (string, bool) {
	prog := unitRuleExecProgram(method, args)
	if prog != "" && unitRuleExecBinaries[prog] {
		return "os/exec." + method + " " + prog, true
	}
	for _, a := range args {
		lit, ok := a.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			continue
		}
		switch {
		case strings.Contains(s, "/dev/tcp/"):
			return "os/exec." + method + " " + prog + " /dev/tcp", true
		case strings.Contains(s, "/dev/udp/"):
			return "os/exec." + method + " " + prog + " /dev/udp", true
		}
	}
	return "", false
}

// unitTier says a file's build constraint can hold without the functional tag: no
// constraint, `!functional`, or one on other tags only (slow, race, a GOOS).
func unitTier(src []byte) (bool, error) {
	expr, err := buildConstraint(src)
	if err != nil || expr == nil {
		return true, err
	}
	// every tag but functional, then every setting of them: Eval short-circuits, so
	// the tags are read off the expression itself
	var tags []string
	seen := map[string]bool{}
	var walk func(e constraint.Expr)
	walk = func(e constraint.Expr) {
		switch v := e.(type) {
		case *constraint.TagExpr:
			if v.Tag != "functional" && !seen[v.Tag] {
				seen[v.Tag] = true
				tags = append(tags, v.Tag)
			}
		case *constraint.NotExpr:
			walk(v.X)
		case *constraint.AndExpr:
			walk(v.X)
			walk(v.Y)
		case *constraint.OrExpr:
			walk(v.X)
			walk(v.Y)
		}
	}
	walk(expr)
	if len(tags) > 16 {
		return true, fmt.Errorf("a build constraint over %d tags", len(tags))
	}
	for mask := 0; mask < 1<<len(tags); mask++ {
		on := map[string]bool{}
		for i, tag := range tags {
			on[tag] = mask&(1<<i) != 0
		}
		if expr.Eval(func(tag string) bool { return tag != "functional" && on[tag] }) {
			return true, nil
		}
	}
	return false, nil
}

// buildConstraint is the file's //go:build line, nil when it has none.
func buildConstraint(src []byte) (constraint.Expr, error) {
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			return nil, nil
		}
		if constraint.IsGoBuild(line) {
			return constraint.Parse(line)
		}
	}
	return nil, nil
}

// unitRuleFile is one parsed unit-tier test file.
type unitRuleFile struct {
	rel string
	f   *ast.File
}

// scanUnitRule returns every forbidden use in the unit-tier test files under root,
// sorted by file and line. files maps a slash path (relative to root) to its source.
func scanUnitRule(files map[string][]byte) ([]unitRuleFinding, error) {
	fset := token.NewFileSet()
	// group by directory and package name: a helper is resolved within its package
	byPkg := map[string][]unitRuleFile{}
	var keys []string
	for rel, src := range files {
		in, err := unitTier(src)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		if !in {
			continue
		}
		f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		key := filepath.ToSlash(filepath.Dir(rel)) + " " + f.Name.Name
		if _, ok := byPkg[key]; !ok {
			keys = append(keys, key)
		}
		byPkg[key] = append(byPkg[key], unitRuleFile{rel: rel, f: f})
	}
	sort.Strings(keys)
	var out []unitRuleFinding
	for _, key := range keys {
		out = append(out, scanUnitRulePackage(fset, byPkg[key])...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

// unitRuleUse is one forbidden use found while walking a declaration, before the
// bubble rule is applied: bubbled says it sits inside a synctest bubble's function.
type unitRuleUse struct {
	finding unitRuleFinding
	bubbled bool
	decl    string
}

// scanUnitRulePackage walks one package's unit-tier test files. A time use inside a
// testing/synctest bubble (the function given to synctest.Test or synctest.Run) runs
// on the bubble's fake clock and is no finding; so is one inside a helper of the
// package's test files whose every use is inside a bubble or another such helper.
// A socket or a service is a finding in a bubble too.
func scanUnitRulePackage(fset *token.FileSet, files []unitRuleFile) []unitRuleFinding {
	var uses []unitRuleUse
	// refs[name] is every reference to a top-level function or method of that
	// name: in a bubble or not, and from which declaration
	type ref struct {
		bubbled bool
		decl    string
	}
	refs := map[string][]ref{}
	declared := map[string]bool{}
	for _, uf := range files {
		for _, d := range uf.f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				declared[fd.Name.Name] = true
			}
		}
	}
	for _, uf := range files {
		imports := map[string]string{}
		var dotImports []string // packages dot-imported: their names are bare identifiers
		type forbiddenDotImport struct {
			spec *ast.ImportSpec
			path string
			kind string
		}
		var forbiddenDotImports []forbiddenDotImport
		for _, is := range uf.f.Imports {
			p, _ := strconv.Unquote(is.Path.Value)
			name := filepath.Base(p)
			if strings.HasPrefix(name, "go-redis") || p == "github.com/redis/go-redis/v9" {
				name = "redis"
			}
			if is.Name != nil {
				name = is.Name.Name
			}
			if name == "." {
				dotImports = append(dotImports, p)
				if kind, forbidden := unitRuleImports[p]; forbidden {
					forbiddenDotImports = append(forbiddenDotImports, forbiddenDotImport{is, p, kind})
				}
				continue
			}
			imports[name] = p
		}
		find := func(n ast.Node, kind, what string) unitRuleFinding {
			return unitRuleFinding{File: uf.rel, Line: fset.Position(n.Pos()).Line, Kind: kind, What: what}
		}
		// A whole-package ban cannot resolve a dot-import's bare names syntactically;
		// refuse that import itself rather than miss its exported socket entry points.
		for _, dot := range forbiddenDotImports {
			uses = append(uses, unitRuleUse{finding: find(dot.spec, dot.kind, dot.path+" (dot import)")})
		}
		var walk func(n ast.Node, bubbled bool, decl string)
		walk = func(n ast.Node, bubbled bool, decl string) {
			ast.Inspect(n, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.CallExpr:
					if name := unitRuleCalleeName(v.Fun); unitRuleProgramHelpers[name] && declared[name] {
						uses = append(uses, unitRuleUse{find(v, "program", name), bubbled, decl})
					}
					if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
						if id, ok := sel.X.(*ast.Ident); ok && imports[id.Name] == "os/exec" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext") {
							what := "os/exec." + sel.Sel.Name
							if prog := unitRuleExecProgram(sel.Sel.Name, v.Args); prog != "" {
								what += " " + prog
							}
							uses = append(uses, unitRuleUse{find(v, "program", what), bubbled, decl})
							if socket, isSocket := unitRuleExecSocket(sel.Sel.Name, v.Args); isSocket {
								uses = append(uses, unitRuleUse{find(v, "socket", socket), bubbled, decl})
							}
						}
						if id, ok := sel.X.(*ast.Ident); ok && imports[id.Name] == "testing/synctest" && (sel.Sel.Name == "Test" || sel.Sel.Name == "Run") {
							walk(v.Fun, bubbled, decl)
							for _, a := range v.Args {
								switch a := a.(type) {
								case *ast.FuncLit:
									// the callback's body runs inside the bubble
									walk(a, true, decl)
								case *ast.Ident:
									// a named callback is passed, not called, here: its body runs
									// inside the bubble (a bubble-only helper); a plain name has no use
									walk(a, true, decl)
								default:
									// every other argument is evaluated before the bubble starts:
									// synctest.Test(t, mk(time.Now())) reads the real clock
									walk(a, bubbled, decl)
								}
							}
							return false
						}
					}
				case *ast.SelectorExpr:
					if id, ok := v.X.(*ast.Ident); ok {
						if p, ok := imports[id.Name]; ok {
							if kind, ok := unitRuleCalls[p][v.Sel.Name]; ok {
								uses = append(uses, unitRuleUse{find(v, kind, p+"."+v.Sel.Name), bubbled, decl})
							} else if kind, ok := unitRuleImports[p]; ok {
								uses = append(uses, unitRuleUse{find(v, kind, p+"."+v.Sel.Name), bubbled, decl})
							} else if kind, ok := unitRuleMethods[v.Sel.Name]; ok {
								// require.Eventually, assert.Never: the package-level spelling
								uses = append(uses, unitRuleUse{find(v, kind, "."+v.Sel.Name), bubbled, decl})
							}
							return false
						}
					}
					if kind, ok := unitRuleMethods[v.Sel.Name]; ok {
						uses = append(uses, unitRuleUse{find(v, kind, "."+v.Sel.Name), bubbled, decl})
					}
					if declared[v.Sel.Name] {
						refs[v.Sel.Name] = append(refs[v.Sel.Name], ref{bubbled, decl})
					}
				case *ast.Ident:
					if kind, ok := unitRuleHelpers[v.Name]; ok {
						uses = append(uses, unitRuleUse{find(v, kind, v.Name), bubbled, decl})
					}
					// a dot-imported package's names are bare: `import . "time"` then Sleep(1)
					for _, p := range dotImports {
						if kind, ok := unitRuleCalls[p][v.Name]; ok {
							uses = append(uses, unitRuleUse{find(v, kind, p+"."+v.Name), bubbled, decl})
						}
					}
					if declared[v.Name] {
						refs[v.Name] = append(refs[v.Name], ref{bubbled, decl})
					}
				}
				return true
			})
		}
		for _, d := range uf.f.Decls {
			switch v := d.(type) {
			case *ast.FuncDecl:
				if v.Recv != nil {
					walk(v.Recv, false, "")
				}
				walk(v.Type, false, "")
				if v.Body != nil {
					walk(v.Body, false, v.Name.Name)
				}
			default:
				walk(d, false, "")
			}
		}
	}
	// bubbleOnly[name]: every reference to the helper is inside a bubble or inside
	// another bubble-only helper (and there is at least one), so its body runs on the
	// fake clock. Grown to a fixpoint from the direct bubble references.
	bubbleOnly := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for name, rs := range refs {
			if bubbleOnly[name] || len(rs) == 0 {
				continue
			}
			all := true
			for _, r := range rs {
				if !r.bubbled && !(r.decl != "" && r.decl != name && bubbleOnly[r.decl]) {
					all = false
					break
				}
			}
			if all {
				bubbleOnly[name] = true
				changed = true
			}
		}
	}
	var out []unitRuleFinding
	for _, u := range uses {
		if u.finding.Kind == "time" && (u.bubbled || (u.decl != "" && bubbleOnly[u.decl])) {
			continue
		}
		out = append(out, u.finding)
	}
	return out
}

// moduleTestFiles is every visible _test.go under root (hidden, tmp and testdata
// trees skipped), keyed by its slash path from root.
func moduleTestFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if filepath.Clean(path) != filepath.Clean(root) && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			switch d.Name() {
			case ".git", "tmp", "testdata", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = src
		return nil
	})
	require.NoError(t, err)
	return files
}

func TestModuleTestFilesSkipsHiddenDirectoriesButScansRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".hk-root")
	require.NoError(t, os.Mkdir(root, 0700))
	write := func(path, body string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0700))
		require.NoError(t, os.WriteFile(full, []byte(body), 0600))
	}
	forbidden := "package p\nimport \"time\"\nfunc TestBad() { time.Sleep(1) }\n"
	write("root_test.go", forbidden)
	write("pkg/visible_test.go", forbidden)
	write(".hk-transient/hidden_test.go", forbidden)

	files := moduleTestFiles(t, root)
	assert.NotContains(t, files, ".hk-transient/hidden_test.go")
	assert.Contains(t, files, "root_test.go")
	assert.Contains(t, files, "pkg/visible_test.go")
	findings, err := scanUnitRule(files)
	require.NoError(t, err)
	got := map[string]bool{}
	for _, finding := range findings {
		got[finding.File] = true
	}
	assert.Equal(t, map[string]bool{"root_test.go": true, "pkg/visible_test.go": true}, got)
}

// TestUnitTestsUseNoRealTimeSocketsOrServices is the rule's lock: every unit-tier
// test file of the module, read, with no forbidden use outside unit_rule_allowed.txt,
// and no file left on that list that no longer offends.
func TestUnitTestsUseNoRealTimeSocketsOrServices(t *testing.T) {
	files := moduleTestFiles(t, "../..")
	require.NotEmpty(t, files, "the scan read no test files")
	findings, err := scanUnitRule(files)
	require.NoError(t, err)

	allowedCounts := unitRuleAllowedCounts(t)
	listed := map[string]bool{}
	for f := range allowedCounts {
		require.False(t, listed[f], "%s is listed twice in %s", f, unitRuleAllowedFile)
		require.NotNil(t, files[f], "%s names %s, which is no test file of the module: remove the line", unitRuleAllowedFile, f)
		listed[f] = true
	}
	offending := map[string]bool{}
	var unlisted []string
	for _, f := range findings {
		offending[f.File] = true
		if !listed[f.File] {
			unlisted = append(unlisted, f.String())
		}
	}
	var stale []string
	for f := range allowedCounts {
		if !offending[f] {
			stale = append(stale, f)
		}
	}
	sort.Strings(stale)
	over := unitRuleOverAllowance(findings, allowedCounts)
	under := unitRuleUnderAllowance(findings, allowedCounts)
	if len(unlisted) > 0 {
		t.Errorf("%d uses: a unit test (no functional build tag) uses real time, a real socket, an external service or a program; "+
			"move it behind //go:build functional or give it a fake (a fake clock, net.Pipe, httptest.NewRecorder, a synctest bubble); "+
			"a new file that runs a program is a functional test:\n%s",
			len(unlisted), strings.Join(unlisted, "\n"))
	}
	if len(over) > 0 {
		t.Errorf("%d uses: a listed file's use is above its count; lower the count in %s or move the file behind //go:build functional (it only shrinks):\n%s",
			len(over), unitRuleAllowedFile, strings.Join(over, "\n"))
	}
	if len(under) > 0 {
		t.Errorf("%d uses: a listed file's use is below its count; lower the count in %s to the scan (it only shrinks):\n%s",
			len(under), unitRuleAllowedFile, strings.Join(under, "\n"))
	}
	if len(stale) > 0 {
		t.Errorf("these files keep the rule now: remove them from %s (it only shrinks):\n%s", unitRuleAllowedFile, strings.Join(stale, "\n"))
	}
	t.Logf("unit-rule: %d files still allowed in %s", len(allowedCounts), unitRuleAllowedFile)
}

// TestTheUnitRuleAllowanceOnlyShrinks: against the base a change is merged into
// (UNIT_RULE_BASE, a git revision: CI names the pull request's base, as it does for
// PAGELOCK_BASE; unset, the local origin/sprint/comfort, origin/dev or origin/main if
// git has one here, else not checked), every path in unit_rule_allowed.txt is in the
// base's copy or enters with program uses alone during the one-time migration from a
// base with no program counts. After that baseline exists, a new allowance or count is
// red; a removed one or lower count is the move it records. A base with no allowlist
// yet passes (the allowance's first change). No fetch runs here.
func TestTheUnitRuleAllowanceOnlyShrinks(t *testing.T) {
	t.Parallel()
	base := os.Getenv("UNIT_RULE_BASE")
	if base == "" {
		for _, ref := range []string{"refs/remotes/origin/sprint/comfort", "refs/remotes/origin/dev", "refs/remotes/origin/main"} {
			if exec.Command("git", "rev-parse", "--verify", "--quiet", ref+"^{commit}").Run() == nil {
				base = ref
				break
			}
		}
	}
	if base == "" {
		t.Skip("UNIT_RULE_BASE names no base revision and git has no origin/sprint/comfort, origin/dev or origin/main here; CI sets it to the pull request's base")
	}
	// the base must be a commit git can read: a misspelled rev, no git or a refused
	// repository is red, never a pass
	if out, err := exec.Command("git", "rev-parse", "--verify", "--quiet", base+"^{commit}").CombinedOutput(); err != nil {
		t.Fatalf("UNIT_RULE_BASE %s is no commit git can read here (%v: %s): the allowance's shrinking is unchecked", base, err, strings.TrimSpace(string(out)))
	}
	const path = "internal/ci/" + unitRuleAllowedFile
	out, err := exec.Command("git", "show", base+":"+path).Output()
	if err != nil {
		t.Logf("%s holds no %s: this change is the allowance's first", base, unitRuleAllowedFile)
		return
	}
	was := parseAllowedCounts(string(out))
	scanSource, err := exec.Command("git", "show", base+":internal/ci/unit_rule_test.go").Output()
	require.NoError(t, err, "git show %s:internal/ci/unit_rule_test.go", base)
	// The declaration is the migration marker: a ledger with no program counts is a
	// bootstrap base only when its scanner predates this fourth kind.
	baseHasProgramKind := strings.Contains(string(scanSource), "unitRuleProgramHelpers")
	now := unitRuleAllowedCounts(t)
	added, over := unitRuleAllowanceDelta(was, now, baseHasProgramKind)
	if len(added) > 0 {
		t.Errorf("%s allows %d files that %s does not: the list only shrinks; move the file behind //go:build functional or give it a fake instead:\n%s",
			unitRuleAllowedFile, len(added), base, strings.Join(added, "\n"))
	}
	if len(over) > 0 {
		t.Errorf("%s raises a count above %s: the list only shrinks by use too:\n%s",
			unitRuleAllowedFile, base, strings.Join(over, "\n"))
	}
}

// TestTheUnitRuleScanSeesAKnownYesAndAKnownNo proves the scan on cases whose answer
// is known, both ways, before it gates.
func TestTheUnitRuleScanSeesAKnownYesAndAKnownNo(t *testing.T) {
	scan := func(t *testing.T, src string) []string {
		t.Helper()
		got, err := scanUnitRule(map[string][]byte{"p/x_test.go": []byte(src)})
		require.NoError(t, err)
		var out []string
		for _, f := range got {
			out = append(out, fmt.Sprintf("%d %s %s", f.Line, f.Kind, f.What))
		}
		return out
	}
	yes := map[string]struct {
		src  string
		want []string
	}{
		"a sleep": {"package p\nimport \"time\"\nfunc TestX() { time.Sleep(1) }\n",
			[]string{"3 time time.Sleep"}},
		"a sleep under an alias": {"package p\nimport tm \"time\"\nfunc TestX() { tm.Sleep(1) }\n",
			[]string{"3 time time.Sleep"}},
		"a sleep as a value": {"package p\nimport \"time\"\nvar s = struct{ f func(time.Duration) }{f: time.Sleep}\n",
			[]string{"3 time time.Sleep"}},
		"a timer in a select": {"package p\nimport \"time\"\nfunc TestX(c chan int) {\n\tselect {\n\tcase <-c:\n\tcase <-time.After(1):\n\t}\n}\n",
			[]string{"6 time time.After"}},
		"the wall clock": {"package p\nimport \"time\"\nfunc TestX() { _ = time.Now() }\n",
			[]string{"3 time time.Now"}},
		"a wall-clock deadline": {"package p\nimport \"context\"\nfunc TestX() { context.WithTimeout(context.Background(), 1) }\n",
			[]string{"3 time context.WithTimeout"}},
		"require.Eventually": {"package p\nimport \"github.com/stretchr/testify/require\"\nfunc TestX(t require.TestingT) { require.Eventually(t, nil, 1, 1) }\n",
			[]string{"3 time .Eventually"}},
		"a conn deadline": {"package p\nimport \"net\"\nfunc TestX(c net.Conn) { c.SetDeadline(x) }\n",
			[]string{"3 time .SetDeadline"}},
		"a listener": {"package p\nimport \"net\"\nfunc TestX() { net.Listen(\"tcp\", \"127.0.0.1:0\") }\n",
			[]string{"3 socket net.Listen"}},
		"a unix socket dial": {"package p\nimport \"net\"\nfunc TestX() { net.Dial(\"unix\", \"/x.sock\") }\n",
			[]string{"3 socket net.Dial"}},
		"an httptest server": {"package p\nimport \"net/http/httptest\"\nfunc TestX() { httptest.NewServer(nil) }\n",
			[]string{"3 socket net/http/httptest.NewServer"}},
		"an HTTP call": {"package p\nimport \"net/http\"\nfunc TestX() { http.Get(\"https://example.com\") }\n",
			[]string{"3 service net/http.Get"}},
		"a redis-server": {"package p\nimport \"github.com/mas-bandwidth/nova-tools/pkg/testredis\"\nfunc TestX(t testing.TB) { testredis.Start(t) }\n",
			[]string{"3 service github.com/mas-bandwidth/nova-tools/pkg/testredis.Start"}},
		"a networked exec program": {"package p\nimport \"os/exec\"\nfunc TestX() { exec.Command(\"curl\", \"http://x\") }\n",
			[]string{"3 program os/exec.Command curl", "3 socket os/exec.Command curl"}},
		"an exec opening /dev/tcp": {"package p\nimport \"os/exec\"\nfunc TestX() { exec.Command(\"bash\", \"-c\", \"exec 3<>/dev/tcp/127.0.0.1/1\") }\n",
			[]string{"3 program os/exec.Command bash", "3 socket os/exec.Command bash /dev/tcp"}},
		"a CommandContext networked exec": {"package p\nimport \"os/exec\"\nfunc TestX() { exec.CommandContext(context.Background(), \"nc\", \"-l\", \"1\") }\n",
			[]string{"3 program os/exec.CommandContext nc", "3 socket os/exec.CommandContext nc"}},
		"the hook socket helper": {"package p\nfunc TestX(t T) { a.hookSock = shortSock(t) }\n",
			[]string{"2 socket shortSock"}},
		"a !functional file is the unit tier": {"//go:build !functional\n\npackage p\nimport \"time\"\nfunc TestX() { time.Sleep(1) }\n",
			[]string{"5 time time.Sleep"}},
		"a linux branch of a functional-or-linux file is still unit-tier": {"//go:build functional || linux\n\npackage p\nimport \"time\"\nfunc TestX() { time.Sleep(1) }\n",
			[]string{"5 time time.Sleep"}},
		"a slow file is the unit tier": {"//go:build slow\n\npackage p\nimport \"time\"\nfunc TestX() { time.Sleep(1) }\n",
			[]string{"5 time time.Sleep"}},
		"a perf file is the unit tier": {"//go:build perf\n\npackage p\nimport \"time\"\nfunc TestX() { _ = time.Since(t0) }\n",
			[]string{"5 time time.Since"}},
		"a socket inside a bubble": {"package p\nimport (\n\t\"net\"\n\t\"testing/synctest\"\n)\nfunc TestX(t T) { synctest.Test(t, func(t T) { net.Listen(\"tcp\", \"\") }) }\n",
			[]string{"6 socket net.Listen"}},
		"a helper used outside a bubble too": {"package p\nimport (\n\t\"time\"\n\t\"testing/synctest\"\n)\n" +
			"func wait() { time.Sleep(1) }\n" +
			"func TestX(t T) { synctest.Test(t, func(t T) { wait() }) }\n" +
			"func TestY(t T) { wait() }\n",
			[]string{"6 time time.Sleep"}},
		"the wall clock as a bubble's argument": {"package p\nimport (\n\t\"time\"\n\t\"testing/synctest\"\n)\n" +
			"func TestX(t T) { synctest.Test(t, mk(time.Now())) }\n",
			[]string{"6 time time.Now"}},
		"a dot-imported sleep": {"package p\nimport . \"time\"\nfunc TestX() { Sleep(1) }\n",
			[]string{"3 time time.Sleep"}},
		"a delayproxy Serve under a dot import": {"package p\nimport . \"github.com/mas-bandwidth/nova-tools/pkg/delayproxy\"\nfunc TestX() { Serve(nil, \"127.0.0.1:0\", 0, Options{}) }\n",
			[]string{"2 socket github.com/mas-bandwidth/nova-tools/pkg/delayproxy (dot import)"}},
	}
	for name, c := range yes {
		t.Run("yes/"+name, func(t *testing.T) {
			assert.Equal(t, c.want, scan(t, c.src))
		})
	}
	no := map[string]string{
		"a functional file":          "//go:build functional\n\npackage p\nimport \"time\"\nfunc TestX() { time.Sleep(1) }\n",
		"a functional and slow file": "//go:build functional && slow\n\npackage p\nimport \"time\"\nfunc TestX() { time.Sleep(1) }\n",
		"a fixed time":               "package p\nimport \"time\"\nvar t0 = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)\nfunc TestX() { _ = t0.Add(time.Second) }\n",
		"a sleep in a bubble":        "package p\nimport (\n\t\"time\"\n\t\"testing/synctest\"\n)\nfunc TestX(t T) { synctest.Test(t, func(t T) { time.Sleep(1); _ = time.Now() }) }\n",
		"a helper only a bubble calls": "package p\nimport (\n\t\"time\"\n\t\"testing/synctest\"\n)\n" +
			"func wait() { time.Sleep(1) }\n" +
			"func deeper() { wait() }\n" +
			"func TestX(t T) { synctest.Test(t, func(t T) { deeper() }) }\n",
		"a named callback": "package p\nimport (\n\t\"time\"\n\t\"testing/synctest\"\n)\n" +
			"func run(t T) { time.Sleep(1) }\n" +
			"func TestX(t T) { synctest.Test(t, run) }\n",
		"an in-memory pipe":           "package p\nimport \"net\"\nfunc TestX() { a, b := net.Pipe(); _, _ = a, b }\n",
		"a recorder":                  "package p\nimport \"net/http/httptest\"\nfunc TestX() { _ = httptest.NewRecorder(); _ = httptest.NewRequest(\"GET\", \"/\", nil) }\n",
		"a local name like the clock": "package p\nfunc TestX(c clock) { _ = c.Now(); c.Sleep(1) }\n",
		"a test named Eventually":     "package p\nfunc TestStalledCallerIsEventuallyGranted(t T) {}\n",
		"a socket's path":             "package p\nimport \"github.com/mas-bandwidth/nova-tools/pkg/testredis\"\nfunc TestX() { testredis.SocketPath(\"d\", \"n.sock\") }\n",
	}
	for name, src := range no {
		t.Run("no/"+name, func(t *testing.T) {
			assert.Empty(t, scan(t, src))
		})
	}
}
