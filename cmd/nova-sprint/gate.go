package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

// gateExempt are the words that cannot be an operator acting: the help and
// version text, and the three long-running services that are the machine
// itself (the run service, the dashboard, a worker, which hooks itself).
var gateExempt = map[string]bool{"help": true, "-h": true, "--help": true, "version": true, "run": true, "dash": true, "worker": true}

// gateTimeout bounds the one presence lookup.
const gateTimeout = 10 * time.Second

// hookGate is the one check before any verb runs: the acting name (--as,
// else NOVA_SPRINT_AS) is on the bus. It returns the refusal line, or "" to
// proceed. It reads only presence: no store read, no store write, no send.
// An absent name, a missing name and a bus that does not answer all refuse.
func hookGate(ctx context.Context, args []string, getenv func(string) string, who verbs.Presence) string {
	verb := "next"
	if len(args) > 0 {
		verb = args[0]
	}
	if gateExempt[verb] {
		return ""
	}
	name := actingName(args, getenv("NOVA_SPRINT_AS"))
	if name == "" {
		return "SPRINT REFUSED " + verb + ": no acting name; pass --as <you> or set NOVA_SPRINT_AS=<you>, then hook in to the bus: nova-bus hook --as <you>"
	}
	ctx, cancel := context.WithTimeout(ctx, gateTimeout)
	defer cancel()
	present, err := who.Who(ctx)
	if err != nil {
		addr := getenv("NOVA_BUS_REDIS")
		if addr == "" {
			addr = "NOVA_BUS_REDIS (unset)"
		}
		return fmt.Sprintf("SPRINT REFUSED %s: cannot reach the bus at %s (%s); hook in once it answers: nova-bus hook --as %s", verb, addr, edge.RedactN(err.Error(), edge.CommandMax), name)
	}
	if _, ok := present[name]; !ok {
		return fmt.Sprintf("SPRINT REFUSED %s: %s is not hooked in to the bus; hook in first: nova-bus hook --as %s", verb, name, name)
	}
	return ""
}

// actingName is --as <name> or --as=<name> before any "--", else fallback.
func actingName(args []string, fallback string) string {
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		if v, ok := strings.CutPrefix(a, "--as="); ok {
			return v
		}
		if a == "--as" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
	}
	return fallback
}
