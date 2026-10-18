REPO: mas-bandwidth/schema
BASE: sprint/schema-go
PATHS: docs/SPEC-TABLES.md,internal/codegen/gotable/json.go,internal/tabletext/read.go,internal/rfc3339/*.go,generated/bench/**/go/*.go

THE TASK. (1) Standard first, its own commit: 2.11 and the 16.2 row say the text is RFC 3339 section 5.6 date-time exactly: four-digit year; month 01-12; day valid for the month and year; hour 00-23; minute 00-59; second 00-60 only where 2.11 says what a leap second lands as (state it; if it says nothing, 60 is refused, and write that); T or t; a fraction of one or more digits after a dot, truncated to nanoseconds; Z, z or an offset of hours 00-23 and minutes 00-59; nothing before or after. (2) One parser, internal/rfc3339 (standard library only, no allocation, returning the UTC nanosecond count or a refusal), used by the engine; the Go leg's emitted code must stay self-contained, so the emitter embeds the same source (go:embed of the parser file, or the source text the package also compiles) and a test fails if the two differ. (3) Both readers refuse what the parser refuses with `timestamp_not_rfc3339: <field>`, and accept everything it accepts.

THE TESTS: internal/rfc3339 TestStrictRFC3339, a table of accepted and refused spellings covering every rule above, including the four lines of the red; internal/codegen/gotable TestGoTimestampStrictRFC3339 through runGenerated: the generated reader and the engine (tabletext) land the same nanoseconds or the same refusal on every spelling in that table.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/rfc3339/ -run '^TestStrictRFC3339$' -v` and `SCHEMA_SLOW=1 go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run '^TestGoTimestampStrictRFC3339$' -v` RUN and PASS at head, the standard commit is first, and the STEP 5 gate is green or its only reds are named as present at BASE.

STEP 1. (the rest of the brief is cut from this fixture)
