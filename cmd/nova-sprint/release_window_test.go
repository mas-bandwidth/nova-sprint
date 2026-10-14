package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// setReleaseWindowFile points the app's NOVA_RELEASE_WINDOW_FILE at path, over
// the env the rig gave it.
func (ta *testApp) setReleaseWindowFile(path string) {
	base := ta.a.getenv
	ta.a.getenv = func(k string) string {
		if k == releaseWindowEnv {
			return path
		}
		return base(k)
	}
}

// refusingBackend is a store dial that refuses, as the v1.2.23 adopt's was.
func refusingBackend() func(context.Context, string, sprint.Names) (store.Backend, error) {
	return func(context.Context, string, sprint.Names) (store.Backend, error) {
		return nil, errors.New("the store at 127.0.0.1:6379 did not answer: connection refused")
	}
}

// writeSentinel writes the sentinel's bytes to a file under dir and returns its path.
func writeSentinel(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "release-window.json")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

// TestASeatVerbSaysWhenAReleaseWindowIsOpen: a verb that fails to reach the
// store reads the release-window sentinel once, after the failure, and the
// refusal says whether the outage is the planned window (docs/SPEC-SPRINT.md,
// the release window). Each case takes the clock and the file: a temp file,
// the app's now, a fake dial that refuses; no real time, no socket.
func TestASeatVerbSaysWhenAReleaseWindowIsOpen(t *testing.T) {
	t.Parallel()

	// until is 240 s after the rig's clock t0: the open note says "about 240 s".
	until := t0.Add(240 * time.Second)
	openContent := `{"open":true,"reason":"adopt v1.2.24","opened":"2030-01-02T03:00:00Z","until":"` +
		until.UTC().Format(time.RFC3339) + `","back_in_s":300}`
	const original = "nova-sprint bases REFUSED: the store at 127.0.0.1:6379 did not answer: connection refused; run: nova-sprint bases -h"

	t.Run("open", func(t *testing.T) {
		ta := newTestApp(t)
		ta.setReleaseWindowFile(writeSentinel(t, t.TempDir(), openContent))
		ta.a.backend = refusingBackend()

		code, out, errs := ta.do("bases")
		assert.Equal(t, 2, code)
		assert.Contains(t, errs,
			"RELEASE WINDOW OPEN: adopt v1.2.24; the server is expected back in about 240 s (until "+
				until.UTC().Format("15:04:05")+"Z); this is planned, not a failure; retry then. ")
		assert.Contains(t, errs, original)
		assert.Empty(t, out)
	})

	t.Run("open json", func(t *testing.T) {
		ta := newTestApp(t)
		ta.setReleaseWindowFile(writeSentinel(t, t.TempDir(), openContent))
		ta.a.backend = refusingBackend()

		code, _, errs := ta.do("bases --json")
		assert.Equal(t, 2, code)
		assert.Contains(t, errs, "RELEASE WINDOW OPEN: adopt v1.2.24")
		assert.Contains(t, errs, `"release_window"`)
		assert.Contains(t, errs, `"adopt v1.2.24"`)
		assert.Contains(t, errs, "nova-sprint bases REFUSED: the store at 127.0.0.1:6379 did not answer: connection refused")
	})

	t.Run("stale", func(t *testing.T) {
		stale := `{"open":true,"reason":"adopt v1.2.24","opened":"2030-01-02T03:00:00Z","until":"2030-01-02T03:04:04Z","back_in_s":300}`
		ta := newTestApp(t)
		ta.setReleaseWindowFile(writeSentinel(t, t.TempDir(), stale))
		ta.a.backend = refusingBackend()

		code, _, errs := ta.do("bases")
		assert.Equal(t, 2, code)
		assert.Contains(t, errs, "release window file is stale (until 03:04:04Z passed): the outage may be a failure; run: nova-sprint where")
		assert.NotContains(t, errs, "planned")
		assert.Contains(t, errs, original)
	})

	t.Run("none or unreadable", func(t *testing.T) {
		cases := []struct {
			name    string
			content string
			dir     bool
		}{
			{"no file", "", false},
			{"a directory in its place", "", true},
			{"malformed json", `{"open":`, false},
			{"open false", `{"open":false,"reason":"adopt v1.2.24"}`, false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "release-window.json")
				switch {
				case tc.dir:
					require.NoError(t, os.MkdirAll(path, 0o700))
				case tc.content != "":
					require.NoError(t, os.WriteFile(path, []byte(tc.content), 0o600))
				}
				ta := newTestApp(t)
				ta.setReleaseWindowFile(path)
				ta.a.backend = refusingBackend()

				code, _, errs := ta.do("bases")
				assert.Equal(t, 2, code)
				assert.NotContains(t, errs, "RELEASE WINDOW OPEN")
				assert.NotContains(t, errs, "release window file is stale")
				assert.Contains(t, errs, original)
			})
		}
	})

	t.Run("a verb whose dial succeeds never reads the file", func(t *testing.T) {
		ta := newTestApp(t)
		var reads int64
		ta.a.readReleaseWindowFn = func(string, time.Time) (ReleaseWindow, releaseWindowState) {
			atomic.AddInt64(&reads, 1)
			return ReleaseWindow{}, releaseWindowNone
		}
		ta.ok("init --readers reader-a --members m1")
		ta.ok("bases")
		assert.Equal(t, int64(0), atomic.LoadInt64(&reads), "a verb that reached the store read the sentinel")
	})

	t.Run("the file is never written by a verb", func(t *testing.T) {
		path := writeSentinel(t, t.TempDir(), openContent)
		before, err := os.ReadFile(path)
		require.NoError(t, err)
		beforeMode := fileMode(t, path)

		ta := newTestApp(t)
		ta.setReleaseWindowFile(path)
		ta.a.backend = refusingBackend()
		ta.do("bases")

		after, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, before, after)
		assert.Equal(t, beforeMode, fileMode(t, path))
	})
}

// fileMode is the file's mode for the never-written check.
func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	require.NoError(t, err)
	return fi.Mode()
}

// TestReadReleaseWindowStates pins the sentinel's four states directly.
func TestReadReleaseWindowStates(t *testing.T) {
	t.Parallel()
	now := t0

	dir := t.TempDir()
	path := filepath.Join(dir, "rw.json")

	// no file: none
	_, st := ReadReleaseWindow(filepath.Join(dir, "absent.json"), now)
	assert.Equal(t, releaseWindowNone, st)

	// a directory in its place: unreadable
	require.NoError(t, os.MkdirAll(path, 0o700))
	_, st = ReadReleaseWindow(path, now)
	assert.Equal(t, releaseWindowUnreadable, st)

	// malformed JSON: unreadable
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.WriteFile(path, []byte(`{"open":`), 0o600))
	_, st = ReadReleaseWindow(path, now)
	assert.Equal(t, releaseWindowUnreadable, st)

	// open false: none
	require.NoError(t, os.WriteFile(path, []byte(`{"open":false,"reason":"adopt v1.2.24"}`), 0o600))
	_, st = ReadReleaseWindow(path, now)
	assert.Equal(t, releaseWindowNone, st)

	// open, now before until: open
	until := now.Add(time.Minute)
	require.NoError(t, os.WriteFile(path, []byte(`{"open":true,"reason":"adopt v1.2.24","until":"`+until.UTC().Format(time.RFC3339)+`"}`), 0o600))
	w, st := ReadReleaseWindow(path, now)
	assert.Equal(t, releaseWindowOpen, st)
	assert.Equal(t, "adopt v1.2.24", w.Reason)
	assert.True(t, strings.HasPrefix(w.Until, "2030"))

	// open, now after until: stale
	past := now.Add(-time.Minute)
	require.NoError(t, os.WriteFile(path, []byte(`{"open":true,"reason":"adopt v1.2.24","until":"`+past.UTC().Format(time.RFC3339)+`"}`), 0o600))
	_, st = ReadReleaseWindow(path, now)
	assert.Equal(t, releaseWindowStale, st)
}
