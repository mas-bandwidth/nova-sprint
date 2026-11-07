package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

type commandHealth struct{ env verbs.ExecEnvironment }

func (h commandHealth) Check(ctx context.Context, integrity bool) ([]verbs.OperationError, error) {
	ok, _, err := h.env.Fleet(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []verbs.OperationError{{Problem: "fleet is not ready", Fix: "nova-fleet check", Exit: 1}}, nil
	}
	ok, err = (credentialEnvironment{inner: h.env}).Schema(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []verbs.OperationError{{Problem: "store is not set up", Fix: "nova-sprint up", Exit: 1}}, nil
	}
	if integrity {
		return nil, fmt.Errorf("external integrity comparisons are not yet available")
	}
	return nil, nil
}
func validateLiveHistory(ctx context.Context, dsn, password string) error {
	if password == "" {
		return fmt.Errorf("integrity: NOVA_SPRINT_PG_PASSWORD is required")
	}
	dsn, err := setupRoleDSN(dsn, "nova_sprint")
	if err != nil {
		return fmt.Errorf("integrity: configure a passwordless NOVA_PG_DSN")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("integrity: invalid PostgreSQL configuration")
	}
	if password != "" {
		cfg.Password = password
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("integrity: cannot reach PostgreSQL")
	}
	defer conn.Close(context.Background())
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("integrity: cannot open read-only transaction")
	}
	defer tx.Rollback(context.Background())
	if err = store.ValidateHistory(ctx, tx); err != nil {
		return fmt.Errorf("integrity: %w", err)
	}
	return nil
}
func (commandHealth) NovaToolsVersion(ctx context.Context) (string, error) {
	return verbs.NovaToolsVersion(ctx)
}

type liveHistoryVerifier struct{ dsn, password string }

func (v liveHistoryVerifier) CheckHistory(ctx context.Context) error {
	return validateLiveHistory(ctx, v.dsn, v.password)
}

type fakeHistoryVerifier struct{ store store.Store }

func (v fakeHistoryVerifier) CheckHistory(ctx context.Context) error {
	_, err := v.store.Snapshot(ctx)
	return err
}
