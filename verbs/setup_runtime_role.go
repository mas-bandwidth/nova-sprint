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
	"net/url"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mas-bandwidth/nova-sprint/store"
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
	return provisionSprintRole(ctx, dsn, adminPassword, password, false)
}

var errSprintCredentialMismatch = errors.New("stored sprint credential rejected")

func provisionSprintRole(ctx context.Context, dsn, adminPassword, password string, fresh bool) error {
	if adminPassword == "" || password == "" {
		return fmt.Errorf("role credential unavailable: %s", missingNames(map[string]string{"NOVA_PG_ADMIN_PASSWORD": adminPassword, sprintPasswordName: password}))
	}
	dsn, err := sprintDSN(dsn)
	if err != nil {
		return err
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return cause("invalid postgres configuration", err)
	}
	cfg.User = "nova_admin"
	cfg.Password = adminPassword
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return cause("postgres admin unavailable", err)
	}
	defer closeSetupConnection(conn)
	exists, authenticated, owner, err := roleReady(ctx, conn, cfg, password)
	if err != nil {
		return err
	}
	if !fresh && authenticated && owner != nil {
		return nil
	}
	if !fresh && !authenticated {
		return errSprintCredentialMismatch
	}
	if fresh && exists && owner != nil {
		if err = verifyOwnedSprintSchema(ctx, cfg, adminPassword); err != nil {
			return err
		}
	}

	var salt [16]byte
	if _, err = rand.Read(salt[:]); err != nil {
		return cause("role credential unavailable", err)
	}
	verifier, err := scramVerifier(password, salt[:], 4096)
	if err != nil {
		return cause("role credential unavailable", err)
	}
	return setSprintRole(ctx, conn, verifier, fresh)
}
func roleReady(ctx context.Context, admin *pgx.Conn, cfg *pgx.ConnConfig, password string) (bool, bool, *string, error) {
	var exists bool
	var owner *string
	err := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='nova_sprint'), (SELECT pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname='sprint')`).Scan(&exists, &owner)
	if err != nil {
		return false, false, nil, cause("role setup failed", err)
	}
	if owner != nil && *owner != "nova_sprint" {
		return false, false, owner, fmt.Errorf("sprint schema has another owner (%s)", *owner)
	}
	if !exists {
		return false, false, owner, nil
	}
	test := cfg.Copy()
	test.User = "nova_sprint"
	test.Password = password
	conn, err := pgx.ConnectConfig(ctx, test)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "28P01" {
			return true, false, owner, nil
		}
		return false, false, owner, cause("sprint role authentication unavailable", err)
	}
	_ = closeSetupConnection(conn)
	return true, true, owner, nil
}

func verifyOwnedSprintSchema(ctx context.Context, cfg *pgx.ConnConfig, password string) error {
	u, err := url.Parse(cfg.ConnString())
	if err != nil {
		return cause("schema identity unavailable", err)
	}
	u.User = url.User("nova_admin")
	pg, err := store.Open(ctx, u.String(), password)
	if err != nil {
		return cause("schema identity unavailable", err)
	}
	defer pg.Close()
	id, err := pg.Schema(ctx)
	if err != nil {
		return cause("schema identity unavailable", err)
	}
	if !id.Present || !id.Current || id.Owner != "nova_sprint" {
		return fmt.Errorf("schema identity unavailable: present %v, current %v, owner %q (want nova_sprint)", id.Present, id.Current, id.Owner)
	}
	return nil
}
func setSprintRole(ctx context.Context, conn *pgx.Conn, verifier string, fresh bool) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return cause("role setup failed", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()

	var owner *string
	err = tx.QueryRow(ctx, "SELECT pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname='sprint'").Scan(&owner)
	if err != nil && err != pgx.ErrNoRows {
		return cause("role setup failed", err)
	}
	if owner != nil && *owner != "nova_sprint" {
		return fmt.Errorf("sprint schema has another owner (%s)", *owner)
	}
	var role bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='nova_sprint')").Scan(&role); err != nil {
		return cause("role setup failed", err)
	}
	if !role {
		if !fresh {
			return errSprintCredentialMismatch
		}
		if _, err = tx.Exec(ctx, "CREATE ROLE nova_sprint LOGIN"); err != nil {
			return cause("role creation failed", err)
		}
	}
	if fresh {
		if _, err = tx.Exec(ctx, "ALTER ROLE nova_sprint LOGIN PASSWORD '"+verifier+"'"); err != nil {
			return cause("role credential setup failed", err)
		}
	}

	if owner == nil {
		if err = createSprintSchema(ctx, tx); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return cause("role setup failed", err)
	}
	return nil
}

// PostgreSQL 16+ creates a CREATEROLE grant with SET disabled. Temporarily
// enable it to create the owned schema, then restore the original grant.
func createSprintSchema(ctx context.Context, tx pgx.Tx) error {
	var enabled *bool
	err := tx.QueryRow(ctx, `SELECT set_option FROM pg_auth_members WHERE roleid='nova_sprint'::regrole AND member='nova_admin'::regrole AND grantor=current_user::regrole`).Scan(&enabled)
	if err != nil && err != pgx.ErrNoRows {
		return cause("schema creation failed", err)
	}
	if enabled == nil || !*enabled {
		if _, err = tx.Exec(ctx, "GRANT nova_sprint TO nova_admin WITH SET TRUE"); err != nil {
			return cause("schema creation failed", err)
		}
	}
	if _, err = tx.Exec(ctx, "CREATE SCHEMA sprint AUTHORIZATION nova_sprint"); err != nil {
		return cause("schema creation failed", err)
	}
	if enabled == nil {
		_, err = tx.Exec(ctx, "REVOKE nova_sprint FROM nova_admin")
	} else if !*enabled {
		_, err = tx.Exec(ctx, "GRANT nova_sprint TO nova_admin WITH SET FALSE")
	}
	if err != nil {
		return cause("schema grant restoration failed", err)
	}
	return nil
}

// InternalSprintRole reads only credentials injected into its dedicated child.
func InternalSprintRole(ctx context.Context) int { return internalSprintRole(ctx, false) }

// InternalSprintRoleFresh is used only after this up invocation saved a new secret.
func InternalSprintRoleFresh(ctx context.Context) int { return internalSprintRole(ctx, true) }

const SprintRoleCredentialMismatchExit = 3

func internalSprintRole(ctx context.Context, fresh bool) int {
	err := provisionSprintRole(ctx, os.Getenv("NOVA_PG_DSN"), os.Getenv("NOVA_PG_ADMIN_PASSWORD"), os.Getenv(sprintPasswordName), fresh)
	if errors.Is(err, errSprintCredentialMismatch) {
		return ChildFailed(err, SprintRoleCredentialMismatchExit)
	}
	if err != nil {
		return ChildFailed(err, 1)
	}
	return 0
}

// InternalSprintRoleExists supports the existing read-only internal route.
func InternalSprintRoleExists(ctx context.Context) (bool, error) {
	dsn, password := os.Getenv("NOVA_PG_DSN"), os.Getenv("NOVA_PG_ADMIN_PASSWORD")
	if dsn == "" || password == "" {
		return false, fmt.Errorf("role inspection unavailable: %s", missingNames(map[string]string{"NOVA_PG_DSN": dsn, "NOVA_PG_ADMIN_PASSWORD": password}))
	}
	dsn, err := sprintDSN(dsn)
	if err != nil {
		return false, err
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return false, cause("invalid postgres configuration", err)
	}
	cfg.User, cfg.Password = "nova_admin", password
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return false, cause("role inspection unavailable", err)
	}
	defer closeSetupConnection(conn)
	var exists bool
	if err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='nova_sprint')").Scan(&exists); err != nil {
		return false, cause("role inspection unavailable", err)
	}
	return exists, nil
}
