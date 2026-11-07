package verbs

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// sprintRoleDB is the role step's view of PostgreSQL for the sprint role's
// password: a login as nova_sprint, the stored verifier read
// through the admin connection, and setting a verifier. pgRoleDB is the
// real one; the unit tests use a fake.
type sprintRoleDB interface {
	// Login connects as nova_sprint with password and closes; requireSCRAM
	// refuses any server that does not run a SCRAM exchange (a pg_hba
	// trust line included).
	Login(ctx context.Context, password string, requireSCRAM bool) error
	// StoredVerifier is pg_authid's rolpassword for nova_sprint (nil when
	// the role has none); readable is false when the admin may not read
	// pg_authid (a role that is not a superuser).
	StoredVerifier(ctx context.Context) (verifier *string, readable bool, err error)
	// SetVerifier sets nova_sprint's password to a SCRAM verifier: the
	// password itself never reaches SQL or a server log.
	SetVerifier(ctx context.Context, verifier string) error
}

// passwordProof is what proveSprintPassword found: Proven, or why not;
// Unprovable is a host whose pg_hba trusts nova_sprint's connection while
// the admin cannot read pg_authid, so nothing can show the password.
type passwordProof struct {
	Proven, Unprovable bool
	Why                string
}

// proveSprintPassword proves the role's password is password: a SCRAM
// login with it; on a host whose pg_hba trusts the connection (a login
// with a wrong password succeeds) a login proves nothing, so the stored
// SCRAM verifier is recomputed from password instead. A trust line can
// never pass a role with no password or another password.
func proveSprintPassword(ctx context.Context, db sprintRoleDB, password string) (passwordProof, error) {
	scram := db.Login(ctx, password, true)
	if scram == nil {
		return passwordProof{Proven: true}, nil
	}
	var pe *pgconn.PgError
	if errors.As(scram, &pe) && pe.Code == "28P01" {
		return passwordProof{Why: "a SCRAM login as nova_sprint with the seat's NOVA_SPRINT_PG_PASSWORD was refused (" + Redact(pe.Message) + ", SQLSTATE 28P01)"}, nil
	}
	if db.Login(ctx, password+"-trust-probe", false) != nil {
		return passwordProof{Why: "a SCRAM login as nova_sprint failed: " + Redact(scram.Error())}, nil
	}
	verifier, readable, err := db.StoredVerifier(ctx)
	switch {
	case err != nil:
		return passwordProof{}, fmt.Errorf("role password: reading nova_sprint's stored verifier: %w", err)
	case !readable:
		return passwordProof{Unprovable: true, Why: "this machine's pg_hba trusts nova_sprint's connection, so no login can show its password, and " + adminRoleName + " may not read pg_authid"}, nil
	case verifier == nil:
		return passwordProof{Why: "the role nova_sprint has no password (this machine's pg_hba trusts its connection, so a login could not show that)"}, nil
	}
	same, why := scramMatches(*verifier, password)
	if !same {
		return passwordProof{Why: "the role nova_sprint's stored password is not the seat's NOVA_SPRINT_PG_PASSWORD (" + why + ")"}, nil
	}
	return passwordProof{Proven: true}, nil
}

// scramMatches is whether stored (SCRAM-SHA-256$<iter>:<salt>$<key>:<key>)
// is password's verifier, recomputed with its own salt and iterations;
// why names what did not match.
func scramMatches(stored, password string) (bool, string) {
	head, keys, ok := strings.Cut(strings.TrimPrefix(stored, "SCRAM-SHA-256$"), "$")
	iterText, saltText, ok2 := strings.Cut(head, ":")
	if !strings.HasPrefix(stored, "SCRAM-SHA-256$") || !ok || !ok2 || keys == "" {
		return false, "not a SCRAM-SHA-256 verifier"
	}
	iterations, err := strconv.Atoi(iterText)
	if err != nil || iterations < 1 {
		return false, "its iteration count " + strconv.Quote(Redact(iterText)) + " is not a positive number"
	}
	salt, err := base64.StdEncoding.DecodeString(saltText)
	if err != nil {
		return false, "its salt is not base64: " + err.Error()
	}
	want, err := scramVerifier(password, salt, iterations)
	if err != nil {
		return false, err.Error()
	}
	if subtle.ConstantTimeCompare([]byte(want), []byte(stored)) != 1 {
		return false, "the verifiers differ"
	}
	return true, ""
}

// ensureSprintPassword is the role step's password half: prove
// it; when it is not proven, set it from the seat's value (a SCRAM verifier
// with a fresh salt from random) and prove it again. The refusal names
// which failed, the set or the verify. On an unprovable host (pg_hba trust,
// pg_authid unreadable) the password is set every time: the committed set
// is the only evidence there is, and a missing password cannot survive it.
func ensureSprintPassword(ctx context.Context, db sprintRoleDB, password string, random io.Reader) error {
	before, err := proveSprintPassword(ctx, db, password)
	if err != nil || before.Proven {
		return err
	}
	var salt [16]byte
	if _, err = io.ReadFull(random, salt[:]); err != nil {
		return cause("role password: salt", err)
	}
	verifier, err := scramVerifier(password, salt[:], 4096)
	if err != nil {
		return cause("role password", err)
	}
	if err = db.SetVerifier(ctx, verifier); err != nil {
		return rolePermissionRefusal("role password: setting nova_sprint's password from the seat's NOVA_SPRINT_PG_PASSWORD failed (it was not proven: "+before.Why+")", err, true)
	}
	after, err := proveSprintPassword(ctx, db, password)
	if err != nil || after.Proven || after.Unprovable {
		return err
	}
	return &OperationError{
		Problem: "role password: nova_sprint's password was set from the seat's NOVA_SPRINT_PG_PASSWORD, but it does not verify: " + after.Why,
		Fix:     "make pg_hba.conf on the database host ask scram-sha-256 for nova_sprint (with password_encryption = scram-sha-256), then nova-sprint up again",
		Exit:    exitRefused,
	}
}

// pgRoleDB is sprintRoleDB on an admin connection and the config it was
// opened with.
type pgRoleDB struct {
	admin *pgx.Conn
	cfg   *pgx.ConnConfig
}

func (d pgRoleDB) Login(ctx context.Context, password string, requireSCRAM bool) error {
	test := d.cfg.Copy()
	test.User, test.Password, test.RequireAuth = sprintRoleName, password, ""
	if requireSCRAM {
		test.RequireAuth = "scram-sha-256"
	}
	conn, err := pgx.ConnectConfig(ctx, test)
	if err != nil {
		return err
	}
	return closeSetupConnection(conn)
}

func (d pgRoleDB) StoredVerifier(ctx context.Context) (*string, bool, error) {
	var verifier *string
	err := d.admin.QueryRow(ctx, "SELECT rolpassword FROM pg_authid WHERE rolname='nova_sprint'").Scan(&verifier)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "42501" {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return verifier, true, nil
}

func (d pgRoleDB) SetVerifier(ctx context.Context, verifier string) error {
	_, err := d.admin.Exec(ctx, "ALTER ROLE nova_sprint LOGIN PASSWORD '"+verifier+"'")
	return err
}
