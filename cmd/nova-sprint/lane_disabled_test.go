package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The bench lock is disabled until v1.3 (Glenn, 2026-10-13: "Stop with these bench
// locks. Verboten." "Disable until then. Not a sanctioned or stable feature. Not to be
// used."; sprint.LanesDisabled). Three takes of one machine's one Go lane, each with a
// --wait, are all granted at once on the test's clock: none waits, none queues, each
// prints the one DISABLED line, a give gives nothing back, and lane list finds no record.
//
// Not parallel: it sets sprint.LanesDisabled, which TestMain clears for the lanes' own
// tests, and a parallel test runs only after every sequential one has returned.
func TestTheBenchLockIsDisabledUntilV13(t *testing.T) {
	sprint.LanesDisabled = true
	defer func() { sprint.LanesDisabled = false }()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1,m2")
	began := ta.a.now()
	for _, who := range []string{"a", "b", "c"} {
		code, out, errs := ta.do("lane take go --machine m1 --as " + who + " --wait 30m")
		require.Equal(t, 0, code, errs)
		assert.Contains(t, out, "LANE-TAKE OK go machine=m1 as="+who+" disabled=yes")
		assert.Equal(t, sprint.LanesDisabledLine+"\n", errs, "one line, naming the v1.3 card")
	}
	assert.Equal(t, began, ta.a.now(), "no take waited on the clock")
	code, out, errs := ta.do("lane give go --machine m1 --as a")
	require.Equal(t, 0, code, errs)
	assert.Contains(t, out, "gave=no disabled=yes")
	assert.Equal(t, sprint.LanesDisabledLine+"\n", errs)
	assert.Contains(t, ta.ok("lane list"), "LANE-LIST OK machines=0", "no lane record was written")
	assert.Contains(t, sprint.LanesDisabledLine, "v1.3 card the-server-schedules-bench-work-not-a-lock")
}
