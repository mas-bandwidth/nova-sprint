package tlc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeJar(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tlc.jar")
	sum := sha256.Sum256([]byte("jar bytes"))
	if err := os.WriteFile(path, []byte("jar bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, hex.EncodeToString(sum[:])
}

// TestFindJarResolvesTheFlag: FindJar returns the flag path absolute, its
// source and its SHA-256.
func TestFindJarResolvesTheFlag(t *testing.T) {
	t.Parallel()
	path, wantSum := writeJar(t)
	j, err := FindJar(path, func(string) string { return "" })
	if err != nil {
		t.Fatalf("FindJar: %v", err)
	}
	if j.Path != path {
		t.Fatalf("Jar.Path=%q want %q", j.Path, path)
	}
	if j.Source != "flag" {
		t.Fatalf("Jar.Source=%q want flag", j.Source)
	}
	if j.SHA256 != wantSum {
		t.Fatalf("Jar.SHA256=%q want %q", j.SHA256, wantSum)
	}
}

// TestFindJarResolvesTheEnvironment: with an empty flag FindJar uses the
// environment variable.
func TestFindJarResolvesTheEnvironment(t *testing.T) {
	t.Parallel()
	path, _ := writeJar(t)
	j, err := FindJar("", func(k string) string {
		if k == JarEnv {
			return path
		}
		return ""
	})
	if err != nil {
		t.Fatalf("FindJar: %v", err)
	}
	if j.Path != path || j.Source != "env:"+JarEnv {
		t.Fatalf("FindJar env = %+v, want path %q source env", j, path)
	}
}

// TestFindJarRefusesNone: no flag and no env is refused with no jar named.
func TestFindJarRefusesNone(t *testing.T) {
	t.Parallel()
	_, err := FindJar("", func(string) string { return "" })
	if err == nil {
		t.Fatal("FindJar with no jar succeeded")
	}
	if !strings.Contains(err.Error(), "no TLC jar named") {
		t.Fatalf("FindJar error = %v", err)
	}
}

// TestFindJarRefusesAMissingFile: a flag naming a file that does not exist is
// refused.
func TestFindJarRefusesAMissingFile(t *testing.T) {
	t.Parallel()
	_, err := FindJar(filepath.Join(t.TempDir(), "absent.jar"), func(string) string { return "" })
	if err == nil {
		t.Fatal("FindJar accepted a missing file")
	}
	if !strings.Contains(err.Error(), "not a readable file") {
		t.Fatalf("FindJar missing error = %v", err)
	}
}

// TestFindJarFindsEitherOfTwoJars: a tree with two jars finds each; the digest
// is the file's own.
func TestFindJarFindsEitherOfTwoJars(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	one := filepath.Join(dir, "one.jar")
	two := filepath.Join(dir, "two.jar")
	if err := os.WriteFile(one, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(two, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	j1, err := FindJar(one, func(string) string { return "" })
	if err != nil {
		t.Fatalf("FindJar(%s): %v", one, err)
	}
	j2, err := FindJar(two, func(string) string { return "" })
	if err != nil {
		t.Fatalf("FindJar(%s): %v", two, err)
	}
	if j1.Path != one || j2.Path != two || j1.SHA256 == j2.SHA256 {
		t.Fatalf("two jars resolved to %+v and %+v", j1, j2)
	}
}

// TestFindHelperResolvesAnOverride: FindHelper returns the override path
// absolute when it exists.
func TestFindHelperResolvesAnOverride(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "java")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindHelper("java", path, func(string) (string, error) { return "", os.ErrNotExist })
	if err != nil {
		t.Fatalf("FindHelper: %v", err)
	}
	if got != path {
		t.Fatalf("FindHelper=%q want %q", got, path)
	}
}

// TestFindHelperRefusesAMissingOverride: an override that does not exist is
// refused.
func TestFindHelperRefusesAMissingOverride(t *testing.T) {
	t.Parallel()
	_, err := FindHelper("java", filepath.Join(t.TempDir(), "absent"), func(string) (string, error) { return "", nil })
	if err == nil {
		t.Fatal("FindHelper accepted a missing override")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("FindHelper missing override error = %v", err)
	}
}

// TestFindHelperUsesLookPath: with no override FindHelper asks lookPath and
// returns the resolved path.
func TestFindHelperUsesLookPath(t *testing.T) {
	t.Parallel()
	called := false
	got, err := FindHelper("java", "", func(name string) (string, error) {
		called = true
		if name != "java" {
			t.Fatalf("lookPath called with %q", name)
		}
		return "/usr/bin/java", nil
	})
	if err != nil {
		t.Fatalf("FindHelper: %v", err)
	}
	if !called || got != "/usr/bin/java" {
		t.Fatalf("FindHelper lookPath = %q (called=%t)", got, called)
	}
}

// TestFindHelperNamesANotOnPath: a lookPath miss is reported as not on PATH.
func TestFindHelperNamesANotOnPath(t *testing.T) {
	t.Parallel()
	_, err := FindHelper("java", "", func(string) (string, error) { return "", os.ErrNotExist })
	if err == nil || !strings.Contains(err.Error(), "not on PATH") {
		t.Fatalf("FindHelper not-on-path error = %v", err)
	}
}

// TestJavaVersionReadsJava11To21Banners: JavaVersion reads the quoted token from
// the version banners of Java 11, 17 and 21.
func TestJavaVersionReadsJava11To21Banners(t *testing.T) {
	t.Parallel()
	cases := []struct{ banner, want string }{
		{"openjdk version \"11.0.2\" 2019-01-15\nOpenJDK Runtime Environment 18.9 (build 11.0.2+9)\n", "11.0.2"},
		{"openjdk version \"17.0.9\" 2023-10-17\nOpenJDK Runtime Environment (build 17.0.9+9)\n", "17.0.9"},
		{"openjdk version \"21.0.12.1\" 2025-04-15\nOpenJDK 64-Bit Server VM (build 21.0.12.1+7)\n", "21.0.12.1"},
	}
	for _, tc := range cases {
		got, err := JavaVersion(tc.banner)
		if err != nil {
			t.Fatalf("JavaVersion(%q): %v", tc.banner, err)
		}
		if got != tc.want {
			t.Fatalf("JavaVersion=%q want %q", got, tc.want)
		}
	}
}

// TestJavaVersionSkipsToolOptionsLines: a Picked-up line before the version is
// skipped; java version (not openjdk) works too.
func TestJavaVersionSkipsToolOptionsLines(t *testing.T) {
	t.Parallel()
	got, err := JavaVersion("Picked up JAVA_TOOL_OPTIONS: -Xms64m\njava version \"21.0.1\" 2025-04-15\n")
	if err != nil {
		t.Fatalf("JavaVersion: %v", err)
	}
	if got != "21.0.1" {
		t.Fatalf("JavaVersion=%q want 21.0.1", got)
	}
}

// TestJavaVersionRefusesGarbage: garbage with no version line is an error.
func TestJavaVersionRefusesGarbage(t *testing.T) {
	t.Parallel()
	_, err := JavaVersion("some random noise\nnot a version\n")
	if err == nil {
		t.Fatal("JavaVersion accepted garbage")
	}
	if !strings.Contains(err.Error(), "printed no line") {
		t.Fatalf("JavaVersion garbage error = %v", err)
	}
}

// TestReadJavaVersionRunsTheJavaScript: ReadJavaVersion runs the java program
// with -version and returns what JavaVersion reads.
func TestReadJavaVersionRunsTheJavaScript(t *testing.T) {
	t.Parallel()
	runJavaScript(t, "exit 0\n", func(java string) {
		got, err := ReadJavaVersion(context.Background(), java)
		if err != nil {
			t.Fatalf("ReadJavaVersion: %v", err)
		}
		if got != "21.0.1" {
			t.Fatalf("ReadJavaVersion=%q want 21.0.1", got)
		}
	})
}
