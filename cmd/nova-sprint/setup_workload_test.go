package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

// fleetFake is nova-config and nova-fleet for the workload: the machine
// list it was given, the service records added through it, and every
// argv it was asked to run, in order.
type fleetFake struct {
	machines string // the items of machine list --json
	rows     int
	services []map[string]any
	calls    [][]string
	fail     map[string]error // by the call's first two words
}

func (f *fleetFake) command(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if err := f.fail[name+" "+args[0]]; err != nil {
		return nil, err
	}
	switch {
	case name == "nova-config" && args[0] == "machine":
		return []byte(fmt.Sprintf(`{"result":{"verb":"machine list","status":"ok","exit":0},"facts":{"kind":"machine","rows":%d},"items":[%s]}`, f.rows, f.machines)), nil
	case name == "nova-config" && args[0] == "service" && args[1] == "list":
		items := []map[string]any{}
		for _, s := range f.services {
			items = append(items, map[string]any{"kind": "service", "fields": s})
		}
		out, err := json.Marshal(map[string]any{"result": map[string]any{"verb": "service list", "status": "ok", "exit": 0}, "facts": map[string]any{"kind": "service", "rows": len(items)}, "items": items})
		return out, err
	case name == "nova-config" && args[0] == "service" && args[1] == "add":
		f.services = append(f.services, serviceAdded(args))
	}
	return nil, nil
}

// serviceAdded is the record a service add's flags describe, as service
// list --json would return it.
func serviceAdded(args []string) map[string]any {
	flag := func(name string) string {
		i := slices.Index(args, name)
		return args[i+1]
	}
	var argv []string
	var env map[string]string
	_ = json.Unmarshal([]byte(flag("--argv")), &argv)
	_ = json.Unmarshal([]byte(flag("--env")), &env)
	return map[string]any{"name": args[2], "machine": flag("--machine"), "seat": flag("--seat"), "keys": flag("--keys"), "argv": argv, "every": 0, "keepalive": true, "enabled": true, "env": env, "log": ""}
}

// Two benches and studio, which runs the sprint and is not a bench here
// (a run machine with the bench role gets a worker like any bench); amd-2
// has no cores.
const workloadMachines = `{"kind":"machine","fields":{"name":"studio","address":"100.100.1.2","roles":"services","cores":8}},` +
	`{"kind":"machine","fields":{"name":"amd-1","address":"100.100.1.5","roles":"bench,runner","cores":16}},` +
	`{"kind":"machine","fields":{"name":"amd-2","address":"100.100.1.6","roles":"bench"}}`

func workloadRun() verbs.FleetService {
	return verbs.FleetService{Name: "sprint-run", Machine: "studio", Seat: "studio", Keys: []string{"NOVA_BUS_PASSWORD", "NOVA_SPRINT_PG_PASSWORD"},
		Env: map[string]string{"NOVA_PG_DSN": "postgres://nova_sprint@db:5432/nova", "NOVA_BUS_REDIS": "100.100.1.3:6379", "NOVA_BUS_USER": "sprint", "NOVA_PG_PASSWORD_ENV": "NOVA_SPRINT_PG_PASSWORD"}}
}

type snapReader struct{ snap store.Snapshot }

func (s snapReader) Snapshot(context.Context) (store.Snapshot, error) { return s.snap, nil }

func testWorkload(f *fleetFake, rows map[string]store.Row, present map[string]bool) productionWorkload {
	return productionWorkload{services: verbs.FleetServices{Command: f.command}.For("rowan", "nova-sprint up 7/8 workers"), run: workloadRun(),
		store:   snapReader{store.Snapshot{Epoch: 1, Rows: rows}},
		present: func(context.Context) (map[string]bool, error) { return present, nil },
		lastLine: func(_ context.Context, machine, service string) string {
			return fmt.Sprintf("%q", "last of "+service+" on "+machine)
		},
		version: func(context.Context, string) error { return nil }}
}

// One record per worker bench, exactly the worker's flags, its own seat,
// the keys and environment sprint-run's store and bus need; one
// nova-fleet play limited to those benches applies them all.
func TestInstallWorkersWritesOneRecordPerBenchAndOnePlay(t *testing.T) {
	f := &fleetFake{machines: workloadMachines, rows: 3}
	if err := testWorkload(f, nil, nil).InstallWorkers(context.Background()); err != nil {
		t.Fatal(err)
	}
	var adds, plays [][]string
	for _, c := range f.calls {
		if len(c) > 2 && c[1] == "service" && c[2] == "add" {
			adds = append(adds, c)
		}
		if c[0] == "nova-fleet" {
			plays = append(plays, c)
		}
	}
	want := []string{"nova-config", "service", "add", "sprint-worker-amd-1", "--machine", "amd-1",
		"--argv", `["nova-sprint","worker","--as","amd-1","--work-root",".local/share/nova-sprint/work","--harness","opencode","--providers","deepseek=https://api.deepseek.com@DEEPSEEK_API_KEY,inception=https://api.inceptionlabs.ai@INCEPTION_API_KEY","--root",".local/state/nova-worker/amd-1","--state-dir",".local/state"]`,
		"--keepalive", "true", "--enabled", "true",
		"--env", `{"NOVA_BUS_REDIS":"100.100.1.3:6379","NOVA_BUS_USER":"sprint","NOVA_PG_DSN":"postgres://nova_sprint@db:5432/nova"}`,
		"--seat", "amd-1", "--keys", "DEEPSEEK_API_KEY,INCEPTION_API_KEY,NOVA_BUS_PASSWORD,NOVA_SPRINT_PG_PASSWORD",
		"--actor", "rowan", "--reason", "nova-sprint up 7/8 workers"}
	if len(adds) != 2 || !slices.Equal(adds[0], want) || adds[1][3] != "sprint-worker-amd-2" {
		t.Fatalf("records:\n%q\nwant\n%q", adds, want)
	}
	play := []string{"nova-fleet", "services", "--", "-l", "amd-1,amd-2", "-e", "nova_service_only=sprint-worker-amd-1,sprint-worker-amd-2"}
	if len(plays) != 1 || !slices.Equal(plays[0], play) {
		t.Fatalf("plays %q, want %q", plays, play)
	}
	// Again: the records exist and match, nothing is added, the play runs.
	f.calls = nil
	if err := testWorkload(f, nil, nil).InstallWorkers(context.Background()); err != nil || slices.ContainsFunc(f.calls, func(c []string) bool { return len(c) > 2 && c[2] == "add" }) {
		t.Fatal(err, f.calls)
	}
}

// A failed play is a refusal naming the benches and nova-fleet's cause; a
// record that differs is the record's own refusal, before any play.
func TestInstallWorkersRefusals(t *testing.T) {
	f := &fleetFake{machines: workloadMachines, rows: 3, fail: map[string]error{"nova-fleet services": errors.New("amd-2: UNREACHABLE")}}
	err := testWorkload(f, nil, nil).InstallWorkers(context.Background())
	var op *verbs.OperationError
	if !errors.As(err, &op) || !strings.Contains(op.Problem, "amd-1,amd-2 failed: amd-2: UNREACHABLE") || !strings.Contains(op.Fix, "nova-fleet check") {
		t.Fatal(err)
	}
	f = &fleetFake{machines: workloadMachines, rows: 3}
	f.services = []map[string]any{{"name": "sprint-worker-amd-1", "machine": "amd-1", "seat": "amd-1", "keys": "", "argv": []string{"nova-sprint", "old"}, "keepalive": true, "enabled": true, "env": map[string]string{}}}
	err = testWorkload(f, nil, nil).InstallWorkers(context.Background())
	if !errors.As(err, &op) || !strings.Contains(op.Problem, "sprint-worker-amd-1 conflicts with setup") || slices.ContainsFunc(f.calls, func(c []string) bool { return c[0] == "nova-fleet" }) {
		t.Fatal(err, f.calls)
	}
}

// Step 7 verifies only when every worker bench is on the bus and up in
// the store; through RuntimeSetup it is an act first, then a refusal
// naming each bench that is not, with its unit's last log line.
func TestWorkersReadyNamesEachBenchAndItsLog(t *testing.T) {
	f := &fleetFake{machines: workloadMachines, rows: 3}
	rows := map[string]store.Row{"amd-1": {Name: "amd-1", State: "up", Role: "worker"}, "amd-2": {Name: "amd-2", State: "down", Role: "worker"}}
	w := testWorkload(f, rows, map[string]bool{"amd-1": true})
	ok, err := w.WorkersReady(context.Background())
	var nr *verbs.WorkloadNotReady
	if ok || !errors.As(err, &nr) || !strings.Contains(nr.Why, `1 of the worker benches are not hooked and up: amd-2 (not on the bus, store row down; last log line: "last of sprint-worker-amd-2 on amd-2")`) || strings.Contains(nr.Why, "amd-1 (") {
		t.Fatal(ok, err)
	}
	check, err := (verbs.RuntimeSetup{Workload: w}).Verify(context.Background(), 7, verbs.SetupInput{})
	if err != nil || check.Verified || !check.Action || check.Refusal == nil || !strings.Contains(check.Refusal.Problem, "amd-2 (not on the bus") || !strings.Contains(check.Refusal.Fix, "sprint-worker-<bench>") {
		t.Fatalf("%+v %v", check, err)
	}
	rows["amd-2"] = store.Row{Name: "amd-2", State: "up", Role: "worker"}
	w = testWorkload(f, rows, map[string]bool{"amd-1": true, "amd-2": true})
	if check, err = (verbs.RuntimeSetup{Workload: w}).Verify(context.Background(), 7, verbs.SetupInput{}); err != nil || !check.Verified || !strings.Contains(check.Line, "probed with the policy's models") {
		t.Fatalf("%+v %v", check, err)
	}
	// Every bench up, one at another version: not verified, naming it and
	// the install that fixes it (step 5's check, per bench).
	w.version = func(_ context.Context, m string) error {
		if m == "amd-2" {
			return serviceVersionRefusal("v2.1.0", m, "amd-2 reports v2.0.9, this binary is v2.1.0")
		}
		return nil
	}
	check, err = (verbs.RuntimeSetup{Workload: w}).Verify(context.Background(), 7, verbs.SetupInput{})
	if err != nil || check.Verified || check.Refusal == nil || !strings.Contains(check.Refusal.Problem, "amd-2 reports v2.0.9") || !strings.Contains(check.Refusal.Fix, "--tag v2.1.0 amd-2") {
		t.Fatalf("%+v %v", check, err)
	}
	w.version = nil
	if ok, err := w.WorkersReady(context.Background()); ok || err == nil {
		t.Fatal("a workload with no version check verified")
	}
}

// Step 8: every worker row dealable; a bench with no cores is named, held
// at width 0, with the nova-config command that gives it cores.
func TestSmokeReadyNeedsEveryWorkerDealable(t *testing.T) {
	f := &fleetFake{machines: workloadMachines, rows: 3}
	rows := map[string]store.Row{"amd-1": {Name: "amd-1", State: "up"}, "amd-2": {Name: "amd-2", State: "up"}}
	present := map[string]bool{"amd-1": true, "amd-2": true}
	ok, err := testWorkload(f, rows, present).SmokeReady(context.Background())
	var nr *verbs.WorkloadNotReady
	if ok || !errors.As(err, &nr) || nr.Why != "not every worker row is dealable: amd-2 is held at width 0: "+roster.NoCores || !strings.HasPrefix(nr.Fix, "nova-config machine set amd-2 --cores <n>") {
		t.Fatal(ok, err)
	}
	f.machines = strings.Replace(workloadMachines, `"roles":"bench"}`, `"roles":"bench","cores":4}`, 1)
	if ok, err = testWorkload(f, rows, present).SmokeReady(context.Background()); !ok || err != nil {
		t.Fatal(ok, err)
	}
	delete(present, "amd-1")
	if ok, err = testWorkload(f, rows, present).SmokeReady(context.Background()); ok || !errors.As(err, &nr) || !strings.Contains(nr.Why, "amd-1 is not hooked and up") {
		t.Fatal(ok, err)
	}
	if err = (productionWorkload{}).RunSmoke(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// "none" is a valid services choice and verifies.
func TestServicesNoneVerifies(t *testing.T) {
	w := productionWorkload{}
	if ok, err := w.ServicesReady(context.Background(), nil); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := w.InstallServices(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

// The unit's last log line comes over the inventory's pinned ssh.
func TestLastServiceLogLineReadsTheUnitLogOverSSH(t *testing.T) {
	var seen [][]string
	p := productionSetup{cfg: configForSetup(), command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		seen = append(seen, append([]string{name}, args...))
		if name == "nova-config" {
			return []byte(`{"ansible_host":"100.100.1.6","ansible_user":"nova"}`), nil
		}
		return []byte("line one\nworker refused: missing --work-clone <dir>\n"), nil
	}}
	got := p.lastServiceLogLine(context.Background(), "amd-2", "sprint-worker-amd-2")
	if got != `"worker refused: missing --work-clone <dir>"` {
		t.Fatal(got)
	}
	want := []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=yes", "--", "nova@100.100.1.6", "tail", "-n", "1", ".local/state/nova/services/sprint-worker-amd-2.log"}
	if len(seen) != 2 || !slices.Equal(seen[1], want) {
		t.Fatalf("%q", seen)
	}
	p.command = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("no route") }
	if got = p.lastServiceLogLine(context.Background(), "amd-2", "sprint-worker-amd-2"); !strings.Contains(got, "unreadable") || !strings.Contains(got, "no route") {
		t.Fatal(got)
	}
}

// The Setup the engine really gets forwards every seam it type-asserts
// (section 40: up refused "the setup walk has no completion").
func TestProductionSetupForwardsEverySeam(t *testing.T) {
	st := store.NewFake()
	env := verbs.ExecEnvironment{Choices: verbs.Choices{Store: st, Now: func() time.Time { return time.Unix(1000, 0) }}}
	setup, _ := makeProductionSetup(st, env, "alice", "v1.6.0", configForSetup())
	if _, ok := setup.(verbs.SetupCompletion); !ok {
		t.Fatal("no completion")
	}
	if _, ok := setup.(verbs.SetupHandoff); !ok {
		t.Fatal("no handoff")
	}
}

// The worker service records name their directories under the login's
// home; the worker takes a relative one there, never under its cwd.
func TestWorkerDirectoriesAreUnderHome(t *testing.T) {
	for in, want := range map[string]string{".local/state": "/home/nova/.local/state", "/abs/dir": "/abs/dir", "": ""} {
		if got := underHome("/home/nova", in); got != want {
			t.Fatalf("%q: %q, want %q", in, got, want)
		}
	}
}
