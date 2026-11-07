package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Functional tier: a real child process (a script standing in for nova-bus).

func fakeBusBin_internal(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "nova-bus")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}
func waitOrFail_internal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestFunctionalOSBusHookWakesOnALineAndStops(t *testing.T) {
	bin := fakeBusBin_internal(t, `echo "HOOK OK name=$3 waiting=0" >&2
echo "id1	from	-	subject	body"
exec sleep 30`)
	h, err := OSBusHook{Bin: bin, ClaimWait: 10 * time.Second}.Start(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	waitOrFail_internal(t, h.Wake(), "a wake")
	select {
	case <-h.Done():
		t.Fatal("the hook is long-lived: it must still run")
	default:
	}
	h.Stop()
	waitOrFail_internal(t, h.Done(), "the hook to end after Stop")
}

func TestFunctionalOSBusHookDeathCarriesItsExitAndStderr(t *testing.T) {
	bin := fakeBusBin_internal(t, `echo "HOOK OK name=$3 waiting=0" >&2
echo "redis: connection refused" >&2
exit 3`)
	h, err := OSBusHook{Bin: bin, ClaimWait: 10 * time.Second}.Start(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	waitOrFail_internal(t, h.Done(), "the hook to end")
	if e := h.Err(); e == nil || !strings.Contains(e.Error(), "exit status 3") || !strings.Contains(e.Error(), "connection refused") {
		t.Fatalf("Err = %v, want the exit status and the last stderr", e)
	}
}

func TestFunctionalOSBusHookRefusedBeforeClaimingTheName(t *testing.T) {
	bin := fakeBusBin_internal(t, `echo "name already hooked" >&2
exit 1`)
	if _, err := (OSBusHook{Bin: bin, ClaimWait: 10 * time.Second}).Start(context.Background(), "r1"); err == nil || !strings.Contains(err.Error(), "name already hooked") {
		t.Fatalf("want the hook's own refusal, got %v", err)
	}
	slow := fakeBusBin_internal(t, `exec sleep 30`)
	if _, err := (OSBusHook{Bin: slow, ClaimWait: 300 * time.Millisecond}).Start(context.Background(), "r1"); err == nil || !strings.Contains(err.Error(), "did not claim") {
		t.Fatalf("want a claim timeout, got %v", err)
	}
}

func TestFunctionalBusEnvPassesOnlyTheBusConnection(t *testing.T) {
	got := BusEnv([]string{"PATH=/bin", "NOVA_BUS_REDIS=127.0.0.1:6381", "DATABASE_URL=x", "NOVA_BUS_USER=u"})
	if len(got) != 2 || got[0] != "NOVA_BUS_REDIS=127.0.0.1:6381" || got[1] != "NOVA_BUS_USER=u" {
		t.Fatalf("BusEnv = %v", got)
	}
}
