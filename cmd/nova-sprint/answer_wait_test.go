package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/decide"
)

// pushInbox is the mem store with a fake push transport on its inbox: the wait
// answer --every blocks on between passes (store.InboxWaiter). A note already
// after the cursor returns at once, as XREAD does; else at(n) runs at the n-th
// wait, the sprint moving under it, and one step of the transport delivers the
// push of what it committed, with no clock step. A dropped push is lost: the
// wait runs out at its bound on the test app's clock, the backstop.
type pushInbox struct {
	*store.Mem
	ta   *testApp
	drop bool
	at   func(n int)

	mu    sync.Mutex
	waits []string // the cursor each wait began from
}

func (p *pushInbox) WaitInbox(ctx context.Context, after string, d time.Duration) (string, bool, error) {
	p.mu.Lock()
	p.waits = append(p.waits, after)
	n := len(p.waits)
	p.mu.Unlock()
	if tail, ok := p.ahead(ctx, after); ok {
		return tail, true, nil
	}
	if p.at != nil {
		p.at(n)
	}
	if tail, ok := p.ahead(ctx, after); ok && !p.drop {
		return tail, true, nil // one step of the transport: the push arrives
	}
	p.ta.a.sleep(d) // no push: the wait runs to its bound
	tail, _ := p.ahead(ctx, after)
	return tail, false, ctx.Err()
}

// ahead is the inbox's tail and whether a note lies after the cursor.
func (p *pushInbox) ahead(ctx context.Context, after string) (string, bool) {
	notes, _, err := p.Mem.NotesSince(ctx, after, 1)
	require.NoError(p.ta.t, err)
	_, tail, err := p.Mem.Tails(ctx)
	require.NoError(p.ta.t, err)
	return tail, len(notes) > 0
}

func (p *pushInbox) cursors() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.waits...)
}

// pushing is a running sprint of two one-card streams dealt to m1, its store
// behind the fake push transport, and a judge that answers rework at 0.90.
func pushing(t *testing.T, by func(string) (string, float64, string, string)) (*testApp, *judge, *pushInbox, string) {
	t.Helper()
	ta, j, record := answering(t, by)
	p := &pushInbox{Mem: ta.m, ta: ta}
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) { return p, nil }
	ta.ok("add --stream s1 --count 1 --one")
	ta.ok("add --stream s2 --count 1 --one")
	ta.ok("start")
	ta.deal(2)
	return ta, j, p, record
}

// failed commits one judgment to the inbox, the card come back failed, with no
// step of the clock.
func (ta *testApp) failed(card string) {
	ta.t.Helper()
	ta.ok("take --as m1 " + card)
	ta.ok("finish --as m1 " + card + " --failed --report red")
}

// answer --every blocks on the inbox stream between passes, not on its clock
// (Glenn, 2026-10-15: "Event driven push notifications are vastly superior to
// polling"): a judgment committed while it waits ends the wait after that one
// note, and the pass answers it with the clock never advanced; a note
// committed during a pass is after the cursor the next wait starts from, so it
// is answered by the next pass at once, never skipped. --every stays the
// bound: a push lost on the transport is caught by the pass at --every, once.
func TestAnswerWaitsOnTheInboxStream(t *testing.T) {
	t.Parallel()

	t.Run("one event ends the wait", func(t *testing.T) {
		t.Parallel()
		var ta *testApp
		second := false
		ta, j, p, record := pushing(t, func(state string) (string, float64, string, string) {
			if !second && strings.Contains(state, "card: s1-1") {
				second = true
				ta.failed("s2-1.w1@1") // a second note, committed during the pass
			}
			return decide.VerbRework, 0.9, "own", "cannot-be-done"
		})
		start := ta.a.now()
		var atQuiet time.Time
		quiet := false
		p.at = func(n int) {
			switch {
			case n == 1:
				ta.failed("s1-1.w1@1")
			case !quiet: // the first wait that blocks after both are answered
				quiet = true
				atQuiet = ta.a.now()
				ta.ok("stop --reason r --until 9999h")
			}
		}
		stopped := false
		ta.atSleep(func(int) { // the loop never hangs: a sleep of the clock stops the machine
			if !stopped {
				stopped = true
				ta.do("stop --reason r --until 9999h")
			}
		})
		out := ta.ok("answer --bar 0.8 --every 60s --record " + record)
		assert.Contains(t, out, "s1-1  failed  rework  0.90  applied", out)
		assert.Contains(t, out, "s2-1  failed  rework  0.90  applied", out)
		assert.Equal(t, 2, j.asks(), "each judgment asked once")
		require.True(t, quiet, "the loop waited on the inbox stream between passes:\n%s", out)
		assert.Equal(t, start, atQuiet, "both judgments were answered with the clock never advanced: the push, not the --every poll") // wall-ok: the test app's clock
		cs := p.cursors()
		assert.NotEqual(t, cs[0], cs[1], "the wait after the first push starts from the tail it woke at")
		assert.Contains(t, out, "ANSWER STOPPED ", out)
	})

	t.Run("a dropped push is caught by the backstop once", func(t *testing.T) {
		t.Parallel()
		var ta *testApp
		var askedAt []time.Time
		ta, j, p, record := pushing(t, func(string) (string, float64, string, string) {
			askedAt = append(askedAt, ta.a.now())
			return decide.VerbRework, 0.9, "own", "cannot-be-done"
		})
		p.drop = true
		start := ta.a.now()
		p.at = func(n int) {
			if n == 1 {
				ta.failed("s1-1.w1@1")
			}
		}
		ta.atSleep(func(n int) {
			if n == 2 { // the wait after the backstop's pass runs out: the machine stops
				ta.do("stop --reason r --until 9999h")
			}
		})
		out := ta.ok("answer --bar 0.8 --every 60s --record " + record)
		assert.Equal(t, 1, strings.Count(out, "s1-1  failed  rework  0.90  applied"), "caught once:\n%s", out)
		assert.Equal(t, 1, j.asks(), "asked once")
		require.Len(t, askedAt, 1)
		assert.Equal(t, time.Minute, askedAt[0].Sub(start), "caught by the pass at --every, the backstop, and no later") // wall-ok: the test app's clock
		assert.NotEmpty(t, p.cursors(), "the loop waited on the inbox stream:\n%s", out)
	})
}
