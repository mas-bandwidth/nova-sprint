// Package testkit holds the test rigs that were copied from package to package:
// running a tool's entry point in process with both streams captured and
// checked (verb.go), writing and reading the files a test sets up (here and
// tree.go), a recording wait for code a synctest bubble cannot hold
// (waits.go) and a skip by platform (skip.go). Each helper fails the test
// through testify's require, so a caller's setup is one line. Time in a test
// is testing/synctest's first, a clockwork.FakeClock where code does real
// I/O, and never a clock of the kit's own.
//
// A tool's tests keep one adapter of their own, the entry point as a Main, and
// call its methods. A tool whose entry point takes a clock, an environment or
// an app closes over it in that one line:
//
//	var cairn = testkit.Main(run)
//	var bus = testkit.Main(func(a []string, in io.Reader, o, e io.Writer) int { return run(a, in, o, e, now) })
//	out := cairn.OK(t, "open", "--store", dir).Stdout
//	r := cairn.Run("open") // r.Code, r.Stdout, r.Stderr
//	cairn.Do(t, "open").Exit(2).Refused("--store is required")
package testkit

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Main is a tool's entry point in process: the arguments after the tool's
// name, stdin, the two output streams, and the exit code it returns.
type Main func(args []string, stdin io.Reader, stdout, stderr io.Writer) int

// Result is one run of a Main: the exit code and everything each stream got.
type Result struct {
	Code           int
	Stdout, Stderr string
}

// Run calls the entry point with args and an empty stdin. It never fails the
// test: the caller asserts on the Result.
func (m Main) Run(args ...string) Result { return m.RunIn("", args...) }

// RunIn is Run with stdin holding the given text.
func (m Main) RunIn(stdin string, args ...string) Result {
	var out, errb bytes.Buffer
	code := m(args, strings.NewReader(stdin), &out, &errb)
	return Result{Code: code, Stdout: out.String(), Stderr: errb.String()}
}

// WriteFile writes body to path with mode 0o644, making the parent
// directories first, and fails the test on any error.
func WriteFile(t testing.TB, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

// ReadFile returns the file's contents, failing the test when it cannot be read.
func ReadFile(t testing.TB, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(raw)
}
