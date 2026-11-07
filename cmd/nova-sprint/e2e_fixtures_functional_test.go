package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

// TestMain serves the policy engine's child when the test binary is started
// as one: the end-to-end test runs run.Serve in this process, and its
// policy engine starts `<this binary> policy-eval`.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == policy.EvalArg {
		os.Exit(policy.ServeMain())
	}
	os.Exit(m.Run())
}

// transcript is the end-to-end run as a reader sees it: each command, its
// exit and its output. It never holds an environment, so no secret.
type transcript struct {
	mu sync.Mutex
	b  strings.Builder
}

func (tr *transcript) note(format string, args ...any) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	fmt.Fprintf(&tr.b, "# "+format+"\n", args...)
}

func (tr *transcript) record(cmd string, exit int, out string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	fmt.Fprintf(&tr.b, "$ %s\nexit %d\n%s", cmd, exit, out)
	if out != "" && !strings.HasSuffix(out, "\n") {
		tr.b.WriteByte('\n')
	}
	tr.b.WriteByte('\n')
}

// save writes the transcript to run/testdata, where the bench commits it.
func (tr *transcript) save(t *testing.T) {
	path := filepath.Join("..", "..", "run", "testdata", "e2e-transcript.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Logf("transcript: %v", err)
		return
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if err := os.WriteFile(path, []byte(tr.b.String()), 0o644); err != nil {
		t.Logf("transcript: %v", err)
	}
}

// cli runs the built binary with the test's environment.
type cli struct {
	t   *testing.T
	bin string
	env []string
	tr  *transcript
}

// run is one command line, recorded unless quiet.
func (c *cli) run(quiet bool, extra []string, args ...string) (int, string) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.bin, args...)
	cmd.Env = append(slices.Clone(c.env), extra...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	code := 0
	if err != nil {
		code = -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	}
	if !quiet {
		c.tr.record("nova-sprint "+strings.Join(args, " "), code, out.String())
	}
	return code, out.String()
}

// must is a command that exits 0 and prints every part.
func (c *cli) must(args []string, parts ...string) string {
	c.t.Helper()
	code, out := c.run(false, nil, args...)
	if code != 0 {
		c.t.Fatalf("nova-sprint %s: exit %d\n%s", strings.Join(args, " "), code, out)
	}
	for _, p := range parts {
		if !strings.Contains(out, p) {
			c.t.Fatalf("nova-sprint %s lacks %q:\n%s", strings.Join(args, " "), p, out)
		}
	}
	return out
}

// sh runs a helper command (git, nova-bus), recorded.
func sh(t *testing.T, tr *transcript, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		code = -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	}
	tr.record(filepath.Base(name)+" "+strings.Join(args, " "), code, string(out))
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// e2ePG is a throwaway PostgreSQL whose superuser is nova_admin (the role
// up's setup connects as), trust auth on 127.0.0.1 only. It returns the
// passwordless admin DSN.
func e2ePG(t *testing.T) string {
	t.Helper()
	initdb, _ := exec.LookPath("initdb")
	dir := filepath.Join(t.TempDir(), "data")
	if out, err := exec.Command(initdb, "-D", dir, "-U", "nova_admin", "--auth=trust", "--no-locale").CombinedOutput(); err != nil {
		t.Fatalf("initdb: %v\n%s", err, out)
	}
	port := freePort(t)
	ctl := filepath.Join(filepath.Dir(initdb), "pg_ctl")
	opts := fmt.Sprintf("-p %d -c listen_addresses=127.0.0.1 -c unix_socket_directories= -c fsync=off", port)
	if out, err := exec.Command(ctl, "-D", dir, "-l", filepath.Join(dir, "server.log"), "-o", opts, "-w", "start").CombinedOutput(); err != nil {
		t.Fatalf("pg_ctl start: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command(ctl, "-D", dir, "-m", "immediate", "-w", "stop").Run() })
	return fmt.Sprintf("postgres://nova_admin@127.0.0.1:%d/postgres?sslmode=disable", port)
}

// e2eRedis is a throwaway Redis; it returns NOVA_BUS_REDIS's value.
func e2eRedis(t *testing.T) string {
	t.Helper()
	redis, _ := exec.LookPath("redis-server")
	port := freePort(t)
	server := exec.Command(redis, "--bind", "127.0.0.1", "--port", fmt.Sprint(port), "--save", "", "--appendonly", "no", "--dir", t.TempDir())
	server.Stdout, server.Stderr = io.Discard, io.Discard
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })
	return fmt.Sprintf("127.0.0.1:%d", port)
}

// busHook keeps name on the bus, each message it gets appended to inbox.
func busHook(t *testing.T, tr *transcript, env []string, name, inbox string) {
	t.Helper()
	handler := `printf '%s\t%s\n' "$NOVA_BUS_FROM" "$NOVA_BUS_BODY" >> ` + "'" + inbox + "'"
	cmd := exec.Command("nova-bus", "hook", "--as", name, "--exec", handler)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		who := exec.Command("nova-bus", "who")
		who.Env = env
		if out, err := who.Output(); err == nil && strings.Contains(string(out), name+"\t") {
			tr.record("nova-bus hook --as "+name+" --exec <append to inbox> (background)", 0, "on the bus\n")
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s never showed in nova-bus who", name)
}

// fakeGH is a `gh` for the binary's forge edge: auth status names a bot
// login, and pr list reports one open PR at the branch's tip in the local
// origin (FAKE_GH_ORIGIN). Anything else fails.
const fakeGH = `#!/bin/sh
case "$1 $2" in
"auth status") echo "github.com"; echo "  Logged in to github.com account sprint-bot (keyring)"; exit 0 ;;
"pr list")
  sha=$(git --git-dir="$FAKE_GH_ORIGIN" rev-parse --verify --quiet "refs/heads/$6") || { echo "[]"; exit 0; }
  printf '[{"number":7,"url":"https://forge.test/pr/7","headRefName":"%s","headRefOid":"%s","baseRefName":"%s","state":"OPEN"}]\n' "$6" "$sha" "$8"
  exit 0 ;;
esac
echo "fake gh: $1 $2 is not faked" >&2
exit 1
`

// fakeForge is the run side's forge: a PR per head branch, CI green for
// every sha, and Merge moving the local origin's base ref to the gated sha
// (as GitHub's ref update does). Its calls are kept for the transcript.
type fakeForge struct {
	mu     sync.Mutex
	origin string
	calls  []string
	prs    map[string]forge.PRInfo
}

func (f *fakeForge) log(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeForge) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeForge) PR(_ context.Context, _, head, base string) (forge.PRInfo, bool, error) {
	f.log("PR %s -> %s", head, base)
	f.mu.Lock()
	defer f.mu.Unlock()
	pr, ok := f.prs[head+" "+base]
	return pr, ok, nil
}

func (f *fakeForge) OpenPR(_ context.Context, _, head, base, title, _ string) (forge.PRInfo, error) {
	f.log("OpenPR %s -> %s %q", head, base, title)
	f.mu.Lock()
	defer f.mu.Unlock()
	pr := forge.PRInfo{Number: len(f.prs) + 1, HeadRefName: head, HeadRef: head, BaseRefName: base, BaseRef: base, State: "OPEN"}
	f.prs[head+" "+base] = pr
	return pr, nil
}

func (f *fakeForge) Checks(_ context.Context, _ string, shas []string) (map[string]forge.CheckRollup, error) {
	f.log("Checks %s: green", strings.Join(shas, ","))
	out := map[string]forge.CheckRollup{}
	for _, s := range shas {
		out[s] = forge.CheckRollup{SHA: s, State: forge.CheckGreen}
	}
	return out, nil
}

func (f *fakeForge) Merge(_ context.Context, _ string, pr int, sha, base string) error {
	out, err := exec.Command("git", "--git-dir", f.origin, "update-ref", "refs/heads/"+base, sha).CombinedOutput()
	f.log("Merge #%d %s into %s: %v %s", pr, sha, base, err, strings.TrimSpace(string(out)))
	return err
}

// storeSetup is up's store step through the binary. The binary's internal
// mode prints no reason on failure (its JSON is the parent's), so a failure
// is run once more in this process, the same function, for the reason.
func (x *e2e) storeSetup() {
	code, out := x.c.run(false, nil, "--internal-store-setup")
	if code == 0 && strings.Contains(out, `"ready":true`) {
		return
	}
	for _, kv := range [][2]string{{"NOVA_PG_DSN", x.adminDSN}, {"NOVA_SPRINT_PG_PASSWORD", sprintPassword}, {"NOVA_SPRINT_BACKUP_DIR", filepath.Join(x.dir, "backups")}} {
		x.t.Setenv(kv[0], kv[1])
	}
	ctx := context.Background()
	var why []string
	if pg, err := store.Open(ctx, x.sprintDSN, sprintPassword); err != nil {
		why = append(why, "open: "+err.Error())
	} else {
		id, serr := pg.Schema(ctx)
		why = append(why, fmt.Sprintf("schema before: %+v %v", id, serr), fmt.Sprintf("migrate: %v", pg.Migrate(ctx)))
		id, serr = pg.Schema(ctx)
		why = append(why, fmt.Sprintf("schema after: %+v %v", id, serr))
		dir := filepath.Join(x.dir, "backups-diagnosis")
		_ = os.MkdirAll(dir, 0o700)
		_, berr := (verbs.PGBackups{DSN: x.sprintDSN, Password: sprintPassword, Directory: dir, Version: versionString(), Loader: verbs.PGSQLLoader{}}).Backup(ctx, dir)
		why = append(why, fmt.Sprintf("backup: %v", berr))
		pg.Close()
	}
	ready, err := verbs.SprintStoreChild(ctx, true, versionString())
	why = append(why, fmt.Sprintf("SprintStoreChild: ready=%v err=%v", ready, err))
	x.tr.record("up's store step in process, for the reason", exitOf(err), strings.Join(why, "\n"))
	x.t.Fatalf("the store step failed: exit %d %s; in process:\n%s", code, out, strings.Join(why, "\n"))
}

// configSchema is nova-config's schema.sql from the nova-tools module this
// module requires, read before the test moves HOME (the module cache).
func configSchema(t *testing.T) []byte {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/mas-bandwidth/nova-tools/v2").Output()
	if err != nil {
		t.Fatalf("nova-tools module: %v", err)
	}
	schema, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "config", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

// runLogTail records the last lines of run's tick log.
func (x *e2e) runLogTail() {
	b, err := os.ReadFile(filepath.Join(x.dir, "run.log"))
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	x.tr.record(fmt.Sprintf("run's tick log, last %d of %d lines", min(40, len(lines)), len(lines)), 0, strings.Join(lines[max(0, len(lines)-40):], "\n"))
}

// field reads a JSON object's field as text.
func field(t *testing.T, raw string, name string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, raw)
	}
	return fmt.Sprint(m[name])
}
