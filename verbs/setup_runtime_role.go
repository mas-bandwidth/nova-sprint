package verbs

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const sprintPasswordName = "NOVA_SPRINT_PG_PASSWORD"

// scramVerifier builds PostgreSQL's SCRAM verifier without sending plaintext.
func scramVerifier(password string, salt []byte, iterations int) (string, error) {
	salted, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, salted)
	_, _ = mac.Write([]byte("Client Key"))
	client := mac.Sum(nil)
	stored := sha256.Sum256(client)
	mac = hmac.New(sha256.New, salted)
	_, _ = mac.Write([]byte("Server Key"))
	server := mac.Sum(nil)
	for i := range salted {
		salted[i] = 0
	}
	for i := range client {
		client[i] = 0
	}
	return fmt.Sprintf("SCRAM-SHA-256$%d:%s$%s:%s", iterations, base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(stored[:]), base64.StdEncoding.EncodeToString(server)), nil
}

// ProvisionSprintRole runs only in a child under nova-secrets exec. SQL contains
// a SCRAM verifier, never plaintext. Server logs may contain that sensitive verifier.
func ProvisionSprintRole(ctx context.Context, dsn, adminPassword, password string) error {
	if adminPassword == "" || password == "" {
		return fmt.Errorf("role credential unavailable")
	}
	dsn, err := sprintDSN(dsn)
	if err != nil {
		return err
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("invalid postgres configuration")
	}
	cfg.User = "nova_admin"
	cfg.Password = adminPassword
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("postgres admin unavailable")
	}
	defer closeSetupConnection(conn)
	ready, err := roleReady(ctx, conn, cfg, password)
	if err != nil {
		return err
	}
	if ready {
		return nil
	}
	var salt [16]byte
	if _, err = rand.Read(salt[:]); err != nil {
		return fmt.Errorf("role credential unavailable")
	}
	verifier, err := scramVerifier(password, salt[:], 4096)
	if err != nil {
		return fmt.Errorf("role credential unavailable")
	}
	return setSprintRole(ctx, conn, verifier)
}
func roleReady(ctx context.Context, admin *pgx.Conn, cfg *pgx.ConnConfig, password string) (bool, error) {
	var exists bool
	var owner *string
	err := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='nova_sprint'), (SELECT pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname='sprint')`).Scan(&exists, &owner)
	if err != nil {
		return false, fmt.Errorf("role setup failed")
	}
	if owner != nil && *owner != "nova_sprint" {
		return false, fmt.Errorf("sprint schema has another owner")
	}
	if !exists {
		return false, nil
	}
	test := cfg.Copy()
	test.User = "nova_sprint"
	test.Password = password
	conn, err := pgx.ConnectConfig(ctx, test)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "28P01" {
			return false, nil
		}
		return false, fmt.Errorf("sprint role authentication unavailable")
	}
	_ = closeSetupConnection(conn)
	return owner != nil, nil
}
func setSprintRole(ctx context.Context, conn *pgx.Conn, verifier string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("role setup failed")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()

	var owner *string
	err = tx.QueryRow(ctx, "SELECT pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname='sprint'").Scan(&owner)
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("role setup failed")
	}
	if owner != nil && *owner != "nova_sprint" {
		return fmt.Errorf("sprint schema has another owner")
	}
	var role bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='nova_sprint')").Scan(&role); err != nil {
		return fmt.Errorf("role setup failed")
	}
	if !role {
		if _, err = tx.Exec(ctx, "CREATE ROLE nova_sprint LOGIN"); err != nil {
			return fmt.Errorf("role creation failed")
		}
	}
	if _, err = tx.Exec(ctx, "ALTER ROLE nova_sprint LOGIN PASSWORD '"+verifier+"'"); err != nil {
		return fmt.Errorf("role credential setup failed")
	}
	if owner == nil {
		if err = createSprintSchema(ctx, tx); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("role setup failed")
	}
	return nil
}

// PostgreSQL 16+ creates a CREATEROLE grant with SET disabled. Temporarily
// enable it to create the owned schema, then restore the original grant.
func createSprintSchema(ctx context.Context, tx pgx.Tx) error {
	var enabled *bool
	err := tx.QueryRow(ctx, `SELECT set_option FROM pg_auth_members WHERE roleid='nova_sprint'::regrole AND member='nova_admin'::regrole AND grantor=current_user::regrole`).Scan(&enabled)
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("schema creation failed")
	}
	if enabled == nil || !*enabled {
		if _, err = tx.Exec(ctx, "GRANT nova_sprint TO nova_admin WITH SET TRUE"); err != nil {
			return fmt.Errorf("schema creation failed")
		}
	}
	if _, err = tx.Exec(ctx, "CREATE SCHEMA sprint AUTHORIZATION nova_sprint"); err != nil {
		return fmt.Errorf("schema creation failed")
	}
	if enabled == nil {
		_, err = tx.Exec(ctx, "REVOKE nova_sprint FROM nova_admin")
	} else if !*enabled {
		_, err = tx.Exec(ctx, "GRANT nova_sprint TO nova_admin WITH SET FALSE")
	}
	if err != nil {
		return fmt.Errorf("schema grant restoration failed")
	}
	return nil
}

// InternalSprintRole reads only credentials injected into its dedicated child.
func InternalSprintRole(ctx context.Context) int {
	if ProvisionSprintRole(ctx, os.Getenv("NOVA_PG_DSN"), os.Getenv("NOVA_PG_ADMIN_PASSWORD"), os.Getenv(sprintPasswordName)) != nil {
		return 1
	}
	return 0
}

// InternalSprintRoleExists is a read-only bootstrap preflight. A role that
// exists without its durable control-seat credential is never rotated.
func InternalSprintRoleExists(ctx context.Context) (bool, error) {
	dsn, password := os.Getenv("NOVA_PG_DSN"), os.Getenv("NOVA_PG_ADMIN_PASSWORD")
	if dsn == "" || password == "" {
		return false, fmt.Errorf("role inspection unavailable")
	}
	dsn, err := sprintDSN(dsn)
	if err != nil {
		return false, err
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return false, fmt.Errorf("invalid postgres configuration")
	}
	cfg.User = "nova_admin"
	cfg.Password = password
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return false, fmt.Errorf("role inspection unavailable")
	}
	defer closeSetupConnection(conn)
	var exists bool
	if err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='nova_sprint')").Scan(&exists); err != nil {
		return false, fmt.Errorf("role inspection unavailable")
	}
	return exists, nil
}
