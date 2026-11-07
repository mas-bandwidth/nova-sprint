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
	if _, err := s.Snapshot(context.Background()); !errors.Is(err, verbs.ErrNotSetUp) {
		t.Fatalf("missing credential: %v", err)
	}
	s.creds = credentials{dsn: "postgres://nova_config:secret@localhost/db", password: "explicit"}
	if _, err := s.Snapshot(context.Background()); err == nil || !strings.Contains(err.Error(), "passwordless") {
		t.Fatalf("embedded credential: %v", err)
	}
	// No sprint credential is "not set up" (next answers up), never a connection on the ambient one.
	if ok, err := (doctor{creds: credentials{dsn: "postgres://nova_config@localhost/db"}}).Schema(context.Background()); ok || err != nil {
		t.Fatalf("schema without the sprint credential: ok=%v err=%v", ok, err)
	}
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

// The sprint not set up (no NOVA_SPRINT_PG_PASSWORD): hook refuses naming up,
// never a secrets command, and next answers up (exit 0).
func TestNotSetUpNamesUp(t *testing.T) {
	creds := credentials{dsn: "postgres://nova_sprint@localhost/db"}
	e := verbs.Engine{Store: &lazyStore{creds: creds}, Bus: present{"alice"}, Doctor: doctor{creds: creds}, Actor: "alice"}
	hook := e.Execute(context.Background(), []string{"hook", "--role", "worker"})
	if hook.Exit != 1 || hook.Output != "SPRINT REFUSED hook: the sprint is not set up: nova-sprint up --as alice\n" {
		t.Fatalf("hook: %+v", hook)
	}
	next := e.Execute(context.Background(), []string{"next"})
	if next.Exit != 0 || !strings.HasSuffix(next.Output, "next: nova-sprint up --as alice\n") || strings.Contains(next.Output, "nova-secrets") {
		t.Fatalf("next: %+v", next)
	}
	if _, err := (credentials{}).sprint(); !errors.Is(err, verbs.ErrNotSetUp) || strings.Contains(err.Error(), "nova-secrets") {
		t.Fatalf("sprint: %v", err)
	}
}
