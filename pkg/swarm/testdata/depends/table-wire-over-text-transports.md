RESULT: table-wire-over-text-transports sha=b262c81e4dba tier: pro
REPO: mas-bandwidth/schema
BASE: sprint/schema-go
KIND: guard
PRIORITY: high
STREAM: schema-go
DEPENDS-ON: go-fixed-load-sets-up-once-per-plan
PATHS: internal/codegen/gotable/gotable.go, internal/codegen/gotable/json.go, internal/codegen/gotable/refusal.go, internal/tablenames/go.go, docs/USAGE.md, docs/SPEC-TABLES.md, make/go.mk, docs/roadmap.sexp, ROADMAP.md
NEW: internal/codegen/gotable/texttransport_test.go
TEST: internal/codegen/gotable TestTableWireOverATextTransport
START: docs/SPEC-TABLES.md (section 16 and 16.2, the JSON form's base64 rule; section 3, THE FIRST BYTE), docs/USAGE.md (tables section), internal/codegen/gotable/json.go (the emitted JSON reader and writer), internal/codegen/gotable/gotable.go
STOP: when DONE-WHEN below holds and the commit is on your branch, or the deadline is reached; then report, with not-done named.
DEADLINE: finish within 180 minutes.
The REPO: and BASE: lines are the repository and the branch the work starts from and lands on: the member stages REPO: at BASE:, and nova-sprint land merges the card's head onto BASE: (land --base stands in for a card naming no BASE:, land --repo-dir for one naming no REPO:).
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 180 minutes; the judgment of a card that runs past it is the coordinator's, so report what you have with the verdict not-done rather than push past it.
Libraries considered: the Go standard library (encoding/base64, StdEncoding, padded) only.

# Go text helpers: base64 of the binary wire, and the JSON text form, both refusing damage by name

OWNER RULE (Glenn, 2026-10-15 12:34Z): "When you improve schema, improve the standard, and then fix it in golang and verify the fix. Stop the multi-language stuff until we have finished our dogfooding in golang." So the rule is written in the standard first, Go is then made to match it, and red-then-green Go tests and the Go conformance column prove it. No other language is touched, and no port is owed by this card.
SOURCE (Glenn, 12:33Z: "card the 25 schema improvements from both tracer findings as schema-repo cards, ranked, top ones first"): pong.md item 6. Both tracers pinned schema at 4d9941fbc7fd (main); every claim below was re-checked at this card's BASE (sprint/schema-go @ b262c81e4dba).

OWNER ANSWER: (Glenn, 2026-10-15 12:58-13:03Z, final, one by one): support BOTH, base64 of the binary wire (keeps the form byte) and the JSON text form, as Go text helpers; document when to use each, and both readers refuse damaged or newer input by name. (nova-bus's native binary form is a nova card, not this one.)

RANK 13 of 21 (high): no guidance or helpers for carrying a table over a text channel.

Why: An adopter with a text channel had no guidance (the pong tracer chose an encoding for a bus body by hand). The owner ruled: support both, as Go helpers, documented, and both refuse damaged or newer input by name.

THE TASK. Standard first: USAGE.md (and SPEC-TABLES.md beside section 16 for the normative part) says when to use each: base64 (padded standard) of the binary wire keeps the form byte, the framing check and every versioning guarantee and is opaque; the JSON text form is for a human to read or edit and carries what section 16 says it carries; and what each refuses: a base64 body that does not decode, a decoded wire that the binary reader refuses (form byte, framing, a newer form), a JSON document the JSON reader refuses, each by name. Then the Go emitter adds `<Name>SaveText(value) (string, error)` / `<Name>LoadText(text, report) ...` for base64, beside the existing JSON functions (renamed only if the spec asks), claims the names in internal/tablenames/go.go, and the refusals are typed reasons (TableRefuseReason values added at the end, never renumbered).

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run TestTableWireOverATextTransport -v` shows it RUN and PASS at head and FAIL at BASE (both helpers round-trip; a corrupted base64 body, a truncated decoded wire, a newer form byte and a damaged JSON document each refuse by name); make test-go is green or its only reds are named as present at BASE; the go column of make conformance is green.

ORDER. DEPENDS-ON go-fixed-load-sets-up-once-per-plan; go-table-writers-refuse-what-their-readers-refuse follows it. A running attempt keeps its brief; the next attempt takes this one.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set to the machine's shared build cache (JOB.md names it): keep it. Build and test only on the Linux bench JOB.md names, never the working Mac.
STEP 2. Standard first: write the rule into docs/USAGE.md and docs/SPEC-TABLES.md and commit it alone.
STEP 3. Red first: TestTableWireOverATextTransport in the new internal/codegen/gotable/texttransport_test.go (t.Parallel(), runGenerated). Run go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run TestTableWireOverATextTransport -v and keep the failing line.
STEP 4. Make it pass in the files PATHS names, citing the section.
STEP 5. Run the gate: go test -count=1 -timeout 600s ./internal/codegen/gotable/ ./compiler/ ./internal/ci/ and read the last line of each; then make test-go, and make conformance SCHEMA_SKIP_LEGS=cs,java,elixir,dart,js and read the go column (a red in another leg's column is named and compared against BASE, never fixed here), all on the bench. When a test fails, name its file and say whether that file was changed by your work (yours) or is unchanged (already red at BASE: run the same test on the unchanged base to say so), and report that line first.
STEP 6. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (the last STEP), and the member makes both, against the BASE: line's branch, from outside the wall when the card finishes. The pull request body states the diff stat, what was deleted, the tests with what each pins, and what was not done.
STEP 7. End as JOB.md says: where JOB.md ends the card with its pull request, that is the end and there is nothing else to write, the gate's lines in the pull request body; where it asks for RESULT.md, write it in JOB.md's shape (head, branch, verdict, gate, output, report).

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.
No gh, no push: JOB.md's finish does both, from outside the wall. Never merge. Every `go test` gets `-timeout 600s`; every new test opens with `t.Parallel()`; never `go clean`; never git stash.
Unit tests use no real time, sockets, network or outside services: time is an argument, the transport is faked, and anything that needs the real thing is a functional test run by a make target. A generated unit compiled and run by the package's own runGenerated helper in a t.TempDir() is the house pattern and stays inside the job.
Files a change must touch to keep the tree green are always inside PATHS, whatever the brief names: every *_test.go, every file under a testdata/ directory, tla/RUNS.tsv, tla/CASES.tsv and tla/COVERAGE.tsv, internal/docs/catalog.go, and every AGENTS.md map; any other file outside PATHS is still out of scope. ROADMAP.md, docs/roadmap.sexp and docs/roadmap-evidence/*.sexp are lander-owned ledgers inside every card's PATHS: commit the ROADMAP.md that make roadmap writes, never a hand edit.
Touch only the files PATHS names; a fix that needs another file goes into the report as a proposed diff, not a commit. No other language's emitter, runtime or driver is edited, and no other-language card is proposed: that work is parked by the owner. "Not done" is a welcome report; a green claim you did not run is not.
