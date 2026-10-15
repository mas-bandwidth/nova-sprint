RESULT: go-tables-get-a-strict-load sha=b262c81e4dba tier: pro
REPO: mas-bandwidth/schema
BASE: sprint/schema-go
KIND: guard
PRIORITY: high
STREAM: schema-go
DEPENDS-ON: layout-newer-only-where-the-lineage-proves-it
PATHS: internal/codegen/gotable/read.go, internal/codegen/gotable/regions.go, internal/codegen/gotable/refusal.go, internal/codegen/gotable/gotable.go, internal/tablenames/go.go, docs/SPEC-TABLES.md, docs/USAGE.md, docs/VERSIONING.md, docs/roadmap.sexp, ROADMAP.md
NEW: internal/codegen/gotable/strictload_test.go
TEST: internal/codegen/gotable TestGoStrictLoadRefusesEveryLossByName
START: internal/codegen/gotable/refusal.go (TableRefuseReason, TableReport.Err), internal/codegen/gotable/regions.go (<T>LoadMeasureReason, <T>LoadBuilder), internal/codegen/gotable/read.go (<T>Load), internal/codegen/gotable/gotable.go (the TableReport struct: Malformed, Clamped, KindMismatch, Unknown, Widened), docs/CARDS.md (read), docs/SPEC-TABLES.md (read), docs/VERSIONING.md (read)
STOP: when DONE-WHEN below holds and the commit is on your branch, or the deadline is reached; then report, with not-done named.
DEADLINE: finish within 120 minutes.
The REPO: and BASE: lines are the repository and the branch the work starts from and lands on: the member stages REPO: at BASE:, and nova-sprint land merges the card's head onto BASE: (land --base stands in for a card naming no BASE:, land --repo-dir for one naming no REPO:).
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 120 minutes; the judgment of a card that runs past it is the coordinator's, so report what you have with the verdict not-done rather than push past it.
Libraries considered: the Go standard library (errors.Is/As for the typed error); the existing TableRefuseReason type, extended rather than replaced; nothing new.

# Go tables get a one-call strict load and a report error that covers every counter

OWNER RULE (Glenn, 2026-10-15 12:34Z): "When you improve schema, improve the standard, and then fix it in golang and verify the fix. Stop the multi-language stuff until we have finished our dogfooding in golang." So the rule is written in the standard first, Go is then made to match it, and red-then-green Go tests and the Go conformance column prove it. No other language is touched, and no port is owed by this card.
SOURCE (Glenn, 12:33Z: "card the 25 schema improvements from both tracer findings as schema-repo cards, ranked, top ones first"): pong.md item 4 (schema-go-table-strict-load; nova-tools 9ef91022). Both tracers pinned schema at 4d9941fbc7fd (main); every claim below was re-checked at this card's BASE (sprint/schema-go @ b262c81e4dba).

RANK 6 of 19 (high): adopter pain with a safety edge. A safe, refusing read takes four calls and four report checks, <T>Load returns nil with no reason, and TableReport.Err() covers only build_version_mismatch, so adopters hand-write the strict read and can get it wrong.

Why: At BASE a Go adopter who wants a read that refuses anything lossy calls <T>LoadMeasureReason, the builder's Init, <T>LoadBuilder, and then checks Malformed, Clamped and KindMismatch by hand; <T>Load returns nil with no reason; TableReport.Err() returns an error only for build_version_mismatch. nova-tools' pong tracer and the cards facade (docs/CARDS.md's ReadStrict) each wrote the same strict read by hand.

THE TASK. Write the rule first: in docs/SPEC-TABLES.md section 6.5 and docs/VERSIONING.md 'The read report', define the STRICT read: it refuses, with one typed reason, a read that is malformed, clamped, kind-mismatched, of an unknown form, wire-damaged or build-version-mismatched; it does not refuse Unknown, Widened or Retained alone (the precedent of the cards facade's ReadStrict, docs/CARDS.md (read)). Then emit, for each root table, `<Name>ReadStrict(wire []byte) (*<Name>, error)` (one call, its own builder, the reason a TableRefuseReason or TableBuildVersionMismatch so errors.Is works), and make TableReport.Err() return the typed reason for every counter the strict read refuses, still nil on a clean read. New TableRefuseReason values (for example clamped, kind_mismatch) are added at the end of the enum, never renumbered, and claimed in internal/tablenames/go.go. docs/USAGE.md's Go notes show the one call.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run TestGoStrictLoadRefusesEveryLossByName -v` shows it RUN and PASS at head and FAIL at BASE; the rule is in SPEC-TABLES.md 6.5 and VERSIONING.md and cited from the emitter; make test-go is green or its only reds are named as present at BASE; the go column of make conformance is green.

ORDER. After go-fixed-load-names-every-refusal if both are ready (both touch refusal.go); independent of the rest.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set to the machine's shared build cache (JOB.md names it): keep it. Build and test only on the Linux bench JOB.md names, never the working Mac.
STEP 2. The standard first: write the strict-read rule into docs/SPEC-TABLES.md 6.5 and docs/VERSIONING.md 'The read report' and commit it alone.
STEP 3. Red first: TestGoStrictLoadRefusesEveryLossByName in the new internal/codegen/gotable/strictload_test.go (t.Parallel(), runGenerated): a clean wire reads; a clamped value, a kind mismatch, a truncated wire, an unknown form byte and a damaged wire each refuse with errors.Is to the named reason; a wire with only an unknown field reads; TableReport.Err() agrees with ReadStrict on every case. Run go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run TestGoStrictLoadRefusesEveryLossByName -v and keep the failing line.
STEP 4. Make it pass in the files PATHS names, citing the section from each function.
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
