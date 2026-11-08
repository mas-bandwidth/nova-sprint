package verbs

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSprintDSNChangesOnlyUserAndRejectsSecrets(t *testing.T) {
	t.Parallel()
	got, err := sprintDSN("postgres://nova_admin@host:5432/nova?sslmode=require")
	if err != nil || got != "postgres://nova_sprint@host:5432/nova?sslmode=require" {
		t.Fatal(got, err)
	}
	for _, bad := range []string{"postgres://user:secret@host/db", "postgres://host/db?password=secret", "postgres://host/db?user=other", "not-a-dsn"} {
		if _, err := sprintDSN(bad); err == nil {
			t.Fatal("accepted unsafe DSN")
		}
	}
}
func TestSetupStoreChildUsesOnlyKeyName(t *testing.T) {
	t.Parallel()
	sink := &sinkFake{saved: true, value: "fixture-secret"}
	var argsSeen []string
	p := PGSetupStore{Role: SprintRoleBootstrap{Sink: sink, Binary: "/bin/nova-sprint", Secrets: NovaSecrets{Store: "/store", KeyFile: "/key", Seat: "studio"}}, Binary: "/bin/nova-sprint", Secrets: NovaSecrets{Store: "/store", KeyFile: "/key", Seat: "studio"}, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		argsSeen = append([]string{name}, args...)
		return []byte(`{"ready":true}`), nil
	}}
	ok, err := p.Ready(context.Background())
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	for _, arg := range argsSeen {
		if strings.Contains(arg, sink.value) {
			t.Fatal("secret in arguments")
		}
	}
	sink.saved = false
	argsSeen = nil
	ok, err = p.Ready(context.Background())
	if err != nil || ok || len(argsSeen) != 0 {
		t.Fatal(ok, err, argsSeen)
	}
}

// Defect 37: step 2's verify proves the role's password first, in a child
// given both credentials by name (a store probe as nova_sprint passes on a
// pg_hba trust host whatever the password is); the role child's false or
// refusal stops it before the store probe.
func TestSetupStoreReadyChecksTheRolePasswordFirst(t *testing.T) {
	t.Parallel()
	sink := &sinkFake{saved: true, value: "fixture-secret"}
	var modes, only []string
	answer := map[string]string{"--internal-setup-role-password": `{"ready":true}`, "--internal-store-probe": `{"ready":true}`}
	secrets := NovaSecrets{Store: "/store", KeyFile: "/key", Seat: "studio"}
	p := PGSetupStore{Role: SprintRoleBootstrap{Sink: sink, Binary: "/bin/nova-sprint", Secrets: secrets}, Binary: "/bin/nova-sprint", Secrets: secrets, Command: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		mode := args[len(args)-1]
		modes, only = append(modes, mode), append(only, args[2])
		if answer[mode] == "refuse" {
			return nil, &OperationError{Problem: "role password: set but does not verify", Fix: "fix pg_hba", Exit: 1}
		}
		return []byte(answer[mode]), nil
	}}
	ok, err := p.Ready(context.Background())
	if err != nil || !ok || strings.Join(modes, " ") != "--internal-setup-role-password --internal-store-probe" || only[0] != "NOVA_PG_ADMIN_PASSWORD,NOVA_SPRINT_PG_PASSWORD" || only[1] != "NOVA_SPRINT_PG_PASSWORD" {
		t.Fatal(ok, err, modes, only)
	}
	answer["--internal-setup-role-password"], modes = `{"ready":false}`, nil
	if ok, err = p.Ready(context.Background()); err != nil || ok || len(modes) != 1 {
		t.Fatal("role not there yet:", ok, err, modes)
	}
	answer["--internal-setup-role-password"], modes = "refuse", nil
	var op *OperationError
	if ok, err = p.Ready(context.Background()); !errors.As(err, &op) || op.Fix != "fix pg_hba" || ok || len(modes) != 1 {
		t.Fatal("role refusal:", ok, err, modes)
	}
}
