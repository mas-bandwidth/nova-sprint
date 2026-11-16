package card

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ResourcesField is the brief's metadata line a card declares its required
// resources and tags with: `RESOURCES: cpu 4, memory 8, mac, ps5`.
const ResourcesField = "RESOURCES"

// Needs specifies the hardware tags and coarse whole-number resource amounts
// required by a card. Amounts always includes "cpu" >= 1 and "memory" >= 1.
type Needs struct {
	Tags    []string       `json:"tags,omitempty"`
	Amounts map[string]int `json:"amounts,omitempty"`
}

// DefaultReadNeeds returns the default budget for a read consumer (Q4):
// cpu: 1, memory: 1, zero hardware/OS tags.
func DefaultReadNeeds() Needs {
	return Needs{Amounts: map[string]int{"cpu": 1, "memory": 1}}
}

// ParseBriefNeeds extracts the RESOURCES line from a brief's header. It refuses
// NEEDS-ROUTE: (retired in v3) and duplicate RESOURCES lines.
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
		if key == RouteField || key == "NEEDS-ROUTE" {
			return "", 0, fmt.Errorf("brief declares %s: %s; %s is retired in v3, use %s: instead", key, val, key, ResourcesField)
		}
		if key == ResourcesField {
			count++
			if count == 1 {
				raw = val
			}
		}
	}
	if count > 1 {
		return "", count, fmt.Errorf("duplicate %s field in brief (%d lines)", ResourcesField, count)
	}
	return raw, count, nil
}

// ParseNeeds parses a raw resources specification (e.g. "cpu 4, memory 8, mac, ps5")
// per R9/R10. cpu and memory default to 1 if not specified. Bare known resources get
// amount 1; bare known tags are added to Tags. Unknown names, non-positive amounts,
// and tags with amounts return descriptive errors. When both knownTags and
// knownResources are nil, ParseNeeds validates syntax only (identifier tokens and
// positive whole-number amounts).
func ParseNeeds(raw string, knownTags map[string]bool, knownResources map[string]bool) (Needs, error) {
	amounts := map[string]int{"cpu": 1, "memory": 1}
	var tags []string
	syntaxOnly := knownTags == nil && knownResources == nil

	trimmedRaw := strings.TrimSpace(raw)
	if trimmedRaw == "" {
		return Needs{Amounts: amounts}, nil
	}

	items := strings.Split(trimmedRaw, ",")
	for _, item := range items {
		token := strings.TrimSpace(item)
		if token == "" {
			continue
		}
		fields := strings.Fields(token)
		switch len(fields) {
		case 1:
			name := strings.ToLower(fields[0])
			switch {
			case name == "cpu" || name == "memory":
				amounts[name] = 1
			case knownResources != nil && knownResources[name]:
				amounts[name] = 1
			case (knownTags != nil && knownTags[name]) || (syntaxOnly && validNeedsName(name)):
				if !slices.Contains(tags, name) {
					tags = append(tags, name)
				}
			default:
				return Needs{}, fmt.Errorf("unknown tag or resource %q in %s", name, raw)
			}
		case 2:
			name := strings.ToLower(fields[0])
			valStr := fields[1]
			val, err := strconv.Atoi(valStr)
			if err != nil {
				return Needs{}, fmt.Errorf("invalid amount %q for %q in %s: %w", valStr, name, raw, err)
			}
			if val < 1 {
				return Needs{}, fmt.Errorf("resource %q amount must be at least 1, got %d", name, val)
			}
			if knownTags != nil && knownTags[name] {
				return Needs{}, fmt.Errorf("tag %q cannot have an amount (%d specified)", name, val)
			}
			if name == "cpu" || name == "memory" || (knownResources != nil && knownResources[name]) || (syntaxOnly && validNeedsName(name)) {
				amounts[name] = val
			} else {
				return Needs{}, fmt.Errorf("unknown resource %q in %s", name, raw)
			}
		default:
			return Needs{}, fmt.Errorf("malformed resource spec %q in %s", token, raw)
		}
	}
	slices.Sort(tags)
	return Needs{Tags: tags, Amounts: amounts}, nil
}

func validNeedsName(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	letter := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			letter = true
		} else if !(c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return letter
}

// CardNeeds is a lenient reader for already-admitted cards in the store.
// cpu and memory default to 1. Tokens with integer amounts go to Amounts;
// bare tokens go to Tags.
func CardNeeds(brief string) Needs {
	raw, _, err := ParseBriefNeeds(brief)
	if err != nil || strings.TrimSpace(raw) == "" {
		return DefaultReadNeeds()
	}
	amounts := map[string]int{"cpu": 1, "memory": 1}
	var tags []string
	items := strings.Split(raw, ",")
	for _, item := range items {
		token := strings.TrimSpace(item)
		if token == "" {
			continue
		}
		fields := strings.Fields(token)
		switch len(fields) {
		case 1:
			name := strings.ToLower(fields[0])
			if name == "cpu" || name == "memory" {
				amounts[name] = 1
			} else if !slices.Contains(tags, name) {
				tags = append(tags, name)
			}
		case 2:
			name := strings.ToLower(fields[0])
			if val, err := strconv.Atoi(fields[1]); err == nil && val >= 1 {
				amounts[name] = val
			}
		}
	}
	slices.Sort(tags)
	return Needs{Tags: tags, Amounts: amounts}
}

// ResolveNeeds resolves the needs for a card. Reads always return DefaultReadNeeds()
// (cpu: 1, memory: 1, no tags). For other kinds, it parses the RESOURCES line against
// the fleet's known tags and resources, falling back leniently if needed.
func ResolveNeeds(kind, brief string, tags, resources map[string]bool) Needs {
	if kind == "read" {
		return DefaultReadNeeds()
	}
	raw, count, err := ParseBriefNeeds(brief)
	if err != nil || count == 0 || strings.TrimSpace(raw) == "" {
		return DefaultReadNeeds()
	}
	n, err := ParseNeeds(raw, tags, resources)
	if err != nil {
		return CardNeeds(brief)
	}
	return n
}
