package main

import (
	"os"
	"strings"
	"testing"
)

// These tests read the worker's wiring (edge/worker/serve.go, which both
// nova-worker and `nova-sprint worker` call) from the source: the runner is
// built inline there, so its wiring is checked as text.

func serveSource_internal(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../edge/worker/serve.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The worker holds no forge: its contract ends at a pushed head, and the
// run's landing opens the PR as the host's gh identity.
func TestWorkerWiresNoForge(t *testing.T) {
	t.Parallel()
	if s := serveSource_internal(t); strings.Contains(s, "forge.New(") {
		t.Errorf("the worker builds a forge client: only the landing talks to the forge")
	}
}

// A harness runs only inside a nova-reserve slot, under nova-sandbox, and the
// runner knows how to adopt a live harness after a restart: the wall is
// wired with the reserve and sandbox binaries and a process table.
func TestWorkerWiresReserveSandboxAndProcessTable(t *testing.T) {
	t.Parallel()
	s := serveSource_internal(t)
	for _, want := range []string{"UseSandbox: true", `ReserveBin: "nova-reserve"`, `SandboxBin: "nova-sandbox"`, "Procs:   OSProcessTable{}"} {
		if !strings.Contains(s, want) {
			t.Errorf("the worker's runner is not wired with %q", want)
		}
	}
}

// An API-rate key goes to a loopback proxy that reads it from the
// environment by name; the worker never reads a provider key itself, which
// every child that is not CleanEnv would inherit.
func TestWorkerKeyProxyReadsKeysByRouteName(t *testing.T) {
	t.Parallel()
	s := serveSource_internal(t)
	if strings.Contains(s, `os.Getenv("NOVA_PROVIDER_API_KEY")`) {
		t.Errorf("the worker reads a provider key from its own environment")
	}
	if !strings.Contains(s, "NewKeyProxy(providers, os.LookupEnv)") {
		t.Errorf("the key proxy is not built from the --providers routes")
	}
}

// --once serves the row like the wake loop does: the readiness probe runs,
// and the row hooks in only after it passes.
func TestWorkerOnceServesThroughServeRow(t *testing.T) {
	t.Parallel()
	s := serveSource_internal(t)
	if !strings.Contains(s, "r.ServeRow(ctx, c.Row, cmp.Or(c.Harness, \"claude\"), c.Once)") {
		t.Errorf("--once does not go through ServeRow, which probes before it hooks in")
	}
}

// Every program runs through edge.Exec, and the bus hook is the runner's
// OSBusHook, not a bare exec.CommandContext that inherits the whole
// environment.
func TestWorkerRunsProgramsThroughEdgeExec(t *testing.T) {
	t.Parallel()
	s := serveSource_internal(t)
	if strings.Contains(s, "exec.Command") {
		t.Errorf("the worker starts a program outside edge.Exec")
	}
	if !strings.Contains(s, "BusHook: OSBusHook{") {
		t.Errorf("the bus hook is not the runner's OSBusHook")
	}
}

// Git is always wired: with no git edge there is no staging, push or rebase,
// and a finished card would report ok with no head.
func TestWorkerWiresGit(t *testing.T) {
	t.Parallel()
	if s := serveSource_internal(t); !strings.Contains(s, "Git: &OSGitOps{") {
		t.Errorf("the worker's runner has no git edge")
	}
}
