package main

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/run"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

// productionSetupConfig contains only public locations and credential values
// already injected into this process by nova-secrets; it is never printed.
type productionSetupConfig struct {
	Binary, BinaryVersion, Host, DSN, AdminPassword, SprintPassword, BusUser, BusRedis string
	Secrets                                                                            verbs.NovaSecrets
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
	return productionSetupConfig{Binary: binary, BinaryVersion: versionString(), Host: host, DSN: os.Getenv("NOVA_PG_DSN"), AdminPassword: os.Getenv("NOVA_PG_ADMIN_PASSWORD"), SprintPassword: os.Getenv("NOVA_SPRINT_PG_PASSWORD"), BusUser: os.Getenv("NOVA_BUS_USER"), BusRedis: os.Getenv("NOVA_BUS_REDIS"), Secrets: verbs.NovaSecrets{Store: os.Getenv("NOVA_SECRETS_STORE"), KeyFile: os.Getenv("NOVA_SECRETS_KEY"), Pins: os.Getenv("NOVA_SECRETS_PINS"), Seat: seat}}, nil
}

// buildProductionSetup is called only by production CLI composition. It does
// no fleet, network, database or secret-store work while building dependencies.
func buildProductionSetup(st store.Store, env verbs.ExecEnvironment, actor, pin string) (verbs.Setup, verbs.SetupLock) {
	cfg, err := productionSetupConfigFromEnv()
	if err != nil {
		return failedProductionSetup{err}, productionSetupLock{err: err}
	}
	return makeProductionSetup(st, env, actor, pin, cfg)
}
func makeProductionSetup(st store.Store, env verbs.ExecEnvironment, actor, pin string, cfg productionSetupConfig) (verbs.Setup, verbs.SetupLock) {
	command := setupCommandWithBus(cfg, productionFleetCommand(cfg))
	env.Command = command
	env.Choices = verbs.Choices{Store: st, Actor: actor, Now: env.Choices.Now}
	secret := cfg.Secrets
	grants := verbs.FleetGrants{Store: secret.Store, KeyFile: secret.KeyFile, Pins: secret.Pins, ControlSeat: secret.Seat, Command: command}
	role := verbs.SprintRoleBootstrap{Sink: secret, Grants: grants, Secrets: secret, Binary: cfg.Binary}
	schema := verbs.PGSetupStore{Role: role, ControlMachine: cfg.Host, Secrets: secret, Binary: cfg.Binary}
	keys := []string{"NOVA_SPRINT_PG_PASSWORD"}
	if cfg.BusUser != "" {
		keys = append(keys, "NOVA_BUS_PASSWORD")
	}
	runDSN, _ := setupRoleDSN(cfg.DSN, "nova_sprint")
	runtime := verbs.RuntimeSetup{Prerequisites: verbs.SetupPrerequisites{Env: env, Pin: pin}, Store: schema, Run: verbs.PGSetupRun{DSN: runDSN, Password: cfg.SprintPassword, Env: env}, Services: verbs.FleetServices{Command: command}, Keys: verbs.ServiceKey{Terminal: verbs.TTY{}, Sink: secret, Probe: verbs.ModelProbeCommand{Binary: cfg.Binary, Secrets: secret}}, Role: role, Adoption: verbs.VersionAdoption{Binary: cfg.Binary, Secrets: secret, Keys: keys}, Snapshot: st}
	return productionSetup{runtime: runtime, cfg: cfg, command: command, installer: fleetPublicInstaller{command}}, productionSetupLock{cfg: cfg}
}

type failedProductionSetup struct{ err error }

func (f failedProductionSetup) Verify(context.Context, int, verbs.SetupInput) (verbs.SetupCheck, error) {
	return verbs.SetupCheck{}, f.err
}
func (f failedProductionSetup) Act(context.Context, int, verbs.SetupInput) error { return f.err }

type productionSetup struct {
	runtime   verbs.RuntimeSetup
	cfg       productionSetupConfig
	command   verbs.CommandOutput
	installer publicInstaller
	resolve   func(context.Context, string) ([]netip.Addr, error) // nil: the system resolver (MagicDNS)
	home      string                                              // "": the home directory (where this machine's services find nova-sprint)
}

func (p productionSetup) forInput(in verbs.SetupInput) verbs.RuntimeSetup {
	r := p.runtime
	r.Prerequisites.Env.Choices.Actor = in.Actor
	return r
}
func (p productionSetup) Verify(ctx context.Context, step int, in verbs.SetupInput) (verbs.SetupCheck, error) {
	if step == 1 && !handoffActor.MatchString(in.Actor) {
		return verbs.SetupCheck{}, &verbs.OperationError{Problem: "setup coordinator is missing or invalid", Fix: "nova-sprint up --as <name>", Exit: 1}
	}
	if step == 1 && in.RunOn != "" && in.RunOn != p.cfg.Host {
		return verbs.SetupCheck{}, p.remoteRunRefusal()
	}
	if p.cfg.AdminPassword != "" && p.cfg.SprintPassword != "" {
		return verbs.SetupCheck{}, &verbs.OperationError{Problem: "setup process has both admin and sprint credentials", Fix: "run nova-sprint up through the two-phase secrets wrapper", Exit: 1}
	}
	if step == 2 {
		if err := p.configError(); err != nil {
			return verbs.SetupCheck{}, err
		}
	}
	if step >= 3 && p.cfg.SprintPassword == "" {
		return verbs.SetupCheck{}, &verbs.OperationError{Problem: "the sprint is not set up (no sprint credential in this process)", Fix: "nova-sprint up --as " + in.Actor, Exit: 1}
	}
	if step == 6 {
		r := p.forInput(in)
		_, found, err := r.Prerequisites.Env.Choices.Get(ctx, "services")
		if err != nil {
			return verbs.SetupCheck{}, err
		}
		if !found && in.Services == "" {
			return verbs.SetupCheck{Line: "UP 6/8 services: not chosen\nASK the human which API-rate services to set up", Next: "nova-sprint up --services <names|none>"}, nil
		}
		return verbs.SetupCheck{}, workloadUnavailable()
	}
	if step >= 7 {
		return verbs.SetupCheck{}, workloadUnavailable()
	}
	if step >= 4 {
		r, err := p.configuredRuntime(ctx, in, step >= 5)
		if err != nil {
			return verbs.SetupCheck{}, err
		}
		if step == 4 {
			ready, err := p.ensureServiceVersion(ctx, r.RunService.Machine, false)
			if err != nil {
				return verbs.SetupCheck{}, err
			}
			if !ready {
				return verbs.SetupCheck{Action: true}, nil
			}
		}
		if step == 5 {
			value, found, err := r.Prerequisites.Env.Choices.Get(ctx, "dashboard")
			if err != nil {
				return verbs.SetupCheck{}, err
			}
			if !found && in.Dash == "" && value == "" {
				return verbs.SetupCheck{Line: "UP 5/8 dashboard: not chosen\nASK the human: the dashboard on the private network (default), or public on a machine with a public address in nova-config?", Next: "nova-sprint up --dash tailnet      or      nova-sprint up --dash public=<machine>[:<port>]"}, nil
			}
			ready, err := p.ensurePublicVersion(ctx, r, in, false)
			if err != nil {
				return verbs.SetupCheck{}, err
			}
			if !ready {
				return verbs.SetupCheck{Action: true}, nil
			}
		}
		return r.Verify(ctx, step, in)
	}
	return p.forInput(in).Verify(ctx, step, in)
}
func (p productionSetup) Act(ctx context.Context, step int, in verbs.SetupInput) error {
	if step == 2 {
		if err := p.configError(); err != nil {
			return err
		}
		if err := p.checkControlMachine(ctx); err != nil {
			return err
		}
	}
	if step >= 6 {
		return workloadUnavailable()
	}
	if step >= 4 {
		r, err := p.configuredRuntime(ctx, in, step >= 5)
		if err != nil {
			return err
		}
		if step == 4 {
			if _, err = p.ensureServiceVersion(ctx, r.RunService.Machine, true); err != nil {
				return err
			}
		}
		if step == 5 {
			if _, err = p.ensurePublicVersion(ctx, r, in, true); err != nil {
				return err
			}
		}
		return r.Act(ctx, step, in)
	}
	return p.forInput(in).Act(ctx, step, in)
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
		return &verbs.OperationError{Problem: "cannot inspect the control machine inventory: " + verbs.Redact(err.Error()), Fix: "fix what nova-config said, then nova-sprint up again", Exit: 2}
	}
	for _, machine := range machines {
		if machine.Name == p.cfg.Host {
			return nil
		}
	}
	return &verbs.OperationError{Problem: "control machine is absent from inventory", Fix: "run nova-config machine list --json and nova-fleet check", Exit: 1}
}

type productionSetupLock struct {
	cfg productionSetupConfig
	err error
}

func (p productionSetupLock) Acquire(ctx context.Context) (verbs.SetupLease, error) {
	if p.err != nil {
		return nil, p.err
	}
	if p.cfg.DSN == "" {
		return nil, &verbs.OperationError{Problem: "NOVA_PG_DSN is missing", Fix: "nova-sprint check", Exit: 1}
	}
	role, password := "nova_sprint", p.cfg.SprintPassword
	if password == "" {
		role, password = "nova_admin", p.cfg.AdminPassword
	}
	if password == "" {
		return nil, &verbs.OperationError{Problem: "the sprint is not set up (no setup database credential in this process)", Fix: "nova-sprint up --as <you>", Exit: 1}
	}
	dsn, err := setupRoleDSN(p.cfg.DSN, role)
	if err != nil {
		return nil, &verbs.OperationError{Problem: "NOVA_PG_DSN is invalid", Fix: "configure a passwordless PostgreSQL URL and run nova-sprint check", Exit: 1}
	}
	return (verbs.PGSetupLock{DSN: dsn, Password: password, Host: p.cfg.Host}).Acquire(ctx)
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

// handoffEnvelope carries no credentials; the ID correlates a durable intent.
type handoffEnvelope struct {
	ID     string `json:"id"`
	Actor  string `json:"actor"`
	Digest string `json:"digest"`
}
type handoffRecord struct {
	Version int    `json:"version"`
	Epoch   int    `json:"epoch"`
	Actor   string `json:"actor"`
	Digest  string `json:"digest"`
}

var handoffID = regexp.MustCompile(`^[0-9a-f]{64}$`)
var handoffActor = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func handoffDigest(in verbs.SetupInput) (string, error) {
	values := []string{in.Actor, in.Dash, in.Services, in.Key, in.RunOn, in.Version}
	if !handoffActor.MatchString(in.Actor) {
		return "", fmt.Errorf("invalid setup actor %q: want 1-128 of A-Z a-z 0-9 . _ -", edge.Redact(in.Actor))
	}
	for _, v := range values {
		if len(v) > 512 {
			return "", fmt.Errorf("setup argument too long: %d bytes, over 512", len(v))
		}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("nova-sprint/setup/v1\x00"), raw...))
	return hex.EncodeToString(sum[:]), nil
}

// PrepareHandoff is called only while the first child's setup lease is held.
func (p productionSetup) PrepareHandoff(ctx context.Context, in verbs.SetupInput) (string, bool, error) {
	if p.cfg.SprintPassword != "" && p.cfg.AdminPassword != "" {
		return "", false, fmt.Errorf("setup credential separation unavailable: NOVA_SPRINT_PG_PASSWORD and NOVA_PG_ADMIN_PASSWORD are both set")
	}
	if p.cfg.SprintPassword != "" {
		return "", false, nil
	}
	if p.cfg.AdminPassword == "" {
		return "", false, fmt.Errorf("setup credential unavailable: NOVA_PG_ADMIN_PASSWORD not set")
	}
	if err := p.configError(); err != nil {
		return "", false, err
	}
	digest, err := handoffDigest(in)
	if err != nil {
		return "", false, err
	}
	var random [32]byte
	if _, err = rand.Read(random[:]); err != nil {
		return "", false, edge.Cause("continuation unavailable: random id", err)
	}
	request := handoffEnvelope{ID: hex.EncodeToString(random[:]), Actor: in.Actor, Digest: digest}
	result, err := p.writeHandoffChild(ctx, request)
	if err != nil {
		return "", false, err
	}
	if result != request.ID {
		return "", false, fmt.Errorf("continuation was not saved: the child returned id %q, not the one sent", edge.Redact(result))
	}
	return result, true, nil
}
func (p productionSetup) writeHandoffChild(ctx context.Context, request handoffEnvelope) (string, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	s := p.cfg.Secrets
	args := []string{"exec", "--only", "NOVA_SPRINT_PG_PASSWORD", "--store", s.Store, "--key", s.KeyFile, "--as", s.Seat, "--", p.cfg.Binary, "--internal-write-continuation"}
	cmd := exec.CommandContext(ctx, "nova-secrets", args...)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Env = continuationEnv(os.Environ())
	var buf boundedOutput
	err = verbs.OutputWith(cmd, &buf)
	out := buf.Bytes()
	if err != nil {
		return "", fmt.Errorf("continuation could not be saved%s", verbs.ToolDetail(err))
	}
	if len(out) > 128 {
		return "", fmt.Errorf("continuation could not be saved: %s", edge.ParseRefused("child output longer than 128 bytes", 128, out))
	}
	id := strings.TrimSpace(string(out))
	if !handoffID.MatchString(id) {
		return "", fmt.Errorf("continuation could not be saved: %s", edge.ParseRefused("child output is not a 64-hex id", 0, out))
	}
	return id, nil
}
func continuationEnv(env []string) []string {
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

// ValidateHandoff is called again while phase 2 holds its fresh setup lease.
func (p productionSetup) ValidateHandoff(ctx context.Context, id string, in verbs.SetupInput) error {
	if !handoffID.MatchString(id) {
		return handoffRefusal("the continuation id is not 64 hex")
	}
	if p.cfg.SprintPassword == "" || p.cfg.AdminPassword != "" {
		return handoffRefusal(fmt.Sprintf("wants NOVA_SPRINT_PG_PASSWORD set and NOVA_PG_ADMIN_PASSWORD unset (sprint set %t, admin set %t)", p.cfg.SprintPassword != "", p.cfg.AdminPassword != ""))
	}
	digest, err := handoffDigest(in)
	if err != nil {
		return handoffRefusal(edge.Redact(err.Error()))
	}
	dsn, err := setupRoleDSN(p.cfg.DSN, "nova_sprint")
	if err != nil {
		return handoffRefusal(edge.Redact(err.Error()))
	}
	pg, err := store.Open(ctx, dsn, p.cfg.SprintPassword)
	if err != nil {
		return handoffRefusal("open store: " + edge.Redact(err.Error()))
	}
	defer pg.Close()
	identity, err := pg.Schema(ctx)
	if err != nil {
		return handoffRefusal("schema: " + edge.Redact(err.Error()))
	}
	if !identity.Current || identity.Owner != "nova_sprint" {
		return handoffRefusal(fmt.Sprintf("schema current %t, owner %q, want current and nova_sprint", identity.Current, identity.Owner))
	}
	return validateHandoffRecord(ctx, pg, id, in.Actor, digest)
}
func validateHandoffRecord(ctx context.Context, st store.Store, id, actor, digest string) error {
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return handoffRefusal("snapshot: " + edge.Redact(err.Error()))
	}
	record, found, err := findSetupContinuation(ctx, st, snap, id)
	switch {
	case err != nil:
		return handoffRefusal(edge.Redact(err.Error()))
	case !found:
		return handoffRefusal("no continuation recorded with this id")
	case record.Version != 1 || record.Epoch != snap.Epoch:
		return handoffRefusal(fmt.Sprintf("recorded version %d epoch %d, want version 1 epoch %d", record.Version, record.Epoch, snap.Epoch))
	case record.Actor != actor:
		return handoffRefusal(fmt.Sprintf("recorded for actor %q, not %q", edge.Redact(record.Actor), edge.Redact(actor)))
	case record.Digest != digest:
		return handoffRefusal("the setup arguments differ from the ones recorded")
	}
	return nil
}
func handoffRefusal(why string) error {
	return &verbs.OperationError{Problem: edge.RedactN("setup continuation is invalid or stale: "+why, edge.FailureMax), Fix: "run nova-sprint up again", Exit: 1}
}

// writeSetupContinuation is called in a sprint-secret child while the parent
// holds the setup lease. Only the non-secret ID is returned.
func writeSetupContinuation(ctx context.Context, st store.Store, input io.Reader, at time.Time) (string, error) {
	data, err := io.ReadAll(io.LimitReader(input, 1025))
	if err != nil {
		return "", edge.Cause("invalid continuation request: read", err)
	}
	if len(data) > 1024 {
		return "", fmt.Errorf("invalid continuation request: %s", edge.ParseRefused("longer than 1024 bytes", 1024, data))
	}
	var request handoffEnvelope
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&request); err != nil {
		return "", fmt.Errorf("invalid continuation request: %s", edge.ParseFailure(err, data))
	}
	if !handoffID.MatchString(request.ID) || !handoffID.MatchString(request.Digest) || !handoffActor.MatchString(request.Actor) {
		return "", fmt.Errorf("invalid continuation request: %s", edge.ParseRefused("id, digest or actor is not well formed", 0, data))
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return "", fmt.Errorf("invalid continuation request: %s", edge.ParseRefused("more than one request", int(dec.InputOffset()), data))
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return "", edge.Cause("continuation unavailable: snapshot", err)
	}
	existing, found, err := findSetupContinuation(ctx, st, snap, request.ID)
	if err != nil {
		return "", edge.Cause("continuation unavailable", err)
	}
	if found {
		if existing.Version == 1 && existing.Epoch == snap.Epoch && existing.Actor == request.Actor && existing.Digest == request.Digest {
			return request.ID, nil
		}
		return "", fmt.Errorf("continuation unavailable: id %s is already recorded with other content", request.ID[:12])
	}
	record := handoffRecord{Version: 1, Epoch: snap.Epoch, Actor: request.Actor, Digest: request.Digest}
	body, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	op := store.Op{ID: "up-handoff-" + request.ID, Epoch: snap.Epoch, Actor: request.Actor, Kind: "setup continuation", At: at, Expect: []store.Expect{store.AtSeq{Seq: snap.Seq}}, Writes: []store.Write{store.PutPolicy{Kind: "data", Name: "setup:continuation:" + request.ID, Body: string(body)}}}
	if err = applyHandoff(ctx, st, op); err != nil {
		return "", err
	}
	return request.ID, nil
}
func applyHandoff(ctx context.Context, st store.Store, op store.Op) error {
	for range 3 {
		receipt, err := st.Apply(ctx, op)
		if err == nil {
			if !receipt.Accepted {
				return fmt.Errorf("continuation refused: %s", edge.Redact(cmp.Or(receipt.Reason, "no reason given")))
			}
			return nil
		}
		if !errors.Is(err, store.ErrUnknown) {
			return edge.Cause("continuation unavailable: apply", err)
		}
		receipt, found, err := st.Replay(ctx, op.ID)
		if err != nil {
			return edge.Cause("continuation unavailable: replay", err)
		}
		if found {
			if !receipt.Accepted {
				return fmt.Errorf("continuation refused: %s", edge.Redact(cmp.Or(receipt.Reason, "no reason given")))
			}
			return nil
		}
	}
	return fmt.Errorf("continuation unavailable: apply outcome unknown after 3 tries and no replay found")
}
func findSetupContinuation(ctx context.Context, st store.Store, snap store.Snapshot, id string) (handoffRecord, bool, error) {
	cursor := int64(0)
	count := 0
	var result handoffRecord
	for cursor < snap.Seq {
		events, err := st.Events(ctx, cursor, store.MaxEvents)
		if err != nil {
			return result, false, edge.Cause("continuation history unavailable", err)
		}
		if len(events) == 0 {
			return result, false, fmt.Errorf("continuation history unavailable: no events after seq %d, snapshot at %d", cursor, snap.Seq)
		}
		advanced := false
		for _, event := range events {
			if event.Seq > snap.Seq {
				break
			}
			if event.Seq != cursor+1 {
				return result, false, fmt.Errorf("continuation history invalid: seq %d follows %d", event.Seq, cursor)
			}
			cursor = event.Seq
			advanced = true
			if !event.Accepted || event.After == nil {
				continue
			}
			for _, policy := range event.After.Policies {
				if policy.Name != "setup:continuation:"+id {
					continue
				}
				if policy.Kind != "data" || policy.Active {
					return result, false, fmt.Errorf("continuation invalid: seq %d policy kind %q active %t, want data and inactive", event.Seq, policy.Kind, policy.Active)
				}
				if err := json.Unmarshal([]byte(policy.Body), &result); err != nil {
					return result, false, fmt.Errorf("continuation invalid: seq %d: %s", event.Seq, edge.ParseFailure(err, []byte(policy.Body)))
				}
				if policy.Actor != result.Actor {
					return result, false, fmt.Errorf("continuation invalid: seq %d written by %q, records actor %q", event.Seq, edge.Redact(policy.Actor), edge.Redact(result.Actor))
				}
				count++
			}
		}
		if !advanced {
			return result, false, fmt.Errorf("continuation history incomplete: stopped at seq %d, snapshot at %d", cursor, snap.Seq)
		}
	}
	if count > 1 {
		return result, false, fmt.Errorf("continuation duplicated: %d records", count)
	}
	return result, count == 1, nil
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
func workloadUnavailable() error {
	return &verbs.OperationError{Problem: "worker setup is unavailable in this build", Fix: "install a build with the verified worker setup adapter", Exit: 2}
}
func (p productionSetup) configuredRuntime(ctx context.Context, in verbs.SetupInput, needDashboard bool) (verbs.RuntimeSetup, error) {
	r := p.forInput(in)
	host, saved, err := r.Prerequisites.Env.Choices.Get(ctx, "run-on")
	if err != nil {
		return r, err
	}
	if !saved {
		host = in.RunOn
		if host == "" {
			host = p.cfg.Host
		}
	}
	if host != p.cfg.Host {
		return r, p.remoteRunRefusal()
	}
	machines, err := (verbs.FleetServices{Command: p.command}).Machines(ctx)
	if err != nil {
		return r, serviceConfigFailure("cannot inspect machine inventory: ", err)
	}
	found := false
	for _, machine := range machines {
		if machine.Name == host && slices.Contains(machine.Roles, "services") {
			found = true
		}
	}
	if !found {
		return r, serviceConfigError("chosen run host is absent or lacks the services role")
	}
	rows, err := p.listServices(ctx)
	if err != nil {
		return r, err
	}
	run, err := p.resolveRunService(ctx, host, rows, machines)
	if err != nil {
		return r, err
	}
	if err := p.validateRunService(run); err != nil {
		return r, err
	}
	expectedDSN, err := setupRoleDSN(p.cfg.DSN, "nova_sprint")
	if err != nil || run.Env["NOVA_PG_DSN"] != expectedDSN {
		return r, serviceConfigError("sprint-run must name the configured passwordless sprint DSN")
	}
	r.RunService = run
	r.DefaultRunHost = host
	r.Services = verbs.FleetServices{Command: p.command}
	if !needDashboard {
		return r, nil
	}
	return p.configureDashboard(ctx, in, r, machines, rows, run)
}
func (p productionSetup) remoteRunRefusal() error {
	return &verbs.OperationError{Problem: "run on another host needs nova-fleet install of nova-sprint (nova-tools 2.0.x); for now run here", Fix: "nova-sprint up --run-on " + p.cfg.Host, Exit: 1}
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
	port, portErr := strconv.Atoi(busPort)
	if err != nil || portErr != nil || busHost == "" || port < 1 || port > 65535 {
		return verbs.FleetService{}, serviceConfigError("NOVA_BUS_REDIS must name the configured Redis host and port")
	}
	dsn, err := setupRoleDSN(p.cfg.DSN, "nova_sprint")
	if err != nil {
		return verbs.FleetService{}, serviceConfigError("configured sprint DSN is invalid")
	}
	// run records the chooser lock under the machine's nova-config name,
	// handed here, never under the raw hostname (run.ResolveMachine).
	env := map[string]string{"NOVA_PG_DSN": dsn, "NOVA_PG_PASSWORD_ENV": "NOVA_SPRINT_PG_PASSWORD", "NOVA_BUS_REDIS": p.cfg.BusRedis, run.MachineEnv: host}
	keys := []string{"NOVA_SPRINT_PG_PASSWORD"}
	if p.cfg.BusUser != "" {
		env["NOVA_BUS_USER"] = p.cfg.BusUser
		keys = append(keys, "NOVA_BUS_PASSWORD")
	}
	return verbs.FleetService{Name: "sprint-run", Machine: host, Seat: p.cfg.Secrets.Seat, Argv: []string{"nova-sprint", "run", "--listen", net.JoinHostPort(address, "8080")}, Keys: keys, Env: env}, nil
}

func (p productionSetup) configureDashboard(ctx context.Context, in verbs.SetupInput, r verbs.RuntimeSetup, machines []verbs.FleetMachine, rows []setupServiceRow, run verbs.FleetService) (verbs.RuntimeSetup, error) {
	choice, saved, err := r.Prerequisites.Env.Choices.Get(ctx, "dashboard")
	if err != nil {
		return r, err
	}
	if !saved {
		choice = in.Dash
	}
	if choice == "" {
		return r, nil
	}
	privateURL, err := privateDashboardURL(run.Argv[3])
	if err != nil {
		return r, err
	}
	target := verbs.DashboardTarget{URL: privateURL}
	if strings.HasPrefix(choice, "public=") {
		target, err = publicDashboardTarget(strings.TrimPrefix(choice, "public="), run.Machine, machines, rows, privateURL)
		if err != nil {
			return r, err
		}
	}
	if choice != "tailnet" && !strings.HasPrefix(choice, "public=") {
		return r, serviceConfigError("invalid dashboard choice")
	}
	r.Dashboards = map[string]verbs.DashboardTarget{choice: target}
	return r, nil
}

type publicInstaller interface {
	Install(context.Context, string, string) error
}

func (p productionSetup) ensurePublicVersion(ctx context.Context, r verbs.RuntimeSetup, in verbs.SetupInput, install bool) (bool, error) {
	choice, saved, err := r.Prerequisites.Env.Choices.Get(ctx, "dashboard")
	if err != nil {
		return false, err
	}
	if !saved {
		choice = in.Dash
	}
	if !strings.HasPrefix(choice, "public=") {
		return true, nil
	}
	target, ok := r.Dashboards[choice]
	if !ok || target.Service == nil {
		return false, serviceConfigError("public dashboard configuration unavailable")
	}
	return p.ensureServiceVersion(ctx, target.Service.Machine, install)
}

// ensureServiceVersion is true when the nova-sprint a service on machine
// will run is this binary's version. A probe that disagrees is (false,
// nil) unless install, which installs this version through nova-fleet and
// probes again; the probe's own refusal is returned when that fails too.
func (p productionSetup) ensureServiceVersion(ctx context.Context, machine string, install bool) (bool, error) {
	err := p.checkServiceVersion(ctx, machine)
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if !install {
		return false, nil
	}
	if p.installer == nil || !publicRelease.MatchString(p.cfg.BinaryVersion) {
		return false, err
	}
	if installErr := p.installer.Install(ctx, machine, p.cfg.BinaryVersion); installErr != nil {
		return false, serviceVersionRefusal(p.cfg.BinaryVersion, machine, "nova-fleet install on "+machine+" failed: "+installErr.Error())
	}
	if err = p.checkServiceVersion(ctx, machine); err != nil {
		return false, err
	}
	return true, nil
}

// serviceBinDir is where a service's bare nova-sprint is found first:
// nova-fleet's nova_bin_dir, the head of every service unit's PATH and
// where nova-fleet install puts a release.
const serviceBinDir = ".local/bin"

// checkServiceVersion is the one check that the nova-sprint a service on
// machine will run (<home>/.local/bin/nova-sprint) is this binary's
// version: run here when machine is this one, else over ssh to the
// inventory's address for it; its output read by the version verb's own
// parser (verbs.ParseVersionOutput).
func (p productionSetup) checkServiceVersion(ctx context.Context, machine string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	want := p.cfg.BinaryVersion
	if want == "" {
		return serviceVersionRefusal(want, machine, "this build has no version to compare")
	}
	var data []byte
	var err error
	destination := machine
	if run.SameMachine(machine, p.cfg.Host) {
		home := p.home
		if home == "" {
			home, err = os.UserHomeDir()
			if err != nil {
				return serviceVersionRefusal(want, machine, "home directory: "+err.Error())
			}
		}
		binary := filepath.Join(home, serviceBinDir, "nova-sprint")
		if data, err = p.smallCommand(ctx, binary, "version"); err != nil {
			return serviceVersionRefusal(want, machine, binary+" version: "+err.Error())
		}
	} else {
		if destination, err = p.sshDestination(ctx, machine); err != nil {
			return serviceVersionRefusal(want, machine, err.Error())
		}
		data, err = p.smallCommand(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=yes", "--", destination, "~/"+serviceBinDir+"/nova-sprint", "version")
		if err != nil {
			return serviceVersionRefusal(want, machine, "ssh "+destination+": "+err.Error())
		}
	}
	seen, err := verbs.ParseVersionOutput(data)
	if err != nil {
		seen = fmt.Sprintf("%q (not a v2 nova-sprint)", strings.TrimSpace(edge.FirstBytes(data)))
	}
	if seen != want {
		return serviceVersionRefusal(want, machine, destination+" reports "+seen+", this binary is "+want)
	}
	return nil
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
func serviceVersionRefusal(version, machine, why string) error {
	return &verbs.OperationError{Problem: edge.RedactN("nova-sprint on "+machine+" is not this version: "+why, edge.FailureMax), Fix: "nova-fleet install --repo <org/repo> --tag " + orWord(version, "<v>") + " " + machine + ", then nova-sprint up again", Exit: 1}
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

func setupCommandEnv(name string, env []string) []string {
	result := continuationEnv(env)
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
	number, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || number < 1 || number > 65535 {
		return "", serviceConfigError("sprint-run needs an explicit private-network listener")
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
	case slices.Contains(machine.Roles, "bench"):
		return verbs.DashboardTarget{}, verbs.PublicBenchRefusal(machine.Name)
	case machine.Public == "":
		return verbs.DashboardTarget{}, &verbs.OperationError{Problem: "machine " + machine.Name + " has no public address in nova-config", Fix: "nova-config machine set " + machine.Name + " --public <ip> --actor <you> --reason <why>", Exit: 1}
	}
	publicIP, err := netip.ParseAddr(machine.Public)
	if err != nil || !publicIP.IsGlobalUnicast() || publicIP.IsPrivate() || isTailnetIP(publicIP) {
		return verbs.DashboardTarget{}, &verbs.OperationError{Problem: edge.RedactN(fmt.Sprintf("machine %s's public address %q in nova-config is not a public IP", machine.Name, machine.Public), edge.FailureMax), Fix: "nova-config machine set " + machine.Name + " --public <ip> --actor <you> --reason <why>", Exit: 1}
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
		if err != nil || number < 1 || number > 65535 || strconv.Itoa(number) != port {
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
