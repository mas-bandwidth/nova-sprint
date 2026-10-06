package sprint_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/buildinfo"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// baseTwin is a twin of the server's repository: origin (bare) with main, the sprint base,
// and a side branch cut from it; the server's clone, made before main moved on, so a check
// that does not fetch the base cannot see its tip.
type baseTwin struct {
	origin, work, server string
	a, side, b           string // a: main when cloned; side: on the side branch only; b: main's tip after
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=twin", "-c", "user.email=twin@example.invalid", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func newBaseTwin(t *testing.T) *baseTwin {
	t.Helper()
	root := t.TempDir()
	tw := &baseTwin{origin: filepath.Join(root, "origin.git"), work: filepath.Join(root, "work"), server: filepath.Join(root, "server")}
	gitT(t, root, "init", "-q", "--bare", "-b", "main", tw.origin)
	gitT(t, root, "clone", "-q", tw.origin, tw.work)
	commit := func(msg string) string {
		gitT(t, tw.work, "commit", "-q", "--allow-empty", "-m", msg)
		return gitT(t, tw.work, "rev-parse", "HEAD")
	}
	gitT(t, tw.work, "checkout", "-q", "-b", "main")
	tw.a = commit("a")
	gitT(t, tw.work, "push", "-q", "origin", "main")
	gitT(t, tw.work, "checkout", "-q", "-b", "side")
	tw.side = commit("side")
	gitT(t, tw.work, "push", "-q", "origin", "side")
	gitT(t, root, "clone", "-q", tw.origin, tw.server)
	gitT(t, tw.work, "checkout", "-q", "main")
	tw.b = commit("b")
	gitT(t, tw.work, "push", "-q", "origin", "main")
	return tw
}

// stamped is a binary whose version verb prints line: what a binary built at a commit says.
func stamped(t *testing.T, dir, name, line string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\necho '"+line+"'\n"), 0o755))
	return p
}

func lineAt(commit string) string { return "nova-sprint v9.9.9 linux/amd64 go1.26 commit=" + commit }

// TestServerSwitchRefusesABinaryBuiltOffTheSprintBase: the server is built from the sprint base
// and nothing else (docs/SPEC-SPRINT.md section 14, "server-from-base-only.w1"). On a twin
// repository, a binary stamped from a side branch is refused naming its commit, the base and
// the remedy, and nothing on disk changes; so is a binary with no source commit, one built
// from an edited tree, one whose commit origin never had, and a switch with no base named.
// One stamped from the base's tip, which only a fetch brings, is switched and the base it was
// checked against is recorded beside it. The tick raises one judgment while the running
// server's commit is off the base and closes it when it is back on. Not parallel: the tick's
// watch is the process's (sprint.RunningBase), and no parallel test runs while this one does.
func TestServerSwitchRefusesABinaryBuiltOffTheSprintBase(t *testing.T) {
	ctx := context.Background()
	tw := newBaseTwin(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "nova-sprint")
	require.NoError(t, os.WriteFile(target, []byte("the running server"), 0o755))
	switchTo := func(bin, repo, base string) error {
		return sprint.SwitchFromBase(ctx, sprint.ServerSwitchOptions{Binary: bin, Target: target, Repo: repo, Base: base, Rollback: true})
	}
	unchanged := func(why string) {
		t.Helper()
		b, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "the running server", string(b), "%s: the old binary is in place", why)
		for _, side := range []string{".prev", ".switch.json", ".base.json"} {
			_, err := os.Stat(target + side)
			assert.True(t, os.IsNotExist(err), "%s: nothing written beside the target (%s)", why, side)
		}
	}

	t.Run("a binary stamped from a side branch is refused", func(t *testing.T) {
		err := switchTo(stamped(t, dir, "side", lineAt(tw.side)), tw.server, "main")
		var off *sprint.OffBaseError
		require.True(t, errors.As(err, &off), "%v", err)
		msg := err.Error()
		assert.Contains(t, msg, "built from commit "+tw.side[:12])
		assert.Contains(t, msg, "not from the sprint base origin/main (tip "+tw.b[:12]+")", "the base's tip is the fetched one")
		assert.Contains(t, msg, "its commit "+tw.side[:12]+" is not an ancestor of origin/main")
		assert.Contains(t, msg, "remedy: build nova-sprint from origin/main at its tip, then run: nova-sprint server switch <that binary>")
		unchanged("side")
	})

	t.Run("a binary that names no base commit is refused the same way", func(t *testing.T) {
		cases := []struct{ name, line, why string }{
			{"devel", "nova-sprint devel linux/amd64 go1.26", "its build identity devel names no source commit"},
			{"a tag alone", "nova-sprint v1.0.0 linux/amd64 go1.26", "its build identity v1.0.0 names no source commit"},
			{"dirty", lineAt(tw.b + "-dirty"), "built from an edited tree at " + tw.b},
			{"dirty stamp", "nova-sprint 20261005000000-" + tw.b[:12] + "-dirty linux/amd64 go1.26", "built from an edited tree"},
			{"no version line", "usage: nova-sprint <verb>", "is not a version line"},
			{"never on origin", lineAt(strings.Repeat("ab", 20)), "is not in origin/main's history"},
		}
		for _, tc := range cases {
			err := switchTo(stamped(t, dir, "c-"+strings.ReplaceAll(tc.name, " ", "-"), tc.line), tw.server, "main")
			var off *sprint.OffBaseError
			require.True(t, errors.As(err, &off), "%s: %v", tc.name, err)
			assert.Contains(t, err.Error(), tc.why, tc.name)
			assert.Contains(t, err.Error(), "origin/main", tc.name)
			assert.Contains(t, err.Error(), "remedy: build nova-sprint from origin/main at its tip", tc.name)
			if tc.name != "never on origin" {
				assert.Contains(t, err.Error(), "built from no source commit", tc.name)
			}
			unchanged(tc.name)
		}
	})

	t.Run("a switch with no base or clone named is refused", func(t *testing.T) {
		bin := stamped(t, dir, "tip", lineAt(tw.b))
		for _, c := range [][2]string{{"", "main"}, {tw.server, ""}, {tw.server, "--upload-pack=x"}} {
			err := switchTo(bin, c[0], c[1])
			require.Error(t, err, "%v", c)
			assert.Contains(t, err.Error(), "the base check of "+bin+" could not be made", "%v", c)
			assert.Contains(t, err.Error(), "remedy: name the sprint base", "%v", c)
			unchanged("unnamed")
		}
		err := switchTo(bin, filepath.Join(dir, "no-clone"), "main")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fetching origin's main into")
		unchanged("no clone")
	})

	t.Run("a binary stamped from the base's tip is switched", func(t *testing.T) {
		on, err := sprint.CheckServerBinary(ctx, stamped(t, dir, "short", "nova-sprint 20261005000000-"+tw.a[:12]+" linux/amd64 go1.26"), tw.server, "main")
		require.NoError(t, err)
		assert.True(t, on.On, "a vcs stamp's 12 hex is the commit: %+v", on)
		assert.Equal(t, tw.a, on.Commit, "the commit is resolved in full")

		bin := stamped(t, dir, "tip", lineAt(tw.b))
		require.NoError(t, switchTo(bin, tw.server, "main"))
		got, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Contains(t, string(got), tw.b, "the candidate is the server's binary")
		prev, err := os.ReadFile(target + ".prev")
		require.NoError(t, err)
		assert.Equal(t, "the running server", string(prev))
		rec, ok, err := sprint.ReadBaseRecord(target)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, sprint.BaseRecord{Repo: tw.server, Base: "main"}, rec)
		raw, err := os.ReadFile(target + ".base.json")
		require.NoError(t, err)
		assert.True(t, json.Valid(raw))
	})

	t.Run("the tick holds one judgment while the running server is off the base", func(t *testing.T) {
		r := newAlarmRig(t)
		_, _, _, err := r.st.SetMachine(r.ctx, true)
		require.NoError(t, err)
		var mu sync.Mutex
		line := lineAt(tw.side)
		w := &sprint.BaseWatch{Repo: tw.server, Base: "main", Every: time.Nanosecond,
			Check: func(ctx context.Context) (sprint.ServerBase, error) {
				mu.Lock()
				l := line
				mu.Unlock()
				return sprint.CheckServerBase(ctx, l, tw.server, "main")
			}}
		sprint.RunningBase = w
		defer func() {
			// the last tick began a check in the background: it ends before the twin is removed
			sprint.RunningBase = nil
			assert.NoError(t, w.Wait(ctx))
		}()
		settle := func() {
			t.Helper()
			require.NoError(t, w.Wait(ctx))
			w.Fact()
			require.NoError(t, w.Wait(ctx))
			require.NoError(t, w.Err())
		}
		count := func() (written, open int) {
			notes, _, err := r.m.NotesSince(r.ctx, "", 100000)
			require.NoError(t, err)
			for _, n := range notes {
				if n.Kind == sprint.Judgment && n.Type == sprint.NServerOffBase {
					written++
					assert.Contains(t, n.What, "commit "+tw.side[:12]+", not an ancestor of origin/main")
					assert.Contains(t, n.What, "remedy: build nova-sprint from origin/main at its tip")
				}
			}
			for _, o := range r.snap().Open {
				if o.Note.Type == sprint.NServerOffBase {
					open++
				}
			}
			return written, open
		}

		settle()
		r.ticks(3)
		written, open := count()
		assert.Equal(t, 1, written, "one judgment while the server is off the base, never one a tick")
		assert.Equal(t, 1, open)

		mu.Lock()
		line = lineAt(tw.b)
		mu.Unlock()
		settle()
		r.ticks(2)
		written, open = count()
		assert.Equal(t, 1, written)
		assert.Equal(t, 0, open, "closed once the running server is back on the base")
	})
}

// The commit a version line names, read from the line alone (buildinfo.Fields.Commit).
func TestServerBaseReadsTheCommitOffTheVersionLine(t *testing.T) {
	t.Parallel()
	full := strings.Repeat("0123456789", 4)
	cases := []struct{ line, commit, why string }{
		{"nova-sprint v1.0.0 linux/amd64 go1.26 commit=" + full, full, ""},
		{"nova-sprint 20261005000000-0123456789ab linux/amd64 go1.26", "0123456789ab", ""},
		{"nova-sprint v1.0.0 linux/amd64 go1.26 repo=x revision=" + full + " dirty=false build_host=h", full, ""},
		{"nova-sprint v1.0.0 linux/amd64 go1.26 repo=x revision=" + full + " dirty=true build_host=h", "", "dirty=true"},
		{"nova-sprint v1.0.0 linux/amd64 go1.26 commit=" + full + "-dirty", "", "edited tree"},
		{"nova-sprint v1.0.0 linux/amd64 go1.26 commit=main", "", "is not a commit"},
		{"nova-sprint 20261005000000-0123456789ab-dirty linux/amd64 go1.26", "", "edited tree"},
		{"nova-sprint devel linux/amd64 go1.26", "", "names no source commit"},
	}
	for _, tc := range cases {
		f, ok := buildinfo.Parse(tc.line)
		require.True(t, ok, tc.line)
		commit, why := f.Commit()
		assert.Equal(t, tc.commit, commit, tc.line)
		if tc.why == "" {
			assert.Empty(t, why, tc.line)
		} else {
			assert.Contains(t, why, tc.why, tc.line)
		}
	}
}
