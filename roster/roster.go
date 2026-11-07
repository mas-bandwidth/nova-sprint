// Package roster is the sprint's view of who can work: the fleet's bench
// machines (workers) and the enabled friends (kin), read from nova-config,
// the one true source of that configuration. nova-sprint never copies it
// into its store (round 22: a store row is sprint state only); the deal
// guards take each row's width and tiers from a Roster value instead.
//
// The roster is read through a Source. The real one reads nova-config's
// PostgreSQL through nova-tools' config package (source.go). Layer 6 owns
// that read: it opens a pool of its own, with the same credential the
// store uses, and reads the roster in one REPEATABLE READ transaction.
// store.PG has no read seam for it and gets none: the store's pool reads
// the store, and nothing else.
package roster

import (
	"context"
	"fmt"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// FleetWidthCap is the default most consumers a fleet machine works at
// once: a worker's width is min(cores, cap). Policy owns the number (Glenn,
// 2026-11-06: every fleet machine runs at width 8); Load takes it as an
// argument so policy can pass its own.
const FleetWidthCap = 8

// WorkerHarness is the harness every worker runs.
const WorkerHarness = "nova-worker"

// The kinds Row reports.
const (
	KindWorker = "worker"
	KindKin    = "kin"
)

// Machine is a fleet machine as nova-config holds it.
type Machine struct {
	Name     string
	Roles    []string
	Cores    int
	OS, Arch string
}

// Friend is a friend as nova-config holds it. Width is the friend's own
// width (no cap).
type Friend struct {
	Name, Harness, Model string
	Width                int
	Tiers                []string
	Pronouns             string
	Enabled              bool
}

// Source reads the machines and friends from nova-config.
type Source interface {
	Machines(ctx context.Context) ([]Machine, error)
	Friends(ctx context.Context) ([]Friend, error)
}

// Worker is a bench machine dealt to: every tier, the worker harness.
type Worker struct {
	Name    string
	Width   int
	Tiers   []string
	Harness string
}

// Kin is an enabled friend dealt to: its own width, tiers, model and
// harness.
type Kin struct {
	Name, Model, Harness string
	Width                int
	Tiers                []string
}

// Roster is one read of the roster, keyed by row name.
type Roster struct {
	Workers map[string]Worker
	Kin     map[string]Kin
}

// Load reads src once: every machine with the bench role is a worker at
// width min(cores, cap) serving every tier; every enabled friend is kin at
// its own width and tiers. cap is FleetWidthCap unless policy says
// otherwise. A name given twice, a cap below 1, a machine with no cores, a
// negative width and an unknown tier are errors: the roster is not guessed.
func Load(ctx context.Context, src Source, cap int) (Roster, error) {
	if cap < 1 {
		return Roster{}, fmt.Errorf("roster: width cap %d is below 1", cap)
	}
	ms, err := src.Machines(ctx)
	if err != nil {
		return Roster{}, fmt.Errorf("roster: machines: %w", err)
	}
	fs, err := src.Friends(ctx)
	if err != nil {
		return Roster{}, fmt.Errorf("roster: friends: %w", err)
	}
	r := Roster{Workers: map[string]Worker{}, Kin: map[string]Kin{}}
	if err := r.addWorkers(ms, cap); err != nil {
		return Roster{}, err
	}
	if err := r.addKin(fs); err != nil {
		return Roster{}, err
	}
	return r, nil
}

func (r Roster) addWorkers(ms []Machine, cap int) error {
	for _, m := range ms {
		if !slices.Contains(m.Roles, "bench") {
			continue
		}
		if err := r.fresh(m.Name); err != nil {
			return err
		}
		if m.Cores < 1 {
			return fmt.Errorf("roster: machine %s has %d cores", m.Name, m.Cores)
		}
		r.Workers[m.Name] = Worker{Name: m.Name, Width: min(m.Cores, cap), Tiers: slices.Clone(store.Tiers), Harness: WorkerHarness}
	}
	return nil
}

func (r Roster) addKin(fs []Friend) error {
	for _, f := range fs {
		if !f.Enabled {
			continue
		}
		if err := r.fresh(f.Name); err != nil {
			return err
		}
		if f.Width < 0 {
			return fmt.Errorf("roster: friend %s has width %d", f.Name, f.Width)
		}
		for _, t := range f.Tiers {
			if !slices.Contains(store.Tiers, t) {
				return fmt.Errorf("roster: friend %s has tier %q, not one of %v", f.Name, t, store.Tiers)
			}
		}
		r.Kin[f.Name] = Kin{Name: f.Name, Model: f.Model, Harness: f.Harness, Width: f.Width, Tiers: slices.Clone(f.Tiers)}
	}
	return nil
}

// fresh refuses an empty name and one already in the roster.
func (r Roster) fresh(name string) error {
	_, w := r.Workers[name]
	_, k := r.Kin[name]
	switch {
	case name == "":
		return fmt.Errorf("roster: a machine or friend with no name")
	case w || k:
		return fmt.Errorf("roster: %s is named twice", name)
	}
	return nil
}

// Row is what the deal guards read for a row: its kind (KindWorker or
// KindKin), width and tiers; ok is false for a name not in the roster.
func (r Roster) Row(name string) (kind string, width int, tiers []string, ok bool) {
	if w, ok := r.Workers[name]; ok {
		return KindWorker, w.Width, slices.Clone(w.Tiers), true
	}
	if k, ok := r.Kin[name]; ok {
		return KindKin, k.Width, slices.Clone(k.Tiers), true
	}
	return "", 0, nil, false
}
