package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestFleetServiceBareProgramRequiresSecretsWrapper(t *testing.T) {
	t.Parallel()
	s := FleetService{Name: "sprint-run", Machine: "studio", Seat: "studio", Keys: []string{"NOVA_SPRINT_PG_PASSWORD"}, Argv: []string{"nova-sprint", "run"}}
	if !s.valid() {
		t.Fatal("secret-backed nova-sprint was rejected")
	}
	s.Argv[0] = "other-program"
	if s.valid() {
		t.Fatal("unapproved relative executable accepted")
	}
	s.Argv[0], s.Seat, s.Keys = "nova-sprint", "", nil
	if s.valid() {
		t.Fatal("bare secret-free service accepted")
	}
}

func TestFleetServicesCreateOnceAndTargetPlay(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := FleetService{Name: "sprint-run", Machine: "bench-a", Seat: "bench-a", Argv: []string{"/opt/nova/bin/nova-sprint", "run"}, Keys: []string{"NOVA_PG_PASSWORD"}}
	exists := false
	var calls [][]string
	f := FleetServices{Actor: "coord-1", Reason: "nova-sprint up 4/8 run", Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if name == "nova-fleet" {
			return nil, nil
		}
		if args[1] == "add" {
			exists = true
			return nil, nil
		}
		items := []any{}
		if exists {
			items = append(items, map[string]any{"kind": "service", "fields": map[string]any{"name": s.Name, "machine": s.Machine, "seat": s.Seat, "argv": s.Argv, "keys": "NOVA_PG_PASSWORD", "every": 0, "keepalive": true, "enabled": true, "env": map[string]string{}, "log": ""}})
		}
		b, _ := json.Marshal(map[string]any{"result": map[string]any{"verb": "service list", "status": "ok", "exit": 0}, "facts": map[string]any{"kind": "service", "rows": len(items)}, "items": items})
		return b, nil
	}}
	changed, err := f.Ensure(ctx, s)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	changed, err = f.Ensure(ctx, s)
	if err != nil || changed {
		t.Fatal(changed, err)
	}
	if err = f.Apply(ctx, s); err != nil {
		t.Fatal(err)
	}
	want := []string{"nova-fleet", "services", "--", "-l", "bench-a", "-e", "nova_service_only=sprint-run"}
	if !reflect.DeepEqual(calls[len(calls)-1], want) {
		t.Fatal(calls)
	}
	add := []string{"nova-config", "service", "add", "sprint-run", "--machine", "bench-a", "--argv", `["/opt/nova/bin/nova-sprint","run"]`, "--keepalive", "true", "--enabled", "true", "--env", "{}", "--seat", "bench-a", "--keys", "NOVA_PG_PASSWORD", "--actor", "coord-1", "--reason", "nova-sprint up 4/8 run"}
	if !reflect.DeepEqual(calls[1], add) {
		t.Fatalf("service add argv:\n got %q\nwant %q", calls[1], add)
	}
}

// A nova-config write without an actor is refused before anything runs; a
// tool failure stays reachable under the setup's own words.
func TestFleetServicesWriteNeedsActorAndWrapsToolFailure(t *testing.T) {
	ctx := context.Background()
	s := FleetService{Name: "sprint-run", Machine: "bench-a", Argv: []string{"/bin/nova-sprint", "run"}}
	empty := []byte(`{"result":{"verb":"service list","status":"ok","exit":0},"facts":{"kind":"service","rows":0},"items":[]}`)
	writes := 0
	f := FleetServices{Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if args[1] == "add" {
			writes++
		}
		return empty, nil
	}}
	var op *OperationError
	if _, err := f.Ensure(ctx, s); !errors.As(err, &op) || writes != 0 || !strings.Contains(op.Problem, "needs an actor") {
		t.Fatal(err, writes)
	}
	tool := &ToolError{Command: "nova-config service add", Exit: 2, Stderr: "error: --actor is required"}
	f = FleetServices{Actor: "coord-1", Reason: "nova-sprint up 4/8 run", Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if args[1] == "add" {
			return nil, tool
		}
		if name == "nova-fleet" {
			return nil, tool
		}
		return empty, nil
	}}
	_, err := f.Ensure(ctx, s)
	var got *ToolError
	if !errors.As(err, &got) || got != tool || !strings.Contains(err.Error(), "nova-config service add sprint-run failed") || !strings.Contains(err.Error(), "--actor is required") {
		t.Fatal(err)
	}
	f.Command = func(context.Context, string, ...string) ([]byte, error) { return nil, tool }
	if _, err = f.Ensure(ctx, s); !errors.As(err, &got) || !strings.Contains(err.Error(), "nova-config service list failed") {
		t.Fatal(err)
	}
}
func TestFleetServicesRefusesConflictsAndMalformedOutput(t *testing.T) {
	t.Parallel()
	s := FleetService{Name: "sprint-run", Machine: "bench-a", Argv: []string{"/bin/nova-sprint", "run"}}
	for _, out := range []string{`{}`, `{"result":{"verb":"service list","status":"ok","exit":0},"facts":{"kind":"service","rows":1},"items":[{"kind":"service","fields":{"name":"sprint-run","machine":"other"}}]}`} {
		calls := 0
		f := FleetServices{Command: func(context.Context, string, ...string) ([]byte, error) { calls++; return []byte(out), nil }}
		if _, err := f.Ensure(context.Background(), s); err == nil || calls != 1 {
			t.Fatal(err, calls)
		}
		if err := f.Apply(context.Background(), s); err == nil || calls != 2 {
			t.Fatal(err, calls)
		}
	}
}
