package verbs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/run"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func TestSetupRunRequiresRealOwnerAndPresence(t *testing.T) {
	t.Parallel()
	status := run.ChooserStatus{Held: true, Operation: "run", Host: "bench-a"}
	present := true
	r := PGSetupRun{Inspect: func(context.Context, string, string) (run.ChooserStatus, error) { return status, nil }, Env: ExecEnvironment{Command: func(context.Context, string, ...string) ([]byte, error) {
		if present {
			return []byte("WHO OK present=1\nsprint\t1m\n"), nil
		}
		return []byte("WHO OK present=0\n"), nil
	}}}
	ctx := context.Background()
	notReady := func(want string) {
		t.Helper()
		ok, err := r.Ready(ctx, "bench-a", "sprint-run")
		var nr *RunNotReady
		if ok || !errors.As(err, &nr) || !strings.Contains(nr.Why, want) || nr.Fix == "" {
			t.Fatalf("ok %v err %v, want not ready naming %q", ok, err, want)
		}
	}
	if ok, err := r.Ready(ctx, "bench-a", "sprint-run"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	status.Host = "other"
	notReady("lock held by other for run, wanted bench-a run")
	status.Host, status.Operation = "bench-a", "restore"
	notReady("lock held by bench-a for restore, wanted bench-a run")
	status.Operation = "run"
	present = false
	notReady("sprint is not hooked to the bus")
}

// A machine has one name: "x.local" holding the lock is machine "x"; a
// different machine is refused with both names.
func TestSetupRunComparesMachineNames(t *testing.T) {
	t.Parallel()
	status := run.ChooserStatus{Held: true, Operation: "run", Host: "x.local"}
	r := PGSetupRun{Inspect: func(context.Context, string, string) (run.ChooserStatus, error) { return status, nil }, Env: ExecEnvironment{Command: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("WHO OK present=1\nsprint\t1m\n"), nil
	}}}
	if ok, err := r.Ready(context.Background(), "x", "sprint-run"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	status.Host = "amd-2"
	_, err := r.Ready(context.Background(), "amd-1", "sprint-run")
	if err == nil || !strings.Contains(err.Error(), "amd-2") || !strings.Contains(err.Error(), "amd-1") {
		t.Fatal(err)
	}
}

// No lock held: the refusal carries the unit's state and its log's last
// lines, read through the injected command.
func TestSetupRunNoLockShowsUnitStateAndLog(t *testing.T) {
	t.Parallel()
	var calls []string
	cmd := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch {
		case name == "launchctl" && args[1] == "gui/501/com.nova.service.sprint-run":
			return []byte("gui/501/com.nova.service.sprint-run = {\n\tstate = waiting\n\truns = 7\n\tlast exit code = 1\n}\n"), nil
		case name == "tail":
			return []byte("starting\nrun: nova-config machine names: password authentication failed\n"), nil
		}
		return nil, errors.New("unexpected")
	}
	r := PGSetupRun{GOOS: "darwin", Home: "/home/u", UID: func() int { return 501 }, Inspect: func(context.Context, string, string) (run.ChooserStatus, error) { return run.ChooserStatus{}, nil }, Env: ExecEnvironment{Command: cmd}}
	_, err := r.Ready(context.Background(), "x", "sprint-run")
	var nr *RunNotReady
	if !errors.As(err, &nr) {
		t.Fatal(err)
	}
	for _, want := range []string{"run service holds no chooser lock", "com.nova.service.sprint-run", "state = waiting", "last exit code = 1", "password authentication failed", "starting |"} {
		if !strings.Contains(nr.Why, want) {
			t.Fatalf("%q lacks %q", nr.Why, want)
		}
	}
	if calls[len(calls)-1] != "tail -n 3 /home/u/.local/state/nova/services/sprint-run.log" {
		t.Fatal(calls)
	}
	r.GOOS = "linux"
	r.Env.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "systemctl" && args[0] == "--user" {
			return []byte("LoadState=loaded\nActiveState=activating\nSubState=auto-restart\nExecMainStatus=1\nNRestarts=4\n"), nil
		}
		return nil, errors.New("no such file")
	}
	_, err = r.Ready(context.Background(), "x", "sprint-run")
	if !errors.As(err, &nr) || !strings.Contains(nr.Why, "nova-service-sprint-run.service: LoadState loaded, ActiveState activating, SubState auto-restart") || !strings.Contains(nr.Why, "log unreadable") {
		t.Fatal(err)
	}
}

// The run step still not ready after its act is a refusal naming the condition,
// never "step 6 is not verified".
func TestRuntimeRunCheckRefusalNamesTheCondition(t *testing.T) {
	t.Parallel()
	r := RuntimeSetup{Run: notReadyRun{}, RunService: FleetService{Name: "sprint-run", Machine: "bench-a"}, DefaultRunHost: "bench-a"}
	r.Prerequisites.Env.Choices = Choices{Store: store.NewFake(), Actor: "alice", Now: func() time.Time { return time.Unix(1000, 0) }}
	if err := r.Prerequisites.Env.Choices.Save(context.Background(), "run-on", "bench-a"); err != nil {
		t.Fatal(err)
	}
	check, err := r.runCheck(context.Background(), SetupInput{})
	if err != nil || !check.Action || check.Refusal == nil || !strings.Contains(check.Refusal.Problem, "run on bench-a: run service holds no chooser lock") || check.Refusal.Fix == "" {
		t.Fatal(check, err)
	}
}

type notReadyRun struct{}

func (notReadyRun) Ready(context.Context, string, string) (bool, error) {
	return false, &RunNotReady{Why: "run service holds no chooser lock", Fix: "read the log"}
}

// The run step stores and activates the built-in default policy before run
// starts, once: a second act changes nothing. Its line names the secrets
// store the run service opens.
func TestRuntimeRunStoresDefaultPolicyOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := store.NewFake()
	now := func() time.Time { return time.Unix(1000, 0) }
	r := RuntimeSetup{Run: &runtimeRun{ready: true}, PolicyStore: db, Now: now, RunService: FleetService{Name: "sprint-run", Machine: "bench-a", Seat: "bench-a", Keys: []string{sprintPasswordName}}, DefaultRunHost: "bench-a"}
	r.Prerequisites.Env.Choices = Choices{Store: db, Actor: "alice", Now: now}
	if err := r.Prerequisites.Env.Choices.Save(ctx, "run-on", "bench-a"); err != nil {
		t.Fatal(err)
	}
	check, err := r.runCheck(ctx, SetupInput{Actor: "alice"})
	if err != nil || check.Verified || !check.Action {
		t.Fatal(check, err)
	}
	var first store.Snapshot
	for i := range 2 {
		if err = r.ensurePolicy(ctx, "alice"); err != nil {
			t.Fatal(err)
		}
		snap, err := db.Snapshot(ctx)
		if err != nil || snap.Policy == 0 {
			t.Fatal(snap.Policy, err)
		}
		if i == 0 {
			first = snap
		} else if snap.Policy != first.Policy || snap.Seq != first.Seq {
			t.Fatalf("a second up changed the store: policy %d seq %d, was %d %d", snap.Policy, snap.Seq, first.Policy, first.Seq)
		}
	}
	check, err = r.runCheck(ctx, SetupInput{Actor: "alice"})
	if err != nil || !check.Verified || !strings.Contains(check.Line, "from nova-fleet's secrets store on bench-a (~/.local/share/nova/secrets, seat bench-a), not this shell's NOVA_SECRETS_STORE") {
		t.Fatal(check, err)
	}
}

// A run machine or dashboard target that does not fit names why.
func TestRuntimeTargetRefusalsNameTheirCause(t *testing.T) {
	t.Parallel()
	if err := runHostMatches("amd-2", "amd-1"); err == nil || !strings.Contains(err.Error(), `run machine "amd-2" is not "amd-1"`) {
		t.Fatal(err)
	}
	if err := runHostMatches("", "amd-1"); err == nil || !strings.Contains(err.Error(), "no run machine is chosen") {
		t.Fatal(err)
	}
	r := RuntimeSetup{Dashboards: map[string]DashboardTarget{"public=edge-1": {Public: true, Service: &FleetService{Name: "sprint-dashboard", Machine: "edge-1", Seat: "edge-1"}}}}
	for choice, want := range map[string]string{
		"tailnet":       `dashboard choice "tailnet" has no configured target`,
		"public=edge-1": "the public dashboard service on edge-1 is not a pull-only nova-sprint dash without secrets",
	} {
		if _, err := r.dashboardTarget(choice); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatal(choice, err)
		}
	}
}

// A run holding the lock from an older build than this one, or from one
// whose lock names no build, is behind; a current one, or a check with no
// release to compare (a source build), is ready.
func TestSetupRunOfAnOlderBuildIsBehind(t *testing.T) {
	t.Parallel()
	status := run.ChooserStatus{Held: true, Operation: "run", Host: "bench-a", Version: "v2.0.36"}
	r := PGSetupRun{Release: "v2.0.37", Inspect: func(context.Context, string, string) (run.ChooserStatus, error) { return status, nil }, Env: ExecEnvironment{Command: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("WHO OK present=1\nsprint\t1m\n"), nil
	}}}
	for _, version := range []string{"v2.0.36", ""} {
		status.Version = version
		ok, err := r.Ready(context.Background(), "bench-a", "sprint-run")
		var nr *RunNotReady
		if ok || !errors.As(err, &nr) || !nr.Behind || !strings.Contains(nr.Why, "below this build's v2.0.37") || !strings.Contains(nr.Fix, "restarts it through nova-fleet services") {
			t.Fatal(version, ok, err)
		}
	}
	status.Version = "v2.0.37"
	if ok, err := r.Ready(context.Background(), "bench-a", "sprint-run"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	status.Version = run.VersionUnknown // a lock name cut at run.NameMax: never behind, so up never restarts the run in a loop
	if ok, err := r.Ready(context.Background(), "bench-a", "sprint-run"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	status.Version, r.Release = "v2.0.36", "dev+0123456789ab"
	if ok, err := r.Ready(context.Background(), "bench-a", "sprint-run"); !ok || err != nil {
		t.Fatal(ok, err)
	}
}
