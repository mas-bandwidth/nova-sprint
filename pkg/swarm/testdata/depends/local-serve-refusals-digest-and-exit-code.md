RESULT: local-serve-refusals-digest-and-exit-code sha=<sha12> tier: pro
REPO: mas-bandwidth/nova-tools
BASE: v1.3/comms
STOP: serve --expect-digest refuses at exit 1 when the model named is not listed under its reference, a tagless reference matches its :latest listing, every serve refusal exits by its Refusal.Code, and TestServeExpectDigestNeverSkipsSilently and TestServeRefusalExitsByItsCode pin both.
PATHS: cmd/nova-local/main.go, cmd/nova-local/engine.go, cmd/nova-local/ollama.go, cmd/nova-local/main_test.go, cmd/nova-local/fake_test.go, docs/SPEC-LOCAL.md, docs/CLI.md
SHARED: cmd/nova-local/main.go, docs/SPEC-LOCAL.md, docs/CLI.md
TEST: cmd/nova-local TestServeExpectDigestNeverSkipsSilently
DEPENDS-ON: local-rule1-no-shelling-out
SOURCE: Glenn, ~14:13Z 2026-10-15: "Please add the work for nova-local to the current sprint, low priority."; the nova-tools roadmap item nova-local-provider-per-machine ("Restore nova-local built to its spec, with local as a provider per fleet machine that the sprint deals to like any route"; from PR #5307, closed 2026-10-10 when the sprint moved to mas-bandwidth/nova-sprint, its head f322683d4d kept on origin/rowan/nova-local).
PRIORITY: low
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 60 minutes; report what you have with the verdict not-done rather than push past it.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.
Unit tests use no real time, no sockets, no network and no outside services: time is an argument (a fake clock passed in, never sleep), the engine is a fake behind the injected transport, the store is the mem twin.
No local model on the working machine, and none from this card anywhere: never install, pull, start or stop ollama or any engine, and never run nova-local serve against a real engine; every engine this card touches is a fake. A test that would need a real model belongs in the functional tier, skipped where none is present.
A refusal or a check is never routed around, and no existing check is weakened: the change adds a check or moves one earlier, it never turns one off.

THE TASK. Two serve refusals of docs/SPEC-LOCAL.md (rule 4 and "### serve") do not hold. Line numbers are at the v1.3/comms tip.
(1) Rule 4: "--expect-digest is optional and, differing, is exit 1 naming both and serving nothing". cmd/nova-local/main.go:416-427 compares only when m.Ref == model exactly; a --model given without its tag (gemma4 for the listed gemma4:latest), or one not listed at all, matches nothing and the check is skipped: serve goes on and serves a model whose digest nobody checked. The fix: normalize a tagless reference to :latest as ollama does (in the ollama adapter, ollama.go, beside the derived tag's name rule), and with --expect-digest given, a model that matches no listing is exit 1 naming the reference and the digest asked, remedy ollama pull <ref>.
(2) engine.go:97 Refusal.Code (1 retry differently, 2 an input not usable) is never read: main.go:437-440 turns every Refusal into tool.Fail, exit 1. Honour the code: a Refusal of Code 2 is exit 2 through the skeleton's usage refusal, Code 1 (and 0, the zero value) stays exit 1. List in the report each Refusal the adapter returns today and its code.
Libraries considered: none: the change is inside the package's own adapter and verb; the skeleton pkg/tool already has both exits.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set (JOB.md names it): keep it.
STEP 2. Red tests first, in cmd/nova-local/main_test.go, each opening with t.Parallel(), against the fake engine of fake_test.go (no socket, no real time): TestServeExpectDigestNeverSkipsSilently (a tagless --model with a differing --expect-digest is exit 1 naming both, a tagless one with the right digest serves, a --model the engine does not list with --expect-digest is exit 1, and in every refusal the fake records no /api/create and no /api/generate); TestServeRefusalExitsByItsCode (a fake adapter refusal of Code 2 exits 2, of Code 1 exits 1). Run go test -count=1 -timeout 600s ./cmd/nova-local/ -run 'TestServeExpectDigestNeverSkipsSilently|TestServeRefusalExitsByItsCode' and keep the failing lines.
STEP 3. Make them pass in the PATHS files only; TestServeRefusesOnlyPastTheCallersValues stays green unchanged.
STEP 4. Gate: go test -count=1 -timeout 600s ./cmd/nova-local/ ./internal/ci/ and read the last line of each. A red test is named with its file and whether your work changed it.
STEP 5. Commit on your branch with the trailer; the pull request against v1.3/comms states the diff stat, each test and what it pins, and what was not done.
STEP 6. End as JOB.md says.
