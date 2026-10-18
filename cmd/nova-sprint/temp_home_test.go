package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The suite runs with a temporary HOME and temporary XDG config and cache
// directories, and the go command keeps the caches it was started with: no test
// can write the real seat's ~/.config/nova-sprint/seat-environment.json, the
// user's ~/nova-sprint/jobs, or a land clone under the real cache.
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
