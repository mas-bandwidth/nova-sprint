package verbs

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestScramVerifierKnownVector(t *testing.T) {
	t.Parallel()
	salt, _ := base64.StdEncoding.DecodeString("W22ZaJ0SNY7soEsUEjb6gQ==")
	got, err := scramVerifier("pencil", salt, 4096)
	want := "SCRAM-SHA-256$4096:W22ZaJ0SNY7soEsUEjb6gQ==$WG5d8oPm3OtcPnkdi4Uo7BkeZkBFzpcXkuLmtbsT4qY=:wfPLwcE6nTWhTAmQ7tl2KeoiWGPlZqQxSrmfPwDl2dU="
	if err != nil || got != want {
		t.Fatal("SCRAM vector mismatch", err)
	}
}

func TestRoleBootstrapSavesOnceAndPassesOnlyNames(t *testing.T) {
	t.Parallel()
	sink := &sinkFake{}
	var calls [][]string
	var modes []string
	grant := FleetGrants{Store: "/store", KeyFile: "/control.key", Pins: "/pins", ControlSeat: "studio", Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return nil, nil
	}}
	s := SprintRoleBootstrap{Sink: sink, Grants: grant, Secrets: NovaSecrets{Store: "/store", KeyFile: "/control.key", Seat: "studio"}, Binary: "/bin/nova-sprint", Random: bytes.NewReader(make([]byte, 32)), Execute: func(_ context.Context, name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		modes = append(modes, args[len(args)-1])
		return nil
	}}
	seats := []SprintSeat{{Seat: "studio", Machine: "studio"}}
	if err := s.Run(context.Background(), seats); err != nil {
		t.Fatal(err)
	}
	first := sink.value
	if first == "" {
		t.Fatal("missing fixture")
	}
	if err := s.Run(context.Background(), seats); err != nil || sink.value != first {
		t.Fatal(err)
	}
	if len(modes) != 2 || modes[0] != "--internal-setup-role-fresh" || modes[1] != "--internal-setup-role" {
		t.Fatal("wrong role child modes", modes)
	}
	for _, call := range calls {
		for _, arg := range call {
			if strings.Contains(arg, first) {
				t.Fatal("credential reached argv")
			}
		}
	}
}

// PostgreSQL 16+: a CREATEROLE admin may alter or grant only roles it
// administers. A 42501 on a nova_sprint that already existed names the
// superuser's grant; on a role being created, the admin's CREATEROLE; any
// other error keeps the pg error wrapped, SQLSTATE and message intact.
func TestRolePermissionRefusalNamesTheGrant(t *testing.T) {
	t.Parallel()
	denied := &pgconn.PgError{Severity: "ERROR", Code: "42501", Message: "permission denied to alter role"}
	var op *OperationError
	if err := rolePermissionRefusal("role credential setup failed", denied, true); !errors.As(err, &op) ||
		!strings.Contains(op.Problem, "permission denied to alter role (SQLSTATE 42501)") ||
		op.Fix != "as the postgres superuser run: GRANT nova_sprint TO nova_admin WITH ADMIN TRUE, INHERIT FALSE, SET FALSE; then nova-sprint up again" {
		t.Fatalf("%#v", err)
	}
	if err := rolePermissionRefusal("role creation failed", denied, false); !errors.As(err, &op) || !strings.Contains(op.Fix, "ALTER ROLE nova_admin CREATEROLE") {
		t.Fatalf("%#v", err)
	}
	other := &pgconn.PgError{Severity: "ERROR", Code: "42710", Message: "role \"nova_sprint\" already exists"}
	err := rolePermissionRefusal("role creation failed", other, false)
	if errors.As(err, &op) || !errors.Is(err, other) || err.Error() != `role creation failed: ERROR: role "nova_sprint" already exists (SQLSTATE 42710)` {
		t.Fatalf("%v", err)
	}
}
