REPO: mas-bandwidth/schema
BASE: sprint/schema-go
PATHS: internal/codegen/gotable/json.go,internal/tablenames/go.go

THE TASK. Remove the 63-byte cap: scan the whole string token, then parse it; a fraction longer than nine digits truncates to nanoseconds exactly as the engine does (read the engine's rule and match it digit for digit; truncation, not rounding, unless the engine rounds, in which case say so); a string that is not RFC 3339 stays the named refusal with no clamped count. A string longer than any RFC 3339 spelling can be (bound it from the grammar, say how) is refused by name without allocation proportional to its length.

THE TEST, through runGenerated: the 79-byte spelling reads to the same nanosecond count as its nine-digit spelling, silent; 10, 12 and 60 fraction digits likewise; a 10000-byte string of digits after the dot reads (or is refused, if the bound you state is below it) with Clamped 0 in every case; "garbage" keeps `timestamp_not_rfc3339: at` with Clamped 0.

DONE-WHEN: `SCHEMA_SLOW=1 go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run '^TestGoTimestampTextReadsAnyValidLength$' -v` RUN and PASS at head (red at BASE: no tests to run, and the 79-byte line above), and the STEP 5 gate is green or its only reds are named as present at BASE.

STEP 1. (the rest of the brief is cut from this fixture)
