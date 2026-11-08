package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/store"
	"testing"
)

type runtimeStore struct {
	ready   bool
	actions int
}

func (s *runtimeStore) Ready(context.Context) (bool, error) { return s.ready, nil }
func (s *runtimeStore) Install(context.Context) error       { s.ready = true; s.actions++; return nil }

type runtimeWork struct {
	services, workers, smoke bool
	actions                  int
	installed                map[string]policy.Provider // the providers InstallWorkers was given
}

func (w *runtimeWork) ServicesReady(context.Context, []string) (bool, error) { return w.services, nil }
func (w *runtimeWork) InstallServices(context.Context, []string) error {
	w.services = true
	w.actions++
	return nil
}
func (w *runtimeWork) WorkersReady(context.Context) (bool, error) { return w.workers, nil }
func (w *runtimeWork) InstallWorkers(_ context.Context, chosen map[string]policy.Provider) error {
	w.installed = chosen
	w.workers = true
	w.actions++
	return nil
}
func (w *runtimeWork) SmokeReady(context.Context) (bool, error) { return w.smoke, nil }
func (w *runtimeWork) RunSmoke(context.Context) error           { w.smoke = true; w.actions++; return nil }
func TestRuntimeSetupOwnerChecksAreReverified(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := &runtimeStore{}
	w := &runtimeWork{}
	r := RuntimeSetup{Store: st, Workload: w}
	for _, step := range []int{2, 8} {
		for round := 0; round < 2; round++ {
			check, err := r.Verify(ctx, step, SetupInput{})
			if err != nil {
				t.Fatal(step, err)
			}
			if check.Action {
				if err = r.Act(ctx, step, SetupInput{}); err != nil {
					t.Fatal(step, err)
				}
				check, err = r.Verify(ctx, step, SetupInput{})
			}
			if err != nil || !check.Verified {
				t.Fatal(step, check, err)
			}
		}
	}
	if st.actions != 1 || w.actions != 1 {
		t.Fatal(st.actions, w.actions)
	}
	st.ready = false
	check, err := r.Verify(ctx, 2, SetupInput{})
	if err != nil || check.Verified || !check.Action {
		t.Fatal(check, err)
	}
}

// declared is a made-up policy's providers: acme and zeta.
func declared(context.Context) (map[string]policy.Provider, error) {
	return map[string]policy.Provider{"acme": acmeProvider, "zeta": {URL: "https://zeta.test/api", Key: "ZETA_TOKEN", Probe: "https://zeta.test/api/v1/chat"}}, nil
}

// A service is a provider the active policy declares: any other, or one
// named twice, is refused naming those it declares and the line that adds
// one; with none declared, every service is refused.
func TestRuntimeSetupUnknownServicesFailClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := RuntimeSetup{Providers: declared}
	fix := `nova-sprint up --services <provider>[,...] or --services none; a provider is declared in the policy's catalog() "providers" (nova-sprint policy show, then nova-sprint policy set <file> --reason <why>)`
	for _, value := range []string{"unknown", "acme,acme", "acme,"} {
		_, _, err := r.services(ctx, value)
		var op *OperationError
		if !errors.As(err, &op) || !strings.HasSuffix(op.Problem, "it declares: acme, zeta") || op.Fix != fix {
			t.Fatal(value, err)
		}
	}
	names, chosen, err := r.services(ctx, "zeta,acme")
	if err != nil || !slices.Equal(names, []string{"acme", "zeta"}) || chosen["acme"] != acmeProvider || chosen["zeta"].Key != "ZETA_TOKEN" {
		t.Fatal(names, chosen, err)
	}
	if names, chosen, err = r.services(ctx, "none"); err != nil || names != nil || chosen != nil {
		t.Fatal(names, chosen, err)
	}
	var op *OperationError
	if _, _, err = (RuntimeSetup{}).services(ctx, "acme"); !errors.As(err, &op) || !strings.HasSuffix(op.Problem, "it declares: none") {
		t.Fatal(err)
	}
}

// Step 6, unchosen, asks the human which service to set up only when the
// active policy declares a provider; with none declared it says so, and
// the next line is --services none.
func TestServicesCheckSaysWhenThePolicyDeclaresNoProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := Choices{Store: store.NewFake(), Actor: "alice", Now: func() time.Time { return time.Unix(1000, 0) }}
	r := RuntimeSetup{Prerequisites: SetupPrerequisites{Env: ExecEnvironment{Choices: c}}}
	check, err := r.servicesCheck(ctx, SetupInput{})
	want := SetupCheck{Line: "UP 6/8 services: the active policy declares no provider or bench model: declare them (nova-sprint policy show, then policy set <file>) or run up --services none", Next: "nova-sprint up --services none"}
	if err != nil || check != want {
		t.Fatalf("%+v %v", check, err)
	}
	r.Providers = declared
	check, err = r.servicesCheck(ctx, SetupInput{})
	if err != nil || !strings.HasPrefix(check.Line, "UP 6/8 services: not chosen\nASK the human") || check.Next != "nova-sprint up --services <names|none>" {
		t.Fatalf("%+v %v", check, err)
	}
}

// workersRuntime is step 7 on fakes: a services machine and one bench,
// services saved as given, the control seat's store holding saved.
func workersRuntime(t *testing.T, services string, saved bool) (RuntimeSetup, *runtimeWork, *[]string) {
	t.Helper()
	ctx := context.Background()
	calls := &[]string{}
	cmd := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "nova-config" {
			return []byte(`{"result":{"verb":"machine list","status":"ok","exit":0},"facts":{"kind":"machine","rows":2},"items":[{"kind":"machine","fields":{"name":"studio","roles":"services"}},{"kind":"machine","fields":{"name":"amd-1","roles":"bench"}}]}`), nil
		}
		if name == "nova-secrets" {
			*calls = append(*calls, args[1]+"->"+args[3])
		}
		return nil, nil
	}
	c := Choices{Store: store.NewFake(), Actor: "alice", Now: func() time.Time { return time.Unix(1000, 0) }}
	for _, kv := range [][2]string{{"dashboard", "tailnet"}, {"services", services}} {
		if err := c.Save(ctx, kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	work := &runtimeWork{}
	r := RuntimeSetup{Prerequisites: SetupPrerequisites{Env: ExecEnvironment{Choices: c}}, Workload: work, Services: FleetServices{Command: cmd}, Providers: declared,
		Keys:       ServiceKey{Sink: &sinkFake{saved: saved}},
		Role:       SprintRoleBootstrap{Secrets: NovaSecrets{Seat: "control", Store: "/store", KeyFile: "/key"}, Grants: FleetGrants{Store: "/store", KeyFile: "/key", Pins: "/pins", ControlSeat: "control", Command: cmd}},
		RunService: FleetService{Name: "sprint-run", Machine: "studio", Seat: "swarm-studio"}, Dashboards: map[string]DashboardTarget{"tailnet": {URL: "http://host"}}}
	return r, work, calls
}

// Step 7 grants each worker bench's own seat the keys of the providers
// step 6 saved, as the policy declares them, beside the sprint credential,
// and installs the workers for exactly those providers; no other seat gets
// a provider key. With none saved, no key is granted and the workers get no
// route.
func TestWorkersActGrantsServiceKeysToBenchSeats(t *testing.T) {
	t.Parallel()
	r, work, calls := workersRuntime(t, "zeta", true)
	if err := r.workersAct(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{sprintPasswordName + "->swarm-studio", sprintPasswordName + "->amd-1", "ZETA_TOKEN->amd-1"}
	if !slices.Equal(*calls, want) || len(work.installed) != 1 || work.installed["zeta"].Key != "ZETA_TOKEN" {
		t.Fatalf("grants %q, workers for %v; want %q for zeta", *calls, work.installed, want)
	}
	r, work, calls = workersRuntime(t, "none", true)
	if err := r.workersAct(context.Background()); err != nil || !slices.Equal(*calls, want[:2]) || len(work.installed) != 0 {
		t.Fatal(err, *calls, work.installed)
	}
}

type runtimeRun struct{ ready bool }

func (r *runtimeRun) Ready(context.Context, string, string) (bool, error) { return r.ready, nil }
func TestRuntimeSetupWalkTwiceAndRegression(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := store.NewFake()
	st := &runtimeStore{}
	worker := &runtimeWork{}
	run := &runtimeRun{}
	exists := false
	fleetUp := true
	plays := 0
	service := FleetService{Name: "sprint-run", Machine: "bench-a", Seat: "swarm-bench-a", Keys: []string{sprintPasswordName}, Argv: []string{"/bin/nova-sprint", "run"}}
	cmd := func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch name {
		case "nova-bus":
			return []byte("WHO OK present=1\nalice\t1m\n"), nil
		case "nova-fleet":
			if !fleetUp {
				return nil, errors.New("fleet down")
			}
			if args[0] == "services" {
				plays++
				run.ready = true
			}
			return nil, nil
		case "nova-secrets":
			return nil, nil
		case "nova-config":
			if args[0] == "machine" {
				return []byte(`{"result":{"verb":"machine list","status":"ok","exit":0},"facts":{"kind":"machine","rows":1},"items":[{"kind":"machine","fields":{"name":"bench-a","roles":"bench,services","cores":8}}]}`), nil
			}
			if args[1] == "add" {
				exists = true
				return nil, nil
			}
			items := []any{}
			if exists {
				items = append(items, map[string]any{"kind": "service", "fields": map[string]any{"name": service.Name, "machine": service.Machine, "argv": service.Argv, "seat": "swarm-bench-a", "keys": sprintPasswordName, "every": 0, "keepalive": true, "enabled": true, "env": map[string]string{}, "log": ""}})
			}
			b, _ := json.Marshal(map[string]any{"result": map[string]any{"verb": "service list", "status": "ok", "exit": 0}, "facts": map[string]any{"kind": "service", "rows": len(items)}, "items": items})
			return b, nil
		}
		t.Fatalf("unexpected command %s", name)
		return nil, nil
	}
	c := Choices{Store: db, Actor: "alice", Now: func() time.Time { return time.Unix(1000, 0) }}
	transport := dashboardRoundTrip(func(req *http.Request) (*http.Response, error) {
		body := "ok"
		if req.URL.Path == "/api/sprint" {
			body = `{"ok":true,"data":{"epoch":1}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	r := RuntimeSetup{Prerequisites: SetupPrerequisites{Env: ExecEnvironment{Choices: c, Command: cmd, Getenv: func(string) string { return "" }}, Pin: "v1.6.20", Installed: func(context.Context) (string, error) { return "v1.6.29", nil }}, Store: st, Run: run, Workload: worker, Services: FleetServices{Command: cmd}, Role: SprintRoleBootstrap{Secrets: NovaSecrets{Seat: "studio", Store: "/store", KeyFile: "/key"}, Grants: FleetGrants{Store: "/store", KeyFile: "/key", Pins: "/pins", ControlSeat: "studio", Command: cmd}}, Snapshot: db, RunService: service, DefaultRunHost: "bench-a", Dashboards: map[string]DashboardTarget{"tailnet": {URL: "http://host"}}, Dashboard: DashboardProbe{Transport: transport}}
	in := SetupInput{Actor: "alice", Dash: "tailnet", Services: "none"}
	for round := 0; round < 2; round++ {
		for step := 1; step <= 8; step++ {
			if step == 7 && round == 0 { // no commit identity yet: up asks the human, and no act makes it true
				check, err := r.Verify(ctx, step, in)
				if err != nil || check.Verified || check.Action || !strings.Contains(check.Line, "ASK the human which forge account") {
					t.Fatal(step, check, err)
				}
				setIdentity(t, db)
			}
			check, err := r.Verify(ctx, step, in)
			if err != nil {
				t.Fatal(step, err)
			}
			if !check.Verified && check.Action {
				if err = r.Act(ctx, step, in); err != nil {
					t.Fatal(step, err)
				}
				check, err = r.Verify(ctx, step, in)
			}
			if err != nil || !check.Verified {
				t.Fatal(step, check, err)
			}
		}
	}
	if st.actions != 1 || worker.actions != 3 || plays != 2 {
		t.Fatal(st.actions, worker.actions, plays)
	}
	fleetUp = false
	check, err := r.Verify(ctx, 1, in)
	if err != nil || check.Verified || check.Next != "nova-fleet check" {
		t.Fatal(check, err)
	}
}
func TestRuntimeSetupAsksBeforeChoosingDashboard(t *testing.T) {
	t.Parallel()
	db := store.NewFake()
	choices := Choices{Store: db, Actor: "alice", Now: func() time.Time { return time.Unix(1000, 0) }}
	r := RuntimeSetup{Prerequisites: SetupPrerequisites{Env: ExecEnvironment{Choices: choices}}}
	check, err := r.Verify(context.Background(), 5, SetupInput{})
	if err != nil || check.Verified || check.Action || !strings.Contains(check.Line, "ASK the human") || !strings.Contains(check.Next, "--dash tailnet") {
		t.Fatal(check, err)
	}
	_, saved, err := choices.Get(context.Background(), "dashboard")
	if err != nil || saved {
		t.Fatal("choice saved before asking", err)
	}
}
func TestRuntimeSetupFirstTimeRefusesUnhookedCoordinator(t *testing.T) {
	t.Parallel()
	db := store.NewFake()
	choices := Choices{Store: db, Actor: "rowan", Now: func() time.Time { return time.Unix(1000, 0) }}
	command := func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch name {
		case "nova-bus":
			return []byte("WHO OK present=0\n"), nil
		default:
			return nil, nil
		}
	}
	runtime := RuntimeSetup{Prerequisites: SetupPrerequisites{Env: ExecEnvironment{Choices: choices, Command: command, Getenv: func(string) string { return "" }}, Pin: "v1.6.20", Installed: func(context.Context) (string, error) { return "v1.6.29", nil }}, Store: &runtimeStore{ready: true}}
	result := (&Engine{Actor: "rowan", Store: upStore(true), Bus: &fakeBus{}, Now: upNow, Setup: runtime, Lock: setupLeaseFake{}}).Execute(context.Background(), []string{"up"})
	want := "SPRINT REFUSED up: rowan is not on the bus: " + busRecipe("rowan") + "\n"
	if result.Exit != 1 || result.Output != want {
		t.Fatal(result)
	}
}

// The bench role is the only word: the run machine is a worker bench when
// it has the role.
func TestWorkerBenchesIncludeTheRunMachineWhenItIsABench(t *testing.T) {
	t.Parallel()
	got := WorkerBenches([]FleetMachine{{Name: "studio", Roles: []string{"services", "bench"}}, {Name: "edge", Roles: []string{"services"}}, {Name: "amd-1", Roles: []string{"bench"}}})
	if strings.Join(got, ",") != "studio,amd-1" {
		t.Fatal(got)
	}
}
