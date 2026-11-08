// Package pgbin finds the programs the functional tests start: the
// PostgreSQL server programs (initdb, pg_ctl, pg_dump) a throwaway server
// comes from, and the others they name (redis-server, nova-bus).
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

// Dir is the directory holding the PostgreSQL programs for the test t; when
// there is none, Missing.
func Dir(t testing.TB) string {
	t.Helper()
	if dir, ok := Find(exec.LookPath, filepath.Glob); ok {
		return dir
	}
	Missing(t, "PostgreSQL is missing: initdb is not on PATH nor in "+strings.Join(places, ", "))
	return ""
}

// Program is the path of the program name on PATH for the test t; when it
// is not there, Missing.
func Program(t testing.TB, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		Missing(t, name+" is missing: it is not on PATH")
	}
	return p
}

// Missing ends the test t, whose program msg names is absent. A short run
// (-short) skips t; any other run, and any run with NOVA_FUNCTIONAL=1 (CI
// sets it), fails it, so that asking for a functional test and getting none
// is never a pass.
func Missing(t testing.TB, msg string) {
	t.Helper()
	if skips(testing.Short(), os.Getenv("NOVA_FUNCTIONAL")) {
		t.Skip(msg)
	}
	t.Fatal(msg)
}

// skips is whether a missing program skips its test: only in a short run
// that NOVA_FUNCTIONAL (its value functional) does not ask to be strict.
func skips(short bool, functional string) bool { return short && functional != "1" }
