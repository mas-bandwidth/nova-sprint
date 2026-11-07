package verbs

import (
	"context"
	"reflect"
	"testing"
)

func TestFleetGrantsUsesEncryptedSeatCommands(t *testing.T) {
	var calls [][]string
	g := FleetGrants{Store: "/store", KeyFile: "/keys/control.key", Pins: "/pins", ControlSeat: "control", Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return nil, nil
	}}
	if err := g.Grant(context.Background(), "swarm-bench-a", "bench-a", "DEEPSEEK_API_KEY"); err != nil {
		t.Fatal(err)
	}
	if err := g.Refresh(context.Background(), "swarm-bench-a", "bench-a", "DEEPSEEK_API_KEY"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"nova-secrets", "grant", "DEEPSEEK_API_KEY", "--to", "swarm-bench-a", "--pins", "/pins", "--store", "/store", "--key", "/keys/control.key", "--as", "control"}, {"nova-fleet", "secrets-refresh", "--", "-l", "bench-a", "-e", "nova_seal_store=/store"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatal(calls)
	}
	if err := g.Grant(context.Background(), "bad seat", "bench-a", "DEEPSEEK_API_KEY"); err == nil || len(calls) != 2 {
		t.Fatal(err, calls)
	}
}
