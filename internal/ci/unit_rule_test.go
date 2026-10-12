package ci

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
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
// net.Dial, an httptest server, a unix socket, a connection deadline) and never an
// external service (a redis-server, an HTTP call). Those belong behind
// `//go:build functional`, or behind a fake: a fake clock, an in-memory transport
// (net.Pipe, httptest.NewRecorder, a handler RoundTripper), or a testing/synctest
// bubble, whose clock is fake.
//
// TestUnitTestsUseNoRealTimeSocketsOrServices reads every test file of the module
// and fails, naming file, line and call, on any unit-tier use of the forbidden
// calls. There is no allowlist that hides an offender: unitRuleBurnDown below names
// the files still being moved, and fails when one of them stops offending, so it
// can only shrink.

// unitRuleBurnDown is the files that still break the rule, each to be moved behind
// the functional tag or onto a fake. A listed file that no longer offends fails
// the test until its line is removed, and an offender not listed fails it too: the
// list only shrinks, to zero.
//
// TODO (2026-10-12, the v1.2.16 unit-test rule, PR 2 of 2): the 20 files below are
// the inventory left after the first moves (55 uses: 50 of real time, four of them
// require.Eventually; 4 sockets; 1 service). Each is a move to the functional tier
// or onto a fake, with its assertions kept; a file leaves this list the same turn it
// is fixed.
var unitRuleBurnDown = []string{
	"cmd/nova-sprint/fleet_back_test.go",             // require.Eventually on the adopter's goroutine
	"cmd/nova-sprint/friend_start_test.go",           // a job file aged against the wall clock
	"cmd/nova-sprint/friendcards_cover_test.go",      // a spent wall-clock deadline
	"cmd/nova-sprint/friendcards_verb_test.go",       // SyncInbox given the wall clock (as the file times are)
	"cmd/nova-sprint/friendclean_test.go",            // cache entries aged against the wall clock
	"cmd/nova-sprint/land_beat_test.go",              // a two-minute wall bound on the beat loop
	"cmd/nova-sprint/land_class_test.go",             // time.After bounds on the side-by-side gate
	"cmd/nova-sprint/landpass_lanes_test.go",         // time.After bounds on the lanes' locks
	"cmd/nova-sprint/landring_test.go",               // a two-minute wall bound on the beat loop
	"cmd/nova-sprint/landstage_test.go",              // a two-minute wall bound on the beat loop
	"cmd/nova-sprint/tick_deadline_test.go",          // time.After bounds on the watchdog
	"cmd/nova-sprint/tick_gate_test.go",              // the deal's wall measured under load
	"cmd/nova-sprint/where_cost_perf_test.go",        // -tags perf: a wall-clock bound by design, not yet behind functional
	"internal/sprint/deal_load_bench_test.go",        // the deal's wall measured and logged
	"internal/sprint/store/machine_twoloops_test.go", // the store's real fence backoff
	"internal/sprint/store/waitlog_test.go",          // a 30 s wall bound on a wait never woken
	"internal/sprintdash/events_test.go",             // httptest.NewServer, DefaultClient, Eventually
	"internal/sprintdash/fresh_test.go",              // httptest.NewServer
	"internal/sprintdash/pageview_test.go",           // httptest.NewServer
	"internal/sprintdash/serverpy_test.go",           // require.Eventually
}

// unitRuleFinding is one forbidden use: the file (slash path from the module
// root), its line, the kind (time, socket or service) and what was used.
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
	"github.com/mas-bandwidth/nova-sprint/pkg/testredis": {
		"Start": "service", "StartServer": "service", "Far": "service", "FarLink": "service",
	},
}

// unitRuleImports is the packages whose every use is a forbidden one: delayproxy is
// a TCP proxy.
var unitRuleImports = map[string]string{
	"github.com/mas-bandwidth/nova-sprint/pkg/delayproxy": "socket",
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
		for _, is := range uf.f.Imports {
			p, _ := strconv.Unquote(is.Path.Value)
			name := filepath.Base(p)
			if strings.HasPrefix(name, "go-redis") || p == "github.com/redis/go-redis/v9" {
				name = "redis"
			}
			if is.Name != nil {
				name = is.Name.Name
			}
			imports[name] = p
		}
		find := func(n ast.Node, kind, what string) unitRuleFinding {
			return unitRuleFinding{File: uf.rel, Line: fset.Position(n.Pos()).Line, Kind: kind, What: what}
		}
		var walk func(n ast.Node, bubbled bool, decl string)
		walk = func(n ast.Node, bubbled bool, decl string) {
			ast.Inspect(n, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.CallExpr:
					if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
						if id, ok := sel.X.(*ast.Ident); ok && imports[id.Name] == "testing/synctest" && (sel.Sel.Name == "Test" || sel.Sel.Name == "Run") {
							walk(v.Fun, bubbled, decl)
							for _, a := range v.Args {
								walk(a, true, decl)
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

// moduleTestFiles is every _test.go under root (tmp and testdata trees skipped),
// keyed by its slash path from root.
func moduleTestFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
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

// TestUnitTestsUseNoRealTimeSocketsOrServices is the rule's lock: every unit-tier
// test file of the module, read, with no forbidden use outside the burn-down list,
// and no file left on the list that no longer offends.
func TestUnitTestsUseNoRealTimeSocketsOrServices(t *testing.T) {
	files := moduleTestFiles(t, "../..")
	require.NotEmpty(t, files, "the scan read no test files")
	findings, err := scanUnitRule(files)
	require.NoError(t, err)

	listed := map[string]bool{}
	for _, f := range unitRuleBurnDown {
		require.False(t, listed[f], "%s is listed twice", f)
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
	for _, f := range unitRuleBurnDown {
		if !offending[f] {
			stale = append(stale, f)
		}
	}
	if len(unlisted) > 0 {
		t.Errorf("%d uses: a unit test (no functional build tag) uses real time, a real socket or an external service; "+
			"move it behind //go:build functional or give it a fake (a fake clock, net.Pipe, httptest.NewRecorder, a synctest bubble):\n%s",
			len(unlisted), strings.Join(unlisted, "\n"))
	}
	if len(stale) > 0 {
		t.Errorf("these files keep the rule now: remove them from unitRuleBurnDown (it only shrinks):\n%s", strings.Join(stale, "\n"))
	}
	t.Logf("unit-rule burn-down: %d files still listed", len(unitRuleBurnDown))
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
		"a redis-server": {"package p\nimport \"github.com/mas-bandwidth/nova-sprint/pkg/testredis\"\nfunc TestX(t testing.TB) { testredis.Start(t) }\n",
			[]string{"3 service github.com/mas-bandwidth/nova-sprint/pkg/testredis.Start"}},
		"the hook socket helper": {"package p\nfunc TestX(t T) { a.hookSock = shortSock(t) }\n",
			[]string{"2 socket shortSock"}},
		"a !functional file is the unit tier": {"//go:build !functional\n\npackage p\nimport \"time\"\nfunc TestX() { time.Sleep(1) }\n",
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
		"an in-memory pipe":           "package p\nimport \"net\"\nfunc TestX() { a, b := net.Pipe(); _, _ = a, b }\n",
		"a recorder":                  "package p\nimport \"net/http/httptest\"\nfunc TestX() { _ = httptest.NewRecorder(); _ = httptest.NewRequest(\"GET\", \"/\", nil) }\n",
		"a local name like the clock": "package p\nfunc TestX(c clock) { _ = c.Now(); c.Sleep(1) }\n",
		"a test named Eventually":     "package p\nfunc TestStalledCallerIsEventuallyGranted(t T) {}\n",
		"a socket's path":             "package p\nimport \"github.com/mas-bandwidth/nova-sprint/pkg/testredis\"\nfunc TestX() { testredis.SocketPath(\"d\", \"n.sock\") }\n",
	}
	for name, src := range no {
		t.Run("no/"+name, func(t *testing.T) {
			assert.Empty(t, scan(t, src))
		})
	}
}
