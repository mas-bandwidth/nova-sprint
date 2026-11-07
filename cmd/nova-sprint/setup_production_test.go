package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

func configForSetup() productionSetupConfig {
	return productionSetupConfig{Binary: "/bin/nova-sprint", Host: "studio", DSN: "postgres://nova_config@db:5432/nova", Secrets: verbs.NovaSecrets{Store: "/secrets", KeyFile: "/keys/studio.key", Pins: "/pins", Seat: "studio"}}
}
func TestProductionSetupConstructsRealDependenciesWithoutNetwork(t *testing.T) {
	st := store.NewFake()
	cfg := configForSetup()
	cfg.SprintPassword = "fixture"
	env := verbs.ExecEnvironment{DSN: cfg.DSN, Password: cfg.SprintPassword, Choices: verbs.Choices{Store: st, Now: func() time.Time { return time.Unix(1000, 0) }}}
	setup, _ := makeProductionSetup(st, env, "alice", "v1.6.0", cfg)
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
	if _, ok := p.runtime.Adoption.(verbs.VersionAdoption); !ok {
		t.Fatal("missing adoption adapter")
	}
	if p.forInput(verbs.SetupInput{Actor: "rowan"}).Prerequisites.Env.Choices.Actor != "rowan" {
		t.Fatal("input actor not carried")
	}
}
func TestProductionSetupRefusesMissingAndUnimplementedConfiguration(t *testing.T) {
	st := store.NewFake()
	env := verbs.ExecEnvironment{Choices: verbs.Choices{Store: st, Now: func() time.Time { return time.Unix(1000, 0) }}}
	cfg := configForSetup()
	cfg.Secrets.Pins = ""
	setup, lock := makeProductionSetup(st, env, "alice", "v1.6.0", cfg)
	_, err := setup.Verify(context.Background(), 2, verbs.SetupInput{Actor: "alice"})
	var op *verbs.OperationError
	if !errors.As(err, &op) || !strings.Contains(op.Fix, "NOVA_SECRETS_PINS") {
		t.Fatal(err)
	}
	_, err = lock.Acquire(context.Background())
	if !errors.As(err, &op) || op.Fix != "nova-sprint up --as <you>" || strings.Contains(op.Error(), "nova-secrets") {
		t.Fatal(err)
	}
	cfg = configForSetup()
	setup, _ = makeProductionSetup(st, env, "alice", "v1.6.0", cfg)
	_, err = setup.Verify(context.Background(), 3, verbs.SetupInput{Actor: "alice"})
	if !errors.As(err, &op) || op.Fix != "nova-sprint up --as alice" || !strings.HasPrefix(op.Problem, "the sprint is not set up") {
		t.Fatal(err)
	}
	cfg.SprintPassword = "fixture"
	setup, _ = makeProductionSetup(st, env, "alice", "v1.6.0", cfg)
	p := setup.(productionSetup)
	p.command = func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{"result":{"verb":"machine list","status":"ok","exit":0},"facts":{"kind":"machine","rows":0},"items":[]}`), nil
	}
	_, err = p.Verify(context.Background(), 4, verbs.SetupInput{Actor: "alice"})
	if !errors.As(err, &op) || !strings.Contains(op.Problem, "run host") {
		t.Fatal(err)
	}
}
func TestSetupRoleDSNUsesCorrectIdentityWithoutSecrets(t *testing.T) {
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
func TestContinuationRecordIsReusableAndEpochFenced(t *testing.T) {
	ctx := context.Background()
	st := store.NewFake()
	in := verbs.SetupInput{Actor: "alice", Dash: "tailnet", Services: "none"}
	digest, err := handoffDigest(in)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 64)
	request, _ := json.Marshal(handoffEnvelope{ID: id, Actor: in.Actor, Digest: digest})
	st.Drop("up-handoff-"+id, true)
	written, err := writeSetupContinuation(ctx, st, bytes.NewReader(request), time.Unix(1000, 0))
	if err != nil || written != id {
		t.Fatal(written, err)
	}
	written, err = writeSetupContinuation(ctx, st, bytes.NewReader(request), time.Unix(1000, 0))
	if err != nil || written != id {
		t.Fatal(written, err)
	}
	if err = validateHandoffRecord(ctx, st, id, in.Actor, digest); err != nil {
		t.Fatal(err)
	}
	changed := in
	changed.Services = "deepseek"
	other, _ := handoffDigest(changed)
	if err = validateHandoffRecord(ctx, st, id, in.Actor, other); err == nil {
		t.Fatal("changed arguments accepted")
	}
	if err = validateHandoffRecord(ctx, st, id, "bob", digest); err == nil {
		t.Fatal("changed actor accepted")
	}
	snap, _ := st.Snapshot(ctx)
	rec, err := st.Clear(ctx, store.Op{ID: "clear", Epoch: snap.Epoch, Actor: "alice", Kind: "clear", At: time.Unix(1000, 0)})
	if err != nil || !rec.Accepted {
		t.Fatal(rec, err)
	}
	if err = validateHandoffRecord(ctx, st, id, in.Actor, digest); err == nil {
		t.Fatal("stale epoch accepted")
	}
}
func TestContinuationInputBoundsAndNoSecretEnvironment(t *testing.T) {
	id := strings.Repeat("a", 64)
	in := verbs.SetupInput{Actor: "alice"}
	digest, _ := handoffDigest(in)
	request, _ := json.Marshal(handoffEnvelope{ID: id, Actor: "alice", Digest: digest})
	oversized := append(request, bytes.Repeat([]byte(" "), 1025)...)
	if _, err := writeSetupContinuation(context.Background(), store.NewFake(), bytes.NewReader(oversized), time.Unix(1000, 0)); err == nil {
		t.Fatal("oversized request accepted")
	}
	env := continuationEnv([]string{"PATH=/bin", "NOVA_PG_DSN=postgres://x/db", "NOVA_PG_ADMIN_PASSWORD=fixture", "NOVA_SPRINT_PG_PASSWORD=fixture"})
	for _, v := range env {
		if strings.Contains(v, "PASSWORD") || strings.Contains(v, "fixture") {
			t.Fatal("secret inherited")
		}
	}
}

type gappedContinuationStore struct{ store.Store }

func (g gappedContinuationStore) Events(context.Context, int64, int) ([]store.Event, error) {
	return []store.Event{{Seq: 2}}, nil
}

func TestContinuationRejectsGappedHistory(t *testing.T) {
	st := gappedContinuationStore{store.NewFake()}
	if _, _, err := findSetupContinuation(context.Background(), st, store.Snapshot{Seq: 2}, strings.Repeat("a", 64)); err == nil {
		t.Fatal("gapped history accepted")
	}
}
func TestProductionSetupRejectsCombinedSecrets(t *testing.T) {
	cfg := configForSetup()
	cfg.AdminPassword = "fixture-admin"
	cfg.SprintPassword = "fixture-sprint"
	st := store.NewFake()
	env := verbs.ExecEnvironment{Choices: verbs.Choices{Store: st, Now: func() time.Time { return time.Unix(1000, 0) }}}
	setup, _ := makeProductionSetup(st, env, "alice", "v1.6.0", cfg)
	_, err := setup.Verify(context.Background(), 1, verbs.SetupInput{Actor: "alice"})
	var op *verbs.OperationError
	if !errors.As(err, &op) || !strings.Contains(op.Problem, "both admin and sprint") {
		t.Fatal(err)
	}
}
func TestProductionSetupRequiresCoordinatorBeforeEffects(t *testing.T) {
	p := productionSetup{}
	_, err := p.Verify(context.Background(), 1, verbs.SetupInput{})
	var op *verbs.OperationError
	if !errors.As(err, &op) || op.Fix != "nova-sprint up --as <name>" {
		t.Fatalf("missing coordinator did not produce a prerequisite: %v", err)
	}
}
func TestProductionSetupDerivesRunAndDashboardFromFleetRows(t *testing.T) {
	st := store.NewFake()
	cfg := configForSetup()
	cfg.SprintPassword = "fixture"
	cfg.BusRedis = "100.100.1.3:6379"
	env := verbs.ExecEnvironment{Choices: verbs.Choices{Store: st, Now: func() time.Time { return time.Unix(1000, 0) }}}
	setup, _ := makeProductionSetup(st, env, "alice", "v1.6.0", cfg)
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
	r, err := p.configuredRuntime(context.Background(), verbs.SetupInput{Actor: "alice", Dash: "tailnet"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.RunService.Machine != "studio" || r.RunService.Argv[0] != "/remote/bin/nova-sprint" || r.Dashboards["tailnet"].URL != "http://100.100.1.2:8080" {
		t.Fatal("incorrect fleet-derived target")
	}
}
func TestProductionSetupRefusesUnsafeOrUnprovenEndpoints(t *testing.T) {
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
	machines := []verbs.FleetMachine{
		{Name: "studio", Address: "studio.example-tailnet.ts.net", Public: "8.8.8.8", Roles: []string{"services"}},
		{Name: "bench-1", Address: "bench-1.example-tailnet.ts.net", Public: "8.8.4.4", Roles: []string{"bench"}},
		{Name: "edge-1", Address: "edge-1.example-tailnet.ts.net", Roles: []string{"services"}},
		{Name: "edge-2", Address: "edge-2.example-tailnet.ts.net", Public: "100.100.1.9"},
	}
	for _, tc := range []struct{ chosen, problem, fix string }{
		{"absent", "no machine named absent in nova-config", "nova-config machine list"},
		{"studio", "studio runs the sprint; the public dashboard goes on another machine", "--dash public=<other>"},
		{"bench-1", "public dashboard host bench-1 is also a bench", "remove the bench role from bench-1, or --dash public=<other machine>"},
		{"edge-1", "machine edge-1 has no public address in nova-config", "nova-config machine set edge-1 --public <ip> --actor <you> --reason <why>"},
		{"edge-2", `machine edge-2's public address "100.100.1.9" in nova-config is not a public IP`, "nova-config machine set edge-2 --public <ip> --actor <you> --reason <why>"},
	} {
		_, err := publicDashboardTarget(tc.chosen, "studio", machines, nil, "http://100.100.1.2:8080")
		var op *verbs.OperationError
		if !errors.As(err, &op) || op.Problem != tc.problem || op.Fix != tc.fix {
			t.Fatalf("%s: %v", tc.chosen, err)
		}
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
	f := verbs.FleetServices{Command: func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{"result":{"verb":"machine list","status":"ok","exit":0},"facts":{"kind":"machine","rows":2},"items":[{"kind":"machine","fields":{"name":"edge-1","address":"edge-1.example-tailnet.ts.net","public":"8.8.8.8","roles":"services"}},{"kind":"machine","fields":{"name":"studio","address":"studio.example-tailnet.ts.net","roles":"services"}}]}`), nil
	}}
	machines, err := f.Machines(context.Background())
	if err != nil || len(machines) != 2 || machines[0].Public != "8.8.8.8" || machines[1].Public != "" {
		t.Fatal(machines, err)
	}
}

func TestRunServiceRejectsUnsafeListenerAndUnexpectedKeys(t *testing.T) {
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

func TestRemoteRunRefusedBeforeFleetCommands(t *testing.T) {
	st := store.NewFake()
	env := verbs.ExecEnvironment{Choices: verbs.Choices{Store: st, Now: func() time.Time { return time.Unix(1000, 0) }}}
	setup, _ := makeProductionSetup(st, env, "alice", "v1.6.0", configForSetup())
	p := setup.(productionSetup)
	p.command = func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("fleet command before remote refusal")
		return nil, nil
	}
	_, err := p.configuredRuntime(context.Background(), verbs.SetupInput{Actor: "alice", RunOn: "remote"}, false)
	var op *verbs.OperationError
	if !errors.As(err, &op) || op.Problem != "run on another host needs nova-fleet install of nova-sprint (nova-tools 2.0.x); for now run here" {
		t.Fatalf("wrong remote refusal: %v", err)
	}
	_, err = p.Verify(context.Background(), 1, verbs.SetupInput{Actor: "alice", RunOn: "remote"})
	if !errors.As(err, &op) || op.Problem != "run on another host needs nova-fleet install of nova-sprint (nova-tools 2.0.x); for now run here" {
		t.Fatalf("remote host was not refused before bootstrap: %v", err)
	}
}

// One check reads the binary a service will run, on the run host here and
// on any other machine over ssh, through the version verb's own parser:
// the verb's real output passes; another version or a pre-v2 bare word is
// refused naming what it saw, with the nova-fleet install fix.
func TestServiceVersionUsesFleetInventoryAndPinnedSSH(t *testing.T) {
	p := productionSetup{cfg: productionSetupConfig{BinaryVersion: "v2.0.0", Host: "studio"}, home: "/home/u"}
	reported := versionVerbOutput(t, "v2.0.0")
	var calls []string
	p.command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch {
		case name == "nova-config" && strings.Join(args, " ") == "inventory --host public":
			return []byte(`{"ansible_host":"100.100.1.2","ansible_user":"fleet"}`), nil
		case name == "ssh" && strings.Join(args, " ") == "-o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=yes -- fleet@100.100.1.2 ~/.local/bin/nova-sprint version":
			return reported, nil
		case name == "/home/u/.local/bin/nova-sprint" && strings.Join(args, " ") == "version":
			return reported, nil
		}
		t.Fatalf("unexpected command: %s %v", name, args)
		return nil, nil
	}
	if err := p.checkServiceVersion(context.Background(), "public"); err != nil || len(calls) != 2 {
		t.Fatal(err, calls)
	}
	if err := p.checkServiceVersion(context.Background(), "studio"); err != nil || len(calls) != 3 {
		t.Fatal(err, calls)
	}
	p.cfg.BinaryVersion = "v2.0.1"
	var op *verbs.OperationError
	err := p.checkServiceVersion(context.Background(), "public")
	if !errors.As(err, &op) || !strings.Contains(op.Problem, "fleet@100.100.1.2 reports v2.0.0, this binary is v2.0.1") || op.Fix != "nova-fleet install --repo <org/repo> --tag v2.0.1 public, then nova-sprint up again" {
		t.Fatal(err)
	}
	reported = []byte("v1.6.29\n")
	if err = p.checkServiceVersion(context.Background(), "studio"); !errors.As(err, &op) || !strings.Contains(op.Problem, `reports "v1.6.29" (not a v2 nova-sprint), this binary is v2.0.1`) {
		t.Fatal(err)
	}
	p.command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "nova-config" {
			return []byte(`{"ansible_host":"100.100.1.2"}`), nil
		}
		return nil, errors.New("connection timed out")
	}
	if err = p.checkServiceVersion(context.Background(), "public"); !errors.As(err, &op) || !strings.Contains(op.Problem, "ssh 100.100.1.2: connection timed out") {
		t.Fatal(err)
	}
}

// versionVerbOutput is what `nova-sprint version` prints, from the verb.
func versionVerbOutput(t *testing.T, version string) []byte {
	t.Helper()
	r := (&verbs.Engine{Version: version}).Execute(context.Background(), []string{"version"})
	if r.Exit != 0 {
		t.Fatal(r.Output)
	}
	return []byte(r.Output)
}

type fakePublicInstaller struct{ install func() }

func (f fakePublicInstaller) Install(context.Context, string, string) error { f.install(); return nil }

func TestPublicVersionInstallRequiresRecheck(t *testing.T) {
	st := store.NewFake()
	r := verbs.RuntimeSetup{Prerequisites: verbs.SetupPrerequisites{Env: verbs.ExecEnvironment{Choices: verbs.Choices{Store: st}}}, Dashboards: map[string]verbs.DashboardTarget{"public=public": {Public: true, Service: &verbs.FleetService{Machine: "public"}}}}
	version, installs, probes := "v1.0.0", 0, 0
	p := productionSetup{cfg: productionSetupConfig{BinaryVersion: "v2.0.0"}}
	p.command = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "nova-config" {
			return []byte(`{"ansible_host":"100.100.1.2"}`), nil
		}
		probes++
		return versionVerbOutput(t, version), nil
	}
	p.installer = fakePublicInstaller{install: func() { installs++; version = "v2.0.0" }}
	in := verbs.SetupInput{Dash: "public=public"}
	ready, err := p.ensurePublicVersion(context.Background(), r, in, false)
	if err != nil || ready || installs != 0 {
		t.Fatal("probe changed installation", ready, err)
	}
	ready, err = p.ensurePublicVersion(context.Background(), r, in, true)
	if err != nil || !ready || installs != 1 || probes != 3 {
		t.Fatal("installation was not followed by a fresh version probe", ready, err, installs, probes)
	}
}

func TestFleetCommandsUseIsolatedConfigCredential(t *testing.T) {
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

func TestUnwrappedToolsReceiveOnlyTheirOwnInputs(t *testing.T) {
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
