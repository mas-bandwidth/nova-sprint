package verbs

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type handoffLock struct {
	held     bool
	acquired int
	onClose  func()
}

func (l *handoffLock) Acquire(context.Context) (SetupLease, error) {
	l.held = true
	l.acquired++
	return l, nil
}
func (l *handoffLock) Monitor(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(ctx)
}
func (l *handoffLock) Check(context.Context) error { return nil }
func (l *handoffLock) Close() error {
	l.held = false
	if l.onClose != nil {
		l.onClose()
	}
	return nil
}

type handoffSetup struct {
	setupFake
	validated   bool
	validations int
}

func (s *handoffSetup) PrepareHandoff(context.Context, SetupInput) (string, bool, error) {
	return strings.Repeat("a", 64), true, nil
}
func (s *handoffSetup) ValidateHandoff(_ context.Context, id string, _ SetupInput) error {
	s.validations++
	s.validated = id == strings.Repeat("a", 64)
	return nil
}
func TestSetupHandoffClosesLeaseBeforeSignal(t *testing.T) {
	s := &handoffSetup{}
	lock := &handoffLock{}
	called := false
	e := Engine{Actor: "rowan", Setup: s, SetupLock: lock, ReportContinuation: func(_ context.Context, id string, in SetupInput) error {
		if lock.held || id != strings.Repeat("a", 64) || in.Actor != "rowan" {
			t.Fatal(lock.held, id, in)
		}
		called = true
		return nil
	}}
	got := e.Execute(context.Background(), []string{"up"})
	if got.Exit != 0 || got.Output != "" || !called {
		t.Fatal(got)
	}
	if strings.Contains(strings.Join(s.trace, ","), "verify3") {
		t.Fatal(s.trace)
	}
	s.trace = nil
	got = e.Execute(context.Background(), []string{"up", "--continuation", strings.Repeat("a", 64)})
	if got.Exit != 0 || !s.validated || s.validations != 2 || lock.held || !strings.Contains(strings.Join(s.trace, ","), "verify8") {
		t.Fatal(got, s.trace)
	}
}

func TestSetupObservationsFollowActualCalls(t *testing.T) {
	s := &setupFake{stop: 5}
	var seen []SetupObservation
	e := Engine{Actor: "rowan", Setup: s, SetupLock: setupLeaseFake{}, SetupObserver: func(o SetupObservation) { seen = append(seen, o) }}
	r := e.Execute(context.Background(), []string{"up"})
	if r.Exit != 1 {
		t.Fatal(r)
	}
	actual := []string{}
	for _, o := range seen {
		if o.Kind == "verify" || o.Kind == "act" {
			actual = append(actual, fmt.Sprintf("%s%d", o.Kind, o.Step))
		}
	}
	if strings.Join(actual, ",") != strings.Join(s.trace, ",") {
		t.Fatal(actual, s.trace)
	}
	found := false
	for _, o := range seen {
		if o.Scope == "walk" && o.Kind == "stop" && o.Step == 5 {
			found = true
		}
	}
	if !found {
		t.Fatal(seen)
	}
}

type rejectingHandoff struct{ handoffSetup }

func (*rejectingHandoff) ValidateHandoff(context.Context, string, SetupInput) error {
	return &OperationError{Problem: "continuation changed", Fix: "nova-sprint up", Exit: 1}
}
func TestSetupRejectsContinuationBeforeLockedSteps(t *testing.T) {
	s := &rejectingHandoff{}
	lock := &handoffLock{}
	e := Engine{Actor: "rowan", Setup: s, SetupLock: lock}
	got := e.Execute(context.Background(), []string{"up", "--continuation", strings.Repeat("a", 64)})
	if got.Exit != 1 || lock.held || lock.acquired != 0 || len(s.trace) != 0 {
		t.Fatal(got, s.trace, lock.held)
	}
}

func TestSetupHandoffCancellationBeforeSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lock := &handoffLock{onClose: cancel}
	called := false
	e := Engine{Actor: "rowan", Setup: &handoffSetup{}, SetupLock: lock, ReportContinuation: func(context.Context, string, SetupInput) error {
		called = true
		return nil
	}}
	got := e.Execute(ctx, []string{"up"})
	if got.Exit == 0 || called || lock.held {
		t.Fatal(got, called, lock.held)
	}
}
