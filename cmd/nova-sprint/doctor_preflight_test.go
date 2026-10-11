package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// doctor --preflight <candidate> is the release gate's frame: before any switch or
// adopt it runs the candidate's version and its tick --shadow --json against the
// store this doctor reads (docs/SPEC-SPRINT.md section 14, "The server"). A
// candidate whose shadow refuses the store's schema (a locked column missing) makes
// it exit 1 with the shadow's own text; a candidate whose shadow passes exits 0.
// The candidates are fake scripts written the way the package's server switch tests
// write them (candidateAt, under the fork lock).

// a candidate whose shadow prints SHADOW TICK FAILED with a locked-shape error makes
// doctor --preflight exit 1 with that text.
func TestDoctorPreflightRefusesAStoreMissingTheLockedColumns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	text := "fleet is missing finished, redealt; run on a STOPPED machine, before this build is installed (internal/sprint/TABLES.lock): " +
		"nova-table col add fleet finished --after load; nova-table col set fleet --hide finished; nova-table col add fleet redealt --after load"
	body := fmt.Sprintf("if [ \"$1\" = tick ]; then echo 'nova-sprint tick --shadow: %s; run: nova-sprint tick -h' >&2; exit 2; fi", text)
	candidate := candidateAt(t, filepath.Join(dir, "locked"), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", body)

	ta := newTestApp(t)
	code, out, errs := ta.do("doctor --preflight " + candidate)
	assert.Equal(t, 1, code, "a candidate whose shadow refuses the schema fails the gate: %s%s", out, errs)
	all := out + errs
	assert.Contains(t, all, "DOCTOR FAIL schema", all)
	assert.Contains(t, all, "fleet is missing finished, redealt", all)
	assert.Contains(t, all, "nova-table col add fleet finished --after load", all)
	assert.Contains(t, all, "run the commands above on a STOPPED machine, or the adopt's schema step", all)
}

// a candidate whose shadow passes exits 0, its schema row OK naming its version, and
// the gate's later checks are four WARN rows saying this build does not measure them.
func TestDoctorPreflightPassesAWorkingCandidate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := `if [ "$1" = tick ]; then echo '{"shadow":{"epoch":0,"state":"RUNNING","parts":[],"size":0,"took_ns":1}}'; exit 0; fi`
	candidate := candidateAt(t, filepath.Join(dir, "working"), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", body)

	ta := newTestApp(t)
	code, out, errs := ta.do("doctor --preflight " + candidate)
	require.Equal(t, 0, code, "a working candidate passes the gate: %s%s", out, errs)
	assert.Contains(t, out, "DOCTOR OK schema v9.9.9", out)
	var warns int
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, "DOCTOR WARN ") {
			warns++
		}
	}
	assert.Equal(t, 4, warns, "the gate's four later checks are WARN rows: %s", out)
	for _, name := range []string{"members", "mirrors", "links", "stable"} {
		assert.Contains(t, out, "DOCTOR WARN "+name, name)
	}
}
