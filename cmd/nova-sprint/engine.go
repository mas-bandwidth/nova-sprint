package main

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/dash"
	"github.com/mas-bandwidth/nova-sprint/run"
	"github.com/mas-bandwidth/nova-sprint/run/filestore"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

// toolPin is set by release builds with -ldflags.
var toolPin = "v2.0.2"

func versionString() string {
	if binaryVersion != "dev" {
		return binaryVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		return sourceVersion(info.Settings)
	}
	return "dev"
}
func sourceVersion(settings []debug.BuildSetting) string {
	revision := ""
	clean := false
	for _, setting := range settings {
		if setting.Key == "vcs.revision" {
			revision = setting.Value
		}
		if setting.Key == "vcs.modified" && setting.Value == "false" {
			clean = true
		}
	}
	if !clean || len(revision) < 12 {
		return "dev"
	}
	return "dev+" + revision[:12]
}

func executeVerb(ctx context.Context, args []string, phase string) int {
	mode, actor := os.Getenv("NOVA_SPRINT_STORE"), os.Getenv("NOVA_SPRINT_AS")
	st, env, closeStore, err := openCommandStore(mode, actor)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer closeStore()
	preview := func(_ context.Context, text string) error { _, err := fmt.Fprint(os.Stdout, text); return err }
	var e *verbs.Engine
	if mode == "" {
		e = &verbs.Engine{Store: st, Env: env, Actor: actor, Now: time.Now, Reports: verbs.CardReportPlanner{}, Version: versionString(), ToolPin: toolPin, Preview: preview}
		configureProduction(e, st, env.(verbs.ExecEnvironment))
		if closer, ok := e.Roster.(interface{ Close() }); ok { defer closer.Close() }
		if phase == "phase1" {
			e.ReportContinuation = reportHandoff
		}
	} else {
		e, err = simulationEngine(st, mode, actor, preview)
		if err != nil {
			fmt.Fprintln(os.Stderr, "SPRINT REFUSED: fake mode needs a valid build version")
			return 2
		}
		if len(args) == 0 || args[0] != "version" {
			fmt.Fprintln(os.Stderr, "FAKE MODE: local test store; external checks simulated")
		}
	}
	result := e.Execute(ctx, args)
	if _, err = fmt.Fprint(os.Stdout, result.Output); err != nil {
		return 2
	}
	return result.Exit
}
func openCommandStore(mode, actor string) (store.Store, verbs.Environment, func(), error) {
	if strings.HasPrefix(mode, "fake:") {
		f, err := filestore.Open(strings.TrimPrefix(mode, "fake:"))
		return f, nil, func() {}, err
	}
	if mode != "" {
		return nil, nil, func() {}, fmt.Errorf("SPRINT REFUSED: invalid NOVA_SPRINT_STORE: use fake:<path> or leave it unset")
	}
	dsn, password := os.Getenv("NOVA_PG_DSN"), os.Getenv("NOVA_SPRINT_PG_PASSWORD")
	if password != "" {
		if sprintDSN, err := setupRoleDSN(dsn, "nova_sprint"); err == nil {
			dsn = sprintDSN
		}
	}
	pg := &lazyStore{dsn: dsn, password: password}
	choices := verbs.Choices{Store: pg, Actor: actor, Now: time.Now}
	return pg, verbs.ExecEnvironment{DSN: dsn, Password: password, Choices: choices, Command: toolCommand}, pg.Close, nil
}
func simulationEngine(st store.Store, mode, actor string, preview func(context.Context, string) error) (*verbs.Engine, error) {
	archive := fakeArchive{source: st, directory: fakeBackupDir(strings.TrimPrefix(mode, "fake:"))}
	e, err := verbs.Simulation(st, actor, versionString(), toolPin, time.Now, preview, archive)
	if err != nil {
		return nil, err
	}
	e.Reports = verbs.CardReportPlanner{}
	e.Repositories = localRepositories{}
	e.Briefs = verbs.FileBriefs{Verifier: localBriefVerifier{}}
	e.History = fakeHistoryVerifier{store: st}
	return e, nil
}
func configureProduction(e *verbs.Engine, st store.Store, env verbs.ExecEnvironment) {
	if cfg, err := productionSetupConfigFromEnv(); err == nil {
		env.Command = setupCommandWithBus(cfg, productionFleetCommand(cfg))
	} else {
		env.Command = func(context.Context, string, ...string) ([]byte, error) { return nil, err }
	}
	e.Env = credentialEnvironment{inner: env}
	e.Roles = verbs.SnapshotRoles{Store: st}
	e.SetupHook = e.EnsureCoordinatorHook
	e.Roster = &lazyRoster{dsn: env.DSN, password: env.Password}
	e.Install = verbs.ReleaseInstaller{}
	e.PolicyFiles = verbs.LocalPolicyFiles{}
	e.RunInfo = pgRunInfo{dsn: env.DSN, password: env.Password}
	e.Health = commandHealth{env: env}
	e.History = liveHistoryVerifier{dsn: env.DSN, password: env.Password}
	e.Setup, e.SetupLock = buildProductionSetup(st, env, e.Actor, toolPin)
	if env.Password == "" {
		return
	}
	dsn, err := setupRoleDSN(env.DSN, "nova_sprint")
	if err != nil {
		return
	}
	e.Archive = verbs.NewPGArchive(dsn, env.Password, e.Version, os.Getenv("NOVA_SPRINT_BACKUP_DIR"), func(ctx context.Context) (verbs.RestoreLease, error) {
		host, _ := os.Hostname()
		lock, err := run.OpenRestoreLock(ctx, dsn, env.Password, host, os.Getpid())
		if err != nil {
			return nil, err
		}
		return restoreLease{lock: lock}, nil
	})
}

type restoreLease struct{ lock run.RestoreLock }

func (l restoreLease) Check(ctx context.Context) error { return l.lock.Check(ctx) }
func (l restoreLease) Close() error                    { return l.lock.Close(context.Background()) }
func (l restoreLease) Restore(ctx context.Context, d *verbs.VerifiedDump) error {
	return l.lock.Restore(ctx, func(w run.RestoreWriter) error { return d.Execute(ctx, w) })
}

type localRepositories struct{}

func (localRepositories) Readable(context.Context, string, string, bool) error { return nil }

type localBriefVerifier struct{}

func (localBriefVerifier) VerifyBrief(context.Context, store.Stream, card.Add, []string) error {
	return nil
}

func executeDash(ctx context.Context, args []string) error {
	mode := os.Getenv("NOVA_SPRINT_STORE")
	if mode == "" {
		return dash.Command(ctx, args)
	}
	if !strings.HasPrefix(mode, "fake:") {
		return fmt.Errorf("dashboard: invalid NOVA_SPRINT_STORE")
	}
	s, err := filestore.Open(strings.TrimPrefix(mode, "fake:"))
	if err != nil {
		return err
	}
	return dash.ServeTestStore(ctx, args, s)
}
