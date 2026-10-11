package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// failedOnce is a sprint with s1-1's work come back failed: its judgment group.
func failedOnce(t *testing.T) (*testApp, sprint.Group) {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)
	ta.failOnce("m1", "s1-1.w1@1", "tests red")
	return ta, ta.group(sprint.NWorkFailed, "s1")
}

// wait --group on failed work is refused, naming the action instead, and sets nothing
// (internal/sprint/wait_refused.go).
func TestWaitByGroupIsRefusedOnFailedWork(t *testing.T) {
	t.Parallel()
	ta, g := failedOnce(t)
	code, out, errs := ta.do("wait --group " + g.ID + " --expect 1 --for 30m")
	assert.NotEqual(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, errs, "WAIT REFUSED note=", errs)
	assert.Contains(t, errs, "holds its card", errs)
	assert.Contains(t, errs, "rework", errs)
	assert.NotContains(t, out, "WAIT OK", out)
	require.False(t, ta.group(sprint.NWorkFailed, "s1").Quiet, "no review time was set")
}

// wait j<n> on failed work is refused the same way.
func TestWaitByAliasIsRefusedOnFailedWork(t *testing.T) {
	t.Parallel()
	ta, g := failedOnce(t)
	require.True(t, sprint.IsAlias(g.Alias), g.Alias)
	code, out, errs := ta.do("wait " + g.Alias + " --for 30m")
	assert.NotEqual(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, errs, "WAIT REFUSED note=", errs)
	assert.Contains(t, errs, "holds its card", errs)
	assert.NotContains(t, out, "WAIT OK", out)
	require.False(t, ta.group(sprint.NWorkFailed, "s1").Quiet, "no review time was set")
}
