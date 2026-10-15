package ci

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoTestSignalsItsOwnProcess is the guard for the hook that lost its socket on CI
// (2026-10-12, v1.2.16): TestTheInterruptOfTheCommandIsSIGINTAndSIGTERM sent a real
// SIGINT and SIGTERM to its own process, the test binary, and every command of every
// test running beside it that listens for an interrupt (a held `nova-hook hook`,
// `where --watch`) ended at it. TestAHookStaysLiveAcrossThreeBoundsWhileItsSeatActs and
// TestHookCompactPrintsOnlyWhatTheSeatActsOn then found their hook gone, its socket
// removed, while their seat still acted. A test that wants a real signal sends it to a
// child process it started (exec.Cmd's Process), never to the test binary: this scan
// refuses, in every _test.go of the module, a signal sent to the process itself
// (os.FindProcess of its own pid, a kill of its own pid or of its process group).
func TestNoTestSignalsItsOwnProcess(t *testing.T) {
	root := "../.."
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "tmp":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(files)
	require.NotEmpty(t, files, "the scan found no test files under %s", root)

	var problems []string
	for _, path := range files {
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		found, err := scanSelfSignals(path, src)
		require.NoError(t, err, "parsing %s", path)
		problems = append(problems, found...)
	}
	require.Empty(t, problems, "a test signals its own process, the test binary every parallel test shares; start a child and signal it instead:\n%s", strings.Join(problems, "\n"))
}

// The scan on known cases: each way a test can signal itself is found, and each
// way of signalling, or naming, another process is not.
func TestTheSelfSignalScanFindsEachFormAndNothingElse(t *testing.T) {
	t.Parallel()
	yes := map[string]string{
		"find own pid":         `self, _ := os.FindProcess(os.Getpid()); _ = self.Signal(syscall.SIGINT)`,
		"find own pid syscall": `self, _ := os.FindProcess(syscall.Getpid()); _ = self.Kill()`,
		"kill own pid":         `_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)`,
		"kill own pid unix":    `_ = unix.Kill(unix.Getpid(), unix.SIGINT)`,
		"kill own pid by var":  `pid := os.Getpid(); _ = syscall.Kill(pid, syscall.SIGINT)`,
		"find own pid by var":  `var me = os.Getpid(); p, _ := os.FindProcess(me); _ = p.Signal(os.Interrupt)`,
		"kill own group":       `_ = syscall.Kill(0, syscall.SIGINT)`,
		"kill own group neg":   `_ = syscall.Kill(-syscall.Getpgrp(), syscall.SIGINT)`,
		"tgkill own pid":       `_ = unix.Tgkill(os.Getpid(), unix.Gettid(), unix.SIGINT)`,
	}
	no := map[string]string{
		"child process":   `cmd := exec.Command("x"); _ = cmd.Start(); _ = cmd.Process.Signal(syscall.SIGINT)`,
		"find child":      `p, _ := os.FindProcess(cmd.Process.Pid); _ = p.Signal(syscall.SIGTERM)`,
		"kill child pid":  `_ = syscall.Kill(child, syscall.SIGINT)`,
		"probe child":     `_ = syscall.Kill(pid, 0)`,
		"own pid as data": `st := demoState{PID: os.Getpid()}; _ = st`,
		"own pid printed": `fmt.Println(os.Getpid())`,
	}
	wrap := func(body string) []byte {
		return []byte("package x\n\nfunc f() {\n" + body + "\n}\n")
	}
	for name, body := range yes {
		found, err := scanSelfSignals(name+"_test.go", wrap(body))
		require.NoError(t, err, name)
		assert.Len(t, found, 1, "%s: %s", name, body)
	}
	for name, body := range no {
		found, err := scanSelfSignals(name+"_test.go", wrap(body))
		require.NoError(t, err, name)
		assert.Empty(t, found, "%s: %s", name, body)
	}
}

// scanSelfSignals finds, in one Go source file, each call that signals the process
// itself: os.FindProcess of its own pid, and Kill or Tgkill (syscall, unix) of its own
// pid or of its own process group (0, or minus its group id). Its own pid is a
// Getpid() call or an identifier the file assigns one to.
func scanSelfSignals(path string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, err
	}
	pidVars := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range v.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && i < len(v.Rhs) && isCallOf(v.Rhs[i], "Getpid") {
					pidVars[id.Name] = true
				}
			}
		case *ast.ValueSpec:
			for i, id := range v.Names {
				if i < len(v.Values) && isCallOf(v.Values[i], "Getpid") {
					pidVars[id.Name] = true
				}
			}
		}
		return true
	})
	ownPid := func(e ast.Expr) bool {
		if id, ok := e.(*ast.Ident); ok {
			return pidVars[id.Name]
		}
		return isCallOf(e, "Getpid")
	}
	ownGroup := func(e ast.Expr) bool {
		if lit, ok := e.(*ast.BasicLit); ok {
			return lit.Kind == token.INT && lit.Value == "0"
		}
		if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.SUB {
			return isCallOf(u.X, "Getpgrp") || ownPid(u.X)
		}
		return false
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok || len(ce.Args) == 0 {
			return true
		}
		sel, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		var what string
		switch sel.Sel.Name {
		case "FindProcess":
			if ownPid(ce.Args[0]) {
				what = "finds its own process to signal it"
			}
		case "Kill", "Tgkill":
			if len(ce.Args) >= 2 && ownPid(ce.Args[0]) {
				what = "kills its own pid"
			} else if len(ce.Args) >= 2 && ownGroup(ce.Args[0]) {
				what = "kills its own process group"
			}
		}
		if what != "" {
			out = append(out, fset.Position(ce.Pos()).String()+": "+what)
		}
		return true
	})
	return out, nil
}

// isCallOf reports whether e is a call of a function or method named name with no
// arguments (os.Getpid(), syscall.Getpgrp()).
func isCallOf(e ast.Expr, name string) bool {
	ce, ok := e.(*ast.CallExpr)
	if !ok || len(ce.Args) != 0 {
		return false
	}
	switch fn := ce.Fun.(type) {
	case *ast.SelectorExpr:
		return fn.Sel.Name == name
	case *ast.Ident:
		return fn.Name == name
	}
	return false
}
