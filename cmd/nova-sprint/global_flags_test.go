package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGlobalFlagsBeforeVerb pins card sprint-accepts-global-flags-before-verb.
func TestGlobalFlagsBeforeVerb(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	c1, o1, e1 := ta.do("where --redis mem:0")
	c2, o2, e2 := ta.do("--redis mem:0 where")
	assert.NotContains(t, e2, "unknown verb")
	assert.Equal(t, c1, c2)
	assert.Equal(t, o1, o2)
	assert.Equal(t, e1, e2)
	c3, _, e3 := ta.do("--redis=mem:0 where")
	assert.NotContains(t, e3, "unknown verb")
	assert.Equal(t, c1, c3)
}
