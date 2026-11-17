package reserve

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
)

// OverlayRoster enriches bench workers in ro with live Tags and Resources from
// snap when nova-reserve subscribe is connected (R48, R49).
func OverlayRoster(ro roster.Roster, snap FleetSnapshot) roster.Roster {
	if !snap.Connected || len(ro.Workers) == 0 || len(snap.Machines) == 0 {
		return ro
	}
	byHost := snap.ByHost()
	workers := maps.Clone(ro.Workers)
	for name, w := range workers {
		m, ok := byHost[name]
		if !ok {
			continue
		}
		if len(m.Tags) > 0 {
			tags := slices.Clone(w.Tags)
			for _, tag := range m.Tags {
				if tag != "" && !slices.Contains(tags, tag) {
					tags = append(tags, tag)
				}
			}
			slices.Sort(tags)
			w.Tags = tags
		}
		if len(m.Resources) > 0 {
			res := maps.Clone(w.Resources)
			if res == nil {
				res = make(map[string]int, len(m.Resources))
			}
			for k, v := range m.Resources {
				res[k] = v
			}
			w.Resources = res
			if w.Held == roster.UnmeasuredResources && res["cpu"] >= 1 && res["memory"] >= 1 {
				w.Held = ""
			}
		}
		workers[name] = w
	}
	ro.Workers = workers
	return ro
}

// effectiveNeeds returns a lease or claim's resource vector on machine m,
// expanding Exclusive holds to m.Resources when Needs is empty.
func effectiveNeeds(needs map[string]int, exclusive bool, total map[string]int) map[string]int {
	if exclusive && len(needs) == 0 {
		return maps.Clone(total)
	}
	return needs
}

func isSprintHolder(holder, consumer string) bool {
	if strings.TrimSpace(consumer) == "" {
		return false
	}
	h := strings.ToLower(strings.TrimSpace(holder))
	return h == "bench" || h == "sprint"
}

// ToBenchReserve converts a live FleetSnapshot into per-bench pure inputs for
// modules/steps (R48-R54, R39b). When snap.Connected is false, nil is returned.
func ToBenchReserve(snap FleetSnapshot, now time.Time) map[string]steps.BenchReserve {
	if !snap.Connected {
		return nil
	}
	out := make(map[string]steps.BenchReserve, len(snap.Machines))
	for _, m := range snap.Machines {
		br := steps.BenchReserve{
			Offline:      !m.Online,
			PausedMillis: maps.Clone(snap.PausedMillis[m.Host]),
		}
		var extHeld, softClaimed, nonSprint map[string]int
		var sprintHold map[string]map[string]int
		activeCPU := 0

		for _, l := range m.Leases {
			if l.Sublease {
				continue // sub-leases carry 0 additional host debit (§6.2)
			}
			if l.Consumer != "" && l.PausedMs > 0 {
				if br.PausedMillis == nil {
					br.PausedMillis = map[string]int64{}
				}
				br.PausedMillis[l.Consumer] = max(br.PausedMillis[l.Consumer], l.PausedMs)
			}
			if strings.EqualFold(l.State, "paused") {
				br.AnyPaused = true
				continue
			}
			needs := effectiveNeeds(l.Needs, l.Exclusive, m.Resources)
			activeCPU += needs["cpu"]
			if isSprintHolder(l.Holder, l.Consumer) {
				if sprintHold == nil {
					sprintHold = map[string]map[string]int{}
				}
				if sprintHold[l.Consumer] == nil {
					sprintHold[l.Consumer] = map[string]int{}
				}
				for k, v := range needs {
					sprintHold[l.Consumer][k] += v
				}
				continue
			}
			if extHeld == nil {
				extHeld = map[string]int{}
			}
			if nonSprint == nil {
				nonSprint = map[string]int{}
			}
			for k, v := range needs {
				extHeld[k] += v
				nonSprint[k] += v
			}
		}

		for _, cl := range m.Claims {
			if cl.Expired(now) {
				continue
			}
			needs := effectiveNeeds(cl.Needs, cl.Exclusive, m.Resources)
			activeCPU += needs["cpu"]
			if softClaimed == nil {
				softClaimed = map[string]int{}
			}
			if nonSprint == nil {
				nonSprint = map[string]int{}
			}
			for k, v := range needs {
				softClaimed[k] += v
				nonSprint[k] += v
			}
		}

		br.ExternalHeld = extHeld
		br.SoftClaimed = softClaimed
		br.NonSprintUsed = nonSprint
		br.SprintHolding = sprintHold
		br.ActiveClaimedCPU = activeCPU
		out[m.Host] = br
	}
	return out
}

// DisplayLabel maps a lease onto the board's color vocabulary (R60, R61):
// ci, claim, blocker, critical, fix, read, work.
func DisplayLabel(holder, label, priority string) string {
	h := strings.ToLower(strings.TrimSpace(holder))
	l := strings.ToLower(strings.TrimSpace(label))
	p := strings.ToLower(strings.TrimSpace(priority))
	switch {
	case h == "ci" || l == "ci":
		return "ci"
	case h == "person" || l == "claim":
		return "claim"
	case l == "blocker" || l == "critical" || l == "fix" || l == "read" || l == "work":
		return l
	case p == "blocker":
		return "blocker"
	case p == "critical":
		return "critical"
	case p == "read":
		return "read"
	default:
		return "work"
	}
}
