package sprint_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func lineAt(commit string) string {
	return "nova-sprint v9.9.9 linux/amd64 go1.26 commit=" + commit
}

// TestCheckServerBaseRefusesACommitOffTheSprintBase: the server is built from the sprint base
// and nothing else (docs/SPEC-SPRINT.md section 14, "server-from-base-only-w-ns-bb.w1"): the
// commit a version line names must be an ancestor of origin's base, fetched, by git merge-base
// --is-ancestor. A side-branch commit is refused naming the base's fetched tip; a commit that
// is on the base (the tip, and an ancestor of it) is On; a line that names no commit, and a
// build from an edited tree, are not On; and a check with no base or clone is an error.
func TestCheckServerBaseRefusesACommitOffTheSprintBase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tw := newBaseTwin(t)

	t.Run("a side-branch commit is off the base", func(t *testing.T) {
		got, err := sprint.CheckServerBase(ctx, lineAt(tw.side), tw.server, "main")
		require.NoError(t, err)
		assert.False(t, got.On)
		assert.Equal(t, tw.b, got.Tip, "the tip is the fetched one, not the clone's stale main")
		assert.Contains(t, got.Why, "its commit "+tw.side[:12]+" is not an ancestor of origin/main")
	})

	t.Run("the base's tip and its ancestors are on the base", func(t *testing.T) {
		tip, err := sprint.CheckServerBase(ctx, lineAt(tw.b), tw.server, "main")
		require.NoError(t, err)
		assert.True(t, tip.On, "%+v", tip)
		assert.Equal(t, tw.b, tip.Commit)
		anc, err := sprint.CheckServerBase(ctx, lineAt(tw.a), tw.server, "main")
		require.NoError(t, err)
		assert.True(t, anc.On, "an ancestor of the base tip is on the base")
	})

	t.Run("no source commit is not on the base", func(t *testing.T) {
		cases := []struct{ line, why string }{
			{"nova-sprint devel linux/amd64 go1.26", "names no source commit"},
			{"nova-sprint v1.0.0 linux/amd64 go1.26", "names no source commit"},
			{"nova-sprint " + tw.b[:12] + "-dirty linux/amd64 go1.26", "edited tree"},
			{"nova-sprint 20261005000000-" + tw.b[:12] + "-dirty linux/amd64 go1.26", "edited tree"},
			{"usage: nova-sprint <verb>", "is not a version line"},
			{lineAt(strings.Repeat("ab", 20)), "not in origin/main's history"},
		}
		for _, tc := range cases {
			got, err := sprint.CheckServerBase(ctx, tc.line, tw.server, "main")
			require.NoError(t, err, tc.line)
			assert.False(t, got.On, tc.line)
			assert.Contains(t, got.Why, tc.why, tc.line)
		}
	})

	t.Run("a check with no base or clone is an error", func(t *testing.T) {
		for _, c := range [][2]string{{"", "main"}, {tw.server, ""}, {tw.server, "--upload-pack=x"}} {
			_, err := sprint.CheckServerBase(ctx, lineAt(tw.b), c[0], c[1])
			require.Error(t, err, "%v", c)
		}
	})
}

// TestSwitchFromBaseRefusesOffBaseAndRecordsTheBase: SwitchFromBase refuses an off-base
// candidate with an *OffBaseError and nothing on disk changed, and switches one from the base's
// tip, recording the clone and base beside the target (docs/SPEC-SPRINT.md section 14,
// "server-from-base-only-w-ns-bb.w1").
func TestSwitchFromBaseRefusesOffBaseAndRecordsTheBase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tw := newBaseTwin(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "nova-sprint")
	require.NoError(t, os.WriteFile(target, []byte("the running server"), 0o755))

	stamped := func(name, commit string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\necho '"+lineAt(commit)+"'\n"), 0o755))
		return p
	}

	off := stamped("side", tw.side)
	err := sprint.SwitchFromBase(ctx, sprint.ServerSwitchOptions{Binary: off, Target: target, Repo: tw.server, Base: "main", Rollback: true})
	var oe *sprint.OffBaseError
	require.ErrorAs(t, err, &oe)
	kept, rerr := os.ReadFile(target)
	require.NoError(t, rerr)
	assert.Equal(t, "the running server", string(kept), "the old binary is in place")
	for _, side := range []string{".prev", ".switch.json", ".base.json"} {
		_, serr := os.Stat(target + side)
		assert.True(t, os.IsNotExist(serr), "nothing written beside the target (%s)", side)
	}

	on := stamped("tip", tw.b)
	require.NoError(t, sprint.SwitchFromBase(ctx, sprint.ServerSwitchOptions{Binary: on, Target: target, Repo: tw.server, Base: "main", Rollback: true}))
	got, rerr := os.ReadFile(target)
	require.NoError(t, rerr)
	assert.Contains(t, string(got), lineAt(tw.b), "the candidate is the server's binary")
	rec, ok, rerr := sprint.ReadBaseRecord(target)
	require.NoError(t, rerr)
	require.True(t, ok)
	assert.Equal(t, sprint.BaseRecord{Repo: tw.server, Base: "main"}, rec)
}
