package sprint

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

// The machine gate checks: pins and reach.

const (
	GateLintPinAbsent = "gate-pin-absent"
	GateLintPinBroken = "gate-pin-broken"
	GateLintReach     = "gate-reach-unreached"
)

// GateLintFinding is a finding from the extended machine gate checks.
type GateLintFinding struct {
	What string
}

// GateLintInput is the input for the extended gate checks.
type GateLintInput struct {
	MergeBase    string
	Head         string
	RevertedHead string
	TestPkg      string
	TestName     string
	ChangedFiles []string // non-test files changed by the commit
	ChangedDir   string   // directory of the changed files
	BaseDir      string   // merge-base tree, used to identify declarations the commit adds
	SkipReach    bool     // deletion cards remove code and have no new API to reach
}

// BenchRunner runs a test at a given commit and returns whether it passed.
type BenchRunner func(commit, pkg, testname string) (passed bool, err error)

// GateLintFindingsChecked returns an error when a probe or source analysis did
// not answer. Such an error is not evidence that the test is red or code is used.
func GateLintFindingsChecked(input GateLintInput, run BenchRunner) ([]GateLintFinding, error) {
	var out []GateLintFinding

	if input.TestPkg != "" && input.TestName != "" && run != nil {
		// Check 1: the test should fail at merge-base (or not exist).
		passedAtMergeBase, err := run(input.MergeBase, input.TestPkg, input.TestName)
		if err != nil {
			return nil, err
		}
		if passedAtMergeBase {
			out = append(out, GateLintFinding{What: GateLintPinAbsent + ": test passes at merge-base"})
			return out, nil
		}
		if input.RevertedHead == "" {
			return append(out, GateLintFinding{What: GateLintPinBroken + ": no reverted non-test tree"}), nil
		}
		// Check 2: the test should fail when non-test hunks are reverted.
		passedAtReverted, err := run(input.RevertedHead, input.TestPkg, input.TestName)
		if err != nil {
			return nil, err
		}
		if passedAtReverted {
			out = append(out, GateLintFinding{What: GateLintPinBroken + ": test passes with non-test hunks reverted"})
		}
	}

	// Check 3: Reach check - every exported symbol in changed non-test files
	// should have a reference from a non-test file
	if !input.SkipReach && len(input.ChangedFiles) > 0 && input.ChangedDir != "" {
		symbols, err := addedDeclarations(input.BaseDir, input.ChangedDir, input.ChangedFiles)
		if err != nil {
			return nil, err
		}
		verbs, err := addedVerbRows(input.BaseDir, input.ChangedDir, input.ChangedFiles)
		if err != nil {
			return nil, err
		}
		for verb, runner := range verbs {
			if runner == "" || !hasMethod(input.ChangedDir, runner) {
				out = append(out, GateLintFinding{What: GateLintReach + ": verb " + verb + " has no declared run function"})
			}
		}
		if len(symbols) > 0 {
			reached, err := findTreeReferences(input.ChangedDir, symbols)
			if err != nil {
				return append(out, GateLintFinding{What: GateLintReach + ": reference analysis could not complete: " + err.Error()}), nil
			}
			for id, ok := range reached {
				if !ok {
					out = append(out, GateLintFinding{What: GateLintReach + ": " + symbols[id] + " has no reference from any non-test file"})
				}
			}
		}
	}

	return out, nil
}

// addedDeclarations compares declarations at the merge-base with the pinned head.
// The key includes package directory and receiver so same-spelled declarations in
// different packages cannot suppress or satisfy one another.
func addedDeclarations(baseDir, headDir string, changed []string) (map[string]string, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("merge-base source tree is required for reach analysis")
	}
	base, err := declarationNames(baseDir, changed)
	if err != nil {
		return nil, err
	}
	head, err := declarationNames(headDir, changed)
	if err != nil {
		return nil, err
	}
	for id := range base {
		delete(head, id)
	}
	return head, nil
}

func declarationNames(root string, paths []string) (map[string]string, error) {
	out := map[string]string{}
	for _, rel := range paths {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		path := filepath.Join(root, rel)
		src, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", rel, err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				continue
			}
			recv := ""
			if fn.Recv != nil && len(fn.Recv.List) > 0 {
				recv = receiverName(fn.Recv.List[0].Type)
			}
			pkg, _ := filepath.Rel(root, filepath.Dir(path))
			out[reachID(filepath.ToSlash(pkg), recv, fn.Name.Name)] = fn.Name.Name
		}
	}
	return out, nil
}

func receiverName(expr ast.Expr) string {
	if p, ok := expr.(*ast.StarExpr); ok {
		expr = p.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
func reachID(pkg, recv, name string) string { return pkg + "#" + recv + "#" + name }

func addedVerbRows(baseDir, headDir string, paths []string) (map[string]string, error) {
	base, err := verbRows(baseDir, paths)
	if err != nil {
		return nil, err
	}
	head, err := verbRows(headDir, paths)
	if err != nil {
		return nil, err
	}
	for name, runner := range base {
		if head[name] == runner {
			delete(head, name)
		}
	}
	return head, nil
}
func verbRows(root string, paths []string) (map[string]string, error) {
	out := map[string]string{}
	for _, rel := range paths {
		if !strings.HasSuffix(rel, "cmd/nova-sprint/verbs.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		f, err := parser.ParseFile(token.NewFileSet(), rel, src, 0)
		if err != nil {
			return nil, err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, elt := range c.Elts {
				row, ok := elt.(*ast.CompositeLit)
				if !ok || len(row.Elts) < 4 {
					continue
				}
				nameLit, ok := row.Elts[0].(*ast.BasicLit)
				if !ok || nameLit.Kind != token.STRING {
					continue
				}
				name := strings.Trim(nameLit.Value, "\"")
				run := ""
				switch x := row.Elts[len(row.Elts)-1].(type) {
				case *ast.SelectorExpr:
					run = x.Sel.Name
				case *ast.Ident:
					if x.Name != "nil" {
						run = x.Name
					}
				}
				out[name] = run
			}
			return true
		})
	}
	return out, nil
}
func hasMethod(root, name string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return nil
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv != nil && fn.Name.Name == name {
				found = true
			}
		}
		return nil
	})
	return found
}

// findTreeReferences uses type-checked object identity across every Go package in
// the pinned tree. The importer loads local module packages from source and shares
// their types.Package objects with the root scan.
func findTreeReferences(root string, targets map[string]string) (map[string]bool, error) {
	reached := map[string]bool{}
	for id := range targets {
		reached[id] = false
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	module := "_gatelint"
	for _, line := range strings.Split(string(mod), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			module = fields[1]
			break
		}
	}
	if strings.Contains(string(mod), "module ") && module == "_gatelint" {
		return nil, fmt.Errorf("go.mod has no module path")
	}
	fset := token.NewFileSet()
	var dirs []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			if path != root && (d.Name() == ".git" || d.Name() == "vendor" || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			match, e := build.Default.MatchFile(filepath.Dir(path), filepath.Base(path))
			if e != nil {
				return e
			}
			if match {
				dirs = append(dirs, filepath.Dir(path))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	imports, err := importsInDirs(dirs, module)
	if err != nil {
		return nil, err
	}
	std, err := newGoListImporter(root, fset, imports)
	if err != nil {
		return nil, err
	}
	uniq := map[string]bool{}
	imp := &treeImporter{root: root, module: module, fset: fset, cache: map[string]*types.Package{}, loading: map[string]bool{}, std: std}
	for _, dir := range dirs {
		if uniq[dir] {
			continue
		}
		uniq[dir] = true
		if _, e := imp.loadDir(dir); e != nil {
			return nil, e
		}
	}
	// Re-typecheck each package with Uses populated; cached imports preserve identity.
	for _, dir := range dirs {
		if uniq[dir+"#checked"] {
			continue
		}
		uniq[dir+"#checked"] = true
		_, info, e := imp.checkDir(dir)
		if e != nil {
			return nil, e
		}
		for _, obj := range info.Uses {
			if fn, ok := obj.(*types.Func); ok && fn.Pkg() != nil {
				id := objectReachID(root, fn)
				if _, found := targets[id]; found {
					reached[id] = true
				}
			}
		}
	}
	return reached, nil
}

func objectReachID(root string, fn *types.Func) string {
	rel, err := filepath.Rel(root, filepath.FromSlash(fn.Pkg().Path()))
	if err != nil {
		return ""
	}
	recv := ""
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		t := sig.Recv().Type()
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		if n, ok := t.(*types.Named); ok {
			recv = n.Obj().Name()
		}
	}
	return reachID(filepath.ToSlash(rel), recv, fn.Name())
}

type treeImporter struct {
	root, module string
	fset         *token.FileSet
	cache        map[string]*types.Package
	loading      map[string]bool
	std          types.Importer
}

func (i *treeImporter) Import(path string) (*types.Package, error) {
	if p := i.cache[path]; p != nil {
		return p, nil
	}
	if path == i.module || strings.HasPrefix(path, i.module+"/") {
		rel := strings.TrimPrefix(strings.TrimPrefix(path, i.module), "/")
		dir := filepath.Join(i.root, filepath.FromSlash(rel))
		return i.loadDir(dir)
	}
	return i.std.Import(path)
}
func importsInDirs(dirs []string, module string) ([]string, error) {
	found := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			match, err := build.Default.MatchFile(dir, name)
			if err != nil {
				return nil, err
			}
			if !match {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.ImportsOnly)
			if err != nil {
				return nil, err
			}
			for _, spec := range file.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return nil, err
				}
				if path == "C" || path == module || strings.HasPrefix(path, module+"/") {
					continue
				}
				found[path] = true
			}
		}
	}
	paths := make([]string, 0, len(found))
	for path := range found {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

// newGoListImporter uses the bench's resolved Go toolchain to locate export data
// in one go-list pass. A single gc importer then preserves standard and external
// types.Package identity across every package in the tree; it does not depend on
// runtime.GOROOT or build.Default.GOROOT being populated in a trimmed wall binary.
func newGoListImporter(dir string, fset *token.FileSet, imports []string) (types.Importer, error) {
	goBin := swarm.BenchGoBin(runtime.GOOS, os.Getenv("HOME"), os.Getenv("PATH"))
	if goBin == "" {
		return nil, fmt.Errorf("the bench Go toolchain is not on its declared roots or PATH")
	}
	exports := map[string]string{}
	if len(imports) > 0 {
		args := append([]string{"list", "-deps", "-export", "-json"}, imports...)
		cmd := exec.Command(filepath.Join(goBin, "go"), args...)
		cmd.Dir = dir
		env := make([]string, 0, len(os.Environ())+2)
		for _, item := range os.Environ() {
			if !strings.HasPrefix(item, "GOROOT=") && !strings.HasPrefix(item, "GOTOOLCHAIN=") {
				env = append(env, item)
			}
		}
		env = append(env, "GOROOT="+filepath.Dir(goBin), "GOTOOLCHAIN=local")
		cmd.Env = env
		out, err := cmd.Output()
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok && len(exit.Stderr) > 0 {
				return nil, fmt.Errorf("go list could not locate import export data: %w: %s", err, strings.TrimSpace(string(exit.Stderr)))
			}
			return nil, fmt.Errorf("go list could not locate import export data: %w", err)
		}
		dec := json.NewDecoder(strings.NewReader(string(out)))
		for {
			var pkg struct {
				ImportPath string
				Export     string
			}
			if err := dec.Decode(&pkg); err == io.EOF {
				break
			} else if err != nil {
				return nil, fmt.Errorf("decode go list export data: %w", err)
			}
			if pkg.ImportPath != "" && pkg.Export != "" {
				exports[pkg.ImportPath] = pkg.Export
			}
		}
	}
	return importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		archive := exports[path]
		if archive == "" {
			return nil, fmt.Errorf("go list returned no export data for %q", path)
		}
		return os.Open(archive)
	}), nil
}

func (i *treeImporter) loadDir(dir string) (*types.Package, error) {
	imp := filepath.ToSlash(strings.TrimPrefix(dir, i.root))
	imp = strings.TrimPrefix(imp, "/")
	path := i.module
	if imp != "" {
		path += "/" + imp
	}
	if p := i.cache[path]; p != nil {
		return p, nil
	}
	if i.loading[path] {
		return nil, fmt.Errorf("import cycle while loading %s", path)
	}
	i.loading[path] = true
	defer delete(i.loading, path)
	p, _, err := i.checkDir(dir)
	if err == nil {
		i.cache[path] = p
	}
	return p, err
}
func (i *treeImporter) checkDir(dir string) (*types.Package, *types.Info, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	ctxt := build.Default
	var files []*ast.File
	names := map[string]bool{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		match, er := ctxt.MatchFile(dir, n)
		if er != nil {
			return nil, nil, er
		}
		if !match {
			continue
		}
		names[n] = true
		f, er := parser.ParseFile(i.fset, filepath.Join(dir, n), nil, parser.ParseComments)
		if er != nil {
			return nil, nil, er
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no buildable Go files in %s", dir)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: i, Error: func(error) {}}
	p, err := conf.Check(filepath.ToSlash(dir), i.fset, files, info)
	if err != nil {
		return nil, nil, fmt.Errorf("type-check %s: %w", dir, err)
	}
	return p, info, nil
}

// getChangedExportedSymbols returns exported declarations from changed files.
func getChangedExportedSymbols(dir string, changedFiles []string) map[string]bool {
	symbols := make(map[string]bool)
	fset := token.NewFileSet()

	for _, f := range changedFiles {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		isVerbTable := strings.HasSuffix(f, "cmd/nova-sprint/verbs.go")
		f = filepath.Join(dir, f)
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			continue
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.IsExported() || isVerbTable {
					symbols[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						if ts.Name.IsExported() {
							symbols[ts.Name.Name] = true
						}
					}
				}
			}
		}
	}
	return symbols
}

// findNonTestReferences finds references to symbols from non-test files in the package.
func findNonTestReferences(pkgDir string, symbols map[string]bool) (map[string]bool, error) {
	reached := make(map[string]bool)
	for sym := range symbols {
		reached[sym] = false
	}

	fset := token.NewFileSet()
	var files []*ast.File

	// Only this package counts. A same-named local, field, or declaration in a
	// different package must not make an exported addition look wired.
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return reached, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(pkgDir, entry.Name())
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			continue
		}
		files = append(files, file)
	}
	return referencesByObject(fset, files, symbols, reached)
}

func referencesByObject(fset *token.FileSet, files []*ast.File, symbols map[string]bool, reached map[string]bool) (map[string]bool, error) {
	var dir string
	imports := map[string]bool{}
	for _, file := range files {
		if dir == "" {
			dir = filepath.Dir(fset.Position(file.Pos()).Filename)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return nil, err
			}
			if path != "C" && path != "unsafe" {
				imports[path] = true
			}
		}
	}
	if dir == "" {
		dir = "."
	}
	paths := make([]string, 0, len(imports))
	for path := range imports {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	imp, err := newGoListImporter(dir, fset, paths)
	if err != nil {
		return nil, err
	}
	info := &types.Info{Defs: make(map[*ast.Ident]types.Object), Uses: make(map[*ast.Ident]types.Object)}
	conf := types.Config{Importer: imp, Error: func(error) {}}
	if _, err := conf.Check("gatelint", fset, files, info); err != nil {
		return nil, fmt.Errorf("type-check references: %w", err)
	}
	targets := make(map[types.Object]string)
	for _, file := range files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if symbols[d.Name.Name] {
					if obj := info.Defs[d.Name]; obj != nil {
						targets[obj] = d.Name.Name
					}
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok && symbols[ts.Name.Name] {
						if obj := info.Defs[ts.Name]; obj != nil {
							targets[obj] = ts.Name.Name
						}
					}
				}
			}
		}
	}
	for ident, obj := range info.Uses {
		if name, ok := targets[obj]; ok && ident.Name == name {
			reached[name] = true
		}
	}
	return reached, nil
}

// parseTestLine extracts the test line from a brief. It is unexported: the production
// path parses the TEST line with cardhdr (TestLineOfBrief), so no non-test file calls it,
// and the reach check would name an exported one as dead.
func parseTestLine(brief string) (pkg, name string) {
	lines := strings.Split(brief, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "TEST:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "TEST:"))
			if val == "none" || val == "" {
				return "", ""
			}
			parts := strings.Fields(val)
			if len(parts) == 0 {
				return "", ""
			}
			pkg = parts[0]
			if len(parts) > 1 {
				name = parts[1]
			}
			return pkg, name
		}
	}
	return "", ""
}

// GateLintFindingString returns a string representation of a gate lint finding.
func GateLintFindingString(f GateLintFinding) string {
	return "machine gate: " + f.What
}

// getExportedDecls returns all exported declarations from a file. It is unexported for
// the same reason as parseTestLine: only tests read it, so an exported one would read as
// a change with no non-test caller.
func getExportedDecls(src []byte, fset *token.FileSet) map[string]token.Pos {
	out := map[string]token.Pos{}
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return out
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name.IsExported() {
				out[d.Name.Name] = d.Pos()
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					if ts.Name.IsExported() {
						out[ts.Name.Name] = ts.Pos()
					}
				}
			}
		}
	}
	return out
}

// findNonTestReferencesInFiles finds references to symbols from the given non-test files.
// It is unexported: the production reach check scans a package directory itself
// (findNonTestReferences), so only tests need the explicit-files form.
func findNonTestReferencesInFiles(pkgDir string, changedFiles []string, symbols map[string]bool) (map[string]bool, error) {
	reached := make(map[string]bool)
	for sym := range symbols {
		reached[sym] = false
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, f := range changedFiles {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		if !filepath.IsAbs(f) {
			f = filepath.Join(pkgDir, f)
		}
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		file, err := parser.ParseFile(fset, f, src, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return referencesByObject(fset, files, symbols, reached)
}
