package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// gcLoopBuf is the loop's stdout, safe to read from the test while the loop runs.
type gcLoopBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *gcLoopBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *gcLoopBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// gcLoopLocal is the name the loop says for this machine: its host name, shortened.
func gcLoopLocal(t *testing.T) string {
	t.Helper()
	h, err := os.Hostname()
	require.NoError(t, err)
	h, _, _ = strings.Cut(h, ".")
	if h == "" {
		h = "local"
	}
	return h
}

// gcLoopDrive starts the loop in the bubble and returns once it has made its
// first round: everything before gcLoopFirst is silent.
func gcLoopDrive(t *testing.T, a *app, inv inventoryFn) (*gcLoopBuf, context.CancelFunc, <-chan struct{}) {
	t.Helper()
	a.now = time.Now // the bubble's fake clock
	a.inventory = inv
	ctx, cancel := context.WithCancel(context.Background())
	var buf gcLoopBuf
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.gcLoop(ctx, &buf)
	}()
	time.Sleep(gcLoopFirst)
	synctest.Wait()
	return &buf, cancel, done
}

// TestGcLoopRunsTheLocalMachineAtItsFirstRound pins gcLoop's first round
// (docs/SPEC-SPRINT.md, the verb gc): nothing runs before gcLoopFirst, then the
// local machine's first run is said with why=first, its class line and its
// summary prefixed with the time, and no removal line is said.
func TestGcLoopRunsTheLocalMachineAtItsFirstRound(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		a, _, _, _ := gcApp(t)
		empty := func(context.Context, string) ([]config.MachineWidth, error) { return nil, nil }
		a.now = time.Now
		a.inventory = empty
		ctx, cancel := context.WithCancel(context.Background())
		var buf gcLoopBuf
		done := make(chan struct{})
		go func() {
			defer close(done)
			a.gcLoop(ctx, &buf)
		}()
		local := gcLoopLocal(t)

		time.Sleep(gcLoopFirst - time.Minute)
		synctest.Wait()
		assert.NotContains(t, buf.String(), "GC machine=", "nothing is said before gcLoopFirst")

		time.Sleep(time.Minute)
		synctest.Wait()
		out := buf.String()
		assert.Regexp(t, `(?m)^\d{2}:\d{2}:\d{2} GC machine=`+regexp.QuoteMeta(local)+` why=first: GC jobs count=`, out)
		assert.Regexp(t, `(?m)^\d{2}:\d{2}:\d{2} GC machine=`+regexp.QuoteMeta(local)+` why=first: GC OK freed=`, out)
		assert.NotContains(t, out, "GC REMOVED ")
		assert.NotContains(t, out, "GC WOULD-REMOVE ")
		assert.NotContains(t, out, "GC KEPT ")

		cancel()
		<-done
	})
}

// TestGcLoopDoesNotRunTheLocalMachineAgainWithinTheHour pins gcLoop against
// sprint.GCDue's hour: the loop wakes every gcLoopEvery but the local machine's
// second run is not due within the hour.
func TestGcLoopDoesNotRunTheLocalMachineAgainWithinTheHour(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		a, _, _, _ := gcApp(t)
		empty := func(context.Context, string) ([]config.MachineWidth, error) { return nil, nil }
		buf, cancel, done := gcLoopDrive(t, a, empty)
		first := buf.String()
		require.Contains(t, first, "why=first", "the first round runs")

		time.Sleep(5 * gcLoopEvery)
		synctest.Wait()
		out := buf.String()
		assert.Equal(t, strings.Count(first, "GC machine="), strings.Count(out, "GC machine="),
			"no second run within the hour though the loop wakes every gcLoopEvery")
		assert.NotContains(t, out, "why=hourly")

		cancel()
		<-done
	})
}

// TestGcLoopSaysTheInventoryFailureOnce pins gcLoop's inventory line: the
// config's machines that cannot be read are said once however many rounds fail,
// and again only after a round that answered.
func TestGcLoopSaysTheInventoryFailureOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		a, _, _, _ := gcApp(t)
		a.now = time.Now
		var failing atomicBool
		failing.set(true)
		a.inventory = func(context.Context, string) ([]config.MachineWidth, error) {
			if failing.get() {
				return nil, errors.New("the config could not be read")
			}
			return nil, nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		var buf gcLoopBuf
		done := make(chan struct{})
		go func() {
			defer close(done)
			a.gcLoop(ctx, &buf)
		}()

		time.Sleep(gcLoopFirst + 3*gcLoopEvery)
		synctest.Wait()
		assert.Equal(t, 1, strings.Count(buf.String(), "GC the config's machines were not read ("),
			"the inventory failure is said once however many rounds fail")

		failing.set(false)
		time.Sleep(gcLoopEvery)
		synctest.Wait()
		failing.set(true)
		time.Sleep(gcLoopEvery)
		synctest.Wait()
		assert.Equal(t, 2, strings.Count(buf.String(), "GC the config's machines were not read ("),
			"it is said again after a round that answered")

		cancel()
		<-done
	})
}

// TestGcLoopEndsOnACancelledContext pins gcLoop's ctx.Done arm: a cancelled
// context ends the loop and nothing more is said.
func TestGcLoopEndsOnACancelledContext(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		a, _, _, _ := gcApp(t)
		empty := func(context.Context, string) ([]config.MachineWidth, error) { return nil, nil }
		buf, cancel, done := gcLoopDrive(t, a, empty)
		before := buf.String()
		require.Contains(t, before, "why=first")

		cancel()
		<-done
		time.Sleep(5 * gcLoopEvery)
		synctest.Wait()
		assert.Equal(t, before, buf.String(), "a cancelled context says nothing more")
	})
}

// atomicBool is a bool read and written from the loop and the test goroutines.
type atomicBool struct {
	mu sync.Mutex
	v  bool
}

func (b *atomicBool) set(v bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.v = v
}

func (b *atomicBool) get() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.v
}
