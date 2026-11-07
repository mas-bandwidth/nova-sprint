package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFunctionalFakeCLIUsesPersistentStoreWithoutTools(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "nova-sprint")
	build := exec.Command("go", "build", "-ldflags", "-X main.binaryVersion=dev+0123456789ab", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	preflight := exec.Command(binary, "help")
	preflight.Env = []string{"PATH=" + dir, "NOVA_PG_DSN=invalid"}
	if out, err := preflight.CombinedOutput(); err != nil || !strings.Contains(string(out), "nova-sprint commands") {
		t.Fatalf("help without store: %v %s", err, out)
	}
	for _, arg := range []string{"help", "version"} {
		c := exec.Command(binary, arg)
		c.Env = []string{"PATH=" + dir, "NOVA_SPRINT_STORE=fake:" + filepath.Join(dir, "missing-parent", "state")}
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%s without store: %v %s", arg, err, out)
		}
	}
	path := filepath.Join(dir, "state.json")
	run := func(args ...string) string {
		t.Helper()
		c := exec.Command(binary, args...)
		c.Env = []string{"PATH=" + dir, "NOVA_SPRINT_STORE=fake:" + path, "NOVA_SPRINT_AS=rowan"}
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
		return string(out)
	}
	if got := run("version"); got != "dev+0123456789ab\n" {
		t.Fatalf("version %q", got)
	}
	if got := run("help"); !strings.Contains(got, "nova-sprint commands") {
		t.Fatalf("help %q", got)
	}
	if got := run("up", "--as", "rowan"); !strings.Contains(got, "UP DONE run on simulation") {
		t.Fatalf("up %q", got)
	}
	if got := run("check", "--history"); !strings.Contains(got, "CHECK history verified") {
		t.Fatalf("history %q", got)
	}
	if got := run("stream", "add", "sample", "--repo", "example/test"); !strings.Contains(got, "sample") {
		t.Fatalf("stream %q", got)
	}
	blocked := exec.Command(binary, "dash", "--pull", "http://127.0.0.1:1")
	blocked.Env = []string{"PATH=" + dir, "NOVA_SPRINT_STORE=fake:" + path}
	if out, err := blocked.CombinedOutput(); err == nil || !strings.Contains(string(out), "fake mode") {
		t.Fatalf("fake pull: %v %s", err, out)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if got := run("show", "sample"); !strings.Contains(got, "sample") {
		t.Fatalf("show %q", got)
	}
	if got := run("clear", "--confirm", "1"); !strings.Contains(got, "CLEARED epoch=2") {
		t.Fatalf("clear %q", got)
	}
	entries, err := os.ReadDir(fakeBackupDir(path))
	if err != nil || len(entries) == 0 {
		t.Fatalf("backup files: %v %v", entries, err)
	}
}
