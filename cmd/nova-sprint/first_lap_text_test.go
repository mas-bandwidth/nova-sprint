package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Init names the stopped state, start's help names the clean card-check
// prerequisite and the twin exemption, and add's help names the hook as
// step 0 (docs/SPEC-SPRINT.md section 14, "The card check"; "The hook").
func TestInitStartAndAddNameTheStoppedStateTheHookGateAndTheCardCheck(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	initOut := ta.ok("init --readers reader-a --members m1")
	assert.Contains(t, initOut, "run: nova-sprint start", "init:\n%s", initOut)

	a := newApp(func(string) string { return "" })
	help := func(verb string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := runInto(a, []string{"help", verb}, &out, &errb)
		require.Equal(t, 0, code, "help %s: %s", verb, errb.String())
		return out.String()
	}

	start := help("start")
	assert.Contains(t, start, "clean check cards", "help start names the live prerequisite:\n%s", start)
	assert.True(t, strings.Contains(start, "mem:<file>") && strings.Contains(start, "does not"),
		"help start names the twin exemption:\n%s", start)

	add := help("add")
	assert.Contains(t, add, "step 0", "help add names the hook as step 0:\n%s", add)
	assert.Contains(t, add, "You must hook in first", "help add names the refusal line:\n%s", add)
	assert.Contains(t, add, "--redis mem:<file>", "help add names the twin that skips the hook:\n%s", add)
}
