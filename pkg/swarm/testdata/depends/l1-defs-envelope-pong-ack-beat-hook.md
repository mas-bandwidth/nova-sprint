RESULT: l1-defs-envelope-pong-ack-beat-hook sha=<sha12> tier: heavy
REPO: mas-bandwidth/nova-tools
BASE: v1.3/comms
KIND: feature
PRIORITY: high
STREAM: v13-schema-adoption
DEPENDS-ON: nova-tools-schema-home-and-pin,nova-schema-conformance-ci,l1-tla-typed-frames,nova-tools-strict-read-and-form-counter,table-wire-over-text-transports
SOURCE: nova-sprint docs/research/schema-tables/UNION.md (read) at 98f8a280 (branch sprint/schema-adoption-union-fixes.w2.g1.e21) section 6, C10 and sections 4.1, 4.2 and 4.6 (the definitions and the named refusals).
PATHS: Makefile,pkg/buswire/layer1_test.go
NEW: schemas/bus/,schemas/bus/bus.schema,schemas/bus/tables.baseline,pkg/buswire/,pkg/buswire/generate.go
TEST: pkg/buswire TestLayer1Golden
DONE-WHEN: `go test -count=1 -timeout 600s ./pkg/buswire -run TestLayer1Golden` passes (red first): each type round-trips as binary wire, as base64 text and as JSON text through pkg/schemaread; each named refusal of section 4.6 fires on its bound; each renders to the name-keyed text form; `make schemawire-check` and `make schemawire-conformance` exit 0; schemas/bus/tables.baseline is committed; the generate line pins a schema pseudo-version that contains table-wire-over-text-transports
TOOLCHAIN: go (the version go.mod names) on the Linux bench JOB.md names
STOP: the definitions and their golden test are committed with DONE-WHEN holding, or the deadline is reached
DEADLINE: finish within 420 minutes.

The REPO: and BASE: lines are the repository and the branch the work starts from and lands on: the member stages REPO: at BASE:, and nova-sprint land merges the card's head onto BASE: (land --base stands in for a card naming no BASE:, land --repo-dir for one naming no REPO:).
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 420 minutes; the judgment of a card that runs past it is the coordinator's, so report what you have with the verdict not-done rather than push past it.
Libraries considered: the schema compiler at its pinned pseudo-version (go run, no go.mod change); pkg/schemaread; the generated SaveText and LoadText helpers for base64 and the generated JSON functions.

THIS REPOSITORY. mas-bandwidth/nova-tools: the fleet's Go tools (nova-bus, nova-friend, the hook, typed records). `go test ./...` is the suite; internal/ci holds the repository's class tests; tla/ holds the TLA+ models, tla/RUNS.tsv their TLC records (tla/README.md (read): 'Refreshing the records after a model edit', run with tools/tlacheck). nova-sprint imports this repository's pkg/cardhdr, pkg/typedrec, hook and pkg/bus, so a type both repos read is defined once here.

# C10: the Layer 1 definitions (envelope, pong, ack, beat, hook stamp) and their generated Go
THE OWNER'S RULES (Glenn, 2026-10-15, binding on every schema-adoption card; a conflict is reported, never settled by weakening a rule). S1 Adopt BOTH table forms: a FIXED table for small, bounded, server-owned records whose reader is always the same version or newer; a VARIABLE table for every message an older reader may meet (everything friends exchange). S2 Text transports carry BOTH forms: padded standard base64 of the binary wire (it keeps the form byte and the framing check) and the schema JSON text form; a reader accepts either, refuses damage by name, and counts reads per form. S3 nova-bus gets a native binary form (no base64 on the bus itself). S4 Returning friends just update: no offline-compatibility policy, no long dual-emit window; each cutover is readers first, fleet-wide, then writers, in a short coordinated window, with the per-form read counter as the evidence. S5 Schema-side fixes go standard first, then Go, then verified in Go; no other language is touched. S6 Full nova adoption is the bar before the table forms are offered to other people.

THE TASK. Write schemas/bus/bus.schema with the variable tables of UNION.md sections 4.1 and 4.2: Envelope (with the Kind enum and the Name element, bounds from pkg/bus/bus.go (read)), Pong (friend, nonce text(32), queue, working, width with the tracer's bounds, sent_ms), Ack (id, stage, push, reason), the beat's hot scalars, and the hook stamp with int64 millisecond times. Fields a liveness count needs are required or optional by the schema's rule, never absent-as-zero. Generate pkg/buswire with generate.go holding the pin, commit the baseline, and write the golden test.

SCHEMA IMPROVEMENTS WANTED. The pull request body (and RESULT.md where JOB.md asks for one) carries a section headed exactly 'Schema improvements wanted': one item per friction this card met in the schema compiler, its generated Go or its docs, each with a title, the evidence (file and line, or the command and its output) and the smallest fix; write 'none met' when there were none. Name an existing schema-go or schema-shared card when it already covers the item. Fixes go standard first, then Go, then verified in Go (S5); this card does not make them in the schema repository.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names.
STEP 2. Red first: write TestLayer1Golden in pkg/buswire/layer1_test.go, opening with t.Parallel(). Run go test -count=1 -timeout 600s ./pkg/buswire/ -run TestLayer1Golden and keep the failing line.
STEP 3. Write the schema, generate, and make it pass.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./pkg/buswire/ ./pkg/schemaread/ ./internal/ci/ and read the last line of each. When a test fails, name its file and say whether that file was changed by your work (yours) or is unchanged (already red at BASE: run the same test on the unchanged base to say so), and report that line first.
STEP 5. Commit as `<card id>: <what>` with the trailer and end as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the pull request body states the diff stat, what was deleted, the tests with what each pins, the 'Schema improvements wanted' section, and what was not done.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.
Unit tests use no real time, no sockets, no network and no outside services; a test that needs any of them is a functional test and keeps to that tier.
Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set to the machine's shared build cache (JOB.md names it): keep it.
Build and test only on the Linux bench JOB.md names, never the coordinator's machine; TLC runs on a Linux bench, never on the coordinator's machine.
Commit on your own branch; no gh, no push: JOB.md's finish does both, from outside the wall. Never merge into BASE.
No dependency the card does not name: no package-manager fetch beyond the pinned `go run` of the schema compiler a generate line names.
Touch only the files PATHS and NEW name; a fix that needs another file goes into the report as a proposed diff, not a commit.
Docs and comments in the present tense, with no names of people or machines in code.
"Not done" is a welcome report; a green claim you did not run is not.
