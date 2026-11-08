package main

import (
	"bytes"
	"flag"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/worker"
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

// The mirror layout is the worker's own: a bare mirror per repository,
// <name>.git under the mirror root, cloned on first use and fetched per
// job. No flag's help names the retired service.
func TestHelpNamesNoRetiredService(t *testing.T) {
	t.Parallel()
	var c worker.Config
	fs := newFlagSet(&c)
	retired := "mirror-" + "refresh"
	fs.VisitAll(func(f *flag.Flag) {
		if strings.Contains(f.Usage, retired) {
			t.Errorf("%s: usage names the retired service: %q", f.Name, f.Usage)
		}
	})
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
