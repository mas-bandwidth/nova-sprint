package main

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

func configForSetup() productionSetupConfig {
	return productionSetupConfig{Binary: "/bin/nova-sprint", Host: "studio", DSN: "postgres://nova_config@db:5432/nova", Secrets: verbs.NovaSecrets{Store: "/secrets", KeyFile: "/keys/studio.key", Pins: "/pins", Seat: "studio"}}
}
func TestProductionSetupConstructsRealDependenciesWithoutNetwork(t *testing.T) {
	t.Parallel()
	st := store.NewFake()
	cfg := configForSetup()
	cfg.SprintPassword = "fixture"
	env := verbs.ExecEnvironment{DSN: cfg.DSN, Password: cfg.SprintPassword}
	setup := makeProductionSetup(st, env, "v1.6.0", cfg)
	p, ok := setup.(productionSetup)
	if !ok {
		t.Fatal("wrong adapter")
	}
	if _, ok := p.runtime.Store.(verbs.PGSetupStore); !ok {
		t.Fatal("missing store adapter")
	}
	if _, ok := p.runtime.Run.(verbs.PGSetupRun); !ok {
		t.Fatal("missing run adapter")
	}
}
func TestProductionSetupRefusesMissingAndUnimplementedConfiguration(t *testing.T) {
	t.Parallel()
	st := store.NewFake()
	env := verbs.ExecEnvironment{}
	cfg := configForSetup()
	cfg.Secrets.Pins = ""
	_, err := makeProductionSetup(st, env, "v1.6.0", cfg).Verify(context.Background(), 2, verbs.SetupInput{Actor: "alice"})
	var op *verbs.OperationError
	if !errors.As(err, &op) || !strings.Contains(op.Fix, "NOVA_SECRETS_PINS") {
		t.Fatal(err)
	}
	cfg = configForSetup()
	_, err = makeProductionSetup(st, env, "v1.6.0", cfg).Verify(context.Background(), 4, verbs.SetupInput{Actor: "alice"})
	if !errors.As(err, &op) || op.Fix != "nova-sprint up --as alice" || !strings.Contains(op.Problem, "no sprint credential") {
		t.Fatal(err)
	}
	cfg.SprintPassword = "fixture"
	p := makeProductionSetup(st, env, "v1.6.0", cfg).(productionSetup)
	p.command = func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{"result":{"verb":"machine list","status":"ok","exit":0},"facts":{"kind":"machine","rows":0},"items":[]}`), nil
	}
	_, err = p.Verify(context.Background(), 4, verbs.SetupInput{Actor: "alice"})
	if !errors.As(err, &op) || !strings.Contains(op.Problem, "absent from the inventory") {
		t.Fatal(err)
	}
}
func TestSetupRoleDSNUsesCorrectIdentityWithoutSecrets(t *testing.T) {
	t.Parallel()
	got, err := setupRoleDSN("postgres://nova_config@db:5432/nova?sslmode=require", "nova_admin")
	if err != nil || got != "postgres://nova_admin@db:5432/nova?sslmode=require" {
		t.Fatal(got, err)
	}
	for _, s := range []string{"postgres://u:secret@db/nova", "postgres://db/nova?password=secret", "postgres://db/nova?user=other", "not-a-url"} {
		if _, err := setupRoleDSN(s, "nova_sprint"); err == nil {
			t.Fatal("accepted invalid DSN")
		}
	}
}
func TestProductionSetupDerivesRunAndDashboardFromFleetRows(t *testing.T) {
	t.Parallel()
	st := store.NewFake()
	cfg := configForSetup()
	cfg.SprintPassword = "fixture"
	cfg.BusRedis = "100.100.1.3:6379"
	env := verbs.ExecEnvironment{}
	setup := makeProductionSetup(st, env, "v1.6.0", cfg)
	p := setup.(productionSetup)
	p.command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "nova-config" {
			t.Fatal(name)
		}
		if args[0] == "machine" {
			return []byte(`{"result":{"verb":"machine list","status":"ok","exit":0},"facts":{"kind":"machine","rows":1},"items":[{"kind":"machine","fields":{"name":"studio","address":"100.100.1.2","roles":"services"}}]}`), nil
		}
		return []byte(`{"result":{"verb":"service list","status":"ok","exit":0},"facts":{"kind":"service","rows":1},"items":[{"kind":"service","fields":{"name":"sprint-run","machine":"studio","seat":"studio","keys":"NOVA_SPRINT_PG_PASSWORD","argv":["/remote/bin/nova-sprint","run","--listen","100.100.1.2:8080"],"every":0,"keepalive":true,"enabled":true,"env":{"NOVA_PG_DSN":"postgres://nova_sprint@db:5432/nova","NOVA_BUS_REDIS":"100.100.1.3:6379"},"log":""}}]}`), nil
	}
	r, _, err := p.configuredRuntime(context.Background(), verbs.SetupInput{Actor: "alice", Dash: "tailnet"})
	if err != nil {
		t.Fatal(err)
	}
	if r.RunService.Machine != "studio" || r.RunService.Argv[0] != "/remote/bin/nova-sprint" || r.Board.URL != "http://100.100.1.2:8080" || r.Board.Public {
		t.Fatal("incorrect fleet-derived target")
	}
}

func TestProductionSetupRefusesUnsafeOrUnprovenEndpoints(t *testing.T) {
	t.Parallel()
	if _, err := privateDashboardURL("0.0.0.0:8080"); err == nil {
		t.Fatal("public wildcard accepted")
	}
	if _, err := privateDashboardURL("192.168.1.2:8080"); err == nil {
		t.Fatal("non-tailnet address accepted")
	}
	row := setupServiceRow{Name: "sprint-run", Machine: "studio", Seat: "studio", Keys: "NOVA_SPRINT_PG_PASSWORD,NOVA_PG_ADMIN_PASSWORD", Argv: []string{"/bin/nova-sprint", "run", "--listen", "100.100.1.2:8080"}, Keepalive: true, Enabled: true, Env: map[string]string{"NOVA_BUS_REDIS": "100.100.1.3:6379"}}
	if _, ok := selectRunService([]setupServiceRow{row}, "studio"); ok {
		t.Fatal("admin credential accepted in run service")
	}
	row.Keys = "NOVA_SPRINT_PG_PASSWORD"
	row.Env["NOVA_PG_PASSWORD_ENV"] = "NOVA_SPRINT_PG_PASSWORD"
	if _, ok := selectRunService([]setupServiceRow{row}, "studio"); !ok {
		t.Fatal("canonical password mapping rejected")
	}
	row.Env["NOVA_PG_PASSWORD_ENV"] = "NOVA_PG_ADMIN_PASSWORD"
	if _, ok := selectRunService([]setupServiceRow{row}, "studio"); ok {
		t.Fatal("incorrect password mapping accepted")
	}
	machines := []verbs.FleetMachine{{Name: "studio", Address: "8.8.8.8", Roles: []string{"services"}}}
	if _, err := publicDashboardTarget("studio", "studio", machines, nil, "http://100.100.1.2:8080"); err == nil {
		t.Fatal("public dashboard on run machine accepted")
	}
}

func TestProductionSetupBuildsMissingRunFromInventory(t *testing.T) {
	t.Parallel()
	cfg := configForSetup()
	cfg.BusRedis = "100.100.1.3:6379"
	cfg.BusUser = "sprint"
	p := productionSetup{cfg: cfg}
	machines := []verbs.FleetMachine{{Name: "studio", Address: "100.100.1.2", Roles: []string{"services"}}}
	run, err := p.newRunService(context.Background(), "studio", machines)
	if err != nil || run.Argv[0] != "nova-sprint" || run.Argv[3] != "100.100.1.2:8080" || run.Seat != "studio" || run.Env["NOVA_PG_PASSWORD_ENV"] != "NOVA_SPRINT_PG_PASSWORD" {
		t.Fatalf("invalid derived service: %v", err)
	}
	if _, err := p.newRunService(context.Background(), "remote", machines); err == nil {
		t.Fatal("guessed a remote seat")
	}
	machines[0].Address = "192.168.1.2"
	if _, err := p.newRunService(context.Background(), "studio", machines); err == nil {
		t.Fatal("guessed a tailnet address")
	}
}

// The run host's inventory address may be its MagicDNS name: resolved (by
// the injected resolver) to its tailnet IP, or kept as the name when it does
// not resolve here; a name that resolves off the tailnet, or one that is
// not a tailnet name, is refused.
func TestProductionSetupRunHostByTailnetName(t *testing.T) {
	t.Parallel()
	cfg := configForSetup()
	cfg.BusRedis = "100.100.1.3:6379"
	answers := map[string][]netip.Addr{"studio.example-tailnet.ts.net": {netip.MustParseAddr("fd7a:115c:a1e0::1"), netip.MustParseAddr("100.100.1.2")}, "off.example-tailnet.ts.net": {netip.MustParseAddr("8.8.8.8")}}
	p := productionSetup{cfg: cfg, resolve: func(_ context.Context, host string) ([]netip.Addr, error) {
		if a, ok := answers[host]; ok {
			return a, nil
		}
		return nil, errors.New("no such host")
	}}
	for _, tc := range []struct{ address, listen string }{
		{"studio.example-tailnet.ts.net", "100.100.1.2:8080"},
		{"Studio.Example-Tailnet.ts.net.", "100.100.1.2:8080"},
		{"later.example-tailnet.ts.net", "later.example-tailnet.ts.net:8080"},
		{"off.example-tailnet.ts.net", ""},
		{"studio.example.com", ""},
	} {
		machines := []verbs.FleetMachine{{Name: "studio", Address: tc.address, Roles: []string{"services"}}}
		run, err := p.newRunService(context.Background(), "studio", machines)
		if tc.listen == "" {
			if err == nil || !strings.Contains(err.Error(), "private-network address") {
				t.Fatalf("%s: accepted %v", tc.address, run.Argv)
			}
			continue
		}
		if err != nil || run.Argv[3] != tc.listen {
			t.Fatalf("%s: %v %v", tc.address, run.Argv, err)
		}
		if url, err := privateDashboardURL(run.Argv[3]); err != nil || url != "http://"+tc.listen {
			t.Fatalf("%s: board %q %v", tc.address, url, err)
		}
	}
	if _, err := privateDashboardURL("studio.example.com:8080"); err == nil {
		t.Fatal("a public name passed as private")
	}
}

func TestProductionSetupBuildsPublicPullService(t *testing.T) {
	t.Parallel()
	machines := []verbs.FleetMachine{{Name: "public", Address: "public.example-tailnet.ts.net", Public: "8.8.8.8", Roles: []string{"services"}}}
	target, err := publicDashboardTarget("public", "studio", machines, nil, "http://100.100.1.2:8080")
	if err != nil || target.Service == nil || target.URL != "http://8.8.8.8:8080" || strings.Join(target.Service.Argv[:3], " ") != "/usr/bin/env nova-sprint dash" || len(target.Service.Keys) != 0 {
		t.Fatalf("invalid public target: %v", err)
	}
	conflict := []setupServiceRow{{Name: "sprint-dashboard", Machine: "other"}}
	if _, err := publicDashboardTarget("public", "studio", machines, conflict, "http://100.100.1.2:8080"); err == nil {
		t.Fatal("conflicting public service accepted")
	}
	// Fronted on 80: the URL up verifies is :80; the service listens on :8080.
	target, err = publicDashboardTarget("public:80", "studio", machines, nil, "http://100.100.1.2:8080")
	if err != nil || target.URL != "http://8.8.8.8:80" || target.Service.Argv[len(target.Service.Argv)-1] != ":8080" {
		t.Fatalf("fronted target %+v: %v", target, err)
	}
	for _, bad := range []string{"public:0", "public:http", "public:080", ":80"} {
		if _, err := publicDashboardTarget(bad, "studio", machines, nil, "http://100.100.1.2:8080"); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

// Each reason the public board cannot go on a machine is its own refusal,
// with its own fix; none is a generic service-configuration one.
func TestPublicDashboardTargetRefusalsAreDistinct(t *testing.T) {
	t.Parallel()
	machines := []verbs.FleetMachine{
		{Name: "studio", Address: "studio.example-tailnet.ts.net", Public: "8.8.8.8", Roles: []string{"services"}},
		{Name: "bench-1", Address: "bench-1.example-tailnet.ts.net", Public: "8.8.4.4", Roles: []string{"bench"}},
		{Name: "edge-1", Address: "edge-1.example-tailnet.ts.net", Roles: []string{"services"}},
		{Name: "edge-2", Address: "edge-2.example-tailnet.ts.net", Public: "100.100.1.9"},
	}
	for _, tc := range []struct{ chosen, problem, fix string }{
		{"absent", "no machine named absent in nova-config", "nova-config machine list"},
		{"studio", "studio runs the sprint; the public dashboard goes on another machine", "--dash public=<other>"},
		{"edge-1", "machine edge-1 has no public address in nova-config", "nova-config machine set edge-1 --public <ip> --actor <you> --reason <why>"},
		{"edge-2", `machine edge-2's public address "100.100.1.9" in nova-config is not a public IP`, "nova-config machine set edge-2 --public <ip> --actor <you> --reason <why>"},
	} {
		_, err := publicDashboardTarget(tc.chosen, "studio", machines, nil, "http://100.100.1.2:8080")
		var op *verbs.OperationError
		if !errors.As(err, &op) || op.Problem != tc.problem || op.Fix != tc.fix {
			t.Fatalf("%s: %v", tc.chosen, err)
		}
	}
	// A bench serves the public board and works cards (Glenn, 2026-11-07).
	if target, err := publicDashboardTarget("bench-1", "studio", machines, nil, "http://100.100.1.2:8080"); err != nil || target.URL != "http://8.8.4.4:8080" {
		t.Fatalf("public board on a bench: %+v: %v", target, err)
	}
	// A refusal keeps its own words through the setup's wrapping.
	inner := &verbs.OperationError{Problem: "p", Fix: "f", Exit: 1}
	if err := serviceConfigFailure("cannot inspect machine inventory: ", inner); err != error(inner) {
		t.Fatal(err)
	}
	if err := serviceConfigFailure("cannot inspect machine inventory: ", errors.New("boom")); !strings.Contains(err.Error(), "cannot inspect machine inventory: boom") {
		t.Fatal(err)
	}
}

// machine list --json carries the optional "public" field (nova-tools 2.0.3).
func TestFleetMachinesReadPublicAddress(t *testing.T) {
	t.Parallel()
	f := verbs.FleetServices{Command: func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{"result":{"verb":"machine list","status":"ok","exit":0},"facts":{"kind":"machine","rows":2},"items":[{"kind":"machine","fields":{"name":"edge-1","address":"edge-1.example-tailnet.ts.net","public":"8.8.8.8","roles":"services"}},{"kind":"machine","fields":{"name":"studio","address":"studio.example-tailnet.ts.net","roles":"services"}}]}`), nil
	}}
	machines, err := f.Machines(context.Background())
	if err != nil || len(machines) != 2 || machines[0].Public != "8.8.8.8" || machines[1].Public != "" {
		t.Fatal(machines, err)
	}
}

func TestRunServiceRejectsUnsafeListenerAndUnexpectedKeys(t *testing.T) {
	t.Parallel()
	p := productionSetup{cfg: productionSetupConfig{Host: "studio", BusRedis: "100.100.1.3:6379"}}
	service := verbs.FleetService{Machine: "studio", Seat: "studio", Argv: []string{"nova-sprint", "run", "--listen", "0.0.0.0:8080"}, Keys: []string{"NOVA_SPRINT_PG_PASSWORD"}, Env: map[string]string{"NOVA_BUS_REDIS": "100.100.1.3:6379"}}
	if err := p.validateRunService(service); err == nil {
		t.Fatal("public run listener accepted")
	}
	service.Argv[3] = "100.100.1.2:8080"
	service.Keys = append(service.Keys, "UNRELATED_SECRET")
	if err := p.validateRunService(service); err == nil {
		t.Fatal("unrelated secret accepted")
	}
	service.Keys = service.Keys[:1]
	if err := p.validateRunService(service); err != nil {
		t.Fatal(err)
	}
}

func TestFleetCommandsUseIsolatedConfigCredential(t *testing.T) {
	t.Parallel()
	cfg := configForSetup()
	calls := 0
	command := productionFleetCommandWith(cfg, func(_ context.Context, clean bool, name string, args ...string) ([]byte, error) {
		calls++
		joined := strings.Join(args, " ")
		if calls == 1 && (!clean || name != "nova-secrets" || !strings.Contains(joined, "--only NOVA_PG_PASSWORD") || !strings.Contains(joined, "NOVA_PG_DSN=postgres://nova_config@db:5432/nova") || !strings.Contains(joined, "NOVA_PG_PASSWORD_ENV=NOVA_PG_PASSWORD nova-config inventory --host studio")) {
			t.Fatal("inventory did not use isolated config credential", name, joined)
		}
		if calls == 2 && (!clean || name != "ssh") {
			t.Fatal("SSH received config credential wrapper")
		}
		return nil, nil
	})
	_, _ = command(context.Background(), "nova-config", "inventory", "--host", "studio")
	_, _ = command(context.Background(), "ssh", "studio", "nova-sprint", "version")
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestFleetCommandRefusesIncompleteSecretsBeforeRunning(t *testing.T) {
	t.Parallel()
	for missing, clear := range map[string]func(*verbs.NovaSecrets){
		"NOVA_SECRETS_STORE": func(s *verbs.NovaSecrets) { s.Store = "" },
		"NOVA_SECRETS_KEY":   func(s *verbs.NovaSecrets) { s.KeyFile = "" },
		"NOVA_SECRETS_SEAT":  func(s *verbs.NovaSecrets) { s.Seat = "" },
	} {
		cfg := configForSetup()
		clear(&cfg.Secrets)
		command := productionFleetCommandWith(cfg, func(context.Context, bool, string, ...string) ([]byte, error) {
			t.Fatal("nova-secrets ran with", missing, "empty")
			return nil, nil
		})
		_, err := command(context.Background(), "nova-config", "machine", "list", "--json")
		var op *verbs.OperationError
		if !errors.As(err, &op) || !strings.Contains(op.Problem, "control-seat secrets configuration is incomplete") || !strings.Contains(op.Problem, missing) || !strings.Contains(op.Fix, "run nova-secrets -h") {
			t.Fatal(missing, err)
		}
	}
}

func TestUnwrappedToolsReceiveOnlyTheirOwnInputs(t *testing.T) {
	t.Parallel()
	env := []string{"PATH=/bin", "NOVA_PG_ADMIN_PASSWORD=admin", "NOVA_SPRINT_PG_PASSWORD=sprint", "NOVA_PG_PASSWORD=config", "NOVA_BUS_REDIS=127.0.0.1:6379", "NOVA_BUS_USER=bus", "NOVA_BUS_PASSWORD=bus-secret", "NOVA_SECRETS_STORE=/store", "CODEX_THREAD_ID=fixture"}
	fleet := setupCommandEnv("nova-fleet", env)
	bus := setupCommandEnv("nova-bus", env)
	for _, got := range [][]string{fleet, bus} {
		for _, entry := range got {
			if strings.HasPrefix(entry, "NOVA_PG_ADMIN_PASSWORD=") || strings.HasPrefix(entry, "NOVA_SPRINT_PG_PASSWORD=") || strings.HasPrefix(entry, "NOVA_PG_PASSWORD=") {
				t.Fatal("database secret inherited by unrelated tool")
			}
		}
	}
	if !slices.Contains(fleet, "NOVA_SECRETS_STORE=/store") || slices.Contains(fleet, "NOVA_BUS_PASSWORD=bus-secret") || !slices.Contains(bus, "NOVA_BUS_PASSWORD=bus-secret") || !slices.Contains(bus, "CODEX_THREAD_ID=fixture") {
		t.Fatal("tool-specific inputs missing or crossed")
	}
}

// A NOVA_PG_DSN the run's role DSN cannot be made from refuses the walk with
// its cause, never a run adapter on an empty DSN.
func TestProductionSetupRefusesABadDSN(t *testing.T) {
	t.Parallel()
	st := store.NewFake()
	env := verbs.ExecEnvironment{}
	cfg := configForSetup()
	cfg.DSN = "mysql://db/nova"
	setup := makeProductionSetup(st, env, "v1.6.0", cfg)
	if _, err := setup.Verify(context.Background(), 1, verbs.SetupInput{Actor: "alice"}); err == nil || !strings.Contains(err.Error(), `scheme "mysql" is not postgres`) {
		t.Fatalf("verify: %v", err)
	}
}

// up checks this release where a step runs nova-sprint with nova-fleet
// install --check, and installs it on the act; a recap line that would
// change a machine, or none for it, is drift; a build that is not a
// release can be neither checked nor installed.
func TestReleaseChecksAndInstallsThroughNovaFleet(t *testing.T) {
	t.Parallel()
	var calls [][]string
	recap := "PLAY RECAP *****\namd-1                      : ok=5    changed=0    unreachable=0    failed=0    skipped=1\namd-2                      : ok=5    changed=1    unreachable=0    failed=0    skipped=1\n"
	p := productionSetup{cfg: productionSetupConfig{BinaryVersion: "v2.1.0"}, command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return []byte(recap), nil
	}}
	err := p.release(context.Background(), []string{"amd-1", "amd-2"}, false)
	var op *verbs.OperationError
	if !errors.As(err, &op) || !strings.Contains(op.Problem, "is not what amd-2 runs") || strings.Contains(op.Problem, "amd-1") || op.Fix != "nova-fleet install --repo mas-bandwidth/nova-sprint --tag v2.1.0 --names nova-sprint amd-1 amd-2, then nova-sprint up again" {
		t.Fatal(err)
	}
	if want := []string{"nova-fleet", "install", "--check", "--repo", "mas-bandwidth/nova-sprint", "--tag", "v2.1.0", "--names", "nova-sprint", "amd-1", "amd-2"}; !slices.Equal(calls[0], want) {
		t.Fatalf("%q", calls[0])
	}
	if err = p.release(context.Background(), []string{"amd-1", "amd-2"}, true); err != nil || slices.Contains(calls[1], "--check") {
		t.Fatalf("%v %q", err, calls[1])
	}
	if err = p.release(context.Background(), []string{"amd-1", "absent"}, false); !errors.As(err, &op) || !strings.Contains(op.Problem, "absent runs") {
		t.Fatal(err)
	}
	if err = p.release(context.Background(), []string{"amd-1"}, false); err != nil {
		t.Fatal(err)
	}
	if err = p.release(context.Background(), nil, false); err != nil || len(calls) != 4 {
		t.Fatal(err, len(calls))
	}
	p.cfg.BinaryVersion = "dev+0123456789ab"
	if err = p.release(context.Background(), []string{"amd-1"}, true); !errors.As(err, &op) || !strings.Contains(op.Problem, "not a release") || len(calls) != 4 {
		t.Fatal(err)
	}
}
