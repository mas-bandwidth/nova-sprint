package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGlobalFlagsBeforeVerb pins card sprint-accepts-global-flags-before-verb.
func TestGlobalFlagsBeforeVerb(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	run := func(args ...string) (int, string, string) {
		var out, errb bytes.Buffer
		code := ta.a.run(args, &out, &errb)
		return code, out.String(), errb.String()
	}
	c1, o1, e1 := run("where", "--redis", "mem:0")
	c2, o2, e2 := run("--redis", "mem:0", "where")
	assert.NotContains(t, e2, "unknown verb")
	assert.Equal(t, c1, c2)
	assert.Equal(t, o1, o2)
	assert.Equal(t, e1, e2)
	// equals form also hoists
	c3, _, e3 := run("--redis=mem:0", "where")
	assert.NotContains(t, e3, "unknown verb")
	assert.Equal(t, c1, c3)
	_ = require.New(t)
}
