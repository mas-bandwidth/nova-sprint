package verbs

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Simulation constructs explicit fake-mode adapters for check, up and clear.
// The adapters themselves perform no network calls or service operations.
func Simulation(st store.Store, actor, version, pin string, now Clock, preview func(context.Context, string) error, archive Archive) (*Engine, error) {
	if st == nil || now == nil || preview == nil || !validBuildVersion(version) {
		return nil, fmt.Errorf("simulation needs a store, clock, preview and build version")
	}
	if _, err := releaseVersion(pin); err != nil {
		return nil, fmt.Errorf("simulation needs a valid tools pin")
	}
	if archive == nil {
		if _, ok := st.(*store.Fake); ok {
			archive = &simulationArchive{store: st, backups: map[string]BackupInfo{}}
		}
	}
	choices := Choices{Store: st, Now: now}
	env := simulationEnvironment{choices: choices}
	setup := simulationSetup{choices: choices}
	engine := &Engine{Store: st, Env: env, Roles: SnapshotRoles{Store: st}, Actor: actor, Now: now, Version: version, ToolPin: pin, Health: simulationHealth{pin: pin}, Setup: setup, SetupLock: &simulationLock{}, Archive: archive, Preview: preview}
	engine.SetupHook = engine.EnsureCoordinatorHook
	return engine, nil
}

type simulationEnvironment struct{ choices Choices }

func (simulationEnvironment) Fleet(context.Context) (bool, string, error) { return true, "", nil }
func (simulationEnvironment) Schema(context.Context) (bool, error)        { return true, nil }
func (simulationEnvironment) Hooked(context.Context, string) (bool, string, error) {
	return true, "", nil
}
func (s simulationEnvironment) Coordinator(ctx context.Context) (string, error) {
	name, found, err := s.choices.Get(ctx, "coordinator")
	if err != nil {
		return "", err
	}
	if !found {
		return "", &OperationError{Problem: "simulation coordinator is not set", Fix: "nova-sprint up --as <name>", Exit: 1}
	}
	return name, nil
}

type simulationHealth struct{ pin string }

func (simulationHealth) Check(context.Context, bool) ([]OperationError, error) { return nil, nil }
func (s simulationHealth) NovaToolsVersion(context.Context) (string, error) {
	return "nova-version " + s.pin + " simulation", nil
}

type simulationSetup struct{ choices Choices }

func (s simulationSetup) Verify(ctx context.Context, step int, in SetupInput) (SetupCheck, error) {
	if step == 3 {
		name, found, err := s.choices.Get(ctx, "coordinator")
		if err != nil {
			return SetupCheck{}, err
		}
		if !found {
			if in.Actor == "" {
				return SetupCheck{Line: "choose a simulation coordinator", Next: "nova-sprint up --as <name>"}, nil
			}
			return SetupCheck{Action: true}, nil
		}
		if in.Actor != "" && in.Actor != name {
			return SetupCheck{Refusal: &OperationError{Problem: "simulation coordinator is " + name, Fix: "nova-sprint up --as " + name, Exit: 1}}, nil
		}
	}
	return SetupCheck{Verified: true, Line: fmt.Sprintf("UP %d/8 simulation: VERIFIED", step)}, nil
}
func (s simulationSetup) Act(ctx context.Context, step int, in SetupInput) error {
	if step != 3 {
		return fmt.Errorf("simulation has no setup effects")
	}
	s.choices.Actor = in.Actor
	return s.choices.Save(ctx, "coordinator", in.Actor)
}
func (s simulationSetup) Completion(ctx context.Context) (string, error) {
	name, found, err := s.choices.Get(ctx, "coordinator")
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("simulation coordinator is missing")
	}
	return "UP DONE run on simulation; dashboard simulation; 0 workers up; coordinator " + name, nil
}

type simulationLock struct{ mu sync.Mutex }

func (l *simulationLock) Acquire(context.Context) (SetupLease, error) {
	if !l.mu.TryLock() {
		return nil, &OperationError{Problem: "setup already running in simulation", Fix: "wait for setup to finish", Exit: 1}
	}
	return &simulationLease{lock: l}, nil
}

type simulationLease struct{ lock *simulationLock }

func (l *simulationLease) Monitor(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(ctx)
}
func (l *simulationLease) Check(ctx context.Context) error { return ctx.Err() }
func (l *simulationLease) Close() error                    { l.lock.mu.Unlock(); return nil }

type simulationArchive struct {
	mu      sync.Mutex
	store   store.Store
	backups map[string]BackupInfo
}

func (a *simulationArchive) Backup(ctx context.Context, _ string) (BackupInfo, error) {
	snap, err := a.store.Snapshot(ctx)
	if err != nil {
		return BackupInfo{}, err
	}
	fold := newEventFold()
	digest, err := digestEvents(ctx, a.store, snap.Seq, fold)
	if err != nil {
		return BackupInfo{}, err
	}
	if !reflect.DeepEqual(fold.result(), snap) {
		return BackupInfo{}, fmt.Errorf("simulation event rebuild differs")
	}
	identity, err := store.NewFake().Schema(ctx)
	if err != nil {
		return BackupInfo{}, err
	}
	info := BackupInfo{Path: fmt.Sprintf("simulation:e%d:s%d", snap.Epoch, snap.Seq), Epoch: snap.Epoch, Seq: snap.Seq, PrefixDigest: digest, SchemaHash: identity.Hash}
	a.mu.Lock()
	a.backups[info.Path] = info
	a.mu.Unlock()
	return info, nil
}
func (a *simulationArchive) Verify(_ context.Context, path string) (BackupInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	info, ok := a.backups[path]
	if !ok {
		return BackupInfo{}, fmt.Errorf("simulation backup does not exist")
	}
	return info, nil
}
func (a *simulationArchive) AcquireRestore(context.Context) (RestoreLease, error) {
	return nil, &OperationError{Problem: "simulation restore is unavailable", Fix: "use a fresh fake store", Exit: 1}
}
