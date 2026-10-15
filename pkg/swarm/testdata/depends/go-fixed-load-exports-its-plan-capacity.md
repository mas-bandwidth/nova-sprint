RESULT: go-fixed-load-exports-its-plan-capacity sha=b262c81e4dba tier: pro
REPO: mas-bandwidth/schema
BASE: sprint/schema-go
KIND: guard
PRIORITY: high
STREAM: schema-go
DEPENDS-ON: go-tables-get-a-strict-load, go-fixed-load-sets-up-once-per-plan
PATHS: internal/codegen/gotable/fixedform.go, internal/codegen/gotable/fixedlineage.go, internal/tablenames/go.go, docs/FIXED-FORM-ALGORITHM.md, docs/SPEC-TABLES.md, docs/USAGE.md, make/go.mk, docs/roadmap.sexp, ROADMAP.md
NEW: internal/codegen/gotable/fixedplanmax_test.go
TEST: internal/codegen/gotable TestFixedPlanMaxCoversEveryLineagePlan
START: internal/codegen/gotable/fixedform.go (the `int64(entryCount) > int64(len(plan))` check, plan_too_large), internal/codegen/gotable/fixedlineage.go (the lineage plans laid as static data), internal/codegen/gotable/fixedroadmap_t02_test.go (read), internal/codegen/gotable/fixedversioning_test.go (read), docs/FIXED-FORM-ALGORITHM.md (read)
STOP: when DONE-WHEN below holds and the commit is on your branch, or the deadline is reached; then report, with not-done named.
DEADLINE: finish within 120 minutes.
The REPO: and BASE: lines are the repository and the branch the work starts from and lands on: the member stages REPO: at BASE:, and nova-sprint land merges the card's head onto BASE: (land --base stands in for a card naming no BASE:, land --repo-dir for one naming no REPO:).
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 120 minutes; the judgment of a card that runs past it is the coordinator's, so report what you have with the verdict not-done rather than push past it.
Libraries considered: the Go standard library only; the constant is computed by the generator from the lineage it already holds.

# Go's fixed load exports the plan capacity every lineage layout needs

OWNER RULE (Glenn, 2026-10-15 12:34Z): "When you improve schema, improve the standard, and then fix it in golang and verify the fix. Stop the multi-language stuff until we have finished our dogfooding in golang." So the rule is written in the standard first, Go is then made to match it, and red-then-green Go tests and the Go conformance column prove it. No other language is touched, and no port is owed by this card.
SOURCE (Glenn, 12:33Z: "card the 25 schema improvements from both tracer findings as schema-repo cards, ranked, top ones first"): done-record.md item 8 (FixedLoad's plan argument is a capacity check; nova-sprint d53dd1a4). Both tracers pinned schema at 4d9941fbc7fd (main); every claim below was re-checked at this card's BASE (sprint/schema-go @ b262c81e4dba).

RANK 12 of 19 (high): adopter pain with a refusal edge. <T>FixedLoad's plan argument is only a capacity check, and the size a lineage plan needs is not exported, so adopters guess and a short guess refuses a lawful old file as plan_too_large.

Why: At BASE the plan slice a caller passes to <T>FixedLoad is only compared against the selected plan's entry count (plan_too_large); the largest count over the lineage is not exported. The done-record tracer guessed `2 * len(DoneRecordFixedPlan.Entries)`; the conformance tests use make(..., 4096). A guess that is short for an older layout refuses a lawful file.

THE TASK. Write the rule first in docs/FIXED-FORM-ALGORITHM.md 5.2 (and USAGE.md's Go notes): the generated code exports `<T>FixedPlanMax`, the largest entry count over the identity plan and every lineage plan, and a plan slice of that length never refuses plan_too_large for a layout the lineage holds. Keep the parameter (R25 holds the refusal). Emit the constant from fixedform.go/fixedlineage.go, claim it in internal/tablenames/go.go.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run TestFixedPlanMaxCoversEveryLineagePlan -v` shows it RUN and PASS at head and FAIL at BASE; R25 still passes; make tables-go-fixedform and make test-go are green or their only reds are named as present at BASE; the go column of make conformance is green.

ORDER. Independent; not beside the other fixedform.go cards (go-fixed-load-names-every-refusal, go-fixed-load-sets-up-once-per-plan).

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set to the machine's shared build cache (JOB.md names it): keep it. Build and test only on the Linux bench JOB.md names, never the working Mac.
STEP 2. The standard first: write the rule into docs/FIXED-FORM-ALGORITHM.md 5.2 and commit it alone.
STEP 3. Red first: TestFixedPlanMaxCoversEveryLineagePlan in the new internal/codegen/gotable/fixedplanmax_test.go (t.Parallel(), runGenerated, a lineage of two locked layouts where the older plan is the larger): a plan of <T>FixedPlanMax loads both images; one entry fewer refuses plan_too_large on the larger. Run go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run TestFixedPlanMaxCoversEveryLineagePlan -v and keep the failing line.
STEP 4. Make it pass in the files PATHS names, citing the section; add the test to tables-go-fixedform's proof list in make/go.mk.
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
