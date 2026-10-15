RESULT: generated-go-is-lint-clean-and-names-its-sources sha=b262c81e4dba tier: pro
REPO: mas-bandwidth/schema
BASE: sprint/schema-go
KIND: guard
PRIORITY: high
STREAM: schema-go
DEPENDS-ON: go-fixed-load-exports-its-plan-capacity
PATHS: internal/codegen/gotable/refusal.go, internal/codegen/gotable/gotable.go, internal/codegen/gotable/view.go, internal/codegen/gotable/json.go, internal/codegen/gotable/cook.go, internal/codegen/gotable/block.go, internal/codegen/gotable/fixedform.go, internal/codegen/gotable/message.go, internal/codegen/golang/golang.go, generated/**, docs/USAGE.md, docs/roadmap.sexp, ROADMAP.md
NEW: internal/codegen/gotable/generatedtext_test.go
TEST: internal/codegen/gotable TestGeneratedGoTextIsHonestAndLintClean
START: internal/codegen/gotable/refusal.go (TableBuildVersionMismatch.Error: `m.Peer>>=4` and `m.Own>>=4` on a value receiver, whose last shift is never read: staticcheck SA4005), internal/codegen/gotable/gotable.go (assemble: 'from %s.schema' and 'Wide values use serialize.go storage'), internal/codegen/gotable/view.go (base := GoExportName(Package)+"View", so the header names <Pkg>View.schema, which does not exist), internal/codegen/gotable/json.go, internal/codegen/gotable/block.go, internal/codegen/gotable/cook.go, docs/USAGE.md (read)
STOP: when DONE-WHEN below holds and the commit is on your branch, or the deadline is reached; then report, with not-done named.
DEADLINE: finish within 120 minutes.
The REPO: and BASE: lines are the repository and the branch the work starts from and lands on: the member stages REPO: at BASE:, and nova-sprint land merges the card's head onto BASE: (land --base stands in for a card naming no BASE:, land --repo-dir for one naming no REPO:).
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 120 minutes; the judgment of a card that runs past it is the coordinator's, so report what you have with the verdict not-done rather than push past it.
Libraries considered: the Go standard library (go/parser, go/ast, go/token to read the emitted source in the test); staticcheck is not a test dependency: the test checks the two rules directly and the gate runs staticcheck only where the bench has it.

# Generated Go is lint-clean and names its real sources

OWNER RULE (Glenn, 2026-10-15 12:34Z): "When you improve schema, improve the standard, and then fix it in golang and verify the fix. Stop the multi-language stuff until we have finished our dogfooding in golang." So the rule is written in the standard first, Go is then made to match it, and red-then-green Go tests and the Go conformance column prove it. No other language is touched, and no port is owed by this card.
SOURCE (Glenn, 12:33Z: "card the 25 schema improvements from both tracer findings as schema-repo cards, ranked, top ones first"): pong.md item 8 (schema-generated-go-lint-clean) merged with done-record.md item 11 (generated headers name a file that does not exist and a dependency not used): both are the generated Go's own text. Both tracers pinned schema at 4d9941fbc7fd (main); every claim below was re-checked at this card's BASE (sprint/schema-go @ b262c81e4dba).

RANK 9 of 19 (high): adopter pain. Generated Go trips staticcheck (SA4005) and adopters' path linters, and its headers name a file that does not exist and a dependency it does not use.

Why: At BASE: (1) TableBuildVersionMismatch.Error (refusal.go) shifts its value receiver's fields in each hex loop and never reads the last shift: staticcheck SA4005 at the pong tracer's pongTable.go:288 and :292. (2) A view file says 'generated from <Pkg>View.schema', a file that does not exist (view.go's synthetic Base). (3) Every table file says 'Wide values use serialize.go storage' while importing nothing from it. (4) Comments cite this repository's own paths (the tables spec page, the Go packet emitter, the table-names package) with no repository named, which adopters' path linters read as their own repo's paths. Dropped from pong item 8: the JSON reader's whitespace method name colliding with a host name in nova's own token list is nova's linter's allowlist to fix, not schema's.

THE TASK. Write the rule first in docs/USAGE.md's Go notes: generated Go passes go vet and staticcheck; its header names the .schema file it came from, or says 'for package <p>' when there is none; it names serialize.go only when it imports it; and a path it cites in a comment is prefixed with the schema repository's name (`mas-bandwidth/schema` then the page), so the reference reads as another repo's. Then fix the emitters to match, re-emit the in-tree generated Go under generated/ with the make target that owns it (never by hand), and re-pin any golden the text change moves.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run TestGeneratedGoTextIsHonestAndLintClean -v` shows it RUN and PASS at head and FAIL at BASE; go vet over a generated unit is clean; staticcheck over a generated unit reports no SA4005 where the bench has staticcheck (say so if it has none); make test-go is green or its only reds are named as present at BASE; the go column of make conformance is green.

ORDER. Not beside go-generate-emits-only-the-surface-asked-for (both edit gotable.go's assembly). It moves comment text in many goldens: run it alone in the stream.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set to the machine's shared build cache (JOB.md names it): keep it. Build and test only on the Linux bench JOB.md names, never the working Mac.
STEP 2. The standard first: write the rule into docs/USAGE.md's Go notes and commit it alone.
STEP 3. Red first: TestGeneratedGoTextIsHonestAndLintClean in the new internal/codegen/gotable/generatedtext_test.go (t.Parallel()): generate a unit with a variable table, a fixed table and a view; parse each file with go/parser; assert no method with a value receiver assigns to a receiver field, every header names an existing .schema or says 'for package', 'serialize.go' appears only in files that import it, and no comment cites a bare docs/ or internal/ path. Run go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run TestGeneratedGoTextIsHonestAndLintClean -v and keep the failing line.
STEP 4. Make it pass in the files PATHS names; re-emit generated/ by its make target and re-pin moved goldens.
STEP 5. Run the gate: go test -count=1 -timeout 600s ./internal/codegen/gotable/ ./compiler/ ./internal/ci/ and read the last line of each; then go vet and, where present, staticcheck over one generated unit; then make test-go, and make conformance SCHEMA_SKIP_LEGS=cs,java,elixir,dart,js and read the go column (a red in another leg's column is named and compared against BASE, never fixed here), all on the bench. When a test fails, name its file and say whether that file was changed by your work (yours) or is unchanged (already red at BASE: run the same test on the unchanged base to say so), and report that line first.
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
