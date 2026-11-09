package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

func TestFunctionalUpChildHoldsOnlyTheSprintSecret(t *testing.T) { // unitlint: serial, it sets the process environment (t.Setenv)
	if os.Getenv("NOVA_FUNCTIONAL") != "1" {
		t.Skip("set NOVA_FUNCTIONAL=1 for child-process test")
	}
	dir := t.TempDir()
	secrets := filepath.Join(dir, "nova-secrets")
	child := filepath.Join(dir, "child")
	script := `#!/bin/sh
[ "$3" = NOVA_SPRINT_PG_PASSWORD ] || exit 9
while [ "$1" != "--" ]; do shift; done
shift
export NOVA_SPRINT_PG_PASSWORD=sprint_fixture
exec "$@"
`
	childScript := `#!/bin/sh
[ "$1" = --internal-up ] && [ "$2" = --as ] && [ "$3" = alice ] || exit 10
[ -z "$NOVA_PG_ADMIN_PASSWORD" ] || exit 12
[ "$NOVA_SPRINT_PG_PASSWORD" = sprint_fixture ] || exit 13
[ "$NOVA_PG_PASSWORD_ENV" = NOVA_SPRINT_PG_PASSWORD ] || exit 14
[ "$NOVA_SECRETS_SEAT" = seat ] || exit 15
`
	if err := os.WriteFile(secrets, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, []byte(childScript), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("NOVA_PG_ADMIN_PASSWORD", "must_not_leak")
	t.Setenv("NOVA_SPRINT_PG_PASSWORD", "must_not_leak")
	cfg := productionSetupConfig{Binary: child, Secrets: verbs.NovaSecrets{Store: "/store", KeyFile: "/key", Seat: "seat"}}
	if code := runUpChild(context.Background(), cfg, []string{"--as", "alice"}); code != 0 {
		t.Fatalf("the child walk exited %d", code)
	}
}

// up's own refusals go to stdout with its exit, as every verb's do; stderr
// stays empty (one stream for the one refusal line).
func TestUpWrapperRefusesOnStdout(t *testing.T) { // unitlint: serial, it sets the process environment (t.Setenv) and swaps os.Stdout
	t.Setenv("NOVA_PG_ADMIN_PASSWORD", "fixture")
	dir := t.TempDir()
	stdout, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	keepOut, keepErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdout, stderr
	code := runUpWrapper(context.Background(), []string{"--as", "alice"})
	os.Stdout, os.Stderr = keepOut, keepErr
	out, err := os.ReadFile(stdout.Name())
	if err != nil {
		t.Fatal(err)
	}
	errOut, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 || !strings.HasPrefix(string(out), "SPRINT REFUSED up: up is the one verb run without database credentials") || len(errOut) != 0 {
		t.Fatalf("exit %d stdout %q stderr %q", code, out, errOut)
	}
}
