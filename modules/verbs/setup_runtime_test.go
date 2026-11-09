package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

type runtimeStore struct {
	ready   bool
	actions int
}

func (s *runtimeStore) Ready(context.Context) (bool, error) { return s.ready, nil }
func (s *runtimeStore) Install(context.Context) error       { s.ready = true; s.actions++; return nil }

type runtimeWork struct {
	workers, smoke bool
	actions        int
}

func (w *runtimeWork) WorkersReady(context.Context) (bool, error) { return w.workers, nil }
func (w *runtimeWork) InstallWorkers(context.Context) error {
	w.workers = true
	w.actions++
	return nil
}
func (w *runtimeWork) SmokeReady(context.Context) (bool, error) { return w.smoke, nil }
func TestRuntimeSetupOwnerChecksAreReverified(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := &runtimeStore{}
	r := RuntimeSetup{Store: st}
	for _, step := range []int{2} {
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
	if st.actions != 1 {
		t.Fatal(st.actions)
	}
	st.ready = false
	check, err := r.Verify(ctx, 2, SetupInput{})
	if err != nil || check.Verified || !check.Action {
		t.Fatal(check, err)
	}
}

type runtimeRun struct{ ready bool }

func (r *runtimeRun) Ready(context.Context, string, string) (bool, error) { return r.ready, nil }
func TestRuntimeSetupWalkTwiceAndRegression(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := store.NewFake()
	st := &runtimeStore{}
	worker := &runtimeWork{smoke: true}
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
	transport := dashboardRoundTrip(func(req *http.Request) (*http.Response, error) {
		body := "ok"
		if req.URL.Path == "/api/sprint" {
			body = `{"ok":true,"data":{"epoch":1}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	r := RuntimeSetup{Prerequisites: SetupPrerequisites{Env: ExecEnvironment{Command: cmd, Getenv: func(string) string { return "" }}, Pin: "v1.6.20", Installed: func(context.Context) (string, error) { return "v1.6.29", nil }}, Store: st, Run: run, Workload: worker, Services: FleetServices{Command: cmd}, Role: SprintRoleBootstrap{Secrets: NovaSecrets{Seat: "studio", Store: "/store", KeyFile: "/key"}, Grants: FleetGrants{Store: "/store", KeyFile: "/key", Pins: "/pins", ControlSeat: "studio", Command: cmd}}, Snapshot: db, RunService: service, Board: DashboardTarget{URL: "http://host"}, Dashboard: DashboardProbe{Transport: transport}}
	in := SetupInput{Actor: "alice", Dash: "tailnet"}
	for round := 0; round < 2; round++ {
		for _, step := range []int{1, 2, 4, 5, 6, 7} {
			if step == 6 && round == 0 { // no commit identity yet: up asks the human, and no act makes it true
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
	if st.actions != 1 || worker.actions != 1 || plays != 1 {
		t.Fatal(st.actions, worker.actions, plays)
	}
	fleetUp = false
	check, err := r.Verify(ctx, 1, in)
	if err != nil || check.Verified || check.Next != "nova-fleet check" {
		t.Fatal(check, err)
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
