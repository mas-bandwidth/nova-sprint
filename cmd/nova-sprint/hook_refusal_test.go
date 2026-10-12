package main

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// A refusal queued behind another store operation must count only its own writes.
// Wait proves the helper is blocked on the control line before that operation;
// no socket, polling or scheduler delay stands in for the ordering.
func TestHookRefusalCountsOnlyItsOwnWrites(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ta, _ := hookSprint(t, "hook-refusal-count")
		// The fake server has no lifetime loop: keep its absent bus proof fresh
		// for this command, so the gate needs no cache-refresh goroutine.
		ta.a.hk.cache = map[string]hookCopy{"hook-refusal-count": {at: time.Now()}}
		done := make(chan struct{})
		ta.lined(func() {
			go func() {
				defer close(done)
				ta.refusedFirst("where", sprint.HookFirst)
			}()
			synctest.Wait()

			// A preceding operation really commits through the same in-memory backend.
			before := ta.applies()
			st := &store.Store{B: ta.m, Actor: "hook-refusal-count", Now: ta.a.now}
			res, err := st.Run(context.Background(), store.AddStep(sprint.AddReq{
				Stream: "preceding", IDs: []string{"preceding-gate"}, Sentinel: true,
			}))
			assert.NoError(t, err)
			assert.Empty(t, res.Refused)
			assert.Greater(t, ta.applies(), before, "the preceding operation must write")
		})
		<-done
	})
}
