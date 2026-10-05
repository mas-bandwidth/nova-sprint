package sprint_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// baseTwin is a twin of the server's repository: a bare origin with the sprint base and a
// side branch cut from it, the base moved on after the cut, and the server's clone of origin
// that the check fetches into.
type baseTwin struct {
	origin, clone, base string
	baseCommit, side    string // a commit on the base, and the side branch's tip
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=twin", "-c", "user.email=twin@example.com", "-c", "init.defaultBranch=main"}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func newBaseTwin(t *testing.T) *baseTwin {
	t.Helper()
	dir := t.TempDir()
	tw := &baseTwin{origin: filepath.Join(dir, "origin.git"), clone: filepath.Join(dir, "clone"), base: "sprint/base"}
	work := filepath.Join(dir, "work")
	runGit(t, dir, "init", "-q", "--bare", tw.origin)
	runGit(t, dir, "init", "-q", work)
	commit := func(msg string) string {
		require.NoError(t, os.WriteFile(filepath.Join(work, "f"), []byte(msg), 0o644))
		runGit(t, work, "add", "f")
		runGit(t, work, "commit", "-q", "-m", msg)
		return runGit(t, work, "rev-parse", "HEAD")
	}
	runGit(t, work, "checkout", "-q", "-b", tw.base)
	tw.baseCommit = commit("on the base")
	runGit(t, work, "checkout", "-q", "-b", "side")
	tw.side = commit("off the base")
	runGit(t, work, "checkout", "-q", tw.base)
	commit("the base moves on")
	runGit(t, work, "remote", "add", "origin", tw.origin)
	runGit(t, work, "push", "-q", "origin", tw.base, "side")
	runGit(t, dir, "clone", "-q", tw.origin, tw.clone)
	return tw
}

// stamped is a binary whose version verb prints line.
func stamped(t *testing.T, line string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "nova-sprint")
	script := "#!/bin/sh\nif [ \"$1\" = version ]; then echo '" + line + "'; exit 0; fi\nexit 2\n"
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))
	return bin
}

func stampOf(commit string) string {
	return "nova-sprint 20261005120000-" + commit[:12] + " linux/amd64 go1.24.0"
}

// TestServerSwitchRefusesABinaryBuiltOffTheSprintBase: the server is built from the sprint
// base and nothing else (docs/SPEC-SPRINT.md section 14, "server-from-base-only.w3"). On a
// twin repository a binary stamped from a side branch is refused, naming its commit, the base
// and the remedy; one stamped from the base is on it, however far the base has moved; one
// with no source commit, or built from an edited tree, is refused the same way; and the tick
// keeps one judgment while the running server is off the base, closed when it is back on.
func TestServerSwitchRefusesABinaryBuiltOffTheSprintBase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tw := newBaseTwin(t)

	t.Run("a binary stamped from a side branch is refused", func(t *testing.T) {
		bin := stamped(t, stampOf(tw.side))
		b, err := sprint.CheckServerBinary(ctx, bin, tw.clone, tw.base)
		require.NoError(t, err)
		assert.False(t, b.On)
		assert.Equal(t, tw.side, b.Commit)
		why := b.Refusal(bin)
		assert.Contains(t, why, "commit "+tw.side[:12])
		assert.Contains(t, why, "not an ancestor of origin/"+tw.base)
		assert.Contains(t, why, "remedy: build nova-sprint from origin/"+tw.base+" at its tip, then run: nova-sprint server switch")
	})

	t.Run("a binary stamped from the base is on it", func(t *testing.T) {
		b, err := sprint.CheckServerBinary(ctx, stamped(t, stampOf(tw.baseCommit)), tw.clone, tw.base)
		require.NoError(t, err)
		assert.True(t, b.On, b.Why)
		assert.Equal(t, tw.baseCommit, b.Commit)
	})

	t.Run("the source revision extra is read whole", func(t *testing.T) {
		line := "nova-sprint v1.2.3 linux/amd64 go1.24.0 repo=github.com/mas-bandwidth/nova-tools revision=" + tw.baseCommit + " dirty=false build_host=bench"
		b, err := sprint.CheckServerBinary(ctx, stamped(t, line), tw.clone, tw.base)
		require.NoError(t, err)
		assert.True(t, b.On, b.Why)
	})

	t.Run("a binary with no source commit is refused the same way", func(t *testing.T) {
		for line, why := range map[string]string{
			"nova-sprint devel linux/amd64 go1.24.0":  "names no source commit",
			"nova-sprint v1.2.3 linux/amd64 go1.24.0": "names no source commit",
			"nova-sprint v1.2.3 linux/amd64 go1.24.0 repo=r revision=" + tw.baseCommit + " dirty=true build_host=h": "built from an edited tree",
			"nova-sprint 20261005120000-" + tw.baseCommit[:12] + "-dirty x/y z":                                     "built from an edited tree",
			"usage: nova-sprint <verb>":       "is not a version line",
			stampOf(strings.Repeat("ab", 20)): "is in no branch fetched from origin",
		} {
			bin := stamped(t, line)
			b, err := sprint.CheckServerBinary(ctx, bin, tw.clone, tw.base)
			require.NoError(t, err, line)
			assert.False(t, b.On, line)
			assert.Contains(t, b.Refusal(bin), why, line)
			assert.Contains(t, b.Refusal(bin), "not from the sprint base origin/"+tw.base, line)
		}
	})

	t.Run("a check that cannot be made is an error, never a pass", func(t *testing.T) {
		bin := stamped(t, stampOf(tw.baseCommit))
		_, err := sprint.CheckServerBinary(ctx, bin, tw.clone, "")
		assert.ErrorContains(t, err, "is not a branch name")
		_, err = sprint.CheckServerBinary(ctx, bin, "", tw.base)
		assert.ErrorContains(t, err, "no clone")
		_, err = sprint.CheckServerBinary(ctx, bin, tw.clone, "no-such-base")
		assert.ErrorContains(t, err, "git fetch origin no-such-base")
	})

	t.Run("a running server off the base raises one judgment, closed when it is back on", func(t *testing.T) {
		s := &sprint.Snapshot{Now: time.Date(2026, 10, 5, 9, 20, 0, 0, time.UTC), Coordinator: "coordinator"}
		off, err := sprint.CheckServerBinary(ctx, stamped(t, stampOf(tw.side)), tw.clone, tw.base)
		require.NoError(t, err)

		p, _ := sprint.TickServerBase(s, sprint.TickReq{}, &off)
		require.Len(t, p.Notes, 1)
		n := p.Notes[0]
		assert.Equal(t, sprint.Judgment, n.Kind)
		assert.Equal(t, sprint.NServerOffBase, n.Type)
		assert.Contains(t, n.What, "commit "+tw.side[:12])
		assert.Contains(t, n.What, "origin/"+tw.base)
		assert.Contains(t, n.What, "remedy: build nova-sprint from origin/"+tw.base)
		n.ID = "j1"
		s.Open = []sprint.Open{{Key: sprint.OpenKey(n.ID, sprint.StreamSubject("")), Note: n}}

		p, _ = sprint.TickServerBase(s, sprint.TickReq{}, &off)
		assert.Empty(t, p.Notes, "one judgment while it stands, never one a tick")
		assert.Empty(t, p.Closes)

		p, _ = sprint.TickServerBase(s, sprint.TickReq{}, nil)
		assert.Empty(t, p.Notes, "a tick with no check made raises nothing")
		assert.Empty(t, p.Closes, "and closes nothing")

		on, err := sprint.CheckServerBinary(ctx, stamped(t, stampOf(tw.baseCommit)), tw.clone, tw.base)
		require.NoError(t, err)
		p, _ = sprint.TickServerBase(s, sprint.TickReq{}, &on)
		assert.Empty(t, p.Notes)
		require.Len(t, p.Closes, 1, "back on the base closes it")
		assert.Equal(t, "j1", p.Closes[0].Note.ID)
	})
}

// TestTheLiveTickRaisesTheServerOffTheBase: the tick's half of server-from-base-only
// (docs/SPEC-SPRINT.md section 14, "server-from-base-only.w3") is wired, not a planner called by
// hand. The store's tick reads the running server's watch (sprint.RunningBase) into its request,
// TickCheck plans the judgment on it, and the tick writes it: on a twin repository a server
// stamped from a side branch raises one "the server runs off the sprint base" across ticks, and a
// server stamped from the base closes it. The watch never fetches in the tick: its first read is
// no fact, and a check that cannot be made is none either. Not parallel: it sets the process's
// watch, and restores it before any parallel test runs.
func TestTheLiveTickRaisesTheServerOffTheBase(t *testing.T) {
	ctx := context.Background()
	tw := newBaseTwin(t)
	was := sprint.RunningBase
	t.Cleanup(func() { sprint.RunningBase = was })

	watch := func(line, repo string) *sprint.BaseWatch {
		w := &sprint.BaseWatch{Line: line, Repo: repo, Base: tw.base}
		assert.Nil(t, w.Fact(), "the first read is no fact: the tick never waits on git")
		require.NoError(t, w.Wait(ctx))
		return w
	}
	off := watch(stampOf(tw.side), tw.clone)
	require.NotNil(t, off.Fact())
	assert.False(t, off.Fact().On)
	assert.Equal(t, tw.side, off.Fact().Commit)
	broken := watch(stampOf(tw.side), filepath.Join(t.TempDir(), "no-clone"))
	assert.Nil(t, broken.Fact(), "a check that cannot be made is no fact")
	assert.ErrorContains(t, broken.Err(), "git fetch origin "+tw.base)
	var none *sprint.BaseWatch
	assert.Nil(t, none.Fact(), "no watch is no fact")

	r := newAlarmRig(t)
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Member: "m1", Width: 4}))
	r.must(store.AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"a"}}))
	_, _, _, err := r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	raised := func() (n int, open int) {
		notes, _, err := r.m.NotesSince(r.ctx, "", 100000)
		require.NoError(t, err)
		for _, x := range notes {
			if x.Kind == sprint.Judgment && x.Type == sprint.NServerOffBase {
				n++
				assert.Contains(t, x.What, "commit "+tw.side[:12])
				assert.Contains(t, x.What, "remedy: build nova-sprint from origin/"+tw.base)
			}
		}
		for _, o := range r.snap().Open {
			if o.Note.Type == sprint.NServerOffBase {
				open++
			}
		}
		return n, open
	}

	sprint.RunningBase = nil
	r.ticks(2)
	n, open := raised()
	assert.Equal(t, [2]int{0, 0}, [2]int{n, open}, "no watch raises nothing")

	sprint.RunningBase = off
	r.ticks(3)
	n, open = raised()
	assert.Equal(t, [2]int{1, 1}, [2]int{n, open}, "off the base: one judgment, open, across ticks")

	sprint.RunningBase = broken
	r.ticks(2)
	n, open = raised()
	assert.Equal(t, [2]int{1, 1}, [2]int{n, open}, "a check that cannot be made closes nothing")

	sprint.RunningBase = watch(stampOf(tw.baseCommit), tw.clone)
	require.True(t, sprint.RunningBase.Fact().On)
	r.ticks(2)
	n, open = raised()
	assert.Equal(t, [2]int{1, 0}, [2]int{n, open}, "back on the base: closed, and not raised again")
}

// The watch checks again once its last check is Every old, never on every read, and never two
// at once.
func TestTheServerBaseWatchChecksOnceAnInterval(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 10, 5, 9, 20, 0, 0, time.UTC)
	checks := 0
	w := &sprint.BaseWatch{Every: time.Minute, Now: func() time.Time { return now },
		Check: func(context.Context) (sprint.ServerBase, error) {
			checks++
			return sprint.ServerBase{On: checks > 1}, nil
		}}
	assert.Nil(t, w.Fact())
	require.NoError(t, w.Wait(ctx))
	for range 5 {
		require.NotNil(t, w.Fact())
		require.NoError(t, w.Wait(ctx))
	}
	assert.Equal(t, 1, checks, "one check within the interval")
	assert.False(t, w.Fact().On)
	now = now.Add(time.Minute)
	w.Fact()
	require.NoError(t, w.Wait(ctx))
	assert.Equal(t, 2, checks, "a second once the last is Every old")
	assert.True(t, w.Fact().On)
}
