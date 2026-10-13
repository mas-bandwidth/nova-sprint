package sprint

import (
	"fmt"
	"sort"
	"strings"
)

// Provider is a provider row nova-config applied. A missing row is unknown,
// distinct from an applied row whose Funded is false.
type Provider struct {
	Name    string `json:"name"`
	Billing string `json:"billing"`
	Funded  bool   `json:"funded"`
	Problem string `json:"problem,omitempty"` // malformed declaration, never a funded row
}

// RouteGap names an enabled, funded route absent from every effective array.
type RouteGap struct {
	Route    string `json:"route"`
	Provider string `json:"provider"`
	Tier     string `json:"tier"`
}

// ProviderTier counts enabled native-tier routes and enabled routes included in
// this tier's effective array, each once. Foreign-tier entries are ignored as
// servedRoute ignores them when the deal draws this tier.
type ProviderTier struct {
	Tier    string `json:"tier"`
	In      int    `json:"in"`
	Enabled int    `json:"enabled"`
}

// ProviderCoverage is one applied provider and its coverage by the deal's arrays.
type ProviderCoverage struct {
	Provider
	Tiers []ProviderTier `json:"tiers"`
}

// ProviderGapReport is the same sprint-wide gaps the check and where report.
// ProviderProblem is one named malformed applied row, independent of its route count.
type ProviderProblem struct {
	Name    string `json:"name"`
	Problem string `json:"problem"`
}

type ProviderGapReport struct {
	InvalidProviders []ProviderProblem `json:"invalid_providers,omitempty"`
	Routes           []RouteGap        `json:"routes"`
	Tiers            []string          `json:"tiers"`
}

// providerTiers includes native tiers and explicitly configured arrays. Frontier
// has no configured tier row, but its enabled native routes still serve it.
func (s *Snapshot) providerTiers() []string {
	seen := map[string]bool{}
	for t := range s.Tiers {
		seen[t] = true
	}
	for _, r := range s.Routes {
		if r.Tier != "" {
			seen[r.Tier] = true
		}
	}
	var out []string
	for t := range seen {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		rank := func(t string) int {
			switch t {
			case "flash":
				return 0
			case "pro":
				return 1
			case "heavy":
				return 2
			case "frontier":
				return 3
			}
			return 4
		}
		if rank(out[i]) != rank(out[j]) {
			return rank(out[i]) < rank(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

// ProviderGaps checks the applied provider rows against the arrays as the deal
// reads them. tier limits findings to that native route tier and that array;
// membership considers every array, including empty arrays' fallback, but an
// entry covers only its native tier, as the deal's servedRoute requires.
func (s *Snapshot) ProviderGaps(tier string) (routes []RouteGap, tiers []string) {
	routes, tiers = []RouteGap{}, []string{}
	included := map[string]bool{}
	byName := map[string]Route{}
	for _, r := range s.Routes {
		byName[r.Name] = r
	}
	for _, t := range s.providerTiers() {
		funded := false
		for _, name := range s.tierArray(t) {
			r, ok := byName[name]
			if !ok || !r.Enabled || r.Tier != t {
				continue
			}
			included[name] = true
			if s.ProviderFunded(r.Provider) {
				funded = true
			}
		}
		if _, configured := s.Tiers[t]; configured && !funded && (tier == "" || tier == t) {
			tiers = append(tiers, t)
		}
	}
	for _, r := range s.Routes {
		if r.Enabled && s.ProviderFunded(r.Provider) && !included[r.Name] && (tier == "" || tier == r.Tier) {
			routes = append(routes, RouteGap{Route: r.Name, Provider: r.Provider, Tier: r.Tier})
		}
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Route < routes[j].Route })
	return routes, tiers
}

// ProviderCoverages gives each applied row, even one without routes, in name order.
func (s *Snapshot) ProviderCoverages() []ProviderCoverage {
	out := []ProviderCoverage{}
	byName := map[string]Route{}
	for _, r := range s.Routes {
		byName[r.Name] = r
	}
	for name, p := range s.Providers {
		p.Name = name
		row := ProviderCoverage{Provider: p, Tiers: []ProviderTier{}}
		for _, tier := range s.providerTiers() {
			count := ProviderTier{Tier: tier}
			for _, r := range s.Routes {
				if r.Enabled && r.Provider == name && r.Tier == tier {
					count.Enabled++
				}
			}
			seen := map[string]bool{}
			for _, n := range s.tierArray(tier) {
				r, ok := byName[n]
				if ok && !seen[n] && r.Enabled && r.Tier == tier && r.Provider == name {
					count.In++
					seen[n] = true
				}
			}
			if count.Enabled > 0 || count.In > 0 {
				row.Tiers = append(row.Tiers, count)
			}
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ProvidersLine formats the same coverage and gaps carried by where --json.
func (s *Snapshot) ProvidersLine() string {
	routes, tiers := s.ProviderGaps("")
	return ProvidersLine(s.ProviderCoverages(), ProviderGapReport{Routes: routes, Tiers: tiers, InvalidProviders: s.ProviderProblems()})
}

// ProvidersLine draws applied funding facts only: no balance implies funding.
func ProvidersLine(rows []ProviderCoverage, gaps ProviderGapReport) string {
	if len(rows) == 0 {
		return "providers: none applied WARN no provider rows applied (nova-config apply --kind provider)"
	}
	parts := []string{}
	for _, p := range rows {
		word := p.Name + " unfunded"
		if p.Problem != "" {
			parts = append(parts, p.Name+" unknown ("+p.Problem+"; remedy=nova-config apply --kind provider)")
			continue
		}
		if p.Funded {
			word = p.Name + " funded"
			counts := []string{}
			for _, t := range p.Tiers {
				counts = append(counts, fmt.Sprintf("%s %d/%d", t.Tier, t.In, t.Enabled))
			}
			if len(counts) > 0 {
				word += " (" + strings.Join(counts, ", ") + ")"
			}
		}
		parts = append(parts, word)
	}
	line := "providers: " + strings.Join(parts, "; ")
	if n := len(gaps.Routes); n > 0 {
		line += fmt.Sprintf(" WARN %d funded routes in no tier array (check cards names them)", n)
	}
	if len(gaps.Tiers) > 0 {
		line += " WARN tiers " + strings.Join(gaps.Tiers, ",") + " hold no route of a funded provider (check cards names them)"
	}
	return line
}

// ProviderFunded excludes malformed rows; no balance implies funding.
func (s *Snapshot) ProviderFunded(name string) bool {
	p, applied := s.Providers[name]
	return applied && p.Problem == "" && p.Funded
}

// ProviderProblems gives each malformed applied provider exactly once, in name order.
func (s *Snapshot) ProviderProblems() []ProviderProblem {
	out := []ProviderProblem{}
	for name, p := range s.Providers {
		if p.Problem != "" {
			out = append(out, ProviderProblem{Name: name, Problem: p.Problem})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
