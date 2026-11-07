// Command silentlint refuses the forms by which an error goes silent (the
// rule from the first live runs: "We are NOT ALLOWED to fail silently."
// "We provide output to help resolve the error."). It reads Go source with
// go/ast only (no type checker), so each rule is a syntactic shape:
//
//	S1 fixed-string: inside an err branch (`if err != nil`, err being any
//	   identifier named err, errX or xErr), a new error built with
//	   fmt.Errorf (no %w and no use of err), errors.New, a refusal literal
//	   (&OperationError{...}, &Fail{...}) or a refusal constructor (refuse,
//	   broken, misuse, serviceConfigError, publicVersionRefusal) that does
//	   not use err. A nested if whose condition reads err (errors.Is,
//	   errors.As, a code test) has classified err and is not checked.
//	S2 pass-through: inside an err branch, a refusal literal or refusal
//	   constructor that does use err, in a branch that never asks
//	   errors.As/AsRefusal on err first: a deeper *OperationError's Problem
//	   and Fix would be flattened into a new one.
//	S3 blank discard: `_ = f()` or `_, _ = f()` (every left side blank, one
//	   call on the right) unless the call is one of the best-effort
//	   cleanups named in allowedDiscards.
//	S4 bare exit: inside an err branch that never reads err, `return N` (N
//	   a non-zero integer literal, alone or first of several results) or
//	   os.Exit(N): the process says it failed without saying why.
//
// Test files (_test.go) and testdata are not read. Usage:
// silentlint [dir ...] (default "."); each finding is printed as
// "file:line: rule: message" and any finding exits 1.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// allowedDiscards are the calls whose error may be dropped with `_ =`, by
// the called name (a method or function name), each a best-effort cleanup
// on a path that has already failed or already finished:
var allowedDiscards = map[string]string{
	"Close":     "closing a file, connection, session or body after use or after a failure",
	"Rollback":  "rolling back a transaction that did not commit (a no-op after Commit)",
	"Remove":    "removing a temporary file or empty directory",
	"RemoveAll": "removing a temporary directory",
	"Kill":      "killing a child that is being abandoned",
	"Signal":    "signalling a child that is being stopped",
	"Wait":      "reaping a child that was just killed or abandoned",
}

// refusalConstructors build a refusal from strings; refusalTypes are the
// refusal literal types.
var (
	refusalConstructors = map[string]bool{"refuse": true, "broken": true, "misuse": true, "serviceConfigError": true, "publicVersionRefusal": true}
	refusalTypes        = map[string]bool{"OperationError": true, "Fail": true}
	passThroughCalls    = map[string]bool{"AsRefusal": true, "RefusalOr": true}
)

// Finding is one silent form.
type Finding struct {
	Pos  token.Position
	Rule string
	Msg  string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", f.Pos.Filename, f.Pos.Line, f.Rule, f.Msg)
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		args = []string{"."}
	}
	var all []Finding
	files := 0
	for _, root := range args {
		found, n, err := lintTree(root)
		if err != nil {
			fmt.Fprintf(stderr, "silentlint: %s: %v\n", root, err)
			return 2
		}
		all = append(all, found...)
		files += n
	}
	for _, f := range all {
		fmt.Fprintln(stdout, f)
	}
	if len(all) > 0 {
		fmt.Fprintf(stderr, "silentlint: %d silent form(s) in %d file(s) read\n", len(all), files)
		return 1
	}
	fmt.Fprintf(stdout, "silentlint: clean (%d files, rules S1 fixed-string, S2 pass-through, S3 blank discard, S4 bare exit)\n", files)
	return 0
}

// lintTree lints every non-test .go file under root, skipping testdata and
// hidden directories.
func lintTree(root string) ([]Finding, int, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	sort.Strings(paths)
	var out []Finding
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, 0, err
		}
		found, err := LintSource(path, src)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, found...)
	}
	return out, len(paths), nil
}

// LintSource lints one file's source.
func LintSource(name string, src []byte) ([]Finding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	l := &linter{fset: fset}
	ast.Inspect(file, l.visit)
	sort.SliceStable(l.found, func(i, j int) bool { return l.found[i].Pos.Offset < l.found[j].Pos.Offset })
	return l.found, nil
}

type linter struct {
	fset  *token.FileSet
	found []Finding
}

func (l *linter) add(at token.Pos, rule, msg string) {
	l.found = append(l.found, Finding{Pos: l.fset.Position(at), Rule: rule, Msg: msg})
}

func (l *linter) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.IfStmt:
		if name := errCondition(n.Cond); name != "" {
			l.errBranch(name, n.Body)
		}
	case *ast.AssignStmt:
		l.blankDiscard(n)
	}
	return true
}

// errName is an identifier that holds an error: err, errX, xErr.
func errName(name string) bool {
	if name == "err" {
		return true
	}
	if strings.HasPrefix(name, "err") && len(name) > 3 && (name[3] >= 'A' && name[3] <= 'Z' || name[3] >= '0' && name[3] <= '9') {
		return true
	}
	return strings.HasSuffix(name, "Err") && len(name) > 3
}

// errCondition is the error identifier an if tests against nil (`err !=
// nil`, alone or as one operand of &&), or "".
func errCondition(cond ast.Expr) string {
	switch c := cond.(type) {
	case *ast.ParenExpr:
		return errCondition(c.X)
	case *ast.BinaryExpr:
		switch c.Op {
		case token.NEQ:
			x, ok := c.X.(*ast.Ident)
			y, ok2 := c.Y.(*ast.Ident)
			if ok && ok2 && y.Name == "nil" && errName(x.Name) {
				return x.Name
			}
		case token.LAND:
			if name := errCondition(c.X); name != "" {
				return name
			}
			return errCondition(c.Y)
		}
	}
	return ""
}

// uses is true when node reads the identifier name.
func uses(node ast.Node, name string) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// calleeName is a call's last name: f for f(), x.f(), x.y.f().
func calleeName(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.IndexExpr:
		if id, ok := f.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// qualified is a call's pkg.Name when it is a selector on an identifier.
func qualified(call *ast.CallExpr) string {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if id, ok := sel.X.(*ast.Ident); ok {
			return id.Name + "." + sel.Sel.Name
		}
	}
	return ""
}

func refusalLiteral(n ast.Node) (string, bool) {
	u, ok := n.(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return "", false
	}
	lit, ok := u.X.(*ast.CompositeLit)
	if !ok {
		return "", false
	}
	var name string
	switch t := lit.Type.(type) {
	case *ast.Ident:
		name = t.Name
	case *ast.SelectorExpr:
		name = t.Sel.Name
	}
	return name, refusalTypes[name]
}

// errBranch checks the body of an `if name != nil` branch.
func (l *linter) errBranch(name string, body *ast.BlockStmt) {
	read := false // the branch reads err (beyond the condition)
	passed := false
	for _, st := range body.List {
		if uses(st, name) {
			read = true
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && len(call.Args) > 0 {
			if (qualified(call) == "errors.As" || passThroughCalls[calleeName(call)]) && uses(call.Args[0], name) {
				passed = true
			}
		}
		return true
	})
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.IfStmt:
			if n.Cond != nil && uses(n.Cond, name) {
				// err classified: its own branches are a translation; an
				// inner `if err != nil` is visited on its own.
				return false
			}
		case *ast.UnaryExpr:
			if typ, ok := refusalLiteral(n); ok {
				l.refusal(n, name, "&"+typ+"{...}", passed)
				return false
			}
		case *ast.CallExpr:
			l.errCall(n, name, passed)
		case *ast.ReturnStmt:
			if !read && len(n.Results) > 0 && nonZeroInt(n.Results[0]) {
				l.add(n.Pos(), "S4", "a bare non-zero return in an err branch that never writes "+name+": write it (ChildFailed, a refusal line) before exiting")
			}
		case *ast.ExprStmt:
			if call, ok := n.X.(*ast.CallExpr); ok && !read && qualified(call) == "os.Exit" && len(call.Args) == 1 && nonZeroInt(call.Args[0]) {
				l.add(n.Pos(), "S4", "os.Exit in an err branch that never writes "+name+": write it before exiting")
			}
		}
		return true
	})
}

func (l *linter) refusal(n ast.Node, name, what string, passed bool) {
	switch {
	case !uses(n, name):
		l.add(n.Pos(), "S1", what+" in an err branch drops "+name+": carry it (its text in the Problem, or wrap it)")
	case !passed:
		l.add(n.Pos(), "S2", what+" re-wraps "+name+" without errors.As/AsRefusal first: a deeper *OperationError's Problem and Fix must pass through")
	}
}

func (l *linter) errCall(call *ast.CallExpr, name string, passed bool) {
	switch q := qualified(call); {
	case q == "fmt.Errorf":
		if len(call.Args) == 0 {
			return
		}
		format := ""
		if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			format, _ = strconv.Unquote(lit.Value)
		}
		if !strings.Contains(format, "%w") && !uses(call, name) {
			l.add(call.Pos(), "S1", "fmt.Errorf in an err branch neither wraps (%w) nor uses "+name)
		}
	case q == "errors.New":
		if !uses(call, name) {
			l.add(call.Pos(), "S1", "errors.New in an err branch drops "+name+": wrap it (fmt.Errorf %w, edge.Cause)")
		}
	case refusalConstructors[calleeName(call)]:
		l.refusal(call, name, calleeName(call)+"(...)", passed)
	}
}

func nonZeroInt(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return false
	}
	n, err := strconv.ParseInt(lit.Value, 0, 64)
	return err == nil && n != 0
}

// blankDiscard is S3: every left side blank and one call on the right.
func (l *linter) blankDiscard(a *ast.AssignStmt) {
	if a.Tok != token.ASSIGN || len(a.Rhs) != 1 {
		return
	}
	for _, lhs := range a.Lhs {
		if id, ok := lhs.(*ast.Ident); !ok || id.Name != "_" {
			return
		}
	}
	call, ok := a.Rhs[0].(*ast.CallExpr)
	if !ok {
		return
	}
	name := calleeName(call)
	if _, ok := allowedDiscards[name]; ok {
		return
	}
	l.add(a.Pos(), "S3", "the result of "+name+"(...) is discarded with _: handle or report its error (allowed discards: "+allowedList()+")")
}

func allowedList() string {
	names := make([]string, 0, len(allowedDiscards))
	for n := range allowedDiscards {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
