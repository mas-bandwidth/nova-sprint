// Package pgbin finds the PostgreSQL server programs (initdb, pg_ctl, pg_dump)
// the functional tests start a throwaway server from.
package pgbin

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Where, after PATH, the programs are looked for: Debian's versioned
// directories, then Homebrew's on Apple silicon and on Intel.
var places = []string{
	"/usr/lib/postgresql/*/bin",
	"/opt/homebrew/opt/postgresql*/bin",
	"/usr/local/opt/postgresql*/bin",
}

var digits = regexp.MustCompile(`\d+`)

// Find returns the directory holding initdb: the one on PATH, else the first
// of places holding it, the highest version first within a place. lookPath
// and glob are exec.LookPath and filepath.Glob in a test run.
func Find(lookPath func(string) (string, error), glob func(string) ([]string, error)) (string, bool) {
	if p, err := lookPath("initdb"); err == nil {
		return filepath.Dir(p), true
	}
	for _, place := range places {
		dirs, _ := glob(place) // ignored: a bad pattern is a constant's
		sort.SliceStable(dirs, func(i, j int) bool { return version(dirs[i]) > version(dirs[j]) })
		for _, dir := range dirs {
			if found, _ := glob(filepath.Join(dir, "initdb")); len(found) > 0 {
				return dir, true
			}
		}
	}
	return "", false
}

// version is the first number in the bin directory's parent's name
// (postgresql@18 -> 18, 17 -> 17); an unversioned name is 0.
func version(dir string) int {
	n, _ := strconv.Atoi(digits.FindString(filepath.Base(filepath.Dir(dir))))
	return n
}

// Dir is the directory holding the PostgreSQL programs for the test t. When
// there is none, a short run (-short) skips t; any other run, and any run with
// NOVA_FUNCTIONAL=1, fails it, so that asking for a functional test and getting
// none is never a pass.
func Dir(t testing.TB) string {
	t.Helper()
	if dir, ok := Find(exec.LookPath, filepath.Glob); ok {
		return dir
	}
	msg := "PostgreSQL is missing: initdb is not on PATH nor in " + strings.Join(places, ", ")
	if testing.Short() && os.Getenv("NOVA_FUNCTIONAL") != "1" {
		t.Skip(msg)
	}
	t.Fatal(msg)
	return ""
}
