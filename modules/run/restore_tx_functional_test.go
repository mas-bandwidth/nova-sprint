package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/require"
)

func TestFunctionalRestoreSameSessionAndRollback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := chooserTestPG(t)
	admin, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer admin.Close(ctx)
	_, err = admin.Exec(ctx, "CREATE ROLE restore_user LOGIN; CREATE SCHEMA sprint AUTHORIZATION restore_user")
	require.NoError(t, err)
	limited := strings.Replace(dsn, "nova@", "restore_user@", 1)
	identity, _ := store.CheckSchema(ctx, limited)
	require.NotEmpty(t, identity.Expected)
	lock, err := OpenRestoreLock(ctx, limited, "", "bench", 20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Close(ctx) })
	err = lock.Restore(ctx, testDigest, func(w RestoreWriter) error {
		other, e := OpenChooser(ctx, dsn, "", "other", 21, "v2.0.0", &fakeHook{lease: &fakeLease{}})
		if other != nil {
			_ = other.Close(ctx)
		}
		if e == nil {
			return errors.New("chooser acquired during restore")
		}
		if e = w.Exec(ctx, "CREATE TABLE sprint.x (v integer)"); e != nil {
			return e
		}
		return w.Exec(ctx, "-- not allowed\n COMMIT")
	})
	require.ErrorContains(t, err, "transaction control")
	var exists bool
	require.NoError(t, admin.QueryRow(ctx, "SELECT to_regnamespace('sprint') IS NOT NULL").Scan(&exists))
	require.True(t, exists, "rollback preserves the pre-provisioned schema")
	require.NoError(t, admin.QueryRow(ctx, "SELECT to_regclass('sprint.x') IS NOT NULL").Scan(&exists))
	require.False(t, exists, "failed restore must roll back its SQL")
	require.NoError(t, lock.Restore(ctx, testDigest, func(w RestoreWriter) error {
		if err := w.Exec(ctx, fmt.Sprintf("INSERT INTO sprint.meta (id,schema,hash) VALUES (1,2,'%s')", identity.Expected)); err != nil {
			return err
		}
		return w.Exec(ctx, "INSERT INTO sprint.epoch (id,n,stopped,stop_reason) VALUES (1,1,true,'never started')")
	}))
	var value int
	require.NoError(t, admin.QueryRow(ctx, "SELECT n FROM sprint.epoch").Scan(&value))
	require.Equal(t, 1, value)
	called := false
	err = lock.Restore(ctx, testDigest, func(RestoreWriter) error { called = true; return nil })
	require.Error(t, err)
	require.False(t, called)
}
func TestFunctionalRestoreRejectsMultipleStatements(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	lock, err := OpenRestoreLock(ctx, chooserTestPG(t), "", "bench", 20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Close(ctx) })
	err = lock.Restore(ctx, testDigest, func(w RestoreWriter) error { return w.Exec(ctx, "CREATE SCHEMA sprint; COMMIT") })
	require.ErrorContains(t, err, "dump statement failed")
}
