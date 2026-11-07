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
	for _, text := range []string{"BEGIN;", "COMMIT/*c*/;", "-- hi\nSTART/*c*/TRANSACTION;", "COPY(sprint.t) TO STDOUT;", "\\! echo no\n", "\\restrict a\nSELECT 1;\n\\unrestrict b\n", "SELECT 'unterminated;", "\\restrict a\nSELECT 1;"} {
		if err := ParseDump(strings.NewReader(text), nil); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
}

func TestConfineDataRejectsExecutableValuesAndDDL(t *testing.T) {
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
	hash := strings.Repeat("a", 64)
	header := "-- nova-sprint-backup schema=" + hash + " version=dev+abcdef123456\n"
	consumer := "INSERT INTO sprint.cards VALUES (E'child\\'id', 'work', 'parent');\n"
	producer := "INSERT INTO sprint.cards VALUES ('parent', 'producer', NULL);\n"
	statements, err := ConfineData([]byte(header+consumer+producer), hash)
	if err != nil || len(statements) != 2 || !strings.Contains(statements[0], "'producer'") {
		t.Fatal(statements, err)
	}
}
