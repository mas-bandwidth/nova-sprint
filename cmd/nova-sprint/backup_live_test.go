package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// midSaveSource is a twin as backup --file's source: its dump is taken, the
// fleet beats, and only then is its state read, as a running fleet's two reads
// are (the dump and the state are not one read).
type midSaveSource struct {
	store.MemSource
	between func(ctx context.Context) error
}

func (s midSaveSource) Save(ctx context.Context) ([]byte, store.SnapshotCounts, error) {
	doc, c, err := s.MemSource.Save(ctx)
	if err != nil {
		return doc, c, err
	}
	return doc, c, s.between(ctx)
}

// backupFileRun is one run of backup --file over st, beating the fleet between
// the dump and the state read. It returns the verb's error and the file path.
func backupFileRun(t *testing.T, st *store.Store, between func(context.Context, *store.Store) error) (error, string) {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "backup.bak")
	src := midSaveSource{MemSource: store.MemSource{M: st.B.(*store.Mem), Names: st.Names},
		between: func(ctx context.Context) error { return between(ctx, st) }}
	var out bytes.Buffer
	err := runBackup(context.Background(), src, store.MemTwin{Names: st.Names}, dest, false, &out)
	return err, dest
}

// The backup --file restore check passes while a live member's beat rewrites
// the fleet's display cells between the dump and the state read: the pre-adopt
// backup of 2026-10-10 evening refused here (the dump does not restore the
// sprint the store held, the fleet rows) and the switch went ahead with no
// backup.
func TestBackupFilePassesWhileMemberBeatsRewriteTheFleet(t *testing.T) {
	t.Parallel()
	st := liveFleetTwin(t)
	dest := filepath.Join(t.TempDir(), "backup.bak")
	src := midSaveSource{MemSource: store.MemSource{M: st.B.(*store.Mem), Names: st.Names},
		between: func(ctx context.Context) error { return beatsMidDump(ctx, st) }}
	var out bytes.Buffer
	require.NoError(t, runBackup(context.Background(), src, store.MemTwin{Names: st.Names}, dest, false, &out))
	assert.Contains(t, out.String(), "BACKUP OK file="+dest)
	assert.Contains(t, out.String(), "restore=semantic compared=state+document+counts")
	_, err := os.Stat(dest)
	assert.NoError(t, err, "the verified backup is written")
}

// A real change written mid-dump still fails the check and removes the file: a
// member shown held (not a beat cell).
func TestBackupFileStillFailsOnARealChangeMidDump(t *testing.T) {
	t.Parallel()
	st := liveFleetTwin(t)
	err, dest := backupFileRun(t, st, func(ctx context.Context, st *store.Store) error {
		if err := beatsMidDump(ctx, st); err != nil {
			return err
		}
		return st.B.(*store.Mem).RowSet(ctx, st.Names.Table(sprint.Fleet), "m1", map[string]string{sprint.Status: sprint.Held})
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not restore the sprint the store held")
	_, serr := os.Stat(dest)
	assert.True(t, os.IsNotExist(serr), "a failed check removes the file")
}
