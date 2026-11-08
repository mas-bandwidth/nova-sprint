package verbs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/store"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"
)

// SetupStore implements role, schema identity/ownership, migration and the
// verified first backup; Ready observes the world, never a saved checkpoint.
type SetupStore interface {
	Ready(context.Context) (bool, error)
	Install(context.Context) error
}

// SetupRun observes the chooser lock and sprint bus presence on a machine
// (by name) for a service (by name); not ready is (false, *RunNotReady).
type SetupRun interface {
	Ready(ctx context.Context, machine, service string) (bool, error)
}

// SetupWorkload is supplied by the binary for steps 6 to 8 (services,
// workers, first deal). InstallWorkers writes the workers for the providers
// step 6 saved (their routes and keys, ServiceRoutes). WorkersReady proves every worker bench
// (WorkerBenches) is hooked on the bus and up in the store; SmokeReady
// proves every worker row is dealable when the sprint starts. A not-ready
// answer is (false, *WorkloadNotReady) naming what is not: up acts, and
// when the step still does not verify that is the refusal.
type SetupWorkload interface {
	ServicesReady(context.Context, []string) (bool, error)
	InstallServices(context.Context, []string) error
	WorkersReady(context.Context) (bool, error)
	InstallWorkers(ctx context.Context, chosen map[string]policy.Provider) error
	SmokeReady(context.Context) (bool, error)
	RunSmoke(context.Context) error
}

// WorkloadNotReady is a step 7 or 8 that is not ready: Why names each
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

// workloadReady is setupReady for steps 7 and 8: a *WorkloadNotReady is
// an act first, then its refusal.
func workloadReady(ctx context.Context, ok bool, line string, err error) (SetupCheck, error) {
	var notReady *WorkloadNotReady
	if errors.As(err, &notReady) && ctx.Err() == nil {
		return SetupCheck{Action: true, Refusal: &OperationError{Problem: edge.RedactN(notReady.Why, edge.FailureMax+400), Fix: notReady.Fix, Exit: 1}}, nil
	}
	return setupReady(ok, line, err)
}

// SetupAdoption verifies an installed release and runs the approved updater.
type SetupAdoption interface {
	Ready(context.Context, string) (bool, error)
	Apply(context.Context, string) error
}
type DashboardTarget struct {
	URL     string
	Public  bool
	Service *FleetService
}

// RuntimeSetup combines real tools with owner-supplied store/run/worker seams.
type RuntimeSetup struct {
	Prerequisites  SetupPrerequisites
	Store          SetupStore
	Run            SetupRun
	Workload       SetupWorkload
	Services       FleetServices
	Keys           ServiceKey
	Role           SprintRoleBootstrap
	Adoption       SetupAdoption
	Dashboard      DashboardProbe
	Snapshot       Reader
	RunService     FleetService
	DefaultRunHost string
	Dashboards     map[string]DashboardTarget
	// PolicyStore is where step 4 stores and activates the built-in
	// default policy (policy.Up) before run starts; nil skips it.
	PolicyStore store.Store
	// Providers is the API-rate providers the active policy's catalog
	// declares (policy.Catalog.Providers), the services up offers; nil
	// declares none.
	Providers func(context.Context) (map[string]policy.Provider, error)
	Now       func() time.Time // nil: time.Now
}

func (r RuntimeSetup) Verify(ctx context.Context, step int, in SetupInput) (SetupCheck, error) {
	switch step {
	case 1:
		return r.Prerequisites.Tools(ctx)
	case 2:
		if r.Store == nil {
			return SetupCheck{}, fmt.Errorf("store setup unavailable")
		}
		ok, err := r.Store.Ready(ctx)
		if err != nil || !ok || in.Version == "" {
			return setupReady(ok, fmt.Sprintf("UP 2/8 store: schema sprint version %d and backup: VERIFIED", store.SchemaVersion), err)
		}
		if r.Adoption == nil {
			return SetupCheck{}, fmt.Errorf("version adoption unavailable")
		}
		ok, err = r.Adoption.Ready(ctx, in.Version)
		return setupReady(ok, fmt.Sprintf("UP 2/8 store: schema sprint version %d, backup and version check: VERIFIED", store.SchemaVersion), err)
	case 3:
		return r.Prerequisites.Coordinator(ctx, in.Actor)
	case 4:
		return r.runCheck(ctx, in)
	case 5:
		return r.dashboardCheck(ctx, in)
	case 6:
		return r.servicesCheck(ctx, in)
	case 7:
		if r.Workload == nil {
			return SetupCheck{}, fmt.Errorf("worker setup unavailable")
		}
		id, check, err := r.commitIdentity(ctx)
		if err != nil || !id.Set() {
			return check, err
		}
		ok, err := r.Workload.WorkersReady(ctx)
		return workloadReady(ctx, ok, "UP 7/8 workers: every worker bench at this version, its harness probed with the policy's models and its seat's provider keys, hooked on the bus and up in the store, each committing as "+id.Name+" <"+id.Email+">: VERIFIED", err)
	case 8:
		if r.Workload == nil {
			return SetupCheck{}, fmt.Errorf("smoke setup unavailable")
		}
		ok, err := r.Workload.SmokeReady(ctx)
		return workloadReady(ctx, ok, "UP 8/8 first deal: every worker row hooked, up and dealable (width from its cores); the sprint deals once started: VERIFIED", err)
	}
	return SetupCheck{}, fmt.Errorf("invalid setup step")
}

// commitIdentity is step 7's first condition: the sprint's commit identity
// (store.Identity) is set, since a worker hooks only once it is (hook
// refuses) and each job's clone takes it from the take receipt. None set is
// a question for the human, like an unchosen dashboard: up stops with the
// command that sets it, and no act can make it true.
func (r RuntimeSetup) commitIdentity(ctx context.Context) (store.Identity, SetupCheck, error) {
	if r.Snapshot == nil {
		return store.Identity{}, SetupCheck{}, fmt.Errorf("store unavailable")
	}
	snap, err := r.Snapshot.Snapshot(ctx)
	if err != nil || snap.Identity.Set() {
		return snap.Identity, SetupCheck{}, err
	}
	return snap.Identity, SetupCheck{Line: "UP 7/8 workers: the sprint has no commit identity\nASK the human which forge account on the repositories' contributor-agreement allowlist every commit is made as (its git user.name and user.email, typically its noreply address)",
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
func (r RuntimeSetup) answer(ctx context.Context, name, input, def string) (string, bool, error) {
	c := r.Prerequisites.Env.Choices
	if c.Store == nil {
		return "", false, fmt.Errorf("setup choices unavailable")
	}
	value, stored, err := c.Effective(ctx, name, input)
	if err != nil {
		return "", false, err
	}
	if value == "" {
		return def, false, nil
	}
	return value, stored, nil
}
func (r RuntimeSetup) runCheck(ctx context.Context, in SetupInput) (SetupCheck, error) {
	host, saved, err := r.answer(ctx, "run-on", in.RunOn, r.DefaultRunHost)
	if err != nil {
		return SetupCheck{}, err
	}
	if err := runHostMatches(host, r.RunService.Machine); err != nil {
		return SetupCheck{}, err
	}
	if !saved {
		return SetupCheck{Action: true}, nil
	}
	if r.Run == nil {
		return SetupCheck{}, fmt.Errorf("run readiness unavailable")
	}
	active, err := r.policyActive(ctx)
	if err != nil {
		return SetupCheck{}, err
	}
	if !active {
		return SetupCheck{Action: true, Refusal: &OperationError{Problem: "no policy version is active after storing the built-in default", Fix: "nova-sprint policy show --list", Exit: 1}}, nil
	}
	secrets := "UP 4/8 run: on " + host + "; " + runSecretsLine(r.RunService)
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
func (r RuntimeSetup) dashboardCheck(ctx context.Context, in SetupInput) (SetupCheck, error) {
	choice, saved, err := r.answer(ctx, "dashboard", in.Dash, "")
	if err != nil {
		return SetupCheck{}, err
	}
	if !saved && choice == "" {
		return SetupCheck{Line: "UP 5/8 dashboard: not chosen\nASK the human: the dashboard on the private network (default), or public on a machine with a public address in nova-config?", Next: "nova-sprint up --dash tailnet      or      nova-sprint up --dash public=<machine>[:<port>]"}, nil
	}
	target, err := r.dashboardTarget(choice)
	if err != nil {
		return SetupCheck{}, err
	}
	if !saved {
		return SetupCheck{Action: true}, nil
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
	return setupReady(ok, "UP 5/8 dashboard: "+target.URL+": VERIFIED", err)
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
func (r RuntimeSetup) servicesCheck(ctx context.Context, in SetupInput) (SetupCheck, error) {
	value, saved, err := r.answer(ctx, "services", in.Services, "")
	if err != nil {
		return SetupCheck{}, err
	}
	if value == "" {
		if declared, err := r.declared(ctx); err != nil {
			return SetupCheck{}, err
		} else if len(declared) == 0 {
			return SetupCheck{Line: "UP 6/8 services: the active policy declares no provider or bench model: declare them (nova-sprint policy show, then policy set <file>) or run up --services none", Next: "nova-sprint up --services none"}, nil
		}
		return SetupCheck{Line: "UP 6/8 services: not chosen\nASK the human which API-rate services to set up", Next: "nova-sprint up --services <names|none>"}, nil
	}
	names, chosen, err := r.services(ctx, value)
	if err != nil {
		return SetupCheck{}, err
	}
	if in.Key != "" && !slices.Contains(names, in.Key) {
		return SetupCheck{}, &OperationError{Problem: "key service is not selected", Fix: "nova-sprint up --services <names>", Exit: 1}
	}
	if !saved {
		return SetupCheck{Action: true}, nil
	}
	for _, name := range names {
		ok, err := r.Keys.Verify(ctx, name, chosen[name])
		if err != nil {
			return SetupCheck{}, err
		}
		if !ok {
			return SetupCheck{Action: true}, nil
		}
	}
	if r.Workload == nil {
		return SetupCheck{}, fmt.Errorf("service setup unavailable")
	}
	ok, err := r.Workload.ServicesReady(ctx, names)
	line := "UP 6/8 services: none selected: VERIFIED"
	if len(names) > 0 {
		line = "UP 6/8 services: account and model endpoints answered; configuration: VERIFIED"
	}
	return setupReady(ok, line, err)
}

// services is a services choice resolved against the providers the active
// policy declares: the names in order and each one's configuration; none is
// none. A name the policy does not declare, or one named twice, is refused
// naming those it does and the line that adds one.
func (r RuntimeSetup) services(ctx context.Context, value string) ([]string, map[string]policy.Provider, error) {
	if value == "none" {
		return nil, nil, nil
	}
	declared, err := r.declared(ctx)
	if err != nil {
		return nil, nil, err
	}
	names := strings.Split(value, ",")
	chosen := map[string]policy.Provider{}
	for _, name := range names {
		p, ok := declared[name]
		if _, twice := chosen[name]; !ok || twice {
			offered := strings.Join(slices.Sorted(maps.Keys(declared)), ", ")
			return nil, nil, &OperationError{Problem: fmt.Sprintf("service %q is not a provider the active policy declares, or is named twice; it declares: %s", Redact(name), cmp.Or(offered, "none")),
				Fix: "nova-sprint up --services <provider>[,...] or --services none; a provider is declared in the policy's catalog() \"providers\" (nova-sprint policy show, then " + policy.CatalogFix + ")", Exit: 1}
		}
		chosen[name] = p
	}
	slices.Sort(names)
	return names, chosen, nil
}

// declared is the providers the active policy declares; none without a
// Providers reader.
func (r RuntimeSetup) declared(ctx context.Context) (map[string]policy.Provider, error) {
	if r.Providers == nil {
		return map[string]policy.Provider{}, nil
	}
	return r.Providers(ctx)
}

func (r RuntimeSetup) Act(ctx context.Context, step int, in SetupInput) error {
	switch step {
	case 2:
		return r.storeAct(ctx, in)
	case 3:
		return r.Prerequisites.SaveCoordinator(ctx, in.Actor)
	case 4:
		return r.runAct(ctx, in)
	case 5:
		return r.dashboardAct(ctx, in)
	case 6:
		return r.servicesAct(ctx, in)
	case 7:
		return r.workersAct(ctx)
	case 8:
		if r.Workload == nil {
			return fmt.Errorf("smoke setup unavailable")
		}
		return r.Workload.RunSmoke(ctx)
	}
	return fmt.Errorf("setup action unavailable")
}
func (r RuntimeSetup) storeAct(ctx context.Context, in SetupInput) error {
	if r.Store == nil {
		return fmt.Errorf("store setup unavailable")
	}
	if in.Version != "" && r.Adoption == nil {
		return fmt.Errorf("version adoption unavailable")
	}
	ready, err := r.Store.Ready(ctx)
	if err != nil {
		return err
	}
	if !ready {
		if err = r.Store.Install(ctx); err != nil {
			return err
		}
	}
	if in.Version == "" {
		return nil
	}
	ready, err = r.Adoption.Ready(ctx, in.Version)
	if err != nil {
		return err
	}
	if ready {
		return nil
	}
	return r.Adoption.Apply(ctx, in.Version)
}

func (r RuntimeSetup) runAct(ctx context.Context, in SetupInput) error {
	if r.Run == nil {
		return fmt.Errorf("run readiness unavailable")
	}
	host, saved, err := r.answer(ctx, "run-on", in.RunOn, r.DefaultRunHost)
	if err != nil {
		return err
	}
	if err := runHostMatches(host, r.RunService.Machine); err != nil {
		return err
	}
	if !saved {
		if err = r.Prerequisites.Env.Choices.Save(ctx, "run-on", host); err != nil {
			return err
		}
	}
	if err = r.ensurePolicy(ctx, in.Actor); err != nil {
		return err
	}
	if !slices.Contains(r.RunService.Keys, sprintPasswordName) {
		return fmt.Errorf("run service lacks sprint credential")
	}
	if err = r.Role.GrantSeat(ctx, SprintSeat{Seat: r.RunService.Seat, Machine: host}); err != nil {
		return err
	}
	services := r.Services.For(in.Actor, "nova-sprint up 4/8 run")
	if _, err = services.Ensure(ctx, r.RunService); err != nil {
		return err
	}
	return services.Apply(ctx, r.RunService)
}
func (r RuntimeSetup) dashboardAct(ctx context.Context, in SetupInput) error {
	if r.Snapshot == nil {
		return fmt.Errorf("store unavailable")
	}
	choice, saved, err := r.answer(ctx, "dashboard", in.Dash, "tailnet")
	if err != nil {
		return err
	}
	target, err := r.dashboardTarget(choice)
	if err != nil {
		return err
	}
	if !saved {
		if err = r.Prerequisites.Env.Choices.Save(ctx, "dashboard", choice); err != nil {
			return err
		}
	}
	service := r.RunService
	if target.Service != nil {
		service = *target.Service
	}
	services := r.Services.For(in.Actor, "nova-sprint up 5/8 dashboard")
	if _, err = services.Ensure(ctx, service); err != nil {
		return err
	}
	return services.Apply(ctx, service)
}
func (r RuntimeSetup) servicesAct(ctx context.Context, in SetupInput) error {
	if r.Workload == nil {
		return fmt.Errorf("service setup unavailable")
	}
	value, saved, err := r.answer(ctx, "services", in.Services, "")
	if err != nil {
		return err
	}
	names, chosen, err := r.services(ctx, value)
	if err != nil {
		return err
	}
	if len(names) > 0 && (r.Keys.Terminal == nil || r.Keys.Sink == nil || r.Keys.Probe == nil) {
		return fmt.Errorf("service key setup unavailable")
	}
	if !saved {
		if err = r.Prerequisites.Env.Choices.Save(ctx, "services", value); err != nil {
			return err
		}
	}
	for _, name := range names {
		if err := r.Keys.Act(ctx, name, chosen[name]); err != nil {
			return err
		}
	}
	if r.Workload == nil {
		return fmt.Errorf("service setup unavailable")
	}
	return r.Workload.InstallServices(ctx, names)
}

func (r RuntimeSetup) workersAct(ctx context.Context) error {
	if r.Workload == nil {
		return fmt.Errorf("worker setup unavailable")
	}
	choice, found, err := r.answer(ctx, "dashboard", "", "")
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("dashboard choice unavailable")
	}
	target, err := r.dashboardTarget(choice)
	if err != nil {
		return err
	}
	publicHost := ""
	if target.Public {
		if target.Service == nil {
			return fmt.Errorf("public dashboard host unavailable")
		}
		publicHost = target.Service.Machine
	}
	machines, err := r.Services.Machines(ctx)
	if err != nil {
		return err
	}
	seats, err := SprintSeats(machines, r.RunService, publicHost)
	if err != nil {
		return err
	}
	value, found, err := r.answer(ctx, "services", "", "")
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("services choice unavailable")
	}
	_, chosen, err := r.services(ctx, value)
	if err != nil {
		return err
	}
	// A worker's seat (the bench's own, named as it) opens the keys of the
	// providers its routes name, beside the sprint credential.
	_, keys := ServiceRoutes(chosen)
	benches := WorkerBenches(machines)
	for _, seat := range seats {
		var grant []string
		if seat.Seat == seat.Machine && slices.Contains(benches, seat.Machine) {
			grant = keys
		}
		if err = r.Role.GrantSeat(ctx, seat, grant...); err != nil {
			return err
		}
	}
	return r.Workload.InstallWorkers(ctx, chosen)
}

// runHostMatches refuses a chosen run machine that is not the one the
// sprint-run service record names, naming both.
func runHostMatches(host, machine string) error {
	switch {
	case host == "":
		return &OperationError{Problem: "no run machine is chosen", Fix: "nova-sprint up --run-on <machine>", Exit: 1}
	case host != machine:
		return &OperationError{Problem: fmt.Sprintf("run machine %q is not %q, the machine the sprint-run service record names", host, machine), Fix: "nova-sprint up --run-on " + orWord(machine, "<machine>") + ", or correct the sprint-run record (nova-config service list)", Exit: 1}
	}
	return nil
}

// dashboardTarget is the configured target for choice, or why there is
// none: no target for that choice, or a public one whose service record
// is not a pull-only board without secrets.
func (r RuntimeSetup) dashboardTarget(choice string) (DashboardTarget, error) {
	target, ok := r.Dashboards[choice]
	public := strings.HasPrefix(choice, "public=")
	switch {
	case !ok:
		return target, &OperationError{Problem: edge.RedactN(fmt.Sprintf("dashboard choice %q has no configured target", choice), edge.FailureMax), Fix: "nova-sprint up --dash tailnet, or --dash public=<machine>[:<port>]", Exit: 1}
	case target.Public != public:
		return target, &OperationError{Problem: edge.RedactN(fmt.Sprintf("dashboard choice %q resolved to a public=%t target", choice, target.Public), edge.FailureMax), Fix: "nova-sprint up --dash tailnet, or --dash public=<machine>[:<port>]", Exit: 1}
	case public && !validPublicTarget(target.Service):
		machine := "(none)"
		if target.Service != nil {
			machine = target.Service.Machine
		}
		return target, &OperationError{Problem: "the public dashboard service on " + machine + " is not a pull-only nova-sprint dash without secrets", Fix: "correct or remove that service record (nova-config service list), then nova-sprint up again", Exit: 1}
	}
	return target, nil
}

func validPublicTarget(s *FleetService) bool {
	if s == nil || !s.valid() || len(s.Keys) != 0 || s.Seat != "" || len(s.Env) != 0 {
		return false
	}
	argv := s.Argv
	if len(argv) == 7 && argv[0] == "/usr/bin/env" && argv[1] == "nova-sprint" {
		argv = argv[1:]
	} else if len(argv) != 6 || argv[0] == "/usr/bin/env" {
		return false
	}
	if argv[1] != "dash" || argv[2] != "--pull" || argv[4] != "--listen" || argv[5] == "" {
		return false
	}
	u, err := url.Parse(argv[3])
	return err == nil && u.User == nil && u.Host != "" && u.RawQuery == "" && u.Fragment == "" && (u.Scheme == "http" || u.Scheme == "https")
}
