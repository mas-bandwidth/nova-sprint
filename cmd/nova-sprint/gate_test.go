package main

import (
	"context"
	"errors"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge/bus"
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
	}{
		{"absent", []string{"take", "c@1", "--as", "bob"}, nil, here,
			"SPRINT REFUSED take: bob is not hooked in to the bus; hook in first: nova-bus hook --as bob"},
		{"absent via env", []string{"next"}, map[string]string{"NOVA_SPRINT_AS": "bob"}, here, bobNext},
		{"no args is next", nil, map[string]string{"NOVA_SPRINT_AS": "bob"}, here, bobNext},
		{"no name", []string{"queue"}, nil, here, noName},
		{"as without value", []string{"queue", "--as"}, nil, here, noName},
		{"bus down", []string{"queue", "--as=ann"}, map[string]string{"NOVA_BUS_REDIS": "10.0.0.1:6379"}, &fakePresence{err: errors.New("dial refused")},
			"SPRINT REFUSED queue: cannot reach the bus at 10.0.0.1:6379 (dial refused); hook in once it answers: nova-bus hook --as ann"},
		{"present", []string{"queue", "--as", "ann"}, nil, here, ""},
		{"present via env", []string{"queue"}, map[string]string{"NOVA_SPRINT_AS": "ann"}, here, ""},
		{"flag beats env", []string{"queue", "--as", "ann"}, map[string]string{"NOVA_SPRINT_AS": "bob"}, here, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hookGate(context.Background(), c.args, envOf(c.env), c.fp); got != c.want {
				t.Fatalf("hookGate(%v) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}

func TestHookGateExemptVerbsAskNothing(t *testing.T) {
	t.Parallel()
	for _, verb := range []string{"help", "-h", "--help", "version", "run", "dash", "worker"} {
		fp := &fakePresence{err: errors.New("down")}
		if got := hookGate(context.Background(), []string{verb}, envOf(nil), fp); got != "" || fp.calls != 0 {
			t.Fatalf("%s: gate = %q after %d lookups, want an exempt pass", verb, got, fp.calls)
		}
	}
}
