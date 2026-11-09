package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/run"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

// productionSetupConfig contains only public locations and credential values
// already injected into this process by nova-secrets; it is never printed.
type productionSetupConfig struct {
	Binary, BinaryVersion, Host, DSN, SprintPassword, BusUser, BusRedis string
	Secrets                                                             verbs.NovaSecrets
}

func productionSetupConfigFromEnv() (productionSetupConfig, error) {
	binary, err := os.Executable()
	if err != nil {
		return productionSetupConfig{}, edge.Cause("cannot locate nova-sprint", err)
	}
	if !filepath.IsAbs(binary) {
		return productionSetupConfig{}, fmt.Errorf("cannot locate nova-sprint: %q is not absolute", edge.Redact(binary))
	}
	host, err := os.Hostname()
	if err != nil {
		return productionSetupConfig{}, edge.Cause("cannot determine control machine", err)
	}
	host = strings.SplitN(host, ".", 2)[0]
	seat := os.Getenv("NOVA_SECRETS_SEAT")
	if seat == "" {
		seat = host
	}
	return productionSetupConfig{Binary: binary, BinaryVersion: versionString(), Host: host, DSN: os.Getenv("NOVA_PG_DSN"), SprintPassword: os.Getenv("NOVA_SPRINT_PG_PASSWORD"), BusUser: os.Getenv("NOVA_BUS_USER"), BusRedis: os.Getenv("NOVA_BUS_REDIS"), Secrets: verbs.NovaSecrets{Store: os.Getenv("NOVA_SECRETS_STORE"), KeyFile: os.Getenv("NOVA_SECRETS_KEY"), Pins: os.Getenv("NOVA_SECRETS_PINS"), Seat: seat}}, nil
}

// buildProductionSetup is called only by production CLI composition. It does
// no fleet, network, database or secret-store work while building dependencies.
func buildProductionSetup(st store.Store, env verbs.ExecEnvironment, pin string) verbs.Setup {
	cfg, err := productionSetupConfigFromEnv()
	if err != nil {
		return failedProductionSetup{err}
	}
	return makeProductionSetup(st, env, pin, cfg)
}
func makeProductionSetup(st store.Store, env verbs.ExecEnvironment, pin string, cfg productionSetupConfig) verbs.Setup {
	command := setupCommandWithBus(cfg, productionFleetCommand(cfg))
	env.Command = command
	secret := cfg.Secrets
	grants := verbs.FleetGrants{Store: secret.Store, KeyFile: secret.KeyFile, Pins: secret.Pins, ControlSeat: secret.Seat, Command: command}
	role := verbs.SprintRoleBootstrap{Sink: secret, Grants: grants, Secrets: secret, Binary: cfg.Binary}
	schema := verbs.PGSetupStore{Role: role, ControlMachine: cfg.Host, Secrets: secret, Binary: cfg.Binary}
	runDSN, err := setupRoleDSN(cfg.DSN, "nova_sprint")
	if err != nil && cfg.DSN != "" { // unset, the store step says so; set and malformed, nothing can run
		return failedProductionSetup{verbs.RefusalOr(err, &verbs.OperationError{Problem: "NOVA_PG_DSN cannot be used (" + err.Error() + ")", Fix: "export NOVA_PG_DSN=postgres://<host>:<port>/<database> with no password in it, then nova-sprint up again", Exit: 1})}
	}
	runtime := verbs.RuntimeSetup{Prerequisites: verbs.SetupPrerequisites{Env: env, Pin: pin}, Store: schema, Run: verbs.PGSetupRun{DSN: runDSN, Password: cfg.SprintPassword, Env: env}, Services: verbs.FleetServices{Command: command}, Role: role, Snapshot: st, PolicyStore: st}
	return productionSetup{runtime: runtime, cfg: cfg, command: command}
}

// Every seam the engine type-asserts on e.Setup, and the workload the
// runtime calls, is checked here at build time: a missing method is a
// build failure, never a refusal on a real fleet.
var (
	_ verbs.Setup           = productionSetup{}
	_ verbs.SetupCompletion = productionSetup{}
	_ verbs.SetupWorkload   = productionWorkload{}
	_ verbs.Setup           = failedProductionSetup{}
)

// Completion is the walk's UP DONE line, from the configured runtime.
func (p productionSetup) Completion(ctx context.Context, in verbs.SetupInput) (string, error) {
	r, _, err := p.configuredRuntime(ctx, in)
	if err != nil {
		return "", err
	}
	return r.Completion(ctx, in)
}

type failedProductionSetup struct{ err error }

func (f failedProductionSetup) Verify(context.Context, int, verbs.SetupInput) (verbs.SetupCheck, error) {
	return verbs.SetupCheck{}, f.err
}
func (f failedProductionSetup) Act(context.Context, int, verbs.SetupInput) error { return f.err }

type productionSetup struct {
	runtime verbs.RuntimeSetup
	cfg     productionSetupConfig
	command verbs.CommandOutput
	resolve func(context.Context, string) ([]netip.Addr, error) // nil: the system resolver (MagicDNS)
}

// Verify is step 1 and 2 on the runtime as built; from step 4, on the
// runtime configured from nova-config's records, with this release checked
// where the step runs nova-sprint (releaseMachines).
func (p productionSetup) Verify(ctx context.Context, step int, in verbs.SetupInput) (verbs.SetupCheck, error) {
	if step == 2 {
		if err := p.configError(); err != nil {
			return verbs.SetupCheck{}, err
		}
	}
	if step < 4 {
		return p.runtime.Verify(ctx, step, in)
	}
	if p.cfg.SprintPassword == "" {
		return verbs.SetupCheck{}, &verbs.OperationError{Problem: "this process has no sprint credential (step " + strconv.Itoa(step) + " reads the store)", Fix: "nova-sprint up --as " + in.Actor, Exit: 1}
	}
	r, machines, err := p.configuredRuntime(ctx, in)
	if err != nil {
		return verbs.SetupCheck{}, err
	}
	check, err := r.Verify(ctx, step, in)
	if err != nil || (!check.Verified && !check.Action) {
		return check, err // a question for the human, or a refusal: no release act first
	}
	err = p.release(ctx, releaseMachines(step, r, machines), false)
	var refusal *verbs.OperationError
	if errors.As(err, &refusal) && ctx.Err() == nil {
		return verbs.SetupCheck{Action: true, Line: check.Line, Refusal: refusal}, nil
	}
	return check, err
}

// Act installs this release where the step runs nova-sprint, then acts.
func (p productionSetup) Act(ctx context.Context, step int, in verbs.SetupInput) error {
	if step == 2 {
		if err := p.configError(); err != nil {
			return err
		}
		if err := p.checkControlMachine(ctx); err != nil {
			return err
		}
	}
	if step < 4 {
		return p.runtime.Act(ctx, step, in)
	}
	r, machines, err := p.configuredRuntime(ctx, in)
	if err != nil {
		return err
	}
	if err = p.release(ctx, releaseMachines(step, r, machines), true); err != nil {
		return err
	}
	return r.Act(ctx, step, in)
}

// releaseMachines is where step runs nova-sprint: the run machine (4), the
// public board's machine (5), every worker bench (6).
func releaseMachines(step int, r verbs.RuntimeSetup, machines []verbs.FleetMachine) []string {
	switch {
	case step == 4:
		return []string{r.RunService.Machine}
	case step == 5 && r.Board.Public && r.Board.Service != nil:
		return []string{r.Board.Service.Machine}
	case step == 6:
		return verbs.WorkerBenches(machines)
	}
	return nil
}

func (p productionSetup) configError() error {
	s := p.cfg.Secrets
	if !filepath.IsAbs(s.Store) || !filepath.IsAbs(s.KeyFile) || !filepath.IsAbs(s.Pins) || s.Seat == "" {
		return &verbs.OperationError{Problem: "control-seat secrets configuration is incomplete", Fix: "set NOVA_SECRETS_STORE, NOVA_SECRETS_KEY, NOVA_SECRETS_PINS and NOVA_SECRETS_SEAT; run nova-secrets -h", Exit: 1}
	}
	if s.Seat != p.cfg.Host {
		return &verbs.OperationError{Problem: "control seat must match the inventory machine name", Fix: "use the default machine seat and run nova-sprint up again", Exit: 1}
	}
	return nil
}
func (p productionSetup) checkControlMachine(ctx context.Context) error {
	machines, err := (verbs.FleetServices{Command: p.command}).Machines(ctx)
	if err != nil {
		return verbs.RefusalOr(err, &verbs.OperationError{Problem: "cannot inspect the control machine inventory: " + edge.RedactN(err.Error(), edge.CommandMax), Fix: "fix what nova-config said, then nova-sprint up again", Exit: 2})
	}
	for _, machine := range machines {
		if machine.Name == p.cfg.Host {
			return nil
		}
	}
	return &verbs.OperationError{Problem: "control machine is absent from inventory", Fix: "run nova-config machine list --json and nova-fleet check", Exit: 1}
}

func setupRoleDSN(dsn, role string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", edge.Cause("invalid database URL", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("invalid database URL: scheme %q is not postgres or postgresql", edge.Redact(u.Scheme))
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid database URL: no host")
	}
	if u.User != nil {
		if _, ok := u.User.Password(); ok {
			return "", fmt.Errorf("password in database URL")
		}
	}
	for key := range u.Query() {
		if strings.EqualFold(key, "password") || strings.EqualFold(key, "user") {
			return "", fmt.Errorf("invalid database URL: query parameter %q is not allowed", key)
		}
	}
	u.User = url.User(role)
	return u.String(), nil
}

// setupServiceRow is the public nova-config service-list JSON subset.
type setupServiceRow struct {
	Name, Machine, Seat, Keys, Log string
	Argv                           []string
	Every                          int
	Keepalive, Enabled             bool
	Env                            map[string]string
}
type setupServicesResponse struct {
	Result struct {
		Verb, Status string
		Exit         int
	} `json:"result"`
	Facts struct {
		Kind string
		Rows int
	} `json:"facts"`
	Items []struct {
		Kind   string          `json:"kind"`
		Fields setupServiceRow `json:"fields"`
	} `json:"items"`
}

func (p productionSetup) listServices(ctx context.Context) ([]setupServiceRow, error) {
	call := p.command
	if call == nil {
		call = verbs.Output
	}
	out, err := call(ctx, "nova-config", "service", "list", "--json")
	if err != nil {
		return nil, serviceConfigFailure("cannot inspect fleet services: ", err)
	}
	if len(out) > 1<<20 {
		return nil, serviceConfigError("cannot inspect fleet services: " + edge.ParseRefused("output longer than 1 MiB", 1<<20, out))
	}
	var response setupServicesResponse
	if err := json.Unmarshal(out, &response); err != nil {
		return nil, serviceConfigError("fleet service response is invalid: " + edge.ParseFailure(err, out))
	}
	if response.Result.Verb != "service list" || response.Result.Status != "ok" || response.Result.Exit != 0 || response.Facts.Kind != "service" || response.Facts.Rows != len(response.Items) {
		return nil, serviceConfigError(fmt.Sprintf("fleet service response is invalid: result %q %q exit %d, facts kind %q rows %d for %d items", response.Result.Verb, response.Result.Status, response.Result.Exit, response.Facts.Kind, response.Facts.Rows, len(response.Items)))
	}
	rows := make([]setupServiceRow, 0, len(response.Items))
	seen := map[string]bool{}
	for i, item := range response.Items {
		if item.Kind != "service" || item.Fields.Name == "" || seen[item.Fields.Name] {
			return nil, serviceConfigError(fmt.Sprintf("fleet service response is invalid: item %d kind %q name %q (empty or repeated)", i, item.Kind, item.Fields.Name))
		}
		seen[item.Fields.Name] = true
		rows = append(rows, item.Fields)
	}
	return rows, nil
}

// serviceConfigFailure is err when it already is a refusal with its own
// problem and fix (errors.As), else a service configuration refusal
// carrying err's text after what.
func serviceConfigFailure(what string, err error) error {
	var op *verbs.OperationError
	if errors.As(err, &op) {
		return err
	}
	return serviceConfigError(what + err.Error())
}
func serviceConfigError(problem string) error {
	return &verbs.OperationError{Problem: edge.RedactN(problem, edge.FailureMax), Fix: "correct the service records nova-config holds (nova-fleet help services)", Exit: 1}
}

// configuredRuntime is the runtime for steps 4 to 7: the run service on
// this machine, from nova-config's record or built from the inventory, and
// the board (configureDashboard); with the machine inventory it read.
func (p productionSetup) configuredRuntime(ctx context.Context, in verbs.SetupInput) (verbs.RuntimeSetup, []verbs.FleetMachine, error) {
	r := p.runtime
	host := p.cfg.Host
	machines, err := (verbs.FleetServices{Command: p.command}).Machines(ctx)
	if err != nil {
		return r, nil, serviceConfigFailure("cannot inspect machine inventory: ", err)
	}
	if !slices.ContainsFunc(machines, func(m verbs.FleetMachine) bool { return m.Name == host && slices.Contains(m.Roles, "services") }) {
		return r, nil, serviceConfigError("this machine (" + host + ") is absent from the inventory or lacks the services role; up runs the sprint on the machine it runs on")
	}
	rows, err := p.listServices(ctx)
	if err != nil {
		return r, nil, err
	}
	run, err := p.resolveRunService(ctx, host, rows, machines)
	if err != nil {
		return r, nil, err
	}
	if err := p.validateRunService(run); err != nil {
		return r, nil, err
	}
	expectedDSN, err := setupRoleDSN(p.cfg.DSN, "nova_sprint")
	if err != nil {
		return r, nil, verbs.RefusalOr(err, serviceConfigError("sprint-run must name the configured passwordless sprint DSN: the configured DSN: "+err.Error()))
	}
	if run.Env["NOVA_PG_DSN"] != expectedDSN {
		return r, nil, serviceConfigError("sprint-run must name the configured passwordless sprint DSN")
	}
	r.RunService = run
	r.Services = verbs.FleetServices{Command: p.command}
	r.Workload = productionWorkload{services: r.Services.For(in.Actor, "nova-sprint up 6/7 workers"), run: run, store: r.Snapshot, present: r.Prerequisites.Env.Present}
	r.Board, err = dashboardTarget(in.Dash, run, machines, rows)
	return r, machines, err
}

func (p productionSetup) validateRunService(run verbs.FleetService) error {
	if run.Machine != p.cfg.Host || run.Seat != p.cfg.Host {
		return serviceConfigError("sprint-run must use the local machine's default seat")
	}
	if _, err := privateDashboardURL(run.Argv[3]); err != nil {
		return err
	}
	if p.cfg.BusRedis == "" || run.Env["NOVA_BUS_REDIS"] != p.cfg.BusRedis || run.Env["NOVA_BUS_USER"] != p.cfg.BusUser {
		return serviceConfigError("sprint-run must use the configured Redis endpoint and user")
	}
	expected := []string{"NOVA_SPRINT_PG_PASSWORD"}
	if p.cfg.BusUser != "" {
		expected = append(expected, "NOVA_BUS_PASSWORD")
	}
	actual := append([]string(nil), run.Keys...)
	slices.Sort(expected)
	slices.Sort(actual)
	if !slices.Equal(actual, expected) {
		return serviceConfigError("sprint-run has unexpected credential names")
	}
	return nil
}

func (p productionSetup) resolveRunService(ctx context.Context, host string, rows []setupServiceRow, machines []verbs.FleetMachine) (verbs.FleetService, error) {
	if run, ok := selectRunService(rows, host); ok {
		return run, nil
	}
	for _, row := range rows {
		if row.Name == "sprint-run" {
			return verbs.FleetService{}, serviceConfigError("the sprint-run service record conflicts with setup")
		}
	}
	return p.newRunService(ctx, host, machines)
}

func (p productionSetup) newRunService(ctx context.Context, host string, machines []verbs.FleetMachine) (verbs.FleetService, error) {
	if host != p.cfg.Host || p.cfg.Secrets.Seat == "" {
		return verbs.FleetService{}, serviceConfigError("run host needs an existing service record with its actual seat")
	}
	address := ""
	for _, machine := range machines {
		if machine.Name == host {
			address = p.runAddress(ctx, machine.Address)
		}
	}
	if address == "" {
		return verbs.FleetService{}, serviceConfigError("run host needs a private-network address (a tailnet IP or name) in the inventory")
	}
	busHost, busPort, err := net.SplitHostPort(p.cfg.BusRedis)
	if err != nil {
		return verbs.FleetService{}, verbs.RefusalOr(err, serviceConfigError("NOVA_BUS_REDIS must name the configured Redis host and port: "+err.Error()))
	}
	port, err := strconv.Atoi(busPort)
	if err != nil {
		return verbs.FleetService{}, verbs.RefusalOr(err, serviceConfigError("NOVA_BUS_REDIS must name the configured Redis host and port: "+err.Error()))
	}
	if busHost == "" || port < 1 || port > 65535 {
		return verbs.FleetService{}, serviceConfigError(fmt.Sprintf("NOVA_BUS_REDIS must name the configured Redis host and port (host %q, port %d)", busHost, port))
	}
	dsn, err := setupRoleDSN(p.cfg.DSN, "nova_sprint")
	if err != nil {
		return verbs.FleetService{}, verbs.RefusalOr(err, serviceConfigError("configured sprint DSN is invalid: "+err.Error()))
	}
	// run records the chooser lock under the machine's nova-config name,
	// handed here, never under the raw hostname (run.ResolveMachine).
	env := map[string]string{"NOVA_PG_DSN": dsn, "NOVA_PG_PASSWORD_ENV": "NOVA_SPRINT_PG_PASSWORD", "NOVA_BUS_REDIS": p.cfg.BusRedis, run.MachineEnv: host}
	// run's forge and git act as the host's gh identity (intent rule 32);
	// no GitHub token is required of the service.
	keys := []string{"NOVA_SPRINT_PG_PASSWORD"}
	if p.cfg.BusUser != "" {
		env["NOVA_BUS_USER"] = p.cfg.BusUser
		keys = append(keys, "NOVA_BUS_PASSWORD")
	}
	return verbs.FleetService{Name: "sprint-run", Machine: host, Seat: p.cfg.Secrets.Seat, Argv: []string{"nova-sprint", "run", "--listen", net.JoinHostPort(address, "8080")}, Keys: keys, Env: env}, nil
}

// dashboardTarget is the board: --dash when given, else the public board
// nova-config's sprint-dashboard record already names, else the tailnet
// board run serves itself. A --dash tailnet beside a sprint-dashboard
// record is refused: the record is the answer until it is removed.
func dashboardTarget(dash string, run verbs.FleetService, machines []verbs.FleetMachine, rows []setupServiceRow) (verbs.DashboardTarget, error) {
	privateURL, err := privateDashboardURL(run.Argv[3])
	if err != nil {
		return verbs.DashboardTarget{}, err
	}
	recorded := ""
	for _, row := range rows {
		if row.Name == "sprint-dashboard" {
			recorded = row.Machine
		}
	}
	switch {
	case dash == "tailnet" && recorded != "":
		return verbs.DashboardTarget{}, &verbs.OperationError{Problem: "nova-config's sprint-dashboard record puts the board on " + recorded + ", public", Fix: "nova-config service rm sprint-dashboard --actor <you> --reason <why>, then nova-sprint up --dash tailnet; or nova-sprint up --dash public=" + recorded, Exit: 1}
	case dash == "" && recorded != "":
		dash = "public=" + recorded
	case dash == "":
		dash = "tailnet"
	}
	if chosen, public := strings.CutPrefix(dash, "public="); public {
		return publicDashboardTarget(chosen, run.Machine, machines, rows, privateURL)
	}
	return verbs.DashboardTarget{URL: privateURL}, nil
}

// release checks (nova-fleet install --check) or, with install, installs
// this binary's release of nova-sprint on machines. A check whose recap
// would change a machine, or does not name it, is a refusal naming the
// install; a build that is not a release can be neither.
func (p productionSetup) release(ctx context.Context, machines []string, install bool) error {
	if len(machines) == 0 {
		return nil
	}
	want, names := p.cfg.BinaryVersion, strings.Join(machines, " ")
	fix := "nova-fleet install --repo mas-bandwidth/nova-sprint --tag " + orWord(want, "<v>") + " --names nova-sprint " + names + ", then nova-sprint up again"
	if !publicRelease.MatchString(want) {
		return &verbs.OperationError{Problem: edge.RedactN("this nova-sprint is "+orWord(want, "unversioned")+", not a release: up installs and checks only a release on "+names, edge.FailureMax), Fix: "run up with a release's nova-sprint: " + fix, Exit: 1}
	}
	args := append([]string{"install", "--check", "--repo", "mas-bandwidth/nova-sprint", "--tag", want, "--names", "nova-sprint"}, machines...)
	if install {
		args = append(args[:1], args[2:]...)
	}
	out, err := p.command(ctx, "nova-fleet", args...)
	if err != nil {
		return verbs.RefusalOr(err, &verbs.OperationError{Problem: edge.RedactN("nova-fleet "+strings.Join(args, " ")+" failed: "+err.Error(), edge.FailureMax), Fix: fix, Exit: 1})
	}
	if drift := releaseDrift(out, machines); !install && drift != "" {
		return &verbs.OperationError{Problem: "nova-sprint " + want + " is not what " + drift + " runs (nova-fleet install --check would change it)", Fix: fix, Exit: 1}
	}
	return nil
}

// publicRelease is a release tag nova-fleet install can fetch.
var publicRelease = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

// recapLine is one host's line of the play recap ansible prints last.
var recapLine = regexp.MustCompile(`(?m)^(\S+)\s+:\s+ok=\d+\s+changed=(\d+)\s+unreachable=(\d+)\s+failed=(\d+)`)

// releaseDrift is the machines of a --check run's recap that would change,
// are unreachable or failed, or are not in it: "" when every one is at the
// release.
func releaseDrift(out []byte, machines []string) string {
	clean := map[string]bool{}
	for _, m := range recapLine.FindAllStringSubmatch(string(out), -1) {
		clean[m[1]] = m[2] == "0" && m[3] == "0" && m[4] == "0"
	}
	var drift []string
	for _, machine := range machines {
		if !clean[machine] {
			drift = append(drift, machine)
		}
	}
	return strings.Join(drift, ", ")
}

// sshDestination is the inventory's [user@]address for machine.
func (p productionSetup) sshDestination(ctx context.Context, machine string) (string, error) {
	data, err := p.smallCommand(ctx, "nova-config", "inventory", "--host", machine)
	if err != nil {
		return "", fmt.Errorf("nova-config inventory --host %s: %s", machine, err.Error())
	}
	var inventory struct {
		Host string `json:"ansible_host"`
		User string `json:"ansible_user"`
	}
	if err = json.Unmarshal(data, &inventory); err != nil {
		return "", fmt.Errorf("nova-config inventory --host %s: %s", machine, edge.ParseFailure(err, data))
	}
	if !safeSSHName(inventory.Host) || (inventory.User != "" && (!safeSSHName(inventory.User) || strings.Contains(inventory.User, ":"))) {
		return "", fmt.Errorf("nova-config inventory --host %s: ansible_host %q or ansible_user %q is not a safe ssh name", machine, inventory.Host, inventory.User)
	}
	if inventory.User != "" {
		return inventory.User + "@" + inventory.Host, nil
	}
	return inventory.Host, nil
}
func orWord(s, or string) string {
	if s == "" {
		return or
	}
	return s
}

func safeSSHName(value string) bool {
	return value != "" && !strings.HasPrefix(value, "-") && !strings.ContainsAny(value, "@/\\ \t\r\n") && regexp.MustCompile(`^[A-Za-z0-9._:-]+$`).MatchString(value)
}

func (p productionSetup) smallCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	if p.command != nil {
		return p.command(ctx, name, args...)
	}
	return runSetupCommand(ctx, true, name, args...)
}

func runSetupCommand(ctx context.Context, clean bool, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if clean {
		cmd.Env = setupCommandEnv(name, os.Environ())
	}
	if name == "ssh" || (clean && name == "nova-secrets") {
		if socket := os.Getenv("SSH_AUTH_SOCK"); socket != "" {
			cmd.Env = append(cmd.Env, "SSH_AUTH_SOCK="+socket)
		}
	}
	var out boundedOutput
	if err := verbs.OutputWith(cmd, &out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// baseEnv is what every setup tool child gets: the paths, the locale and
// the store's location, never a credential.
func baseEnv(env []string) []string {
	allowed := map[string]bool{"PATH": true, "HOME": true, "LANG": true, "LC_ALL": true, "TZ": true, "TMPDIR": true, "NOVA_PG_DSN": true, "SSL_CERT_DIR": true, "SSL_CERT_FILE": true}
	result := []string{}
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if ok && allowed[name] {
			result = append(result, entry)
		}
	}
	return result
}

func setupCommandEnv(name string, env []string) []string {
	result := baseEnv(env)
	allowed := map[string]bool{}
	if name == "nova-bus" {
		allowed = map[string]bool{"NOVA_BUS_REDIS": true, "NOVA_BUS_USER": true, "NOVA_BUS_PASSWORD": true, "CODEX_THREAD_ID": true}
	} else if name == "nova-fleet" {
		allowed = map[string]bool{"NOVA_SECRETS_STORE": true, "NOVA_SECRETS_KEY": true, "NOVA_SECRETS_PINS": true, "NOVA_SECRETS_SEAT": true}
	}
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && allowed[key] {
			result = append(result, entry)
		}
	}
	return result
}

func productionFleetCommand(cfg productionSetupConfig) verbs.CommandOutput {
	return productionFleetCommandWith(cfg, runSetupCommand)
}
func productionFleetCommandWith(cfg productionSetupConfig, run func(context.Context, bool, string, ...string) ([]byte, error)) verbs.CommandOutput {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		wrapped := name == "nova-config" || (name == "nova-fleet" && len(args) > 0 && args[0] != "check")
		if !wrapped {
			return run(ctx, true, name, args...)
		}
		dsn, err := setupRoleDSN(cfg.DSN, "nova_config")
		if err != nil {
			return nil, edge.Cause("nova-config DSN unavailable", err)
		}
		s := cfg.Secrets
		if err = s.ExecReady(); err != nil {
			return nil, err
		}
		prefix := []string{"exec", "--only", "NOVA_PG_PASSWORD", "--store", s.Store, "--key", s.KeyFile, "--as", s.Seat, "--", "env", "NOVA_PG_DSN=" + dsn, "NOVA_PG_PASSWORD_ENV=NOVA_PG_PASSWORD", name}
		return run(ctx, true, "nova-secrets", append(prefix, args...)...)
	}
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(data []byte) (int, error) {
	if b.Len()+len(data) > 1<<20 {
		return 0, fmt.Errorf("command output exceeds limit of %d bytes", 1<<20)
	}
	return b.Buffer.Write(data)
}
func selectRunService(rows []setupServiceRow, host string) (verbs.FleetService, bool) {
	for _, row := range rows {
		if row.Name != "sprint-run" {
			continue
		}
		if row.Machine != host || row.Seat == "" || row.Every != 0 || !row.Keepalive || !row.Enabled || row.Log != "" || len(row.Argv) != 4 || (row.Argv[0] != "nova-sprint" && !filepath.IsAbs(row.Argv[0])) || row.Argv[1] != "run" || row.Argv[2] != "--listen" || row.Argv[3] == "" {
			return verbs.FleetService{}, false
		}
		for key := range row.Env {
			if key != "NOVA_PG_DSN" && key != "NOVA_BUS_REDIS" && key != "NOVA_BUS_USER" && key != "NOVA_PG_PASSWORD_ENV" && key != run.MachineEnv {
				return verbs.FleetService{}, false
			}
		}
		if value, ok := row.Env[run.MachineEnv]; ok && value != host {
			return verbs.FleetService{}, false
		}
		if value, ok := row.Env["NOVA_PG_PASSWORD_ENV"]; ok && value != "NOVA_SPRINT_PG_PASSWORD" {
			return verbs.FleetService{}, false
		}
		busHost, busPort, err := net.SplitHostPort(row.Env["NOVA_BUS_REDIS"])
		portNumber, perr := strconv.Atoi(busPort)
		if err != nil || perr != nil || busHost == "" || portNumber < 1 || portNumber > 65535 {
			return verbs.FleetService{}, false
		}
		keys := splitServiceKeys(row.Keys)
		if !slices.Contains(keys, "NOVA_SPRINT_PG_PASSWORD") || slices.Contains(keys, "NOVA_PG_ADMIN_PASSWORD") {
			return verbs.FleetService{}, false
		}
		return verbs.FleetService{Name: row.Name, Machine: row.Machine, Seat: row.Seat, Argv: row.Argv, Keys: keys, Env: row.Env}, true
	}
	return verbs.FleetService{}, false
}
func splitServiceKeys(raw string) []string {
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}
func privateDashboardURL(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", verbs.RefusalOr(err, serviceConfigError("sprint-run needs an explicit private-network listener: "+err.Error()))
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		return "", verbs.RefusalOr(err, serviceConfigError("sprint-run needs an explicit private-network listener: "+err.Error()))
	}
	if number < 1 || number > 65535 {
		return "", serviceConfigError(fmt.Sprintf("sprint-run needs an explicit private-network listener: port %d is out of range", number))
	}
	if ip, err := netip.ParseAddr(host); err == nil && isTailnetIP(ip) || err != nil && tailnetName.MatchString(host) {
		return "http://" + net.JoinHostPort(host, port), nil
	}
	return "", serviceConfigError("sprint-run must listen on a private-network address (a tailnet IP or name)")
}

// tailnetName is a MagicDNS name, <host>.<tailnet>.ts.net: private, as a
// 100.64.0.0/10 address is.
var tailnetName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+\.ts\.net$`)

// runAddress is the run host's inventory address as run listens on it: a
// tailnet IP as is; a MagicDNS name resolved to its tailnet IP (IPv4
// first), or the name itself when it does not resolve here; anything else,
// or a name that resolves only off the tailnet, "" (not private).
func (p productionSetup) runAddress(ctx context.Context, address string) string {
	if ip, err := netip.ParseAddr(address); err == nil {
		if isTailnetIP(ip) {
			return ip.String()
		}
		return ""
	}
	name := strings.ToLower(strings.TrimSuffix(address, "."))
	if !tailnetName.MatchString(name) {
		return ""
	}
	resolve := p.resolve
	if resolve == nil {
		resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	addrs, err := resolve(ctx, name)
	if err != nil || len(addrs) == 0 {
		return name // MagicDNS answers on the tailnet; run resolves it where it runs
	}
	for _, four := range []bool{true, false} {
		for _, a := range addrs {
			if a = a.Unmap(); a.Is4() == four && isTailnetIP(a) {
				return a.String()
			}
		}
	}
	return "" // a tailnet name that resolves off the tailnet is not private
}
func isTailnetIP(ip netip.Addr) bool {
	return netip.MustParsePrefix("100.64.0.0/10").Contains(ip) || netip.MustParsePrefix("fd7a:115c:a1e0::/48").Contains(ip)
}

// publicDashboardTarget is the board for --dash public=<machine>[:<port>]:
// the machine by its nova-config name, its public address (nova-config's
// optional machine field "public"; fleet addresses are MagicDNS names by
// design), and the URL the board answers on: <port> when given (a fleet
// that fronts public services on 80), else the service's own listener. The
// service keeps listening on its own port (:8080) behind any front.
func publicDashboardTarget(chosen, runMachine string, machines []verbs.FleetMachine, rows []setupServiceRow, privateURL string) (verbs.DashboardTarget, error) {
	name, front, err := splitPublicChoice(chosen)
	if err != nil {
		return verbs.DashboardTarget{}, err
	}
	var machine *verbs.FleetMachine
	for i := range machines {
		if machines[i].Name == name {
			machine = &machines[i]
		}
	}
	switch {
	case machine == nil:
		return verbs.DashboardTarget{}, &verbs.OperationError{Problem: "no machine named " + name + " in nova-config", Fix: "nova-config machine list", Exit: 1}
	case run.SameMachine(machine.Name, runMachine):
		return verbs.DashboardTarget{}, &verbs.OperationError{Problem: machine.Name + " runs the sprint; the public dashboard goes on another machine", Fix: "--dash public=<other>", Exit: 1}
	case machine.Public == "":
		return verbs.DashboardTarget{}, &verbs.OperationError{Problem: "machine " + machine.Name + " has no public address in nova-config", Fix: "nova-config machine set " + machine.Name + " --public <ip> --actor <you> --reason <why>", Exit: 1}
	}
	publicFix := "nova-config machine set " + machine.Name + " --public <ip> --actor <you> --reason <why>"
	publicIP, err := netip.ParseAddr(machine.Public)
	if err != nil {
		return verbs.DashboardTarget{}, &verbs.OperationError{Problem: edge.RedactN(fmt.Sprintf("machine %s's public address %q in nova-config is not an IP: %s", machine.Name, machine.Public, edge.ParseFailure(err, []byte(machine.Public))), edge.FailureMax), Fix: publicFix, Exit: 1}
	}
	if !publicIP.IsGlobalUnicast() || publicIP.IsPrivate() || isTailnetIP(publicIP) {
		return verbs.DashboardTarget{}, &verbs.OperationError{Problem: edge.RedactN(fmt.Sprintf("machine %s's public address %q in nova-config is not a public IP", machine.Name, machine.Public), edge.FailureMax), Fix: publicFix, Exit: 1}
	}
	var candidate *verbs.FleetService
	port := ""
	for _, row := range rows {
		if row.Name == "sprint-dashboard" && row.Machine != machine.Name {
			return verbs.DashboardTarget{}, serviceConfigError("sprint-dashboard conflicts with the selected host")
		}
		if row.Machine != machine.Name || (row.Name != "sprint-dashboard" && !isDashboardService(row.Argv)) {
			continue
		}
		if candidate != nil {
			return verbs.DashboardTarget{}, serviceConfigError("public dashboard service is ambiguous")
		}
		service, p, ok := publicService(row, privateURL)
		if !ok {
			return verbs.DashboardTarget{}, serviceConfigError("public dashboard service must be pull-only without secrets")
		}
		candidate = &service
		port = p
	}
	if candidate == nil {
		service := verbs.FleetService{Name: "sprint-dashboard", Machine: machine.Name, Argv: []string{"/usr/bin/env", "nova-sprint", "dash", "--pull", privateURL, "--listen", ":8080"}}
		candidate, port = &service, "8080"
	}
	if front != "" {
		port = front
	}
	return verbs.DashboardTarget{URL: "http://" + net.JoinHostPort(publicIP.String(), port), Public: true, Service: candidate}, nil
}

// splitPublicChoice is <machine>[:<port>] from --dash public=: the machine
// name and the port the board is fronted on ("" when not given).
func splitPublicChoice(chosen string) (string, string, error) {
	name, port, hasPort := strings.Cut(chosen, ":")
	if hasPort {
		number, err := strconv.Atoi(port)
		if err != nil {
			return "", "", &verbs.OperationError{Problem: edge.RedactN(fmt.Sprintf("--dash public=%s: port %q is not a number: %s", chosen, port, edge.ParseFailure(err, []byte(port))), edge.FailureMax), Fix: "nova-sprint up --dash public=<machine>[:<port>]", Exit: 1}
		}
		if number < 1 || number > 65535 || strconv.Itoa(number) != port {
			return "", "", &verbs.OperationError{Problem: edge.RedactN(fmt.Sprintf("--dash public=%s: port %q is not 1-65535", chosen, port), edge.FailureMax), Fix: "nova-sprint up --dash public=<machine>[:<port>]", Exit: 1}
		}
	}
	if name == "" {
		return "", "", &verbs.OperationError{Problem: "--dash public= names no machine", Fix: "nova-sprint up --dash public=<machine>[:<port>]", Exit: 1}
	}
	return name, port, nil
}
func isDashboardService(argv []string) bool {
	return (len(argv) > 1 && argv[1] == "dash") || (len(argv) > 2 && argv[0] == "/usr/bin/env" && argv[1] == "nova-sprint" && argv[2] == "dash")
}
func publicService(row setupServiceRow, privateURL string) (verbs.FleetService, string, bool) {
	if !strings.HasPrefix(row.Name, "sprint-") || row.Seat != "" || row.Keys != "" || len(row.Env) != 0 || row.Log != "" || row.Every != 0 || !row.Keepalive || !row.Enabled {
		return verbs.FleetService{}, "", false
	}
	argv := row.Argv
	if len(argv) == 7 && argv[0] == "/usr/bin/env" && argv[1] == "nova-sprint" {
		argv = argv[1:]
	} else if len(argv) != 6 || !filepath.IsAbs(argv[0]) || argv[0] == "/usr/bin/env" {
		return verbs.FleetService{}, "", false
	}
	if argv[1] != "dash" || argv[2] != "--pull" || argv[3] != privateURL || argv[4] != "--listen" {
		return verbs.FleetService{}, "", false
	}
	host, port, err := net.SplitHostPort(argv[5])
	number, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || number < 1 || number > 65535 {
		return verbs.FleetService{}, "", false
	}
	if host != "" && host != "0.0.0.0" && host != "::" {
		return verbs.FleetService{}, "", false
	}
	return verbs.FleetService{Name: row.Name, Machine: row.Machine, Argv: row.Argv}, port, true
}
