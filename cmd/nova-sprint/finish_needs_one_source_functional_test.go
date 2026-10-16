//go:build functional

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// workingOnAStore is a real store (a redis-server of the test's own, the function
// library loaded, the environment scrubbed to the store) with "work" taken by m1 and
// working, and "dep" ready behind it.
func workingOnAStore(t *testing.T) (*functionalTestApp, func(args ...string) string) {
	t.Helper()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, fn.Load(context.Background(), c))
	ta := newFunctionalTestApp(t, addr)
	run := func(args ...string) string {
		t.Helper()
		return ta.runVerb(args...)
	}
	dir := t.TempDir()
	run("init", "--readers", "reader-a,reader-b", "--members", "m1")
	run("add", "--one", "--stream", "a", "work", "--brief-file", writeHeaderBrief(t, dir, "work", "-", "internal/work.go"))
	run("add", "--one", "--stream", "a", "dep", "--brief-file", writeHeaderBrief(t, dir, "dep", "-", "internal/dep.go"))
	run("fleet", "beat", "m1")
	run("start")
	run("tick")
	run("tick")
	require.Contains(t, run("card", "work"), "working")
	run("take", "--as", "m1", "--limit", "100")
	return ta, run
}

// TestFinishAgreesWithTheNeedsRecordOnAStore runs the go-table case and the refusal
// bound on a real store, every refusal included: a need gained while working and then
// cleared finishes to review on the first try, exit 0, and a finish the lifecycle
// refuses over and over raises exactly one NFinishRefused judgment.
func TestFinishAgreesWithTheNeedsRecordOnAStore(t *testing.T) {
	t.Parallel()

	t.Run("a need gained then cleared finishes to review", func(t *testing.T) {
		t.Parallel()
		ta, run := workingOnAStore(t)
		assert.Contains(t, run("needs", "set", "work", "--needs", "dep"), "working, waits for dep after this attempt")
		assert.NotContains(t, run("needs", "set", "work", "--clear"), "waits for")
		assert.Contains(t, run("check"), "CHECK OK")
		out, code, errs := ta.runVerbOut("finish", "--as", "m1", "work.w1@1", "--epoch", "0", "--head", strings.Repeat("a", 40), "--report", "done")
		require.Equal(t, 0, code, out+errs)
		assert.Contains(t, out, "working -> review")
		run("tick") // the machine applies the finish's queued move
		assert.Contains(t, run("card", "work"), "review")
		assert.Contains(t, run("check"), "CHECK OK")
	})

	t.Run("a finish refused past the bound raises one judgment", func(t *testing.T) {
		t.Parallel()
		ta, run := workingOnAStore(t)
		// a server-default hold on the working primary: the lifecycle refuses any move of it
		ctx := context.Background()
		b, err := ta.app().backend(ctx, ta.store, sprint.Names{})
		require.NoError(t, err)
		st := &store.Store{B: b, Names: sprint.Names{}, Actor: "migration", Now: ta.app().now, NewID: store.NewID}
		res, err := st.Run(ctx, store.Step{Verb: "seed hold", Load: []string{sprint.Work},
			Plan: func(s *sprint.Snapshot) sprint.Plan {
				c := s.Work.Card("work")
				e := ntable.BatchMemberEntry{ID: "work", Expect: &ntable.MemberExpect{Revision: itoa(int(c.Rev))}, Set: map[string]string{
					sprint.FieldHeld: "2026-10-15T00:00:00Z", "held_by": sprint.ServerDefaultActor, "held_reason": "test", "decision_held_from": sprint.Review}}
				return sprint.Plan{Units: []sprint.Unit{{Key: "work", Changes: []sprint.Change{{Table: sprint.Work, Entry: e}}}}}
			}})
		require.NoError(t, err)
		require.Empty(t, res.Refused)

		raised := 0
		for i := 1; i <= 7; i++ {
			out, code, errs := ta.runVerbOut("finish", "--as", "m1", "work.w1@1", "--epoch", "0", "--head", strings.Repeat("e", 40), "--report", "done")
			require.Equal(t, 1, code, "finish %d: %s%s", i, out, errs)
			assert.Contains(t, errs, "release the server-default hold", "finish %d", i)
			if strings.Contains(out+errs, "JUDGMENT raised") {
				raised++
				assert.Equal(t, 6, i)
			}
		}
		assert.Equal(t, 1, raised)
		assert.Equal(t, 1, strings.Count(run("inbox"), sprint.NFinishRefused), "one judgment in the inbox")
	})
}
