package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/internal/subproc"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/filelock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The clone lock fences the public land path before any git or store report
// (docs/SPEC-SPRINT.md, land-one-lander-now-ns.w1).
func TestLandRefusesWhileAnotherLanderHoldsTheClone(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.ok("add --stream s1 --count 1 --one")
	r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "change.txt", "change\n")}, "s1-1")
	before, applies := r.git(r.remote, "rev-parse", "main"), r.applies()
	held, err := filelock.TryLock(r.clone+".land.lock", "nova-sprint run --land")
	require.NoError(t, err)
	defer func() { require.NoError(t, held.Unlock()) }()
	code, _, errs := r.do("land --repo-dir " + r.clone + " --base main")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, fmt.Sprintf("pid=%d", os.Getpid()))
	assert.Contains(t, errs, "run --land")
	assert.Equal(t, before, r.git(r.remote, "rev-parse", "main"))
	assert.Equal(t, applies, r.applies())
	r.ok("land --repo-dir " + r.clone + " --base main --dry-run")
	require.NoError(t, held.Unlock())
	r.ok("land --repo-dir " + r.clone + " --base main")
	assert.NotEqual(t, before, r.git(r.remote, "rev-parse", "main"))
}

func TestLandHoldsTheCloneAcrossNestedPasses(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.ok("add --stream s1 --count 1 --one")
	r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "change.txt", "change\n")}, "s1-1")
	called := false
	r.a.beforePush = func(int) {
		if called {
			return
		}
		called = true
		code, _, errs := r.do("land --repo-dir " + r.clone + " --base main")
		assert.Equal(t, 1, code)
		assert.Contains(t, errs, "lock held")
	}
	r.ok("land --repo-dir " + r.clone + " --base main")
	assert.True(t, called, "second pass runs before the first pass pushes")
	held, err := filelock.TryLock(r.clone+".land.lock", "after pass")
	require.NoError(t, err, "the public pass releases its clone on return")
	require.NoError(t, held.Unlock())
}

func TestLandCloneAliasesShareALockAndDryRunsDoNotTouchIt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	real := filepath.Join(root, "real")
	alias := filepath.Join(root, "alias")
	require.NoError(t, os.Mkdir(real, 0o755))
	require.NoError(t, os.Symlink(real, alias))
	first := &lander{a: &app{}}
	defer func() { require.NoError(t, first.releaseClones()) }()
	dir, why := first.holdClone(filepath.Join(real, "missing", "clone"))
	require.Empty(t, why)
	second := &lander{a: &app{}}
	_, why = second.holdClone(filepath.Join(alias, "missing", "clone"))
	assert.Contains(t, why, "lock held")
	stamp, err := filelock.ReadStamp(dir + ".land.lock")
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), stamp.PID)
	assert.Equal(t, "nova-sprint land", stamp.Label)
	dry := &lander{a: &app{}, dry: true}
	_, why = dry.holdClone(filepath.Join(alias, "missing", "clone"))
	require.Empty(t, why)
	after, err := filelock.ReadStamp(dir + ".land.lock")
	require.NoError(t, err)
	assert.Equal(t, stamp, after, "dry run and rejected contender cannot erase a live owner")
	untouched := filepath.Join(root, "dry-only", "clone")
	_, why = dry.holdClone(untouched)
	require.Empty(t, why)
	assert.NoDirExists(t, filepath.Dir(untouched))
	require.NoError(t, first.releaseClones())
	_, why = second.holdClone(filepath.Join(alias, "missing", "clone"))
	require.Empty(t, why)
	require.NoError(t, second.releaseClones())
}

func TestLandTakesOverAndLogsADeadHoldersStamp(t *testing.T) {
	t.Parallel()
	child := subproc.Prepare(context.Background(), time.Minute, "sh", "-c", "exit 0")
	defer child.Cancel()
	require.NoError(t, child.Cmd.Run())
	dead := child.Cmd.Process.Pid
	dir := filepath.Join(t.TempDir(), "clone")
	stamp := filelock.Stamp{PID: dead, Label: "nova-sprint land"}
	require.NoError(t, os.WriteFile(dir+".land.lock", []byte(stamp.Format()), 0o600))
	var log bytes.Buffer
	pass := &lander{a: &app{}, lockLog: &log}
	_, why := pass.holdClone(dir)
	require.Empty(t, why)
	assert.Contains(t, log.String(), "LAND LOCK RECOVERED")
	assert.Contains(t, log.String(), fmt.Sprintf("pid=%d", dead))
	require.NoError(t, pass.releaseClones())
	data, err := os.ReadFile(dir + ".land.lock")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(data)))
}
