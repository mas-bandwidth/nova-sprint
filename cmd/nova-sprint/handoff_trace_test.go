package main

// This prefix fixture exercises the real walker and private child channel.
// Lease and credential/schema readiness are fixture facts, not PG or secrets
// proof. The record is written and validated by the real helpers on a fake.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

type handoffTraceFixture struct {
	t            *testing.T
	path         string
	role         string
	ready        [9]bool
	held         bool
	records      []string
	boot         bool
	validated    bool
	walkActivity bool
}

func (f *handoffTraceFixture) add(action, fields string) {
	f.records = append(f.records, fmt.Sprintf("action |-> %q%s", action, fields))
}
func (f *handoffTraceFixture) Verify(_ context.Context, n int, _ verbs.SetupInput) (verbs.SetupCheck, error) {
	if n == 1 && !f.boot {
		f.boot = true
		if f.role == "admin" {
			f.add("Prefix1", `, seat |-> "admin", lease |-> "free"`)
		} else {
			f.add("Prefix2", `, lease |-> "free"`)
		}
	} else if n == 1 && f.role == "sprint" && f.held {
		if !f.validated {
			return verbs.SetupCheck{}, fmt.Errorf("fresh check before validation")
		}
		f.add("Verify2", `, lease |-> "ours"`)
	}
	if f.role == "sprint" && f.held && n >= 2 {
		f.walkActivity = true
	}
	return verbs.SetupCheck{Verified: f.ready[n], Action: !f.ready[n]}, nil
}
func (f *handoffTraceFixture) Act(_ context.Context, n int, _ verbs.SetupInput) error {
	f.ready[n] = true
	if n == 2 && f.role == "admin" {
		if !f.held {
			return fmt.Errorf("step 2 without lease")
		}
		f.add("PreparedRole", `, credential |-> TRUE, schema |-> TRUE, lease |-> "ours"`)
	}
	return nil
}
func (f *handoffTraceFixture) Completion(context.Context) (string, error) {
	return "UP DONE fixture complete", nil
}
func (f *handoffTraceFixture) Acquire(context.Context) (verbs.SetupLease, error) {
	if f.held {
		return nil, fmt.Errorf("lease held")
	}
	f.held = true
	if f.role == "admin" {
		f.add("Acquire1", `, lease |-> "ours"`)
	} else {
		f.add("Acquire2", `, lease |-> "ours"`)
	}
	return f, nil
}
func (f *handoffTraceFixture) Monitor(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(ctx)
}
func (f *handoffTraceFixture) Check(context.Context) error {
	if !f.held {
		return fmt.Errorf("lease missing")
	}
	return nil
}
func (f *handoffTraceFixture) Close() error { f.held = false; return nil }
func (f *handoffTraceFixture) PrepareHandoff(ctx context.Context, in verbs.SetupInput) (string, bool, error) {
	if !f.held || !f.ready[2] {
		return "", false, fmt.Errorf("prerequisites absent")
	}
	st, err := openFileStore(f.path)
	if err != nil {
		return "", false, err
	}
	digest, err := handoffDigest(in)
	if err != nil {
		return "", false, err
	}
	id := strings.Repeat("a", 64)
	b, err := json.Marshal(handoffEnvelope{ID: id, Actor: in.Actor, Digest: digest})
	if err != nil {
		return "", false, err
	}
	written, err := writeSetupContinuation(ctx, st, bytes.NewReader(b), time.Unix(1000, 0))
	if err != nil {
		return "", false, err
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return "", false, err
	}
	record, found, err := findSetupContinuation(ctx, st, snap, id)
	if err != nil || !found || record.Epoch != snap.Epoch || written != id {
		return "", false, fmt.Errorf("record not persisted")
	}
	f.add("SaveRecord", `, record |-> "setup", recordEpoch |-> 0, epoch |-> 0, credential |-> TRUE, schema |-> TRUE, lease |-> "ours"`)
	return id, true, nil
}
func (f *handoffTraceFixture) ValidateHandoff(ctx context.Context, id string, in verbs.SetupInput) error {
	st, err := openFileStore(f.path)
	if err != nil {
		return err
	}
	digest, err := handoffDigest(in)
	if err != nil {
		return err
	}
	if err = validateHandoffRecord(ctx, st, id, in.Actor, digest); err != nil {
		return err
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return err
	}
	record, found, err := findSetupContinuation(ctx, st, snap, id)
	if err != nil || !found || snap.Epoch != 1 || record.Epoch != snap.Epoch {
		return fmt.Errorf("unexpected record state")
	}
	action, lease := "Prevalidate", "free"
	if f.held {
		action, lease = "Validate", "ours"
		f.validated = true
	}
	f.add(action, fmt.Sprintf(`, record |-> "setup", recordEpoch |-> 0, epoch |-> 0, binding |-> TRUE, credential |-> TRUE, schema |-> TRUE, lease |-> %q`, lease))
	return nil
}

func TestHandoffTraceChild(t *testing.T) {
	mode := ""
	for _, a := range os.Args {
		if a == "--internal-up-phase1" || a == "--internal-up-phase2" {
			mode = a
		}
	}
	if mode == "" {
		return
	}
	if len(os.Args) < 3 {
		t.Fatal("missing fixture paths")
	}
	state, events := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	f := &handoffTraceFixture{t: t, path: state, role: "admin"}
	f.ready[1] = true
	args := []string{"up", "--as", "alice"}
	if mode == "--internal-up-phase1" {
		if os.Getenv("NOVA_PG_ADMIN_PASSWORD") != "admin_fixture" || os.Getenv("NOVA_SPRINT_PG_PASSWORD") != "" {
			t.Fatal("wrong admin environment")
		}
	} else {
		if os.Getenv("NOVA_SPRINT_PG_PASSWORD") != "sprint_fixture" || os.Getenv("NOVA_PG_ADMIN_PASSWORD") != "" {
			t.Fatal("wrong sprint environment")
		}
		f.role = "sprint"
		for i := 2; i <= 8; i++ {
			f.ready[i] = true
		}
		id := ""
		for i, a := range os.Args {
			if a == "--continuation" && i+1 < len(os.Args) {
				id = os.Args[i+1]
			}
		}
		args = append(args, "--continuation", id)
	}
	st, err := openFileStore(state)
	if err != nil {
		t.Fatal(err)
	}
	e := verbs.Engine{Actor: "alice", Store: st, Bus: present{"alice"}, Now: func() time.Time { return time.Unix(1002, 0) }, Setup: f, Lock: f, ReportContinuation: func(ctx context.Context, id string, in verbs.SetupInput) error {
		if f.held {
			return fmt.Errorf("lease still held")
		}
		return reportHandoff(ctx, id, in)
	}}
	result := e.Execute(context.Background(), args)
	handoffChildFinish(t, f, result, events)
}

func handoffChildFinish(t *testing.T, f *handoffTraceFixture, result verbs.Result, events string) {
	t.Helper()
	if f.role == "sprint" && hasHandoffFixtureArg("--fixture-stale") {
		if result.Exit != 1 || f.held || f.boot || f.walkActivity || f.validated {
			t.Fatalf("stale continuation did not refuse before bootstrap: %+v", result)
		}
		f.add("Refuse", `, lease |-> "free", exposed |-> {"status","id"}, attemptEffect |-> FALSE`)
		if err := os.WriteFile(events, []byte(strings.Join(f.records, "\n")), 0600); err != nil {
			t.Fatal(err)
		}
		os.Exit(1)
	}
	if result.Exit != 0 || f.held {
		t.Fatalf("walk: %+v held=%v", result, f.held)
	}
	if f.role == "sprint" {
		if !f.validated || !f.walkActivity {
			t.Fatal("phase2 did not validate and walk")
		}
		f.add("Effect", `, lease |-> "free", exposed |-> {"status","id"}, attemptEffect |-> TRUE`)
	}
	if err := os.WriteFile(events, []byte(strings.Join(f.records, "\n")), 0600); err != nil {
		t.Fatal(err)
	}
}

func hasHandoffFixtureArg(want string) bool {
	for _, a := range os.Args {
		if a == want {
			return true
		}
	}
	return false
}

func handoffTraceScripts(t *testing.T, dir string) (string, string, string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	state, events := filepath.Join(dir, "state.json"), filepath.Join(dir, "child.events")
	secrets := `#!/bin/sh
key="$3"
while [ "$1" != "--" ]; do shift; done
shift
if [ "$key" = NOVA_PG_ADMIN_PASSWORD ]; then export NOVA_PG_ADMIN_PASSWORD=admin_fixture; else export NOVA_SPRINT_PG_PASSWORD=sprint_fixture; fi
exec "$@"
`
	child := fmt.Sprintf("#!/bin/sh\nexec %s -test.run '^TestHandoffTraceChild$' -- \"$@\" %s %s\n", strconv.Quote(binary), strconv.Quote(state), strconv.Quote(events))
	for name, contents := range map[string]string{"nova-secrets": secrets, "child": child} {
		if err = os.WriteFile(filepath.Join(dir, name), []byte(contents), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return state, events, filepath.Join(dir, "child")
}

func TestEmitHandoffTrace(t *testing.T) {
	dir := t.TempDir()
	state, events, child := handoffTraceScripts(t, dir)
	t.Setenv("PATH", dir)
	st, err := openFileStore(state)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := st.Snapshot(context.Background())
	if err != nil || snap.Epoch != 1 || snap.Seq != 0 {
		t.Fatalf("initial snapshot %+v %v", snap, err)
	}
	cfg := productionSetupConfig{Binary: child, Secrets: verbs.NovaSecrets{Store: "/fixture", KeyFile: "/fixture", Seat: "fixture"}}
	id, _, code := runUpChild(context.Background(), cfg, "NOVA_PG_ADMIN_PASSWORD", "--internal-up-phase1", nil, true)
	if code != 0 || id != strings.Repeat("a", 64) {
		t.Fatalf("phase1 status %q code %d", id, code)
	}
	raw, err := os.ReadFile(events)
	if err != nil {
		t.Fatal(err)
	}
	entries := strings.Split(string(raw), "\n")
	if len(entries) != 4 {
		t.Fatalf("unexpected child observations %q", raw)
	}
	entries = append([]string{`action |-> "Init", epoch |-> 0, recordEpoch |-> 0, binding |-> TRUE, seat |-> "admin", lease |-> "free", credential |-> FALSE, schema |-> FALSE, record |-> "absent", exposed |-> {}`}, entries...)
	entries = append(entries, `action |-> "Release", lease |-> "free", exposed |-> {"status","id"}`)
	stale := os.Getenv("NOVA_HANDOFF_TRACE_STALE") == "1"
	args := []string{"--continuation", id}
	if stale {
		clearHandoffFixture(t, st)
		entries = append(entries, `action |-> "ChangeEpoch", epoch |-> 1`)
		args = append(args, "--fixture-stale")
	}
	_, output, code := runUpChild(context.Background(), cfg, "NOVA_SPRINT_PG_PASSWORD", "--internal-up-phase2", args, false)
	wantCode := 0
	if stale {
		wantCode = 1
	}
	if code != wantCode {
		t.Fatalf("phase2 code %d: %s", code, output)
	}
	entries = append(entries, `action |-> "Launch2", seat |-> "sprint", lease |-> "free"`)
	raw, err = os.ReadFile(events)
	if err != nil {
		t.Fatal(err)
	}
	phase2 := strings.Split(string(raw), "\n")
	wantEvents := 6
	if stale {
		wantEvents = 1
	}
	if len(phase2) != wantEvents {
		t.Fatalf("unexpected phase2 observations %q", raw)
	}
	entries = append(entries, phase2...)
	writeHandoffTrace(t, entries)
}

func clearHandoffFixture(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	snap, err := st.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := st.Clear(ctx, store.Op{ID: "fixture-clear", Kind: "clear", Actor: "alice", Epoch: snap.Epoch, At: time.Unix(1001, 0)})
	if err != nil || !receipt.Accepted {
		t.Fatalf("clear %+v %v", receipt, err)
	}
	snap, err = st.Snapshot(ctx)
	if err != nil || snap.Epoch != 2 {
		t.Fatalf("epoch %+v %v", snap, err)
	}
}

func writeHandoffTrace(t *testing.T, entries []string) {
	t.Helper()
	path := os.Getenv("NOVA_HANDOFF_TRACE_PATH")
	if path == "" {
		path = filepath.Join(t.TempDir(), "SetupHandoffTraceLog.tla")
	}
	var lines []string
	for i, e := range entries {
		lines = append(lines, fmt.Sprintf("  [i |-> %d, %s]", i+1, e))
	}
	data := "---------------- MODULE SetupHandoffTraceLog ----------------\nEXTENDS Naturals, Sequences\nTraceLog == <<\n" + strings.Join(lines, ",\n") + "\n>>\n==============================================================\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("trace %s records=%d", path, len(lines))
}
