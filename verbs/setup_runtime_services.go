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
type FleetServices struct{ Command CommandOutput }

func (f FleetServices) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	if f.Command != nil {
		return f.Command(ctx, name, args...)
	}
	return toolOutput(ctx, name, args...)
}
func (s FleetService) valid() bool {
	if !fleetName.MatchString(s.Name) || !strings.HasPrefix(s.Name, "sprint-") || !fleetName.MatchString(s.Machine) || len(s.Argv) == 0 || !filepath.IsAbs(s.Argv[0]) {
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
		return false, fmt.Errorf("nova-config service list failed")
	}
	var list configServices
	if json.Unmarshal(out, &list) != nil || list.Result.Verb != "service list" || list.Result.Status != "ok" || list.Result.Exit != 0 || list.Facts.Kind != "service" || list.Facts.Rows != len(list.Items) {
		return false, fmt.Errorf("nova-config returned invalid service list")
	}
	found := false
	for _, item := range list.Items {
		if item.Kind != "service" || item.Fields.Name == "" {
			return false, fmt.Errorf("nova-config returned invalid service list")
		}
		if item.Fields.Name != s.Name {
			continue
		}
		if found || !sameFleetService(item.Fields, s) {
			return false, &OperationError{Problem: "service " + s.Name + " conflicts with setup", Fix: "inspect nova-config service show " + s.Name, Exit: 1}
		}
		found = true
	}
	return found, nil
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
	argv, _ := json.Marshal(s.Argv)
	env := s.Env
	if env == nil {
		env = map[string]string{}
	}
	envJSON, _ := json.Marshal(env)
	keys := append([]string(nil), s.Keys...)
	sort.Strings(keys)
	args := []string{"service", "add", s.Name, "--machine", s.Machine, "--argv", string(argv), "--keepalive", "true", "--enabled", "true", "--env", string(envJSON)}
	if s.Seat != "" {
		args = append(args, "--seat", s.Seat)
	}
	if len(keys) > 0 {
		args = append(args, "--keys", strings.Join(keys, ","))
	}
	if _, err = f.command(ctx, "nova-config", args...); err != nil {
		return false, fmt.Errorf("nova-config service add failed")
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
		return fmt.Errorf("fleet service is absent")
	}
	_, err = f.command(ctx, "nova-fleet", "services", "--", "-l", s.Machine, "-e", "nova_service_only="+s.Name)
	if err != nil {
		return fmt.Errorf("nova-fleet services failed")
	}
	return nil
}
