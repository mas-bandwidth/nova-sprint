package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestWorkerCLIFlagRefusals(t *testing.T) {
	t.Parallel()
	var out, errBuf bytes.Buffer
	if code := run_internal([]string{"--host", "b1"}, &out, &errBuf); code != 1 || !strings.Contains(errBuf.String(), "missing --as <row>") {
		t.Fatalf("expected missing --as refusal, got code=%d err=%q", code, errBuf.String())
	}
	errBuf.Reset()
	if code := run_internal([]string{"--host", "b1", "--as", "amd-1", "--harness", "nonexistent", "--root", t.TempDir()}, &out, &errBuf); code != 1 || !strings.Contains(errBuf.String(), "unknown harness") {
		t.Fatalf("expected unknown harness refusal, got code=%d err=%q", code, errBuf.String())
	}
	// The flags of one work clone and of a coordinator list are gone
	// (directives 41, 42): the flag set refuses them.
	for _, gone := range []string{"--coordinator-hosts", "--work-clone", "--mirror", "--origin", "--provider-upstream", "--secrets-bin"} {
		errBuf.Reset()
		if code := run_internal([]string{"--as", "amd-1", gone, "x"}, &out, &errBuf); code != 2 {
			t.Fatalf("%s: code=%d err=%q: want the flag set's refusal", gone, code, errBuf.String())
		}
	}
}

// The worker holds no provider route of its own (drift row 15): a route is
// <name>=<url>@<KEY>, and a malformed one is refused naming the form.
func TestWorkerCLIProviderRoutesAreFlags(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"deepseek", "deepseek=https://api.example@lower", "DeepSeek=https://api.example@K", "deepseek=ftp://x@K", "deepseek=https://u:p@api.example@K", "d=https://a.example@K,d=https://b.example@K"} {
		var out, errBuf bytes.Buffer
		if code := run_internal([]string{"--host", "b1", "--as", "amd-1", "--harness", "script", "--providers", bad}, &out, &errBuf); code != 1 || !strings.Contains(errBuf.String(), "--providers") {
			t.Fatalf("--providers %q: code=%d err=%q: want a refusal naming --providers", bad, code, errBuf.String())
		}
	}
}

// --wall-read repeats, and a relative or missing directory is refused
// naming the flag before the worker starts.
func TestWorkerCLIWallReadRefusals(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, c := range []struct{ arg, why string }{{"sdk/go", "not an absolute path"}, {dir + "/missing", "not a directory"}} {
		var out, errBuf bytes.Buffer
		if code := run_internal([]string{"--host", "b1", "--as", "amd-1", "--harness", "script", "--wall-read", dir, "--wall-read", c.arg}, &out, &errBuf); code != 1 || !strings.Contains(errBuf.String(), "--wall-read") || !strings.Contains(errBuf.String(), c.why) {
			t.Fatalf("--wall-read %s: code=%d err=%q", c.arg, code, errBuf.String())
		}
	}
}
