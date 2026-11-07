package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestWorkerCLIHostAndFlagRefusals(t *testing.T) {
	t.Parallel()
	var out, errBuf bytes.Buffer
	code := run_internal([]string{"--host", "studio.local", "--as", "amd-1.ds", "--coordinator-hosts", "studio,studio.local"}, &out, &errBuf)
	if code != 1 {
		t.Fatalf("expected exit 1 on coordinator host, got %d", code)
	}
	if !strings.Contains(errBuf.String(), "host studio.local is a coordinator host") {
		t.Fatalf("unexpected refusal text: %q", errBuf.String())
	}

	errBuf.Reset()
	code = run_internal([]string{"--host", "b1", "--coordinator-hosts", "studio"}, &out, &errBuf)
	if code != 1 || !strings.Contains(errBuf.String(), "missing --as <row>") {
		t.Fatalf("expected missing --as refusal, got code=%d err=%q", code, errBuf.String())
	}

	errBuf.Reset()
	code = run_internal([]string{"--host", "b1", "--coordinator-hosts", "studio", "--as", "amd-1.ds", "--harness", "nonexistent", "--root", t.TempDir()}, &out, &errBuf)
	if code != 1 || !strings.Contains(errBuf.String(), "unknown harness") {
		t.Fatalf("expected unknown harness refusal, got code=%d err=%q", code, errBuf.String())
	}
}

// The worker holds no coordinator list and no provider route of its own
// (drift row 15): with none given it refuses, naming the flag.
func TestWorkerCLIHoldsNoCoordinatorOrProviderDefaults(t *testing.T) {
	t.Parallel()
	var out, errBuf bytes.Buffer
	if code := run_internal([]string{"--host", "studio", "--as", "amd-1.ds"}, &out, &errBuf); code != 1 || !strings.Contains(errBuf.String(), "pass --coordinator-hosts") {
		t.Fatalf("code=%d err=%q: want a refusal naming --coordinator-hosts", code, errBuf.String())
	}
	for _, args := range [][]string{
		{"--provider-upstream", "http://p.example"},
		{"--provider-upstream", "http://p.example", "--provider-hosts", "p.example"},
		{"--provider-hosts", "p.example", "--provider-key", "k"},
	} {
		errBuf.Reset()
		full := append([]string{"--host", "b1", "--coordinator-hosts", "studio", "--as", "amd-1.ds", "--work-clone", t.TempDir()}, args...)
		if code := run_internal(full, &out, &errBuf); code != 1 || !strings.Contains(errBuf.String(), "--provider-") {
			t.Fatalf("args %v: code=%d err=%q: want a refusal naming the provider flags", args, code, errBuf.String())
		}
	}
}
