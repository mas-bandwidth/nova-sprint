RESULT: usage-adopting-tables-from-go sha=b262c81e4dba tier: pro
REPO: mas-bandwidth/schema
BASE: sprint/schema-go
KIND: guard
PRIORITY: high
STREAM: schema-go
DEPENDS-ON: generated-go-is-lint-clean-and-names-its-sources
PATHS: docs/USAGE.md, docs/CARDS.md, compiler/docs_test.go, internal/codegen/gotable/usagerecipe_test.go, docs/roadmap.sexp, ROADMAP.md
NEW: internal/codegen/gotable/usagerecipe_test.go
TEST: internal/codegen/gotable TestUsageGoTablesRecipeRuns
START: docs/USAGE.md (read), docs/CARDS.md (read), compiler/docs_test.go (read), internal/codegen/gotable/values_test.go (read), internal/codegen/gotable/fixedform.go (read)
STOP: when DONE-WHEN below holds and the commit is on your branch, or the deadline is reached; then report, with not-done named.
DEADLINE: finish within 120 minutes.
The REPO: and BASE: lines are the repository and the branch the work starts from and lands on: the member stages REPO: at BASE:, and nova-sprint land merges the card's head onto BASE: (land --base stands in for a card naming no BASE:, land --repo-dir for one naming no REPO:).
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 120 minutes; the judgment of a card that runs past it is the coordinator's, so report what you have with the verdict not-done rather than push past it.
Libraries considered: the Go standard library only (strings, os to pull the fenced blocks out of the page); runGenerated runs them.

# USAGE.md: adopting tables from Go, variable and fixed

OWNER RULE (Glenn, 2026-10-15 12:34Z): "When you improve schema, improve the standard, and then fix it in golang and verify the fix. Stop the multi-language stuff until we have finished our dogfooding in golang." So the rule is written in the standard first, Go is then made to match it, and red-then-green Go tests and the Go conformance column prove it. No other language is touched, and no port is owed by this card.
SOURCE (Glenn, 12:33Z: "card the 25 schema improvements from both tracer findings as schema-repo cards, ranked, top ones first"): pong.md item 9 (schema-go-adopter-recipe) merged with done-record.md item 14 (no Go usage page for the fixed form): one USAGE.md section. Both tracers pinned schema at 4d9941fbc7fd (main); every claim below was re-checked at this card's BASE (sprint/schema-go @ b262c81e4dba).

RANK 10 of 19 (high): adopter pain. Both tracers learned the Go surface from generated code: the pin recipe is undocumented and there is no Go page for the fixed form.

Why: docs/CARDS.md 'Import and module setup' describes only a local module plus replace. Both tracers had to discover that table-only units are stdlib-only, that `go run github.com/mas-bandwidth/schema/v2/cmd/schema@<version>` in a go:generate line pins the compiler without touching go.mod (it worked under -mod=readonly and was byte-identical across two build paths), when serialize.go is needed, and the whole fixed-form surface (FixedSave, FixedLoad, the plan, TableReport.Reason, the lineage, locking before an edit). SPEC-TABLES.md is about 1 MB and FIXED-FORM-ALGORITHM.md is language-neutral.

THE TASK. The standard here IS the page: write a 'Tables from Go' section in docs/USAGE.md (under 'Per-language notes' or beside the tables section) holding: the go:generate pin line and why it needs no go.mod change; table-only units need no module; when serialize.go is needed (`require` it at a tagged version, no replace); a variable table saved and loaded with its report read; a fixed table saved and loaded, the plan argument sized, the refusal names, and `schema lock` before the first edit. Every Go block in the section is a complete test body the red test runs; the section's schema block is listed in compiler/docs_test.go. Point docs/CARDS.md's 'Import and module setup' at it. Write only what the code at BASE does: where a sibling card (go-tables-get-a-strict-load, go-fixed-load-names-every-refusal, go-fixed-load-exports-its-plan-capacity) has not landed, document today's surface and say in the report which paragraph each would change.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run TestUsageGoTablesRecipeRuns -v` shows it RUN and PASS at head and FAIL at BASE (the section is missing); go test -count=1 -timeout 600s ./compiler/ -run TestDocExamplesParse is green with the new block listed; make test-go is green or its only reds are named as present at BASE.

ORDER. Best after go-tables-get-a-strict-load, go-fixed-load-names-every-refusal and go-fixed-load-exports-its-plan-capacity, but not blocked by them.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set to the machine's shared build cache (JOB.md names it): keep it. Build and test only on the Linux bench JOB.md names, never the working Mac.
STEP 2. Red first: TestUsageGoTablesRecipeRuns in the new internal/codegen/gotable/usagerecipe_test.go (t.Parallel()): find the 'Tables from Go' section in docs/USAGE.md, take its schema block and each fenced Go block, and run each Go block through runGenerated against the schema; it fails at BASE naming the missing section. Run go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run TestUsageGoTablesRecipeRuns -v and keep the failing line.
STEP 3. Write the section (the standard), list its schema block in compiler/docs_test.go, and point docs/CARDS.md at it.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./internal/codegen/gotable/ ./compiler/ ./internal/ci/ and read the last line of each; then make test-go on the bench. When a test fails, name its file and say whether that file was changed by your work (yours) or is unchanged (already red at BASE: run the same test on the unchanged base to say so), and report that line first.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (the last STEP), and the member makes both, against the BASE: line's branch, from outside the wall when the card finishes. The pull request body states the diff stat, what was deleted, the tests with what each pins, and what was not done.
STEP 6. End as JOB.md says: where JOB.md ends the card with its pull request, that is the end and there is nothing else to write, the gate's lines in the pull request body; where it asks for RESULT.md, write it in JOB.md's shape (head, branch, verdict, gate, output, report).

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
