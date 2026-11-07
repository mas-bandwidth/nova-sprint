package verbs

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// pg_dump's major is the first word whose leading integer parses: a
// packager's suffix after the version is no version.
func TestDumpVersionReadsTheFirstNumberedWord(t *testing.T) {
	t.Parallel()
	for _, line := range []string{"pg_dump (PostgreSQL) 18.6 (Homebrew)\n", "pg_dump (PostgreSQL) 18.6", "pg_dump (PostgreSQL) 18.0 (Debian 18.0-1.pgdg120+1)"} {
		if err := dumpVersionRefusal(line, 18); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
	}
	err := dumpVersionRefusal("pg_dump (PostgreSQL) 16.4 (Debian 16.4-1.pgdg120+1)\n", 18)
	var op *OperationError
	if !errors.As(err, &op) || !strings.Contains(op.Problem, "pg_dump 16 vs server 18") || !strings.Contains(op.Problem, `"pg_dump (PostgreSQL) 16.4 (Debian 16.4-1.pgdg120+1)"`) || !strings.Contains(op.Fix, "PostgreSQL 18's bin directory") {
		t.Fatalf("%#v", err)
	}
	if err := dumpVersionRefusal("pg_dump (PostgreSQL) 14.9", 18); !errors.As(err, &op) || !strings.Contains(op.Problem, "pg_dump 14 vs server 18") {
		t.Fatalf("%#v", err)
	}
	if err := dumpVersionRefusal("pg_dump (PostgreSQL) devel", 18); !errors.As(err, &op) || !strings.Contains(op.Problem, `no version number in "pg_dump (PostgreSQL) devel"`) || op.Fix != dumpFix(18) {
		t.Fatalf("%#v", err)
	}
}

// With neither LANG nor LC_ALL a PostgreSQL child gets LC_ALL=C; a locale
// given is kept and none added; nothing outside the allow-list passes.
func TestSafeProcessEnvAlwaysHasALocale(t *testing.T) {
	t.Parallel()
	got := safeProcessEnv([]string{"PATH=/bin", "NOVA_PG_PASSWORD=x", "HOME=/h"})
	if !slices.Equal(got, []string{"PATH=/bin", "HOME=/h", "LC_ALL=C"}) {
		t.Fatalf("%q", got)
	}
	for _, locale := range []string{"LANG=en_US.UTF-8", "LC_ALL=en_GB.UTF-8"} {
		got = safeProcessEnv([]string{"PATH=/bin", locale})
		if !slices.Equal(got, []string{"PATH=/bin", locale}) {
			t.Fatalf("%q", got)
		}
	}
}

// The verification cluster uses no System V shared memory and no TCP.
func TestVerifyServerOptionsAvoidSysV(t *testing.T) {
	t.Parallel()
	got := verifyServerOptions("/tmp/v")
	for _, want := range []string{"-k /tmp/v", "listen_addresses=''", "shared_memory_type=mmap", "dynamic_shared_memory_type=posix"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q lacks %q", got, want)
		}
	}
}

// A verify cluster that does not start says what its log said: the last
// 20 lines, on one line, redacted; a missing log says so.
func TestLogTailQuotesTheServerLog(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "postgres.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("noise\n", 400)+"FATAL:  could not create shared memory segment: No space left on device\nDETAIL:  Failed system call was shmget(key=1, size=56, 03600).\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := logTail(path)
	if !strings.HasPrefix(got, "noise | ") || !strings.HasSuffix(got, "| FATAL:  could not create shared memory segment: No space left on device | DETAIL:  Failed system call was shmget(key=<redacted> size=<redacted> 03600).") || strings.Count(got, "noise") != 18 {
		t.Fatalf("%q", got)
	}
	if got := logTail(filepath.Join(t.TempDir(), "none")); !strings.HasPrefix(got, "unreadable (") {
		t.Fatalf("%q", got)
	}
}
