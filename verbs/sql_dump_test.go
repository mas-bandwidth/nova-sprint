package verbs

import (
	"context"
	"strings"
	"testing"
)

type sqlCollect struct{ statements []string }

func (s *sqlCollect) Exec(_ context.Context, v string) error {
	s.statements = append(s.statements, v)
	return nil
}
func TestDumpParserQuotesAndComments(t *testing.T) {
	t.Parallel()
	dump := `-- generated
\restrict abc123
SET standard_conforming_strings = on;
CREATE FUNCTION sprint.f() RETURNS void AS $tag$ BEGIN PERFORM ';'; /* nested /* c */ x */ END; $tag$ LANGUAGE plpgsql;
INSERT INTO sprint.t VALUES ('one;two', E'a\'b', "x;y"); -- comment;
\unrestrict abc123
`
	writer := &sqlCollect{}
	if err := ParseDump(strings.NewReader(dump), func(s string) error { return writer.Exec(context.Background(), s) }); err != nil {
		t.Fatal(err)
	}
	if len(writer.statements) != 3 {
		t.Fatalf("got %d: %q", len(writer.statements), writer.statements)
	}
}
func TestDumpParserRejectsControlAndDirectives(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"BEGIN;", "COMMIT/*c*/;", "-- hi\nSTART/*c*/TRANSACTION;", "COPY(sprint.t) TO STDOUT;", "\\! echo no\n", "\\restrict a\nSELECT 1;\n\\unrestrict b\n", "SELECT 'unterminated;", "\\restrict a\nSELECT 1;"} {
		if err := ParseDump(strings.NewReader(text), nil); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
}

func TestConfineDataRejectsExecutableValuesAndDDL(t *testing.T) {
	t.Parallel()
	hash := strings.Repeat("a", 64)
	header := "-- nova-sprint-backup schema=" + hash + " version=v2.0.0\n"
	valid := header + "INSERT INTO sprint.events VALUES (E'hi\\'there', 10, NULL, true);\n"
	if _, err := ConfineData([]byte(valid), hash); err != nil {
		t.Fatal(err)
	}
	for _, extra := range []string{"INSERT INTO sprint.events VALUES (pg_sleep(1));", "CREATE TABLE public.hijack (v text);", "INSERT INTO sprint.events VALUES ('x'::text);", "SELECT pg_catalog.setval('public.seq', 1, false);", "CREATE FUNCTION sprint.bad() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;"} {
		if _, err := ConfineData([]byte(header+extra), hash); err == nil {
			t.Fatalf("accepted %q", extra)
		}
	}
	if _, err := ConfineData([]byte(valid), strings.Repeat("b", 64)); err == nil {
		t.Fatal("accepted wrong schema")
	}
}

func TestConfineDataOrdersCardsWithEscapedIdentifiers(t *testing.T) {
	t.Parallel()
	hash := strings.Repeat("a", 64)
	header := "-- nova-sprint-backup schema=" + hash + " version=dev+abcdef123456\n"
	consumer := "INSERT INTO sprint.cards VALUES (E'child\\'id', 'work', 'parent', 's', 'ready');\n"
	producer := "INSERT INTO sprint.cards VALUES ('parent', 'producer', NULL, 's', 'working');\n"
	statements, err := ConfineData([]byte(header+consumer+producer), hash)
	if err != nil || len(statements) != 2 || !strings.Contains(statements[0], "'producer'") {
		t.Fatal(statements, err)
	}
}

// TestConfineDataRefusesAnUnmappedCardState: a record whose state this
// build does not map (an importer's carried-over terminal state, here a v1
// landed timestamp) is refused naming the record and the one fix, before
// the verification cluster's CHECK would refuse it unnamed.
func TestConfineDataRefusesAnUnmappedCardState(t *testing.T) {
	t.Parallel()
	hash := strings.Repeat("a", 64)
	header := "-- nova-sprint-backup schema=" + hash + " version=v2.0.0\n"
	for _, record := range []string{
		"INSERT INTO sprint.cards VALUES ('c7', 'producer', NULL, 's', '2026-10-01 12:00:00+00', 0);\n",
		"INSERT INTO sprint.cards VALUES ('c7', 'producer', NULL, 's', 'merged', 0);\n",
		"INSERT INTO sprint.cards VALUES ('c7', 'producer', NULL, 's', NULL, 0);\n",
	} {
		_, err := ConfineData([]byte(header+record), hash)
		r := AsRefusal(err)
		if r == nil || !strings.Contains(r.Error(), "record sprint.cards 'c7': state") || !strings.Contains(classify(r).Fix, "map the record's state to one of those in the importer") {
			t.Fatalf("%q: %v", record, err)
		}
	}
	short := "INSERT INTO sprint.cards VALUES ('c7', 'producer', NULL);\n"
	if _, err := ConfineData([]byte(header+short), hash); AsRefusal(err) == nil {
		t.Fatalf("accepted a card with no state: %v", err)
	}
	good := "INSERT INTO sprint.cards VALUES ('c7', 'producer', NULL, 's', 'landed', 0);\n"
	if _, err := ConfineData([]byte(header+good), hash); err != nil {
		t.Fatal(err)
	}
}

// TestConfineDataCarriesRound25Balances: a backup of a round 25 store holds
// sprint.balances and its id sequence; both are sprint data and pass, after
// everything else, while another sequence is still refused (the e2e's
// backup step refused setval('sprint.balances_id_seq') before this).
func TestConfineDataCarriesRound25Balances(t *testing.T) {
	t.Parallel()
	hash := strings.Repeat("a", 64)
	header := "-- nova-sprint-backup schema=" + hash + " version=v2.0.0\n"
	balance := "INSERT INTO sprint.balances VALUES (1, 'deepseek', '2026-11-07 12:00:00+00', 5, 12.5, '');\n"
	seq := "SELECT pg_catalog.setval('sprint.balances_id_seq', 1, true);\n"
	event := "INSERT INTO sprint.events VALUES ('x', 1);\n"
	statements, err := ConfineData([]byte(header+balance+seq+event), hash)
	if err != nil || len(statements) != 3 || !strings.Contains(statements[0], "sprint.events") || !strings.Contains(statements[1], "sprint.balances") {
		t.Fatal(statements, err)
	}
	if _, err := ConfineData([]byte(header+"SELECT pg_catalog.setval('sprint.other_seq', 1, true);\n"), hash); err == nil {
		t.Fatal("accepted another sequence")
	}
}

// pg_dump 18 wraps a plain dump in \restrict/\unrestrict and sets
// transaction_timeout in its preamble: a verified backup accepts both.
func TestConfineDataAcceptsPgDump18Preamble(t *testing.T) {
	t.Parallel()
	hash := strings.Repeat("a", 64)
	dump := "-- nova-sprint-backup schema=" + hash + " version=v2.0.0\n" +
		"\\restrict Abc123\n" +
		"SET statement_timeout = 0;\nSET lock_timeout = 0;\nSET idle_in_transaction_session_timeout = 0;\nSET transaction_timeout = 0;\n" +
		"SET client_encoding = 'UTF8';\nSET standard_conforming_strings = on;\nSELECT pg_catalog.set_config('search_path', '', false);\n" +
		"INSERT INTO sprint.events VALUES (E'hi', 10, NULL, true);\n" +
		"\\unrestrict Abc123\n"
	if _, err := ConfineData([]byte(dump), hash); err != nil {
		t.Fatal(err)
	}
}
