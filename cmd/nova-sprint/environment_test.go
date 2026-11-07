package main

import (
	"context"
	"strings"
	"testing"
)

func TestProductionStoreRejectsAmbientCredentials(t *testing.T) {
	t.Setenv("PGPASSWORD", "ambient-should-not-be-used")
	s := &lazyStore{creds: credentials{dsn: "postgres://nova_config@localhost/db"}}
	if _, err := s.Snapshot(context.Background()); err == nil || !strings.Contains(err.Error(), "NOVA_SPRINT_PG_PASSWORD") {
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
