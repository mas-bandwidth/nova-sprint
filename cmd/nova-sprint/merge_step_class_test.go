package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const mergeStepStoreImport = "github.com/mas-bandwidth/nova-sprint/internal/sprint/store"

// MergeStep is entered by the merge and the lander's one step wrapper only:
// the merge verb has its own push and ancestor guards, and the lander supplies
// the pins those guards establish. Keep the direct caller set exact.
func TestOnlyTheMergeVerbAndLanderCallStoreMergeStep(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	require.Empty(t, mergeStepCallerProblems(root))
}

func TestMergeStepCallerGuardRejectsOtherFunctionsAndFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0700))
		require.NoError(t, os.WriteFile(full, []byte(body), 0600))
	}

	write("cmd/nova-sprint/verbs.go", `package main
import stepStore "`+mergeStepStoreImport+`"
type app struct{}
func external()
func (*app) cmdMerge() { _ = stepStore.MergeStep(nil); func() { _ = stepStore.MergeStep(nil) }() }
func (*app) helper() { _ = stepStore.MergeStep(nil) }
func unrelated() { _ = stepStore.MergeStep(nil) }
`)
	write("cmd/nova-sprint/land.go", `package main
import storeAlias "`+mergeStepStoreImport+`"
type lander struct{}
func (*lander) stepWith() { _ = storeAlias.MergeStep(nil) }
`)
	write("internal/elsewhere/caller.go", `package elsewhere
import "`+mergeStepStoreImport+`"
func accidental() { _ = store.MergeStep(nil) }
`)
	write("cmd/nova-sprint/verbs_test.go", `package main
import "`+mergeStepStoreImport+`"
func testOnly() { _ = store.MergeStep(nil) }
`)
	write("cmd/nova-sprint/unrelated.go", `package main
import other "example.com/other"
func unrelatedType() { _ = other.MergeStep(nil) }
`)

	problems := mergeStepCallerProblems(root)
	require.Len(t, problems, 4, "only the same-file other method, same-file free function, nested function literal, and other file are reported")
	require.Contains(t, strings.Join(problems, "\n"), "cmd/nova-sprint/verbs.go:6: func app.helper")
	require.Contains(t, strings.Join(problems, "\n"), "cmd/nova-sprint/verbs.go:7: func unrelated")
	require.Contains(t, strings.Join(problems, "\n"), "cmd/nova-sprint/verbs.go:5: func function literal")
	require.Contains(t, strings.Join(problems, "\n"), "internal/elsewhere/caller.go:3: func accidental")
}

func TestMergeStepCallerGuardSkipsHiddenDirectoriesButScansRoot(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	root := filepath.Join(parent, ".hk-root")
	require.NoError(t, os.Mkdir(root, 0700))
	write := func(path, body string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0700))
		require.NoError(t, os.WriteFile(full, []byte(body), 0600))
	}
	badCaller := `package p
import store "` + mergeStepStoreImport + `"
func accidental() { store.MergeStep(nil) }
`
	write("root.go", badCaller)
	write("pkg/visible.go", badCaller)
	write(".hk-transient/hidden.go", badCaller)

	problems := mergeStepCallerProblems(root)
	require.Len(t, problems, 2, "the root and visible package files remain scanned; hidden transient files do not")
	joined := strings.Join(problems, "\n")
	require.Contains(t, joined, "root.go:3: func accidental")
	require.Contains(t, joined, "pkg/visible.go:3: func accidental")
	require.NotContains(t, joined, ".hk-transient")
}

func mergeStepCallerProblems(root string) []string {
	var problems []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if filepath.Clean(path) != filepath.Clean(root) && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			switch entry.Name() {
			case ".git", "node_modules", "testdata", "tmp", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		fset := gotoken.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: parse: %v", rel, err))
			return nil
		}
		aliases, dotImport := mergeStepImportNames(file)
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				problems = appendMergeStepCalls(problems, fset, rel, "package initializer", false, decl, aliases, dotImport)
				continue
			}
			caller := mergeStepCallerName(fd)
			allowed := mergeStepCallerAllowed(rel, caller)
			if fd.Body == nil {
				continue
			}
			problems = appendMergeStepCalls(problems, fset, rel, caller, allowed, fd.Body, aliases, dotImport)
		}
		return nil
	})
	if err != nil {
		problems = append(problems, fmt.Sprintf("walk %s: %v", root, err))
	}
	sort.Strings(problems)
	return problems
}

func mergeStepImportNames(file *ast.File) (map[string]bool, bool) {
	aliases := make(map[string]bool)
	dotImport := false
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != mergeStepStoreImport {
			continue
		}
		name := filepath.Base(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		switch name {
		case ".":
			dotImport = true
		case "_":
			// A blank import cannot name MergeStep.
		default:
			aliases[name] = true
		}
	}
	return aliases, dotImport
}

func mergeStepCallerName(fd *ast.FuncDecl) string {
	if fd == nil || fd.Name == nil {
		return ""
	}
	if receiver := mergeStepReceiverName(fd.Recv); receiver != "" {
		return receiver + "." + fd.Name.Name
	}
	return fd.Name.Name
}

func mergeStepReceiverName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) != 1 {
		return ""
	}
	typ := recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func mergeStepCallerAllowed(path, caller string) bool {
	switch {
	case path == "cmd/nova-sprint/verbs.go" && caller == "app.cmdMerge":
		return true
	case path == "cmd/nova-sprint/land.go" && caller == "lander.stepWith":
		return true
	default:
		return false
	}
}

func appendMergeStepCalls(problems []string, fset *gotoken.FileSet, path, caller string, allowed bool, node ast.Node, aliases map[string]bool, dotImport bool) []string {
	if node == nil {
		return problems
	}
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			problems = appendMergeStepCalls(problems, fset, path, "function literal", false, lit.Body, aliases, dotImport)
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isStoreMergeStepCall(call.Fun, aliases, dotImport) || allowed {
			return true
		}
		pos := fset.Position(call.Pos())
		problems = append(problems, fmt.Sprintf("%s:%d: func %s calls store.MergeStep outside its two named callers", path, pos.Line, caller))
		return true
	})
	return problems
}

func isStoreMergeStepCall(fun ast.Expr, aliases map[string]bool, dotImport bool) bool {
	switch x := fun.(type) {
	case *ast.SelectorExpr:
		packageName, ok := x.X.(*ast.Ident)
		return ok && aliases[packageName.Name] && x.Sel.Name == "MergeStep"
	case *ast.Ident:
		return dotImport && x.Name == "MergeStep"
	default:
		return false
	}
}
