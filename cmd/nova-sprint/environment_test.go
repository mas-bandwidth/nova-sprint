package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/verbs"
)

func TestProductionStoreRejectsAmbientCredentials(t *testing.T) {
	t.Setenv("PGPASSWORD", "ambient-should-not-be-used")
	s := &lazyStore{creds: credentials{dsn: "postgres://nova_config@localhost/db"}}
	if _, err := s.Snapshot(context.Background()); !isNoCredential(err) {
		t.Fatalf("missing credential: %v", err)
	}
	s.creds = credentials{dsn: "postgres://nova_config:secret@localhost/db", password: "explicit"}
	if _, err := s.Snapshot(context.Background()); err == nil || !strings.Contains(err.Error(), "passwordless") {
		t.Fatalf("embedded credential: %v", err)
	}
	// No sprint credential is the credential refusal, never "not set up" and
	// never a connection on the ambient one.
	if ok, err := (doctor{creds: credentials{dsn: "postgres://nova_config@localhost/db"}}).Schema(context.Background()); ok || !isNoCredential(err) {
		t.Fatalf("schema without the sprint credential: ok=%v err=%v", ok, err)
	}
}

// isNoCredential is err the refusal of a process without the sprint's
// credential (verbs.NoCredential).
func isNoCredential(err error) bool {
	var op *verbs.OperationError
	return errors.As(err, &op) && *op == *verbs.NoCredential()
}

func TestToolEnvironmentExcludesDatabaseSecrets(t *testing.T) {
	env := toolEnvironment([]string{"PATH=/bin", "NOVA_BUS_PASSWORD=bus-fixture", "NOVA_SPRINT_PG_PASSWORD=secret", "NOVA_PG_ADMIN_PASSWORD=secret", "PGPASSWORD=secret"}, "nova-bus")
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "NOVA_BUS_PASSWORD=bus-fixture") || strings.Contains(joined, "PG_PASSWORD") || strings.Contains(joined, "PGPASSWORD") {
		t.Fatalf("environment was not filtered")
	}
	fleet := strings.Join(toolEnvironment([]string{"NOVA_BUS_PASSWORD=bus-fixture", "NOVA_SPRINT_PG_PASSWORD=secret", "NOVA_SECRETS_STORE=/store"}, "nova-fleet"), "\n")
	if strings.Contains(fleet, "PASSWORD") || !strings.Contains(fleet, "NOVA_SECRETS_STORE=/store") {
		t.Fatal("fleet environment was not filtered")
	}
}

// A process without NOVA_SPRINT_PG_PASSWORD: every
// verb refuses naming nova-secrets exec, the problem and the fix split, and
// next answers the same (exit 0); "not set up" is kept for a store without
// the schema, which the credential cannot tell.
func TestNoCredentialNamesSecretsExec(t *testing.T) {
	creds := credentials{dsn: "postgres://nova_sprint@localhost/db"}
	e := verbs.Engine{Store: &lazyStore{creds: creds}, Bus: present{"alice"}, Doctor: doctor{creds: creds}, Actor: "alice"}
	const line = "this process has no sprint credential: run under nova-secrets exec --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD -- nova-sprint ..."
	for _, args := range [][]string{{"hook", "--role", "worker"}, {"stop", "--reason", "r"}, {"start"}, {"show"}, {"queue"}} {
		r := e.Execute(context.Background(), args)
		if r.Exit != 1 || r.Output != "SPRINT REFUSED "+args[0]+": "+line+"\n" {
			t.Fatalf("%v: %+v", args, r)
		}
	}
	next := e.Execute(context.Background(), []string{"next"})
	if next.Exit != 0 || !strings.Contains(next.Output, "NEXT this process has no sprint credential") || !strings.HasSuffix(next.Output, "next: "+verbs.CredentialFix+"\n") {
		t.Fatalf("next: %+v", next)
	}
	if _, err := (credentials{}).sprint(); !isNoCredential(err) || errors.Is(err, verbs.ErrNotSetUp) {
		t.Fatalf("sprint: %v", err)
	}
}
