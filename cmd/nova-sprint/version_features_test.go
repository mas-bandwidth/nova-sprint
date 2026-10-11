package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/docs"
)

// versionTestBuild is a release build with a fixed version string: the version
// rules are pure over the version the version line prints, so a test fixes it
// here rather than rebuilding a binary.
const versionTestBuild = "v1.2.9"

// versionRun is versionCommand over a fixed build version with its two streams
// captured: the shape every version test reads.
func versionRun(t *testing.T, build string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := versionCommand(args, build, &out, &errb)
	return code, out.String(), errb.String()
}

// A fix of a release at or below the build's version is carried: --has exits 0.
func TestVersionHasNamesAnEarlierRelease(t *testing.T) {
	t.Parallel()
	code, out, errb := versionRun(t, versionTestBuild, "--has", "lua-digest-restore")
	require.Equal(t, 0, code, "--has an earlier fix: exit %d\n%s%s", code, out, errb)
	assert.Contains(t, out, "v1.2.4 lua-digest-restore", "--has prints the fix: %s", out)
}

// A fix of a release past the build's version is not carried: --has exits 1 and
// names the later release.
func TestVersionHasNamesALaterRelease(t *testing.T) {
	t.Parallel()
	code, out, errb := versionRun(t, versionTestBuild, "--has", "inbox-push-write-once-race")
	require.Equal(t, 1, code, "--has a later fix: exit %d\n%s%s", code, out, errb)
	assert.Contains(t, errb, "v1.2.11", "the later release is named: %s", errb)
}

// --features lists every carried fix and no fix of a later release.
func TestVersionFeaturesListsNoLaterRelease(t *testing.T) {
	t.Parallel()
	code, out, errb := versionRun(t, versionTestBuild, "--features")
	require.Equal(t, 0, code, "--features: exit %d\n%s%s", code, out, errb)
	assert.Contains(t, out, "v1.2.4 lua-digest-restore", "--features lists an earlier fix: %s", out)
	for _, later := range []string{"inbox-push-write-once-race", "a-red-base-is-pushed-to-the-coordinator"} {
		assert.NotContains(t, out, later, "--features lists no fix of a later release: %s", out)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		release, _, _ := strings.Cut(line, " ")
		assert.LessOrEqual(t, release, versionTestBuild, "a listed release is at or below the build: %q", line)
	}
}

// A dev build carries the shipped releases' fixes and says the unreleased fixes
// of its tree are not listed.
func TestVersionFeaturesOnADevBuild(t *testing.T) {
	t.Parallel()
	code, out, errb := versionRun(t, "devel", "--features")
	require.Equal(t, 0, code, "--features on a dev build: exit %d\n%s%s", code, out, errb)
	assert.Contains(t, out, "and the unreleased fixes of the tree it was built from: not listed",
		"a dev build says the unreleased fixes are not listed: %s", out)
	assert.NotContains(t, out, "inbox-push-write-once-race", "a dev build lists no unreleased fix: %s", out)
}

// The compiled ledger is the file: a stale embed cannot ship.
func TestVersionEmbeddedLedgerEqualsTheFile(t *testing.T) {
	t.Parallel()
	onDisk, err := os.ReadFile("../../docs/fixes.sexp")
	require.NoError(t, err, "read docs/fixes.sexp")
	assert.Equal(t, string(onDisk), string(docs.FixesSexp), "the embedded fixes ledger equals docs/fixes.sexp")
}
