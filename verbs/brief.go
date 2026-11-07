package verbs

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// BriefVerifier checks semantic contract gates against the actual base: red
// controls, negative controls, tools, existing work and worker instructions.
type BriefVerifier interface {
	VerifyBrief(context.Context, store.Stream, card.Add, []string) error
}

// FileBriefs is the canonical on-disk brief parser and its external gate check.
type FileBriefs struct{ Verifier BriefVerifier }

func (b FileBriefs) Parse(ctx context.Context, path string, s store.Stream) (card.Add, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return card.Add{}, &OperationError{Problem: "cannot read brief " + path, Fix: "provide a readable --brief-file", Exit: 1}
	}
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	a, tests, err := parseBrief(id, string(data))
	if err != nil {
		return card.Add{}, err
	}
	if b.Verifier == nil {
		return card.Add{}, &OperationError{Problem: "brief gates cannot be verified", Fix: "configure the base and gate verifier", Exit: 2}
	}
	if err = b.Verifier.VerifyBrief(ctx, s, a, tests); err != nil {
		return card.Add{}, err
	}
	return a, nil
}

type briefParts struct {
	a     card.Add
	tests []string
	seen  map[string]bool
	prose []string
}

func parseBrief(id, text string) (card.Add, []string, error) {
	p := briefParts{a: card.Add{ID: id, Kind: "producer", Brief: text}, seen: map[string]bool{}}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "tier: ") {
		return p.a, nil, briefError("tier first line")
	}
	p.a.Tier = strings.TrimSpace(strings.TrimPrefix(lines[0], "tier: "))
	if !slices.Contains(store.Tiers, p.a.Tier) {
		return p.a, nil, briefError("tier")
	}
	separator := -1
	for i, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			separator = i + 2
			break
		}
		if err := p.line(line); err != nil {
			return p.a, nil, err
		}
	}
	if separator < 0 || separator >= len(lines) {
		return p.a, nil, briefError("task prose")
	}
	p.prose = append(p.prose, lines[separator:]...)
	for _, field := range []string{"PATHS", "TEST", "DONE-WHEN"} {
		if !p.seen[field] {
			return p.a, nil, briefError(field)
		}
	}
	p.a.Prose = strings.TrimSpace(strings.Join(p.prose, "\n"))
	if p.a.Prose == "" {
		return p.a, nil, briefError("task prose")
	}
	return p.a, p.tests, nil
}
func briefError(field string) error {
	return &OperationError{Problem: "brief missing or invalid " + field, Fix: "fix the brief's " + field + " section", Exit: 1}
}

func (p *briefParts) line(line string) error {
	trimmed := strings.TrimSpace(line)
	key, value, ok := strings.Cut(trimmed, ":")
	if !ok {
		return briefError("metadata")
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	switch strings.ToUpper(key) {
	case "DEPENDS-ON", "DEPENDENCIES", "NEED":
		return briefError("NEEDS")
	case "PATHS", "NEEDS", "AFTER", "PRIORITY", "TEST", "DONE-WHEN":
		if key != strings.ToUpper(key) || value == "" {
			return briefError(strings.ToUpper(key))
		}
		if p.seen[key] && key != "DONE-WHEN" && key != "TEST" {
			return briefError(key)
		}
		p.seen[key] = true
		return p.field(key, value)
	default:
		return briefError("metadata")
	}
}
func (p *briefParts) field(key, value string) error {
	switch key {
	case "PATHS":
		paths := strings.Split(value, ",")
		for i, v := range paths {
			v = strings.TrimSpace(v)
			if !validPath(v) {
				return briefError("PATHS")
			}
			paths[i] = v
		}
		p.a.Paths = paths
	case "NEEDS":
		if value != "none" {
			p.a.Needs = splitNames(value)
		}
	case "AFTER":
		if value != "none" {
			p.a.After = splitNames(value)
		}
	case "PRIORITY":
		if !slices.Contains(card.Levels, value) {
			return briefError("PRIORITY")
		}
		p.a.Priority = value
	case "DONE-WHEN":
		p.a.DoneWhen = append(p.a.DoneWhen, value)
	case "TEST":
		p.tests = append(p.tests, value)
	}
	return nil
}
func validPath(path string) bool {
	return path != "" && !filepath.IsAbs(path) && !strings.ContainsAny(path, " \t()") && path != ".." && !strings.HasPrefix(filepath.Clean(path), "../")
}
func splitNames(value string) []string {
	parts := strings.Split(value, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
