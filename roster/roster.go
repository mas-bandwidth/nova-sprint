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
	"maps"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/store"
)

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

// Refused is a machine or friend nova-config holds that this read could not
// load, and why; it is skipped, never guessed, and the rest of the roster
// loads (one bench with 0 cores once emptied the whole board). Kind is the
// kind it was read as (KindWorker for a machine, KindKin for a friend).
type Refused struct {
	Name, Kind, Reason string
}

// Roster is one read of the roster, keyed by row name. Refused is the rows
// that could not be loaded, in source order.
type Roster struct {
	Workers map[string]Worker
	Kin     map[string]Kin
	Refused []Refused
}

// Clone is a deep copy: a reader that keeps a roster keeps its own.
func (r Roster) Clone() Roster {
	r.Workers = maps.Clone(r.Workers)
	for n, w := range r.Workers {
		w.Tiers = slices.Clone(w.Tiers)
		r.Workers[n] = w
	}
	r.Kin = maps.Clone(r.Kin)
	for n, k := range r.Kin {
		k.Tiers = slices.Clone(k.Tiers)
		r.Kin[n] = k
	}
	r.Refused = slices.Clone(r.Refused)
	return r
}

// RefusedNamed is the refusal of row name, if this read refused it.
func (r Roster) RefusedNamed(name string) (Refused, bool) {
	for _, x := range r.Refused {
		if x.Name == name {
			return x, true
		}
	}
	return Refused{}, false
}

// Load reads src once: every machine with the bench role is a worker at
// width min(cores, cap) serving every tier; every enabled friend is kin at
// its own width and tiers. cap is policy's limits().fleet_width_cap (drift
// row 26; the built-in says 8). A cap below 1 and a failed read are errors.
// A row that cannot be loaded (no name, a name given twice, a bench with no
// cores, a negative width, an unknown tier) is skipped and named in Refused:
// the roster is not guessed, and one bad row never empties it.
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
	r.addWorkers(ms, cap)
	r.addKin(fs)
	return r, nil
}

func (r *Roster) refuse(name, kind, reason string) {
	r.Refused = append(r.Refused, Refused{Name: name, Kind: kind, Reason: reason})
}

func (r *Roster) addWorkers(ms []Machine, cap int) {
	for _, m := range ms {
		if !slices.Contains(m.Roles, "bench") {
			continue
		}
		if why := r.fresh(m.Name); why != "" {
			r.refuse(m.Name, KindWorker, "machine "+why)
			continue
		}
		if m.Cores < 1 {
			r.refuse(m.Name, KindWorker, fmt.Sprintf("machine %s has %d cores; set its cores in nova-config", m.Name, m.Cores))
			continue
		}
		r.Workers[m.Name] = Worker{Name: m.Name, Width: min(m.Cores, cap), Tiers: slices.Clone(store.Tiers), Harness: WorkerHarness}
	}
}

func (r *Roster) addKin(fs []Friend) {
	for _, f := range fs {
		if !f.Enabled {
			continue
		}
		if why := r.fresh(f.Name); why != "" {
			r.refuse(f.Name, KindKin, "friend "+why)
			continue
		}
		if f.Width < 0 {
			r.refuse(f.Name, KindKin, fmt.Sprintf("friend %s has width %d", f.Name, f.Width))
			continue
		}
		if i := slices.IndexFunc(f.Tiers, func(t string) bool { return !slices.Contains(store.Tiers, t) }); i >= 0 {
			r.refuse(f.Name, KindKin, fmt.Sprintf("friend %s has tier %q, not one of %v", f.Name, f.Tiers[i], store.Tiers))
			continue
		}
		r.Kin[f.Name] = Kin{Name: f.Name, Model: f.Model, Harness: f.Harness, Width: f.Width, Tiers: slices.Clone(f.Tiers)}
	}
}

// fresh is why name cannot be added (empty, or already in the roster or
// already refused), or "".
func (r Roster) fresh(name string) string {
	_, w := r.Workers[name]
	_, k := r.Kin[name]
	_, x := r.RefusedNamed(name)
	switch {
	case name == "":
		return "with no name"
	case w || k || x:
		return name + " is named twice"
	}
	return ""
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
