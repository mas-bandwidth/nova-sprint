package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// midDumpBackup is a twin as a backup's source that is written to after its
// dump is taken and before its state is read, as a running fleet's beats are:
// the source's dump and its state are two reads, not one.
type midDumpBackup struct {
	memBackup
	between func(ctx context.Context, st *store.Store) error
}

func (m midDumpBackup) Take(ctx context.Context) (uint64, []store.DumpKey, map[string]int, error) {
	epoch, keys, cols, err := m.memBackup.Take(ctx)
	if err != nil {
		return epoch, keys, cols, err
	}
	return epoch, keys, cols, m.between(ctx, m.st)
}

// liveFleetTwin is a twin with two members, a card and a started sprint.
func liveFleetTwin(t *testing.T) *store.Store {
	t.Helper()
	file := filepath.Join(t.TempDir(), "sprint.twin")
	for _, line := range []string{
		"nova-sprint init --readers reader-a,reader-b --members m1,m2",
		"nova-sprint add --stream s1 --count 2",
		"nova-sprint start",
		"nova-sprint tick",
	} {
		code, out, errs := twinProcess(t, file, line)
		require.Equal(t, 0, code, "%s: exit %d\n%s%s", line, code, out, errs)
	}
	b, err := os.ReadFile(file)
	require.NoError(t, err)
	m := store.NewMem()
	require.NoError(t, m.Restore(b))
	return &store.Store{B: m, Names: sprint.Names{}, Actor: "boss"}
}

func liveBackupRun(t *testing.T, st *store.Store, between func(context.Context, *store.Store) error) (backupResult, error, string) {
	t.Helper()
	for _, bin := range []string{"xz", "split"} {
		_, err := exec.LookPath(bin)
		require.NoError(t, err, "the backup runs the system's %s", bin)
	}
	self, err := os.Executable()
	require.NoError(t, err)
	out := filepath.Join(t.TempDir(), "backup")
	b := backupOut{out: out, partBytes: 600, src: midDumpBackup{memBackup{st: st}, between}, xz: "xz", split: "split",
		twin: func(context.Context, string) (backupTwin, error) { return &memTwin{names: st.Names}, nil },
		scan: backupScanner{bin: fakeSecrets(t, "nsv_FAKE_FOR_LIVE_TEST"), store: t.TempDir(), as: "tester", key: "unused", sops: "/bin/true", self: self}}
	res, err := b.run(context.Background())
	return res, err, out
}

// beatsMidDump is what a tick's showFleet and orderFleet do to the fleet table
// while the dump is being taken: display cells rewritten, rows reordered.
func beatsMidDump(ctx context.Context, st *store.Store) error {
	m := st.B.(*store.Mem)
	fleet := st.Names.Table(sprint.Fleet)
	shapes, err := m.Shapes(ctx, []string{fleet})
	if err != nil {
		return err
	}
	var order []string
	for i := len(shapes[0].Rows) - 1; i >= 0; i-- {
		order = append(order, shapes[0].Rows[i].Key)
	}
	for _, r := range shapes[0].Rows {
		if r.Texts[sprint.Status] == sprint.Held {
			continue
		}
		if err := m.RowSet(ctx, fleet, r.Key, map[string]string{sprint.Load: "31.5 beat", sprint.Status: sprint.Down}); err != nil {
			return err
		}
	}
	return m.RowsOrder(ctx, fleet, order)
}

// Beats written between the dump and the state read do not fail the backup's
// restore check (the pre-clear backup of 2026-10-10 failed on exactly this).
func TestBackupOutPassesWhileMemberBeatsRewriteTheFleet(t *testing.T) {
	t.Parallel()
	st := liveFleetTwin(t)
	res, err, out := liveBackupRun(t, st, beatsMidDump)
	require.NoError(t, err)
	assert.Contains(t, res.line, "restore=semantic compared=state+counts")
	_, serr := os.Stat(out)
	assert.NoError(t, serr, "--out is written")
}

// Reversed witness: the exact comparison does see these beats, so the passing
// test above is the live comparison's doing.
func TestBeatsMidDumpDoDifferExactly(t *testing.T) {
	t.Parallel()
	st := liveFleetTwin(t)
	before, err := store.ReadState(context.Background(), st.B, st.Names)
	require.NoError(t, err)
	require.NoError(t, beatsMidDump(context.Background(), st))
	after, err := store.ReadState(context.Background(), st.B, st.Names)
	require.NoError(t, err)
	assert.NotEmpty(t, before.Diff(after), "the beats change fleet parts")
	assert.Empty(t, before.DiffLive(after), "and nothing but beat cells")
}

// A real change written mid-dump still fails the check: a held member, and a
// card added.
func TestBackupOutStillFailsOnARealChangeMidDump(t *testing.T) {
	t.Parallel()
	t.Run("held status", func(t *testing.T) {
		t.Parallel()
		st := liveFleetTwin(t)
		res, err, out := liveBackupRun(t, st, func(ctx context.Context, st *store.Store) error {
			if err := beatsMidDump(ctx, st); err != nil {
				return err
			}
			return st.B.(*store.Mem).RowSet(ctx, st.Names.Table(sprint.Fleet), "m1", map[string]string{sprint.Status: sprint.Held})
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "the dump does not restore the sprint the store held")
		assert.Contains(t, err.Error(), "row "+sprint.Fleet+" m1")
		assert.Empty(t, res.line)
		_, serr := os.Stat(out)
		assert.True(t, os.IsNotExist(serr), "a failed check writes no --out")
	})
	t.Run("card added", func(t *testing.T) {
		t.Parallel()
		st := liveFleetTwin(t)
		_, err, out := liveBackupRun(t, st, func(ctx context.Context, st *store.Store) error {
			if err := beatsMidDump(ctx, st); err != nil {
				return err
			}
			res, err := st.Run(ctx, store.AddStep(sprint.AddReq{Stream: "s9", Count: 1, Brief: "c: a card added mid-dump\nREPO: mas-bandwidth/nova-tools\n\nThe task."}))
			if err != nil {
				return err
			}
			require.Empty(t, res.Refused)
			return nil
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "the dump does not restore the sprint the store held")
		_, serr := os.Stat(out)
		assert.True(t, os.IsNotExist(serr))
	})
}
