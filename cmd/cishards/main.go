// Command cishards checks that the per-commit test jobs of ci.yml run every
// test exactly once. The unit and functional jobs are matrices of shards;
// each shard runs `go test -run RUN -skip SKIP PKGS`, and the shard whose
// pkgs is "rest" runs every package outside its own "others" list. A test
// split across shards by its subtests (a run of "^Name$/X") has exactly one
// complement: the shard that runs "^Name$" and skips "^Name$/(X1|X2|...)",
// so a subtest added or renamed always runs in the complement.
//
// For every package with tests and every top-level test, fuzz target or
// example in it, cishards counts the shards that run it: the unit job must
// run each one whose name does not contain "Functional" once, the
// functional job each one whose name does, and neither job the other kind.
//
//	go run ./cmd/cishards [repo]
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// shard is one matrix entry of a test job.
type shard struct {
	Name   string `yaml:"shard"`
	Pkgs   string `yaml:"pkgs"`
	Others string `yaml:"others"`
	Run    string `yaml:"run"`
	Skip   string `yaml:"skip"`
}

type workflow struct {
	Jobs map[string]struct {
		Strategy struct {
			Matrix struct {
				Include []shard `yaml:"include"`
			} `yaml:"matrix"`
		} `yaml:"strategy"`
	} `yaml:"jobs"`
}

var functional = regexp.MustCompile("Functional")

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	problems, err := check(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cishards:", err)
		os.Exit(2)
	}
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, "cishards:", p)
	}
	if len(problems) > 0 {
		os.Exit(1)
	}
}

func check(root string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		return nil, err
	}
	var wf workflow
	if err = yaml.Unmarshal(raw, &wf); err != nil {
		return nil, fmt.Errorf("ci.yml: %w", err)
	}
	tests, err := testsByPackage(root)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, job := range []struct {
		name string
		want func(string) bool
	}{
		{"unit", func(n string) bool { return !functional.MatchString(n) }},
		{"functional", functional.MatchString},
	} {
		problems = append(problems, checkJob(job.name, wf.Jobs[job.name].Strategy.Matrix.Include, tests, job.want)...)
	}
	return problems, nil
}

// testsByPackage maps each package directory ("./modules/card") to its
// top-level tests, fuzz targets and examples.
func testsByPackage(root string) (map[string][]string, error) {
	out := map[string][]string{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root && (d.Name() == "testdata" || d.Name() == "vendor" || strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := "./" + filepath.ToSlash(rel)
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && topLevel(fn.Name.Name) {
				out[pkg] = append(out[pkg], fn.Name.Name)
			}
		}
		return nil
	})
	return out, err
}

// topLevel is whether go test runs a function of this name.
func topLevel(name string) bool {
	if name == "TestMain" {
		return false
	}
	for _, prefix := range []string{"Test", "Fuzz", "Example"} {
		if rest, ok := strings.CutPrefix(name, prefix); ok && (rest == "" || !isLower(rest[0])) {
			return true
		}
	}
	return false
}

func isLower(c byte) bool { return c >= 'a' && c <= 'z' }

// covers is whether the package pattern (a directory, or one ending in
// "/...") names pkg.
func covers(pattern, pkg string) bool {
	if base, ok := strings.CutSuffix(pattern, "/..."); ok {
		return pkg == base || strings.HasPrefix(pkg, base+"/")
	}
	return pattern == pkg
}

func checkJob(job string, shards []shard, tests map[string][]string, want func(string) bool) []string {
	if len(shards) == 0 {
		return []string{job + ": no matrix include list"}
	}
	var problems []string
	var named []string
	var rest *shard
	for i := range shards {
		if shards[i].Pkgs == "rest" {
			if rest != nil {
				problems = append(problems, job+": two rest shards")
			}
			rest = &shards[i]
			continue
		}
		named = append(named, strings.Fields(shards[i].Pkgs)...)
	}
	if rest == nil {
		problems = append(problems, job+": no rest shard (pkgs: rest), so a new package would run in no shard")
	} else if a, b := uniq(named), uniq(strings.Fields(rest.Others)); a != b {
		problems = append(problems, fmt.Sprintf("%s: rest's others is %q; the other shards' packages are %q", job, b, a))
	}
	for _, p := range uniqList(named) {
		found := false
		for pkg := range tests {
			found = found || covers(p, pkg)
		}
		if !found {
			problems = append(problems, fmt.Sprintf("%s: %s has no tests", job, p))
		}
	}
	pkgs := make([]string, 0, len(tests))
	for pkg := range tests {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	for _, pkg := range pkgs {
		var in []shard
		for _, s := range shards {
			if s.Pkgs == "rest" {
				if !coveredBy(strings.Fields(s.Others), pkg) {
					in = append(in, s)
				}
			} else if coveredBy(strings.Fields(s.Pkgs), pkg) {
				in = append(in, s)
			}
		}
		for _, name := range tests[pkg] {
			if p := checkTest(in, name, want(name)); p != "" {
				problems = append(problems, fmt.Sprintf("%s: %s %s: %s", job, pkg, name, p))
			}
		}
	}
	return problems
}

func coveredBy(patterns []string, pkg string) bool {
	for _, p := range patterns {
		if covers(p, pkg) {
			return true
		}
	}
	return false
}

// checkTest is "" when the shards run the test the right number of times:
// once, or split by subtests with one complement; else what is wrong.
func checkTest(in []shard, name string, want bool) string {
	var owners, parts []shard
	for _, s := range in {
		run, skip := split(s.Run), split(s.Skip)
		if len(run) > 0 && !match(run[0], name) || len(skip) == 1 && match(skip[0], name) {
			continue
		}
		if len(run) >= 2 {
			parts = append(parts, s)
		} else {
			owners = append(owners, s)
		}
	}
	switch {
	case !want && len(owners)+len(parts) > 0:
		return "runs in shard " + names(append(owners, parts...)) + ", which is not this job's kind of test"
	case !want:
		return ""
	case len(owners) == 0:
		return "runs in no shard"
	case len(owners) > 1:
		return "runs in more than one shard: " + names(owners)
	}
	skip := split(owners[0].Skip)
	splitSkip := len(skip) >= 2 && match(skip[0], name)
	if len(parts) == 0 {
		if splitSkip {
			return "shard " + owners[0].Name + " skips some of its subtests and no shard runs them"
		}
		return ""
	}
	var subs []string
	for _, p := range parts {
		r := split(p.Run)
		if len(r) != 2 {
			return "shard " + p.Name + "'s run must be ^Name$/X"
		}
		subs = append(subs, r[1])
	}
	wantSkip := "(" + strings.Join(subs, "|") + ")"
	if !splitSkip || len(skip) != 2 || skip[1] != wantSkip {
		return fmt.Sprintf("shards %s run some subtests; their complement %s must skip %s/%s", names(parts), owners[0].Name, strings.Split(owners[0].Run, "/")[0], wantSkip)
	}
	return ""
}

func match(pattern, name string) bool {
	re, err := regexp.Compile(pattern)
	return err == nil && re.MatchString(name)
}

// split is go test's: a pattern's elements, one per subtest level, split at
// each slash outside brackets and parentheses.
func split(pattern string) []string {
	if pattern == "" {
		return nil
	}
	var out []string
	cs, cp, start := 0, 0, 0
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '[':
			cs++
		case ']':
			if cs > 0 {
				cs--
			}
		case '(':
			if cs == 0 {
				cp++
			}
		case ')':
			if cs == 0 && cp > 0 {
				cp--
			}
		case '\\':
			i++
		case '/':
			if cs == 0 && cp == 0 {
				out = append(out, pattern[start:i])
				start = i + 1
			}
		}
	}
	return append(out, pattern[start:])
}

func names(ss []shard) string {
	var n []string
	for _, s := range ss {
		n = append(n, s.Name)
	}
	return strings.Join(n, ", ")
}

func uniqList(xs []string) []string {
	m := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !m[x] {
			m[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func uniq(xs []string) string { return strings.Join(uniqList(xs), " ") }
