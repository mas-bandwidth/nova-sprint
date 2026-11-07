package run

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// RestoreWriter accepts only parser-verified, individual dump statements.
// The dump parser must reject psql commands, transaction control and SQL
// outside the verified sprint backup. Extended protocol rejects multi-command
// strings, and this layer also refuses leading transaction-control words.
type RestoreWriter interface {
	Exec(context.Context, string) error
}
type restoreWriter struct{ conn *pgx.Conn }

func (s *pgChooser) Restore(ctx context.Context, apply func(RestoreWriter) error) error {
	if err := s.Check(ctx); err != nil {
		return fmt.Errorf("restore: chooser lock lost: %w", err)
	}
	tx, err := s.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("restore: begin transaction: %w", err)
	}
	defer rollbackRestore(tx)
	var used bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_depend d JOIN pg_namespace n ON n.oid=d.refobjid WHERE d.refclassid='pg_namespace'::regclass AND n.nspname='sprint')`).Scan(&used)
	if err != nil {
		return fmt.Errorf("restore: inspect schema: %w", err)
	}
	if used {
		return fmt.Errorf("SPRINT REFUSED restore: schema sprint is not empty: use an empty store")
	}
	if _, err = tx.Exec(ctx, "DROP SCHEMA IF EXISTS sprint"); err != nil {
		return fmt.Errorf("restore: prepare empty schema: %w", err)
	}
	if err = apply(restoreWriter{conn: s.conn}); err != nil {
		return fmt.Errorf("restore: apply verified dump: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("restore: commit outcome unknown: inspect the store before retrying: %w", err)
	}
	return nil
}
func rollbackRestore(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
func (w restoreWriter) Exec(ctx context.Context, statement string) error {
	word, err := leadingSQLWord(statement)
	if err != nil {
		return err
	}
	switch word {
	case "BEGIN", "START", "COMMIT", "END", "ROLLBACK", "ABORT", "SAVEPOINT", "RELEASE", "PREPARE":
		return fmt.Errorf("restore: transaction control is not permitted")
	}
	if _, err = w.conn.PgConn().ExecParams(ctx, statement, nil, nil, nil, nil).Close(); err != nil {
		return fmt.Errorf("restore: dump statement failed: %w", err)
	}
	return nil
}
func leadingSQLWord(s string) (string, error) {
	for {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "--") {
			i := strings.IndexByte(s, '\n')
			if i < 0 {
				return "", fmt.Errorf("restore: empty statement")
			}
			s = s[i+1:]
			continue
		}
		if strings.HasPrefix(s, "/*") {
			i := strings.Index(s, "*/")
			if i < 0 {
				return "", fmt.Errorf("restore: unterminated comment")
			}
			s = s[i+2:]
			continue
		}
		break
	}
	i := 0
	for i < len(s) && ((s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z')) {
		i++
	}
	if i == 0 {
		return "", fmt.Errorf("restore: expected SQL statement")
	}
	return strings.ToUpper(s[:i]), nil
}
