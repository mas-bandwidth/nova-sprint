package verbs

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type setupLeaseFake struct{}

func (setupLeaseFake) Acquire(context.Context) (SetupLease, error) { return setupLeaseFake{}, nil }
func (setupLeaseFake) Monitor(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(ctx)
}
func (setupLeaseFake) Check(context.Context) error { return nil }
func (setupLeaseFake) Close() error                { return nil }

type setupFake struct {
	ready [9]bool
	trace []string
	stop  int
}

func (f *setupFake) Completion(context.Context) (string, error) {
	return "UP DONE run on studio; dashboard http://studio; 4 workers up; coordinator rowan", nil
}
func (f *setupFake) Verify(_ context.Context, n int, _ SetupInput) (SetupCheck, error) {
	f.trace = append(f.trace, fmt.Sprintf("verify%d", n))
	if n == f.stop && !f.ready[n] {
		return SetupCheck{Line: "ASK dashboard", Next: "nova-sprint up --dash tailnet"}, nil
	}
	return SetupCheck{Verified: f.ready[n], Action: !f.ready[n], Line: fmt.Sprintf("UP %d/8", n)}, nil
}
func (f *setupFake) Act(_ context.Context, n int, _ SetupInput) error {
	f.trace = append(f.trace, fmt.Sprintf("act%d", n))
	f.ready[n] = true
	return nil
}
func TestSetupVerifiesActionsAndRestarts(t *testing.T) {
	f := &setupFake{stop: 5}
	e := Engine{Actor: "rowan", Setup: f, SetupLock: setupLeaseFake{}}
	first := e.Execute(context.Background(), []string{"up"})
	if first.Exit != 1 || !strings.Contains(first.Output, "ASK dashboard\nnext:") {
		t.Fatal(first)
	}
	if strings.Join(f.trace, ",") != "verify1,act1,verify1,verify1,verify2,act2,verify2,verify3,act3,verify3,verify4,act4,verify4,verify5" {
		t.Fatal(f.trace)
	}
	f.trace = nil
	f.stop = 0
	f.ready[2] = false
	second := e.Execute(context.Background(), []string{"up", "--dash", "tailnet"})
	if second.Exit != 0 || !strings.HasPrefix(strings.Join(f.trace, ","), "verify1,verify1,verify2,act2,verify2") {
		t.Fatal(second, f.trace)
	}
}
func TestSetupArguments(t *testing.T) {
	e := Engine{}
	got := e.Execute(context.Background(), []string{"up", "--dash", "invalid"})
	if got.Exit != 1 || got.Output != "SPRINT REFUSED up: invalid arguments: nova-sprint help up\n" {
		t.Fatal(got)
	}
}
