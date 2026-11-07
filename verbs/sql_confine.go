package verbs

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

var insertPrefix = regexp.MustCompile(`^INSERT INTO sprint\.(cards|cursors|epoch|events|judgements|meta|policy|rows|streams) VALUES \(`)
var sequenceValue = regexp.MustCompile(`^SELECT pg_catalog\.setval\('sprint\.policy_version_seq', [0-9]+, (true|false)\);$`)
var backupHeader = regexp.MustCompile(`^-- nova-sprint-backup schema=([0-9a-f]{64}) version=(v[0-9]+\.[0-9]+\.[0-9]+|dev\+[0-9a-f]{12})$`)

type orderedSQL struct {
	order int
	sql   string
}

// ConfineData accepts this binary's schema and the literal-only data statements
// emitted by pg_dump --data-only --inserts, with dependency-safe ordering.
func ConfineData(dump []byte, expectedHash string) ([]string, error) {
	line, _, ok := bytes.Cut(dump, []byte("\n"))
	if !ok {
		return nil, fmt.Errorf("line 1: backup header missing: %s", edge.ParseRefused("no newline", len(dump), dump))
	}
	match := backupHeader.FindStringSubmatch(string(line))
	if len(match) != 3 || match[1] != expectedHash || !validBuildVersion(match[2]) {
		return nil, fmt.Errorf("line 1: backup schema or version header does not match: %s", edge.ParseRefused(confineHeaderWhy(match, expectedHash), 0, line))
	}
	rows := []orderedSQL{}
	err := ParseDump(bytes.NewReader(dump), func(s string) error {
		statement := strings.TrimSpace(stripComments(s))
		order, ok := dataStatement(statement)
		if !ok {
			return fmt.Errorf("dump contains a statement outside sprint data: %q", edge.FirstBytes([]byte(statement)))
		}
		rows = append(rows, orderedSQL{order, statement})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].order < rows[j].order })
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.sql)
	}
	return out, nil
}
func confineHeaderWhy(match []string, expectedHash string) string {
	switch {
	case len(match) != 3:
		return "not a nova-sprint-backup schema= version= header"
	case match[1] != expectedHash:
		return "schema " + match[1] + " is not this build's " + expectedHash
	default:
		return "version " + match[2] + " is not a build version"
	}
}
func dataStatement(s string) (int, bool) {
	if harmlessSet(s) {
		return 0, true
	}
	if sequenceValue.MatchString(s) {
		return 90, true
	}
	prefix := insertPrefix.FindStringSubmatch(s)
	if len(prefix) != 2 || !validInsert(s) {
		return 0, false
	}
	order := map[string]int{"meta": 10, "epoch": 11, "streams": 20, "rows": 21, "cards": 30, "judgements": 40, "events": 50, "cursors": 60, "policy": 70}[prefix[1]]
	if prefix[1] == "cards" {
		kind, ok := insertCardKind(s)
		if !ok {
			return 0, false
		}
		if kind == "work" || kind == "read" {
			order = 31
		}
	}
	return order, true
}
func harmlessSet(s string) bool {
	switch s {
	case "SET statement_timeout = 0;", "SET lock_timeout = 0;", "SET idle_in_transaction_session_timeout = 0;", "SET transaction_timeout = 0;", "SET client_encoding = 'UTF8';", "SET standard_conforming_strings = on;", "SET check_function_bodies = false;", "SET xmloption = content;", "SET client_min_messages = warning;", "SET row_security = off;", "SELECT pg_catalog.set_config('search_path', '', false);":
		return true
	}
	return false
}
func validInsert(s string) bool {
	prefix := insertPrefix.FindString(s)
	if prefix == "" || !strings.HasSuffix(s, ");") {
		return false
	}
	body := s[len(prefix) : len(s)-2]
	i := 0
	values := 0
	for {
		i = skipSpace(body, i)
		if i == len(body) {
			return values > 0
		}
		next, ok := literalEnd(body, i)
		if !ok {
			return false
		}
		values++
		i = skipSpace(body, next)
		if i == len(body) {
			return true
		}
		if body[i] != ',' {
			return false
		}
		i++
		if skipSpace(body, i) == len(body) {
			return false
		}
	}
}
func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}
func literalEnd(s string, i int) (int, bool) {
	escape := false
	if i+1 < len(s) && (s[i] == 'E' || s[i] == 'e') && s[i+1] == '\'' {
		escape = true
		i++
	}
	if i < len(s) && s[i] == '\'' {
		return quotedLiteral(s, i, escape)
	}
	start := i
	if i < len(s) && s[i] == '-' {
		i++
	}
	digits := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > digits {
		return i, true
	}
	for _, word := range []string{"NULL", "true", "false"} {
		if strings.HasPrefix(s[start:], word) {
			return start + len(word), true
		}
	}
	return start, false
}
func quotedLiteral(s string, i int, escape bool) (int, bool) {
	for i++; i < len(s); i++ {
		if escape && s[i] == '\\' {
			i++
			if i >= len(s) {
				return 0, false
			}
			continue
		}
		if s[i] == '\'' {
			if i+1 < len(s) && s[i+1] == '\'' {
				i++
				continue
			}
			return i + 1, true
		}
	}
	return 0, false
}

var developmentVersion = regexp.MustCompile(`^dev\+[0-9a-f]{12}$`)

func validBuildVersion(version string) bool {
	if developmentVersion.MatchString(version) {
		return true
	}
	if !strings.HasPrefix(version, "v") {
		return false
	}
	_, err := releaseVersion(version)
	return err == nil
}

func insertCardKind(s string) (string, bool) {
	prefix := insertPrefix.FindString(s)
	body := s[len(prefix):]
	i := skipSpace(body, 0)
	end, ok := literalEnd(body, i)
	if !ok {
		return "", false
	}
	i = skipSpace(body, end)
	if i >= len(body) || body[i] != ',' {
		return "", false
	}
	i = skipSpace(body, i+1)
	end, ok = literalEnd(body, i)
	if !ok {
		return "", false
	}
	kind := body[i:end]
	switch kind {
	case "'producer'", "'sentinel'", "'work'", "'read'":
		return strings.Trim(kind, "'"), true
	}
	return "", false
}
