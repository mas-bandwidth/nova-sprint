//go:build functional

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
)

// TestAMergeConflictBoundTakesReworkCarryOnARealStore: a card whose landing was refused twice
// ONLY for a merge conflict with the advanced base reaches a bound that is the base's, not the
// brief's (tla/Land.tla, THE BOUND, ConflictBoundCarries; docs/SPEC-SPRINT.md, the brief is
// wrong, not the worker). On a real store (a private redis-server, the function library
// loaded), rework --carry is taken for it whatever the brief-defect rule did, writes the
// brief's CARRY: line naming its last pushed head and resets the bound; a card refused twice
// at the tree gate is the brief's still, and rework --carry is refused for it (exit 1, the
// brief-defect refusal kept). Incident of 2026-10-15: three v13-blockers cards were marked
// brief-defect after two conflict refusals and each was re-cut by hand.
func TestAMergeConflictBoundTakesReworkCarryOnARealStore(t *testing.T) {
	t.Parallel()
	const head1, head2 = "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"

	// drive runs s1-1 to two landing refusals of one kind on a private store and returns the
	// app, with the card in review under the bound's judgment and the machine ticked once
	drive := func(t *testing.T, refuse func(n int) sprint.MergeReq) *functionalTestApp {
		t.Helper()
		addr := testutil.Start(t)
		c := redis.NewClient(&redis.Options{Addr: addr})
		t.Cleanup(func() { _ = c.Close() })
		require.NoError(t, fn.Load(context.Background(), c))
		ta := newFunctionalTestApp(t, addr)
		run := func(args ...string) string { t.Helper(); return ta.runVerb(args...) }
		land := func(head string, n int) {
			t.Helper()
			run("take", "--as", "m1", "--limit", "5", "--epoch", "0")
			run("finish", "--as", "m1", "s1-1.w"+string(rune('0'+n))+"@1", "--head", head, "--epoch", "0")
			run("queue", "--as", "reader-a")
			run("queue", "--as", "reader-b")
			run("tick") // the primary moves to review; its reads are asked
			run("read", "--as", "reader-a", "--begin", "--limit", "10", "--epoch", "0")
			run("read", "--as", "reader-a", "--ok", "--limit", "10", "--epoch", "0")
			run("read", "--as", "reader-b", "--begin", "--limit", "10", "--epoch", "0")
			run("read", "--as", "reader-b", "--ok", "--limit", "10", "--epoch", "0")
			run("accept", "s1-1")
			// the landing's refusal, as land records it (the step, at the epoch held)
			fs, cm := ta.a.verbSetup("merge")
			_, err := parse(fs, []string{"--epoch", "0", "--redis", addr})
			require.NoError(t, err)
			st, err := ta.a.store(*cm)
			require.NoError(t, err)
			req := refuse(n)
			req.Stream, req.Batch, req.Conflict, req.Who = "s1", 1, "s1-1", cm.actor
			var out, errb bytes.Buffer
			code := ta.a.runStep("merge", *cm, st, store.MergeStep(req), &out, &errb)
			require.Zero(t, code, "the refusal %d: exit %d\n%s%s", n, code, out.String(), errb.String())
		}
		run("init", "--readers", "reader-a,reader-b", "--members", "m1")
		run("set", "--read-cards", "off")
		run("add", "--stream", "s1", "--count", "1", "--one")
		run("fleet", "beat", "m1")
		run("start")
		run("tick")
		run("tick") // the fleet update brings m1 up; the next pump deals
		land(head1, 1)
		require.Contains(t, run("card", "s1-1"), "ready", "the first refusal reworks it")
		run("fleet", "beat", "m1")
		run("tick") // the pump deals attempt 2, staged from the refused head
		land(head2, 2)
		require.Contains(t, run("card", "s1-1"), "review", "the second refusal is the bound")
		run("tick", "--answer-rules") // the brief-defect rule answers the judgment: it marks the card or leaves it
		return ta
	}
	card := func(t *testing.T, ta *functionalTestApp) cardView {
		t.Helper()
		var v cardView
		require.NoError(t, json.Unmarshal([]byte(ta.runVerb("card", "s1-1", "--json")), &v))
		return v
	}

	t.Run("two conflict refusals take rework --carry", func(t *testing.T) {
		t.Parallel()
		ta := drive(t, func(n int) sprint.MergeReq {
			return sprint.MergeReq{ConflictKind: "file", ConflictPaths: []string{"internal/x.go"},
				Note: "the head h" + string(rune('0'+n)) + " of s1-1 does not merge: CONFLICT (content): Merge conflict in internal/x.go"}
		})
		require.Contains(t, ta.runVerb("card", "s1-1"), "the landing refused its head twice only for a merge conflict with the advanced base")
		out, code, errb := ta.runVerbOut("rework", "s1-1", "--carry", "--fix", "the base moved: merge it and rerun the gates")
		require.Zero(t, code, "rework --carry: exit %d\n%s%s", code, out, errb)
		ta.runVerb("tick") // the machine is running: the pump applies the rework
		v := card(t, ta)
		assert.Contains(t, v.Primary.F("brief"), "CARRY: s1-1 attempt 2 head="+head2, "the CARRY: line names its last pushed head")
		assert.Equal(t, "2", v.Primary.F(sprint.FieldBriefAttempt), "the bound is reset")
		_, atBound := sprint.AtBriefBound(v.Primary, "", sprint.AttemptsDefault)
		assert.False(t, atBound, "the card is not at a bound after the carry")
		assert.Empty(t, v.Primary.F(sprint.FieldBriefDefect), "clean of the brief-defect mark")
		assert.Contains(t, ta.runVerb("check"), "CHECK OK violations=0")
	})

	t.Run("two gate refusals are refused rework --carry", func(t *testing.T) {
		t.Parallel()
		ta := drive(t, func(n int) sprint.MergeReq {
			return sprint.MergeReq{Note: "the head h" + string(rune('0'+n)) + " of s1-1 fails the tree gate: go vet ./...: exit status 1"}
		})
		out, code, errb := ta.runVerbOut("rework", "s1-1", "--carry", "--fix", "x")
		require.Equal(t, 1, code, "rework --carry is refused: %s%s", out, errb)
		assert.True(t, strings.Contains(out+errb, "it ended on a brief defect"), "the refusal line is kept: %s%s", out, errb)
		assert.NotContains(t, card(t, ta).Primary.F("brief"), "CARRY:", "nothing was written")
	})
}
