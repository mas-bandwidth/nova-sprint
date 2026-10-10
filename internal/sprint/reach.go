package sprint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/diffcheck"
)

// The reach check of the machine lint (docs/SPEC-SPRINT.md section 6, the work lint): a
// reader found 44 changes on epoch 15's log whose new code nothing called (dead code,
// unwired verbs; restart-keeps-reads-r landed ServerRestart with no caller and turned the
// base red). Every exported function or method the change adds must have at least one
// reference from a non-test file in the package it is added to, read with go/parser; a
// symbol nothing references is a finding naming it. A verb is reached when the command's
// verb table names it, a reference like any other.

// reachFindings returns one gate-reach-unreached finding per exported function or method
// the change adds with no reference from a non-test file in its own package, over the head
// tree the view v reads. A change that only rewrites bodies, edits test files, or is not
// Go adds no symbol and finds nothing.
func reachFindings(v WorkView) []LintFinding {
	files := diffcheck.Parse(v.Diff)
	dirs := map[string]bool{}
	var out []LintFinding
	for _, f := range files {
		if f.New == "" || !strings.HasSuffix(f.New, ".go") || strings.HasSuffix(f.New, "_test.go") {
			continue
		}
		dir := path.Dir(f.New)
		if dirs[dir] {
			continue
		}
		dirs[dir] = true
		symbols := map[string]bool{}
		for _, g := range files {
			if path.Dir(g.New) != dir || !strings.HasSuffix(g.New, ".go") || strings.HasSuffix(g.New, "_test.go") {
				continue
			}
			for s := range addedExportedFuncs(v, g) {
				symbols[s] = true
			}
		}
		if len(symbols) == 0 {
			continue
		}
		reached := FindNonTestReferences(v, dir, symbols)
		names := mapsKeys(symbols)
		slices.Sort(names)
		for _, name := range names {
			if !reached[name] {
				out = append(out, LintFinding{File: dir, Token: LintUnreached,
					What: "the change adds the exported symbol " + name + " with no reference from a non-test file in " + dir})
			}
		}
	}
	slices.SortFunc(out, func(a, b LintFinding) int {
		if a.File != b.File {
			return strings.Compare(a.File, b.File)
		}
		return strings.Compare(a.What, b.What)
	})
	return out
}

// funcDeclRE is a function or method declaration's start on one diff line: `func Name` or
// `func (recv) Name`, the name it declares.
var funcDeclRE = regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)`)

// addedExportedFuncs is the exported functions and methods the diff adds to file f, read
// from the head source the view carries: a declaration whose first line the diff adds and
// whose name no line the diff removes declares. The second rule keeps a body-only change
// (a one-line `func A() ...` removed and added) from reading as the symbol A added.
func addedExportedFuncs(v WorkView, f diffcheck.File) map[string]bool {
	src, ok := v.show(f.New)
	if !ok {
		return nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, f.New, src, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	addedLines, removedLines := changedLines(f)
	old := map[string]bool{}
	for l := range removedLines {
		if m := funcDeclRE.FindStringSubmatch(l); m != nil {
			old[m[1]] = true
		}
	}
	out := map[string]bool{}
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || !ast.IsExported(fd.Name.Name) || old[fd.Name.Name] {
			continue
		}
		if _, isNew := addedLines[fset.Position(fd.Pos()).Line]; isNew {
			out[fd.Name.Name] = true
		}
	}
	return out
}

// changedLines is a file's hunks as the new-side line numbers and texts the diff adds, and
// the texts it removes (a set: the text alone is enough to read a declaration's name).
func changedLines(f diffcheck.File) (added map[int]string, removed map[string]bool) {
	added, removed = map[int]string{}, map[string]bool{}
	for _, h := range f.Hunks {
		n := h.NewStart
		for _, l := range h.Lines {
			switch l[0] {
			case '+':
				added[n] = l[1:]
				n++
			case '-':
				removed[l[1:]] = true
			case ' ':
				n++
			}
		}
	}
	return added, removed
}

// FindNonTestReferences marks each symbol of symbols reached when a non-test .go file of
// the package dir references it, over the head tree the view v reads (go/parser and
// go/ast; no go binary, no checkout). A symbol's own declaration is not a reference; a
// use, a call, a selector or a verb-table row is. A view that lists or reads nothing
// reaches nothing, so every symbol stays a finding.
func FindNonTestReferences(v WorkView, dir string, symbols map[string]bool) map[string]bool {
	reached := make(map[string]bool, len(symbols))
	if v.Ls == nil || v.Show == nil || len(symbols) == 0 {
		return reached
	}
	fset := token.NewFileSet()
	for _, f := range v.Ls(dir) {
		name := path.Base(f)
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, ok := v.Show(path.Join(dir, name))
		if !ok {
			continue
		}
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for s := range referencedSymbols(file, symbols) {
			reached[s] = true
		}
	}
	return reached
}

// referencedSymbols is the symbols of set the file references: an identifier that is no
// declaration name, or a selector's field or method name (x.Symbol).
func referencedSymbols(file *ast.File, symbols map[string]bool) map[string]bool {
	decls := map[*ast.Ident]bool{}
	for _, d := range file.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			decls[x.Name] = true
		case *ast.GenDecl:
			for _, s := range x.Specs {
				switch y := s.(type) {
				case *ast.TypeSpec:
					decls[y.Name] = true
				case *ast.ValueSpec:
					for _, n := range y.Names {
						decls[n] = true
					}
				}
			}
		}
	}
	out := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			if !decls[x] && symbols[x.Name] {
				out[x.Name] = true
			}
		case *ast.SelectorExpr:
			if symbols[x.Sel.Name] {
				out[x.Sel.Name] = true
			}
		}
		return true
	})
	return out
}

// mapsKeys is maps.Keys as a slice, without pulling the maps package into this file's
// imports for one call.
func mapsKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
