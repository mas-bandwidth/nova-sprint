package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A card whose attempts keep ending on the environment (an exit 134 with no result: the work
// card withdrawn and dealt to the same member again, no attempt spent) is blocked one past the
// flap limit, with one judgment, and is dealt nothing more; unblock takes it to waiting, and
// the ticks after deal it again. On the in-memory twin, the clock stepped by hand.
func TestACardEndingOnTheEnvironmentIsBlockedThenDealtAgainAfterUnblock(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.live = []string{"m1"}
	ta.ok("init --readers reader-a --members m1")
	ta.ok("set --flap-limit 1 --flap-window 1h")
	ta.ok("add --stream s1 --count 1 --one")
	ta.ok("start")
	ta.ok("tick")
	ta.ok("tick")
	card := func() (col, work string) {
		var v struct {
			Column  string       `json:"column"`
			Primary *sprint.Card `json:"primary"`
		}
		require.NoError(t, json.Unmarshal([]byte(ta.ok("card s1-1 --json")), &v))
		return v.Primary.Col, v.Primary.F("work")
	}
	envExit := func() string {
		t.Helper()
		var q struct {
			Cards []struct {
				ID  string `json:"id"`
				Gen int    `json:"gen"`
			} `json:"cards"`
		}
		require.NoError(t, json.Unmarshal([]byte(ta.ok("queue --as m1 --json")), &q))
		require.Len(t, q.Cards, 1, "s1-1 is dealt")
		ta.ok("take --as m1 --limit 1")
		ta.ok("finish --as m1 " + q.Cards[0].ID + "@" + strconv.Itoa(q.Cards[0].Gen) + " --failed --exit 134 --report 'no result: aborted' --harness 'Aborted (core dumped)'")
		ta.step(time.Minute)
		return ta.ok("tick")
	}
	for i := 0; i < 1; i++ {
		out := envExit()
		col, _ := card()
		require.NotEqual(t, sprint.Blocked, col, "at the limit after %d: %s", i+1, out)
	}
	out := envExit()
	assert.Contains(t, out, "s1-1 working -> blocked (2 flaps in 1h0m0s: no-verdict=2)")
	col, work := card()
	require.Equal(t, sprint.Blocked, col)
	assert.Empty(t, work)

	blocked := ta.ok("blocked")
	assert.Contains(t, blocked, "BLOCKED s1-1 stream=s1 from=working flaps=2 no-verdict=2")
	assert.Contains(t, blocked, "BLOCKED OK cards=1")
	ta.step(time.Hour)
	ta.ok("tick")
	ta.ok("tick")
	col, _ = card()
	assert.Equal(t, sprint.Blocked, col, "no timeout, no automatic exit")
	timeline := ta.ok("card s1-1")
	assert.Equal(t, 1, strings.Count(timeline, "judgment: a primary is blocked: it flapped: blocked:"), "one judgment: %s", timeline)
	assert.NotContains(t, timeline, "an invariant is broken")
	assert.Contains(t, timeline, "unblock: nova-sprint unblock s1-1 --reason")

	code, _, errs := ta.do("unblock s1-1")
	assert.NotZero(t, code)
	assert.Contains(t, errs, "--reason")
	assert.Contains(t, ta.ok("unblock s1-1 --reason 'the env is fixed' --dry-run"), "s1-1 blocked -> waiting")
	col, _ = card()
	require.Equal(t, sprint.Blocked, col, "a dry run writes nothing")
	ta.ok("unblock s1-1 --reason 'the env is fixed'")
	// the machine runs: the move is queued, and the next tick drains it to waiting, resolves and deals
	ta.ok("tick")
	ta.ok("tick")
	col, work = card()
	assert.Equal(t, sprint.Working, col, "dealt again")
	assert.NotEmpty(t, work)
	assert.Contains(t, ta.ok("blocked"), "BLOCKED OK cards=0")
}

// A block is pushed once, as a message of its own and never in the digest; a second look,
// with the judgment still open, sends nothing more.
func TestABlockIsPushedOncePerEntry(t *testing.T) {
	t.Parallel()
	f := sprint.Flaps{Total: 6, ByKind: map[string]int{sprint.FlapNoVerdict: 6}, Last: "2030-01-02T03:04:05Z no-verdict s1-1.w1 working -> ready (m1)"}
	n := sprint.Note{ID: "n1", Kind: sprint.Judgment, Type: sprint.NFlapBlocked, Card: "s1-1", Stream: "s1", At: t0, What: sprint.BlockedWhat("s1-1", f, 30*time.Minute)}
	l := coordLook{holder: "seat", groups: []sprint.Group{{ID: "g1", Kind: sprint.Judgment, Type: sprint.NFlapBlocked, Notes: []string{"n1"}}},
		open: []sprint.Open{{Key: sprint.OpenKey("n1", "s1-1"), Note: n}}}
	told := map[string]bool{}
	sends := coordFresh(told, l, t0)
	require.Len(t, sends, 1)
	assert.Equal(t, "BLOCKED s1-1: 6 flaps in 30m0s", sends[0].subject)
	assert.Equal(t, "seat", sends[0].to)
	assert.Contains(t, sends[0].text, "flaps: no-verdict=6")
	for _, k := range sends[0].keys {
		told[k] = true
	}
	assert.Empty(t, coordFresh(told, l, t0.Add(time.Hour)), "one push per entry")
}
