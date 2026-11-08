package main

import (
	"context"
	"errors"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

type fakePresence struct {
	names map[string]bus.Presence
	err   error
	calls int
}

func (f *fakePresence) Who(context.Context) (map[string]bus.Presence, error) {
	f.calls++
	return f.names, f.err
}

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestHookGate(t *testing.T) {
	t.Parallel()
	here := &fakePresence{names: map[string]bus.Presence{"ann": {}}}
	const noName = "SPRINT REFUSED queue: no acting name; pass --as <you> or set NOVA_SPRINT_AS=<you>, then hook in to the bus: nova-bus hook --as <you>"
	const bobNext = "SPRINT REFUSED next: bob is not hooked in to the bus; hook in first: nova-bus hook --as bob"
	cases := []struct {
		name string
		args []string
		env  map[string]string
		fp   *fakePresence
		want string
		exit int
	}{
		{"absent", []string{"take", "c@1", "--as", "bob"}, nil, here,
			"SPRINT REFUSED take: bob is not hooked in to the bus; hook in first: nova-bus hook --as bob", 1},
		{"absent via env", []string{"next"}, map[string]string{"NOVA_SPRINT_AS": "bob"}, here, bobNext, 1},
		{"no args is next", nil, map[string]string{"NOVA_SPRINT_AS": "bob"}, here, bobNext, 1},
		{"no name", []string{"queue"}, nil, here, noName, 1},
		{"as without value", []string{"queue", "--as"}, nil, here, noName, 1},
		// the bus failing is exit 2, as docs/README.md's exit-code table says
		{"bus down", []string{"queue", "--as=ann"}, map[string]string{"NOVA_BUS_REDIS": "10.0.0.1:6379"}, &fakePresence{err: errors.New("dial refused")},
			"SPRINT REFUSED queue: cannot reach the bus at 10.0.0.1:6379 (dial refused); hook in once it answers: nova-bus hook --as ann", 2},
		// unset, the fix is to set it, not a hook that cannot answer
		{"bus unset", []string{"check", "--as", "alice"}, nil, &fakePresence{err: errors.New("dial refused")},
			"SPRINT REFUSED check: cannot reach the bus: NOVA_BUS_REDIS is not set (dial refused): export NOVA_BUS_REDIS=<host:port>, then hook in: nova-bus hook --as alice", 2},
		// a refusal from the lookup (up's unset secrets store) is named as itself
		{"lookup refused", []string{"up", "--as", "alice"}, nil, &fakePresence{err: &verbs.OperationError{Problem: "control-seat secrets configuration is incomplete (NOVA_SECRETS_STORE is not set)", Fix: "set NOVA_SECRETS_STORE", Exit: 1}},
			"SPRINT REFUSED up: control-seat secrets configuration is incomplete (NOVA_SECRETS_STORE is not set): set NOVA_SECRETS_STORE", 1},
		{"present", []string{"queue", "--as", "ann"}, nil, here, "", 0},
		{"present via env", []string{"queue"}, map[string]string{"NOVA_SPRINT_AS": "ann"}, here, "", 0},
		{"flag beats env", []string{"queue", "--as", "ann"}, map[string]string{"NOVA_SPRINT_AS": "bob"}, here, "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, exit := hookGate(context.Background(), c.args, envOf(c.env), c.fp); got != c.want || exit != c.exit {
				t.Fatalf("hookGate(%v) = %q exit %d, want %q exit %d", c.args, got, exit, c.want, c.exit)
			}
		})
	}
}

// An unknown verb is refused as bad usage before the bus is asked.
func TestHookGateRefusesAnUnknownVerbBeforeTheBus(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"bogusverb", "--as", "alice"}, {"policy", "--as", "alice"}} {
		fp := &fakePresence{err: errors.New("down")}
		line, exit := hookGate(context.Background(), args, envOf(nil), fp)
		want := "SPRINT REFUSED nova-sprint: unknown verb " + args[0] + ": nova-sprint help"
		if line != want || exit != 2 || fp.calls != 0 {
			t.Fatalf("%v: gate = %q exit %d after %d lookups, want %q exit 2 and no lookup", args, line, exit, fp.calls, want)
		}
	}
}

func TestHookGateExemptVerbsAskNothing(t *testing.T) {
	t.Parallel()
	for _, verb := range []string{"help", "-h", "--help", "version", "run", "dash", "worker"} {
		fp := &fakePresence{err: errors.New("down")}
		if got, _ := hookGate(context.Background(), []string{verb}, envOf(nil), fp); got != "" || fp.calls != 0 {
			t.Fatalf("%s: gate = %q after %d lookups, want an exempt pass", verb, got, fp.calls)
		}
	}
}
