package verbs

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// StatementWriter executes one statement on a caller-owned transaction.
type StatementWriter interface {
	Exec(context.Context, string) error
}

// VerifiedDump holds the exact statements that passed schema and rebuild checks.
// Its statements are private so restore cannot accidentally re-read a changed file.
type VerifiedDump struct {
	statements []string
	digest     string
}

// Digest identifies the exact verified dump bytes, including its header.
func (v *VerifiedDump) Digest() string {
	if v == nil {
		return ""
	}
	return v.digest
}

func (v *VerifiedDump) Execute(ctx context.Context, w StatementWriter) error {
	if v == nil || w == nil {
		return fmt.Errorf("verified dump is required")
	}
	for _, statement := range v.statements {
		if err := w.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// PGSQLLoader is for the throwaway verification database only. Live restore
// uses the chooser lease's same-session transaction writer.
type PGSQLLoader struct{}

func (PGSQLLoader) Load(ctx context.Context, dsn string, dump *VerifiedDump) error {
	if dump == nil {
		return fmt.Errorf("verified dump is required")
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = store.RestoreBegin(ctx, tx, dump.Digest()); err != nil {
		return err
	}
	if err = dump.Execute(ctx, pgTxWriter{tx: tx}); err != nil {
		return err
	}
	if _, err = store.RestoreEnd(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type pgTxWriter struct{ tx pgx.Tx }

func (w pgTxWriter) Exec(ctx context.Context, statement string) error {
	result := w.tx.Conn().PgConn().ExecParams(ctx, statement, nil, nil, nil, nil).Read()
	return result.Err
}
