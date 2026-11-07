package main

// The shipped runner wiring (functional: a throwaway git repository, a fake
// nova-sprint and a fake nova-reserve on PATH, the script harness).

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestFunctionalShippedWorkerRunsHarnessOutsideAnySlot (functional tier:
// a real git repository and real processes): `nova-worker --once --as r1 --host b1 --harness script
// --work-root <dir> --mirror-root <dir>` against a nova-sprint that deals one read consumer.
// nova-reserve on PATH records every invocation.
//
// Guarantee: a harness runs only inside a nova-reserve slot, freed when it
// ends, under nova-sandbox, and so do the card's done-when tests.
// The shipped wiring (edge/worker.Serve) must give the runner's Wall a
// ReserveBin and sandbox, and set Runner.Procs so a restart can adopt a live
// harness.
func TestFunctionalShippedWorkerRunsHarnessOutsideAnySlot(t *testing.T) { // unitlint: serial, it sets the process environment (t.Setenv)
	tmp := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	origin := filepath.Join(tmp, "origin.git")
	clone := filepath.Join(tmp, "clone")
	_ = os.MkdirAll(origin, 0o755)
	git(origin, "init", "--bare", "-q")
	git(tmp, "clone", "-q", origin, clone)
	_ = os.WriteFile(filepath.Join(clone, "a.txt"), []byte("a\n"), 0o644)
	git(clone, "add", ".")
	git(clone, "commit", "-qm", "a")
	git(clone, "push", "-q", "origin", "HEAD:refs/heads/main")
	head := git(clone, "rev-parse", "HEAD")

	bin := filepath.Join(tmp, "bin")
	_ = os.MkdirAll(bin, 0o755)
	reserveLog := filepath.Join(tmp, "reserve.log")
	sprintLog := filepath.Join(tmp, "sprint.log")
	next := `{"epoch":1,"seq":5,"row":"r1","state":"up","width":1,"harness":"script","working":[{"id":"p1.r1","producer":"p1","kind":"read","stream":"s1","state":"working","gen":2}]}` // dealt in hand
	take := fmt.Sprintf(`{"consumer":"p1.r1","producer":"p1","kind":"read","stream":"s1","repo":%q,"base":"main","stream_ref":"sprint/s1/group","branch":"sprint/s1/p1","head":%q,"tier":"pro","prose":"read:ok","gen":2,"model":"m1","deadline":900,"forge":"","commit_name":"Sprint Bot","commit_email":"1+sprint-bot@users.noreply.example.test"}`, origin, head)
	sprint := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\ncase \"$1\" in\nnext) echo '%s';;\ntake) echo '%s';;\nreport) echo '{\"accepted\":true}';;\nesac\n", sprintLog, next, take)
	_ = os.WriteFile(filepath.Join(bin, "nova-sprint"), []byte(sprint), 0o755)
	bus := "#!/bin/sh\necho 'HOOK OK name=r1 waiting=0' >&2\nexec sleep 3600\n"
	_ = os.WriteFile(filepath.Join(bin, "nova-bus"), []byte(bus), 0o755)
	reserve := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\nwhile [ \"$1\" != -- ]; do shift; done\nshift\nexec \"$@\"\n", reserveLog)
	_ = os.WriteFile(filepath.Join(bin, "nova-reserve"), []byte(reserve), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out, errBuf bytes.Buffer
	code := run_internal([]string{"--once", "--as", "r1", "--host", "b1", "--harness", "script", "--root", filepath.Join(tmp, "root"),
		"--state-dir", filepath.Join(tmp, "state"), "--work-root", filepath.Join(tmp, "clones"), "--mirror-root", filepath.Join(tmp, "mirrors")}, &out, &errBuf)
	sl, _ := os.ReadFile(sprintLog)
	reported := ""
	for _, l := range strings.Split(string(sl), "\n") {
		if strings.HasPrefix(l, "report ") {
			reported = l
		}
	}
	if reported == "" {
		t.Fatalf("no report reached nova-sprint (exit %d, stderr %q, sprint log %q)", code, errBuf.String(), sl)
	}
	order := []string{}
	for _, l := range strings.Split(string(sl), "\n") {
		if verb, _, _ := strings.Cut(l, " "); verb != "" && verb != "beat" { // the heartbeat may come anywhere
			order = append(order, verb)
		}
	}
	if len(order) < 3 || order[0] != "unhook" || order[1] != "hook" || order[2] != "next" {
		t.Fatalf("nova-sprint verbs ran as %v, want unhook, hook, next first", order)
	}
	if _, err := os.Stat(reserveLog); err != nil {
		t.Fatalf("the harness ran and was reported (%q) but nova-reserve was never invoked: it ran outside any slot", reported)
	}
}
