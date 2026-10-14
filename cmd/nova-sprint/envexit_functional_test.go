//go:build functional

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
)

// TestAnEnvironmentExitOnARealStore drives the verbs against a private redis-server with the
// machine's own clock (docs/SPEC-SPRINT.md, "An environment exit"): the member m1 finishes
// eight of ten cards with a verdict, then two as environment exits (a 134 under the landlock
// sandbox, and a no-result at exit 0, a second later). The tick that drains each puts it back
// in ready at the same attempt with `ended=environment exit=<n>` and the harness tail on its
// work card (the 134 with no redeal spent, the no-result an ended take as the owner's rules
// say), and deals the same work card to m1 again; the doctor and stats --children name the
// rate from the attempt records on the store.
//
// A scrubbed environment: the app sees only the variables the test sets (the actor, a home
// and config directory of its own; no NOVA_SPRINT_SERVER, no NOVA_SPRINT_REDIS, no seat
// record), every verb names the private store with --redis, which never forwards
// (forward.go), and the app's forward fails the test: no verb can reach a live sprint
// server. The doctor's outside is the test's own, so it reaches no host.
func TestAnEnvironmentExitOnARealStore(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(context.Background(), c))
	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_ACTOR": "coordinator", "HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	a := newApp(func(k string) string { return env[k] })
	defer a.close()
	a.home = func() (string, error) { return home, nil }
	a.forward = func(_ context.Context, addr string, verbs ...[]string) ([]sprintwire.Result, error) {
		t.Fatalf("a verb was forwarded to a sprint server at %s: %q", addr, verbs)
		return nil, errors.New("forwarding is refused in this test")
	}
	require.Empty(t, func() string { s, _ := a.namedServer(); return s }(), "no sprint server is named in the test's environment")
	a.outside = doctorOutside(&testApp{t: t, a: a}, []string{"m1"}, nil)
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := runOnStore(a, addr, append(args, "--redis", addr), &out, &errb)
		return code, out.String(), errb.String()
	}
	ok := func(args ...string) string {
		t.Helper()
		code, out, errs := run(args...)
		require.Zero(t, code, "%v: exit %d\n%s%s", args, code, out, errs)
		return out
	}
	tick := func() string {
		t.Helper()
		ok("fleet", "beat", "m1")
		return ok("tick")
	}

	ok("init", "--readers", "reader-a", "--members", "m1")
	ok("add", "--stream", "s1", "--count", "10")
	ok("start")
	tick() // m1 comes up
	tick() // and is dealt the cards
	var q struct {
		Epoch uint64 `json:"epoch"`
		Cards []struct {
			ID      string `json:"id"`
			Primary string `json:"primary"`
			Attempt int    `json:"attempt"`
			Gen     int    `json:"gen"`
		} `json:"cards"`
	}
	require.NoError(t, json.Unmarshal([]byte(ok("queue", "--as", "m1", "--json")), &q))
	require.Len(t, q.Cards, 10, "m1's queue")
	epoch := strconv.FormatUint(q.Epoch, 10)
	ok("take", "--as", "m1", "--limit", "100", "--epoch", epoch)
	for _, c := range q.Cards[:8] {
		ok("finish", "--as", "m1", c.ID+"@"+strconv.Itoa(c.Gen), "--epoch", epoch, "--head", c.ID)
	}
	tick() // a finish is queued until the next tick drains it
	env134, env0 := q.Cards[8], q.Cards[9]
	ok("finish", "--as", "m1", env134.ID+"@"+strconv.Itoa(env134.Gen), "--epoch", epoch, "--failed", "--exit", "134",
		"--report", "no result: aborted", "--harness", "an earlier tail\nAborted (core dumped)")
	assert.Contains(t, tick(), env134.ID+" working -> withdrawn gen=2, environment exit=134 ended on m1's environment (no attempt spent)")
	ok("finish", "--as", "m1", env0.ID+"@"+strconv.Itoa(env0.Gen), "--epoch", epoch, "--failed", "--exit", "0",
		"--report", "no result: no RESULT.md shape", "--harness", "Error: Unknown: ChildProcess.spawn (opencode)\nAborted")
	assert.Contains(t, tick(), env0.ID+" working -> withdrawn gen=2, the child left no result; "+env0.Primary+" working -> ready")

	// each environment exit is on its work card, dealt again to m1 at the same attempt; its
	// primary is working on it again at attempt 1, never failed, with the take's record ending
	// on the environment and the tail on it. The 134 spent no redeal; the no-result at exit 0
	// is still an ended take, its redeal counted (the owner's rules for such a take)
	for _, c := range []struct{ id, primary, ended, end, redeals, tail string }{
		{env134.ID, env134.Primary, "environment exit=134", "environment exit=134", "", "an earlier tail\nAborted (core dumped)"},
		{env0.ID, env0.Primary, "environment exit=0", "no result environment exit=0", "1", "Error: Unknown: ChildProcess.spawn (opencode)\nAborted"},
	} {
		var view struct {
			Primary *sprint.Card        `json:"primary"`
			Column  string              `json:"column"`
			Work    []*sprint.Card      `json:"work_cards"`
			Cost    sprint.CardCostView `json:"cost"`
		}
		require.NoError(t, json.Unmarshal([]byte(ok("card", c.primary, "--json")), &view), c.primary)
		require.NotNil(t, view.Primary, c.primary)
		assert.Equal(t, sprint.Working, view.Primary.Col, c.primary)
		assert.Equal(t, "1", view.Primary.Fields["attempt"], "%s: the same attempt", c.primary)
		assert.Empty(t, view.Primary.Fields["failed"], "%s: never failed work", c.primary)
		assert.Equal(t, c.tail, view.Primary.Fields[sprint.FieldHarnessTail], c.primary)
		var wc *sprint.Card
		for _, w := range view.Work {
			if w.ID == c.id {
				wc = w
			}
		}
		require.NotNil(t, wc, "%s: its work card %s", c.primary, c.id)
		assert.Equal(t, sprint.Ready, wc.Col, c.id)
		assert.Equal(t, "m1", wc.Row, c.id)
		assert.Equal(t, c.redeals, wc.Fields["redeals"], c.id)
		assert.Empty(t, wc.Fields[sprint.FieldTakeEnded], "%s: dealt again", c.id)
		assert.Equal(t, c.ended, wc.Fields[sprint.FieldEnvExit], c.id)
		assert.Equal(t, c.tail, wc.Fields[sprint.FieldHarnessTail], c.id)
		var ends []string
		for _, con := range view.Cost.Consumers {
			ends = append(ends, con.End)
		}
		assert.Equal(t, []string{c.end}, ends, "%s: the take's attempt record", c.primary)
	}

	// the doctor names the rate from the attempt records, with the latest tail and its card
	_, doc, _ := run("doctor", "--area", "fleet")
	// (the 134 alone is a 134/137: the no-result at exit 0 is in its bucket, not the count)
	assert.Contains(t, doc, "DOCTOR WARN member-env m1 child exits: 1 of 10 children in the last hour ended 134/137 with no result (10%), last: an earlier tail remedy=nova-sprint card "+env134.Primary+"\n", doc)
	// stats --children prints the per-member histogram, read from the store
	assert.Equal(t, "CHILD m1 children=10 env=1 rate=10% exit_0=1 exit_134=1 other=9 status=WARN last: an earlier tail\n", ok("stats", "--children"))

	// m1's queue holds the same two work cards again, at the same attempt
	require.NoError(t, json.Unmarshal([]byte(ok("queue", "--as", "m1", "--json")), &q))
	var again []string
	for _, c := range q.Cards {
		again = append(again, c.ID+" attempt="+strconv.Itoa(c.Attempt)+" gen="+strconv.Itoa(c.Gen))
	}
	assert.ElementsMatch(t, []string{env134.ID + " attempt=1 gen=3", env0.ID + " attempt=1 gen=3"}, again, strings.Join(again, "; "))
}
