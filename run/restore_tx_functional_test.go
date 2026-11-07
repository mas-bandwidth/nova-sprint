package run

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestFunctionalRestoreSameSessionAndRollback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := chooserTestPG(t)
	lock, err := OpenRestoreLock(ctx, dsn, "", "bench", 20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Close(ctx) })
	err = lock.Restore(ctx, func(w RestoreWriter) error {
		other, e := OpenChooser(ctx, dsn, "", "other", 21, &fakeHook{lease: &fakeLease{}})
		if other != nil {
			_ = other.Close(ctx)
		}
		if e == nil {
			return errors.New("chooser acquired during restore")
		}
		if e = w.Exec(ctx, "CREATE SCHEMA sprint"); e != nil {
			return e
		}
		if e = w.Exec(ctx, "CREATE TABLE sprint.x (v integer)"); e != nil {
			return e
		}
		if e = w.Exec(ctx, "INSERT INTO sprint.x VALUES (1)"); e != nil {
			return e
		}
		return w.Exec(ctx, "-- not allowed\n COMMIT")
	})
	require.ErrorContains(t, err, "transaction control")
	check, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer check.Close(ctx)
	var exists bool
	require.NoError(t, check.QueryRow(ctx, "SELECT to_regnamespace('sprint') IS NOT NULL").Scan(&exists))
	require.False(t, exists, "failed restore must roll back all SQL")
	require.NoError(t, lock.Restore(ctx, func(w RestoreWriter) error {
		if err := w.Exec(ctx, "CREATE SCHEMA sprint"); err != nil {
			return err
		}
		if err := w.Exec(ctx, "CREATE TABLE sprint.x (v integer)"); err != nil {
			return err
		}
		return w.Exec(ctx, "INSERT INTO sprint.x VALUES (2)")
	}))
	var value int
	require.NoError(t, check.QueryRow(ctx, "SELECT v FROM sprint.x").Scan(&value))
	require.Equal(t, 2, value)
	called := false
	err = lock.Restore(ctx, func(RestoreWriter) error { called = true; return nil })
	require.ErrorContains(t, err, "schema sprint is not empty")
	require.False(t, called)
}
func TestFunctionalRestoreRejectsMultipleStatements(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	lock, err := OpenRestoreLock(ctx, chooserTestPG(t), "", "bench", 20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Close(ctx) })
	err = lock.Restore(ctx, func(w RestoreWriter) error { return w.Exec(ctx, "CREATE SCHEMA sprint; COMMIT") })
	require.ErrorContains(t, err, "dump statement failed")
}
