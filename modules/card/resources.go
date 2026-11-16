package card

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const ResourcesField = "RESOURCES"

// Needs is the tags and coarse resource amounts work needs (R5, R9, R10).
type Needs struct {
	Tags    []string       `json:"tags,omitempty"`
	Amounts map[string]int `json:"amounts,omitempty"` // always includes "cpu" >= 1 and "memory" >= 1
}

// DefaultReadNeeds is the default read budget: cpu 1, memory 1 with zero tags (Q4, R42).
func DefaultReadNeeds() Needs {
	return Needs{
		Amounts: map[string]int{"cpu": 1, "memory": 1},
	}
}

// ParseNeeds parses a resources line ("cpu 4, memory 8, mac, ps5") into Needs.
// Amounts default to cpu 1 and memory 1. Unknown names or non-positive numbers return an error.
func ParseNeeds(raw string, knownTags map[string]bool, knownResources map[string]bool) (Needs, error) {
	n := DefaultReadNeeds()
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return n, nil
	}
	parts := strings.Split(raw, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		fields := strings.Fields(p)
		switch len(fields) {
		case 1:
			name := strings.ToLower(fields[0])
			if knownResources != nil && knownResources[name] {
				n.Amounts[name] = 1
			} else if knownTags == nil || knownTags[name] {
				if !slices.Contains(n.Tags, name) {
					n.Tags = append(n.Tags, name)
				}
			} else {
				return Needs{}, fmt.Errorf("unknown tag or resource %q", name)
			}
		case 2:
			name := strings.ToLower(fields[0])
			val, err := strconv.Atoi(fields[1])
			if err != nil {
				return Needs{}, fmt.Errorf("resource %q amount must be an integer: %w", name, err)
			}
			if val < 1 {
				return Needs{}, fmt.Errorf("resource %q amount must be an integer >= 1 (got %d)", name, val)
			}
			if knownTags != nil && knownTags[name] {
				return Needs{}, fmt.Errorf("tag %q cannot take an amount", name)
			}
			n.Amounts[name] = val
		default:
			return Needs{}, fmt.Errorf("invalid resources token %q", p)
		}
	}
	slices.Sort(n.Tags)
	return n, nil
}

// ParseBriefNeeds extracts the RESOURCES: line from a brief header.
// It refuses NEEDS-ROUTE: and duplicate RESOURCES: lines.
func ParseBriefNeeds(brief string) (raw string, count int, err error) {
	lines := strings.Split(strings.ReplaceAll(brief, "\r\n", "\n"), "\n")
	for _, line := range lines[min(1, len(lines)):] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		k, v, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		if key == RouteField {
			return "", 0, errors.New("NEEDS-ROUTE is retired in v3.0.0; use RESOURCES instead")
		}
		if key == ResourcesField {
			count++
			if count > 1 {
				return "", count, errors.New("duplicate RESOURCES line")
			}
			raw = val
		}
	}
	return raw, count, nil
}

// CardNeeds is a lenient reader for already-admitted cards in store.
func CardNeeds(brief string) Needs {
	raw, count, err := ParseBriefNeeds(brief)
	if err != nil || count == 0 || raw == "" {
		return DefaultReadNeeds()
	}
	n, err := ParseNeeds(raw, nil, nil)
	if err != nil {
		return DefaultReadNeeds()
	}
	return n
}

// ResolveNeeds resolves needs for a given card kind and brief.
// If kind == "read", returns DefaultReadNeeds().
func ResolveNeeds(kind, brief string, tags, resources map[string]bool) Needs {
	if kind == "read" {
		return DefaultReadNeeds()
	}
	raw, count, err := ParseBriefNeeds(brief)
	if err != nil || count == 0 || raw == "" {
		return DefaultReadNeeds()
	}
	n, err := ParseNeeds(raw, tags, resources)
	if err != nil {
		return DefaultReadNeeds()
	}
	return n
}
