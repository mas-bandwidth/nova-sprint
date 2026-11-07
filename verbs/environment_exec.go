package verbs

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// CommandOutput executes nova-tools commands. Arguments never contain secret values.
type CommandOutput func(context.Context, string, ...string) ([]byte, error)

// toolOutput runs a tool for its stdout; a failure carries the command, its
// exit code and its output's first lines (edge.CommandError).
func toolOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return Output(ctx, name, args...)
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
		// Not ready, and why: nova-fleet check's own words.
		return false, errText(err), nil
	}
	return true, "", nil
}
func (e ExecEnvironment) Schema(ctx context.Context) (bool, error) {
	if strings.TrimSpace(e.DSN) == "" {
		return false, nil
	}
	pg, err := store.Open(ctx, e.DSN, e.Password)
	if err != nil {
		return false, RefusalOr(err, &OperationError{Problem: "PostgreSQL cannot be reached or NOVA_PG_DSN is invalid (" + errText(err) + ")", Fix: "check NOVA_PG_DSN and run nova-fleet check", Exit: 2})
	}
	defer pg.Close()
	identity, err := pg.Schema(ctx)
	if err != nil {
		if (!identity.Present && strings.Contains(err.Error(), "there is no schema sprint in this database")) || (identity.Present && strings.Contains(err.Error(), "schema sprint is empty")) {
			return false, nil
		}
		return false, cause("schema identity unavailable", err)
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
	present, err := e.Present(ctx)
	if err != nil {
		return false, recipe, err
	}
	return actor != "" && present[actor], recipe, nil
}

// Present is every name on the bus, from one nova-bus who.
func (e ExecEnvironment) Present(ctx context.Context) (map[string]bool, error) {
	out, err := e.command(ctx, "nova-bus", "who")
	if err != nil {
		return nil, cause("nova-bus who failed", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "WHO OK present=") {
		return nil, fmt.Errorf("nova-bus who returned invalid output: %s", edge.ParseRefused("no `WHO OK present=<n>` header", 0, out))
	}
	count, err := strconv.Atoi(strings.TrimPrefix(lines[0], "WHO OK present="))
	if err != nil {
		return nil, fmt.Errorf("nova-bus who returned invalid output: %s", edge.ParseFailure(err, out))
	}
	if count < 0 || count != len(lines)-1 {
		return nil, fmt.Errorf("nova-bus who returned invalid output: present=%d but %d presence lines: %s", count, len(lines)-1, edge.ParseRefused("count mismatch", 0, out))
	}
	seen := make(map[string]bool, count)
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 || fields[0] == "" || seen[fields[0]] {
			return nil, fmt.Errorf("nova-bus who returned invalid output: %s", edge.ParseRefused("presence line is not a new name<TAB>uptime", strings.Index(string(out), line), out))
		}
		seen[fields[0]] = true
	}
	return seen, nil
}
