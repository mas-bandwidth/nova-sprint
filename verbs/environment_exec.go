package verbs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// CommandOutput executes nova-tools commands. Arguments never contain secret values.
type CommandOutput func(context.Context, string, ...string) ([]byte, error)

func toolOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	var x *exec.ExitError
	if errors.As(err, &x) {
		return out, Failure(name, err, x.Stderr)
	}
	return out, err
}

// ExecEnvironment observes fleet, bus and schema state through their owners.
type ExecEnvironment struct {
	DSN, Password string
	Choices       Choices
	Command       CommandOutput
	Getenv        func(string) string
}

func (e ExecEnvironment) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	if e.Command != nil {
		return e.Command(ctx, name, args...)
	}
	return toolOutput(ctx, name, args...)
}
func (e ExecEnvironment) Fleet(ctx context.Context) (bool, string, error) {
	_, err := e.command(ctx, "nova-fleet", "check")
	if err != nil {
		if ctx.Err() != nil {
			return false, "", ctx.Err()
		}
		return false, "run nova-fleet check", nil
	}
	return true, "", nil
}
func (e ExecEnvironment) Schema(ctx context.Context) (bool, error) {
	if strings.TrimSpace(e.DSN) == "" {
		return false, nil
	}
	pg, err := store.Open(ctx, e.DSN, e.Password)
	if err != nil {
		return false, &OperationError{Problem: "PostgreSQL cannot be reached or NOVA_PG_DSN is invalid", Fix: "check NOVA_PG_DSN and run nova-fleet check", Exit: 2}
	}
	defer pg.Close()
	identity, err := pg.Schema(ctx)
	if err != nil {
		if (!identity.Present && strings.Contains(err.Error(), "there is no schema sprint in this database")) || (identity.Present && strings.Contains(err.Error(), "schema sprint is empty")) {
			return false, nil
		}
		return false, fmt.Errorf("schema identity unavailable")
	}
	return identity.Current, nil
}
func (e ExecEnvironment) Hooked(ctx context.Context, actor string) (bool, string, error) {
	recipe := "nova-bus hook --as " + actor
	getenv := e.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if getenv("CODEX_THREAD_ID") != "" {
		recipe = "nova-bus show codex-agents"
	}
	out, err := e.command(ctx, "nova-bus", "who")
	if err != nil {
		return false, recipe, fmt.Errorf("nova-bus who failed")
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "WHO OK present=") {
		return false, recipe, fmt.Errorf("nova-bus who returned invalid output")
	}
	count, err := strconv.Atoi(strings.TrimPrefix(lines[0], "WHO OK present="))
	if err != nil || count < 0 || count != len(lines)-1 {
		return false, recipe, fmt.Errorf("nova-bus who returned invalid output")
	}
	found := false
	seen := make(map[string]bool, count)
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 || fields[0] == "" || seen[fields[0]] {
			return false, recipe, fmt.Errorf("nova-bus who returned invalid output")
		}
		seen[fields[0]] = true
		if fields[0] == actor && actor != "" {
			found = true
		}
	}
	if found {
		return true, recipe, nil
	}
	return false, recipe, nil
}
