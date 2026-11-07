package verbs

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// roleDBFake is a PostgreSQL for the role's password: trust is a pg_hba
// trust line for nova_sprint's connection, readable whether the admin may
// read pg_authid, verifier the stored rolpassword (nil: none).
type roleDBFake struct {
	trust, readable bool
	verifier        *string
	setErr          error
	sets            int
}

func (f *roleDBFake) Login(_ context.Context, password string, requireSCRAM bool) error {
	switch {
	case f.trust && requireSCRAM:
		return errors.New(`authentication method requirement "scram-sha-256" failed: server did not complete authentication`)
	case f.trust:
		return nil
	case f.verifier != nil:
		if same, _ := scramMatches(*f.verifier, password); same {
			return nil
		}
	}
	return &pgconn.PgError{Code: "28P01", Message: `password authentication failed for user "nova_sprint"`}
}
func (f *roleDBFake) StoredVerifier(context.Context) (*string, bool, error) {
	if !f.readable {
		return nil, false, nil
	}
	return f.verifier, true, nil
}
func (f *roleDBFake) SetVerifier(_ context.Context, verifier string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.sets++
	f.verifier = &verifier
	return nil
}

func verifierOf(t *testing.T, password string) *string {
	t.Helper()
	v, err := scramVerifier(password, make([]byte, 16), 4096)
	if err != nil {
		t.Fatal(err)
	}
	return &v
}

// The role's password is proven or set from the seat; a pg_hba
// trust line never passes a role with no password or another password.
func TestEnsureSprintPassword(t *testing.T) {
	for name, c := range map[string]struct {
		db   roleDBFake
		sets int
	}{
		"scram host, right password: nothing set": {roleDBFake{verifier: verifierOf(t, "seat")}, 0},
		"scram host, other password: set":         {roleDBFake{verifier: verifierOf(t, "old")}, 1},
		"scram host, no password: set":            {roleDBFake{}, 1},
		"trust host, right verifier: nothing set": {roleDBFake{trust: true, readable: true, verifier: verifierOf(t, "seat")}, 0},
		"trust host, no password: set":            {roleDBFake{trust: true, readable: true}, 1},
		"trust host, other verifier: set":         {roleDBFake{trust: true, readable: true, verifier: verifierOf(t, "old")}, 1},
		"trust host, pg_authid unreadable: set":   {roleDBFake{trust: true}, 1},
	} {
		db := c.db
		if err := ensureSprintPassword(context.Background(), &db, "seat", bytes.NewReader(make([]byte, 16))); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if db.sets != c.sets {
			t.Fatalf("%s: %d sets, want %d", name, db.sets, c.sets)
		}
		if same, why := scramMatches(*db.verifier, "seat"); !same {
			t.Fatalf("%s: the stored verifier is not the seat's password: %s", name, why)
		}
	}
}

// The refusal names which of the two failed: setting the password, or
// verifying it once set.
func TestEnsureSprintPasswordRefusalNamesTheFailure(t *testing.T) {
	denied := &pgconn.PgError{Code: "42501", Message: "permission denied to alter role"}
	db := &roleDBFake{setErr: denied}
	err := ensureSprintPassword(context.Background(), db, "seat", bytes.NewReader(make([]byte, 16)))
	var op *OperationError
	if !errors.As(err, &op) || !strings.Contains(op.Problem, "setting nova_sprint's password from the seat's NOVA_SPRINT_PG_PASSWORD failed") || !strings.Contains(op.Fix, "WITH ADMIN TRUE") {
		t.Fatalf("set refusal: %v", err)
	}
	// A server that never takes the set password (the fake's verify keeps
	// refusing) is the verify's refusal, with the pg_hba fix.
	never := &neverVerifies{}
	err = ensureSprintPassword(context.Background(), never, "seat", bytes.NewReader(make([]byte, 16)))
	if !errors.As(err, &op) || !strings.Contains(op.Problem, "was set from the seat's NOVA_SPRINT_PG_PASSWORD, but it does not verify") || !strings.Contains(op.Fix, "scram-sha-256") || never.sets != 1 {
		t.Fatalf("verify refusal: %v (%d sets)", err, never.sets)
	}
	if err = ensureSprintPassword(context.Background(), db, "seat", strings.NewReader("short")); err == nil || !strings.Contains(err.Error(), "role password: salt") {
		t.Fatalf("salt failure: %v", err)
	}
}

type neverVerifies struct{ roleDBFake }

func (n *neverVerifies) Login(context.Context, string, bool) error {
	return &pgconn.PgError{Code: "28P01", Message: "password authentication failed"}
}

func TestScramMatchesRefusesMalformed(t *testing.T) {
	good := *verifierOf(t, "seat")
	if same, why := scramMatches(good, "seat"); !same {
		t.Fatal(why)
	}
	for _, bad := range []string{"md5abc", "SCRAM-SHA-256$x:AAAA$a:b", "SCRAM-SHA-256$4096:!!$a:b", "SCRAM-SHA-256$4096", strings.Replace(good, "4096", "4097", 1)} {
		if same, why := scramMatches(bad, "seat"); same || why == "" {
			t.Fatalf("%q matched (%s)", bad, why)
		}
	}
}
