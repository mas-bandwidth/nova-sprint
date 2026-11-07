package run

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// MachineEnv names the machine a run service runs on, as nova-config names
// it; up hands it to the service in the service record's env.
const MachineEnv = "NOVA_SPRINT_MACHINE"

// MachineName is a host name as the sprint names machines: its first label,
// lower case. A machine has one name in the sprint, nova-config's; a .local
// or MagicDNS suffix is display, never identity ("x.local" and
// "x.tailnet.ts.net" are both machine "x").
func MachineName(host string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(host), ".")
	return strings.ToLower(name)
}

// SameMachine is true when a and b name one machine.
func SameMachine(a, b string) bool {
	return a != "" && MachineName(a) == MachineName(b)
}

// ResolveMachine is the machine run records the chooser lock under:
// NOVA_SPRINT_MACHINE (env) when up handed it, else the machine row whose
// name is the hostname's first label. machines is nova-config's machine
// names; a name it does not hold is refused, naming both.
func ResolveMachine(env, hostname string, machines []string) (string, error) {
	if env = strings.TrimSpace(env); env != "" {
		if !slices.Contains(machines, env) {
			return "", fmt.Errorf("run: %s=%s, but nova-config has no machine named %s; fix the service record's env (nova-sprint up writes it)", MachineEnv, env, env)
		}
		return env, nil
	}
	name := MachineName(hostname)
	if name == "" || !slices.Contains(machines, name) {
		return "", fmt.Errorf("run: hostname %q is machine %q, and nova-config has no machine named %q; set %s to this machine's nova-config name", hostname, name, name, MachineEnv)
	}
	return name, nil
}

// MachineNames is every machine name nova-config holds, from one snapshot.
func (r *ConfigRoster) MachineNames(ctx context.Context) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot, err := r.reader.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(snapshot.Machines))
	for _, m := range snapshot.Machines {
		names = append(names, m.Name)
	}
	return names, nil
}
