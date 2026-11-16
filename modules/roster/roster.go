// Package roster is the sprint's view of who can work: the fleet's bench
// machines (workers) and the enabled friends (kin), read from nova-config,
// the one true source of that configuration. nova-sprint never copies it
// into its store (round 22: a store row is sprint state only); the deal
// guards take each row's tiers and resources from a Roster value instead.
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
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
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
	Name      string
	Roles     []string
	OS, Arch  string
	Tags      []string
	Resources map[string]int // includes "cpu" and "memory" when measured
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

// Worker is a bench machine dealt to: every tier, the worker harness, tags
// and resources. Held is UnmeasuredResources when cpu < 1 or memory < 1 in
// nova-config ("unmeasured resources in nova-config").
type Worker struct {
	Name      string
	Tiers     []string
	Tags      []string       // includes EffectiveOS ("mac" for "darwin") and Arch
	Resources map[string]int // "cpu", "memory", and custom resources
	Harness   string
	Held      string // UnmeasuredResources when cpu < 1 || memory < 1
}

// UnmeasuredResources is the Held reason of a bench with unmeasured resources in nova-config.
const UnmeasuredResources = "unmeasured resources in nova-config"

// Kin is an enabled friend dealt to: its own width, tiers, model and harness.
type Kin struct {
	Name, Model, Harness string
	Width                int
	Tiers                []string
}

// Refused is a machine or friend nova-config holds that this read could not
// load, and why; it is skipped, never guessed, and the rest of the roster
// loads. Kind is the kind it was read as (KindWorker for a machine, KindKin
// for a friend).
type Refused struct {
	Name, Kind, Reason string
}

// Roster is one read of the roster, keyed by row name. Refused is the rows
// that could not be loaded, in source order.
type Roster struct {
	Workers map[string]Worker
	Kin     map[string]Kin
	Refused []Refused
	// Listed is every name nova-config lists in this read, whatever its
	// roles or enabled flag: a machine that is not a bench and a friend
	// that is disabled are listed though neither is a row of the roster.
	// Names reads it; nil (a roster built by hand) adds nothing.
	Listed map[string]bool
}

// Names says whether nova-config names row name in this read: a worker, a
// kin, a refused row, or any machine or friend it lists (Listed). A store
// row it does not name is not in the fleet (Glenn, 2026-11-15: "the
// nova-sprint system and dash follows the set of machines defined
// automatically in nova-config"): the run retires it and the board leaves
// it out.
func (r Roster) Names(name string) bool {
	_, w := r.Workers[name]
	_, k := r.Kin[name]
	_, x := r.RefusedNamed(name)
	return w || k || x || r.Listed[name]
}

// Empty says whether the read has no worker and no kin: the roster of a
// failed read, or a nova-config with nothing in it. Nothing is judged absent
// from an empty roster: the run retires no row on it and the board shows
// every store row (a fault must never wipe the fleet).
func (r Roster) Empty() bool { return len(r.Workers) == 0 && len(r.Kin) == 0 }

// Clone is a deep copy: a reader that keeps a roster keeps its own.
func (r Roster) Clone() Roster {
	r.Workers = maps.Clone(r.Workers)
	for n, w := range r.Workers {
		w.Tiers = slices.Clone(w.Tiers)
		w.Tags = slices.Clone(w.Tags)
		w.Resources = maps.Clone(w.Resources)
		r.Workers[n] = w
	}
	r.Kin = maps.Clone(r.Kin)
	for n, k := range r.Kin {
		k.Tiers = slices.Clone(k.Tiers)
		r.Kin[n] = k
	}
	r.Refused = slices.Clone(r.Refused)
	r.Listed = maps.Clone(r.Listed)
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

// FitsMax reports whether worker w has every tag in n.Tags and w.Resources[k] >= v
// for every required resource in n.Amounts (R12).
func (w Worker) FitsMax(n card.Needs) bool {
	for _, tag := range n.Tags {
		if !slices.Contains(w.Tags, tag) {
			return false
		}
	}
	for k, v := range n.Amounts {
		if w.Resources[k] < v {
			return false
		}
	}
	return true
}

// EffectiveOS returns "mac" when os is "darwin", else lowercased os.
func EffectiveOS(os string) string {
	if strings.EqualFold(os, "darwin") {
		return "mac"
	}
	return strings.ToLower(os)
}

// WorkerTags derives the sorted unique tags for a machine, including its
// configured Tags, its EffectiveOS ("mac" for "darwin"), and its Arch.
func WorkerTags(m Machine) []string {
	var tags []string
	for _, t := range m.Tags {
		low := strings.ToLower(strings.TrimSpace(t))
		if low != "" && !slices.Contains(tags, low) {
			tags = append(tags, low)
		}
	}
	if eos := EffectiveOS(m.OS); eos != "" && !slices.Contains(tags, eos) {
		tags = append(tags, eos)
	}
	if arch := strings.ToLower(strings.TrimSpace(m.Arch)); arch != "" && !slices.Contains(tags, arch) {
		tags = append(tags, arch)
	}
	slices.Sort(tags)
	return tags
}

// Load reads src once: every machine with the bench role is a worker
// serving every tier, with tags (including derived OS/arch) and resources;
// every enabled friend is kin at its own width and tiers. A bench with
// unmeasured resources (cpu < 1 || memory < 1) loads Held UnmeasuredResources.
// Fleet-wide R5 is enforced: no name may be both a tag and a resource.
func Load(ctx context.Context, src Source) (Roster, error) {
	ms, err := src.Machines(ctx)
	if err != nil {
		return Roster{}, fmt.Errorf("roster: machines: %w", err)
	}
	fs, err := src.Friends(ctx)
	if err != nil {
		return Roster{}, fmt.Errorf("roster: friends: %w", err)
	}
	r := Roster{Workers: map[string]Worker{}, Kin: map[string]Kin{}, Listed: map[string]bool{}}
	for _, m := range ms {
		r.Listed[m.Name] = true
	}
	for _, f := range fs {
		r.Listed[f.Name] = true
	}
	delete(r.Listed, "")
	r.addWorkers(ms)
	r.addKin(fs)
	return r, nil
}

func (r *Roster) refuse(name, kind, reason string) {
	r.Refused = append(r.Refused, Refused{Name: name, Kind: kind, Reason: reason})
}

func (r *Roster) addWorkers(ms []Machine) {
	// Fleet-wide R5 enforcement: collect tags and resources across all machines.
	tagMachines := map[string][]string{}
	resMachines := map[string][]string{}
	for _, m := range ms {
		tags := WorkerTags(m)
		for _, t := range tags {
			tagMachines[t] = append(tagMachines[t], m.Name)
		}
		for res := range m.Resources {
			resLow := strings.ToLower(strings.TrimSpace(res))
			if resLow != "" {
				resMachines[resLow] = append(resMachines[resLow], m.Name)
			}
		}
	}

	conflicts := map[string]string{} // machine name -> conflicting token
	for name, mNames := range tagMachines {
		if resMNames, exists := resMachines[name]; exists {
			for _, mn := range append(slices.Clone(mNames), resMNames...) {
				if _, ok := conflicts[mn]; !ok {
					conflicts[mn] = name
				}
			}
		}
	}

	for _, m := range ms {
		if !slices.Contains(m.Roles, "bench") {
			continue
		}
		if why := r.fresh(m.Name); why != "" {
			r.refuse(m.Name, KindWorker, "machine "+why)
			continue
		}
		if conflictName, conflicted := conflicts[m.Name]; conflicted {
			r.refuse(m.Name, KindWorker, fmt.Sprintf("machine %s: %q appears as both a tag and a resource in the fleet (R5)", m.Name, conflictName))
			continue
		}
		tags := WorkerTags(m)
		resources := maps.Clone(m.Resources)
		if resources == nil {
			resources = map[string]int{}
		}

		w := Worker{
			Name:      m.Name,
			Tiers:     slices.Clone(store.Tiers),
			Tags:      tags,
			Resources: resources,
			Harness:   WorkerHarness,
		}
		if w.Resources["cpu"] < 1 || w.Resources["memory"] < 1 {
			w.Held = UnmeasuredResources
		}
		r.Workers[m.Name] = w
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

// Row is what callers read for a row: its kind (KindWorker or KindKin), width
// (for kin; 0 for worker) and tiers; ok is false for a name not in the roster.
func (r Roster) Row(name string) (kind string, width int, tiers []string, ok bool) {
	if w, ok := r.Workers[name]; ok {
		return KindWorker, 0, slices.Clone(w.Tiers), true
	}
	if k, ok := r.Kin[name]; ok {
		return KindKin, k.Width, slices.Clone(k.Tiers), true
	}
	return "", 0, nil, false
}

// Advertised is r with each worker's tiers as its own row says it can serve:
// a worker's heartbeat advertises the tiers its harness adapter serves (store.Row Tiers).
// A worker whose row's binary is behind version, the run's release (Behind),
// serves no tier. Kin are untouched.
func (r Roster) Advertised(rows map[string]store.Row, version string) Roster {
	out := r.Clone()
	for name, w := range out.Workers {
		row, ok := rows[name]
		if ok && row.Tiers != "" {
			w.Tiers = store.SplitTiers(row.Tiers)
		}
		if Behind(row.Version, version) {
			w.Tiers = nil
		}
		out.Workers[name] = w
	}
	return out
}

// Behind says whether have, a row's binary release, is below want, the
// run's: never when want is not a release vMAJOR.MINOR.PATCH (a dev build
// pins nothing); always when have is not one (a binary that did not say).
func Behind(have, want string) bool {
	w, ok := release(want)
	if !ok {
		return false
	}
	h, ok := release(have)
	return !ok || slices.Compare(h[:], w[:]) < 0
}

// release is v as vMAJOR.MINOR.PATCH's three numbers, false when it is not one.
func release(v string) ([3]uint64, bool) {
	var out [3]uint64
	rest, ok := strings.CutPrefix(v, "v")
	parts := strings.Split(rest, ".")
	if !ok || len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil || strings.Trim(p, "0123456789") != "" || (len(p) > 1 && p[0] == '0') {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
