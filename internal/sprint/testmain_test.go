package sprint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain pins the go tool's real caches (a test that runs go keeps them) and
// points HOME and the XDG config and cache directories at one temporary
// directory, so no test writes the real seat's or user's files.
func TestMain(m *testing.M) {
	cleanup, err := tempTestHome()
	if err != nil {
		fmt.Fprintln(os.Stderr, "temp test home:", err)
		os.Exit(1)
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func tempTestHome() (func(), error) {
	realHome, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = filepath.Join(realHome, "go")
	}
	gomodcache := os.Getenv("GOMODCACHE")
	if gomodcache == "" {
		gomodcache = filepath.Join(gopath, "pkg", "mod")
	}
	gocache := os.Getenv("GOCACHE")
	if gocache == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}
		gocache = filepath.Join(cache, "go-build")
	}
	dir, err := os.MkdirTemp("", "nova-test-home-")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	for _, kv := range [][2]string{
		{"NOVA_TEST_REAL_HOME", realHome},
		{"GOPATH", gopath},
		{"GOMODCACHE", gomodcache},
		{"GOCACHE", gocache},
		{"HOME", dir},
		{"XDG_CONFIG_HOME", filepath.Join(dir, "config")},
		{"XDG_CACHE_HOME", filepath.Join(dir, "cache")},
	} {
		if err := os.Setenv(kv[0], kv[1]); err != nil {
			cleanup()
			return nil, err
		}
	}
	return cleanup, nil
}

// The suite runs with a temporary HOME and temporary XDG config and cache
// directories, never the real home the go command was started with, so a test
// that writes a twin brief or a cache file cannot touch the live seat.
func TestTheSuiteRunsWithATempHome(t *testing.T) {
	t.Parallel()
	real := os.Getenv("NOVA_TEST_REAL_HOME")
	if real == "" {
		t.Fatal("TestMain records the real home in NOVA_TEST_REAL_HOME")
	}
	temp := os.TempDir()
	for _, c := range []struct {
		name string
		fn   func() (string, error)
	}{
		{"os.UserHomeDir", os.UserHomeDir},
		{"os.UserConfigDir", os.UserConfigDir},
		{"os.UserCacheDir", os.UserCacheDir},
	} {
		got, err := c.fn()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		rel, err := filepath.Rel(temp, got)
		if err != nil {
			t.Fatalf("%s %q against os.TempDir %q: %v", c.name, got, temp, err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			t.Errorf("%s is %q, outside os.TempDir %q", c.name, got, temp)
		}
		if got == real {
			t.Errorf("%s is the real home %q", c.name, got)
		}
	}
}
