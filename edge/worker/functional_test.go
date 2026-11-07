package worker

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func freePort_internal(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	require.NoError(t, l.Close())
	return port
}

func pgBin_internal(t *testing.T) string {
	if p, err := exec.LookPath("initdb"); err == nil {
		return filepath.Dir(p)
	}
	found, _ := filepath.Glob("/opt/homebrew/opt/postgresql@18/bin/initdb")
	debian, _ := filepath.Glob("/usr/lib/postgresql/*/bin/initdb")
	if found = append(found, debian...); len(found) > 0 {
		return filepath.Dir(found[0])
	}
	t.Skip("functional tests need PostgreSQL initdb")
	return ""
}

func startFunctionalPG_internal(t *testing.T) *store.PG {
	t.Helper()
	bin, dir := pgBin_internal(t), t.TempDir()
	initCmd := exec.Command(filepath.Join(bin, "initdb"), "-D", filepath.Join(dir, "data"), "-U", "nova", "--auth=trust", "-E", "UTF8", "--no-locale")
	initCmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := initCmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	port := freePort_internal(t)
	cmd := exec.Command(filepath.Join(bin, "postgres"), "-D", filepath.Join(dir, "data"), "-p", port, "-c", "listen_addresses=127.0.0.1", "-c", "unix_socket_directories=", "-c", "fsync=off")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Signal(syscall.SIGQUIT); _ = cmd.Wait() })
	dsn := "postgres://nova@127.0.0.1:" + port + "/postgres?sslmode=disable"
	require.Eventually(t, func() bool {
		c, err := pgx.Connect(context.Background(), dsn)
		if err == nil {
			_ = c.Close(context.Background())
		}
		return err == nil
	}, 30*time.Second, 50*time.Millisecond)
	p, err := store.Open(context.Background(), dsn, "")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	require.NoError(t, p.Migrate(context.Background()))
	return p
}

func runGitCmd_internal(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Nova Test",
		"GIT_AUTHOR_EMAIL=test@nova.local",
		"GIT_COMMITTER_NAME=Nova Test",
		"GIT_COMMITTER_EMAIL=test@nova.local",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v in %s: %s", args, dir, string(out))
	return strings.TrimSpace(string(out))
}

type gitBenchDirs struct {
	originDir string
	mirrorDir string
	workClone string
	seedClone string
	baseSHA   string
}

func setupGitBench_internal(t *testing.T) gitBenchDirs {
	t.Helper()
	root := t.TempDir()
	originDir := filepath.Join(root, "origin.git")
	mirrorDir := filepath.Join(root, "mirror.git")
	workClone := filepath.Join(root, "work-clone")
	seedClone := filepath.Join(root, "seed-clone")

	runGitCmd_internal(t, root, "init", "--bare", originDir)
	runGitCmd_internal(t, root, "clone", originDir, seedClone)
	require.NoError(t, os.WriteFile(filepath.Join(seedClone, "README.md"), []byte("hello\n"), 0o644))
	runGitCmd_internal(t, seedClone, "add", "README.md")
	runGitCmd_internal(t, seedClone, "commit", "-m", "initial")
	baseSHA := runGitCmd_internal(t, seedClone, "rev-parse", "HEAD")
	runGitCmd_internal(t, seedClone, "push", "origin", "HEAD:refs/heads/sprint/s1/group")

	runGitCmd_internal(t, root, "clone", "--bare", originDir, mirrorDir)
	runGitCmd_internal(t, root, "clone", originDir, workClone)
	altFile := filepath.Join(workClone, ".git", "objects", "info", "alternates")
	require.NoError(t, os.MkdirAll(filepath.Dir(altFile), 0o755))
	require.NoError(t, os.WriteFile(altFile, []byte(filepath.Join(mirrorDir, "objects")+"\n"), 0o644))

	return gitBenchDirs{
		originDir: originDir,
		mirrorDir: mirrorDir,
		workClone: workClone,
		seedClone: seedClone,
		baseSHA:   baseSHA,
	}
}

func seedFunctionalWork_internal(t *testing.T, pg *store.PG, originDir, baseSHA string) {
	t.Helper()
	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	row := "b1.script"
	rec, err := pg.Apply(ctx, store.Op{
		ID: "seed-1", Epoch: 1, Actor: "test", Kind: "seed", At: now,
		Writes: []store.Write{
			store.PutStream{Name: "s1", Repo: originDir, Base: "main", Pos: 1},
			store.PutRow{Name: row},
			store.SetRowState{Row: row, State: "up"},
			store.PutCard{
				ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "normal",
				Brief: "brief", Prose: "implement work.txt", Paths: []string{"work.txt"},
				DoneWhen: []string{"test -f work.txt"},
			},
			store.SetCardState{Card: "p1", State: "ready"},
		},
	})
	require.NoError(t, err)
	require.True(t, rec.Accepted, "seed-1 refused: %s", rec.Reason)

	snap, err := pg.Snapshot(ctx)
	require.NoError(t, err)
	p := card.FromStore(snap.Cards["p1"])
	r := card.RowFromStore(snap.Rows[row], 1, []string{"flash", "pro"})
	s := card.StreamFromStore(snap.Streams["s1"])
	plan, ref := card.Move(p, nil, map[string]card.Row{row: r}, s, nil, card.Start{
		Row: row, Branch: "sprint/s1/p1", Base: baseSHA,
	})
	require.Nil(t, ref)
	rec, err = pg.Apply(ctx, store.Op{
		ID: "start-p1", Epoch: 1, Actor: "test", Kind: "start", At: now.Add(time.Second),
		Expect: plan.Expect, Writes: plan.Writes,
	})
	require.NoError(t, err)
	require.True(t, rec.Accepted, "start-p1 refused: %s", rec.Reason)
}

func TestFunctionalRunnerEndToEndWithRebase(t *testing.T) {
	t.Parallel()
	pg := startFunctionalPG_internal(t)
	gb := setupGitBench_internal(t)
	seedFunctionalWork_internal(t, pg, gb.originDir, gb.baseSHA)

	// Advance sprint/s1 on origin with a non-conflicting commit so Step 6 rebases onto moved stream head.
	require.NoError(t, os.WriteFile(filepath.Join(gb.seedClone, "other.txt"), []byte("other\n"), 0o644))
	runGitCmd_internal(t, gb.seedClone, "add", "other.txt")
	runGitCmd_internal(t, gb.seedClone, "commit", "-m", "stream advance")
	movedStreamSHA := runGitCmd_internal(t, gb.seedClone, "rev-parse", "HEAD")
	runGitCmd_internal(t, gb.seedClone, "push", "origin", "HEAD:refs/heads/sprint/s1/group")

	gitOps := &OSGitOps{WorkCloneDir: gb.workClone, MirrorDir: gb.mirrorDir, OriginURL: gb.originDir}
	r, err := NewRunner(Runner{CoordinatorHosts: testCoordinators_internal,
		Host:      "b1",
		RootDir:   t.TempDir(),
		Sprint:    &StoreSprintClient{Store: pg, Origin: gitOps},
		Git:       gitOps,
		Wall:      &OSReserveWall{},
		Harnesses: map[string]Harness{"script": &ScriptHarness{}},
		Now:       time.Now, Sleep: time.Sleep,
	})
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, r.ProbeRow(ctx, "b1.script", "script", "script-v1"))
	require.NoError(t, r.StepRow(ctx, "b1.script"))

	snap, err := pg.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, "review", snap.Cards["p1"].State)
	assert.Equal(t, "done", snap.Cards["p1.w1"].State)
	assert.Equal(t, "ok", snap.Cards["p1.w1"].Result)
	assert.NotEmpty(t, snap.Cards["p1"].Head)
	assert.NotEqual(t, movedStreamSHA, snap.Cards["p1"].Head)

	parentSHA := runGitCmd_internal(t, gb.originDir, "rev-parse", "refs/heads/sprint/s1/p1^")
	assert.Equal(t, movedStreamSHA, parentSHA)
	_, err = os.Stat(filepath.Join(r.RootDir, "jobs", "p1.w1"))
	assert.True(t, os.IsNotExist(err), "job directory must be cleaned after ok report")
}

func TestFunctionalRunnerRebaseConflict(t *testing.T) {
	t.Parallel()
	pg := startFunctionalPG_internal(t)
	gb := setupGitBench_internal(t)
	seedFunctionalWork_internal(t, pg, gb.originDir, gb.baseSHA)

	// Advance sprint/s1 on origin with a conflicting edit to work.txt.
	require.NoError(t, os.WriteFile(filepath.Join(gb.seedClone, "work.txt"), []byte("stream conflict version\n"), 0o644))
	runGitCmd_internal(t, gb.seedClone, "add", "work.txt")
	runGitCmd_internal(t, gb.seedClone, "commit", "-m", "conflicting work.txt")
	runGitCmd_internal(t, gb.seedClone, "push", "origin", "HEAD:refs/heads/sprint/s1/group")

	gitOps := &OSGitOps{WorkCloneDir: gb.workClone, MirrorDir: gb.mirrorDir, OriginURL: gb.originDir}
	r, err := NewRunner(Runner{CoordinatorHosts: testCoordinators_internal,
		Host:      "b1",
		RootDir:   t.TempDir(),
		Sprint:    &StoreSprintClient{Store: pg, Origin: gitOps},
		Git:       gitOps,
		Wall:      &OSReserveWall{},
		Harnesses: map[string]Harness{"script": &ScriptHarness{}},
		Now:       time.Now, Sleep: time.Sleep,
	})
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, r.ProbeRow(ctx, "b1.script", "script", "script-v1"))
	require.NoError(t, r.StepRow(ctx, "b1.script"))

	snap, err := pg.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, "ready", snap.Cards["p1"].State)
	assert.Equal(t, 1, snap.Cards["p1"].Bounces)
	assert.Equal(t, "done", snap.Cards["p1.w1"].State)
	assert.Equal(t, "fail", snap.Cards["p1.w1"].Result)
	assert.Contains(t, snap.Cards["p1.w1"].Reason, "work.txt")
}
