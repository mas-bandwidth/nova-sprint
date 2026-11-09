package verbs

import (
	"context"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"net/url"
	"slices"
	"strings"
	"time"
)

// SetupStore implements the role, schema identity and ownership, and
// migration; Ready observes the world, never a saved checkpoint.
type SetupStore interface {
	Ready(context.Context) (bool, error)
	Install(context.Context) error
}

// SetupRun observes the chooser lock and sprint bus presence on a machine
// (by name) for a service (by name); not ready is (false, *RunNotReady).
type SetupRun interface {
	Ready(ctx context.Context, machine, service string) (bool, error)
}

// SetupWorkload is supplied by the binary for steps 6 and 7. WorkersReady
// proves every worker bench (WorkerBenches) is hooked on the bus and up in
// the store; SmokeReady proves every worker row is dealable when the
// sprint starts. A not-ready answer is (false, *WorkloadNotReady) naming
// what is not.
type SetupWorkload interface {
	WorkersReady(context.Context) (bool, error)
	InstallWorkers(context.Context) error
	SmokeReady(context.Context) (bool, error)
}

// WorkloadNotReady is a step 6 or 7 that is not ready: Why names each
// bench that is not and why, Fix the one thing to do.
type WorkloadNotReady struct{ Why, Fix string }

func (w *WorkloadNotReady) Error() string { return "not ready: " + w.Why }

// WorkerBenches is the machines a sprint worker runs on: every bench, the
// run machine too when it is one (nova-config's bench role is the only
// word, 2026-11-07), by name, sorted as machines are.
func WorkerBenches(machines []FleetMachine) []string {
	var benches []string
	for _, m := range machines {
		if slices.Contains(m.Roles, "bench") {
			benches = append(benches, m.Name)
		}
	}
	return benches
}

// workloadReady is setupReady for steps 6 and 7: a *WorkloadNotReady is
// its refusal, after an act first when act.
func workloadReady(ctx context.Context, act, ok bool, line string, err error) (SetupCheck, error) {
	var notReady *WorkloadNotReady
	if errors.As(err, &notReady) && ctx.Err() == nil {
		return SetupCheck{Action: act, Refusal: &OperationError{Problem: edge.RedactN(notReady.Why, edge.FailureMax+400), Fix: notReady.Fix, Exit: 1}}, nil
	}
	check, err := setupReady(ok, line, err)
	check.Action = check.Action && act
	return check, err
}

// DashboardTarget is where the board answers: URL, public or not, and for
// a public board the pull-only service that serves it.
type DashboardTarget struct {
	URL     string
	Public  bool
	Service *FleetService
}

// RuntimeSetup is steps 1, 2 and 4 to 7 over the real tools; the binary
// resolves the run service and the board before any step past 3.
type RuntimeSetup struct {
	Prerequisites SetupPrerequisites
	Store         SetupStore
	Run           SetupRun
	Workload      SetupWorkload
	Services      FleetServices
	Role          SprintRoleBootstrap
	Dashboard     DashboardProbe
	Snapshot      Reader
	RunService    FleetService
	Board         DashboardTarget
	// PolicyStore is where step 4 stores and activates the built-in
	// default policy (policy.Up) before run starts; nil skips it.
	PolicyStore store.Store
	Now         func() time.Time // nil: time.Now
}

func (r RuntimeSetup) Verify(ctx context.Context, step int, _ SetupInput) (SetupCheck, error) {
	switch step {
	case 1:
		return r.Prerequisites.Tools(ctx)
	case 2:
		if r.Store == nil {
			return SetupCheck{}, fmt.Errorf("store setup unavailable")
		}
		ok, err := r.Store.Ready(ctx)
		return setupReady(ok, fmt.Sprintf("UP 2/7 store: role password set from the seat; schema sprint version %d, owned by nova_sprint: VERIFIED", store.SchemaVersion), err)
	case 4:
		return r.runCheck(ctx)
	case 5:
		return r.dashboardCheck(ctx)
	case 6:
		if r.Workload == nil {
			return SetupCheck{}, fmt.Errorf("worker setup unavailable")
		}
		id, check, err := r.commitIdentity(ctx)
		if err != nil || !id.Set() {
			return check, err
		}
		ok, err := r.Workload.WorkersReady(ctx)
		return workloadReady(ctx, true, ok, "UP 6/7 workers: every worker bench at this release, its harness probed with the policy's models and its seat's provider keys, hooked on the bus and up in the store, each committing as "+id.Name+" <"+id.Email+">: VERIFIED", err)
	case 7:
		if r.Workload == nil {
			return SetupCheck{}, fmt.Errorf("worker setup unavailable")
		}
		ok, err := r.Workload.SmokeReady(ctx)
		return workloadReady(ctx, false, ok, "UP 7/7 first deal: every worker row hooked, up and dealable (width from its cores); the sprint deals once started: VERIFIED", err)
	}
	return SetupCheck{}, fmt.Errorf("invalid setup step %d", step)
}

// commitIdentity is step 6's first condition: the sprint's commit identity
// (store.Identity) is set, since a worker hooks only once it is (hook
// refuses) and each job's clone takes it from the take receipt. None set is
// a question for the human: up stops with the command that sets it, and no
// act can make it true.
func (r RuntimeSetup) commitIdentity(ctx context.Context) (store.Identity, SetupCheck, error) {
	if r.Snapshot == nil {
		return store.Identity{}, SetupCheck{}, fmt.Errorf("store unavailable")
	}
	snap, err := r.Snapshot.Snapshot(ctx)
	if err != nil || snap.Identity.Set() {
		return snap.Identity, SetupCheck{}, err
	}
	return snap.Identity, SetupCheck{Line: "UP 6/7 workers: the sprint has no commit identity\nASK the human which forge account on the repositories' contributor-agreement allowlist every commit is made as (its git user.name and user.email, typically its noreply address)",
		Next: identityFix + "   then nova-sprint up again"}, nil
}

func setupReady(ok bool, line string, err error) (SetupCheck, error) {
	if err != nil {
		return SetupCheck{}, err
	}
	if ok {
		return SetupCheck{Verified: true, Line: line}, nil
	}
	return SetupCheck{Action: true}, nil
}

func (r RuntimeSetup) runCheck(ctx context.Context) (SetupCheck, error) {
	if r.Run == nil {
		return SetupCheck{}, fmt.Errorf("run readiness unavailable")
	}
	host := r.RunService.Machine
	active, err := r.policyActive(ctx)
	if err != nil {
		return SetupCheck{}, err
	}
	if !active {
		return SetupCheck{Action: true, Refusal: &OperationError{Problem: "no policy version is active after storing the built-in default", Fix: "nova-sprint policy show --list", Exit: 1}}, nil
	}
	secrets := "UP 4/7 run: on " + host + "; " + runSecretsLine(r.RunService)
	ok, err := r.Run.Ready(ctx, host, r.RunService.Name)
	if err != nil && ctx.Err() == nil {
		// Not up yet: act; when it still is not after the act, the
		// refusal names each false condition, never "not verified".
		if notReady := (*RunNotReady)(nil); errors.As(err, &notReady) {
			return SetupCheck{Action: true, Line: secrets, Refusal: &OperationError{Problem: edge.RedactN("run on "+host+": "+notReady.Why, edge.FailureMax+200), Fix: notReady.Fix, Exit: 1}}, nil
		}
	}
	return setupReady(ok, secrets+"; policy active; chooser lock held; sprint hooked: VERIFIED", err)
}

// runSecretsLine says which secrets store the run service opens: nova-fleet
// renders its unit to exec nova-secrets on the run machine's own default
// replica (nova_secrets_store), as the service's seat, never the store
// path this shell's NOVA_SECRETS_STORE names.
func runSecretsLine(s FleetService) string {
	if len(s.Keys) == 0 {
		return "the service opens no secrets"
	}
	keys := append([]string(nil), s.Keys...)
	slices.Sort(keys)
	return "the service opens " + strings.Join(keys, ",") + " from nova-fleet's secrets store on " + s.Machine + " (~/.local/share/nova/secrets, seat " + s.Seat + "), not this shell's NOVA_SECRETS_STORE"
}

// policyActive is true when a policy version is active (or no store is
// wired for it).
func (r RuntimeSetup) policyActive(ctx context.Context) (bool, error) {
	if r.PolicyStore == nil {
		return true, nil
	}
	snap, err := r.PolicyStore.Snapshot(ctx)
	if err != nil {
		return false, err
	}
	return snap.Policy > 0, nil
}

// ensurePolicy stores and activates the built-in default policy when no
// version is active (policy.Up, idempotent: it changes nothing once one
// is), so run starts on a policy.
func (r RuntimeSetup) ensurePolicy(ctx context.Context, actor string) error {
	if r.PolicyStore == nil {
		return nil
	}
	snap, err := r.PolicyStore.Snapshot(ctx)
	if err != nil || snap.Policy > 0 {
		return err
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	c := policy.Caller{Actor: actor, Coordinator: true, OpID: fmt.Sprintf("up-policy-%d-%d", snap.Epoch, snap.Seq), At: now()}
	_, err = policy.Up(ctx, r.PolicyStore, c)
	return err
}

func (r RuntimeSetup) dashboardCheck(ctx context.Context) (SetupCheck, error) {
	target := r.Board
	if target.URL == "" {
		return SetupCheck{}, fmt.Errorf("dashboard target unavailable")
	}
	if r.Snapshot == nil {
		return SetupCheck{}, fmt.Errorf("store unavailable")
	}
	snap, err := r.Snapshot.Snapshot(ctx)
	if err != nil {
		return SetupCheck{}, err
	}
	ok, err := r.Dashboard.Verify(ctx, target.URL, snap.Epoch, target.Public)
	if refusal := r.dashboardRefusal(target, err); refusal != nil && ctx.Err() == nil {
		// Not up yet: act; when it still does not verify after the act,
		// the refusal is the probe's own error and its fix.
		return SetupCheck{Action: true, Refusal: refusal}, nil
	}
	return setupReady(ok, "UP 5/7 dashboard: "+target.URL+": VERIFIED", err)
}

// dashboardRefusal is the probe's error as a refusal with its fix: an
// unreachable board names the port to open on the machine that serves it;
// one that answers wrong names what it answered. nil for any other error.
func (r RuntimeSetup) dashboardRefusal(target DashboardTarget, err error) *OperationError {
	machine, service := r.RunService.Machine, r.RunService.Name
	if target.Public && target.Service != nil {
		machine, service = target.Service.Machine, target.Service.Name
	}
	var unreachable *DashboardUnreachable
	var notReady *DashboardNotReady
	switch {
	case errors.As(err, &unreachable):
		port := "8080"
		if u, perr := url.Parse(target.URL); perr == nil && u.Port() != "" {
			port = u.Port()
		}
		fix := "open " + port + "/tcp on " + machine + " for the dashboard"
		if target.Public && port != "80" {
			fix += ", or front it on 80 (--dash public=" + machine + ":80)"
		}
		return &OperationError{Problem: edge.RedactN("dashboard unreachable: "+unreachable.Error(), edge.FailureMax), Fix: fix + ", then nova-sprint up again", Exit: 1}
	case errors.As(err, &notReady):
		return &OperationError{Problem: edge.RedactN("dashboard "+target.URL+" "+notReady.Why, edge.FailureMax), Fix: runLogFix(service) + " on " + machine + ", then nova-sprint up again", Exit: 1}
	}
	return nil
}

func (r RuntimeSetup) Act(ctx context.Context, step int, in SetupInput) error {
	switch step {
	case 2:
		if r.Store == nil {
			return fmt.Errorf("store setup unavailable")
		}
		return r.Store.Install(ctx)
	case 4:
		return r.runAct(ctx, in)
	case 5:
		return r.dashboardAct(ctx, in)
	case 6:
		return r.workersAct(ctx)
	}
	return fmt.Errorf("setup action unavailable for step %d", step)
}

func (r RuntimeSetup) runAct(ctx context.Context, in SetupInput) error {
	if err := r.ensurePolicy(ctx, in.Actor); err != nil {
		return err
	}
	if !slices.Contains(r.RunService.Keys, sprintPasswordName) {
		return fmt.Errorf("run service lacks sprint credential")
	}
	if err := r.Role.GrantSeat(ctx, SprintSeat{Seat: r.RunService.Seat, Machine: r.RunService.Machine}); err != nil {
		return err
	}
	services := r.Services.For(in.Actor, "nova-sprint up 4/7 run")
	if _, err := services.Ensure(ctx, r.RunService); err != nil {
		return err
	}
	return services.Apply(ctx, r.RunService)
}

func (r RuntimeSetup) dashboardAct(ctx context.Context, in SetupInput) error {
	service := r.RunService
	if r.Board.Service != nil {
		service = *r.Board.Service
	}
	services := r.Services.For(in.Actor, "nova-sprint up 5/7 dashboard")
	if _, err := services.Ensure(ctx, service); err != nil {
		return err
	}
	return services.Apply(ctx, service)
}

func (r RuntimeSetup) workersAct(ctx context.Context) error {
	if r.Workload == nil {
		return fmt.Errorf("worker setup unavailable")
	}
	machines, err := r.Services.Machines(ctx)
	if err != nil {
		return err
	}
	seats, err := SprintSeats(machines, r.RunService, r.publicHost())
	if err != nil {
		return err
	}
	for _, seat := range seats {
		if err = r.Role.GrantSeat(ctx, seat); err != nil {
			return err
		}
	}
	return r.Workload.InstallWorkers(ctx)
}

// publicHost is the public board's machine, "" for the tailnet board.
func (r RuntimeSetup) publicHost() string {
	if r.Board.Public && r.Board.Service != nil {
		return r.Board.Service.Machine
	}
	return ""
}

// Completion is UP DONE from what the steps verified: the run machine,
// the board, the worker benches and the coordinator.
func (r RuntimeSetup) Completion(ctx context.Context, in SetupInput) (string, error) {
	machines, err := r.Services.Machines(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("UP DONE run on %s; dashboard %s; %d workers up; coordinator %s", r.RunService.Machine, r.Board.URL, len(WorkerBenches(machines)), in.Actor), nil
}
