package verbs

import (
	"context"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// sprintVerifier is nova_sprint's password as a SCRAM verifier with a fresh
// salt from random: the password itself never reaches SQL or a server log.
// Every up sets the role's password from the seat's value (defect 37: the
// seat's value is the one every service reads, and where pg_hba trusts a
// connection no login can show the role's password).
func sprintVerifier(password string, random io.Reader) (string, error) {
	var salt [16]byte
	if _, err := io.ReadFull(random, salt[:]); err != nil {
		return "", cause("role password: salt", err)
	}
	return scramVerifier(password, salt[:], 4096)
}

// setSprintPassword sets nova_sprint's password to verifier.
func setSprintPassword(ctx context.Context, db interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, verifier string) error {
	if _, err := db.Exec(ctx, "ALTER ROLE nova_sprint LOGIN PASSWORD '"+verifier+"'"); err != nil {
		return rolePermissionRefusal("role password: setting nova_sprint's password from the seat's NOVA_SPRINT_PG_PASSWORD failed", err, true)
	}
	return nil
}

func closeSetupConnection(conn *pgx.Conn) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return conn.Close(ctx)
}
