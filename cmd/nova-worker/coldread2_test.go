package main

import (
	"os"
	"strings"
	"testing"
)

// Cold read 2, 2026-11-06, at ef4df30c (Rowan's cold reader; probes marked
// Stella verify findings from Stella's source-only read). These read main.go:
// the wiring is the defect, and main builds the Runner inline.

func cr2Main_internal(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Round 2 ruling (worker): "main wires ... the forge". main.go:90 builds
// forge.New(forge.Config{Exec, Timeout}) with no Identity, so every gh call
// is refused ("neither GH_TOKEN nor GH_CONFIG_DIR is set", forge.go:114) and
// every PR a stream wants is reported as a fault.
func TestColdread2DefectForgeWiredWithoutIdentity(t *testing.T) {
	t.Parallel()
	s := cr2Main_internal(t)
	i := strings.Index(s, "forge.New(forge.Config{")
	if i >= 0 && !strings.Contains(s[i:i+200], "Identity") {
		t.Errorf("nova-worker builds its forge with no identity: %s", s[i:i+strings.Index(s[i:], "\n")])
	}
}

// Round 2 ruling (worker): "main wires ... the secrets binary". Design: "an
// API-rate key goes to a loopback proxy the runner starts under nova-secrets
// exec"; "The DSN reaches only the verb process, through nova-secrets exec".
// --secrets-bin defaults to "" (main.go:35), so verbs run bare and need the
// DSN in nova-worker's own environment, and the proxy reads its key from
// nova-worker's environment (main.go:74), which every child that is not
// CleanEnv inherits (the runner's git, edge/exec.go:121-123).
func TestColdread2DefectSecretsNotWired(t *testing.T) {
	t.Parallel()
	s := cr2Main_internal(t)
	if strings.Contains(s, `fs.String("secrets-bin", "",`) {
		t.Errorf("--secrets-bin defaults to empty: sprint verbs run without nova-secrets exec")
	}
	if strings.Contains(s, `os.Getenv("NOVA_PROVIDER_API_KEY")`) {
		t.Errorf("the key proxy reads the provider key from nova-worker's own environment")
	}
}

// Stella, source read: --once bypasses the readiness probe. Design: "The
// probe (LESSONS 4.22): at start ... The row hooks in only after it passes."
// The --once branch (main.go:100-107) takes and runs cards with no ProbeRow.
func TestColdread2DefectOnceSkipsProbe(t *testing.T) {
	t.Parallel()
	s := cr2Main_internal(t)
	i, j := strings.Index(s, "if once {"), strings.Index(s, "return runWakeLoop_internal")
	if i >= 0 && j > i && !strings.Contains(s[i:j], "ProbeRow") {
		t.Errorf("nova-worker --once runs StepRow without the harness probe")
	}
}

// Round 2 ruling (worker): "every program runs through edge.Exec". The hook
// is started with exec.CommandContext (main.go:116), inheriting the whole
// environment, and when it exits the worker returns 0 (main.go:129-130):
// the service gates on the bus (LESSONS 4.25: no daemon gates on another).
func TestColdread2DefectHookBypassesExecAndEndsWorker(t *testing.T) {
	t.Parallel()
	s := cr2Main_internal(t)
	if strings.Contains(s, `exec.CommandContext(ctx, "nova-bus", "hook"`) {
		t.Errorf("nova-bus hook runs outside edge.Exec, and the worker exits 0 when the hook ends")
	}
}

// Stella, source read: git is optional where the design requires it. With
// no --work-clone, main leaves Git nil (main.go:83-86): no staging, no push,
// no rebase, and ok reports with no head (see
// edge/worker TestColdread2DefectNoGitReportsOkWithoutHead).
func TestColdread2DefectGitOptional(t *testing.T) {
	t.Parallel()
	s := cr2Main_internal(t)
	if strings.Contains(s, `if workClone != "" {`) && !strings.Contains(s, "missing --work-clone") {
		t.Errorf("nova-worker starts without a work clone and runs cards with no git edge")
	}
}
