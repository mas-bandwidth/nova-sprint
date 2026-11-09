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

// Step 4 still not ready after its act is a refusal naming the condition,
// never "step 4 is not verified".
func TestRuntimeRunCheckRefusalNamesTheCondition(t *testing.T) {
	t.Parallel()
	r := RuntimeSetup{Run: notReadyRun{}, RunService: FleetService{Name: "sprint-run", Machine: "bench-a"}}
	check, err := r.runCheck(context.Background())
	if err != nil || !check.Action || check.Refusal == nil || !strings.Contains(check.Refusal.Problem, "run on bench-a: run service holds no chooser lock") || check.Refusal.Fix == "" {
		t.Fatal(check, err)
	}
}

type notReadyRun struct{}

func (notReadyRun) Ready(context.Context, string, string) (bool, error) {
	return false, &RunNotReady{Why: "run service holds no chooser lock", Fix: "read the log"}
}

// Step 4 stores and activates the built-in default policy before run
// starts, once: a second act changes nothing. Its line names the secrets
// store the run service opens.
func TestRuntimeRunStoresDefaultPolicyOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := store.NewFake()
	now := func() time.Time { return time.Unix(1000, 0) }
	r := RuntimeSetup{Run: &runtimeRun{ready: true}, PolicyStore: db, Now: now, RunService: FleetService{Name: "sprint-run", Machine: "bench-a", Seat: "bench-a", Keys: []string{sprintPasswordName}}}
	check, err := r.runCheck(ctx)
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
	check, err = r.runCheck(ctx)
	if err != nil || !check.Verified || !strings.Contains(check.Line, "from nova-fleet's secrets store on bench-a (~/.local/share/nova/secrets, seat bench-a), not this shell's NOVA_SECRETS_STORE") {
		t.Fatal(check, err)
	}
}
