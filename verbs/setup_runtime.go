package verbs

import (
	"context"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/store"
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

// SetupWorkload is supplied by layer 4 for grants, rows, workers and smoke.
// WorkersReady proves every configured worker is hooked and up; SmokeReady
// proves one work and read were dealt and released within one tick.
type SetupWorkload interface {
	ServicesReady(context.Context, []string) (bool, error)
	InstallServices(context.Context, []string) error
	WorkersReady(context.Context) (bool, error)
	InstallWorkers(context.Context) error
	SmokeReady(context.Context) (bool, error)
	RunSmoke(context.Context) error
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
	Now         func() time.Time // nil: time.Now
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
		return setupReady(ok, fmt.Sprintf("UP 2/8 store: schema sprint version %d, backup and version shadow: VERIFIED", store.SchemaVersion), err)
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
		ok, err := r.Workload.WorkersReady(ctx)
		return setupReady(ok, "UP 7/8 workers: hooked and up: VERIFIED", err)
	case 8:
		if r.Workload == nil {
			return SetupCheck{}, fmt.Errorf("smoke setup unavailable")
		}
		ok, err := r.Workload.SmokeReady(ctx)
		return setupReady(ok, "UP 8/8 first deal: work and read dealt and released within one tick: VERIFIED", err)
	}
	return SetupCheck{}, fmt.Errorf("invalid setup step")
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
	value, found, err := c.Get(ctx, name)
	if err != nil {
		return "", false, err
	}
	if found {
		return value, true, nil
	}
	if input != "" {
		return input, false, nil
	}
	return def, false, nil
}
func (r RuntimeSetup) runCheck(ctx context.Context, in SetupInput) (SetupCheck, error) {
	host, saved, err := r.answer(ctx, "run-on", in.RunOn, r.DefaultRunHost)
	if err != nil {
		return SetupCheck{}, err
	}
	if host == "" || host != r.RunService.Machine {
		return SetupCheck{}, fmt.Errorf("run host configuration unavailable")
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
	var notReady *RunNotReady
	if err != nil && ctx.Err() == nil && errors.As(err, &notReady) {
		// Not up yet: act; when it still is not after the act, the
		// refusal names each false condition, never "not verified".
		return SetupCheck{Action: true, Line: secrets, Refusal: &OperationError{Problem: edge.RedactN("run on "+host+": "+notReady.Why, edge.FailureMax+200), Fix: notReady.Fix, Exit: 1}}, nil
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
	target, ok := r.Dashboards[choice]
	if !ok || target.Public != strings.HasPrefix(choice, "public=") || (target.Public && !validPublicTarget(target.Service)) {
		return SetupCheck{}, fmt.Errorf("dashboard configuration unavailable")
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
	ok, err = r.Dashboard.Verify(ctx, target.URL, snap.Epoch, target.Public)
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
		return SetupCheck{Line: "UP 6/8 services: not chosen\nASK the human which API-rate services to set up", Next: "nova-sprint up --services <names|none>"}, nil
	}
	names, err := setupServices(value)
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
		ok, err := r.Keys.Verify(ctx, name)
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
func setupServices(value string) ([]string, error) {
	if value == "none" {
		return nil, nil
	}
	names := strings.Split(value, ",")
	seen := map[string]bool{}
	for _, name := range names {
		if _, ok := modelProbeProviders[name]; !ok || seen[name] {
			return nil, fmt.Errorf("unsupported or duplicate service")
		}
		seen[name] = true
	}
	slices.Sort(names)
	return names, nil
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
	if host == "" || host != r.RunService.Machine {
		return fmt.Errorf("run host configuration unavailable")
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
	target, ok := r.Dashboards[choice]
	if !ok || target.Public != strings.HasPrefix(choice, "public=") || (target.Public && !validPublicTarget(target.Service)) {
		return fmt.Errorf("dashboard configuration unavailable")
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
	names, err := setupServices(value)
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
		if err := r.Keys.Act(ctx, name); err != nil {
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
	target, ok := r.Dashboards[choice]
	if !ok || target.Public != strings.HasPrefix(choice, "public=") || (target.Public && !validPublicTarget(target.Service)) {
		return fmt.Errorf("dashboard configuration unavailable")
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
	for _, seat := range seats {
		if err = r.Role.GrantSeat(ctx, seat); err != nil {
			return err
		}
	}
	return r.Workload.InstallWorkers(ctx)
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
