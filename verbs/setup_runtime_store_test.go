package verbs

import (
	"context"
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
