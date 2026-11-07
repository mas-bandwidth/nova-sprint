package verbs

import (
	"cmp"
	"fmt"
	"regexp"
	"strings"
)

// UpNeed is one thing nova-sprint up needs on this machine before its
// first step: its name (a program or an environment variable), what it is
// and who gives it, and the line that sets it.
type UpNeed struct{ Name, What, Fix string }

// UpNeeds is everything up needs before step 1, in the order it checks
// them: help up prints it, docs/SETUP.md section 0 is it rendered
// (TestSetupDocMatchesUpNeeds), and up refuses the first one missing with
// its line (UpNeedRefusal). pin is the nova-tools release this build needs.
func UpNeeds(pin string) []UpNeed {
	pin = cmp.Or(pin, "<the release nova-sprint check names>")
	fleet := "nova-fleet check (it prints the line that makes it), then nova-sprint up again"
	return []UpNeed{
		{"nova-tools", "nova-tools " + pin + " or later, the fleet nova-sprint runs on: " + strings.Join(toolPrograms, ", ") + " on PATH", "git clone -b " + pin + " https://" + toolsRepo + " && for t in fleet secrets config bus; do go build -C nova-tools -o ~/.local/bin/nova-$t ./$t; done; then nova-fleet check"},
		{"NOVA_SECRETS_STORE", "this machine's nova-secrets store (unset: ~/.config/nova-secrets/store, as nova-secrets)", fleet},
		{"NOVA_SECRETS_KEY", "this machine's seat key (unset: ~/.config/nova-secrets/<seat>.key, as nova-secrets)", fleet},
		{"NOVA_SECRETS_PINS", "the pinned seat keys (unset: ~/.config/nova-secrets/pins, as nova-secrets)", fleet},
		{"NOVA_SECRETS_SEAT", "this machine's seat, the control seat: its name in nova-config (unset: the short hostname)", "unset NOVA_SECRETS_SEAT, or export NOVA_SECRETS_SEAT=<this machine's name in nova-config machine list>"},
		{"NOVA_PG_DSN", "the fleet store's PostgreSQL (16 or later) URL, with no password: nova-fleet up prints it", "export NOVA_PG_DSN=postgres://nova_config@<the store machine>:5432/nova"},
		{"NOVA_BUS_REDIS", "the fleet bus's Redis host:port, on the store machine: nova-fleet up prints it (UP BUS OK)", "export NOVA_BUS_REDIS=<the store machine>:6379 NOVA_BUS_USER=bus"},
	}
}

// toolsRepo is where nova-tools is built from (toolsModule's repository),
// and toolPrograms the nova-tools programs up runs.
const toolsRepo = "github.com/mas-bandwidth/nova-tools"

var toolPrograms = []string{"nova-fleet", "nova-secrets", "nova-config", "nova-bus"}

// UpEnv is what UpNeedRefusal observes: env reads a variable as up will
// use it (a path variable's default filled in), onPath a program, exists a
// path, and host this machine's short name.
type UpEnv struct {
	Env    func(string) string
	OnPath func(string) bool
	Exists func(string) bool
	Host   string
}

// UpNeedRefusal is the first of UpNeeds that is missing, as a refusal
// naming it, what it is and its line; nil when all are there.
func UpNeedRefusal(pin string, e UpEnv) error {
	for _, n := range UpNeeds(pin) {
		var problem string
		switch {
		case n.Name == "nova-tools":
			for _, program := range toolPrograms {
				if problem == "" && !e.OnPath(program) {
					problem = program + " is not on PATH"
				}
			}
		case n.Name == "NOVA_SECRETS_SEAT":
			if seat := e.Env(n.Name); seat != e.Host {
				problem = fmt.Sprintf("NOVA_SECRETS_SEAT is %q, not this machine's name %q", Redact(seat), e.Host)
			}
		case strings.HasPrefix(n.Name, "NOVA_SECRETS_"):
			if v := e.Env(n.Name); v == "" || !e.Exists(v) {
				problem = n.Name + " names " + Redact(v) + ", which is not there"
			}
		case e.Env(n.Name) == "":
			problem = n.Name + " is not set"
		}
		if problem != "" {
			return &OperationError{Problem: problem + " (" + n.What + ")", Fix: n.Fix, Exit: 1}
		}
	}
	return nil
}

// UpNeedsHelp is UpNeeds as help up prints them, one line each.
func UpNeedsHelp(pin string) []string {
	lines := []string{"needs before step 1 (up refuses the first missing, with its line):"}
	for _, n := range UpNeeds(pin) {
		lines = append(lines, "  "+n.Name+": "+n.What+"; "+n.Fix)
	}
	return lines
}

// fleetMissing is the names nova-fleet check's CHECK MISSING lines give,
// in order: a name is not a value, so it is printed as it is.
var fleetMissing = regexp.MustCompile(`(?m)^CHECK MISSING ([A-Za-z0-9_.-]+):`)

// FleetMissing is "missing: a, b" for nova-fleet check's output, or "".
func FleetMissing(out []byte) string {
	var names []string
	for _, m := range fleetMissing.FindAllSubmatch(out, -1) {
		names = append(names, string(m[1]))
	}
	if len(names) == 0 {
		return ""
	}
	return "missing: " + strings.Join(names, ", ")
}
