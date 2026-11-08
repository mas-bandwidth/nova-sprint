package worker

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
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
	// pg_ctl starts it and stops it (-m fast: a clean shutdown; the postmaster stops its
	// children and frees its SysV shared memory before it exits).
	ctl := filepath.Join(bin, "pg_ctl")
	start := exec.Command(ctl, "-D", filepath.Join(dir, "data"), "-l", filepath.Join(dir, "postgres.log"), "-o", "-p "+port+" -c listen_addresses=127.0.0.1 -c unix_socket_directories= -c fsync=off", "-w", "start")
	start.Env = append(os.Environ(), "LC_ALL=C")
	// Stop is registered before start: a start that fails or times out can
	// leave a postmaster running, and t.Fatalf must not orphan it.
	t.Cleanup(func() {
		stop := exec.Command(ctl, "-D", filepath.Join(dir, "data"), "-m", "fast", "-w", "stop")
		stop.Env = append(os.Environ(), "LC_ALL=C")
		if out, err := stop.CombinedOutput(); err != nil {
			t.Logf("pg_ctl stop: %v\n%s", err, out)
		}
	})
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("pg_ctl start: %v\n%s", err, out)
	}
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
	seedClone string
	baseSHA   string
}

func setupGitBench_internal(t *testing.T) gitBenchDirs {
	t.Helper()
	root := t.TempDir()
	originDir := filepath.Join(root, "origin.git")
	seedClone := filepath.Join(root, "seed-clone")

	runGitCmd_internal(t, root, "init", "--bare", originDir)
	runGitCmd_internal(t, root, "clone", originDir, seedClone)
	require.NoError(t, os.WriteFile(filepath.Join(seedClone, "README.md"), []byte("hello\n"), 0o644))
	runGitCmd_internal(t, seedClone, "add", "README.md")
	runGitCmd_internal(t, seedClone, "commit", "-m", "initial")
	baseSHA := runGitCmd_internal(t, seedClone, "rev-parse", "HEAD")
	runGitCmd_internal(t, seedClone, "push", "origin", "HEAD:refs/heads/sprint/s1/group")

	// The worker makes the mirror and the work clone on first use.

	return gitBenchDirs{
		originDir: originDir,
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

	gitOps := &OSGitOps{WorkRoot: t.TempDir(), MirrorRoot: t.TempDir()}
	r, err := NewRunner(Runner{
		Host:      "b1",
		RootDir:   t.TempDir(),
		Sprint:    &StoreSprintClient{Store: pg, Origin: gitOps},
		Git:       gitOps,
		Wall:      &OSReserveWall{Config: WallConfig{WorkRoot: gitOps.WorkRoot, MirrorRoot: gitOps.MirrorRoot}},
		Harnesses: map[string]Harness{"script": &ScriptHarness{}},
		Now:       time.Now, Sleep: time.Sleep,
	})
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, r.ProbeRow(ctx, "b1.script", "script"))
	require.NoError(t, r.StepRow(ctx, "b1.script"))

	snap, err := pg.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, "review", snap.Cards["p1"].State)
	assert.Equal(t, "done", snap.Cards["p1.w1"].State)
	assert.Equal(t, "ok", snap.Cards["p1.w1"].Result)
	assert.NotEmpty(t, snap.Cards["p1"].Head)
	assert.NotEqual(t, movedStreamSHA, snap.Cards["p1"].Head)

	assert.Equal(t, snap.Cards["p1"].Head, runGitCmd_internal(t, gb.originDir, "rev-parse", "refs/heads/sprint/s1/p1"), "the job's rebased head is on origin's branch")
	// The worker's git reads no global or system config: the rebase's committer is the job's identity, set in the worker's clone.
	assert.Equal(t, runGitCmd_internal(t, gb.originDir, "log", "-1", "--format=%an <%ae>", snap.Cards["p1"].Head),
		runGitCmd_internal(t, gb.originDir, "log", "-1", "--format=%cn <%ce>", snap.Cards["p1"].Head))
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

	gitOps := &OSGitOps{WorkRoot: t.TempDir(), MirrorRoot: t.TempDir()}
	r, err := NewRunner(Runner{
		Host:      "b1",
		RootDir:   t.TempDir(),
		Sprint:    &StoreSprintClient{Store: pg, Origin: gitOps},
		Git:       gitOps,
		Wall:      &OSReserveWall{Config: WallConfig{WorkRoot: gitOps.WorkRoot, MirrorRoot: gitOps.MirrorRoot}},
		Harnesses: map[string]Harness{"script": &ScriptHarness{}},
		Now:       time.Now, Sleep: time.Sleep,
	})
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, r.ProbeRow(ctx, "b1.script", "script"))
	require.NoError(t, r.StepRow(ctx, "b1.script"))

	snap, err := pg.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, "ready", snap.Cards["p1"].State)
	assert.Equal(t, 1, snap.Cards["p1"].Bounces)
	assert.Equal(t, "done", snap.Cards["p1.w1"].State)
	assert.Equal(t, "fail", snap.Cards["p1.w1"].Result)
	assert.Contains(t, snap.Cards["p1.w1"].Reason, "work.txt")
}

// TestFunctionalLateWriterCannotReachThePush (functional tier: real git,
// the real wall) is the round-4 reader's race: a writer that outlives the
// harness rewrites the job's .git/config (a clean filter that touches a
// marker outside the job directory, origin's push URL and an insteadOf
// pointed at another repository) before the seal step. The seal step runs
// that config inside the wall; the worker's push, from its own clone,
// lands the job's commits (the left tree committed too) on origin's branch
// and nothing on the other repository, and the marker never appears. A
// plain git add in the job's tree runs the filter (the control).
func TestFunctionalLateWriterCannotReachThePush(t *testing.T) {
	t.Parallel()
	gb := setupGitBench_internal(t)
	root := t.TempDir()
	g := &OSGitOps{WorkRoot: t.TempDir(), MirrorRoot: t.TempDir()}
	r, err := NewRunner(Runner{Host: "b1", RootDir: root, Git: g, Wall: &OSReserveWall{Config: WallConfig{WorkRoot: g.WorkRoot, MirrorRoot: g.MirrorRoot}}, Now: time.Now, Sleep: time.Sleep})
	require.NoError(t, err)
	ctx := context.Background()
	jobDir := filepath.Join(root, "jobs", "p1.w1")
	wt := filepath.Join(jobDir, "repo")
	j := Job{Repo: gb.originDir, Kind: "work", Branch: "sprint/s1/p1", BaseSHA: gb.baseSHA, Consumer: "p1.w1", Row: "b1.script",
		JobDir: jobDir, WorktreeDir: wt, CommitName: testCommitName, CommitEmail: testCommitEmail}
	require.NoError(t, g.Fetch(ctx, j))
	_, err = g.StageWorktree(ctx, j)
	require.NoError(t, err)
	require.NoError(t, r.keepRemote_internal(TakeInfo{Consumer: j.Consumer, Repo: gb.originDir}))
	rec := JobRecord{Epoch: 1, Consumer: j.Consumer, Kind: "work", Row: j.Row, Repo: gb.originDir, Branch: j.Branch, BaseSHA: gb.baseSHA, Gen: 1, StartTipRead: true}

	// The harness: one commit, and a file left uncommitted.
	require.NoError(t, os.WriteFile(filepath.Join(wt, "work.txt"), []byte("work\n"), 0o644))
	runGitCmd_internal(t, wt, "add", "work.txt")
	runGitCmd_internal(t, wt, "commit", "-q", "-m", "work")
	require.NoError(t, os.WriteFile(filepath.Join(wt, "left.txt"), []byte("left\n"), 0o644))

	// The late writer.
	evil := filepath.Join(t.TempDir(), "evil.git")
	runGitCmd_internal(t, root, "init", "-q", "--bare", evil)
	marker := filepath.Join(t.TempDir(), "MARKER")
	runGitCmd_internal(t, wt, "config", "filter.x.clean", "sh -c 'touch "+marker+"; cat'")
	require.NoError(t, os.WriteFile(filepath.Join(wt, ".gitattributes"), []byte("* filter=x\n"), 0o644))
	runGitCmd_internal(t, wt, "config", "remote.origin.pushurl", evil)
	runGitCmd_internal(t, wt, "config", "url."+evil+".insteadOf", gb.originDir)

	out := r.harvestWorkHead_internal(ctx, j, &rec, Outcome{Result: "ok"})
	require.Equal(t, "ok", out.Result, "%+v", out)
	require.Equal(t, out.Head, runGitCmd_internal(t, gb.originDir, "rev-parse", "refs/heads/sprint/s1/p1"), "the job's head is on origin's branch")
	runGitCmd_internal(t, gb.originDir, "cat-file", "-e", out.Head+":left.txt") // the seal step committed the left tree
	assert.Equal(t, testCommitName+" <"+testCommitEmail+">", runGitCmd_internal(t, gb.originDir, "log", "-1", "--format=%an <%ae>", out.Head), "the seal step commits as the job's identity")
	runGitCmd_internal(t, gb.originDir, "merge-base", "--is-ancestor", gb.baseSHA, out.Head)
	require.Empty(t, runGitCmd_internal(t, evil, "for-each-ref"), "nothing reached the late writer's repository")
	_, statErr := os.Stat(marker)
	require.True(t, os.IsNotExist(statErr), "the late writer's filter ran outside the wall")

	require.NoError(t, os.WriteFile(filepath.Join(wt, "control.txt"), []byte("c\n"), 0o644))
	runGitCmd_internal(t, wt, "add", "control.txt") // the control: plain git runs it
	_, statErr = os.Stat(marker)
	require.NoError(t, statErr, "the control: plain git add should have run the filter, so the case is live")
}

// TestFunctionalRestartActsOnlyOnTheKeptRecord (functional tier: real git,
// the real wall) is the round-5 reader's case: a writer that escapes the
// harness's process group rewrites the job directory's job.json to push to
// main, by lease from main's tip, and the worker restarts. The restart acts
// only on the record it kept outside the job directory (RecordsDir), and a
// job directory whose copy differs is refused and said: nothing reaches
// main. The control, with no late writer, releases the job and pushes its
// commit to the job's own branch, so the case is live.
func TestFunctionalRestartActsOnlyOnTheKeptRecord(t *testing.T) {
	t.Parallel()
	// control: the kept record is acted on; tampered: a late writer sets
	// job.json to main; pre-upgrade: a job taken before kept copies. Both
	// last are set aside to quarantine, their harness killed, nothing pushed.
	for _, kase := range []string{"control", "tampered", "pre-upgrade"} {
		t.Run(kase, func(t *testing.T) {
			gb := setupGitBench_internal(t)
			runGitCmd_internal(t, gb.seedClone, "push", "origin", "HEAD:refs/heads/main")
			clock := testClock_internal()
			st, row := seedFakeStore_internal(t, clock)
			g := &OSGitOps{WorkRoot: t.TempDir(), MirrorRoot: t.TempDir()}
			sc := &StoreSprintClient{Store: st, Origin: g, Now: clock}
			ctx := context.Background()
			taken, err := sc.Take(ctx, row, "p1.w1", 0)
			require.NoError(t, err)
			root := t.TempDir()
			jobDir := filepath.Join(root, "jobs", "p1.w1")
			j := Job{Repo: gb.originDir, Kind: "work", Branch: taken.Branch, BaseSHA: gb.baseSHA, Consumer: "p1.w1", Row: row,
				JobDir: jobDir, WorktreeDir: filepath.Join(jobDir, "repo"), CommitName: testCommitName, CommitEmail: testCommitEmail}
			require.NoError(t, g.Fetch(ctx, j))
			_, err = g.StageWorktree(ctx, j)
			require.NoError(t, err)
			rec := JobRecord{Epoch: 1, Consumer: "p1.w1", Producer: "p1", Kind: "work", Row: row, Stream: "s1", Repo: gb.originDir,
				Branch: taken.Branch, BaseSHA: gb.baseSHA, Gen: taken.Gen, Attempt: 1, State: JobStaged, StartTipRead: true, PID: 4242, Nonce: "n1"}
			writeStagedRecord_internal(t, jobDir, rec) // no receipt: the restart releases it, its commits pushed first

			// The harness's commit.
			require.NoError(t, os.WriteFile(filepath.Join(j.WorktreeDir, "work.txt"), []byte("work\n"), 0o644))
			runGitCmd_internal(t, j.WorktreeDir, "add", "work.txt")
			runGitCmd_internal(t, j.WorktreeDir, "commit", "-q", "-m", "work")
			switch kase {
			case "tampered": // the late writer: main, by lease from main's tip
				evil := rec
				evil.Branch, evil.StartTip = "main", gb.baseSHA
				data, err := json.Marshal(evil)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(jobDir, "job.json"), append(data, '\n'), 0o600))
			case "pre-upgrade":
				require.NoError(t, os.RemoveAll(filepath.Join(root, RecordsDir)))
			}

			var warns []string
			procs := &fakeProcTable{alive: map[string]bool{"4242:n1": true}}
			r, err := NewRunner(Runner{Host: "b1", RootDir: root, Sprint: sc, Git: g, Procs: procs,
				Wall: &OSReserveWall{Config: WallConfig{WorkRoot: g.WorkRoot, MirrorRoot: g.MirrorRoot}}, Now: clock, Sleep: time.Sleep,
				Warn: func(s string) { warns = append(warns, s) }})
			require.NoError(t, err)
			require.NoError(t, r.RecoverRow(ctx, row))

			assert.Equal(t, gb.baseSHA, runGitCmd_internal(t, gb.originDir, "rev-parse", "refs/heads/main"), "%s: main moved", kase)
			branch := runGitCmd_internal(t, gb.originDir, "for-each-ref", "--format=%(objectname)", "refs/heads/"+taken.Branch)
			if kase != "control" {
				said := strings.Join(warns, "\n")
				assert.Empty(t, branch, "%s: a refused job pushes nothing", kase)
				assert.Contains(t, said, "not the one the worker kept outside it", "%s: the refusal is said", kase)
				assert.Contains(t, said, "quarantine", "%s: the quarantine is said", kase)
				assert.NoDirExists(t, jobDir, "%s: the job directory is set aside", kase)
				assert.FileExists(t, filepath.Join(r.quarantineDir_internal("p1.w1", taken.Gen), "job.json"), kase)
				assert.Equal(t, []int{4242}, procs.killed, "%s: its harness is killed", kase)
				assert.NoDirExists(t, filepath.Join(root, RecordsDir, "p1.w1"), kase)
				require.NoError(t, r.RecoverRow(ctx, row)) // the next pass leaves it set aside
				assert.Empty(t, runGitCmd_internal(t, gb.originDir, "for-each-ref", "--format=%(objectname)", "refs/heads/"+taken.Branch), kase)
			} else {
				require.NotEmpty(t, branch, "the control: the release pushes the job's commit to its own branch (warnings %q)", warns)
				runGitCmd_internal(t, gb.originDir, "cat-file", "-e", branch+":work.txt")
			}
		})
	}
}
