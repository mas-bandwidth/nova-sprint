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

// The flag help names no retired service: the v1 fleet service is retired,
// and the worker itself manages its mirrors.
func TestHelpNamesNoRetiredService(t *testing.T) {
	t.Parallel()
	retired := "mirror-" + "refresh"
	fs := flag.NewFlagSet("nova-worker", flag.ContinueOnError)
	var c worker.Config
	fs.StringVar(&c.Row, "as", "", "worker row name (a machine nova-config gives the bench role)")
	fs.StringVar(&c.Host, "host", "", "bench hostname (defaults to os.Hostname)")
	fs.StringVar(&c.Root, "root", "", "worker root directory for job and probe directories")
	fs.StringVar(&c.StateDir, "state-dir", "", "state directory for logs")
	fs.StringVar(&c.Harness, "harness", "claude", "harness adapter (claude, codex, opencode)")
	fs.StringVar(&c.WorkRoot, "work-root", "", "directory of the work clones, one per stream repo, made on first use (default <root>/clones)")
	fs.StringVar(&c.MirrorRoot, "mirror-root", "", "directory of the bare mirrors, one per repository as <name>.git, cloned on first use and fetched per job (default ~/nova-bench/mirror)")
	fs.StringVar(&c.SprintBin, "sprint-bin", "nova-sprint", "nova-sprint binary (run directly: the worker's environment holds the sprint's credentials)")
	fs.StringVar(&c.Providers, "providers", "", "key proxy routes, <name>=<upstream url>@<KEY NAME>[,...]; each key is read from the environment")
	fs.BoolVar(&c.Once, "once", false, "run one reconciliation/step pass and exit")
	fs.VisitAll(func(f *flag.Flag) {
		if strings.Contains(f.Usage, retired) {
			t.Errorf("flag --%s usage mentions the retired service: %q", f.Name, f.Usage)
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
