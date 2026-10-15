RESULT: local-engine-line-store-on-a-down-engine sha=<sha12> tier: pro
REPO: mas-bandwidth/nova-tools
BASE: v1.3/comms
STOP: every STATUS ENGINE line, an engine down or timed out included, carries store= and shared=, unknown with the reason when the store cannot be read, and TestEveryEngineLineNamesItsStore pins it.
PATHS: cmd/nova-local/main.go, cmd/nova-local/main_test.go, docs/TESTS.md, docs/SPEC-LOCAL.md
SHARED: cmd/nova-local/main.go, docs/TESTS.md, docs/SPEC-LOCAL.md
TEST: cmd/nova-local TestEveryEngineLineNamesItsStore
DEPENDS-ON: local-status-box-fields-always-printed
SOURCE: Glenn, ~14:13Z 2026-10-15: "Please add the work for nova-local to the current sprint, low priority."; the nova-tools roadmap item nova-local-provider-per-machine ("Restore nova-local built to its spec, with local as a provider per fleet machine that the sprint deals to like any route"; from PR #5307, closed 2026-10-10 when the sprint moved to mas-bandwidth/nova-sprint, its head f322683d4d kept on origin/rowan/nova-local).
PRIORITY: low
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 45 minutes; report what you have with the verdict not-done rather than push past it.

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

THE TASK. docs/SPEC-LOCAL.md rule 15: "Every ENGINE line and the serving SERVE OK line carry store= and shared=yes|no|unknown ... unknown when the store or the AI root cannot be read, which is no verdict". At the v1.3/comms tip cmd/nova-local/main.go (around lines 284-291) appends the item of an engine whose state is not up and continues before the store is read, so a down or timed-out engine's line has no store= and no shared=. The change: a down or timed-out engine's ENGINE line prints store=unknown shared=unknown with the reason (the engine did not answer), and an up engine's line is unchanged. status still never refuses on the store. Update docs/TESTS.md's nova-local first run if a printed line changes.
Libraries considered: none: a change to one printed line.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set (JOB.md names it): keep it.
STEP 2. Red test first: TestEveryEngineLineNamesItsStore in cmd/nova-local/main_test.go, opening with t.Parallel(), against the fake engine: one engine answering 503 (state=down) and one whose fake transport returns context.DeadlineExceeded (state=timeout; nothing sleeps) each print store=unknown shared=unknown; an up engine prints its store as today. Run go test -count=1 -timeout 600s ./cmd/nova-local/ -run TestEveryEngineLineNamesItsStore and keep the failing line.
STEP 3. Make it pass in the PATHS files only; TestTheSharedStore stays green unchanged.
STEP 4. Gate: go test -count=1 -timeout 600s ./cmd/nova-local/ ./internal/docs/ ./internal/ci/ and read the last line of each. A red test is named with its file and whether your work changed it.
STEP 5. Commit on your branch with the trailer; the pull request against v1.3/comms states the diff stat, the test and what it pins, and what was not done.
STEP 6. End as JOB.md says.
