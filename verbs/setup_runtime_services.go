package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

// FleetService is a non-secret nova-config service definition. Credentials
// are named in Keys and opened by nova-fleet's unit through nova-secrets.
type FleetService struct {
	Name, Machine, Seat string
	Argv, Keys          []string
	Env                 map[string]string
}
type configService struct {
	Name      string            `json:"name"`
	Machine   string            `json:"machine"`
	Argv      []string          `json:"argv"`
	Seat      string            `json:"seat"`
	Keys      string            `json:"keys"`
	Every     int               `json:"every"`
	Keepalive bool              `json:"keepalive"`
	Enabled   bool              `json:"enabled"`
	Env       map[string]string `json:"env"`
	Log       string            `json:"log"`
}
type configServices struct {
	Result struct {
		Verb, Status string
		Exit         int
	} `json:"result"`
	Facts struct {
		Kind string
		Rows int
	} `json:"facts"`
	Items []struct {
		Kind   string        `json:"kind"`
		Fields configService `json:"fields"`
	} `json:"items"`
}

var fleetName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)
var fleetKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// FleetServices uses only the public nova-config and nova-fleet commands.
// Actor and Reason are the --actor and --reason every nova-config write
// carries (nova-config v2 refuses a write without them): the up actor's
// row and "nova-sprint up <step>".
type FleetServices struct {
	Command       CommandOutput
	Actor, Reason string
}

// For is f writing as actor for reason.
func (f FleetServices) For(actor, reason string) FleetServices {
	f.Actor, f.Reason = actor, reason
	return f
}

// writeArgs is args with the --actor and --reason a nova-config write
// needs; an empty actor or reason is refused before anything is written.
func (f FleetServices) writeArgs(args ...string) ([]string, error) {
	if !fleetActor.MatchString(f.Actor) || strings.TrimSpace(f.Reason) == "" {
		return nil, &OperationError{Problem: fmt.Sprintf("nova-config %s needs an actor and a reason (actor %q, reason %q)", strings.Join(args[:min(2, len(args))], " "), Redact(f.Actor), Redact(f.Reason)), Fix: "nova-sprint up --as <your row>", Exit: 1}
	}
	return append(args, "--actor", f.Actor, "--reason", f.Reason), nil
}

var fleetActor = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func (f FleetServices) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	if f.Command != nil {
		return f.Command(ctx, name, args...)
	}
	return toolOutput(ctx, name, args...)
}
func (s FleetService) valid() bool {
	if !fleetName.MatchString(s.Name) || !strings.HasPrefix(s.Name, "sprint-") || !fleetName.MatchString(s.Machine) || len(s.Argv) == 0 || (!filepath.IsAbs(s.Argv[0]) && !(s.Argv[0] == "nova-sprint" && s.Seat != "" && len(s.Keys) > 0)) {
		return false
	}
	if (s.Seat != "" && !fleetName.MatchString(s.Seat)) || (len(s.Keys) > 0 && s.Seat == "") {
		return false
	}
	seen := make(map[string]bool, len(s.Keys))
	for _, k := range s.Keys {
		if !fleetKey.MatchString(k) || seen[k] {
			return false
		}
		seen[k] = true
	}
	return true
}
func (f FleetServices) existing(ctx context.Context, s FleetService) (bool, error) {
	out, err := f.command(ctx, "nova-config", "service", "list", "--json")
	if err != nil {
		return false, cause("nova-config service list failed", err)
	}
	var list configServices
	if err := json.Unmarshal(out, &list); err != nil {
		return false, fmt.Errorf("nova-config service list --json: %s", ParseFailure(err, out))
	}
	if list.Result.Verb != "service list" || list.Result.Status != "ok" || list.Result.Exit != 0 || list.Facts.Kind != "service" || list.Facts.Rows != len(list.Items) {
		return false, fmt.Errorf("nova-config service list --json: result %q %q exit %d, facts %q rows %d for %d items; first bytes %q",
			list.Result.Verb, list.Result.Status, list.Result.Exit, list.Facts.Kind, list.Facts.Rows, len(list.Items), firstBytes(out))
	}
	found := false
	for _, item := range list.Items {
		if item.Kind != "service" || item.Fields.Name == "" {
			return false, fmt.Errorf("nova-config service list --json: item kind %q name %q is not a named service", item.Kind, item.Fields.Name)
		}
		if item.Fields.Name != s.Name {
			continue
		}
		if found {
			return false, &OperationError{Problem: "service " + s.Name + " conflicts with setup: nova-config lists it twice", Fix: "remove the duplicate " + s.Name + " service record", Exit: 1}
		}
		if !sameFleetService(item.Fields, s) {
			return false, &OperationError{Problem: edge.RedactN("service "+s.Name+" conflicts with setup: "+fleetServiceDiff(item.Fields, s), edge.CommandMax), Fix: "make the " + s.Name + " service record match setup, or remove it so setup creates it", Exit: 1}
		}
		found = true
	}
	return found, nil
}

// fleetServiceDiff names the fields where the record differs from what setup
// wants; environment differences name the variables, never their values.
func fleetServiceDiff(got configService, want FleetService) string {
	keys := append([]string(nil), want.Keys...)
	sort.Strings(keys)
	diff := []string{}
	if got.Machine != want.Machine {
		diff = append(diff, fmt.Sprintf("machine %q, want %q", got.Machine, want.Machine))
	}
	if got.Seat != want.Seat {
		diff = append(diff, fmt.Sprintf("seat %q, want %q", got.Seat, want.Seat))
	}
	if got.Keys != strings.Join(keys, ",") {
		diff = append(diff, fmt.Sprintf("keys %q, want %q", got.Keys, strings.Join(keys, ",")))
	}
	if got.Every != 0 || !got.Keepalive || !got.Enabled || got.Log != "" {
		diff = append(diff, fmt.Sprintf("every %d keepalive %t enabled %t log %q, want 0 true true \"\"", got.Every, got.Keepalive, got.Enabled, got.Log))
	}
	if !reflect.DeepEqual(got.Argv, want.Argv) {
		diff = append(diff, fmt.Sprintf("argv %q, want %q", strings.Join(got.Argv, " "), strings.Join(want.Argv, " ")))
	}
	env := want.Env
	if env == nil {
		env = map[string]string{}
	}
	if !reflect.DeepEqual(got.Env, env) {
		names := []string{}
		for k, v := range got.Env {
			if w, ok := env[k]; !ok || w != v {
				names = append(names, k)
			}
		}
		for k := range env {
			if _, ok := got.Env[k]; !ok {
				names = append(names, k)
			}
		}
		sort.Strings(names)
		diff = append(diff, "env differs in "+strings.Join(names, ","))
	}
	return strings.Join(diff, "; ")
}
func sameFleetService(got configService, want FleetService) bool {
	keys := append([]string(nil), want.Keys...)
	sort.Strings(keys)
	env := want.Env
	if env == nil {
		env = map[string]string{}
	}
	return got.Machine == want.Machine && got.Seat == want.Seat && got.Keys == strings.Join(keys, ",") && got.Every == 0 && got.Keepalive && got.Enabled && got.Log == "" && reflect.DeepEqual(got.Argv, want.Argv) && reflect.DeepEqual(got.Env, env)
}

// Ensure creates a missing service and refuses a collision; it never overwrites.
func (f FleetServices) Ensure(ctx context.Context, s FleetService) (bool, error) {
	if !s.valid() {
		return false, fmt.Errorf("invalid fleet service")
	}
	found, err := f.existing(ctx, s)
	if err != nil || found {
		return false, err
	}
	argv, err := json.Marshal(s.Argv)
	if err != nil {
		return false, cause("fleet service argv", err)
	}
	env := s.Env
	if env == nil {
		env = map[string]string{}
	}
	envJSON, err := json.Marshal(env)
	if err != nil {
		return false, cause("fleet service env", err)
	}
	keys := append([]string(nil), s.Keys...)
	sort.Strings(keys)
	args := []string{"service", "add", s.Name, "--machine", s.Machine, "--argv", string(argv), "--keepalive", "true", "--enabled", "true", "--env", string(envJSON)}
	if s.Seat != "" {
		args = append(args, "--seat", s.Seat)
	}
	if len(keys) > 0 {
		args = append(args, "--keys", strings.Join(keys, ","))
	}
	if args, err = f.writeArgs(args...); err != nil {
		return false, err
	}
	if _, err = f.command(ctx, "nova-config", args...); err != nil {
		return false, cause("nova-config service add "+s.Name+" failed", err)
	}
	return true, nil
}

// Apply limits the play to this machine and service, avoiding unrelated units.
func (f FleetServices) Apply(ctx context.Context, s FleetService) error {
	if !s.valid() {
		return fmt.Errorf("invalid fleet service")
	}
	found, err := f.existing(ctx, s)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("fleet service %s is absent from nova-config service list", s.Name)
	}
	_, err = f.command(ctx, "nova-fleet", "services", "--", "-l", s.Machine, "-e", "nova_service_only="+s.Name)
	if err != nil {
		return cause("nova-fleet services "+s.Name+" on "+s.Machine+" failed", err)
	}
	return nil
}
