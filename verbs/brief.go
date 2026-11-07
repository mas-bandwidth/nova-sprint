package verbs

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// A brief (CARD-CONTRACT) is a tier line, metadata lines (PATHS, NEEDS,
// AFTER, PRIORITY, TEST, DONE-WHEN), a blank line and the task prose. The
// parser checks the form; the gates TEST names are the worker's to run.
type briefParts struct {
	a    card.Add
	seen map[string]bool
}

// parseBrief is brief id's text as a producer's add, or the first rule it
// breaks.
func parseBrief(id, text string) (card.Add, error) {
	p := briefParts{a: card.Add{ID: id, Kind: "producer", Brief: text}, seen: map[string]bool{}}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if !strings.HasPrefix(lines[0], "tier: ") {
		return p.a, lintAt(id, 1, "tier first line")
	}
	p.a.Tier = strings.TrimSpace(strings.TrimPrefix(lines[0], "tier: "))
	if !slices.Contains(store.Tiers, p.a.Tier) {
		return p.a, lintAt(id, 1, "tier")
	}
	separator := -1
	for i, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			separator = i + 2
			break
		}
		if field := p.line(line); field != "" {
			return p.a, lintAt(id, i+2, field)
		}
	}
	for _, field := range []string{"PATHS", "TEST", "DONE-WHEN"} {
		if !p.seen[field] {
			return p.a, lintAt(id, len(lines), field)
		}
	}
	if separator > 0 {
		p.a.Prose = strings.TrimSpace(strings.Join(lines[separator:], "\n"))
	}
	if p.a.Prose == "" {
		return p.a, lintAt(id, len(lines), "task prose")
	}
	if n, what, fix := retiredMechanic(lines); what != "" {
		return p.a, &BriefLintError{Brief: id, Line: n, Rule: "names " + what + ", a retired mechanic", Fix: fix}
	}
	return p.a, nil
}

// BriefLintError is the first brief rule broken, without quoting the brief;
// Fix is the rule's own fix when it has one (else: fix the brief).
type BriefLintError struct {
	Brief string
	Line  int
	Rule  string
	Fix   string
}

func (e *BriefLintError) Error() string {
	return fmt.Sprintf("%s: line %d %s", oneLine(e.Brief), e.Line, e.Rule)
}

func lintAt(id string, line int, field string) error {
	rule := "must contain a valid " + field + " section"
	switch field {
	case "tier first line", "tier":
		rule = "must be tier: <flash|pro|heavy|frontier>"
	case "metadata":
		rule = "must be a recognized metadata field"
	case "task prose":
		rule = "must include task prose after a blank line"
	case "DONE-WHEN":
		rule = "must contain a valid DONE-WHEN section (a shell command that parses: quotes, parentheses and backquotes closed, no dangling | & ; or \\)"
	case card.RouteField:
		rule = "must contain a valid " + card.RouteField + " section (one route name per line, 1 to 128 of a-z A-Z 0-9 . _ -)"
	case "HEAD":
		rule = "must contain a valid HEAD section (the full 40-hex sha the work already has)"
	case "PR":
		rule = "must contain a valid PR section (the forge's pull request number the work already has)"
	case "retired WHO", "retired LOCK", "retired JOB":
		return &BriefLintError{Brief: id, Line: line, Rule: "is a " + field + " line, a retired mechanic", Fix: retiredFix}
	}
	return &BriefLintError{Brief: id, Line: line, Rule: rule}
}

// line reads one metadata line; it returns the field it breaks, or "".
func (p *briefParts) line(line string) string {
	key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
	if !ok {
		return "metadata"
	}
	key, value = strings.TrimSpace(key), strings.TrimSpace(value)
	switch strings.ToUpper(key) {
	case "DEPENDS-ON", "DEPENDENCIES", "NEED":
		return "NEEDS"
	case "WHO", "LOCK", "JOB":
		return "retired " + strings.ToUpper(key)
	case "PATHS", "NEEDS", "AFTER", "PRIORITY", "TEST", "DONE-WHEN", card.RouteField, "HEAD", "PR":
		if key != strings.ToUpper(key) || value == "" || (p.seen[key] && !repeatable[key]) {
			return strings.ToUpper(key)
		}
		p.seen[key] = true
		return p.field(key, value)
	}
	return "metadata"
}

func (p *briefParts) field(key, value string) string {
	switch key {
	case "PATHS":
		for v := range strings.SplitSeq(value, ",") {
			if v = strings.TrimSpace(v); !validPath(v) {
				return "PATHS"
			}
			p.a.Paths = append(p.a.Paths, v)
		}
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
			return "PRIORITY"
		}
		p.a.Priority = value
	case "DONE-WHEN":
		if shellProblem(value) != "" {
			return "DONE-WHEN"
		}
		p.a.DoneWhen = append(p.a.DoneWhen, value)
	case card.RouteField:
		if !validName(value) {
			return card.RouteField
		}
	case "HEAD":
		if !fullSHA(value) {
			return "HEAD"
		}
	case "PR":
		if n, err := strconv.Atoi(value); err != nil || n < 1 {
			return "PR"
		}
	}
	return ""
}

// repeatable are the metadata fields a brief may give more than once.
var repeatable = map[string]bool{"DONE-WHEN": true, "TEST": true, card.RouteField: true}

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
