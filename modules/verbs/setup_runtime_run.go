package verbs

import (
	"context"
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/modules/run"
)

// PGSetupRun reads the chooser's owner API and nova-bus; service records alone
// never establish that run is alive or that the requested host holds the lock.
// When run holds no lock, the refusal names the service's log.
type PGSetupRun struct {
	DSN, Password string
	Env           ExecEnvironment
	Inspect       func(context.Context, string, string) (run.ChooserStatus, error)
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
		return false, &RunNotReady{Why: "run service holds no chooser lock", Fix: runLogFix(service) + ", fix what it says, then nova-sprint up again"}
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
