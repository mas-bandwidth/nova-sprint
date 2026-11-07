package verbs

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestExecEnvironmentUsesToolsAndExactNames(t *testing.T) {
	ctx := context.Background()
	var calls [][]string
	env := ExecEnvironment{Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if name == "nova-fleet" {
			return []byte("CHECK OK\n"), nil
		}
		return []byte("WHO OK present=2\nalice\t1m\nalice2\t2m\n"), nil
	}}
	ok, _, err := env.Fleet(ctx)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	ok, _, err = env.Hooked(ctx, "alice")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	ok, _, err = env.Hooked(ctx, "ali")
	if err != nil || ok {
		t.Fatal(ok, err)
	}
	if !reflect.DeepEqual(calls, [][]string{{"nova-fleet", "check"}, {"nova-bus", "who"}, {"nova-bus", "who"}}) {
		t.Fatal(calls)
	}
}
func TestExecEnvironmentFailsClosed(t *testing.T) {
	ctx := context.Background()
	env := ExecEnvironment{Command: func(context.Context, string, ...string) ([]byte, error) { return []byte("nonsense"), nil }}
	if ok, _, err := env.Hooked(ctx, "alice"); ok || err == nil {
		t.Fatal(ok, err)
	}
	env.Command = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("failed") }
	if ok, _, err := env.Fleet(ctx); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestExecEnvironmentMissingDSNIsUnconfigured(t *testing.T) {
	ok, err := (ExecEnvironment{}).Schema(context.Background())
	if err != nil || ok {
		t.Fatal(ok, err)
	}
}
func TestColdNextWithMissingDSNGoesToSetup(t *testing.T) {
	env := ExecEnvironment{Command: func(context.Context, string, ...string) ([]byte, error) { return nil, nil }}
	result := (&Engine{Env: env, Actor: "alice"}).Execute(context.Background(), nil)
	if result.Exit != 1 || !strings.Contains(result.Output, "nova-sprint up --as alice") {
		t.Fatal(result)
	}
}
func TestExecEnvironmentMalformedDSNHasRemedy(t *testing.T) {
	ok, err := (ExecEnvironment{DSN: "postgres://%zz"}).Schema(context.Background())
	var op *OperationError
	if ok || !errors.As(err, &op) || op.Exit != 2 || op.Fix != "check NOVA_PG_DSN and run nova-fleet check" {
		t.Fatal(ok, err)
	}
}
func TestExecEnvironmentOffersCodexHookRecipe(t *testing.T) {
	env := ExecEnvironment{Getenv: func(name string) string {
		if name == "CODEX_THREAD_ID" {
			return "fixture"
		}
		return ""
	}, Command: func(context.Context, string, ...string) ([]byte, error) { return []byte("WHO OK present=0\n"), nil }}
	hooked, recipe, err := env.Hooked(context.Background(), "alice")
	if err != nil || hooked || recipe != "nova-bus show codex-agents" {
		t.Fatal(hooked, recipe, err)
	}
}
