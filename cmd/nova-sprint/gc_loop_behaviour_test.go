package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// gcLoopLocal is the name gcLoop gives this machine: the host name to its first
// dot, "local" when it has none (gc.go gcLoop).
func gcLoopLocal(t *testing.T) string {
	t.Helper()
	h, err := os.Hostname()
	require.NoError(t, err)
	name, _, _ := strings.Cut(h, ".")
	if name == "" {
		name = "local"
	}
	return name
}

// gcLoopTestApp is gcApp on the synctest bubble's clock with the inventory the
// test gives: the loop's first round runs gc on this machine alone, so every
// root it walks is the test's own temp directory (gc_test.go gcApp, gcFile).
func gcLoopTestApp(t *testing.T, inv inventoryFn) (*app, string) {
	t.Helper()
	a, _, _, _ := gcApp(t)
	a.now = time.Now // the synctest bubble's fake clock
	a.inventory = inv
	return a, gcLoopLocal(t)
}

// (1) The loop says nothing before gcLoopFirst; at it this machine's first run
// is said with why=first, its class lines and summary each prefixed with the
// time and the machine, and no removal or keep detail is said (gc.go gcLoop;
// docs/SPEC-SPRINT.md section 1, "gc"; sprint.GCDue says why=first).
func TestGcLoopSaysTheLocalFirstRunAtTheFirstRound(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		a, local := gcLoopTestApp(t, func(context.Context, string) ([]config.MachineWidth, error) {
			return nil, nil
		})
		var buf bytes.Buffer
		ctx, cancel := context.WithCancel(context.Background())
		go a.gcLoop(ctx, &buf)

		time.Sleep(gcLoopFirst - time.Second)
		synctest.Wait()
		assert.Empty(t, buf.String(), "nothing is said before gcLoopFirst")

		time.Sleep(time.Second)
		synctest.Wait()
		out := buf.String()
		assert.Contains(t, out, "GC machine="+local+" why=first: GC jobs count=0 ", "the class lines are prefixed")
		assert.Contains(t, out, "GC machine="+local+" why=first: GC OK freed=0 ", "the summary is prefixed")
		assert.NotContains(t, out, "GC REMOVED")
		assert.NotContains(t, out, "GC WOULD-REMOVE")
		assert.NotContains(t, out, "GC KEPT")

		cancel()
		synctest.Wait()
	})
}

// (2) The loop wakes every gcLoopEvery but runs this machine again only when
// sprint.GCDue says so: inside the hour no second run, so no why=hourly line
// (gc.go gcLoop; sprint.GCDue's GCEvery).
func TestGcLoopDoesNotRunAgainBeforeTheHour(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		a, _ := gcLoopTestApp(t, func(context.Context, string) ([]config.MachineWidth, error) {
			return nil, nil
		})
		var buf bytes.Buffer
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go a.gcLoop(ctx, &buf)

		time.Sleep(gcLoopFirst)
		synctest.Wait()
		first := strings.Count(buf.String(), "why=first")
		require.Positive(t, first, "the first run is said:\n%s", buf.String())

		for i := 0; i < 6; i++ {
			time.Sleep(gcLoopEvery)
			synctest.Wait()
		}
		out := buf.String()
		assert.Equal(t, first, strings.Count(out, "why=first"), "no second first run")
		assert.NotContains(t, out, "why=hourly", "the hour is not up")

		cancel()
		synctest.Wait()
	})
}

// (3) An inventory that errors is said once however many rounds it keeps failing,
// and again only after a round where it answered (gc.go gcLoop; the line "GC the
// config's machines were not read").
func TestGcLoopSaysTheInventoryFailureOnceUntilItAnswers(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		a, _ := gcLoopTestApp(t, func(context.Context, string) ([]config.MachineWidth, error) {
			if calls.Add(1) == 3 {
				return nil, nil // the round that answers
			}
			return nil, errors.New("nova-config is down")
		})
		var buf bytes.Buffer
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go a.gcLoop(ctx, &buf)

		time.Sleep(gcLoopFirst)
		synctest.Wait()
		time.Sleep(gcLoopEvery)
		synctest.Wait()
		assert.Equal(t, 1, strings.Count(buf.String(), "the config's machines were not read"), "said once for two failures")

		time.Sleep(gcLoopEvery) // the round that answers
		synctest.Wait()
		time.Sleep(gcLoopEvery) // and fails again
		synctest.Wait()
		assert.Equal(t, 2, strings.Count(buf.String(), "the config's machines were not read"), "said again after a round that answered")

		cancel()
		synctest.Wait()
	})
}

// (4) A cancelled ctx ends the loop with nothing more said (gc.go gcLoop's
// `case <-ctx.Done(): return`).
func TestGcLoopStopsWhenTheContextIsCancelled(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		a, _ := gcLoopTestApp(t, func(context.Context, string) ([]config.MachineWidth, error) {
			return nil, nil
		})
		var buf bytes.Buffer
		ctx, cancel := context.WithCancel(context.Background())
		go a.gcLoop(ctx, &buf)

		time.Sleep(gcLoopFirst)
		synctest.Wait()
		before := buf.String()

		cancel()
		synctest.Wait()
		time.Sleep(3 * gcLoopEvery)
		synctest.Wait()
		assert.Equal(t, before, buf.String(), "nothing more is said after the cancel")
	})
}
