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
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mas-bandwidth/nova-sprint/store"
)

const sprintPasswordName = "NOVA_SPRINT_PG_PASSWORD"

// The two PostgreSQL roles up uses: the admin it connects as (the config's
// CREATEROLE role) and the sprint role it sets up.
const (
	adminRoleName  = "nova_admin"
	sprintRoleName = "nova_sprint"
)

// rolePermissionRefusal is a PostgreSQL permission refusal (SQLSTATE 42501)
// met while setting up the sprint role, as a refusal naming the fix: when
// the sprint role already existed, PostgreSQL 16+ lets a CREATEROLE role
// alter or grant only the roles it holds ADMIN OPTION on, so the postgres
// superuser grants it; when the role was being created, the admin lacks
// CREATEROLE. Any other error is what failed with err wrapped (%w; its
// SQLSTATE and message kept, passwords never in it).
func rolePermissionRefusal(what string, err error, existed bool) error {
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || pe.Code != "42501" {
		return fmt.Errorf("%s: %w", what, err)
	}
	if existed {
		return &OperationError{
			Problem: fmt.Sprintf("%s: %s (SQLSTATE 42501): the role %s already exists and %s does not hold ADMIN OPTION on it (PostgreSQL 16+ lets a CREATEROLE role manage only the roles it administers)", what, Redact(pe.Message), sprintRoleName, adminRoleName),
			Fix:     fmt.Sprintf("as the postgres superuser run: GRANT %s TO %s WITH ADMIN TRUE, INHERIT FALSE, SET FALSE; then nova-sprint up again", sprintRoleName, adminRoleName),
			Exit:    exitRefused,
		}
	}
	return &OperationError{
		Problem: fmt.Sprintf("%s: %s (SQLSTATE 42501): %s may not create the role %s", what, Redact(pe.Message), adminRoleName, sprintRoleName),
		Fix:     fmt.Sprintf("as the postgres superuser run: ALTER ROLE %s CREATEROLE; then nova-sprint up again", adminRoleName),
		Exit:    exitRefused,
	}
}

// scramVerifier builds PostgreSQL's SCRAM verifier without sending plaintext.
func scramVerifier(password string, salt []byte, iterations int) (string, error) {
	salted, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return "", fmt.Errorf("SCRAM key derivation: %w", err)
	}
	// hash.Hash's Write never returns an error
	mac := hmac.New(sha256.New, salted)
	mac.Write([]byte("Client Key"))
	client := mac.Sum(nil)
	stored := sha256.Sum256(client)
	mac = hmac.New(sha256.New, salted)
	mac.Write([]byte("Server Key"))
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

// provisionSprintRole is the role step: the role nova_sprint and its schema
// made when missing, then its password proven, or set from the seat's value
// and proven (ensureSprintPassword, defect 37). The seat's value is the one
// every service reads, so a role whose password is not it is set to it.
// fresh (the credential was minted by this up) also checks an existing
// owned schema is the sprint's before adopting it.
func provisionSprintRole(ctx context.Context, dsn, adminPassword, password string, fresh bool) error {
	conn, cfg, err := sprintAdminConnection(ctx, dsn, adminPassword, password)
	if err != nil {
		return err
	}
	defer closeSetupConnection(conn)
	var server string
	if err = conn.QueryRow(ctx, "SHOW server_version_num").Scan(&server); err != nil {
		return fmt.Errorf("cannot inspect PostgreSQL version: %w", err)
	}
	if err = requireServerVersion(server); err != nil {
		return err
	}
	exists, owner, err := sprintRoleState(ctx, conn)
	if err != nil {
		return err
	}
	if fresh && exists && owner != nil {
		if err = verifyOwnedSprintSchema(ctx, cfg, adminPassword); err != nil {
			return err
		}
	}
	if !exists || owner == nil {
		if err = setSprintRole(ctx, conn); err != nil {
			return err
		}
	}
	return ensureSprintPassword(ctx, pgRoleDB{admin: conn, cfg: cfg}, password, rand.Reader)
}

// sprintAdminConnection is the admin's connection to the store dsn names,
// and its config; both credentials must be present.
func sprintAdminConnection(ctx context.Context, dsn, adminPassword, password string) (*pgx.Conn, *pgx.ConnConfig, error) {
	if adminPassword == "" || password == "" {
		return nil, nil, fmt.Errorf("role credential unavailable: %s", missingNames(map[string]string{"NOVA_PG_ADMIN_PASSWORD": adminPassword, sprintPasswordName: password}))
	}
	dsn, err := sprintDSN(dsn)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid postgres configuration: %w", err)
	}
	cfg.User = adminRoleName
	cfg.Password = adminPassword
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("postgres admin unavailable: %w", err)
	}
	return conn, cfg, nil
}

// sprintRoleState is whether nova_sprint exists and who owns schema sprint
// (nil when there is none); a schema another role owns is refused.
func sprintRoleState(ctx context.Context, admin *pgx.Conn) (bool, *string, error) {
	var exists bool
	var owner *string
	err := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='nova_sprint'), (SELECT pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname='sprint')`).Scan(&exists, &owner)
	if err != nil {
		return false, nil, fmt.Errorf("role setup failed: %w", err)
	}
	if owner != nil && *owner != "nova_sprint" {
		return false, owner, fmt.Errorf("sprint schema has another owner (%s)", *owner)
	}
	return exists, owner, nil
}

func verifyOwnedSprintSchema(ctx context.Context, cfg *pgx.ConnConfig, password string) error {
	u, err := url.Parse(cfg.ConnString())
	if err != nil {
		return fmt.Errorf("schema identity unavailable: %w", err)
	}
	u.User = url.User("nova_admin")
	pg, err := store.Open(ctx, u.String(), password)
	if err != nil {
		return fmt.Errorf("schema identity unavailable: %w", err)
	}
	defer pg.Close()
	id, err := pg.Schema(ctx)
	if err != nil {
		return fmt.Errorf("schema identity unavailable: %w", err)
	}
	if !id.Present || !id.Current || id.Owner != "nova_sprint" {
		return fmt.Errorf("schema identity unavailable: present %v, current %v, owner %q (want nova_sprint)", id.Present, id.Current, id.Owner)
	}
	return nil
}

// minServerVersion is the oldest PostgreSQL the sprint is set up on (its
// server_version_num): the role step reads pg_auth_members.set_option and
// grants WITH SET, which PostgreSQL 16 introduced.
const minServerVersion = 160000

// requireServerVersion refuses a server older than minServerVersion,
// naming its version and the upgrade, before any SQL that needs 16 runs.
func requireServerVersion(num string) error {
	n, err := strconv.Atoi(num)
	if err != nil {
		return fmt.Errorf("cannot inspect PostgreSQL version: server_version_num %q: %w", num, err)
	}
	if n < minServerVersion {
		return &OperationError{
			Problem: fmt.Sprintf("PostgreSQL %d.%d is too old: nova-sprint needs PostgreSQL %d or newer", n/10000, n%10000, minServerVersion/10000),
			Fix:     fmt.Sprintf("upgrade the PostgreSQL server to %d or newer, then run nova-sprint up again", minServerVersion/10000),
			Exit:    exitRefused,
		}
	}
	return nil
}

// setSprintRole makes the role nova_sprint (LOGIN, its password set after
// by ensureSprintPassword) and schema sprint, whichever is missing, in one
// transaction.
func setSprintRole(ctx context.Context, conn *pgx.Conn) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("role setup failed: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()

	var owner *string
	err = tx.QueryRow(ctx, "SELECT pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname='sprint'").Scan(&owner)
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("role setup failed: %w", err)
	}
	if owner != nil && *owner != "nova_sprint" {
		return fmt.Errorf("sprint schema has another owner (%s)", *owner)
	}
	var role bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='nova_sprint')").Scan(&role); err != nil {
		return fmt.Errorf("role setup failed: %w", err)
	}
	if !role {
		if _, err = tx.Exec(ctx, "CREATE ROLE nova_sprint LOGIN"); err != nil {
			return rolePermissionRefusal("role creation failed", err, false)
		}
	}
	if owner == nil {
		if err = createSprintSchema(ctx, tx, role); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("role setup failed: %w", err)
	}
	return nil
}

// PostgreSQL 16+ creates a CREATEROLE grant with SET disabled. Temporarily
// enable it to create the owned schema, then restore the original grant.
// existed is whether nova_sprint was there before this run (then a
// permission refusal names the ADMIN OPTION grant).
func createSprintSchema(ctx context.Context, tx pgx.Tx, existed bool) error {
	var enabled *bool
	err := tx.QueryRow(ctx, `SELECT set_option FROM pg_auth_members WHERE roleid='nova_sprint'::regrole AND member='nova_admin'::regrole AND grantor=current_user::regrole`).Scan(&enabled)
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("schema creation failed: reading the admin's grant on nova_sprint: %w", err)
	}
	if enabled == nil || !*enabled {
		if _, err = tx.Exec(ctx, "GRANT nova_sprint TO nova_admin WITH SET TRUE"); err != nil {
			return rolePermissionRefusal("schema creation failed: GRANT nova_sprint TO nova_admin WITH SET TRUE", err, existed)
		}
	}
	if _, err = tx.Exec(ctx, "CREATE SCHEMA sprint AUTHORIZATION nova_sprint"); err != nil {
		return rolePermissionRefusal("schema creation failed: CREATE SCHEMA sprint AUTHORIZATION nova_sprint", err, existed)
	}
	if enabled == nil {
		_, err = tx.Exec(ctx, "REVOKE nova_sprint FROM nova_admin")
	} else if !*enabled {
		_, err = tx.Exec(ctx, "GRANT nova_sprint TO nova_admin WITH SET FALSE")
	}
	if err != nil {
		return rolePermissionRefusal("schema grant restoration failed", err, existed)
	}
	return nil
}

// InternalSprintRole reads only credentials injected into its dedicated child.
func InternalSprintRole(ctx context.Context) int { return internalSprintRole(ctx, false) }

// InternalSprintRoleFresh is used only after this up invocation saved a new secret.
func InternalSprintRoleFresh(ctx context.Context) int { return internalSprintRole(ctx, true) }

func internalSprintRole(ctx context.Context, fresh bool) int {
	err := provisionSprintRole(ctx, os.Getenv("NOVA_PG_DSN"), os.Getenv("NOVA_PG_ADMIN_PASSWORD"), os.Getenv(sprintPasswordName), fresh)
	if err != nil {
		return ChildFailed(err, 1)
	}
	return 0
}

// InternalSprintRolePassword is step 2's verify of the role's password
// (defect 37), in its own child under nova-secrets exec with both
// credentials: false when the role does not exist yet (the act makes it);
// else the password proven, or set from the seat and proven
// (ensureSprintPassword), or the refusal naming which failed.
func InternalSprintRolePassword(ctx context.Context) (bool, error) {
	conn, cfg, err := sprintAdminConnection(ctx, os.Getenv("NOVA_PG_DSN"), os.Getenv("NOVA_PG_ADMIN_PASSWORD"), os.Getenv(sprintPasswordName))
	if err != nil {
		return false, err
	}
	defer closeSetupConnection(conn)
	exists, _, err := sprintRoleState(ctx, conn)
	if err != nil || !exists {
		return false, err
	}
	if err = ensureSprintPassword(ctx, pgRoleDB{admin: conn, cfg: cfg}, os.Getenv(sprintPasswordName), rand.Reader); err != nil {
		return false, err
	}
	return true, nil
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
		return false, fmt.Errorf("invalid postgres configuration: %w", err)
	}
	cfg.User, cfg.Password = "nova_admin", password
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return false, fmt.Errorf("role inspection unavailable: %w", err)
	}
	defer closeSetupConnection(conn)
	var exists bool
	if err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='nova_sprint')").Scan(&exists); err != nil {
		return false, fmt.Errorf("role inspection unavailable: %w", err)
	}
	return exists, nil
}
