package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// views counts the "view" calls the forge saw: the looks at the pull request.
func (f *fakeForge) views() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, "view ") {
			n++
		}
	}
	return n
}

// TestPromoteWakesOnEvGithub: while a promotion's pull request is pending, the
// wait blocks on the store's ev:github stream for a row naming the pull request
// instead of polling gh every --poll; a row ends the wait at once, the clock not
// advanced, and the poll stays the backstop for a lost row.
func TestPromoteWakesOnEvGithub(t *testing.T) {
	t.Parallel()

	const args = "promote --once --branch sprint/live --poll 1m --repo-dir "

	t.Run("a check_run row ends the wait without the clock", func(t *testing.T) {
		t.Parallel()
		w := newPromoteTwin(t)
		tip := w.remote("refs/heads/sprint/live")
		w.inFlight(tip, false)
		f := &fakeForge{checks: []promoteCheck{{Name: "functional", Bucket: "pending"}}}
		useForge(t, w, f)
		ta := promoteApp(t, w)
		merged := w.remote("refs/heads/sprint/live")

		wakeCh := make(chan struct{})
		promoteWakes.Store(w.dir, func(context.Context, time.Duration) (<-chan struct{}, error) {
			return wakeCh, nil
		})
		t.Cleanup(func() { promoteWakes.Delete(w.dir) })

		// after fires never: the waking row ends this wait when it arrives, and
		// the next look at gh merges, ending the --once run.
		ta.a.after = func(time.Duration) <-chan time.Time {
			f.mergedSHA = merged
			close(wakeCh)
			return make(chan time.Time)
		}

		code, out, errs := ta.do(args + w.dir)
		require.Zero(t, code, "%s\n%s", out, errs)
		require.Equal(t, 2, f.views(), "a row wakes one more look at gh: %v", f.calls)

		ta.mu.Lock()
		now := ta.now
		ta.mu.Unlock()
		require.Equal(t, t0, now, "the waking row does not advance the clock")

		require.NotEmpty(t, ta.promotedSha(), "the wake let the promotion merge")
		ta.clean()
	})

	t.Run("with no row the backstop asks at --poll", func(t *testing.T) {
		t.Parallel()
		w := newPromoteTwin(t)
		tip := w.remote("refs/heads/sprint/live")
		w.inFlight(tip, false)
		f := &fakeForge{checks: []promoteCheck{{Name: "functional", Bucket: "pending"}}}
		useForge(t, w, f)
		ta := promoteApp(t, w)
		merged := w.remote("refs/heads/sprint/live")

		wakeCh := make(chan struct{})
		promoteWakes.Store(w.dir, func(context.Context, time.Duration) (<-chan struct{}, error) {
			return wakeCh, nil
		})
		t.Cleanup(func() { promoteWakes.Delete(w.dir) })

		// no row arrives; the backstop fires at --poll, and the next look merges.
		ta.a.after = func(d time.Duration) <-chan time.Time {
			ta.mu.Lock()
			ta.now = ta.now.Add(d)
			ta.mu.Unlock()
			f.mergedSHA = merged
			fired := make(chan time.Time, 1)
			fired <- ta.now
			return fired
		}

		code, out, errs := ta.do(args + w.dir)
		require.Zero(t, code, "%s\n%s", out, errs)
		require.Equal(t, 2, f.views(), "without a row the backstop asks at --poll: %v", f.calls)

		ta.mu.Lock()
		now := ta.now
		ta.mu.Unlock()
		require.Equal(t, t0.Add(time.Minute), now, "the backstop advances the clock by --poll")

		require.NotEmpty(t, ta.promotedSha())
		ta.clean()
	})
}
