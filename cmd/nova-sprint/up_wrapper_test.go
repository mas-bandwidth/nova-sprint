package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

func TestHandoffStatusRejectsMalformedOrDuplicate(t *testing.T) {
	t.Parallel()
	id := strings.Repeat("a", 64)
	good := `{"status":"continue","id":"` + id + `"}`
	if got, err := readHandoffStatus(strings.NewReader(good)); err != nil || got != id {
		t.Fatalf("id=%q err=%v", got, err)
	}
	for _, bad := range []string{good + good, `{"status":"continue","id":"short"}`, `{"status":"continue","id":"` + id + `","extra":1}`} {
		if _, err := readHandoffStatus(strings.NewReader(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
func TestFunctionalUpChildUsesPrivateChannelAndSeparateSecrets(t *testing.T) { // unitlint: serial, it sets the process environment (t.Setenv)
	if os.Getenv("NOVA_FUNCTIONAL") != "1" {
		t.Skip("set NOVA_FUNCTIONAL=1 for child-process test")
	}
	dir := t.TempDir()
	secrets := filepath.Join(dir, "nova-secrets")
	child := filepath.Join(dir, "child")
	script := `#!/bin/sh
key="$3"
while [ "$1" != "--" ]; do shift; done
shift
if [ "$key" = NOVA_PG_ADMIN_PASSWORD ]; then export NOVA_PG_ADMIN_PASSWORD=admin_fixture; else export NOVA_SPRINT_PG_PASSWORD=sprint_fixture; fi
exec "$@"
`
	childScript := `#!/bin/sh
if [ "$1" = --internal-up-phase1 ]; then
 [ "$NOVA_PG_ADMIN_PASSWORD" = admin_fixture ] || exit 10
 [ -z "$NOVA_SPRINT_PG_PASSWORD" ] || exit 11
 printf '{"status":"continue","id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}\n' >&3
else
 [ -z "$NOVA_PG_ADMIN_PASSWORD" ] || exit 12
 [ "$NOVA_SPRINT_PG_PASSWORD" = sprint_fixture ] || exit 13
 [ "$NOVA_PG_PASSWORD_ENV" = NOVA_SPRINT_PG_PASSWORD ] || exit 14
 printf 'phase2 complete\n'
fi
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
	id, out, code := runUpChild(context.Background(), cfg, "NOVA_PG_ADMIN_PASSWORD", "--internal-up-phase1", nil, true)
	if code != 0 || out != "" || id != strings.Repeat("a", 64) {
		t.Fatalf("phase1 id=%q out=%q code=%d", id, out, code)
	}
	_, out, code = runUpChild(context.Background(), cfg, "NOVA_SPRINT_PG_PASSWORD", "--internal-up-phase2", []string{"--continuation", id}, false)
	if code != 0 || out != "phase2 complete\n" {
		t.Fatalf("phase2 out=%q code=%d", out, code)
	}
}

func TestWaitHandoffCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := waitHandoff(ctx, make(chan handoffRead), nil); err == nil {
		t.Fatal("cancellation ignored")
	}
}

// An in-memory pipe stands in for the OS pipe and a nil expiry channel for the
// timer: the read completes when the writer closes.
func TestHandoffReaderCompletesAfterWriterClose(t *testing.T) {
	t.Parallel()
	reader, writer := io.Pipe()
	result := make(chan handoffRead, 1)
	go func() {
		id, err := readHandoffStatus(reader)
		result <- handoffRead{id: id, err: err}
	}()
	id := strings.Repeat("a", 64)
	if _, err := writer.Write([]byte(`{"status":"continue","id":"` + id + `"}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := waitHandoff(context.Background(), result, nil)
	if err != nil || got != id {
		t.Fatalf("id=%q err=%v", got, err)
	}
}

// A channel still open when the expiry has passed is refused.
func TestHandoffRefusedWhenWriterStaysOpenPastExpiry(t *testing.T) {
	t.Parallel()
	expired := make(chan time.Time)
	close(expired)
	if _, err := waitHandoff(context.Background(), make(chan handoffRead), expired); err == nil {
		t.Fatal("an open channel past its expiry was accepted")
	}
}
