package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// heldMember is a member's hold as where --json --cards shows it, ok false when it is not
// held.
func (ta *testApp) heldMember(m string) (sprint.HoldView, bool) {
	ta.t.Helper()
	var v struct {
		Holds []sprint.HoldView `json:"holds"`
	}
	ta.json("where --cards", &v)
	for _, h := range v.Holds {
		if h.Kind == sprint.HoldMember && h.Name == m {
			return h, true
		}
	}
	return sprint.HoldView{}, false
}

// A hold's judgment cause is the judgment of its own epoch (the cold read of #206): the
// alias j<n> of epoch E names nothing at E+1 and its note id is open no more, so neither is
// taken as the cause of a hold placed at E+1; the alias of E+1's judgment is taken, the
// hold stands while that judgment is open, and the tick ends it once it is answered.
func TestAJudgmentOfAnEarlierEpochIsNoCauseOfAHold(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 2")
	ta.deal(2)
	ta.ok("take --as m1 --limit 2")
	ta.ok("finish --as m1 s1-1.w1@1 --failed --report 'red'")
	old := ta.group(sprint.NWorkFailed, "s1")
	require.NotEmpty(t, old.Alias)

	ta.clearWithReturns(0, "m1", "s1-2.w1@1")

	code, _, errs := ta.do("hold m1 --indefinite --reason 'looking' --cause " + old.Alias)
	assert.NotEqual(t, 0, code, "the old epoch's alias was taken: %s", errs)
	assert.Contains(t, errs, "epoch")
	code, _, errs = ta.do("hold m1 --indefinite --reason 'looking' --cause " + old.Notes[0])
	assert.NotEqual(t, 0, code, "the old epoch's note was taken: %s", errs)
	assert.Contains(t, errs, "names no open judgment")
	_, held := ta.heldMember("m1")
	require.False(t, held, "a refused hold held m1")

	// a judgment of the new epoch: its alias may be the old one's text, and names it alone
	ta.timerOnce("look-again")
	now := ta.group(sprint.NTimer, "")
	require.NotEqual(t, old.Notes[0], now.Notes[0])
	ta.ok("hold m1 --indefinite --reason 'looking' --cause " + now.Alias)
	h, held := ta.heldMember("m1")
	require.True(t, held)
	assert.Equal(t, now.Alias, h.Cause)

	ta.ok("start")
	ta.ok("tick")
	_, held = ta.heldMember("m1")
	require.True(t, held, "the hold ended while its cause is open")

	ta.ok("ack " + now.Alias + " --reason 'looked again'")
	ta.ok("tick")
	_, held = ta.heldMember("m1")
	assert.False(t, held, "the hold outlived its answered cause")
}
