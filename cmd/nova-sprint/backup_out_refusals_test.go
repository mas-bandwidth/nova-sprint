package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// (5)-(11) drive backupOut.run directly against a twin store the way
// backup_out_test.go and backup_out_live_test.go build it, one broken field a
// test (backup_out.go run; docs/SPEC-SPRINT.md, sprint-backup-out).

// backupOutRefusal is a working backupOut over a live twin store: every step
// passes until the test breaks one field.
func backupOutRefusal(t *testing.T) (*backupOut, *store.Store) {
	t.Helper()
	for _, bin := range []string{"xz", "split"} {
		_, err := exec.LookPath(bin)
		require.NoError(t, err, "the backup runs the system's %s", bin)
	}
	st := liveFleetTwin(t)
	self, err := os.Executable()
	require.NoError(t, err)
	b := &backupOut{
		out:       filepath.Join(t.TempDir(), "backup"),
		partBytes: 600,
		src:       memBackup{st: st},
		xz:        "xz",
		split:     "split",
		twin:      func(context.Context, string) (backupTwin, error) { return &memTwin{names: st.Names}, nil },
		scan:      backupScanner{bin: fakeSecrets(t, "nsv_FAKE_REFUSAL"), store: t.TempDir(), as: "tester", key: "unused", sops: "/bin/true", self: self},
	}
	return b, st
}

// assertNoBackupLeft says a refused backup wrote no --out and left no work
// directory beside it (backup_out.go: "a failed backup writes no --out").
func assertNoBackupLeft(t *testing.T, out string) {
	t.Helper()
	_, err := os.Stat(out)
	assert.True(t, os.IsNotExist(err), "--out %s is not written", out)
	es, err := os.ReadDir(filepath.Dir(out))
	require.NoError(t, err)
	for _, e := range es {
		assert.NotContains(t, e.Name(), ".nova-sprint-backup-", "no work directory is left beside --out")
	}
}

// (5) --out naming a regular file cannot be read: refused, and the file is left
// as it is (backup_out.go run's os.ReadDir of --out).
func TestBackupOutRefusesAnOutThatIsNotADirectory(t *testing.T) {
	t.Parallel()
	b, _ := backupOutRefusal(t)
	b.out = filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(b.out, []byte("keep me\n"), 0o644))

	_, err := b.run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be read")
	fi, serr := os.Stat(b.out)
	require.NoError(t, serr, "the file is left as it is")
	assert.True(t, fi.Mode().IsRegular())
	es, rerr := os.ReadDir(filepath.Dir(b.out))
	require.NoError(t, rerr)
	for _, e := range es {
		assert.NotContains(t, e.Name(), ".nova-sprint-backup-", "no work directory is left beside --out")
	}
}

// (6) The source's dump fails: the error wraps the store's own, and nothing is
// left (backup_out.go run's "the store gave no dump").
func TestBackupOutRefusesWhenTheSourceGivesNoDump(t *testing.T) {
	t.Parallel()
	b, _ := backupOutRefusal(t)
	b.src = dumpFails{err: errors.New("the store is gone")}

	_, err := b.run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the store gave no dump")
	assert.Contains(t, err.Error(), "the store is gone")
	assertNoBackupLeft(t, b.out)
}

// dumpFails is a backup source whose dump always fails.
type dumpFails struct{ err error }

func (d dumpFails) Take(context.Context) (uint64, []store.DumpKey, map[string]int, error) {
	return 0, nil, nil, d.err
}

// (7) xz or split set to /bin/false: refused naming the failed step and the flag
// to fix it (backup_out.go run's "xz -9 failed: ...; run: ... --xz" and "split
// failed: ...; run: ... --split").
func TestBackupOutRefusesWhenXzOrSplitFails(t *testing.T) {
	t.Parallel()
	t.Run("xz", func(t *testing.T) {
		t.Parallel()
		b, _ := backupOutRefusal(t)
		b.xz = "/bin/false"
		_, err := b.run(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "xz -9 failed")
		assert.Contains(t, err.Error(), "--xz")
		assertNoBackupLeft(t, b.out)
	})
	t.Run("split", func(t *testing.T) {
		t.Parallel()
		b, _ := backupOutRefusal(t)
		b.split = "/bin/false"
		_, err := b.run(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "split failed")
		assert.Contains(t, err.Error(), "--split")
		assertNoBackupLeft(t, b.out)
	})
}

// (8) No throwaway store, and a twin whose restore holds other counts: each is
// refused with its words and nothing is left (backup_out.go run's "no throwaway
// store to restore into" and "the dump restored with other counts than the store
// held").
func TestBackupOutRefusesABadThrowawayOrOtherCounts(t *testing.T) {
	t.Parallel()
	t.Run("no twin", func(t *testing.T) {
		t.Parallel()
		b, _ := backupOutRefusal(t)
		b.twin = func(context.Context, string) (backupTwin, error) {
			return nil, errors.New("no redis-server on this machine")
		}
		_, err := b.run(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no throwaway store to restore into")
		assertNoBackupLeft(t, b.out)
	})
	t.Run("other counts", func(t *testing.T) {
		t.Parallel()
		b, st := backupOutRefusal(t)
		b.twin = func(context.Context, string) (backupTwin, error) {
			return otherCountsTwin{&memTwin{names: st.Names}}, nil
		}
		_, err := b.run(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "the dump restored with other counts than the store held")
		assertNoBackupLeft(t, b.out)
	})
}

// otherCountsTwin is a twin whose restore reports counts other than the source
// held: the count comparison is what fails.
type otherCountsTwin struct{ *memTwin }

func (otherCountsTwin) Restore(context.Context, []store.DumpKey) (store.BackupCounts, error) {
	return store.BackupCounts{Keys: 1 << 20, Columns: map[string]int{}}, nil
}

// (9) No nova-secrets seat for the scan: refused "the secret scan did not run"
// (backup_out.go run's wrap of sealedNames).
func TestBackupOutRefusesWhenTheSecretScanCannotRun(t *testing.T) {
	t.Parallel()
	b, _ := backupOutRefusal(t)
	b.scan = backupScanner{} // no bin, store, as, key or sops

	_, err := b.run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the secret scan did not run")
	assertNoBackupLeft(t, b.out)
}

// (10) A source with no state reader is proved on counts only: the result says
// restore=integrity compared=counts, the README says the counts are not a
// semantic restore (backup_out.go run's stateSource path; backupReadme).
func TestBackupOutSaysCountsOnlyWhenTheSourceHasNoState(t *testing.T) {
	t.Parallel()
	b, st := backupOutRefusal(t)
	b.src = countsOnlySource{memBackup{st: st}}

	res, err := b.run(context.Background())
	require.NoError(t, err)
	assert.Contains(t, res.line, "restore=integrity compared=counts")
	assert.Contains(t, res.line, "not a semantic restore")

	readme, err := os.ReadFile(filepath.Join(b.out, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "the counts are not a semantic restore")
}

// countsOnlySource is a backup source with no State reader: it satisfies
// backupSource but not stateSource, so the restore is compared on counts only.
type countsOnlySource struct{ backupSource }

// (11) An existing empty --out is replaced, and the backup succeeds (backup_out.go
// run's os.Remove of the empty --out then Rename).
func TestBackupOutReplacesAnEmptyOutAndSucceeds(t *testing.T) {
	t.Parallel()
	b, _ := backupOutRefusal(t)
	require.NoError(t, os.Mkdir(b.out, 0o755))

	res, err := b.run(context.Background())
	require.NoError(t, err)
	assert.Contains(t, res.line, "BACKUP OK out="+b.out+" ")
	readme, err := os.ReadFile(filepath.Join(b.out, "README.md"))
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(readme), "Sprint backup, epoch "), "the README is written")
	_, err = os.Stat(filepath.Join(b.out, "SHA256SUMS"))
	assert.NoError(t, err, "the sums are written")
}
