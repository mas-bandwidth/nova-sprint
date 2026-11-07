package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestWorkerCLIHostAndFlagRefusals(t *testing.T) {
	t.Parallel()
	var out, errBuf bytes.Buffer
	code := run_internal([]string{"--host", "studio.local", "--as", "amd-1.ds"}, &out, &errBuf)
	if code != 1 {
		t.Fatalf("expected exit 1 on coordinator host, got %d", code)
	}
	if !strings.Contains(errBuf.String(), "host studio.local is a coordinator host") {
		t.Fatalf("unexpected refusal text: %q", errBuf.String())
	}

	errBuf.Reset()
	code = run_internal([]string{"--host", "b1"}, &out, &errBuf)
	if code != 1 || !strings.Contains(errBuf.String(), "missing --as <row>") {
		t.Fatalf("expected missing --as refusal, got code=%d err=%q", code, errBuf.String())
	}

	errBuf.Reset()
	code = run_internal([]string{"--host", "b1", "--as", "amd-1.ds", "--harness", "nonexistent", "--root", t.TempDir()}, &out, &errBuf)
	if code != 1 || !strings.Contains(errBuf.String(), "unknown harness") {
		t.Fatalf("expected unknown harness refusal, got code=%d err=%q", code, errBuf.String())
	}
}
