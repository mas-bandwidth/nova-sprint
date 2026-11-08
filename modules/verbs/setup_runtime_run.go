package verbs

import (
	"context"
	"fmt"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/run"
)

// PGSetupRun reads the chooser's owner API and nova-bus; service records alone
// never establish that run is alive or that the requested host holds the lock.
// When run holds no lock, the service's unit is read (its state and the last
// lines of its log, through Env's command) so the refusal says why.
type PGSetupRun struct {
	DSN, Password string
	Env           ExecEnvironment
	Inspect       func(context.Context, string, string) (run.ChooserStatus, error)
	GOOS, Home    string     // "": this machine's (runtime.GOOS, the home directory)
	UID           func() int // nil: os.Getuid
}

// RunNotReady is run not (yet) serving this sprint on the chosen machine,
// and why: each false condition by name.
type RunNotReady struct{ Why, Fix string }

func (r *RunNotReady) Error() string { return "run not ready: " + r.Why }

// Ready is true when run holds the chooser lock on machine host and the
// bus name sprint is hooked; false with a *RunNotReady naming what is not.
// Machines compare by name only (run.SameMachine): a .local or MagicDNS
// suffix is display, not identity.
func (p PGSetupRun) Ready(ctx context.Context, host, service string) (bool, error) {
	inspect := p.Inspect
	if inspect == nil {
		inspect = run.InspectChooser
	}
	status, err := inspect(ctx, p.DSN, p.Password)
	if err != nil {
		return false, err
	}
	switch {
	case !status.Held:
		why := "run service holds no chooser lock"
		if service != "" {
			why += "; " + p.unitState(ctx, service)
		}
		return false, &RunNotReady{Why: why, Fix: runLogFix(service) + ", fix what it says, then nova-sprint up again"}
	case status.Operation != "run" || !run.SameMachine(status.Host, host):
		return false, &RunNotReady{Why: fmt.Sprintf("lock held by %s for %s, wanted %s run", orWord(status.Host, "(unknown host)"), orWord(status.Operation, "(unknown operation)"), host),
			Fix: "stop the other holder (nova-sprint show run names it), then nova-sprint up again"}
	}
	hooked, _, err := p.Env.Hooked(ctx, "sprint")
	if err != nil {
		return false, err
	}
	if !hooked {
		return false, &RunNotReady{Why: "sprint is not hooked to the bus (nova-bus who does not list it) while run holds the lock on " + host, Fix: runLogFix(service) + ", then nova-sprint up again"}
	}
	return true, nil
}

// runLogFix is where to look: the run service's log, where nova-fleet's
// unit appends its output.
func runLogFix(service string) string {
	if service == "" {
		return "read the run service's log (nova-sprint show run)"
	}
	return "read the " + service + " log (tail ~/.local/state/nova/services/" + service + ".log)"
}

// unitState is the service unit's state and the last lines of its log, on
// one line: what launchctl print (darwin) or systemctl show (linux) says,
// then the log's tail. A read that fails says so in place of its part.
func (p PGSetupRun) unitState(ctx context.Context, service string) string {
	goos := p.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	var state string
	if goos == "darwin" {
		uid := os.Getuid
		if p.UID != nil {
			uid = p.UID
		}
		label := "com.nova.service." + service
		state = "unit " + label + " not loaded"
		for _, domain := range []string{"gui/" + strconv.Itoa(uid()), "system"} {
			out, err := p.Env.command(ctx, "launchctl", "print", domain+"/"+label)
			if err == nil {
				state = "unit " + label + ": " + launchdState(string(out))
				break
			}
		}
	} else {
		unit := "nova-service-" + service + ".service"
		state = "unit " + unit + " not loaded"
		for _, scope := range [][]string{{"--user"}, nil} {
			args := append(append([]string{}, scope...), "show", unit, "--property=LoadState,ActiveState,SubState,ExecMainStatus,NRestarts")
			out, err := p.Env.command(ctx, "systemctl", args...)
			if err == nil && !strings.Contains(string(out), "LoadState=not-found") {
				state = "unit " + unit + ": " + systemdState(string(out))
				break
			}
		}
	}
	home := p.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	log := path.Join(home, ".local/state/nova/services", service+".log")
	out, err := p.Env.command(ctx, "tail", "-n", "3", log)
	switch {
	case err != nil:
		return state + "; log unreadable (" + edge.RedactN(err.Error(), 120) + ")"
	case strings.TrimSpace(string(out)) == "":
		return state + "; log empty"
	}
	lines := []string{}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return state + "; log: " + edge.RedactN(strings.Join(lines, " | "), edge.FailureMax)
}

// launchdState is the state, pid and last exit code launchctl print shows.
func launchdState(out string) string {
	keep := []string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		for _, key := range []string{"state = ", "pid = ", "last exit code = ", "runs = "} {
			if strings.HasPrefix(line, key) && len(keep) < 4 {
				keep = append(keep, line)
			}
		}
	}
	if len(keep) == 0 {
		return "loaded, no state shown"
	}
	return edge.RedactN(strings.Join(keep, ", "), 160)
}

// systemdState is the properties systemctl show printed, as "name value"
// (a NAME=value pair would read as a secret to the redaction).
func systemdState(out string) string {
	keep := []string{}
	for _, line := range strings.Split(out, "\n") {
		if name, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			keep = append(keep, name+" "+value)
		}
	}
	if len(keep) == 0 {
		return "no state shown"
	}
	return edge.RedactN(strings.Join(keep, ", "), 160)
}
