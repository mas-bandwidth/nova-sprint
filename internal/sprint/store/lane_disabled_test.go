package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// lanesDisabledShipped is sprint.LanesDisabled as the package ships it, read before
// TestMain clears it for the lanes' own tests.
var lanesDisabledShipped bool

func TestMain(m *testing.M) {
	lanesDisabledShipped = sprint.LanesDisabled
	// the lanes are disabled until v1.3; their tests keep the machinery tested until it
	// is replaced, and TestTheBenchLockIsDisabledUntilV13 pins the disabled path
	sprint.LanesDisabled = false
	cleanup, err := tempTestHome()
	if err != nil {
		fmt.Fprintln(os.Stderr, "temp test home:", err)
		os.Exit(1)
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}

// tempTestHome pins the go tool's real caches (a test that runs go keeps them) and
// points HOME and the XDG config and cache directories at one temporary directory,
// so no test writes the real seat's or user's files.
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
// that writes a cache file or a seat file cannot touch the live machine.
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

// The bench lock is disabled until v1.3 (Glenn, 2026-10-13: "Stop with these bench
// locks. Verboten." "Disable until then. Not a sanctioned or stable feature. Not to be
// used."): the package ships with sprint.LanesDisabled set, and then every take of one
// machine's one Go lane is granted at once (never queued, so it never blocks), a give
// gives nothing back, and the lanes record is never created. A bad name is still refused.
//
// Not parallel: it sets sprint.LanesDisabled, and a parallel test runs only after every
// sequential one has returned.
func TestTheBenchLockIsDisabledUntilV13(t *testing.T) {
	require.True(t, lanesDisabledShipped, "the lanes ship disabled until v1.3")
	sprint.LanesDisabled = true
	defer func() { sprint.LanesDisabled = false }()
	h := newHarness(t)
	for _, who := range []string{"a", "b", "c", "d"} {
		ans, err := h.st.LaneStep(h.ctx, sprint.LaneGo, "m1", who, false)
		require.NoError(t, err)
		assert.True(t, ans.Granted, "%s is granted at once on a one-lane machine", who)
		assert.Zero(t, ans.Place, "%s is never queued", who)
	}
	ans, err := h.st.LaneStep(h.ctx, sprint.LaneGo, "m1", "a", true)
	require.NoError(t, err)
	assert.False(t, ans.Gave, "a give gives nothing back: nothing was held")
	_, ok, err := h.m.GetKey(h.ctx, laneKey(sprint.LaneGo))
	require.NoError(t, err)
	assert.False(t, ok, "the lanes record is never created")
	rows, err := h.st.LaneRows(h.ctx)
	require.NoError(t, err)
	assert.Empty(t, rows)
	_, err = h.st.LaneStep(h.ctx, "rust", "m 1", "a,b", false)
	assert.Error(t, err, "a bad kind, machine or worker is still refused")
}
