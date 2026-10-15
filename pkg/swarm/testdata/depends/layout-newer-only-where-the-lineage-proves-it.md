RESULT: layout-newer-only-where-the-lineage-proves-it sha=b262c81e4dba tier: pro
REPO: mas-bandwidth/schema
BASE: sprint/schema-go
KIND: guard
PRIORITY: critical
STREAM: schema-go
DEPENDS-ON: fixed-count-and-length-past-bound-refuse-or-clamp
PATHS: internal/codegen/gotable/fixedform.go, internal/codegen/gotable/fixedruntime.go, internal/codegen/gotable/fixedlineage.go, internal/tablenames/go.go, docs/SPEC-TABLES.md, docs/FIXED-FORM-ALGORITHM.md, docs/VERSIONING.md, docs/USAGE.md, make/go.mk, docs/roadmap.sexp, ROADMAP.md
NEW: internal/codegen/gotable/fixedunknownhash_test.go
TEST: internal/codegen/gotable TestFixedLoadUnknownHashIsLayoutUnknown
START: internal/codegen/gotable/fixedform.go (STEP 5 and 6 of the emitted <T>FixedLoad: `pick < 0` returns layout_newer), docs/SPEC-TABLES.md (section 3.4 and section 21), docs/FIXED-FORM-ALGORITHM.md (5.2, 5.3), docs/FIXED-FORM-BILL-READS-BACKWARD.md (read), internal/codegen/gotable/fixedroadmap_t02_test.go (R23 pins the layout_newer site), compiler/lineage.go (read), internal/lockfile/lineage.go (read)
STOP: when DONE-WHEN below holds and the commit is on your branch, or the deadline is reached; then report, with not-done named.
DEADLINE: finish within 180 minutes.
The REPO: and BASE: lines are the repository and the branch the work starts from and lands on: the member stages REPO: at BASE:, and nova-sprint land merges the card's head onto BASE: (land --base stands in for a card naming no BASE:, land --repo-dir for one naming no REPO:).
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 180 minutes; the judgment of a card that runs past it is the coordinator's, so report what you have with the verdict not-done rather than push past it.
Libraries considered: the Go standard library only.

# layout_unknown for a hash the lineage does not hold; layout_newer only where the lineage proves it

OWNER RULE (Glenn, 2026-10-15 12:34Z): "When you improve schema, improve the standard, and then fix it in golang and verify the fix. Stop the multi-language stuff until we have finished our dogfooding in golang." So the rule is written in the standard first, Go is then made to match it, and red-then-green Go tests and the Go conformance column prove it. No other language is touched, and no port is owed by this card.
SOURCE (Glenn, 12:33Z: "card the 25 schema improvements from both tracer findings as schema-repo cards, ranked, top ones first"): done-record.md item 6. Both tracers pinned schema at 4d9941fbc7fd (main); every claim below was re-checked at this card's BASE (sprint/schema-go @ b262c81e4dba).

OWNER ANSWER: (Glenn, 2026-10-15 12:58-13:03Z, final, one by one): ADD `layout_unknown`; `layout_newer` only when the lock lineage proves the layout newer. The rename is Go first.

RANK 4 of 21 (critical): corruption reads as layout_newer and sends the operator after a newer reader that does not exist.

Why: At BASE every hash outside the lineage answers layout_newer, so a flipped hash byte or an image of a never-locked layout tells the operator to ship a newer reader. The owner ruled: add layout_unknown, and keep layout_newer only where the lock lineage proves direction.

THE TASK. Standard first: SPEC-TABLES.md 3.4 and 21, FIXED-FORM-ALGORITHM.md 5.3, VERSIONING.md's read report and USAGE.md's refusal list define layout_unknown ('a layout hash this build's lineage does not hold') and narrow layout_newer to what the lock lineage proves. Work out, from the lineage the generator holds, what can prove direction (for example a hash the lock recorded as RETIRED or a lineage entry this build was generated to refuse); write it down, and if nothing on the wire or in the lock can prove it, say so in the spec and in the report: then every unknown hash is layout_unknown and layout_newer is reserved, never emitted, until something proves it. Then the Go fixed load answers to match (the file's hash still in report.LayoutHash), the name is exported as a constant and claimed in internal/tablenames/go.go, and R23 in fixedroadmap_t02_test.go follows the rule, never deleted. Other languages keep answering layout_newer until their turn; the spec says Go is first and names the gap.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run TestFixedLoadUnknownHashIsLayoutUnknown -v` shows it RUN and PASS at head and FAIL at BASE (a flipped hash byte and an unlocked older image answer layout_unknown; any case the lineage proves newer answers layout_newer); make tables-go-fixedform and make test-go are green or their only reds are named as present at BASE; the go column of make conformance is green.

ORDER. DEPENDS-ON fixed-count-and-length-past-bound-refuse-or-clamp; go-tables-get-a-strict-load follows it.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set to the machine's shared build cache (JOB.md names it): keep it. Build and test only on the Linux bench JOB.md names, never the working Mac.
STEP 2. Standard first: write the rule into the docs named and commit it alone.
STEP 3. Red first: TestFixedLoadUnknownHashIsLayoutUnknown in the new internal/codegen/gotable/fixedunknownhash_test.go (t.Parallel(), runGenerated: a record with one hash byte flipped, an image of an older layout with no lock, and an image of a newer locked layout read by a build that does not hold it). Run go test -count=1 -timeout 600s ./internal/codegen/gotable/ -run TestFixedLoadUnknownHashIsLayoutUnknown -v and keep the failing line.
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
