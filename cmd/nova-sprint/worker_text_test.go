package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The worker's verbs answer a cold reader in one turn (docs/STANDARD.md section
// 3, a refusal names the next command; section 2, a verb's -h quotes the table):
// a refusal that wants a generation names the queue command that mints it, a
// finish refuses a --head that is not a commit before the card moves, and each
// usage line names every flag a form needs (docs/SPEC-SPRINT.md section 2,
// "Reads are priced like work", for read's --usage).
func TestWorkerRefusalsPrintTheQueueCommandAndFinishChecksTheHeadAndUsageHasEveryFlag(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)
	ta.ok("start")
	ta.ok("take --as m1 s1-1.w1@1")

	// a take that wants <card>@<gen> names the command that mints the generation
	code, out, errs := ta.do("take --as m1 s1-1")
	require.Equal(t, 2, code)
	assert.Empty(t, out)
	assert.Contains(t, errs, "nova-sprint queue --as m1 --json", "a take that wants <card>@<gen> names the command that mints it")

	// a finish --head that is not a commit is refused at exit 2, before the
	// card moves out of working
	before := ta.applies()
	code, _, errs = ta.do("finish --as m1 s1-1.w1@1 --head notacommit")
	require.Equal(t, 2, code)
	assert.Contains(t, errs, "notacommit", "the refusal names the value it found")
	assert.Equal(t, before, ta.applies(), "a finish refused for its --head wrote to the store")

	// the finish usage names --branch, the line the packet's report carries
	assert.Contains(t, usageLine(ta.ok("finish -h")), "--branch", "the finish usage names --branch")

	// read's help says a priced (routed) read needs its usage report, and shows
	// a routed example with --usage; an unpriced read needs none
	readHelp := ta.ok("read -h")
	assert.Contains(t, readHelp, "priced (routed) read needs --usage", "read's help says a priced read needs --usage")
	assert.Contains(t, readHelp, "--ok s1-1.r1.reader-a --usage", "read's help shows a routed example with --usage")

	// stop's optional --until is bracketed, as its syntax is optional
	assert.Contains(t, usageLine(ta.ok("stop -h")), "[--until", "stop's usage brackets its optional --until")
}

// usageLine is a verb's usage line: the first line its -h prints.
func usageLine(help string) string {
	line, _, _ := strings.Cut(help, "\n")
	return line
}
