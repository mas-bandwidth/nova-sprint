//go:build functional

package main

import (
	"fmt"
	"os"
	"testing"

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
