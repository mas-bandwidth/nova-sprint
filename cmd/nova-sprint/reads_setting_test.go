package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// set --reads (sprint.PropReadsNeeded): where says "reads: <n>" only while a count is set,
// and where --json carries reads_needed, the count or "default", always.
func TestWhereShowsTheReadsSettingOnlyWhileSet(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	readsOf := func() any {
		var top map[string]any
		require.NoError(t, json.Unmarshal([]byte(ta.ok("where --json")), &top))
		return top["reads_needed"]
	}
	assert.NotContains(t, ta.ok("where"), "reads: ")
	assert.Equal(t, "default", readsOf())

	assert.Contains(t, ta.ok("set --reads 1"), "reads-needed 1")
	assert.Contains(t, ta.ok("where"), "reads: 1\n")
	assert.Equal(t, float64(1), readsOf())

	assert.Contains(t, ta.ok("set --reads 0"), "reads-needed 0")
	assert.Contains(t, ta.ok("where"), "reads: 0\n")
	assert.Equal(t, float64(0), readsOf())

	assert.Contains(t, ta.ok("set --reads 3"), "reads-needed 3")
	assert.Contains(t, ta.ok("where"), "reads: 3\n")
	assert.Equal(t, float64(3), readsOf())

	assert.Contains(t, ta.ok("set --reads 5"), "reads-needed 5")
	assert.Contains(t, ta.ok("where"), "reads: 5\n")
	assert.Equal(t, float64(5), readsOf())

	code, _, errs := ta.do("set --reads 6")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errs, "--reads wants 0, 1, 2, 3, 4, 5 or default")

	assert.Contains(t, ta.ok("set --reads default"), "reads-needed default")
	assert.NotContains(t, ta.ok("where"), "reads: ")
	assert.Equal(t, "default", readsOf())
}

// set --read-mix (sprint.PropReadMix): a mix of friends and fleet reads, at most five
// together; default is the working mix friends=2,fleet=3 and off takes it off.
func TestSetReadMix(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	assert.Contains(t, ta.ok("set --read-mix friends=2,fleet=2"), "read-mix friends=2,fleet=2")
	assert.Contains(t, ta.ok("settings"), "friends=2,fleet=2")
	assert.Contains(t, ta.ok("set --read-mix default"), "read-mix default (friends=2,fleet=3")
	assert.Contains(t, ta.ok("settings"), "friends=2,fleet=3")
	assert.Contains(t, ta.ok("set --read-mix off"), "read-mix off")
	for _, bad := range []string{"friends=3,fleet=3", "friends=0,fleet=0", "friends=x", "friends=1,friends=1", "crowd=2"} {
		code, _, errs := ta.do("set --read-mix " + bad)
		assert.NotEqual(t, 0, code, bad)
		assert.Contains(t, errs, "--read-mix wants", bad)
	}
}
