package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// An alarm is never off, through the verb (the owner, 2026-10-11: "Turning off alarms,
// warnings is forbidden from now on."): set --alarm-review off exits 1 with the rule and
// writes nothing, and the review alarm still fires when a card goes over its threshold.
func TestSetAlarmOffIsRefusedAndTheAlarmStillFires(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1:2")
	ta.ok("set --alarm-review 0") // raised once one primary is in review

	code, out, errs := ta.do("set --alarm-review off")
	require.Equal(t, 1, code, "set --alarm-review off exits 1\n%s%s", out, errs)
	assert.Contains(t, out+errs, "--alarm-review off is refused")
	assert.Contains(t, out+errs, sprint.AlarmOffRefused)
	for _, flag := range []string{"--alarm-ready", "--alarm-merging", "--alarm-fleet"} {
		code, out, errs := ta.do("set " + flag + " off")
		assert.Equal(t, 1, code, "set %s off exits 1\n%s%s", flag, out, errs)
	}

	ta.ok("add --stream s1 --one --brief-file " + proBrief(t, "s1-1"))
	ta.ok("start")
	ta.ok("tick")
	ta.ok("tick")
	ta.ok("take --as m1 --limit 1")
	ta.ok("finish --as m1 s1-1.w1@1 --report done")
	ta.ok("tick")
	ta.ok("tick")
	assert.Contains(t, ta.ok("inbox"), sprint.NAlarmReview, "the review alarm fires: its threshold 0 stands, the refused off changed nothing")
}
