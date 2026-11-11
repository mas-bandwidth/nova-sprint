package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/worker"
	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/run"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
	config "github.com/mas-bandwidth/nova-tools/v2/config/lib"
)

// credentials are the store's: NOVA_PG_DSN without a password, its user
// rewritten to nova_sprint, and NOVA_SPRINT_PG_PASSWORD, which up mints and
// the fleet's units inject.
type credentials struct{ dsn, password string }

func credentialsFromEnv() credentials {
	return credentials{dsn: os.Getenv("NOVA_PG_DSN"), password: os.Getenv("NOVA_SPRINT_PG_PASSWORD")}
}

// sprint is the DSN as nova_sprint, or the refusal of a process without the
// sprint's own credential (verbs.NoCredential: the store may well be set
// up; "not set up" is for a store without the schema, defect 22). A process
// with neither the DSN nor the credential names no store at all
// (verbs.NoStore: up is its next command, never nova-secrets exec).
func (c credentials) sprint() (string, error) {
	if c.password == "" && c.dsn == "" {
		return "", verbs.NoStore("")
	}
	if c.password == "" {
		return "", verbs.NoCredential()
	}
	dsn, err := setupRoleDSN(c.dsn, "nova_sprint")
	if err != nil {
		return "", fmt.Errorf("NOVA_PG_DSN is not a passwordless postgres URL (%s): set it so", edge.RedactN(err.Error(), edge.CommandMax))
	}
	return dsn, nil
}

// newEngine is the one way the binary opens what the verbs read and write:
// the store and nova-config lazily (help and version open nothing), the
// bus, the policy engine, the git and forge edges, the doctor, the
// archive, the long-running services and the up walk.
func newEngine(args []string, phase string) (*verbs.Engine, func()) {
	actor, creds := os.Getenv("NOVA_SPRINT_AS"), credentialsFromEnv()
	st := &lazyStore{creds: creds}
	ro := &lazyRoster{creds: creds}
	pol := policy.NewEngine(policy.RealClock{})
	git, fg := gitEdge(), forgeEdge()
	e := &verbs.Engine{Store: st, Bus: busEdge(), Roster: ro, Answers: pol, Policy: pol, Origin: &origin{git: git, dir: reposDir()}, Forge: hostForge{exec: edge.NewOSExec(), land: fg},
		Doctor: doctor{creds: creds}, Archive: newArchive(creds), Now: time.Now, Stdin: os.Stdin, Actor: actor, Version: versionString(), WorkerBuild: os.Getenv(worker.BuildEnv), ToolPin: toolPin}
	e.Lander = &lander{clones: &origin{git: git, dir: filepath.Join(stateDir(), "land")}, forge: fg}
	e.Services = services{creds: creds, store: st, roster: ro, policy: pol, git: git, forge: fg, admit: e.ReproveStream}
	env := verbs.ExecEnvironment{DSN: creds.dsn, Password: creds.password, Choices: verbs.Choices{Store: st, Actor: actor, Now: time.Now}, Command: toolCommand}
	e.Setup, e.Lock = buildProductionSetup(st, env, actor, toolPin)
	e.Doctor = fleetDoctor(creds, e.Setup)
	if len(args) > 0 && args[0] == "up" {
		e.Bus = setupPresence{}
	}
	if phase == "phase1" {
		e.ReportContinuation = reportHandoff
	}
	return e, func() { st.Close(); ro.Close() }
}

// fleetDoctor is the doctor with the setup walk's view of the fleet, through
// which check and show read the worker benches (doctor.Workers).
func fleetDoctor(creds credentials, setup verbs.Setup) doctor {
	d := doctor{creds: creds}
	switch p := setup.(type) {
	case productionSetup:
		d.fleet = &p
	case failedProductionSetup:
		d.fleetErr = p.err
	}
	return d
}

// busEdge is nova-bus with its connection only.
func busEdge() *bus.Edge {
	return bus.New(bus.Config{Bin: busBinary(), Env: toolEnvironment(os.Environ(), "nova-bus")})
}

// stateDir is nova-sprint's own state: ~/.local/state/nova-sprint.
func stateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".local", "state", "nova-sprint")
}

// reposDir is where the verbs and run keep one clone per stream repository.
func reposDir() string { return filepath.Join(stateDir(), "repos") }

// newArchive is backup and restore on the store, the restore under the
// chooser lock.
func newArchive(c credentials) verbs.Archive {
	dsn, err := c.sprint()
	if err != nil {
		dsn = ""
	}
	return verbs.NewPGArchive(dsn, c.password, versionString(), os.Getenv("NOVA_SPRINT_BACKUP_DIR"), func(ctx context.Context) (run.RestoreLock, error) {
		if dsn == "" {
			return nil, err
		}
		host, _ := os.Hostname()
		if machine := os.Getenv(run.MachineEnv); machine != "" {
			host = machine
		}
		return run.OpenRestoreLock(ctx, dsn, c.password, run.MachineName(host), os.Getpid())
	})
}

// lazyStore connects on the first verb that reads or writes.
type lazyStore struct {
	mu    sync.Mutex
	creds credentials
	pg    *store.PG
}

func (s *lazyStore) get(ctx context.Context) (*store.PG, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pg != nil {
		return s.pg, nil
	}
	dsn, err := s.creds.sprint()
	if err != nil {
		return nil, err
	}
	openCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if s.pg, err = store.Open(openCtx, dsn, s.creds.password); err != nil {
		return nil, err
	}
	return s.pg, nil
}

func (s *lazyStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pg != nil {
		s.pg.Close()
	}
}

func (s *lazyStore) Snapshot(ctx context.Context) (store.Snapshot, error) {
	return with(s, ctx, func(p *store.PG) (store.Snapshot, error) { return p.Snapshot(ctx) })
}

func (s *lazyStore) Read(ctx context.Context, scope store.Scope) (store.Snapshot, error) {
	return with(s, ctx, func(p *store.PG) (store.Snapshot, error) { return p.Read(ctx, scope) })
}

// Landings forwards the board's landing times to the store (dash and run look
// for it by interface): without this the run's board had no ETA (2026-11-07).
func (s *lazyStore) Landings(ctx context.Context, epoch int, upToSeq int64) ([]time.Time, error) {
	return with(s, ctx, func(p *store.PG) ([]time.Time, error) { return p.Landings(ctx, epoch, upToSeq) })
}

// StatsSince forwards the board's stats mark to the store, as Landings does:
// run looks for it by interface.
func (s *lazyStore) StatsSince(ctx context.Context, mark int64) (store.StatsMark, error) {
	return with(s, ctx, func(p *store.PG) (store.StatsMark, error) { return p.StatsSince(ctx, mark) })
}

func (s *lazyStore) Apply(ctx context.Context, o store.Op) (store.Receipt, error) {
	return with(s, ctx, func(p *store.PG) (store.Receipt, error) { return p.Apply(ctx, o) })
}

func (s *lazyStore) Clear(ctx context.Context, o store.Op) (store.Receipt, error) {
	return with(s, ctx, func(p *store.PG) (store.Receipt, error) { return p.Clear(ctx, o) })
}

func (s *lazyStore) Events(ctx context.Context, seq int64, max int) ([]store.Event, error) {
	return with(s, ctx, func(p *store.PG) ([]store.Event, error) { return p.Events(ctx, seq, max) })
}

func (s *lazyStore) Policy(ctx context.Context, v int) (store.PolicyVersion, error) {
	return with(s, ctx, func(p *store.PG) (store.PolicyVersion, error) { return p.Policy(ctx, v) })
}

func (s *lazyStore) Replay(ctx context.Context, id string) (store.Receipt, bool, error) {
	p, err := s.get(ctx)
	if err != nil {
		return store.Receipt{}, false, err
	}
	return p.Replay(ctx, id)
}

func (s *lazyStore) Restored(ctx context.Context) (string, time.Time, bool, error) {
	p, err := s.get(ctx)
	if err != nil {
		return "", time.Time{}, false, err
	}
	return p.Restored(ctx)
}

func with[T any](s *lazyStore, ctx context.Context, f func(*store.PG) (T, error)) (T, error) {
	p, err := s.get(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	return f(p)
}

// lazyRoster is nova-config, connected on the first read.
type lazyRoster struct {
	mu     sync.Mutex
	creds  credentials
	reader *run.ConfigRoster
}

func (l *lazyRoster) open(ctx context.Context, widthCap int) (*run.ConfigRoster, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reader != nil {
		return l.reader, nil
	}
	dsn, err := l.creds.sprint()
	if err != nil {
		return nil, err
	}
	openCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := config.Open(openCtx, dsn, l.creds.password)
	if err != nil {
		return nil, err
	}
	if l.reader, err = run.NewConfigRoster(conn, widthCap); err != nil {
		conn.Close()
		return nil, err
	}
	return l.reader, nil
}

// ReadRosterAt is the roster at the active policy's width cap.
func (l *lazyRoster) ReadRosterAt(ctx context.Context, widthCap int) (roster.Roster, error) {
	r, err := l.open(ctx, widthCap)
	if err != nil {
		return roster.Roster{}, err
	}
	return r.ReadRosterAt(ctx, widthCap)
}

func (l *lazyRoster) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reader != nil {
		l.reader.Close()
		l.reader = nil
	}
}
