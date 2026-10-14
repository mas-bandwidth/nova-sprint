package tlc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeJar(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tlc.jar")
	sum := sha256.Sum256([]byte("jar bytes"))
	require.NoError(t, os.WriteFile(path, []byte("jar bytes"), 0o644))
	return path, hex.EncodeToString(sum[:])
}

// TestFindJarResolvesTheFlag: FindJar returns the flag path absolute, its
// source and its SHA-256.
func TestFindJarResolvesTheFlag(t *testing.T) {
	t.Parallel()
	path, wantSum := writeJar(t)
	j, err := FindJar(path, func(string) string { return "" })
	require.NoError(t, err, "FindJar")
	assert.Equal(t, path, j.Path)
	assert.Equal(t, "flag", j.Source)
	assert.Equal(t, wantSum, j.SHA256)
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
	require.NoError(t, err, "FindJar")
	assert.Equal(t, path, j.Path)
	assert.Equal(t, "env:"+JarEnv, j.Source)
}

// TestFindJarRefusesNone: no flag and no env is refused with no jar named.
func TestFindJarRefusesNone(t *testing.T) {
	t.Parallel()
	_, err := FindJar("", func(string) string { return "" })
	require.Error(t, err, "FindJar with no jar succeeded")
	assert.ErrorContains(t, err, "no TLC jar named")
}

// TestFindJarRefusesAMissingFile: a flag naming a file that does not exist is
// refused.
func TestFindJarRefusesAMissingFile(t *testing.T) {
	t.Parallel()
	_, err := FindJar(filepath.Join(t.TempDir(), "absent.jar"), func(string) string { return "" })
	require.Error(t, err, "FindJar accepted a missing file")
	assert.ErrorContains(t, err, "not a readable file")
}

// TestFindJarFindsEitherOfTwoJars: a tree with two jars finds each; the digest
// is the file's own.
func TestFindJarFindsEitherOfTwoJars(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	one := filepath.Join(dir, "one.jar")
	two := filepath.Join(dir, "two.jar")
	require.NoError(t, os.WriteFile(one, []byte("one"), 0o644))
	require.NoError(t, os.WriteFile(two, []byte("two"), 0o644))
	j1, err := FindJar(one, func(string) string { return "" })
	require.NoError(t, err, "FindJar(%s)", one)
	j2, err := FindJar(two, func(string) string { return "" })
	require.NoError(t, err, "FindJar(%s)", two)
	assert.Equal(t, one, j1.Path)
	assert.Equal(t, two, j2.Path)
	assert.NotEqual(t, j1.SHA256, j2.SHA256)
}

// TestFindHelperResolvesAnOverride: FindHelper returns the override path
// absolute when it exists.
func TestFindHelperResolvesAnOverride(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "java")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755))
	got, err := FindHelper("java", path, func(string) (string, error) { return "", os.ErrNotExist })
	require.NoError(t, err, "FindHelper")
	assert.Equal(t, path, got)
}

// TestFindHelperRefusesAMissingOverride: an override that does not exist is
// refused.
func TestFindHelperRefusesAMissingOverride(t *testing.T) {
	t.Parallel()
	_, err := FindHelper("java", filepath.Join(t.TempDir(), "absent"), func(string) (string, error) { return "", nil })
	require.Error(t, err, "FindHelper accepted a missing override")
	assert.ErrorContains(t, err, "does not exist")
}

// TestFindHelperUsesLookPath: with no override FindHelper asks lookPath and
// returns the resolved path.
func TestFindHelperUsesLookPath(t *testing.T) {
	t.Parallel()
	called := false
	got, err := FindHelper("java", "", func(name string) (string, error) {
		called = true
		assert.Equal(t, "java", name, "lookPath called with wrong name")
		return "/usr/bin/java", nil
	})
	require.NoError(t, err, "FindHelper")
	assert.True(t, called)
	assert.Equal(t, "/usr/bin/java", got)
}

// TestFindHelperNamesANotOnPath: a lookPath miss is reported as not on PATH.
func TestFindHelperNamesANotOnPath(t *testing.T) {
	t.Parallel()
	_, err := FindHelper("java", "", func(string) (string, error) { return "", os.ErrNotExist })
	require.Error(t, err)
	assert.ErrorContains(t, err, "not on PATH")
}

// TestJavaVersionReadsJava11To21Banners: JavaVersion reads the quoted token from
// the version banners of Java 11, 17 and 21.
func TestJavaVersionReadsJava11To21Banners(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, banner, want string }{
		{"11", "openjdk version \"11.0.2\" 2019-01-15\nOpenJDK Runtime Environment 18.9 (build 11.0.2+9)\n", "11.0.2"},
		{"17", "openjdk version \"17.0.9\" 2023-10-17\nOpenJDK Runtime Environment (build 17.0.9+9)\n", "17.0.9"},
		{"21", "openjdk version \"21.0.12.1\" 2025-04-15\nOpenJDK 64-Bit Server VM (build 21.0.12.1+7)\n", "21.0.12.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := JavaVersion(tc.banner)
			require.NoError(t, err, "JavaVersion(%q)", tc.banner)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestJavaVersionSkipsToolOptionsLines: a Picked-up line before the version is
// skipped; java version (not openjdk) works too.
func TestJavaVersionSkipsToolOptionsLines(t *testing.T) {
	t.Parallel()
	got, err := JavaVersion("Picked up JAVA_TOOL_OPTIONS: -Xms64m\njava version \"21.0.1\" 2025-04-15\n")
	require.NoError(t, err, "JavaVersion")
	assert.Equal(t, "21.0.1", got)
}

// TestJavaVersionRefusesGarbage: garbage with no version line is an error.
func TestJavaVersionRefusesGarbage(t *testing.T) {
	t.Parallel()
	_, err := JavaVersion("some random noise\nnot a version\n")
	require.Error(t, err, "JavaVersion accepted garbage")
	assert.ErrorContains(t, err, "printed no line")
}

// TestReadJavaVersionRunsTheJavaScript: ReadJavaVersion runs the java program
// with -version and returns what JavaVersion reads.
func TestReadJavaVersionRunsTheJavaScript(t *testing.T) {
	t.Parallel()
	runJavaScript(t, "exit 0\n", func(java string) {
		got, err := ReadJavaVersion(context.Background(), java)
		require.NoError(t, err, "ReadJavaVersion")
		assert.Equal(t, "21.0.1", got)
	})
}
