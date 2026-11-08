package verbs

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

var insertPrefix = regexp.MustCompile(`^INSERT INTO sprint\.(balances|cards|cursors|epoch|events|judgements|meta|policy|rows|streams) VALUES \(`)
var sequenceValue = regexp.MustCompile(`^SELECT pg_catalog\.setval\('sprint\.(policy_version_seq|balances_id_seq)', [0-9]+, (true|false)\);$`)
var backupHeader = regexp.MustCompile(`^-- nova-sprint-backup schema=([0-9a-f]{64}) version=(` + buildVersionPattern + `)$`)

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
		if err := cardStateMapped(statement); err != nil {
			return err
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
	order := map[string]int{"meta": 10, "epoch": 11, "streams": 20, "rows": 21, "cards": 30, "judgements": 40, "events": 50, "cursors": 60, "policy": 70, "balances": 80}[prefix[1]]
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
	if i > digits && i+1 < len(s) && s[i] == '.' && s[i+1] >= '0' && s[i+1] <= '9' {
		// a numeric's fraction (sprint.balances.balance_usd, round 25)
		for i++; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		}
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

// buildVersionPattern is every version a build reports and a backup
// records: a release (vX.Y.Z); what `git describe --always --dirty --tags`
// stamps through -ldflags (vX.Y.Z-N-gHEX, or a bare HEX with no tag, each
// perhaps -dirty); and an unstamped source build's dev+<12 hex> from its VCS
// stamp (-dirty when the tree was modified). A build with no VCS stamp is
// "dev" and is refused: nothing would say what made the backup.
const buildVersionPattern = `(?:v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9]+-g[0-9a-f]{7,40})?|dev\+[0-9a-f]{12}|[0-9a-f]{7,40})(?:-dirty)?`

var buildVersion = regexp.MustCompile(`^` + buildVersionPattern + `$`)

func validBuildVersion(version string) bool { return buildVersion.MatchString(version) }

// cardLiterals is the first n literals of a sprint.cards INSERT, as
// written (quoted strings keep their quotes): id, kind, "of", stream, state
// in the table's column order.
func cardLiterals(s string, n int) ([]string, bool) {
	prefix := insertPrefix.FindString(s)
	body := s[len(prefix):]
	var out []string
	for i := skipSpace(body, 0); len(out) < n; {
		end, ok := literalEnd(body, i)
		if !ok {
			return nil, false
		}
		out = append(out, body[i:end])
		i = skipSpace(body, end)
		if len(out) < n {
			if i >= len(body) || body[i] != ',' {
				return nil, false
			}
			i = skipSpace(body, i+1)
		}
	}
	return out, true
}

func insertCardKind(s string) (string, bool) {
	lits, ok := cardLiterals(s, 2)
	if !ok {
		return "", false
	}
	switch kind := lits[1]; kind {
	case "'producer'", "'sentinel'", "'work'", "'read'":
		return strings.Trim(kind, "'"), true
	}
	return "", false
}

// cardStateMapped refuses a card record whose state this build does not
// map (store.CardStates): an importer that carried a terminal state over
// from another shape, a v1 landed say, names the record and the one fix
// here, before the verification cluster's CHECK refuses it unnamed.
func cardStateMapped(s string) error {
	if !strings.HasPrefix(s, "INSERT INTO sprint.cards ") {
		return nil
	}
	lits, ok := cardLiterals(s, 5)
	if !ok {
		return &OperationError{Problem: "a sprint.cards record has fewer than five values (id, kind, of, stream, state): " + edge.FirstBytes([]byte(s)),
			Fix: "restore a backup this build wrote (nova-sprint backup), or map the importer's cards to this build's columns", Exit: 1}
	}
	state := strings.Trim(lits[4], "'")
	if slices.Contains(store.CardStates, state) && lits[4] != state {
		return nil
	}
	return &OperationError{Problem: fmt.Sprintf("record sprint.cards %s: state %s is not a state this build maps (%s)", clipN(lits[0], 80), clipN(lits[4], 40), strings.Join(store.CardStates, ", ")),
		Fix: "map the record's state to one of those in the importer, or drop the record, then restore again", Exit: 1}
}
