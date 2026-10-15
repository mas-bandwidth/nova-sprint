RESULT: go-fixed-load-names-every-refusal sha=b262c81e4dba tier: pro
REPO: mas-bandwidth/schema
BASE: sprint/schema-go
KIND: guard
PRIORITY: critical
STREAM: schema-go
PATHS: internal/codegen/gotable/fixedform.go, internal/codegen/gotable/fixedruntime.go, internal/codegen/gotable/refusal.go, internal/tablenames/go.go, docs/SPEC-TABLES.md, docs/FIXED-FORM-ALGORITHM.md, docs/USAGE.md, make/go.mk, docs/roadmap.sexp, ROADMAP.md
NEW: internal/codegen/gotable/fixedrefusalnames_test.go
TEST: internal/codegen/gotable TestFixedLoadNamesEveryRefusal
START: internal/codegen/gotable/fixedform.go (the emitted <T>FixedLoad: the `len(data) < 1`, `len(data) < TableFixedHeaderBytes+4`, reserved-byte and `rest%recordBytes != 0` returns set report.Malformed and return -1 with no Reason), internal/codegen/gotable/fixedruntime.go (tableFixedRefuse, tableFixedRefuseHash, every string-literal name), internal/codegen/gotable/refusal.go (read: TableRefuseReason, the cooked form's typed reason), docs/FIXED-FORM-ALGORITHM.md (read: 5.3 and the refusal table), docs/SPEC-TABLES.md (read: section 3.4 'THE FRAMING' and 'THE FORM-BYTE REGISTRY'), internal/codegen/gotable/fixedform_under_20_bytes_test.go (read), internal/codegen/gotable/fixedform_ragged_tail_test.go (read)
STOP: when DONE-WHEN below holds and the commit is on your branch, or the deadline is reached; then report, with not-done named.
DEADLINE: finish within 120 minutes.
The REPO: and BASE: lines are the repository and the branch the work starts from and lands on: the member stages REPO: at BASE:, and nova-sprint land merges the card's head onto BASE: (land --base stands in for a card naming no BASE:, land --repo-dir for one naming no REPO:).
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 120 minutes; the judgment of a card that runs past it is the coordinator's, so report what you have with the verdict not-done rather than push past it.
Libraries considered: the Go standard library only; the names are exported string constants in the emitted runtime, so an adopter's existing `report.Reason == "layout_newer"` comparisons keep compiling.

# Go's fixed load names every refusal, and exports the names

OWNER RULE (Glenn, 2026-10-15 12:34Z): "When you improve schema, improve the standard, and then fix it in golang and verify the fix. Stop the multi-language stuff until we have finished our dogfooding in golang." So the rule is written in the standard first, Go is then made to match it, and red-then-green Go tests and the Go conformance column prove it. No other language is touched, and no port is owed by this card.
SOURCE (Glenn, 12:33Z: "card the 25 schema improvements from both tracer findings as schema-repo cards, ranked, top ones first"): done-record.md item 1 (fixed-form framing damage has no refusal name) merged with item 2 (fixed-form refusal names are not exported): one surface, the fixed load's refusal names. Both tracers pinned schema at 4d9941fbc7fd (main); every claim below was re-checked at this card's BASE (sprint/schema-go @ b262c81e4dba).

RANK 5 of 19 (critical): a silent refusal. Four framing-damage returns of <T>FixedLoad leave Reason empty, so a caller that switches on Reason sees nothing; every other refusal is named, and none of the names is exported.

Why: At BASE the emitted <T>FixedLoad returns -1 with report.Malformed set and report.Reason empty on a record with no first byte, a header shorter than the twenty bytes, a non-zero reserved byte, and a record tail that is not a whole number of records; every other refusal sets a name (previous_form, newer_form, layout_malformed, layout_newer, plan_too_large, no_layout, text_ill_formed, ...). Those names live only as string literals, so an adopter matches "layout_newer" by hand.

THE TASK. Write the rule first: in docs/FIXED-FORM-ALGORITHM.md 5.3 (and SPEC-TABLES.md 3.4 where it lists the fixed load's answers) every refusal of the fixed load has a name, and the framing-damage returns are named (use the names the page already gives if it gives them; otherwise `truncated` for a short input or ragged tail and `reserved_not_zero` for a reserved byte, matching the cooked form's TableRefuseTruncated and TableRefuseReservedNotZero spellings); Malformed stays set beside the name. Then: every return in <T>FixedLoad sets Reason, and the emitted runtime exports one string constant per fixed-load refusal name (for example TableFixedReasonLayoutNewer = "layout_newer"), used at every site in place of the literal and claimed in internal/tablenames/go.go. USAGE.md's Go notes list the constants.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run TestFixedLoadNamesEveryRefusal -v` shows it RUN and PASS (not SKIP) at head and FAIL at BASE; no `report.Malformed = true` return in the emitted fixed load leaves Reason empty; the rule is in FIXED-FORM-ALGORITHM.md 5.3 and cited from fixedform.go; make tables-go-fixedform and make test-go are green or their only reds are named as present at BASE; the go column of make conformance is green.

ORDER. Lands before layout-newer-only-where-the-lineage-proves-it and go-tables-get-a-strict-load if they are ready together (they read these names); not beside fixed-count-and-length-past-bound-refuse-or-clamp (same file).

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set to the machine's shared build cache (JOB.md names it): keep it. Build and test only on the Linux bench JOB.md names, never the working Mac.
STEP 2. The standard first: write the naming rule into docs/FIXED-FORM-ALGORITHM.md 5.3 and SPEC-TABLES.md 3.4 and commit it alone.
STEP 3. Red first: TestFixedLoadNamesEveryRefusal in the new internal/codegen/gotable/fixedrefusalnames_test.go (t.Parallel(), runGenerated over a small fixed table): an empty input, a 19-byte header, a non-zero reserved byte, a ragged tail, and one case per existing name; each refusal returns -1 with a non-empty Reason equal to the exported constant. Run go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run TestFixedLoadNamesEveryRefusal -v and keep the failing line.
STEP 4. Make it pass in the files PATHS names, citing the section from each function; existing tests that match the literal strings keep passing because the constants hold the same text. Add the test to tables-go-fixedform's proof list in make/go.mk.
STEP 5. Run the gate: go test -count=1 -timeout 600s ./internal/codegen/gotable/ ./compiler/ ./internal/ci/ and read the last line of each; then make tables-go-fixedform; then make test-go, and make conformance SCHEMA_SKIP_LEGS=cs,java,elixir,dart,js and read the go column (a red in another leg's column is named and compared against BASE, never fixed here), all on the bench. When a test fails, name its file and say whether that file was changed by your work (yours) or is unchanged (already red at BASE: run the same test on the unchanged base to say so), and report that line first.
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
